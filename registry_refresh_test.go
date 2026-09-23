// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
	"github.com/wintermi/sigma/provider/githubcopilot"
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/provider/opencode"
)

type registryRefreshTransport func(*http.Request) (*http.Response, error)

func (f registryRefreshTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRegistryRefreshDispatch(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		provider       sigma.ProviderID
		id             sigma.ModelID
		adapter        sigma.TextProvider
		path, response string
	}{
		{sigma.ProviderAzureOpenAIResponses, "gpt-6-astra", openai.NewAzureResponsesProvider(), "/openai/v1/responses", "responses"},
		{sigma.ProviderAzureOpenAIResponses, "gpt-6-luna", openai.NewAzureResponsesProvider(), "/openai/v1/responses", "responses"},
		{sigma.ProviderAzureOpenAIResponses, "gpt-6-sol", openai.NewAzureResponsesProvider(), "/openai/v1/responses", "responses"},
		{sigma.ProviderOpenAI, "gpt-6-sol", openai.NewResponsesProvider(), "/responses", "responses"},
		{sigma.ProviderOpenAI, "gpt-6-luna", openai.NewResponsesProvider(), "/responses", "responses"},
		{sigma.ProviderAnthropic, "claude-opus-5-5", anthropic.NewProvider(), "/messages", "anthropic"},
		{sigma.ProviderAnthropic, "claude-fable-5-1", anthropic.NewProvider(), "/messages", "anthropic"},
		{sigma.ProviderGoogle, "gemini-3.8-flash", google.NewProvider(), "/models/gemini-3.8-flash:streamGenerateContent", "google"},
		{sigma.ProviderXAI, "grok-4.7", openai.NewResponsesProvider(), "/responses", "responses"},
		{sigma.ProviderZAI, "glm-5.3-flash", openai.NewProvider(), "/chat/completions", "chat"},
		{sigma.ProviderZAICodingCN, "glm-5.3-flash", openai.NewProvider(), "/chat/completions", "chat"},
		{sigma.ProviderOpenCode, "gpt-6-sol", opencode.NewProvider(), "/responses", "responses"},
		{sigma.ProviderOpenCode, "claude-opus-5-5", opencode.NewProvider(), "/messages", "anthropic"},
		{sigma.ProviderOpenCode, "qwen3.8-flash", opencode.NewProvider(), "/messages", "anthropic"},
		{sigma.ProviderOpenCodeGo, "grok-4.7", opencode.NewProvider(), "/responses", "responses"},
		{sigma.ProviderOpenCodeGo, "deepseek-v4.1-flash", opencode.NewProvider(), "/chat/completions", "chat"},
		{sigma.ProviderGitHubCopilot, "gpt-6-sol", githubcopilot.NewResponsesProvider(), "/responses", "responses"},
		{sigma.ProviderGitHubCopilot, "claude-opus-5.5", githubcopilot.NewAnthropicProvider(), "/messages", "anthropic"},
		{sigma.ProviderGitHubCopilot, "gemini-3.8-flash", githubcopilot.NewProvider(), "/chat/completions", "chat"},
	} {
		t.Run(string(tt.provider)+"/"+string(tt.id), func(t *testing.T) {
			t.Parallel()
			registry := sigma.DefaultRegistry()
			model, ok := registry.Model(tt.provider, tt.id)
			if !ok {
				t.Fatal("missing model")
			}
			if err := registry.RegisterTextProvider(tt.provider, tt.adapter); err != nil {
				t.Fatal(err)
			}
			captured := make(chan map[string]any, 1)
			client := sigma.NewClient(sigma.WithRegistry(registry))
			transport := &http.Client{Transport: registryRefreshTransport(func(r *http.Request) (*http.Response, error) {
				if !strings.HasSuffix(r.URL.Path, tt.path) {
					return nil, fmt.Errorf("route %s, want suffix %s", r.URL.Path, tt.path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				captured <- body
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(registryRefreshSSE(tt.response, string(tt.id))))}, nil
			})}
			req := sigma.Request{Messages: []sigma.Message{sigma.UserContent(sigma.Text("Inspect"), sigma.ImageBase64("image/png", "aGk="))}, Tools: []sigma.Tool{{Name: "inspect", Description: "Inspect the item", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"item": map[string]any{"type": "string"}}}}}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := client.Complete(ctx, model, req, sigma.WithAPIKey("catalog-test"), sigma.WithRequestHTTPClient(transport), sigma.WithReasoningLevel(sigma.ThinkingLevelHigh), openai.WithAzureResponsesEndpoint(tt.provider, "https://catalog.test"))
			if err != nil {
				t.Fatal(err)
			}
			body := <-captured
			if tt.response != "google" && body["model"] != string(tt.id) {
				t.Fatalf("request model = %v", body["model"])
			}
			if body["tools"] == nil {
				t.Fatal("function tools omitted")
			}
			if tt.response == "responses" && body["reasoning"].(map[string]any)["effort"] != "high" {
				t.Fatalf("reasoning = %#v", body["reasoning"])
			}
			if tt.response == "anthropic" && strings.HasPrefix(string(tt.id), "claude") && body["thinking"].(map[string]any)["type"] != "adaptive" {
				t.Fatalf("thinking = %#v", body["thinking"])
			}
			if model.SupportsThinkingLevel(sigma.ThinkingLevelOff) || tt.response == "google" || tt.provider == sigma.ProviderGitHubCopilot || (tt.response == "anthropic" && tt.provider != sigma.ProviderAnthropic) {
				return
			}
			_, err = client.Complete(ctx, model, req, sigma.WithAPIKey("catalog-test"), sigma.WithRequestHTTPClient(transport), sigma.WithReasoningLevel(sigma.ThinkingLevelOff), openai.WithAzureResponsesEndpoint(tt.provider, "https://catalog.test"))
			if !errors.Is(err, sigma.ErrInvalidOptions) {
				t.Fatalf("disabled mandatory reasoning: %v", err)
			}
			if len(captured) != 0 {
				t.Fatal("invalid reasoning reached transport")
			}
		})
	}
}

func registryRefreshSSE(api, model string) string {
	switch api {
	case "responses":
		return fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_catalog\",\"model\":%q,\"status\":\"completed\",\"output\":[]}}\n\n", model)
	case "anthropic":
		return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_catalog\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	case "google":
		return "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"
	default:
		return "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	}
}

func TestRegistryRefreshPricingBoundaries(t *testing.T) {
	t.Parallel()
	for _, provider := range []sigma.ProviderID{sigma.ProviderOpenAI, sigma.ProviderOpenAICodex, sigma.ProviderOpenCode} {
		for _, tt := range []struct {
			id                         sigma.ModelID
			input, output, read, write float64
		}{{"gpt-6-sol", 2, 10, .2, 2.5}, {"gpt-6-luna", .1, .5, .01, .125}, {"gpt-5.6-sol", 4, 20, .4, 5}} {
			model, ok := sigma.DefaultRegistry().Model(provider, tt.id)
			if !ok {
				t.Fatal("missing model")
			}
			for _, tokens := range []int{272000, 272001} {
				input, output, read, write := tt.input, tt.output, tt.read, tt.write
				if tokens > 272000 {
					input *= 2
					output *= 1.5
					read *= 2
					write *= 2
				}
				usage := sigma.Usage{InputTokens: tokens - 3000, CacheReadInputTokens: 2000, CacheWriteInputTokens: 1000, OutputTokens: 1000}
				got := sigma.CostForUsage(model, usage)
				want := (float64(tokens-3000)*input + 2000*read + 1000*write + 1000*output) / 1e6
				if math.Abs(got.TotalCost-want) > 1e-10 {
					t.Fatalf("%s/%s/%d: cost %v want %v", provider, tt.id, tokens, got.TotalCost, want)
				}
			}
		}
	}
}

func TestRegistryRefreshImageGeneration(t *testing.T) {
	t.Parallel()
	for _, id := range []sigma.ModelID{"gpt-image-1.5", "gpt-image-2"} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()
			registry := sigma.DefaultRegistry()
			model, ok := registry.ImageModel(sigma.ProviderOpenAI, id)
			if !ok {
				t.Fatal("missing model")
			}
			if err := openai.RegisterImages(registry, sigma.ProviderOpenAI); err != nil {
				t.Fatal(err)
			}
			httpClient := &http.Client{Transport: registryRefreshTransport(func(r *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				if r.URL.Path != "/v1/images/generations" || body["model"] != string(id) || body["size"] != "1024x1024" || body["output_format"] != "webp" {
					return nil, fmt.Errorf("incorrect image dispatch: %s %v", r.URL.Path, body)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aW1hZ2U="}],"output_format":"webp"}`))}, nil
			})}
			result, err := sigma.NewClient(sigma.WithRegistry(registry)).GenerateImages(context.Background(), model, sigma.ImageRequest{Prompt: "A tree", Size: "1024x1024", MIMEType: "image/webp"}, sigma.WithImageAPIKey("catalog-test"), sigma.WithImageHTTPClient(httpClient))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Images) != 1 {
				t.Fatalf("images = %v", result.Images)
			}
		})
	}
}

func TestRegistryRefreshEmbeddingDimensions(t *testing.T) {
	t.Parallel()
	for _, dim := range []int{0, 128, 3072} {
		t.Run(fmt.Sprint(dim), func(t *testing.T) {
			t.Parallel()
			registry := sigma.DefaultRegistry()
			model, ok := registry.EmbeddingModel(sigma.ProviderGoogle, "gemini-embedding-2")
			if !ok {
				t.Fatal("missing model")
			}
			if err := google.RegisterEmbeddings(registry, sigma.ProviderGoogle); err != nil {
				t.Fatal(err)
			}
			calls := 0
			httpClient := &http.Client{Transport: registryRefreshTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if !strings.HasSuffix(r.URL.Path, "/models/gemini-embedding-2:batchEmbedContents") {
					return nil, fmt.Errorf("wrong embedding route: %s", r.URL.Path)
				}
				var body struct {
					Requests []struct {
						Model      string `json:"model"`
						Dimensions int    `json:"outputDimensionality"`
					} `json:"requests"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				if len(body.Requests) != 2 || body.Requests[0].Model != "models/gemini-embedding-2" || body.Requests[0].Dimensions != dim {
					return nil, fmt.Errorf("wrong batch dimensions: %+v", body)
				}
				size := dim
				if size == 0 {
					size = 3072
				}
				vec := "[" + strings.Repeat("0,", size-1) + "0]"
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"embeddings":[{"values":%s},{"values":%s}]}`, vec, vec)))}, nil
			})}
			result, err := sigma.NewClient(sigma.WithRegistry(registry)).Embed(context.Background(), model, sigma.EmbeddingRequest{Inputs: []string{"one", "two"}, Dimensions: dim}, sigma.WithEmbeddingAPIKey("catalog-test"), sigma.WithEmbeddingHTTPClient(httpClient))
			if err != nil {
				t.Fatal(err)
			}
			size := dim
			if size == 0 {
				size = 3072
			}
			if calls != 1 || len(result.Vectors) != 2 || len(result.Vectors[0].Vector) != size {
				t.Fatalf("unexpected vectors: calls=%d count=%d", calls, len(result.Vectors))
			}
		})
	}
}

func TestRegistryRefreshRetirementBoundaries(t *testing.T) {
	t.Parallel()
	registry := sigma.DefaultRegistry()
	data, err := os.ReadFile("internal/modeldata/testdata/registry-removals-2026-09-23.json")
	if err != nil {
		t.Fatal(err)
	}
	type removedModel struct {
		Provider sigma.ProviderID
		ID       sigma.ModelID
	}
	var removed struct{ TextModels, ImageModels, EmbeddingModels []removedModel }
	if err := json.Unmarshal(data, &removed); err != nil {
		t.Fatal(err)
	}
	for _, model := range removed.TextModels {
		if _, ok := registry.Model(model.Provider, model.ID); ok {
			t.Errorf("removed text model still discoverable: %s/%s", model.Provider, model.ID)
		}
	}
	for _, model := range removed.ImageModels {
		if _, ok := registry.ImageModel(model.Provider, model.ID); ok {
			t.Errorf("removed image model still discoverable: %s/%s", model.Provider, model.ID)
		}
	}
	for _, model := range removed.EmbeddingModels {
		if _, ok := registry.EmbeddingModel(model.Provider, model.ID); ok {
			t.Errorf("removed embedding model still discoverable: %s/%s", model.Provider, model.ID)
		}
	}
	for _, tt := range []struct {
		provider sigma.ProviderID
		id       sigma.ModelID
	}{
		{sigma.ProviderOpenRouter, "openai/gpt-5.2-codex"},
		{sigma.ProviderOpenCode, "gpt-5.1-codex"},
		{sigma.ProviderAmazonBedrock, "us.anthropic.claude-opus-4-1-20250805-v1:0"},
		{sigma.ProviderGoogle, "gemini-2.5-pro"},
		{sigma.ProviderOpenAI, "gpt-4.1-nano"},
	} {
		if _, ok := registry.Model(tt.provider, tt.id); !ok {
			t.Errorf("retirement crossed service or date boundary: %s/%s", tt.provider, tt.id)
		}
	}
	if _, ok := registry.EmbeddingModel(sigma.ProviderGoogle, "text-embedding-004"); ok {
		t.Fatal("retired direct embedding remains")
	}
	if _, ok := registry.EmbeddingModel(sigma.ProviderGoogleVertex, "text-embedding-004"); !ok {
		t.Fatal("direct retirement removed Vertex embedding")
	}
}
