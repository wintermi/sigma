// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package radius

import (
	"encoding/json"
	"testing"

	"github.com/wintermi/sigma"
)

func TestRequestPayloadCarriesPromptAndToolsInLeadingSystemMessage(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ID: "radius-model", Provider: sigma.ProviderRadius}
	tests := []struct {
		name       string
		req        sigma.Request
		wantSystem bool
	}{
		{
			name:       "prompt and tools",
			req:        sigma.Request{SystemPrompt: "be brief", Messages: []sigma.Message{sigma.UserText("hi")}, Tools: []sigma.Tool{{Name: "read", Description: "Read a file"}}},
			wantSystem: true,
		},
		{
			name:       "tools only",
			req:        sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}, Tools: []sigma.Tool{{Name: "read"}}},
			wantSystem: true,
		},
		{
			name: "neither",
			req:  sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload, err := requestPayload(model, tt.req, sigma.Options{})
			if err != nil {
				t.Fatalf("requestPayload returned error: %v", err)
			}
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}
			var decoded struct {
				Context map[string]json.RawMessage `json:"context"`
			}
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal returned error: %v", err)
			}
			for _, legacy := range []string{"systemPrompt", "tools"} {
				if _, ok := decoded.Context[legacy]; ok {
					t.Fatalf("context has legacy %q field: %s", legacy, data)
				}
			}
			var messages []struct {
				Role       string `json:"role"`
				Content    any    `json:"content"`
				ToolsAdded []struct {
					Name string `json:"name"`
				} `json:"toolsAdded"`
			}
			if err := json.Unmarshal(decoded.Context["messages"], &messages); err != nil {
				t.Fatalf("Unmarshal messages returned error: %v", err)
			}
			if got := messages[0].Role == "system"; got != tt.wantSystem {
				t.Fatalf("leading system message = %v, want %v: %s", got, tt.wantSystem, data)
			}
			if !tt.wantSystem {
				if len(messages) != 1 {
					t.Fatalf("messages = %d, want 1", len(messages))
				}
				return
			}
			if messages[0].Content != tt.req.SystemPrompt {
				t.Fatalf("system content = %#v, want %q", messages[0].Content, tt.req.SystemPrompt)
			}
			if len(messages[0].ToolsAdded) != len(tt.req.Tools) || messages[0].ToolsAdded[0].Name != tt.req.Tools[0].Name {
				t.Fatalf("toolsAdded = %#v, want %v", messages[0].ToolsAdded, tt.req.Tools)
			}
			if len(messages) != 2 || messages[1].Role != "user" {
				t.Fatalf("messages after system = %s", data)
			}
		})
	}
}
