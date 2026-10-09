// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestResponsesPayloadOmitsToolItemIDWhenReasoningIsDropped(t *testing.T) {
	t.Parallel()

	model, ok := sigma.GetModel(sigma.ProviderOpenAI, "o4-mini")
	if !ok {
		t.Fatal("o4-mini not in catalog")
	}
	reasoning := func(id string, encrypted string) sigma.ContentBlock {
		block := sigma.Thinking("", "")
		block.ProviderSignature = encrypted
		block.ProviderMetadata = map[string]any{"id": id}
		return block
	}
	call := func(callID string, itemID string) sigma.ContentBlock {
		block := sigma.ToolCallBlock(callID, "lookup", map[string]any{})
		block.ProviderMetadata = map[string]any{"id": itemID}
		return block
	}

	tests := []struct {
		name     string
		provider sigma.ProviderID
		api      sigma.API
		content  []sigma.ContentBlock
		wantIDs  map[string]string
	}{
		{
			name:     "same model reasoning without encrypted content",
			provider: model.Provider,
			api:      model.API,
			content:  []sigma.ContentBlock{reasoning("rs_1", ""), call("call_1", "fc_1")},
			wantIDs:  map[string]string{"call_1": ""},
		},
		{
			name:     "foreign provider encrypted reasoning",
			provider: sigma.ProviderOpenAICodex,
			api:      sigma.APIOpenAICodexResponses,
			content:  []sigma.ContentBlock{reasoning("rs_9", "enc"), call("call_9", "fc_9")},
			wantIDs:  map[string]string{"call_9": ""},
		},
		{
			name:     "only calls after a dropped reasoning item",
			provider: model.Provider,
			api:      model.API,
			content: []sigma.ContentBlock{
				reasoning("rs_1", ""), call("call_1", "fc_1"),
				reasoning("rs_2", "enc"), call("call_2", "fc_2"),
			},
			wantIDs: map[string]string{"call_1": "", "call_2": "fc_2"},
		},
		{
			name:    "foreign item ID without reasoning is normalized",
			content: []sigma.ContentBlock{sigma.ToolCallBlock("call_foreign|foreign/item+", "lookup", map[string]any{})},
			wantIDs: map[string]string{"call_foreign": "fc_*"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assistant := sigma.Message{Role: sigma.RoleAssistant, Content: tt.content, Provider: tt.provider, API: tt.api, Model: model.ID}
			payload, err := responsesPayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("q"), assistant}}, sigma.Options{})
			if err != nil {
				t.Fatal(err)
			}
			items, _ := payload["input"].([]map[string]any)
			got := map[string]string{}
			for _, item := range items {
				if item["type"] == "function_call" {
					id, _ := item["id"].(string)
					got[item["call_id"].(string)] = id
				}
			}
			for callID, want := range tt.wantIDs {
				if prefix, ok := strings.CutSuffix(want, "*"); ok && strings.HasPrefix(got[callID], prefix) {
					continue
				}
				if got[callID] != want {
					t.Fatalf("function_call %s id = %q, want %q (items %v)", callID, got[callID], want, items)
				}
			}
		})
	}
}
