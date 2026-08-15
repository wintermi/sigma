// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestGoogleAssistantPartsPreserveOnlyReplayableBlankSignatures(t *testing.T) {
	t.Parallel()

	const validSignature = "AAAAAAAAAAAAAAAAAAAAAA=="
	tests := []struct {
		name     string
		provider sigma.ProviderID
		api      sigma.API
	}{
		{name: "generative ai", provider: sigma.ProviderGoogle, api: sigma.APIGoogleGenerativeAI},
		{name: "vertex", provider: sigma.ProviderGoogleVertex, api: sigma.APIGoogleVertex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model := sigma.Model{ID: "gemini-test", Provider: tt.provider, API: tt.api}
			message := sigma.Message{
				Role:     sigma.RoleAssistant,
				Provider: model.Provider,
				API:      model.API,
				Model:    model.ID,
				Content: []sigma.ContentBlock{
					googleReplayBlock(sigma.Text("   "), ""),
					googleReplayBlock(sigma.Text(""), validSignature),
					googleReplayBlock(sigma.Thinking("   ", ""), validSignature),
					googleReplayBlock(sigma.Text(""), "not-base64"),
					googleReplayBlock(sigma.Thinking("", ""), "not-base64"),
					googleReplayBlock(sigma.Text("visible"), "not-base64"),
					googleReplayBlock(sigma.ToolCallBlock("call_1", "lookup", map[string]any{"city": "Melbourne"}), validSignature),
				},
			}

			parts, err := googleAssistantParts(model, message, newGoogleToolCallIDNormalizer(model))
			if err != nil {
				t.Fatalf("googleAssistantParts returned error: %v", err)
			}
			if got, want := len(parts), 4; got != want {
				t.Fatalf("parts = %#v, want %d", parts, want)
			}
			if got, want := parts[0]["thoughtSignature"], validSignature; got != want || parts[0]["text"] != "" {
				t.Fatalf("signed empty text = %#v, want empty text with signature %q", parts[0], want)
			}
			if got, want := parts[1]["thoughtSignature"], validSignature; got != want || parts[1]["thought"] != true {
				t.Fatalf("signed blank thinking = %#v, want thinking with signature %q", parts[1], want)
			}
			if got := parts[2]["text"]; got != "visible" {
				t.Fatalf("nonblank text = %#v, want visible text", parts[2])
			}
			if _, ok := parts[2]["thoughtSignature"]; ok {
				t.Fatalf("nonblank text retained invalid signature: %#v", parts[2])
			}
			if _, ok := parts[3]["functionCall"]; !ok || parts[3]["thoughtSignature"] != validSignature {
				t.Fatalf("tool call = %#v, want ordered signed function call", parts[3])
			}
		})
	}
}

func TestGoogleAssistantPartsDropBlankForeignSignatures(t *testing.T) {
	t.Parallel()

	const validSignature = "AAAAAAAAAAAAAAAAAAAAAA=="
	model := sigma.Model{ID: "gemini-test", Provider: sigma.ProviderGoogle, API: sigma.APIGoogleGenerativeAI}
	tests := []struct {
		name     string
		provider sigma.ProviderID
		api      sigma.API
		model    sigma.ModelID
	}{
		{name: "provider", provider: sigma.ProviderGoogleVertex, api: model.API, model: model.ID},
		{name: "api", provider: model.Provider, api: sigma.APIGoogleVertex, model: model.ID},
		{name: "model", provider: model.Provider, api: model.API, model: "other-model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			message := sigma.Message{
				Role:     sigma.RoleAssistant,
				Provider: tt.provider,
				API:      tt.api,
				Model:    tt.model,
				Content: []sigma.ContentBlock{
					googleReplayBlock(sigma.Text(""), validSignature),
					googleReplayBlock(sigma.Thinking("", ""), validSignature),
					sigma.ToolCallBlock("call_1", "lookup", map[string]any{}),
				},
			}

			parts, err := googleAssistantParts(model, message, newGoogleToolCallIDNormalizer(model))
			if err != nil {
				t.Fatalf("googleAssistantParts returned error: %v", err)
			}
			if got, want := len(parts), 1; got != want {
				t.Fatalf("parts = %#v, want only the tool call", parts)
			}
			if _, ok := parts[0]["functionCall"]; !ok {
				t.Fatalf("remaining part = %#v, want function call", parts[0])
			}
		})
	}
}

func googleReplayBlock(block sigma.ContentBlock, signature string) sigma.ContentBlock {
	block.ProviderSignature = signature
	return block
}
