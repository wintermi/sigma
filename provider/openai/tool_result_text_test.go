// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestChatToolResultTextJoinsBlocksAndFillsEmptyResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []sigma.ContentBlock
		want    string
	}{
		{name: "blocks are newline separated", content: []sigma.ContentBlock{sigma.Text("line one"), sigma.Text("line two")}, want: "line one\nline two"},
		{name: "empty result gets a placeholder", content: nil, want: "(no tool output)"},
		{name: "empty text block gets a placeholder", content: []sigma.ContentBlock{sigma.Text("")}, want: "(no tool output)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model := sigma.Model{Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAICompletions, ID: "test"}
			req := sigma.Request{Messages: []sigma.Message{
				sigma.UserText("run"),
				{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_1", "run", map[string]any{})}},
				{Role: sigma.RoleTool, ToolCallID: "call_1", Content: tt.content},
			}}
			payload, err := chatCompletionsPayload(model, req, sigma.Options{}, completionsCompat{})
			if err != nil {
				t.Fatalf("chatCompletionsPayload returned error: %v", err)
			}
			messages, _ := payload["messages"].([]map[string]any)
			var got any
			for _, message := range messages {
				if message["role"] == "tool" {
					got = message["content"]
				}
			}
			if got != tt.want {
				t.Fatalf("tool content = %#v, want %q", got, tt.want)
			}
		})
	}
}
