// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestHostedReplayValidation(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderAnthropic, API: sigma.APIAnthropicMessages, ID: "claude", SupportsThinking: true}
	for _, tc := range []struct {
		name string
		edit func([]sigma.ContentBlock)
		omit bool
	}{
		{name: "valid", edit: func([]sigma.ContentBlock) {}},
		{name: "null", edit: func(b []sigma.ContentBlock) { b[1].ProviderMetadata[hostedReplayKey] = nil }},
		{name: "scalar", edit: func(b []sigma.ContentBlock) { b[1].ProviderMetadata[hostedReplayKey] = "invalid" }},
		{name: "array", edit: func(b []sigma.ContentBlock) { b[1].ProviderMetadata[hostedReplayKey] = []any{} }},
		{name: "results object", edit: func(b []sigma.ContentBlock) {
			b[1].ProviderMetadata[hostedReplayKey].(map[string]any)["results_after"] = map[string]any{}
		}},
		{name: "null result", edit: func(b []sigma.ContentBlock) {
			b[1].ProviderMetadata[hostedReplayKey].(map[string]any)["results_after"] = []any{nil}
		}},
		{name: "unknown result", edit: func(b []sigma.ContentBlock) {
			b[1].ProviderMetadata[hostedReplayKey].(map[string]any)["results_after"] = []any{map[string]any{"type": "unknown", "tool_use_id": "server:1"}}
		}},
		{name: "forward association", edit: func(b []sigma.ContentBlock) { b[0], b[1] = b[1], b[0] }},
		{name: "server id mismatch", edit: func(b []sigma.ContentBlock) {
			b[0].ProviderMetadata[hostedReplayKey].(map[string]any)["server_tool_use"].(map[string]any)["id"] = "other"
		}},
		{name: "server type mismatch", edit: func(b []sigma.ContentBlock) {
			b[0].ProviderMetadata[hostedReplayKey].(map[string]any)["server_tool_use"].(map[string]any)["type"] = "tool_use"
		}},
		{name: "missing source", omit: true, edit: func(b []sigma.ContentBlock) {
			delete(b[1].ProviderMetadata[hostedReplayKey].(map[string]any), "provider")
		}},
		{name: "foreign source", omit: true, edit: func(b []sigma.ContentBlock) { b[1].ProviderMetadata[hostedReplayKey].(map[string]any)["api"] = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			call := sigma.ToolCallBlock("server:1", "web_search", map[string]any{})
			callMetadata := hostedReplayMetadata(model)
			callMetadata["server_tool_use"] = map[string]any{"type": "server_tool_use", "id": "server:1", "name": "web_search", "input": map[string]any{}}
			call.ProviderMetadata = map[string]any{"type": "server_tool_use", hostedReplayKey: callMetadata}
			anchor := sigma.Text("visible")
			results := hostedReplayMetadata(model)
			results["results_after"] = []map[string]any{{"type": "web_search_tool_result", "tool_use_id": "server:1", "content": []any{}}}
			anchor.ProviderMetadata = map[string]any{hostedReplayKey: results}
			blocks := []sigma.ContentBlock{call, anchor}
			tc.edit(blocks)
			payload, err := messagesPayload(model, sigma.Request{Messages: []sigma.Message{{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, Content: blocks}}}, sigma.Options{}, messagesCompat{})
			if tc.name == "valid" || tc.omit {
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "server_tool_use") == tc.omit {
					t.Fatalf("payload = %s", data)
				}
			} else {
				var invalid *sigma.Error
				if !errors.As(err, &invalid) || invalid.Code != sigma.ErrorInvalidRequest {
					t.Fatalf("error = %v", err)
				}
			}
		})
	}
}

func TestAnthropicBlankContentFiltering(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderAnthropic, API: sigma.APIAnthropicMessages, ID: "claude", SupportsThinking: true}
	for _, blank := range []string{"", " ", "\t\n", "\xff"} {
		t.Run("blank_"+blank, func(t *testing.T) {
			t.Parallel()
			payload, err := messagesPayload(model, sigma.Request{Messages: []sigma.Message{
				sigma.UserText(" first \n"), sigma.UserText(blank),
				{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, Content: []sigma.ContentBlock{sigma.Text(blank)}},
			}}, sigma.Options{CacheRetention: sigma.CacheRetentionShort}, messagesCompat{})
			if err != nil {
				t.Fatal(err)
			}
			messages := payload["messages"].([]map[string]any)
			if len(messages) != 1 {
				t.Fatalf("messages = %#v", messages)
			}
			blocks := messages[0]["content"].([]map[string]any)
			if blocks[0]["text"] != " first \n" || blocks[0]["cache_control"] == nil {
				t.Fatalf("filtered cache placement = %#v", blocks)
			}
		})
	}
	for _, blocks := range [][]sigma.ContentBlock{nil, {sigma.Text("")}, {sigma.Text(" ")}} {
		result, err := anthropicToolResultContent(model, sigma.Message{Role: sigma.RoleTool, ToolCallID: "call", Content: blocks})
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "[]" && string(data) != `""` && string(data) != `" "` {
			t.Fatalf("empty result = %s", data)
		}
	}
}
