// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/wintermi/sigma"
)

func TestAstraCatalogReasoningAndToolReplay(t *testing.T) {
	t.Parallel()
	for _, provider := range []sigma.ProviderID{sigma.ProviderOpenAI, sigma.ProviderOpenAICodex} {
		t.Run(string(provider), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)
			model, client := astraCatalogClient(t, provider, server.URL)
			for _, level := range []sigma.ThinkingLevel{
				sigma.ThinkingLevelOff, sigma.ThinkingLevelMinimal,
				sigma.ThinkingLevelLow, sigma.ThinkingLevelMedium, sigma.ThinkingLevelHigh,
				sigma.ThinkingLevelXHigh, sigma.ThinkingLevel("max"),
			} {
				t.Run(string(level), func(t *testing.T) {
					req := deferredToolsRequest()
					req.Messages = append(req.Messages, sigma.Message{Role: sigma.RoleUser, Content: []sigma.ContentBlock{
						sigma.Text("Inspect this image"), sigma.ImageBase64("image/png", "aGk="),
					}})
					before := calls.Load()
					_, err := client.Complete(context.Background(), model, req,
						sigma.WithReasoningLevel(level), sigma.WithCacheRetention(sigma.CacheRetentionNone))
					if level == sigma.ThinkingLevelOff || level == sigma.ThinkingLevelMinimal {
						if model.SupportsThinkingLevel(level) || !errors.Is(err, sigma.ErrInvalidOptions) {
							t.Fatalf("unsupported level %s: supported=%v, error=%v", level, model.SupportsThinkingLevel(level), err)
						}
						if calls.Load() != before {
							t.Fatal("unsupported reasoning reached the provider")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if effort, ok := model.ProviderThinkingLevel(level); !ok || effort != string(level) {
						t.Fatalf("catalog reasoning level %s = %q, %v", level, effort, ok)
					}
					request := receiveRequest(t, requests)
					assertAdditionalToolsPayload(t, request.Body)
					payload := decodeResponsesPayload(t, request.Body)
					if request.Path != "/responses" || payload["model"] != "gpt-6-astra" {
						t.Fatalf("wrong route/model: %s, %v", request.Path, payload["model"])
					}
					if payload["reasoning"].(map[string]any)["effort"] != string(level) {
						t.Fatalf("reasoning = %#v", payload["reasoning"])
					}
					input := payload["input"].([]any)
					content := input[len(input)-1].(map[string]any)["content"].([]any)
					image := content[1].(map[string]any)
					if image["type"] != "input_image" || image["image_url"] != "data:image/png;base64,aGk=" {
						t.Fatalf("image input = %#v", image)
					}
					if provider == sigma.ProviderOpenAI {
						assertHeader(t, request.Headers, "Authorization", "Bearer resolved-key")
						if payload["prompt_cache_options"].(map[string]any)["mode"] != "explicit" {
							t.Fatalf("cache options = %#v", payload["prompt_cache_options"])
						}
					} else {
						assertHeader(t, request.Headers, "chatgpt-account-id", "acct_codex")
						if _, ok := payload["prompt_cache_options"]; ok {
							t.Fatal("Codex received direct OpenAI cache options")
						}
					}
				})
			}
		})
	}
}

func TestAstraCatalogUsagePricing(t *testing.T) {
	t.Parallel()
	for _, provider := range []sigma.ProviderID{sigma.ProviderOpenAI, sigma.ProviderOpenAICodex} {
		for _, inputTokens := range []int{272000, 272001} {
			for _, tier := range []struct {
				name       string
				multiplier float64
			}{{"default", 1}, {"flex", 0.5}, {"priority", 2}} {
				t.Run(fmt.Sprintf("%s/%d/%s", provider, inputTokens, tier.name), func(t *testing.T) {
					t.Parallel()
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						writeResponsesSSE(t, w, fmt.Sprintf(`data: {"type":"response.completed","response":{"id":"resp_astra","model":"gpt-6-astra","status":"completed","service_tier":%q,"output":[],"usage":{"input_tokens":%d,"output_tokens":1000,"input_tokens_details":{"cached_tokens":20000}}}}

`, tier.name, inputTokens))
					}))
					t.Cleanup(server.Close)
					model, client := astraCatalogClient(t, provider, server.URL)
					final, err := client.Complete(context.Background(), model,
						sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
					if err != nil {
						t.Fatal(err)
					}
					inputRate, outputRate, readRate, writeRate := 10.0, 50.0, 1.0, 12.5
					if inputTokens > 272000 {
						inputRate, outputRate, readRate, writeRate = 20, 75, 2, 25
					}
					if final.Usage == nil || final.Usage.InputTokens != inputTokens-20000 || final.Usage.CacheReadInputTokens != 20000 {
						t.Fatalf("usage = %#v", final.Usage)
					}
					if final.Cost == nil {
						t.Fatal("missing estimated cost")
					}
					wantInput := float64(inputTokens-20000) * inputRate / 1e6 * tier.multiplier
					wantOutput := 1000 * outputRate / 1e6 * tier.multiplier
					wantRead := 20000 * readRate / 1e6 * tier.multiplier
					for _, check := range []struct{ got, want float64 }{
						{final.Cost.InputCost, wantInput},
						{final.Cost.OutputCost, wantOutput},
						{final.Cost.CacheReadInputCost, wantRead},
						{final.Cost.TotalCost, wantInput + wantOutput + wantRead},
					} {
						if math.Abs(check.got-check.want) > 1e-10 {
							t.Fatalf("cost = %v, want %v", check.got, check.want)
						}
					}
					// Cache writes are not emitted by the Responses parser, but callers
					// can account for them through the shared usage API.
					if tier.name != "default" {
						return
					}
					cost := sigma.CostForUsage(model, sigma.Usage{
						InputTokens: inputTokens - 30000, OutputTokens: 1000,
						CacheReadInputTokens: 20000, CacheWriteInputTokens: 10000,
					})
					want := float64(inputTokens-30000)*inputRate/1e6 + 1000*outputRate/1e6 + 20000*readRate/1e6 + 10000*writeRate/1e6
					if math.Abs(cost.TotalCost-want) > 1e-10 || cost.CacheWriteInputCost != 10000*writeRate/1e6 {
						t.Fatalf("cache-write accounting = %#v, want total %v", cost, want)
					}
				})
			}
		}
	}
}

func astraCatalogClient(t *testing.T, provider sigma.ProviderID, baseURL string) (sigma.Model, *sigma.Client) {
	t.Helper()
	model, ok := sigma.DefaultRegistry().Model(provider, "gpt-6-astra")
	if !ok {
		t.Fatalf("missing catalog model %s/gpt-6-astra", provider)
	}
	if model.ContextWindow != 272000 || model.MaxOutputTokens != 128000 || model.DefaultTransport != sigma.TransportSSE {
		t.Fatalf("unexpected Astra limits or default transport: %+v", model)
	}
	model.ProviderMetadata["baseURL"] = baseURL
	if provider == sigma.ProviderOpenAICodex {
		if model.API != sigma.APIOpenAICodexResponses {
			t.Fatalf("Codex API = %q", model.API)
		}
		return model, codexResponsesTestClient(t, provider, model, baseURL, codexTokenProvider("astra-oauth-token"))
	}
	if model.API != sigma.APIOpenAIResponses {
		t.Fatalf("direct API = %q", model.API)
	}
	return model, responsesTestClient(t, provider, model, baseURL)
}
