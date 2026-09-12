// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package transform

import (
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

func TestPrepareReplayBeforeToolPlanning(t *testing.T) {
	t.Parallel()
	for _, stop := range []sigma.StopReason{sigma.StopReasonError, sigma.StopReasonAborted} {
		t.Run(string(stop), func(t *testing.T) {
			t.Parallel()
			model := sigma.Model{Provider: sigma.ProviderAnthropic, API: sigma.APIAnthropicMessages, ID: "claude"}
			text := sigma.Text(" exact \n\t")
			text.Signature = "signature"
			text.ProviderSignature = "opaque"
			text.ProviderMetadata = map[string]any{"opaque": true}
			usage := &sigma.Usage{TotalTokens: 7}
			req := sigma.Request{Tools: []sigma.Tool{{Name: "base"}, {Name: "late"}}, Messages: []sigma.Message{
				{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock("load", "base", nil)}},
				{Role: sigma.RoleTool, ToolCallID: "load", AddedToolNames: []string{"late"}},
				{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, StopReason: stop, Usage: usage, ProviderThinkingLevel: "high", Content: []sigma.ContentBlock{text, sigma.Thinking("secret", "sig"), sigma.ToolCallBlock("failed", "late", `{"x":`), sigma.Text(" \t")}},
				{Role: sigma.RoleTool, ToolCallID: "failed", AddedToolNames: []string{"bad"}, Content: []sigma.ContentBlock{sigma.Text("discard")}},
				{Role: sigma.RoleAssistant, StopReason: stop, Content: []sigma.ContentBlock{sigma.Thinking("hidden", "")}},
				{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock("unanswered", "base", nil)}},
				sigma.UserText("continue"),
			}}
			prepared := PrepareReplay(model, req)
			if len(prepared.Messages) != 5 {
				t.Fatalf("messages = %#v", prepared.Messages)
			}
			kept := prepared.Messages[2]
			if !reflect.DeepEqual(kept.Content, []sigma.ContentBlock{sigma.Text(text.Text)}) || kept.ProviderThinkingLevel != "" {
				t.Fatalf("retained = %#v", kept)
			}
			if kept.Provider != model.Provider || kept.API != model.API || kept.Model != model.ID || kept.StopReason != stop || kept.Usage != usage {
				t.Fatalf("provenance/usage = %#v", kept)
			}
			plan := PlanDeferredTools(prepared, true, nil)
			if _, ok := plan.Deferred["late"]; !ok {
				t.Fatalf("failed call loaded a deferred tool: %#v", plan)
			}
			transformed, err := Transform(Input{TargetModel: model, Request: req, Compatibility: Compatibility{DropUnansweredToolCalls: true}})
			if err != nil {
				t.Fatal(err)
			}
			synthetic := 0
			for _, msg := range transformed.Messages {
				if msg.Role == sigma.RoleTool && msg.ToolCallID == "unanswered" {
					synthetic++
				}
			}
			if synthetic != 1 {
				t.Fatalf("successful unanswered call got %d synthetic results", synthetic)
			}
			if req.Messages[2].Content[0].Signature != "signature" || len(req.Messages[2].Content) != 4 {
				t.Fatal("mutated original")
			}
		})
	}
}
