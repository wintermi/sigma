// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

func TestResponsesCompatibilityAcrossRequestPaths(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                             string
		provider                         sigma.ProviderID
		catalogID                        sigma.ModelID
		background, unsupported, wantTTL bool
	}{
		{name: "catalog luna", provider: sigma.ProviderOpenAI, catalogID: "gpt-5.6-luna", wantTTL: true},
		{name: "catalog sol", provider: sigma.ProviderOpenAI, catalogID: "gpt-5.6-sol", wantTTL: true},
		{name: "catalog terra", provider: sigma.ProviderOpenAI, catalogID: "gpt-5.6-terra", wantTTL: true},
		{name: "Azure catalog unchanged", provider: sigma.ProviderAzureOpenAIResponses, catalogID: "gpt-5.6-luna"},
		{name: "Codex catalog unchanged", provider: sigma.ProviderOpenAICodex, catalogID: "gpt-5.6-luna"},
		{name: "background catalog", provider: sigma.ProviderOpenAI, catalogID: "gpt-5.6-luna", background: true, wantTTL: true},
		{name: "direct opt out", provider: sigma.ProviderOpenAI, unsupported: true, wantTTL: true},
		{name: "Azure opt out", provider: sigma.ProviderAzureOpenAIResponses, unsupported: true, wantTTL: true},
		{name: "background opt out", provider: sigma.ProviderOpenAI, background: true, unsupported: true, wantTTL: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				if tt.background {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"id":"resp_background","status":"queued"}`))
				} else {
					writeResponsesSSE(t, w, responsesCompletedEvent)
				}
			}))
			t.Cleanup(server.Close)
			model := responsesTestModel(tt.provider)
			if tt.provider == sigma.ProviderAzureOpenAIResponses {
				model = azureResponsesTestModel(tt.provider)
			}
			if tt.catalogID != "" {
				var ok bool
				model, ok = sigma.DefaultRegistry().Model(tt.provider, tt.catalogID)
				if !ok {
					t.Fatalf("missing catalog model %s/%s", tt.provider, tt.catalogID)
				}
			}
			if tt.unsupported {
				model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{
					SupportsExplicitPromptCacheMode: true, SupportsMaxOutputTokens: sigma.OpenAICompatUnsupported,
				}
			}
			if model.ProviderMetadata == nil {
				model.ProviderMetadata = map[string]any{}
			}
			model.ProviderMetadata["baseURL"] = server.URL
			opts := []sigma.Option{sigma.WithMaxTokens(1), sigma.WithCacheRetention(sigma.CacheRetentionLong), sigma.WithSessionID("session")}
			var client *sigma.Client
			switch tt.provider {
			case sigma.ProviderAzureOpenAIResponses:
				client = azureResponsesTestClient(t, tt.provider, model, azureAPIKeyResolver("test-key"))
				opts = append(opts, openai.WithAzureResponsesEndpoint(tt.provider, server.URL))
			case sigma.ProviderOpenAICodex:
				client = codexResponsesTestClient(t, tt.provider, model, server.URL, codexTokenProvider("test-token"))
				opts = append(opts, sigma.WithProviderOption(tt.provider, "extra_body", map[string]any{"max_output_tokens": 88}))
			default:
				client = responsesTestClient(t, tt.provider, model, server.URL)
			}
			req := sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}
			if tt.background {
				if _, err := client.SubmitDeferred(context.Background(), model, req, opts...); err != nil {
					t.Fatal(err)
				}
			} else if _, err := client.Complete(context.Background(), model, req, opts...); err != nil {
				t.Fatal(err)
			}
			payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
			if tt.wantTTL {
				if !reflect.DeepEqual(payload["prompt_cache_options"], map[string]any{"ttl": "30m"}) {
					t.Fatalf("cache options = %#v", payload["prompt_cache_options"])
				}
				if _, ok := payload["prompt_cache_retention"]; ok {
					t.Fatal("legacy retention sent with cache options")
				}
			} else {
				if payload["prompt_cache_retention"] != "24h" {
					t.Fatalf("legacy retention = %#v", payload["prompt_cache_retention"])
				}
				if _, ok := payload["prompt_cache_options"]; ok {
					t.Fatal("cache options sent for unflagged model")
				}
			}
			if payload["prompt_cache_key"] != "session" {
				t.Fatalf("cache key = %#v", payload["prompt_cache_key"])
			}
			if tt.unsupported || tt.provider == sigma.ProviderOpenAICodex {
				if _, ok := payload["max_output_tokens"]; ok {
					t.Fatal("unsupported max_output_tokens sent")
				}
			} else if payload["max_output_tokens"] != float64(16) {
				t.Fatalf("output limit = %#v", payload["max_output_tokens"])
			}
		})
	}
}
