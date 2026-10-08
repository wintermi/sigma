// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
)

func TestGeneratedPartialThinkingMapsAcceptDefaultLevels(t *testing.T) {
	t.Parallel()
	for _, id := range []sigma.ModelID{"gpt-5.4", "gpt-5.5"} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()
			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)
			model, ok := sigma.DefaultRegistry().Model(sigma.ProviderOpenAI, id)
			if !ok {
				t.Fatal("missing model")
			}
			model.ProviderMetadata["baseURL"] = server.URL
			client := responsesTestClient(t, sigma.ProviderOpenAI, model, server.URL)
			for _, level := range []sigma.ThinkingLevel{sigma.ThinkingLevelLow, sigma.ThinkingLevelMedium, sigma.ThinkingLevelHigh} {
				if _, err := client.Complete(context.Background(), model, deferredToolsRequest(), sigma.WithReasoningLevel(level)); err != nil {
					t.Fatalf("%s: %v", level, err)
				}
				payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
				if effort := payload["reasoning"].(map[string]any)["effort"]; effort != string(level) {
					t.Fatalf("%s: reasoning effort = %v", level, effort)
				}
			}
			_, err := client.Complete(context.Background(), model, deferredToolsRequest(), sigma.WithReasoningLevel(sigma.ThinkingLevelMinimal))
			if !errors.Is(err, sigma.ErrInvalidOptions) {
				t.Fatalf("minimal error = %v, want ErrInvalidOptions", err)
			}
		})
	}
}

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
			}{{sigma.ThinkingLevelOff, "none"}, {sigma.ThinkingLevelMinimal, "low"}, {sigma.ThinkingLevel("max"), "max"}} {
				_, err := client.Complete(context.Background(), model, deferredToolsRequest(), sigma.WithReasoningLevel(tt.level))
				if err != nil {
					t.Fatal(err)
				}
				request := receiveRequest(t, requests)
				payload := decodeResponsesPayload(t, request.Body)
				if request.Path != "/codex/responses" || payload["model"] != string(id) {
					t.Fatalf("wrong Codex route: %s %v", request.Path, payload["model"])
				}
				// Codex applies its own default effort when reasoning is omitted, so
				// Off must be sent explicitly.
				if reasoning, _ := payload["reasoning"].(map[string]any); reasoning["effort"] != tt.effort {
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
