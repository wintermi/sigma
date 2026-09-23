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

func TestRegistryRefreshCodexReasoning(t *testing.T) {
	t.Parallel()
	for _, id := range []sigma.ModelID{"gpt-6-luna", "gpt-6-sol"} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()
			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)
			model, ok := sigma.DefaultRegistry().Model(sigma.ProviderOpenAICodex, id)
			if !ok {
				t.Fatal("missing model")
			}
			model.ProviderMetadata["baseURL"] = server.URL
			client := codexResponsesTestClient(t, sigma.ProviderOpenAICodex, model, server.URL, codexTokenProvider("catalog-oauth-token"))
			for _, tt := range []struct {
				level  sigma.ThinkingLevel
				effort string
			}{{sigma.ThinkingLevelOff, ""}, {sigma.ThinkingLevelMinimal, "low"}, {sigma.ThinkingLevel("max"), "max"}} {
				_, err := client.Complete(context.Background(), model, deferredToolsRequest(), sigma.WithReasoningLevel(tt.level))
				if err != nil {
					t.Fatal(err)
				}
				request := receiveRequest(t, requests)
				payload := decodeResponsesPayload(t, request.Body)
				if request.Path != "/responses" || payload["model"] != string(id) {
					t.Fatalf("wrong Codex route: %s %v", request.Path, payload["model"])
				}
				if tt.level == sigma.ThinkingLevelOff {
					if payload["reasoning"] != nil {
						t.Fatalf("off should omit reasoning, got %v", payload["reasoning"])
					}
				} else if payload["reasoning"].(map[string]any)["effort"] != tt.effort {
					t.Fatalf("reasoning=%v", payload["reasoning"])
				}
				assertAdditionalToolsPayload(t, request.Body)
				assertHeader(t, request.Headers, "chatgpt-account-id", "acct_codex")
				if _, ok := payload["prompt_cache_options"]; ok {
					t.Fatal("direct cache options leaked into Codex")
				}
			}
		})
	}
}
