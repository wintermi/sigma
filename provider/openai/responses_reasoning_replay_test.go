// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
)

// With store:false, OpenAI resolves a replayed reasoning item only through its
// encrypted content; a bare or fabricated item ID fails the request.
func TestResponsesReplaysOnlyResolvableReasoningItems(t *testing.T) {
	t.Parallel()

	reasoning := func(id string, encrypted string) sigma.ContentBlock {
		block := sigma.Thinking("plan", "")
		block.ProviderSignature = encrypted
		if id != "" {
			block.ProviderMetadata = map[string]any{"id": id}
		}
		return block
	}
	tests := []struct {
		name     string
		block    sigma.ContentBlock
		store    bool
		wantItem bool
	}{
		{name: "encrypted content", block: reasoning("rs_abc", "enc_abc"), wantItem: true},
		{name: "provider id without encrypted content", block: reasoning("rs_abc", "")},
		{name: "no provider id", block: reasoning("", "")},
		{name: "stored provider id", block: reasoning("rs_abc", ""), store: true, wantItem: true},
		{name: "stored without provider id", block: reasoning("", ""), store: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-reasoning-replay-test")
			model := responsesTestModel(providerID)
			client := responsesTestClient(t, providerID, model, server.URL)
			assistant := sigma.Message{
				Role:     sigma.RoleAssistant,
				Content:  []sigma.ContentBlock{tt.block, sigma.Text("answer")},
				Provider: model.Provider,
				API:      model.API,
				Model:    model.ID,
			}
			opts := []sigma.Option{}
			if tt.store {
				opts = append(opts, sigma.WithProviderOption(providerID, "store", true))
			}
			req := sigma.Request{Messages: []sigma.Message{sigma.UserText("q"), assistant, sigma.UserText("again")}}
			if _, err := client.Complete(context.Background(), model, req, opts...); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			var items []map[string]any
			for _, value := range decodeResponsesPayload(t, receiveRequest(t, requests).Body)["input"].([]any) {
				if item := value.(map[string]any); item["type"] == "reasoning" {
					items = append(items, item)
				}
			}
			if !tt.wantItem {
				if len(items) != 0 {
					t.Fatalf("replayed unresolvable reasoning items: %#v", items)
				}
				return
			}
			if len(items) != 1 || items[0]["id"] != "rs_abc" {
				t.Fatalf("reasoning items = %#v, want one item rs_abc", items)
			}
			if encrypted := tt.block.ProviderSignature; encrypted != "" && items[0]["encrypted_content"] != encrypted {
				t.Fatalf("encrypted_content = %v, want %q", items[0]["encrypted_content"], encrypted)
			}
		})
	}
}
