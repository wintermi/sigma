// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package opencode_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/opencode"
)

func TestOpenCodeRoutedResponsesReplaysHistoryRecordedWithCatalogAPI(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	client := opencodeTestClient(t, sigma.ProviderOpenCode, "gpt-routed", sigma.APIOpenAIResponses, nil, true, opencode.RegisterZen, server.URL)
	model, ok := client.GetModel(sigma.ProviderOpenCode, "gpt-routed")
	if !ok {
		t.Fatal("model not registered")
	}
	thinking := sigma.Thinking("plan", "")
	thinking.ProviderSignature = "encrypted-reasoning"
	thinking.ProviderMetadata = map[string]any{"id": "rs_1"}
	call := sigma.ToolCallBlock("call_1", "lookup", map[string]any{})
	call.ProviderMetadata = map[string]any{"id": "fc_1"}
	// Callers persist the catalog API, which is what Model.API reports.
	assistant := sigma.Message{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, Content: []sigma.ContentBlock{thinking, call}}

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Tools: []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}},
		Messages: []sigma.Message{
			sigma.UserText("q"),
			assistant,
			{Role: sigma.RoleTool, ToolCallID: "call_1", ToolName: "lookup", Content: []sigma.ContentBlock{sigma.Text("ok")}},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatal(err)
	}
	var encrypted, itemID any
	for _, item := range payload.Input {
		switch item["type"] {
		case "reasoning":
			encrypted = item["encrypted_content"]
		case "function_call":
			itemID = item["id"]
		}
	}
	if encrypted != "encrypted-reasoning" || itemID != "fc_1" {
		t.Fatalf("encrypted_content = %v, function_call id = %v; want same-model replay (input %v)", encrypted, itemID, payload.Input)
	}
}
