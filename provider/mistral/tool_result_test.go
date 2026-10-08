// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
)

// Conversations function results are plain strings, so tool images travel as
// image chunks in one user entry after the consecutive results.
func TestConversationToolResultsKeepImagesAsImageChunks(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMistralSSE(t, w, "event: conversation.response.done\ndata: {\"type\":\"conversation.response.done\",\"stop_reason\":\"stop\"}\n\n")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-tool-result-images")
	model := mistralTestModel(providerID)
	model.SupportedInputs = []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage}
	client := mistralTestClient(t, providerID, model, server.URL)

	_, _ = client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{
		sigma.UserText("Run the tools."),
		{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{
			sigma.ToolCallBlock("call_a", "shot", map[string]any{}),
			sigma.ToolCallBlock("call_b", "shot", map[string]any{}),
			sigma.ToolCallBlock("call_c", "noop", map[string]any{}),
		}},
		{Role: sigma.RoleTool, ToolCallID: "call_a", ToolName: "shot", Content: []sigma.ContentBlock{sigma.ImageBase64("image/png", "aGk=")}},
		{Role: sigma.RoleTool, ToolCallID: "call_b", ToolName: "shot", Content: []sigma.ContentBlock{sigma.Text("second"), sigma.ImageBase64("image/png", "aGk=")}},
		{Role: sigma.RoleTool, ToolCallID: "call_c", ToolName: "noop"},
	}})

	inputs := decodePayload(t, receiveRequest(t, requests).Body)["inputs"].([]any)
	first := 0
	for first < len(inputs) && inputs[first].(map[string]any)["type"] != "function.result" {
		first++
	}
	if first+4 > len(inputs) {
		t.Fatalf("inputs = %#v, want three function results and an image entry", inputs)
	}
	var results []string
	for _, input := range inputs[first : first+3] {
		entry := input.(map[string]any)
		if entry["type"] != "function.result" {
			t.Fatalf("entry %#v interrupts the consecutive function results", entry)
		}
		results = append(results, entry["result"].(string))
	}
	want := []string{"(see attached image)", "second", "(no tool output)"}
	for i := range want {
		if results[i] != want[i] {
			t.Fatalf("results = %q, want %q", results, want)
		}
	}
	images := inputs[first+3].(map[string]any)
	content := images["content"].([]any)
	if images["role"] != "user" || len(content) != 2 {
		t.Fatalf("image entry = %#v, want one user entry with both images", images)
	}
	for _, chunk := range content {
		if chunk.(map[string]any)["image_url"] != "data:image/png;base64,aGk=" {
			t.Fatalf("image chunk = %#v, want the tool image", chunk)
		}
	}
}
