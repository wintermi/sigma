// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"encoding/json"
	"strings"
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

func TestContentBlockUnknownKindPreservesExtraFields(t *testing.T) {
	t.Parallel()

	input := []byte(`{"type":"provider-block","providerID":"abc","nested":{"count":9007199254740993},"items":[1,2]}`)
	var block sigma.ContentBlock
	if err := json.Unmarshal(input, &block); err != nil {
		t.Fatalf("unmarshal content block: %v", err)
	}
	if got, want := block.Type, sigma.ContentBlockType("provider-block"); got != want {
		t.Fatalf("block type = %q, want %q", got, want)
	}
	if got, want := block.ExtraFields["providerID"], "abc"; got != want {
		t.Fatalf("extra providerID = %v, want %v", got, want)
	}
	nested, ok := block.ExtraFields["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested extra type = %T, want map", block.ExtraFields["nested"])
	}
	if got, want := nested["count"], json.Number("9007199254740993"); got != want {
		t.Fatalf("nested count = %#v, want %#v", got, want)
	}

	output, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal content block: %v", err)
	}
	assertSameJSON(t, output, input)
}

func TestToolCallArgumentsJSONRoundTripPreservesLargeIntegers(t *testing.T) {
	t.Parallel()

	input := []byte(`{"role":"assistant","content":[{"type":"tool-call","toolCallID":"call_1","toolName":"lookup","toolArguments":{"id":9007199254740993}}]}`)
	var message sigma.Message
	if err := json.Unmarshal(input, &message); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	args, ok := message.Content[0].ToolArguments.(map[string]any)
	if !ok {
		t.Fatalf("tool arguments type = %T, want map", message.Content[0].ToolArguments)
	}
	if got, want := args["id"], json.Number("9007199254740993"); got != want {
		t.Fatalf("tool argument id = %#v, want %#v", got, want)
	}

	output, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	assertSameJSON(t, output, input)
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

func TestContentBlockMarshalPreservesLargeIntegersAlongsideExtraFields(t *testing.T) {
	t.Parallel()

	input := []byte(`{"type":"tool-call","toolCallID":"c1","toolName":"lookup","toolArguments":{"id":9007199254740993},"vendorTag":"x"}`)
	var block sigma.ContentBlock
	if err := json.Unmarshal(input, &block); err != nil {
		t.Fatalf("unmarshal content block: %v", err)
	}
	if got, want := block.ExtraFields["vendorTag"], "x"; got != want {
		t.Fatalf("extra vendorTag = %v, want %v", got, want)
	}

	output, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal content block: %v", err)
	}
	if !strings.Contains(string(output), "9007199254740993") {
		t.Fatalf("marshal lost 64-bit precision: %s", output)
	}
	assertSameJSON(t, output, input)
}

func TestContentBlockUnmarshalTreatsCaseVariantKeysAsKnown(t *testing.T) {
	t.Parallel()

	var block sigma.ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"text","Text":"hi"}`), &block); err != nil {
		t.Fatalf("unmarshal content block: %v", err)
	}
	if got, want := block.Text, "hi"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if len(block.ExtraFields) != 0 {
		t.Fatalf("extra fields = %#v, want none", block.ExtraFields)
	}

	output, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal content block: %v", err)
	}
	if strings.Contains(string(output), `"Text"`) {
		t.Fatalf("marshal emitted duplicate case-variant key: %s", output)
	}
}
