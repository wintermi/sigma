// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"encoding/json"
	"testing"

	"github.com/wintermi/sigma"
)

func TestMessageJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message sigma.Message
	}{
		{
			name:    "user text",
			message: sigma.UserText("hello"),
		},
		{
			name: "user image",
			message: sigma.UserContent(
				sigma.ImageBase64("image/png", "aGVsbG8="),
				sigma.ImageURL("image/jpeg", "https://example.test/input.jpg"),
			),
		},
		{
			name: "user document",
			message: sigma.UserContent(
				sigma.DocumentBase64("application/pdf", "input.pdf", "JVBERi0xLjQ="),
				sigma.DocumentURL("application/pdf", "remote.pdf", "https://example.test/input.pdf"),
				sigma.DocumentFileID("application/pdf", "uploaded.pdf", "file_123"),
			),
		},
		{
			name: "assistant text",
			message: sigma.Message{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.Text("hi")},
			},
		},
		{
			name: "assistant thinking",
			message: sigma.Message{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					{
						Type:              sigma.ContentBlockThinking,
						ThinkingText:      "working",
						Signature:         "sig",
						Redacted:          true,
						ProviderSignature: "provider-sig",
					},
				},
			},
		},
		{
			name: "assistant tool call",
			message: sigma.Message{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_1", "lookup", map[string]any{
						"query": "weather",
					}),
				},
			},
		},
		{
			name:    "tool result",
			message: sigma.ToolResult("call_1", "sunny"),
		},
		{
			name:    "tool error",
			message: sigma.ToolError("call_1", "service unavailable"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			roundTripped := assertJSONRoundTrip(t, tt.message)
			if roundTripped.Role != tt.message.Role {
				t.Fatalf("role changed after round trip: got %q want %q", roundTripped.Role, tt.message.Role)
			}
		})
	}
}

func TestRequestWithToolsJSONRoundTrip(t *testing.T) {
	t.Parallel()

	request := sigma.Request{
		SystemPrompt: "Be concise.",
		Messages: []sigma.Message{
			sigma.UserText("What is the weather?"),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_1", "weather", map[string]any{
						"city": "Melbourne",
					}),
				},
			},
			sigma.ToolResult("call_1", "18 C"),
		},
		Tools: []sigma.Tool{
			{
				Name:        "weather",
				Description: "Looks up current weather.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []any{"city"},
				},
			},
			{
				Name:                "web_search",
				ProviderDefinedType: "web_search",
				ProviderDefinedOptions: map[string]any{
					"search_context_size": "low",
					"external_web_access": false,
				},
			},
		},
	}

	roundTripped := assertJSONRoundTrip(t, request)
	if got, want := len(roundTripped.Tools), 2; got != want {
		t.Fatalf("tool count changed after round trip: got %d want %d", got, want)
	}
	if got, want := roundTripped.Tools[0].Name, "weather"; got != want {
		t.Fatalf("tool name changed after round trip: got %q want %q", got, want)
	}
	if got, want := roundTripped.Tools[1].ProviderDefinedType, "web_search"; got != want {
		t.Fatalf("provider-defined tool type changed after round trip: got %q want %q", got, want)
	}
	if got, want := roundTripped.Tools[1].ProviderDefinedOptions["search_context_size"], "low"; got != want {
		t.Fatalf("provider-defined option changed after round trip: got %v want %v", got, want)
	}
	if got, want := roundTripped.Tools[1].ProviderDefinedOptions["external_web_access"], false; got != want {
		t.Fatalf("provider-defined bool option changed after round trip: got %v want %v", got, want)
	}
}

func assertJSONRoundTrip[T any](t *testing.T, value T) T {
	t.Helper()

	before, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal before round trip: %v", err)
	}

	var decoded T
	if err := json.Unmarshal(before, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	after, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal after round trip: %v", err)
	}

	assertSameJSON(t, before, after)
	return decoded
}

func assertSameJSON(t *testing.T, got []byte, want []byte) {
	t.Helper()

	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("unmarshal got json: %v", err)
	}

	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("unmarshal want json: %v", err)
	}

	gotNormalized, err := json.Marshal(gotValue)
	if err != nil {
		t.Fatalf("normalize got json: %v", err)
	}

	wantNormalized, err := json.Marshal(wantValue)
	if err != nil {
		t.Fatalf("normalize want json: %v", err)
	}

	if string(gotNormalized) != string(wantNormalized) {
		t.Fatalf("json changed after round trip:\ngot  %s\nwant %s", gotNormalized, wantNormalized)
	}
}
