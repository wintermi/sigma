// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/sigmatest"
)

const vertexAnthropicProbeSSE = `event: message_start
data: {"type":"message_start","message":{"id":"msg_vertex_probe","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"sigma-ok"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}

event: message_stop
data: {"type":"message_stop"}

`

func collectProbeModel(ctx context.Context, route routeSpec, modelID string, credential routeCredential, cfg config) []probeResult {
	model := route.Model(route, modelID)
	results := make([]probeResult, 0, len(route.Cases(route, model)))
	probeModelEach(ctx, route, modelID, credential, cfg, func(result probeResult) {
		results = append(results, result)
	})
	return results
}

func TestOpenCodeRouteAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		route string
		id    string
		want  sigma.API
	}{
		{route: "zen", id: "gemini-3-flash", want: sigma.APIGoogleGenerativeAI},
		{route: "zen", id: "claude-opus-4-7", want: sigma.APIAnthropicMessages},
		{route: "zen", id: "qwen3.6-plus", want: sigma.APIAnthropicMessages},
		{route: "zen", id: "gpt-5.1-codex", want: sigma.APIOpenAIResponses},
		{route: "zen", id: "grok-4.6", want: sigma.APIOpenAIResponses},
		{route: "zen", id: "kimi-k2.6", want: sigma.APIOpenAICompletions},
		{route: "go", id: "qwen3.8-flash", want: sigma.APIAnthropicMessages},
		{route: "go", id: "qwen3.7-max", want: sigma.APIOpenAICompletions},
		{route: "go", id: "grok-4.6", want: sigma.APIOpenAIResponses},
		{route: "go", id: "kimi-k2.6", want: sigma.APIOpenAICompletions},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.route+"/"+tt.id, func(t *testing.T) {
			t.Parallel()

			if got := openCodeRouteAPI(tt.route, tt.id); got != tt.want {
				t.Fatalf("openCodeRouteAPI = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKnownUnavailable(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"claude-opus-4-6", "minimax-m2.5-free", "qwen3.6-plus-free", "gpt-5.3-codex-spark"} {
		if !knownUnavailable("zen", id) {
			t.Fatalf("%s was not classified as known unavailable", id)
		}
	}
	if knownUnavailable("go", "qwen3.7-max") {
		t.Fatal("go qwen3.7-max should not be skipped")
	}
}

func TestFireworksRoutesBuildExpectedModels(t *testing.T) {
	t.Parallel()

	openAI := routes["fireworks-openai"].Model(routes["fireworks-openai"], "accounts/fireworks/routers/kimi-k2p6-turbo")
	if openAI.Provider != sigma.ProviderFireworks || openAI.API != sigma.APIOpenAICompletions {
		t.Fatalf("fireworks-openai model provider/API = %q/%q", openAI.Provider, openAI.API)
	}
	if openAI.OpenAICompletionsCompat == nil ||
		openAI.OpenAICompletionsCompat.ReasoningFormat != sigma.OpenAICompletionsReasoningFireworks ||
		openAI.OpenAICompletionsCompat.MaxTokensField != sigma.OpenAICompletionsMaxTokens {
		t.Fatalf("fireworks-openai compat = %#v, want Fireworks OpenAI completions compat", openAI.OpenAICompletionsCompat)
	}
	assertMetadataString(t, openAI.ProviderMetadata, "baseURL", "https://api.fireworks.ai/inference/v1")
	assertMetadataStrings(t, openAI.ProviderMetadata, "apiKeyEnvVars", []string{"FIREWORKS_API_KEY"})

	k3 := routes["fireworks-openai"].Model(routes["fireworks-openai"], "accounts/fireworks/models/kimi-k3")
	if !k3.SupportsThinkingLevel(sigma.ThinkingLevel("max")) {
		t.Fatalf("fireworks-openai K3 max reasoning metadata = %+v, want max support", k3)
	}
	if k3.OpenAICompletionsCompat == nil ||
		k3.OpenAICompletionsCompat.ReasoningFormat != sigma.OpenAICompletionsReasoningEffort ||
		k3.OpenAICompletionsCompat.SupportsSessionAffinity != sigma.OpenAICompatSupported ||
		k3.OpenAICompletionsCompat.SupportsLongCacheRetention != sigma.OpenAICompatUnsupported ||
		k3.OpenAICompletionsCompat.RequiresReasoningContentOnAssistantMessages != sigma.OpenAICompatSupported {
		t.Fatalf("fireworks-openai K3 compat = %#v, want generated K3 compat", k3.OpenAICompletionsCompat)
	}
	assertMetadataString(t, k3.ProviderMetadata, "deferredToolsMode", "kimi")

	anthropic := routes["fireworks-anthropic"].Model(routes["fireworks-anthropic"], "accounts/fireworks/models/kimi-k2p6")
	if anthropic.Provider != sigma.ProviderFireworksAnthropic || anthropic.API != sigma.APIAnthropicMessages {
		t.Fatalf("fireworks-anthropic model provider/API = %q/%q", anthropic.Provider, anthropic.API)
	}
	if anthropic.AnthropicMessagesCompat == nil ||
		anthropic.AnthropicMessagesCompat.SupportsSessionAffinity != sigma.AnthropicCompatSupported ||
		anthropic.AnthropicMessagesCompat.SupportsEagerToolInputStreaming != sigma.AnthropicCompatUnsupported ||
		anthropic.AnthropicMessagesCompat.SupportsLongCacheRetention != sigma.AnthropicCompatUnsupported ||
		anthropic.AnthropicMessagesCompat.SupportsCacheControlOnTools != sigma.AnthropicCompatUnsupported {
		t.Fatalf("fireworks-anthropic compat = %#v, want Fireworks Anthropic compat", anthropic.AnthropicMessagesCompat)
	}
	assertMetadataString(t, anthropic.ProviderMetadata, "baseURL", "https://api.fireworks.ai/inference/v1")
	assertMetadataStrings(t, anthropic.ProviderMetadata, "apiKeyEnvVars", []string{"FIREWORKS_API_KEY"})
}

func TestFireworksModelCapabilitiesComeFromProviderMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/v1/accounts/fireworks/models/qwen3p8-max"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer key"; got != want {
			t.Errorf("authorization = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"READY","contextLength":262144,"supportsImageInput":false,"supportsTools":true}`)
	}))
	t.Cleanup(server.Close)

	route := routes["fireworks-openai"]
	route.BaseURL = server.URL + "/inference/v1"
	model := resolveProbeModel(context.Background(), route, "accounts/fireworks/models/qwen3p8-max", routeCredential{apiKey: "key"})
	if model.SupportsImages() {
		t.Fatal("resolved Fireworks model supports images, want provider metadata to disable image input")
	}
	if !model.SupportsTools {
		t.Fatal("resolved Fireworks model does not support tools, want provider metadata to enable tools")
	}
	if got, want := model.ContextWindow, 262144; got != want {
		t.Fatalf("context window = %d, want %d", got, want)
	}
	if hasString(probeCaseNames(route.Cases(route, model)), "image_input") {
		t.Fatal("image_input case present for model whose provider metadata disables images")
	}
}

func TestFireworksModelMetadataFailureUsesConservativeCapabilities(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	route := routes["fireworks-openai"]
	route.BaseURL = server.URL + "/inference/v1"
	model := resolveProbeModel(context.Background(), route, "accounts/fireworks/models/unknown", routeCredential{apiKey: "key"})
	if model.SupportsImages() || model.SupportsTools || model.SupportsReasoning() {
		t.Fatalf("fallback model has optimistic optional capabilities: %+v", model)
	}
	if !capabilityMetadataUnavailable(model) {
		t.Fatalf("fallback model metadata = %#v, want explicit unavailable marker", model.ProviderMetadata)
	}
}

func TestFireworksModelMetadataFailureEmitsExplicitSkip(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	route := routes["fireworks-openai"]
	route.BaseURL = server.URL + "/inference/v1"
	route.Cases = func(routeSpec, sigma.Model) []probeCase { return nil }
	var results []probeResult
	probeModelEach(context.Background(), route, "accounts/fireworks/models/unknown", routeCredential{apiKey: "key"}, config{}, func(result probeResult) {
		results = append(results, result)
	})
	if got, want := len(results), 1; got != want {
		t.Fatalf("results = %d, want %d: %#v", got, want, results)
	}
	if result := results[0]; result.Case != "optional_capabilities" || result.Attempt != "model_metadata" || result.Outcome != "skipped" || result.Hint != "capability_metadata_unavailable" {
		t.Fatalf("metadata result = %+v, want explicit optional-capability skip", result)
	}
}

func TestFireworksNonReadyModelIsSkipped(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"UPLOADING","supportsImageInput":true,"supportsTools":true}`)
	}))
	t.Cleanup(server.Close)

	route := routes["fireworks-openai"]
	route.BaseURL = server.URL + "/inference/v1"
	var results []probeResult
	probeModelEach(context.Background(), route, "accounts/fireworks/models/uploading", routeCredential{apiKey: "key"}, config{}, func(result probeResult) {
		results = append(results, result)
	})
	if got, want := len(results), 1; got != want {
		t.Fatalf("results = %d, want %d: %#v", got, want, results)
	}
	if result := results[0]; result.Case != "all" || result.Attempt != "skip_model_state" || result.Outcome != "skipped" {
		t.Fatalf("result = %+v, want provider-state skip", result)
	}
}

func TestOpenAIRoutesBuildExpectedModels(t *testing.T) {
	t.Parallel()

	openAIRoute := routes["openai"]
	openAIModel := openAIRoute.Model(openAIRoute, "gpt-5.1")
	if openAIModel.Provider != sigma.ProviderOpenAI || openAIModel.API != sigma.APIOpenAIResponses {
		t.Fatalf("openai model provider/API = %q/%q", openAIModel.Provider, openAIModel.API)
	}
	assertMetadataString(t, openAIModel.ProviderMetadata, "baseURL", "https://api.openai.com/v1")
	assertMetadataString(t, openAIModel.ProviderMetadata, "probeSurface", "openai-responses")
	assertMetadataStrings(t, openAIModel.ProviderMetadata, "apiKeyEnvVars", []string{"OPENAI_API_KEY"})

	codexRoute := routes["openai-codex"]
	codexModel := codexRoute.Model(codexRoute, "gpt-5.1-codex")
	if codexModel.Provider != sigma.ProviderOpenAI || codexModel.API != sigma.APIOpenAICodexResponses {
		t.Fatalf("openai-codex model provider/API = %q/%q", codexModel.Provider, codexModel.API)
	}
	if codexModel.OpenAICodexResponses == nil {
		t.Fatal("openai-codex model missing OpenAICodexResponses config")
	}
	assertMetadataString(t, codexModel.ProviderMetadata, "baseURL", "https://chatgpt.com/backend-api/codex")
	assertMetadataString(t, codexModel.ProviderMetadata, "probeSurface", "openai-codex-responses")
	assertMetadataStrings(t, codexModel.ProviderMetadata, "apiKeyEnvVars", []string{"OPENAI_CODEX_ACCESS_TOKEN", "OPENAI_CODEX_REFRESH_TOKEN"})
}

func TestXAIRouteBuildsExpectedModel(t *testing.T) {
	t.Parallel()

	route := routes["xai"]
	model := route.Model(route, "grok-4.3")
	if model.Provider != sigma.ProviderXAI || model.API != sigma.APIOpenAICompletions {
		t.Fatalf("xai model provider/API = %q/%q", model.Provider, model.API)
	}
	if !model.SupportsTools || !model.SupportsImages() || !model.SupportsReasoning() {
		t.Fatalf("xai probe model did not enable optimistic probe capabilities: %+v", model)
	}
	assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://api.x.ai/v1")
	assertMetadataString(t, model.ProviderMetadata, "modelFamily", "grok")
	assertMetadataString(t, model.ProviderMetadata, "probeRoute", "xai")
	assertMetadataString(t, model.ProviderMetadata, "probeSurface", "openai-completions")
	assertMetadataStrings(t, model.ProviderMetadata, "apiKeyEnvVars", []string{"XAI_API_KEY"})
}

func TestMoonshotRoutesBuildExpectedModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider sigma.ProviderID
		baseURL  string
	}{
		{name: "moonshot", provider: sigma.ProviderMoonshotAI, baseURL: "https://api.moonshot.ai/v1"},
		{name: "moonshot-cn", provider: sigma.ProviderMoonshotAICN, baseURL: "https://api.moonshot.cn/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			route := routes[tt.name]
			if route.RegisterProvider == nil {
				t.Fatalf("route %q missing provider registration", tt.name)
			}
			if got, want := route.Provider, tt.provider; got != want {
				t.Fatalf("provider = %q, want %q", got, want)
			}
			if got, want := route.BaseURL, tt.baseURL; got != want {
				t.Fatalf("base URL = %q, want %q", got, want)
			}
			if got, want := route.APIKeyEnv, "MOONSHOT_API_KEY"; got != want {
				t.Fatalf("api key env = %q, want %q", got, want)
			}

			model := route.Model(route, "kimi-k2.7-code")
			if model.Provider != tt.provider || model.API != sigma.APIOpenAICompletions {
				t.Fatalf("model provider/API = %q/%q", model.Provider, model.API)
			}
			if model.OpenAICompletionsCompat == nil ||
				model.OpenAICompletionsCompat.ReasoningFormat != sigma.OpenAICompletionsReasoningDeepSeek ||
				model.OpenAICompletionsCompat.SupportsReasoningEffort != sigma.OpenAICompatUnsupported {
				t.Fatalf("Moonshot probe compat = %#v, want DeepSeek format without reasoning effort", model.OpenAICompletionsCompat)
			}
		})
	}
}

func TestNVIDIARouteBuildsExpectedModels(t *testing.T) {
	t.Parallel()

	route := routes["nvidia"]
	if route.RegisterProvider == nil {
		t.Fatal("nvidia route missing provider registration")
	}
	if got, want := route.Provider, sigma.ProviderNVIDIA; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if got, want := route.BaseURL, "https://integrate.api.nvidia.com/v1"; got != want {
		t.Fatalf("base URL = %q, want %q", got, want)
	}
	if got, want := route.APIKeyEnv, "NVIDIA_API_KEY"; got != want {
		t.Fatalf("api key env = %q, want %q", got, want)
	}

	generated := route.Model(route, defaultNVIDIAProbeModel)
	if generated.Provider != sigma.ProviderNVIDIA || generated.API != sigma.APIOpenAICompletions {
		t.Fatalf("generated NVIDIA model provider/API = %q/%q", generated.Provider, generated.API)
	}
	if generated.OpenAICompletionsCompat == nil ||
		generated.OpenAICompletionsCompat.SupportsReasoningEffort != sigma.OpenAICompatUnsupported ||
		generated.OpenAICompletionsCompat.SupportsStreamingUsage != sigma.OpenAICompatSupported ||
		generated.OpenAICompletionsCompat.SupportsStrictTools != sigma.OpenAICompatUnsupported ||
		generated.OpenAICompletionsCompat.MaxTokensField != sigma.OpenAICompletionsMaxTokens {
		t.Fatalf("generated NVIDIA compat = %#v, want NVIDIA OpenAI-compatible defaults", generated.OpenAICompletionsCompat)
	}
	assertMetadataString(t, generated.ProviderMetadata, "baseURL", "https://integrate.api.nvidia.com/v1")
	assertMetadataStrings(t, generated.ProviderMetadata, "apiKeyEnvVars", []string{"NVIDIA_API_KEY"})

	discovered := route.Model(route, "custom/nim")
	if discovered.Provider != sigma.ProviderNVIDIA || discovered.API != sigma.APIOpenAICompletions {
		t.Fatalf("discovered NVIDIA model provider/API = %q/%q", discovered.Provider, discovered.API)
	}
	if discovered.OpenAICompletionsCompat == nil ||
		discovered.OpenAICompletionsCompat.SupportsStore != sigma.OpenAICompatUnsupported ||
		discovered.OpenAICompletionsCompat.SupportsDeveloperRole != sigma.OpenAICompatUnsupported ||
		discovered.OpenAICompletionsCompat.SupportsReasoningEffort != sigma.OpenAICompatUnsupported ||
		discovered.OpenAICompletionsCompat.SupportsStreamingUsage != sigma.OpenAICompatSupported ||
		discovered.OpenAICompletionsCompat.SupportsStrictTools != sigma.OpenAICompatUnsupported ||
		discovered.OpenAICompletionsCompat.MaxTokensField != sigma.OpenAICompletionsMaxTokens {
		t.Fatalf("discovered NVIDIA compat = %#v, want NVIDIA OpenAI-compatible defaults", discovered.OpenAICompletionsCompat)
	}
	assertMetadataString(t, discovered.ProviderMetadata, "probeRoute", "nvidia")
	assertMetadataString(t, discovered.ProviderMetadata, "probeSurface", "openai-completions")
	assertMetadataStrings(t, discovered.ProviderMetadata, "apiKeyEnvVars", []string{"NVIDIA_API_KEY"})
}

func TestGoogleVertexRouteUsesGeneratedModels(t *testing.T) {
	t.Parallel()

	route := routes["google-vertex"]
	if route.RegisterProvider == nil {
		t.Fatal("google-vertex route missing provider registration")
	}
	if got, want := route.Provider, sigma.ProviderGoogleVertex; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}

	registry := sigma.NewRegistry()
	if err := route.RegisterProvider(registry, route); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	providers := registry.ListProviders()
	if len(providers) != 1 || providers[0].ID != sigma.ProviderGoogleVertex || providers[0].TextAPI != sigma.APIGoogleVertex {
		t.Fatalf("registered providers = %#v, want google-vertex text provider", providers)
	}

	got := route.Model(route, "gemini-3.1-pro-preview")
	want, ok := sigma.GetModel(sigma.ProviderGoogleVertex, "gemini-3.1-pro-preview")
	if !ok {
		t.Fatal("generated google-vertex model was not registered")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("probe model differs from generated model:\n got: %#v\nwant: %#v", got, want)
	}
	if !got.SupportsThinkingLevel(sigma.ThinkingLevelMedium) || !got.SupportsThinkingLevel(sigma.ThinkingLevelLow) || !got.SupportsThinkingLevel(sigma.ThinkingLevelHigh) || got.SupportsThinkingLevel(sigma.ThinkingLevelMinimal) || got.SupportsThinkingLevel(sigma.ThinkingLevelOff) {
		t.Fatalf("generated thinking restrictions were not preserved: %+v", got)
	}
}

func TestGoogleVertexAnthropicRouteUsesGeneratedModel(t *testing.T) {
	t.Parallel()

	route := routes["google-vertex-anthropic"]
	if route.RegisterProvider == nil {
		t.Fatal("google-vertex-anthropic route missing provider registration")
	}
	if got, want := route.Provider, sigma.ProviderGoogleVertexAnthropic; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}

	registry := sigma.NewRegistry()
	if err := route.RegisterProvider(registry, route); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	providers := registry.ListProviders()
	if len(providers) != 1 || providers[0].ID != sigma.ProviderGoogleVertexAnthropic || providers[0].TextAPI != sigma.APIAnthropicMessages {
		t.Fatalf("registered providers = %#v, want google-vertex-anthropic text provider", providers)
	}

	got := route.Model(route, defaultGoogleVertexAnthropicModel)
	want, ok := sigma.GetModel(sigma.ProviderGoogleVertexAnthropic, defaultGoogleVertexAnthropicModel)
	if !ok {
		t.Fatal("generated google-vertex-anthropic model was not registered")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("probe model differs from generated model:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestModelsForGoogleVertexUseBuiltInCatalog(t *testing.T) {
	t.Parallel()

	route := routes["google-vertex"]
	route.BaseURL = "http://127.0.0.1:1/v1"
	models, err := modelsForRoute(context.Background(), route, routeCredential{}, nil)
	if err != nil {
		t.Fatalf("modelsForRoute returned error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("modelsForRoute returned no built-in Vertex models")
	}
	if !sort.StringsAreSorted(models) {
		t.Fatalf("models are not sorted: %v", models)
	}
	for _, modelID := range models {
		model, ok := sigma.GetModel(sigma.ProviderGoogleVertex, sigma.ModelID(modelID))
		if !ok || model.API != sigma.APIGoogleVertex {
			t.Fatalf("model %q does not resolve to built-in google-vertex text metadata", modelID)
		}
	}
	for _, modelID := range []string{
		"gemini-2.5-flash",
		"gemini-3-flash-preview",
		"gemini-3.1-pro-preview",
		"gemini-3.5-flash-lite",
		"gemini-3.6-flash",
	} {
		if !hasString(models, modelID) {
			t.Fatalf("built-in models do not include %q: %v", modelID, models)
		}
	}
	for _, modelID := range []string{
		"gemini-1.5-flash",
		"gemini-1.5-flash-8b",
		"gemini-1.5-pro",
		"gemini-2.0-flash",
		"gemini-2.0-flash-lite",
		"gemini-2.5-flash-lite-preview-09-2025",
		"gemini-3-pro-preview",
	} {
		if hasString(models, modelID) {
			t.Fatalf("built-in models unexpectedly include retired model %q: %v", modelID, models)
		}
	}
}

func TestModelsForGoogleVertexValidateExplicitModels(t *testing.T) {
	t.Parallel()

	models, err := modelsForRoute(context.Background(), routes["google-vertex"], routeCredential{}, map[string]bool{
		"gemini-3.5-flash-lite": true,
		"gemini-3.6-flash":      true,
	})
	if err != nil {
		t.Fatalf("modelsForRoute returned error: %v", err)
	}
	if !reflect.DeepEqual(models, []string{"gemini-3.5-flash-lite", "gemini-3.6-flash"}) {
		t.Fatalf("models = %v, want sorted selected Vertex models", models)
	}

	for _, modelID := range []string{
		"not-a-vertex-model",
		"gemini-1.5-flash",
		"gemini-1.5-flash-8b",
		"gemini-1.5-pro",
		"gemini-2.0-flash",
		"gemini-2.0-flash-lite",
		"gemini-2.5-flash-lite-preview-09-2025",
		"gemini-3-pro-preview",
	} {
		_, err = modelsForRoute(context.Background(), routes["google-vertex"], routeCredential{}, map[string]bool{modelID: true})
		if err == nil || !strings.Contains(err.Error(), "not a built-in google-vertex text model") {
			t.Fatalf("invalid model %q error = %v, want clear local rejection", modelID, err)
		}
	}
}

func TestModelsForGoogleVertexAnthropicUseBuiltInCatalog(t *testing.T) {
	t.Parallel()

	route := routes["google-vertex-anthropic"]
	route.BaseURL = "http://127.0.0.1:1/v1"
	models, err := modelsForRoute(context.Background(), route, routeCredential{}, nil)
	if err != nil {
		t.Fatalf("modelsForRoute returned error: %v", err)
	}
	if !reflect.DeepEqual(models, []string{defaultGoogleVertexAnthropicModel}) {
		t.Fatalf("models = %v, want default Vertex Anthropic model", models)
	}

	models, err = modelsForRoute(context.Background(), route, routeCredential{}, map[string]bool{
		"claude-opus-4-8":   true,
		"claude-sonnet-4-6": true,
	})
	if err != nil {
		t.Fatalf("selected models returned error: %v", err)
	}
	if !reflect.DeepEqual(models, []string{"claude-opus-4-8", "claude-sonnet-4-6"}) {
		t.Fatalf("models = %v, want sorted selected Vertex Anthropic models", models)
	}

	for _, modelID := range []string{"not-a-vertex-model", "gemini-3.6-flash"} {
		_, err = modelsForRoute(context.Background(), route, routeCredential{}, map[string]bool{modelID: true})
		if err == nil || !strings.Contains(err.Error(), "not a built-in google-vertex-anthropic text model") {
			t.Fatalf("invalid model %q error = %v, want clear local rejection", modelID, err)
		}
	}
}

func TestModelsForRouteUsesSelectedModelsWithoutDiscovery(t *testing.T) {
	t.Parallel()

	models, err := modelsForRoute(context.Background(), routes["fireworks-anthropic"], routeCredential{apiKey: "key"}, map[string]bool{
		"z": true,
		"a": true,
	})
	if err != nil {
		t.Fatalf("modelsForRoute returned error: %v", err)
	}
	if !reflect.DeepEqual(models, []string{"a", "z"}) {
		t.Fatalf("models = %v, want sorted selected models", models)
	}
}

func TestModelsForRouteDefaultsOpenAICodexWithoutDiscovery(t *testing.T) {
	t.Parallel()

	if got, want := defaultOpenAICodexProbeModel, "gpt-5.5"; got != want {
		t.Fatalf("defaultOpenAICodexProbeModel = %q, want %q", got, want)
	}
	models, err := modelsForRoute(context.Background(), routes["openai-codex"], routeCredential{apiKey: "token"}, nil)
	if err != nil {
		t.Fatalf("modelsForRoute returned error: %v", err)
	}
	if !reflect.DeepEqual(models, []string{defaultOpenAICodexProbeModel}) {
		t.Fatalf("models = %v, want default Codex model", models)
	}
}

func TestModelsForRouteDefaultsNVIDIAWithoutDiscovery(t *testing.T) {
	t.Parallel()

	if got, want := defaultNVIDIAProbeModel, "nvidia/nemotron-3-super-120b-a12b"; got != want {
		t.Fatalf("defaultNVIDIAProbeModel = %q, want %q", got, want)
	}
	models, err := modelsForRoute(context.Background(), routes["nvidia"], routeCredential{apiKey: "token"}, nil)
	if err != nil {
		t.Fatalf("modelsForRoute returned error: %v", err)
	}
	if !reflect.DeepEqual(models, []string{defaultNVIDIAProbeModel}) {
		t.Fatalf("models = %v, want default NVIDIA model", models)
	}
}

func TestOpenAICompatibleProbeCasesUseRouteProviderOptions(t *testing.T) {
	t.Parallel()

	testCase := findProbeCase(t, openAICompatibleProbeCases(routes["fireworks-openai"], sigma.Model{}), "json_object")
	options := applyProbeOptions(testCase.Options)
	if _, ok := options.ProviderOptions[sigma.ProviderFireworks]["extra_body"]; !ok {
		t.Fatalf("fireworks provider options = %#v, want extra_body", options.ProviderOptions[sigma.ProviderFireworks])
	}
	if _, ok := options.ProviderOptions[sigma.ProviderOpenCode]; ok {
		t.Fatalf("unexpected OpenCode provider options: %#v", options.ProviderOptions[sigma.ProviderOpenCode])
	}
}

func TestStructuredOutputProbeCasesSelectsJSONOnly(t *testing.T) {
	t.Parallel()

	cases := structuredOutputProbeCases(openAICompatibleProbeCases(routes["xai"], sigma.Model{}))
	if len(cases) != 2 {
		t.Fatalf("cases length = %d, want 2", len(cases))
	}
	if got, want := cases[0].Name, "json_object"; got != want {
		t.Fatalf("first case = %q, want %q", got, want)
	}
	if got, want := cases[1].Name, "json_schema"; got != want {
		t.Fatalf("second case = %q, want %q", got, want)
	}
}

func TestFireworksOpenAIProbeCasesSkipScalarThinkingControls(t *testing.T) {
	t.Parallel()

	model := sigma.Model{SupportsThinking: true}
	cases := openAICompatibleProbeCases(routes["fireworks-openai"], model)
	if hasRepairVariant(cases, "thinking_string_none") {
		t.Fatal("fireworks-openai should not probe scalar thinking string controls")
	}
	if hasRepairVariant(cases, "thinking_bool_false") {
		t.Fatal("fireworks-openai should not probe scalar thinking bool controls")
	}
	if hasRepairVariant(cases, "enable_thinking_false") {
		t.Fatal("fireworks-openai should not probe unsupported enable_thinking controls")
	}
	if !hasRepairVariant(cases, "thinking_object_disabled") {
		t.Fatal("fireworks-openai should still probe object disabled thinking")
	}
	if !hasRepairVariant(openAICompatibleProbeCases(routes["xai"], model), "thinking_string_none") {
		t.Fatal("non-Fireworks OpenAI-compatible routes should keep scalar thinking probes")
	}
}

func TestOpenCodeGoKimiProbeCasesMatchReasoningFormat(t *testing.T) {
	t.Parallel()

	route := routes["go"]
	kimiCode := discoveredOpenCodeModel(route, "kimi-k2.7-code")
	codeCases := openAICompatibleProbeCases(route, kimiCode)
	for _, name := range []string{"thinking_string_none", "thinking_object_disabled", "thinking_bool_false", "enable_thinking_false"} {
		if hasRepairVariant(codeCases, name) {
			t.Fatalf("OpenCode Go Kimi K2.7 Code should not probe raw thinking control %q", name)
		}
	}
	if !hasRepairVariant(codeCases, "reasoning_effort_high") {
		t.Fatal("OpenCode Go Kimi K2.7 Code should probe reasoning_effort")
	}
	if !hasRepairVariant(codeCases, "logprobs") {
		t.Fatal("OpenCode Go Kimi K2.7 Code should retain the logprobs probe")
	}
	if hasRepairVariant(codeCases, "tool_required_file_read") {
		t.Fatal("OpenCode Go Kimi K2.7 Code should not probe unsupported required tool choice")
	}
	if hasRepairVariant(codeCases, "strict_tool_required_write") {
		t.Fatal("OpenCode Go Kimi K2.7 Code should not probe unsupported strict required tool choice")
	}

	kimi26 := discoveredOpenCodeModel(route, "kimi-k2.6")
	kimi26Cases := openAICompatibleProbeCases(route, kimi26)
	for _, name := range []string{"thinking_string_none", "thinking_object_disabled", "thinking_bool_false", "enable_thinking_false"} {
		if hasRepairVariant(kimi26Cases, name) {
			t.Fatalf("OpenCode Go Kimi K2.6 should not probe raw thinking control %q", name)
		}
	}
	if !hasRepairVariant(kimi26Cases, "reasoning_effort_high") {
		t.Fatal("OpenCode Go Kimi K2.6 should probe reasoning_effort")
	}
	if !hasRepairVariant(kimi26Cases, "logprobs") {
		t.Fatal("OpenCode Go Kimi K2.6 should retain the logprobs probe")
	}

	kimiK3 := discoveredOpenCodeModel(route, "kimi-k3")
	k3Cases := openAICompatibleProbeCases(route, kimiK3)
	for _, name := range []string{"thinking_string_none", "thinking_object_disabled", "thinking_bool_false", "enable_thinking_false"} {
		if hasRepairVariant(k3Cases, name) {
			t.Fatalf("OpenCode Go Kimi K3 should not probe raw thinking control %q", name)
		}
	}
	if !hasRepairVariant(k3Cases, "reasoning_effort_high") {
		t.Fatal("OpenCode Go Kimi K3 should probe reasoning_effort")
	}
	if hasRepairVariant(k3Cases, "logprobs") {
		t.Fatal("OpenCode Go Kimi K3 should skip unsupported logprobs")
	}
}

func TestMoonshotK27ProbeCasesSkipDisabledThinkingControls(t *testing.T) {
	t.Parallel()

	route := routes["moonshot"]
	kimiCode := route.Model(route, "kimi-k2.7-code")
	codeCases := openAICompatibleProbeCases(route, kimiCode)
	for _, name := range []string{"thinking_string_none", "thinking_object_disabled", "thinking_bool_false", "enable_thinking_false"} {
		if hasRepairVariant(codeCases, name) {
			t.Fatalf("Moonshot Kimi K2.7 Code should not probe disabled thinking control %q", name)
		}
	}
	if !hasRepairVariant(codeCases, "reasoning_effort_high") {
		t.Fatal("Moonshot Kimi K2.7 Code should keep non-disabled reasoning probes")
	}

	kimi26 := route.Model(route, "kimi-k2.6")
	kimi26Cases := openAICompatibleProbeCases(route, kimi26)
	if !hasRepairVariant(kimi26Cases, "thinking_object_disabled") {
		t.Fatal("Moonshot Kimi K2.6 should still probe disabled thinking controls")
	}
}

func TestXAIProbeCasesUseRouteProviderOptions(t *testing.T) {
	t.Parallel()

	testCase := findProbeCase(t, openAICompatibleProbeCases(routes["xai"], sigma.Model{}), "json_object")
	options := applyProbeOptions(testCase.Options)
	if _, ok := options.ProviderOptions[sigma.ProviderXAI]["extra_body"]; !ok {
		t.Fatalf("xai provider options = %#v, want extra_body", options.ProviderOptions[sigma.ProviderXAI])
	}
	if _, ok := options.ProviderOptions[sigma.ProviderOpenCode]; ok {
		t.Fatalf("unexpected OpenCode provider options: %#v", options.ProviderOptions[sigma.ProviderOpenCode])
	}
	if _, ok := options.ProviderOptions[sigma.ProviderFireworks]; ok {
		t.Fatalf("unexpected Fireworks provider options: %#v", options.ProviderOptions[sigma.ProviderFireworks])
	}
}

func TestOpenAIResponsesProbeCasesUseTypedResponseFormat(t *testing.T) {
	t.Parallel()

	testCase := findProbeCase(t, openAIResponsesProbeCases(routes["openai"], sigma.Model{}), "json_schema")
	options := applyProbeOptions(testCase.Options)
	if options.OpenAIOptions == nil || options.OpenAIOptions.ResponseFormat == nil {
		t.Fatalf("OpenAIOptions.ResponseFormat = %#v, want typed response format", options.OpenAIOptions)
	}
	if _, ok := options.ProviderOptions[sigma.ProviderOpenAI]["extra_body"]; ok {
		t.Fatalf("unexpected extra_body for OpenAI Responses: %#v", options.ProviderOptions[sigma.ProviderOpenAI])
	}
}

func TestOpenAICodexAuthOptionsUseOAuthTokenProvider(t *testing.T) {
	t.Parallel()

	route := routes["openai-codex"]
	options := applyProbeOptions(authOptions(route, routeCredential{
		codex: openAIProbeTestCredentials(),
	}))
	providerOptions := options.ProviderOptions[route.Provider]
	if providerOptions == nil {
		t.Fatal("missing provider options")
	}
	provider, ok := providerOptions["oauthTokenProvider"].(sigma.OAuthTokenProvider)
	if !ok {
		t.Fatalf("oauthTokenProvider type = %T, want sigma.OAuthTokenProvider", providerOptions["oauthTokenProvider"])
	}
	credential, err := provider.Token(context.Background(), route.Model(route, "gpt-5.1-codex"), sigma.Options{})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if credential.Value == "" {
		t.Fatal("credential value was empty")
	}
	if got, want := credential.Metadata["accountID"], "acct_probe"; got != want {
		t.Fatalf("accountID metadata = %v, want %q", got, want)
	}
}

func TestGoogleVertexCredentialPrecedence(t *testing.T) {
	clearGoogleVertexEnvironment(t)
	t.Setenv("GOOGLE_CLOUD_PROJECT", "cloud-project")
	t.Setenv("GCLOUD_PROJECT", "gcloud-project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "cloud-location")
	t.Setenv("GOOGLE_CLOUD_REGION", "cloud-region")
	t.Setenv("GOOGLE_CLOUD_ACCESS_TOKEN", "oauth-token")
	t.Setenv("GOOGLE_CLOUD_API_KEY", "cloud-key")
	t.Setenv("GOOGLE_API_KEY", "google-key")

	credential, err := credentialForRoute(context.Background(), routes["google-vertex"], config{})
	if err != nil {
		t.Fatalf("credentialForRoute returned error: %v", err)
	}
	if credential.projectID != "cloud-project" || credential.location != "cloud-location" {
		t.Fatalf("routing = %q/%q, want cloud-project/cloud-location", credential.projectID, credential.location)
	}
	if credential.accessToken != "oauth-token" || credential.apiKey != "" {
		t.Fatalf("auth selection = token %t, key %t; want token only", credential.accessToken != "", credential.apiKey != "")
	}
	vertexAnthropicCredential, err := credentialForRoute(context.Background(), routes["google-vertex-anthropic"], config{})
	if err != nil {
		t.Fatalf("Vertex Anthropic credentialForRoute returned error: %v", err)
	}
	if !reflect.DeepEqual(vertexAnthropicCredential, credential) {
		t.Fatalf("Vertex Anthropic credential = %#v, want shared Vertex credential %#v", vertexAnthropicCredential, credential)
	}
}

func TestGoogleVertexCredentialFallbacks(t *testing.T) {
	clearGoogleVertexEnvironment(t)
	t.Setenv("GCLOUD_PROJECT", "fallback-project")
	t.Setenv("GOOGLE_CLOUD_REGION", "fallback-region")
	t.Setenv("GOOGLE_CLOUD_API_KEY", "cloud-key")
	t.Setenv("GOOGLE_API_KEY", "google-key")

	credential, err := googleVertexCredential()
	if err != nil {
		t.Fatalf("googleVertexCredential returned error: %v", err)
	}
	if credential.projectID != "fallback-project" || credential.location != "fallback-region" || credential.apiKey != "cloud-key" {
		t.Fatalf("credential = project %q location %q key %q", credential.projectID, credential.location, credential.apiKey)
	}

	t.Setenv("GOOGLE_CLOUD_API_KEY", "")
	credential, err = googleVertexCredential()
	if err != nil {
		t.Fatalf("GOOGLE_API_KEY fallback returned error: %v", err)
	}
	if credential.apiKey != "google-key" {
		t.Fatalf("api key = %q, want GOOGLE_API_KEY fallback", credential.apiKey)
	}
}

func TestGoogleVertexCredentialRequiresRoutingAndAuth(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "project", env: map[string]string{"GOOGLE_CLOUD_LOCATION": "location", "GOOGLE_CLOUD_API_KEY": "key"}, wantErr: "GOOGLE_CLOUD_PROJECT or GCLOUD_PROJECT"},
		{name: "location", env: map[string]string{"GOOGLE_CLOUD_PROJECT": "project", "GOOGLE_CLOUD_API_KEY": "key"}, wantErr: "GOOGLE_CLOUD_LOCATION or GOOGLE_CLOUD_REGION"},
		{name: "authentication", env: map[string]string{"GOOGLE_CLOUD_PROJECT": "project", "GOOGLE_CLOUD_LOCATION": "location"}, wantErr: "GOOGLE_CLOUD_ACCESS_TOKEN, GOOGLE_CLOUD_API_KEY, or GOOGLE_API_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearGoogleVertexEnvironment(t)
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			_, err := googleVertexCredential()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestGoogleVertexAuthOptions(t *testing.T) {
	t.Parallel()

	route := routes["google-vertex"]
	model := route.Model(route, "gemini-2.5-flash")
	oauth := applyProbeOptions(authOptions(route, routeCredential{
		accessToken: "oauth-token",
		projectID:   "test-project",
		location:    "us-central1",
	}))
	providerOptions := oauth.ProviderOptions[route.Provider]
	if providerOptions["projectID"] != "test-project" || providerOptions["location"] != "us-central1" {
		t.Fatalf("provider options = %#v, want request-scoped Vertex routing", providerOptions)
	}
	resolver, ok := oauth.ProviderAuthResolvers[route.Provider]
	if !ok {
		t.Fatal("missing google-vertex auth resolver")
	}
	credential, err := resolver.Resolve(context.Background(), model, oauth)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if credential.Type != sigma.CredentialTypeOAuthToken || credential.Value != "oauth-token" || credential.Source != "env:GOOGLE_CLOUD_ACCESS_TOKEN" {
		t.Fatalf("credential = %s, want typed environment OAuth token", credential)
	}

	apiKey := applyProbeOptions(authOptions(route, routeCredential{
		apiKey:    "api-key",
		projectID: "test-project",
		location:  "us-central1",
	}))
	if apiKey.APIKey != "api-key" || apiKey.ProviderAuthResolvers[route.Provider] != nil {
		t.Fatalf("API-key options = %#v, want API key without OAuth resolver", apiKey)
	}
}

func TestGoogleVertexAnthropicAuthOptions(t *testing.T) {
	t.Parallel()

	route := routes["google-vertex-anthropic"]
	model := route.Model(route, defaultGoogleVertexAnthropicModel)
	oauth := applyProbeOptions(authOptions(route, routeCredential{
		accessToken: "oauth-token",
		projectID:   "test-project",
		location:    "us-central1",
	}))
	providerOptions := oauth.ProviderOptions[route.Provider]
	if providerOptions["projectID"] != "test-project" || providerOptions["location"] != "us-central1" {
		t.Fatalf("provider options = %#v, want request-scoped Vertex routing", providerOptions)
	}
	resolver, ok := oauth.ProviderAuthResolvers[route.Provider]
	if !ok {
		t.Fatal("missing google-vertex-anthropic auth resolver")
	}
	credential, err := resolver.Resolve(context.Background(), model, oauth)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if credential.Type != sigma.CredentialTypeOAuthToken || credential.Value != "oauth-token" || credential.Source != "env:GOOGLE_CLOUD_ACCESS_TOKEN" {
		t.Fatalf("credential = %s, want typed environment OAuth token", credential)
	}

	apiKey := applyProbeOptions(authOptions(route, routeCredential{
		apiKey:    "api-key",
		projectID: "test-project",
		location:  "us-central1",
	}))
	if apiKey.APIKey != "api-key" || apiKey.ProviderAuthResolvers[route.Provider] != nil {
		t.Fatalf("API-key options = %#v, want API key without OAuth resolver", apiKey)
	}
}

func TestGoogleVertexProbeRequestRoutingAndAuthentication(t *testing.T) {
	tests := []struct {
		name              string
		credential        routeCredential
		wantAuthorization string
		wantAPIKey        string
	}{
		{
			name:              "oauth bearer",
			credential:        routeCredential{accessToken: "oauth-secret", projectID: "test-project", location: "us-central1"},
			wantAuthorization: "Bearer oauth-secret",
		},
		{
			name:       "api key",
			credential: routeCredential{apiKey: "api-secret", projectID: "test-project", location: "us-central1"},
			wantAPIKey: "api-secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, want := r.URL.Path, "/v1/projects/test-project/locations/us-central1/publishers/google/models/gemini-2.5-flash:streamGenerateContent"; got != want {
					t.Errorf("path = %q, want %q", got, want)
				}
				if got := r.URL.Query().Get("alt"); got != "sse" {
					t.Errorf("alt = %q, want sse", got)
				}
				if got := r.Header.Get("Authorization"); got != tt.wantAuthorization {
					t.Errorf("Authorization = %q, want %q", got, tt.wantAuthorization)
				}
				if got := r.Header.Get("X-Goog-Api-Key"); got != tt.wantAPIKey {
					t.Errorf("X-Goog-Api-Key = %q, want %q", got, tt.wantAPIKey)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				if bytes.Contains(body, []byte("oauth-secret")) || bytes.Contains(body, []byte("api-secret")) {
					t.Errorf("request payload leaked credentials: %s", body)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"responseId\":\"vertex-response\",\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"sigma-ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1,\"totalTokenCount\":2}}\n\n")
			}))
			defer server.Close()

			route := routes["google-vertex"]
			route.BaseURL = server.URL + "/v1"
			model := route.Model(route, "gemini-2.5-flash")
			result := runCase(context.Background(), route, probeClient(route, model), model,
				singleTurnCase("basic_text", "plain streaming text", basicRequest("Reply with exactly: sigma-ok."), []sigma.Option{sigma.WithMaxTokens(128)}),
				tt.credential, "basic_text")
			if result.Outcome != "ok" || result.Error != "" {
				t.Fatalf("probe result = %+v, want success", result)
			}
		})
	}
}

func TestGoogleVertexAnthropicProbeRequestRoutingAndAuthentication(t *testing.T) {
	tests := []struct {
		name              string
		credential        routeCredential
		wantAuthorization string
		wantAPIKey        string
	}{
		{
			name:              "oauth bearer",
			credential:        routeCredential{accessToken: "oauth-secret", projectID: "test-project", location: "us-central1"},
			wantAuthorization: "Bearer oauth-secret",
		},
		{
			name:       "api key",
			credential: routeCredential{apiKey: "api-secret", projectID: "test-project", location: "us-central1"},
			wantAPIKey: "api-secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, want := r.URL.Path, "/v1/projects/test-project/locations/us-central1/publishers/anthropic/models/claude-sonnet-4-6:streamRawPredict"; got != want {
					t.Errorf("path = %q, want %q", got, want)
				}
				if got := r.Header.Get("Authorization"); got != tt.wantAuthorization {
					t.Errorf("Authorization = %q, want %q", got, tt.wantAuthorization)
				}
				if got := r.Header.Get("X-Goog-Api-Key"); got != tt.wantAPIKey {
					t.Errorf("X-Goog-Api-Key = %q, want %q", got, tt.wantAPIKey)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				if bytes.Contains(body, []byte("oauth-secret")) || bytes.Contains(body, []byte("api-secret")) {
					t.Errorf("request payload leaked credentials: %s", body)
				}
				if !bytes.Contains(body, []byte(`"anthropic_version":"vertex-2023-10-16"`)) {
					t.Errorf("request payload missing Vertex Anthropic version: %s", body)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, vertexAnthropicProbeSSE)
			}))
			defer server.Close()

			route := routes["google-vertex-anthropic"]
			route.BaseURL = server.URL + "/v1"
			model := route.Model(route, defaultGoogleVertexAnthropicModel)
			result := runCase(context.Background(), route, probeClient(route, model), model,
				singleTurnCase("basic_text", "plain streaming text", basicRequest("Reply with exactly: sigma-ok."), []sigma.Option{sigma.WithMaxTokens(128)}),
				tt.credential, "basic_text")
			if result.Outcome != "ok" || result.Error != "" {
				t.Fatalf("probe result = %+v, want success", result)
			}
		})
	}
}

func TestGoogleVertexProbeErrorDoesNotLeakAccessToken(t *testing.T) {
	t.Parallel()

	const accessToken = "oauth-secret-that-must-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"denied"}}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	route := routes["google-vertex"]
	route.BaseURL = server.URL + "/v1"
	model := route.Model(route, "gemini-2.5-flash")
	result := runCase(context.Background(), route, probeClient(route, model), model,
		singleTurnCase("basic_text", "plain streaming text", basicRequest("Reply with exactly: sigma-ok."), nil),
		routeCredential{accessToken: accessToken, projectID: "test-project", location: "us-central1"}, "basic_text")
	if result.Error == "" {
		t.Fatal("probe unexpectedly succeeded")
	}
	if strings.Contains(result.Error, accessToken) {
		t.Fatalf("probe error leaked access token: %s", result.Error)
	}
}

func TestGoogleVertexAnthropicProbeErrorDoesNotLeakAccessToken(t *testing.T) {
	t.Parallel()

	const accessToken = "oauth-secret-that-must-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"denied"}}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	route := routes["google-vertex-anthropic"]
	route.BaseURL = server.URL + "/v1"
	model := route.Model(route, defaultGoogleVertexAnthropicModel)
	result := runCase(context.Background(), route, probeClient(route, model), model,
		singleTurnCase("basic_text", "plain streaming text", basicRequest("Reply with exactly: sigma-ok."), nil),
		routeCredential{accessToken: accessToken, projectID: "test-project", location: "us-central1"}, "basic_text")
	if result.Error == "" {
		t.Fatal("probe unexpectedly succeeded")
	}
	if strings.Contains(result.Error, accessToken) {
		t.Fatalf("probe error leaked access token: %s", result.Error)
	}
}

func TestGoogleVertexProbeCasesFollowModelCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		modelID string
		want    []string
		notWant []string
	}{
		{
			modelID: "gemini-2.5-flash",
			want:    []string{"thinking_disabled", "reasoning_level_low", "reasoning_level_medium", "reasoning_level_high"},
		},
		{
			modelID: "gemini-2.5-pro",
			want:    []string{"reasoning_level_low", "reasoning_level_medium", "reasoning_level_high"},
			notWant: []string{"thinking_disabled"},
		},
		{
			modelID: "gemini-3-flash-preview",
			want:    []string{"reasoning_level_low", "reasoning_level_medium", "reasoning_level_high"},
			notWant: []string{"thinking_disabled"},
		},
		{
			modelID: "gemini-3.1-pro-preview",
			want:    []string{"reasoning_level_low", "reasoning_level_medium", "reasoning_level_high"},
			notWant: []string{"thinking_disabled"},
		},
		{
			modelID: "gemini-3.5-flash-lite",
			want:    []string{"reasoning_level_low", "reasoning_level_medium", "reasoning_level_high"},
			notWant: []string{"thinking_disabled"},
		},
		{
			modelID: "gemini-3.6-flash",
			want:    []string{"reasoning_level_low", "reasoning_level_medium", "reasoning_level_high"},
			notWant: []string{"thinking_disabled"},
		},
		{
			modelID: "gemini-flash-latest",
			notWant: []string{"thinking_disabled", "reasoning_level_low", "reasoning_level_medium", "reasoning_level_high"},
		},
		{
			modelID: "gemini-flash-lite-latest",
			notWant: []string{"thinking_disabled", "reasoning_level_low", "reasoning_level_medium", "reasoning_level_high"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			t.Parallel()
			model, ok := sigma.GetModel(sigma.ProviderGoogleVertex, sigma.ModelID(tt.modelID))
			if !ok {
				t.Fatalf("model %q not found", tt.modelID)
			}
			cases := googleVertexProbeCases(routes["google-vertex"], model)
			for _, name := range tt.want {
				if !hasRepairVariant(cases, name) {
					t.Errorf("model %q missing case %q", tt.modelID, name)
				}
			}
			for _, name := range tt.notWant {
				if hasRepairVariant(cases, name) {
					t.Errorf("model %q unexpectedly includes case %q", tt.modelID, name)
				}
			}
		})
	}

	auto := applyProbeOptions(findProbeCase(t, googleVertexProbeCases(routes["google-vertex"], routes["google-vertex"].Model(routes["google-vertex"], "gemini-2.5-flash")), "tool_auto_file_read").Options)
	if auto.GoogleOptions == nil || auto.GoogleOptions.ToolChoice != "auto" {
		t.Fatalf("auto tool options = %#v", auto.GoogleOptions)
	}
	anyChoice := applyProbeOptions(findProbeCase(t, googleVertexProbeCases(routes["google-vertex"], routes["google-vertex"].Model(routes["google-vertex"], "gemini-2.5-flash")), "tool_any_file_read").Options)
	if anyChoice.GoogleOptions == nil || anyChoice.GoogleOptions.ToolChoice != "any" {
		t.Fatalf("any tool options = %#v", anyChoice.GoogleOptions)
	}
}

func TestParseConfigEnablesOpenAICodexBrowserOAuth(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe", "-routes=openai-codex", "-codex-oauth-browser"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if !cfg.codexOAuthBrowser {
		t.Fatal("codexOAuthBrowser = false, want true")
	}
	if cfg.codexOAuth {
		t.Fatal("codexOAuth = true, want false")
	}
	if !reflect.DeepEqual(cfg.routes, []string{"openai-codex"}) {
		t.Fatalf("routes = %v, want openai-codex", cfg.routes)
	}
}

func TestParseConfigHandoffDefaultOff(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if cfg.handoff {
		t.Fatal("handoff = true, want default false")
	}
}

func TestParseConfigEnablesHandoff(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe", "-handoff", "-routes=sigmatest"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if !cfg.handoff {
		t.Fatal("handoff = false, want true")
	}
	if !reflect.DeepEqual(cfg.routes, []string{"sigmatest"}) {
		t.Fatalf("routes = %v, want sigmatest", cfg.routes)
	}
}

func TestParseConfigEnablesStructuredOutput(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe", "-structured-output", "-routes=zen"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if !cfg.structuredOutput {
		t.Fatal("structuredOutput = false, want true")
	}
	if !reflect.DeepEqual(cfg.routes, []string{"zen"}) {
		t.Fatalf("routes = %v, want zen", cfg.routes)
	}
}

func TestParseConfigSetsCaseTimeout(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe", "-case-timeout=15s"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if got, want := cfg.caseTimeout, 15*time.Second; got != want {
		t.Fatalf("caseTimeout = %s, want %s", got, want)
	}
}

func TestParseConfigEnablesImagesWithOpenAIDefaultRoute(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe", "-images"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if !cfg.images {
		t.Fatal("images = false, want true")
	}
	if !reflect.DeepEqual(cfg.routes, []string{"openai"}) {
		t.Fatalf("routes = %v, want openai image default", cfg.routes)
	}
}

func TestOpenAICodexCredentialRejectsMultipleOAuthModes(t *testing.T) {
	t.Parallel()

	_, err := openAICodexCredential(context.Background(), config{
		codexOAuth:        true,
		codexOAuthBrowser: true,
	})
	if err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("error = %v, want mutually exclusive OAuth mode error", err)
	}
}

func TestOpenAICodexProbeCasesUseURLImageInput(t *testing.T) {
	t.Parallel()

	testCase := findProbeCase(t, openAICodexProbeCases(routes["openai-codex"], sigma.Model{}), "image_input")
	image := testCase.Request.Messages[0].Content[1]
	if got, want := image.ImageSource, "url"; got != want {
		t.Fatalf("image source = %q, want %q", got, want)
	}
	if image.URL == "" {
		t.Fatal("image URL was empty")
	}
}

func TestImageRequestEmbedsValidVisiblePNG(t *testing.T) {
	t.Parallel()

	request := imageRequest()
	image := request.Messages[0].Content[1]
	if image.MIMEType != "image/png" || image.ImageSource != "base64" {
		t.Fatalf("image block = %#v, want base64 PNG", image)
	}
	data, err := base64.StdEncoding.DecodeString(image.Data)
	if err != nil {
		t.Fatalf("decode base64 image: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode PNG image: %v", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != 32 || bounds.Dy() != 32 {
		t.Fatalf("image dimensions = %dx%d, want 32x32", bounds.Dx(), bounds.Dy())
	}
	r, g, b, a := decoded.At(bounds.Min.X+16, bounds.Min.Y+16).RGBA()
	if a == 0 || r <= g || r <= b {
		t.Fatalf("center pixel rgba = %x/%x/%x/%x, want visible red pixel", r, g, b, a)
	}
}

func TestOpenAIImageRouteBuildsExpectedModel(t *testing.T) {
	t.Parallel()

	route := imageRoutes["openai"]
	if route.RegisterProvider == nil {
		t.Fatal("openai image route missing provider registration")
	}
	if got, want := route.Provider, sigma.ProviderOpenAI; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	model := route.Model(route, defaultOpenAIImageProbeModel)
	if model.Provider != sigma.ProviderOpenAI || model.API != sigma.ImageAPIOpenAIImages {
		t.Fatalf("image model provider/API = %q/%q", model.Provider, model.API)
	}
	assertMetadataString(t, model.ProviderMetadata, "baseURL", openai.DefaultBaseURL)
	assertMetadataString(t, model.ProviderMetadata, "probeSurface", "openai-images")
	assertMetadataStrings(t, model.ProviderMetadata, "apiKeyEnvVars", []string{"OPENAI_API_KEY"})
}

func TestGoogleImageRoutesUseGeneratedModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		routeName string
		modelID   string
		wantAPI   sigma.ImageAPI
	}{
		{routeName: "google", modelID: defaultGoogleGeminiImageProbeModel, wantAPI: sigma.ImageAPIGoogleImages},
		{routeName: "google", modelID: defaultGoogleCurrentImageProbeModel, wantAPI: sigma.ImageAPIGoogleImages},
		{routeName: routeGoogleVertex, modelID: defaultGoogleCurrentImageProbeModel, wantAPI: sigma.ImageAPIGoogleVertexImages},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.routeName+"/"+tt.modelID, func(t *testing.T) {
			t.Parallel()

			route := imageRoutes[tt.routeName]
			model := route.Model(route, tt.modelID)
			generated, ok := sigma.GetImageModel(route.Provider, sigma.ModelID(tt.modelID))
			if !ok {
				t.Fatalf("generated image model %q was not registered", tt.modelID)
			}
			if !reflect.DeepEqual(model, generated) {
				t.Fatalf("probe model = %#v, want generated model %#v", model, generated)
			}
			if model.API != tt.wantAPI {
				t.Fatalf("image API = %q, want %q", model.API, tt.wantAPI)
			}

			registry := sigma.NewRegistry()
			if err := route.RegisterProvider(registry, route); err != nil {
				t.Fatalf("register image provider: %v", err)
			}
			provider, ok := registry.ImageProvider(route.Provider)
			if !ok || provider.API() != tt.wantAPI {
				t.Fatalf("registered provider = %#v, want API %q", provider, tt.wantAPI)
			}
		})
	}
}

func TestGoogleImageProbeCasesUseExpectedGeneratedModels(t *testing.T) {
	t.Parallel()

	direct := googleImageProbeCases(imageRoutes["google"])
	if got, want := len(direct), 2; got != want {
		t.Fatalf("direct Google cases = %d, want %d", got, want)
	}
	if got := findImageProbeCase(t, direct, "generate_gemini").ModelID; got != defaultGoogleGeminiImageProbeModel {
		t.Fatalf("Gemini model = %q, want %q", got, defaultGoogleGeminiImageProbeModel)
	}
	if got := findImageProbeCase(t, direct, "generate_gemini_3_1").ModelID; got != defaultGoogleCurrentImageProbeModel {
		t.Fatalf("current Gemini model = %q, want %q", got, defaultGoogleCurrentImageProbeModel)
	}

	vertex := googleImageProbeCases(imageRoutes[routeGoogleVertex])
	if got, want := len(vertex), 1; got != want {
		t.Fatalf("Vertex cases = %d, want %d", got, want)
	}
	if got := vertex[0].ModelID; got != defaultGoogleCurrentImageProbeModel {
		t.Fatalf("Vertex Gemini model = %q, want %q", got, defaultGoogleCurrentImageProbeModel)
	}
}

func TestGoogleImageCredentialPrecedence(t *testing.T) {
	clearGoogleVertexEnvironment(t)
	t.Setenv("GOOGLE_API_KEY", "google-key")
	t.Setenv("GOOGLE_CLOUD_API_KEY", "cloud-key")

	credential, err := credentialForImageRoute(imageRoutes["google"])
	if err != nil {
		t.Fatalf("credentialForImageRoute returned error: %v", err)
	}
	if got, want := credential.apiKey, "google-key"; got != want {
		t.Fatalf("API key = %q, want %q", got, want)
	}

	t.Setenv("GOOGLE_API_KEY", "")
	credential, err = credentialForImageRoute(imageRoutes["google"])
	if err != nil {
		t.Fatalf("cloud fallback returned error: %v", err)
	}
	if got, want := credential.apiKey, "cloud-key"; got != want {
		t.Fatalf("fallback API key = %q, want %q", got, want)
	}
}

func TestGoogleImageCredentialsReportMissingRequirements(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		clearGoogleVertexEnvironment(t)
		_, err := credentialForImageRoute(imageRoutes["google"])
		if err == nil || !strings.Contains(err.Error(), "GOOGLE_API_KEY or GOOGLE_CLOUD_API_KEY") {
			t.Fatalf("error = %v, want direct Google credential requirements", err)
		}
	})
	t.Run("vertex", func(t *testing.T) {
		clearGoogleVertexEnvironment(t)
		_, err := credentialForImageRoute(imageRoutes["google-vertex"])
		if err == nil || !strings.Contains(err.Error(), "GOOGLE_CLOUD_PROJECT or GCLOUD_PROJECT") {
			t.Fatalf("error = %v, want Vertex routing requirements", err)
		}
	})
}

func TestGoogleVertexImageAuthOptions(t *testing.T) {
	t.Parallel()

	route := imageRoutes[routeGoogleVertex]
	model := route.Model(route, defaultGoogleCurrentImageProbeModel)
	oauth := applyImageProbeOptions(imageAuthOptions(route, routeCredential{
		accessToken: "oauth-token",
		projectID:   "test-project",
		location:    "us-central1",
	}))
	providerOptions := oauth.ProviderOptions[route.Provider]
	if providerOptions["projectID"] != "test-project" || providerOptions["location"] != "us-central1" {
		t.Fatalf("provider options = %#v, want request-scoped Vertex routing", providerOptions)
	}
	resolver, ok := oauth.ProviderAuthResolvers[route.Provider]
	if !ok {
		t.Fatal("missing google-vertex image auth resolver")
	}
	credential, err := resolver.Resolve(context.Background(), sigma.Model{ID: model.ID, Provider: model.Provider}, oauth)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if credential.Type != sigma.CredentialTypeOAuthToken || credential.Value != "oauth-token" || credential.Source != "env:GOOGLE_CLOUD_ACCESS_TOKEN" {
		t.Fatalf("credential = %s, want typed environment OAuth token", credential)
	}

	apiKey := applyImageProbeOptions(imageAuthOptions(route, routeCredential{
		apiKey:    "api-key",
		projectID: "test-project",
		location:  "us-central1",
	}))
	if apiKey.APIKey != "api-key" || apiKey.ProviderAuthResolvers[route.Provider] != nil {
		t.Fatalf("API-key options = %#v, want API key without OAuth resolver", apiKey)
	}
}

func TestGoogleImageModelFilteringSkipsBeforeCredentials(t *testing.T) {
	clearGoogleVertexEnvironment(t)

	var results []probeResult
	runImageProbes(context.Background(), config{
		routes:      []string{"google"},
		models:      map[string]bool{"unrelated-image-model": true},
		caseTimeout: time.Second,
	}, func(result probeResult) {
		results = append(results, result)
	})
	if got, want := len(results), 1; got != want {
		t.Fatalf("results = %d, want %d: %#v", got, want, results)
	}
	if result := results[0]; result.Outcome != "skipped" || result.Attempt != "model_selection" || !strings.Contains(result.Error, "not a built-in google image model") {
		t.Fatalf("result = %+v, want explicit model-selection skip", result)
	}
}

func TestOpenAIImageProbeCasesUseExpectedModels(t *testing.T) {
	t.Parallel()

	cases := openAIImageProbeCases(imageRoutes["openai"])
	if got, want := len(cases), 6; got != want {
		t.Fatalf("cases = %d, want %d", got, want)
	}
	if findImageProbeCase(t, cases, "variation").ModelID != defaultOpenAIImageVariationModel {
		t.Fatal("variation case did not use DALL-E 2 model")
	}
	if !findImageProbeCase(t, cases, "stream_partial").Stream {
		t.Fatal("stream_partial case did not enable streaming")
	}
	if !findImageProbeCase(t, cases, "responses_image_tool").ResponsesTool {
		t.Fatal("responses_image_tool case did not use Responses tool path")
	}
}

func TestRunOpenAIImageCasesUseExpectedRequestShapes(t *testing.T) {
	t.Parallel()

	var recordsMu sync.Mutex
	var records []imageProbeRequestRecord
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := captureImageProbeRequest(t, r)
		recordsMu.Lock()
		records = append(records, record)
		recordsMu.Unlock()
		if record.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"type":"image_generation.partial_image","partial_image_index":0,"b64_json":"cGFydGlhbA=="}`+"\n\n")
			_, _ = io.WriteString(w, `data: {"type":"image_generation.completed","data":[{"b64_json":"ZmluYWw="}]}`+"\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":"ZmluYWw="}]}`)
	}))
	t.Cleanup(server.Close)

	route := imageRoutes["openai"]
	route.BaseURL = server.URL
	for _, name := range []string{"generate", "edit_multipart", "edit_reference_json", "variation", "stream_partial"} {
		testCase := findImageProbeCase(t, route.Cases(route), name)
		result := runImageCase(context.Background(), route, testCase, routeCredential{apiKey: "key"})
		if result.Outcome != "ok" {
			t.Fatalf("%s result = %+v, want ok", name, result)
		}
		if result.Hint == "" {
			t.Fatalf("%s hint was empty", name)
		}
	}
	recordsMu.Lock()
	gotRecords := append([]imageProbeRequestRecord(nil), records...)
	recordsMu.Unlock()
	if got, want := len(gotRecords), 5; got != want {
		t.Fatalf("requests = %d, want %d: %#v", got, want, gotRecords)
	}
	assertImageProbeRecord(t, gotRecords[0], "/images/generations", defaultOpenAIImageProbeModel, false, false)
	assertImageProbeRecord(t, gotRecords[1], "/images/edits", defaultOpenAIImageProbeModel, true, false)
	assertImageProbeRecord(t, gotRecords[2], "/images/edits", defaultOpenAIImageProbeModel, true, false)
	assertImageProbeRecord(t, gotRecords[3], "/images/variations", defaultOpenAIImageVariationModel, true, false)
	assertImageProbeRecord(t, gotRecords[4], "/images/generations", defaultOpenAIImageProbeModel, false, true)
}

func TestRunDirectGoogleImageCasesUseExpectedEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		caseName    string
		wantPath    string
		wantPayload string
		response    string
	}{
		{
			caseName:    "generate_gemini",
			wantPath:    "/v1beta/models/gemini-2.5-flash-image:generateContent",
			wantPayload: "generationConfig",
			response:    `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`,
		},
		{
			caseName:    "generate_gemini_3_1",
			wantPath:    "/v1beta/models/gemini-3.1-flash-image:generateContent",
			wantPayload: "generationConfig",
			response:    `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.caseName, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Path; got != tt.wantPath {
					t.Errorf("path = %q, want %q", got, tt.wantPath)
				}
				if got, want := r.Header.Get("X-Goog-Api-Key"), "google-key"; got != want {
					t.Errorf("X-Goog-Api-Key = %q, want %q", got, want)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				} else if !bytes.Contains(body, []byte(`"`+tt.wantPayload+`"`)) {
					t.Errorf("request body = %s, want %q payload", body, tt.wantPayload)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.response)
			}))
			t.Cleanup(server.Close)

			route := imageRoutes["google"]
			route.BaseURL = server.URL + "/v1beta"
			testCase := findImageProbeCase(t, route.Cases(route), tt.caseName)
			result := runImageCase(context.Background(), route, testCase, routeCredential{apiKey: "google-key"})
			if result.Outcome != "ok" || result.Hint != "image_generated" {
				t.Fatalf("result = %+v, want generated image success", result)
			}
		})
	}
}

func TestRunGoogleVertexImageCaseUsesRoutingAndAuthentication(t *testing.T) {
	tests := []struct {
		name              string
		credential        routeCredential
		wantAuthorization string
		wantAPIKey        string
	}{
		{
			name:              "oauth bearer",
			credential:        routeCredential{accessToken: "oauth-secret", projectID: "test-project", location: "us-central1"},
			wantAuthorization: "Bearer oauth-secret",
		},
		{
			name:       "api key",
			credential: routeCredential{apiKey: "api-secret", projectID: "test-project", location: "us-central1"},
			wantAPIKey: "api-secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantPath := "/v1/projects/test-project/locations/us-central1/publishers/google/models/gemini-3.1-flash-image:generateContent"
				if got := r.URL.Path; got != wantPath {
					t.Errorf("path = %q, want %q", got, wantPath)
				}
				if got := r.Header.Get("Authorization"); got != tt.wantAuthorization {
					t.Errorf("Authorization = %q, want %q", got, tt.wantAuthorization)
				}
				if got := r.Header.Get("X-Goog-Api-Key"); got != tt.wantAPIKey {
					t.Errorf("X-Goog-Api-Key = %q, want %q", got, tt.wantAPIKey)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`)
			}))
			t.Cleanup(server.Close)

			route := imageRoutes[routeGoogleVertex]
			route.BaseURL = server.URL + "/v1"
			testCase := findImageProbeCase(t, route.Cases(route), "generate_gemini_3_1")
			result := runImageCase(context.Background(), route, testCase, tt.credential)
			if result.Outcome != "ok" {
				t.Fatalf("result = %+v, want ok", result)
			}
		})
	}
}

func TestGoogleImageProbeRequiresBinaryImage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"I created an image."}]}}]}`)
	}))
	t.Cleanup(server.Close)

	route := imageRoutes["google"]
	route.BaseURL = server.URL + "/v1beta"
	testCase := findImageProbeCase(t, route.Cases(route), "generate_gemini")
	result := runImageCase(context.Background(), route, testCase, routeCredential{apiKey: "google-key"})
	if result.Outcome != "no_working_attempt" || !strings.Contains(result.Error, "non-empty base64 or URL image") {
		t.Fatalf("result = %+v, want text-only response rejected", result)
	}
}

func TestImageProbeRetriesMissingOutput(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxImageProvider(
		sigmatest.ImageScript{Response: sigma.AssistantImages{Images: []sigma.ImageInput{sigma.ImageText("I will create an image.")}}},
		sigmatest.ImageScript{Response: sigma.AssistantImages{Images: []sigma.ImageInput{sigma.ImageOutputData("image/png", "aW1hZ2U=")}}},
	)
	model := sigmatest.ImageModel()
	route := imageRouteSpec{
		Name:     "sigmatest",
		Provider: model.Provider,
		RegisterProvider: func(registry *sigma.Registry, _ imageRouteSpec) error {
			return sigmatest.RegisterImages(registry, provider, model)
		},
		Model: func(_ imageRouteSpec, _ string) sigma.ImageModel { return model },
	}
	testCase := imageProbeCase{
		Name:         "generate",
		ModelID:      string(model.ID),
		Request:      sigma.ImageRequest{Prompt: "Create an icon.", Size: string(sigma.ImageSize1024x1024), Count: 1},
		RequireImage: true,
	}

	result := runImageCaseWithTimeout(context.Background(), time.Second, route, testCase, routeCredential{})
	if result.Outcome != "ok" {
		t.Fatalf("result = %+v, want recovered image success", result)
	}
	if got, want := len(result.FailedAttempts), 1; got != want {
		t.Fatalf("failed attempts = %#v, want %d missing-output attempt", result.FailedAttempts, want)
	}
	if result.OriginalError != "image response did not include a non-empty base64 or URL image" {
		t.Fatalf("original error = %q, want missing-output failure", result.OriginalError)
	}
	requests := provider.Requests()
	if got, want := len(requests), 2; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
	if !reflect.DeepEqual(requests[0].Request, requests[1].Request) {
		t.Fatalf("retry request = %#v, want identical to %#v", requests[1].Request, requests[0].Request)
	}
}

func TestImageProbeReportsPersistentMissingOutput(t *testing.T) {
	t.Parallel()

	textOnly := sigmatest.ImageScript{Response: sigma.AssistantImages{Images: []sigma.ImageInput{sigma.ImageText("I cannot create an image.")}}}
	provider := sigmatest.NewFauxImageProvider(textOnly, textOnly, textOnly)
	model := sigmatest.ImageModel()
	route := imageRouteSpec{
		Name:     "sigmatest",
		Provider: model.Provider,
		RegisterProvider: func(registry *sigma.Registry, _ imageRouteSpec) error {
			return sigmatest.RegisterImages(registry, provider, model)
		},
		Model: func(_ imageRouteSpec, _ string) sigma.ImageModel { return model },
	}
	testCase := imageProbeCase{
		Name:         "generate",
		ModelID:      string(model.ID),
		Request:      sigma.ImageRequest{Prompt: "Create an icon.", Count: 1},
		RequireImage: true,
	}

	result := runImageCaseWithTimeout(context.Background(), time.Second, route, testCase, routeCredential{})
	if result.Outcome != "no_working_attempt" || result.Error != errImageProbeMissingOutput.Error() {
		t.Fatalf("result = %+v, want persistent missing-output failure", result)
	}
	if got, want := len(result.FailedAttempts), maxTransientRetries; got != want {
		t.Fatalf("failed attempts = %#v, want %d retried failures", result.FailedAttempts, want)
	}
	if got, want := len(provider.Requests()), maxTransientRetries+1; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
}

func TestGoogleVertexImageProbeRedactsCredentials(t *testing.T) {
	t.Parallel()

	const secret = "oauth-secret-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid credential","access_token":"`+secret+`"}}`)
	}))
	t.Cleanup(server.Close)

	route := imageRoutes[routeGoogleVertex]
	route.BaseURL = server.URL + "/v1"
	testCase := findImageProbeCase(t, route.Cases(route), "generate_gemini_3_1")
	result := runImageCase(context.Background(), route, testCase, routeCredential{
		accessToken: secret,
		projectID:   "test-project",
		location:    "us-central1",
	})
	if result.Outcome == "ok" {
		t.Fatalf("result = %+v, want authentication failure", result)
	}
	if strings.Contains(result.Error, secret) || !strings.Contains(result.Error, "[redacted]") {
		t.Fatalf("error = %q, want credential redaction", result.Error)
	}
}

func TestImageCaseTimeoutDoesNotCancelFollowingCase(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxImageProvider(
		sigmatest.ImageScript{WaitForCancel: true},
		sigmatest.ImageScript{Response: sigma.AssistantImages{Images: []sigma.ImageInput{sigma.ImageOutputData("image/png", "aW1hZ2U=")}}},
	)
	model := sigmatest.ImageModel()
	route := imageRouteSpec{
		Name:     "sigmatest",
		Provider: model.Provider,
		RegisterProvider: func(registry *sigma.Registry, _ imageRouteSpec) error {
			return sigmatest.RegisterImages(registry, provider, model)
		},
		Model: func(_ imageRouteSpec, _ string) sigma.ImageModel { return model },
	}
	testCase := imageProbeCase{
		Name:         "generate",
		ModelID:      string(model.ID),
		Request:      sigma.ImageRequest{Prompt: "Create an icon.", Size: string(sigma.ImageSize1024x1024), Count: 1},
		RequireImage: true,
	}

	first := runImageCaseWithTimeout(context.Background(), 10*time.Millisecond, route, testCase, routeCredential{})
	if first.Outcome != "upstream_availability" || !strings.Contains(first.Error, "deadline exceeded") {
		t.Fatalf("first result = %+v, want isolated case timeout", first)
	}
	second := runImageCaseWithTimeout(context.Background(), time.Second, route, testCase, routeCredential{})
	if second.Outcome != "ok" {
		t.Fatalf("second result = %+v, want following case to run", second)
	}
}

func TestRunOpenAIResponsesImageToolCaseDetectsImageOutput(t *testing.T) {
	t.Parallel()

	var sawImageToolMu sync.Mutex
	var sawImageTool bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/responses"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		tools, _ := payload["tools"].([]any)
		for _, tool := range tools {
			toolMap, _ := tool.(map[string]any)
			if toolMap["type"] == "image_generation" {
				sawImageToolMu.Lock()
				sawImageTool = true
				sawImageToolMu.Unlock()
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"id":"resp_image","status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_item.added","response_id":"resp_image","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.image_generation_call.partial_image","response_id":"resp_image","item_id":"ig_1","output_index":0,"partial_image_b64":"cGFydGlhbA=="}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_item.done","response_id":"resp_image","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","status":"completed","result":"ZmluYWw="}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"id":"resp_image","status":"completed","output":[{"type":"image_generation_call","id":"ig_1","status":"completed","result":"ZmluYWw="}]}}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	route := imageRoutes["openai"]
	route.BaseURL = server.URL
	testCase := findImageProbeCase(t, route.Cases(route), "responses_image_tool")
	result := runImageCase(context.Background(), route, testCase, routeCredential{apiKey: "key"})
	if result.Outcome != "ok" {
		t.Fatalf("result = %+v, want ok", result)
	}
	if got, want := result.Hint, "image_tool_output_seen"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	sawImageToolMu.Lock()
	gotSawImageTool := sawImageTool
	sawImageToolMu.Unlock()
	if !gotSawImageTool {
		t.Fatal("request did not include image_generation tool")
	}
}

func openAIProbeTestCredentials() *openai.CodexOAuthCredentials {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]string{
			"chatgpt_account_id": "acct_probe",
		},
	})
	token := header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	return &openai.CodexOAuthCredentials{AccessToken: token}
}

func TestAnthropicProbeCasesDoNotSendRawOpenAIExtraBody(t *testing.T) {
	t.Parallel()

	route := routes["fireworks-anthropic"]
	model := route.Model(route, "accounts/fireworks/models/kimi-k2p6")
	for _, testCase := range anthropicCompatibleProbeCases(route, model) {
		options := applyProbeOptions(testCase.Options)
		if providerOptions := options.ProviderOptions[sigma.ProviderFireworks]; providerOptions != nil {
			if _, ok := providerOptions["extra_body"]; ok {
				t.Fatalf("%s set raw extra_body for Anthropic route: %#v", testCase.Name, providerOptions)
			}
		}
	}
}

func TestAnthropicProbeCasesFollowModelCapabilitiesAndTypedOptions(t *testing.T) {
	t.Parallel()

	route := routes["google-vertex-anthropic"]
	model := route.Model(route, defaultGoogleVertexAnthropicModel)
	cases := anthropicCompatibleProbeCases(route, model)
	for _, name := range []string{
		"basic_text",
		"developer_instruction",
		"cache_ephemeral",
		"image_input",
		"reasoning_level_low",
		"reasoning_level_medium",
		"reasoning_level_high",
		"tool_auto_file_read",
		"tool_required_file_read",
	} {
		if !hasRepairVariant(cases, name) {
			t.Errorf("Vertex Anthropic probe cases missing %q", name)
		}
	}

	cache := applyProbeOptions(findProbeCase(t, cases, "cache_ephemeral").Options)
	if got, want := cache.SessionID, "sigma-google-vertex-anthropic-probe"; got != want {
		t.Fatalf("cache session ID = %q, want %q", got, want)
	}

	auto := applyProbeOptions(findProbeCase(t, cases, "tool_auto_file_read").Options)
	if auto.ToolChoice != sigma.ToolChoiceAuto || auto.OpenAIOptions != nil {
		t.Fatalf("automatic Anthropic tool options = %#v, want provider-neutral auto without OpenAI options", auto)
	}
	required := applyProbeOptions(findProbeCase(t, cases, "tool_required_file_read").Options)
	if required.AnthropicOptions == nil || required.AnthropicOptions.ToolChoice == nil ||
		required.AnthropicOptions.ToolChoice.Type != sigma.AnthropicToolChoiceAny || required.OpenAIOptions != nil {
		t.Fatalf("required Anthropic tool options = %#v, want typed Anthropic any without OpenAI options", required)
	}

	limited := sigma.Model{
		ID:              "limited-claude",
		Provider:        sigma.ProviderGoogleVertexAnthropic,
		API:             sigma.APIAnthropicMessages,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
	}
	limitedCases := anthropicCompatibleProbeCases(route, limited)
	if got, want := probeCaseNames(limitedCases), []string{"basic_text", "developer_instruction", "cache_ephemeral"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("limited model cases = %v, want %v", got, want)
	}
}

func TestXAIRouteRegistrationBuildsClient(t *testing.T) {
	t.Parallel()

	route := routes["xai"]
	registry := sigma.NewRegistry()
	if err := route.RegisterProvider(registry, route); err != nil {
		t.Fatalf("RegisterProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(route.Model(route, "grok-code-fast-1")); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestNVIDIARouteRegistrationBuildsClient(t *testing.T) {
	t.Parallel()

	route := routes["nvidia"]
	registry := sigma.NewRegistry()
	if err := route.RegisterProvider(registry, route); err != nil {
		t.Fatalf("RegisterProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(route.Model(route, defaultNVIDIAProbeModel)); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestRunCaseKeepsDistinctRouteNames(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ID: "same-provider-model", Provider: sigma.ProviderFireworks, API: sigmatest.TextAPI}
	for _, routeName := range []string{"fireworks-openai", "fireworks-anthropic"} {
		route := routes[routeName]
		provider := sigmatest.NewFauxProvider()
		registry := sigma.NewRegistry()
		if err := registry.RegisterTextProvider(sigma.ProviderFireworks, provider); err != nil {
			t.Fatalf("RegisterTextProvider returned error: %v", err)
		}
		if err := registry.RegisterModel(model); err != nil {
			t.Fatalf("RegisterModel returned error: %v", err)
		}

		client := sigma.NewClient(sigma.WithRegistry(registry))
		result := runCase(context.Background(), route, client, model, singleTurnCase("basic", "", basicRequest("hi"), nil), routeCredential{apiKey: "key"}, "basic")
		if result.Route != routeName {
			t.Fatalf("route = %q, want %q", result.Route, routeName)
		}
	}
}

func TestGenerateHandoffSourceBuildsToolContext(t *testing.T) {
	t.Parallel()

	route := handoffProbeRoute(t, "handoff-source",
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content:    []sigma.ContentBlock{sigma.ToolCallBlock("call_double", "double_number", map[string]any{"value": 21})},
			StopReason: sigma.StopReasonToolCalls,
		}},
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("42")},
		}},
	)

	source, result := generateHandoffSource(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{})
	if result.Outcome != "ok" {
		t.Fatalf("result = %+v, want ok", result)
	}
	if len(source.Messages) != 4 {
		t.Fatalf("source messages = %d, want 4", len(source.Messages))
	}
	if got, want := source.Messages[1].Content[0].ToolCallID, "call_double"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	if got, want := source.Messages[2].Role, sigma.RoleTool; got != want {
		t.Fatalf("tool result role = %q, want %q", got, want)
	}
	if got, want := source.Messages[2].ToolName, "double_number"; got != want {
		t.Fatalf("tool result name = %q, want %q", got, want)
	}
	if got, want := source.Messages[2].Content[0].Text, "42"; got != want {
		t.Fatalf("tool result text = %q, want %q", got, want)
	}
}

func TestGenerateHandoffSourceSkipsWithoutToolCall(t *testing.T) {
	t.Parallel()

	route := handoffProbeRoute(t, "handoff-no-tool",
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("no tool")},
		}},
	)

	_, result := generateHandoffSource(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{})
	if got, want := result.Outcome, "skipped"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if !strings.Contains(result.Error, "did not emit a tool call") {
		t.Fatalf("error = %q, want no-tool-call diagnostic", result.Error)
	}
}

func TestRunHandoffTargetEmitsSourceMetadata(t *testing.T) {
	t.Parallel()

	sourceRoute := handoffProbeRoute(t, "handoff-source")
	targetRoute := handoffProbeRoute(t, "handoff-target",
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("Hello, handoff successful.")},
		}},
	)
	source := handoffSource{
		Route: sourceRoute,
		Model: sourceRoute.Model(sourceRoute, "source-model"),
		Messages: []sigma.Message{
			sigma.UserText("Use the tool."),
			{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_double", "double_number", map[string]any{"value": 21})},
			},
			{Role: sigma.RoleTool, ToolCallID: "call_double", ToolName: "double_number", Content: []sigma.ContentBlock{sigma.Text("42")}},
			{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.Text("42")}},
		},
	}
	target := handoffSource{
		Route:      targetRoute,
		Model:      targetRoute.Model(targetRoute, "target-model"),
		Credential: routeCredential{apiKey: "key"},
	}

	result := runHandoffTarget(context.Background(), source, target)
	if result.Outcome != "ok" {
		t.Fatalf("result = %+v, want ok", result)
	}
	if got, want := result.SourceRoute, "handoff-source"; got != want {
		t.Fatalf("source route = %q, want %q", got, want)
	}
	if got, want := result.SourceModel, "source-model"; got != want {
		t.Fatalf("source model = %q, want %q", got, want)
	}
}

func TestRunHandoffProbesEmitsPairwiseResults(t *testing.T) {
	oldRoutes := routes
	t.Cleanup(func() {
		routes = oldRoutes
	})

	routes = map[string]routeSpec{
		"handoff-a": handoffProbeRoute(t, "handoff-a",
			sigmatest.Script{Final: sigma.AssistantMessage{
				Content:    []sigma.ContentBlock{sigma.ToolCallBlock("call_a", "double_number", map[string]any{"value": 21})},
				StopReason: sigma.StopReasonToolCalls,
			}},
			sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("42")}}},
			sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("Hello, handoff successful.")}}},
		),
		"handoff-b": handoffProbeRoute(t, "handoff-b",
			sigmatest.Script{Final: sigma.AssistantMessage{
				Content:    []sigma.ContentBlock{sigma.ToolCallBlock("call_b", "double_number", map[string]any{"value": 21})},
				StopReason: sigma.StopReasonToolCalls,
			}},
			sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("42")}}},
			sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("Hello, handoff successful.")}}},
		),
	}
	t.Setenv("SIGMATEST_API_KEY", "key")

	var emitted []probeResult
	runHandoffProbes(context.Background(), config{
		routes: []string{"handoff-a", "handoff-b"},
		models: map[string]bool{"model": true},
	}, func(result probeResult) {
		emitted = append(emitted, result)
	})

	if len(emitted) != 4 {
		t.Fatalf("emitted results = %d, want 4: %#v", len(emitted), emitted)
	}
	if emitted[0].Case != "handoff_source" || emitted[0].Outcome != "ok" {
		t.Fatalf("first source result = %+v, want ok handoff_source", emitted[0])
	}
	if emitted[1].Case != "handoff_source" || emitted[1].Outcome != "ok" {
		t.Fatalf("second source result = %+v, want ok handoff_source", emitted[1])
	}
	if emitted[2].Case != "handoff_replay" || emitted[2].SourceRoute != "handoff-a" || emitted[2].Route != "handoff-b" {
		t.Fatalf("first pairwise result = %+v, want handoff-a -> handoff-b", emitted[2])
	}
	if emitted[3].Case != "handoff_replay" || emitted[3].SourceRoute != "handoff-b" || emitted[3].Route != "handoff-a" {
		t.Fatalf("second pairwise result = %+v, want handoff-b -> handoff-a", emitted[3])
	}
}

func TestProbeModelEachEmitsEachCompletedCase(t *testing.T) {
	t.Parallel()

	route := sigmatestProbeRouteWithCases(t, []probeCase{
		singleTurnCase("first", "first case", basicRequest("first"), nil),
		singleTurnCase("second", "second case", basicRequest("second"), nil),
	}, sigmatest.Script{}, sigmatest.Script{})

	var emitted []probeResult
	probeModelEach(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{}, func(result probeResult) {
		emitted = append(emitted, result)
	})
	if len(emitted) != 2 {
		t.Fatalf("emitted length = %d, want 2", len(emitted))
	}
	if got, want := emitted[0].Case, "first"; got != want {
		t.Fatalf("first emitted case = %q, want %q", got, want)
	}
	if got, want := emitted[1].Case, "second"; got != want {
		t.Fatalf("second emitted case = %q, want %q", got, want)
	}
}

func TestStructuredOutputProbeModelEachRunsOnlyStructuredCases(t *testing.T) {
	t.Parallel()

	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{
		singleTurnCase("basic_text", "basic case", basicRequest("basic"), nil),
		singleTurnCase("json_object", "JSON object mode", basicRequest("json object"), nil),
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("json schema"), nil),
	}, sigmatest.Script{}, sigmatest.Script{})

	var emitted []probeResult
	probeModelEach(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{structuredOutput: true}, func(result probeResult) {
		emitted = append(emitted, result)
	})
	if len(emitted) != 2 {
		t.Fatalf("emitted length = %d, want 2", len(emitted))
	}
	if got, want := emitted[0].Case, "json_object"; got != want {
		t.Fatalf("first case = %q, want %q", got, want)
	}
	if got, want := emitted[0].Hint, "json_object_supported"; got != want {
		t.Fatalf("first hint = %q, want %q", got, want)
	}
	if got, want := emitted[1].Case, "json_schema"; got != want {
		t.Fatalf("second case = %q, want %q", got, want)
	}
	if got, want := emitted[1].Hint, "json_schema_supported"; got != want {
		t.Fatalf("second hint = %q, want %q", got, want)
	}
	recommendation, ok := recommendationFor(emitted[1])
	if !ok {
		t.Fatal("recommendationFor returned false")
	}
	if got, want := recommendation.Evidence, "json_schema supported by json_schema"; got != want {
		t.Fatalf("evidence = %q, want %q", got, want)
	}
}

func TestStructuredOutputProbeModelEachSkipsNonOpenAICompatibleModels(t *testing.T) {
	t.Parallel()

	route := sigmatestProbeRouteWithCases(t, []probeCase{
		singleTurnCase("json_object", "JSON object mode", basicRequest("json object"), nil),
	}, sigmatest.Script{})

	var emitted []probeResult
	probeModelEach(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{structuredOutput: true}, func(result probeResult) {
		emitted = append(emitted, result)
	})
	if len(emitted) != 1 {
		t.Fatalf("emitted length = %d, want 1", len(emitted))
	}
	if got, want := emitted[0].Case, "structured_output"; got != want {
		t.Fatalf("case = %q, want %q", got, want)
	}
	if got, want := emitted[0].Attempt, "unsupported_api"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if got, want := emitted[0].Outcome, "skipped"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if _, ok := recommendationFor(emitted[0]); ok {
		t.Fatal("recommendationFor returned true for incompatible API skip")
	}
}

func TestProbeModelPrefersTargetedRepairOverAvailabilityCheck(t *testing.T) {
	t.Parallel()

	route := sigmatestProbeRoute(
		t,
		sigmatest.Script{Err: errors.New("strict schema failed")},
		sigmatest.Script{},
		sigmatest.Script{},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{repair: true})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if got, want := results[0].Case, "json_schema"; got != want {
		t.Fatalf("case = %q, want %q", got, want)
	}
	if got, want := results[0].Attempt, "json_schema_more_tokens"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if got, want := results[0].Outcome, "fixed_by_repair_variant"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if got, want := results[0].OriginalError, "strict schema failed"; got != want {
		t.Fatalf("original error = %q, want %q", got, want)
	}
	if got, want := results[0].Hint, "json_schema_needs_larger_output_budget"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	assertFailedAttempts(t, results[0].FailedAttempts, []failedAttempt{
		{Attempt: "json_schema", Error: "strict schema failed"},
	})
	recommendation, ok := recommendationFor(results[0])
	if !ok {
		t.Fatal("recommendationFor returned false")
	}
	if recommendation.Route != "sigmatest" || recommendation.Model != "model" ||
		recommendation.Case != "json_schema" || recommendation.Hint != "json_schema_needs_larger_output_budget" ||
		recommendation.Evidence != "json_schema repaired by json_schema_more_tokens" {
		t.Fatalf("recommendation = %+v", recommendation)
	}
}

func TestStructuredOutputProbeReportsJSONObjectAsControl(t *testing.T) {
	t.Parallel()

	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("json schema"), nil),
	},
		sigmatest.Script{Err: errors.New("strict schema failed")},
		sigmatest.Script{},
		sigmatest.Script{Err: errors.New("larger schema failed")},
		sigmatest.Script{},
		sigmatest.Script{Err: errors.New("manual json failed")},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{structuredOutput: true})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if got, want := results[0].Outcome, "inconclusive"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if got, want := results[0].Attempt, "json_schema"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if got, want := results[0].Hint, "minimal_text_available_after_failure"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	if got, want := results[0].SuccessfulControls, []string{"json_object_fallback"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("successful controls = %v, want %v", got, want)
	}
	assertFailedAttempts(t, results[0].FailedAttempts, []failedAttempt{
		{Attempt: "json_schema", Error: "strict schema failed"},
		{Attempt: "json_schema_more_tokens", Error: "larger schema failed"},
		{Attempt: "manual_json", Error: "manual json failed"},
	})
}

func TestStructuredOutputProbeReportsPromptJSONAsControl(t *testing.T) {
	t.Parallel()

	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("json schema"), nil),
	},
		sigmatest.Script{Err: errors.New("strict schema failed")},
		sigmatest.Script{},
		sigmatest.Script{Err: errors.New("larger schema failed")},
		sigmatest.Script{Err: errors.New("json object failed")},
		sigmatest.Script{},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{structuredOutput: true})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if got, want := results[0].Outcome, "inconclusive"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if got, want := results[0].Attempt, "json_schema"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if got, want := results[0].Hint, "minimal_text_available_after_failure"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	if got, want := results[0].SuccessfulControls, []string{"manual_json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("successful controls = %v, want %v", got, want)
	}
	assertFailedAttempts(t, results[0].FailedAttempts, []failedAttempt{
		{Attempt: "json_schema", Error: "strict schema failed"},
		{Attempt: "json_schema_more_tokens", Error: "larger schema failed"},
		{Attempt: "json_object_fallback", Error: "json object failed"},
	})
}

func TestProbeModelReportsAvailabilityCheckSeparately(t *testing.T) {
	t.Parallel()

	route := sigmatestProbeRoute(
		t,
		sigmatest.Script{Err: errors.New("thinking_level is not supported by this model")},
		sigmatest.Script{},
		sigmatest.Script{Err: errors.New("larger schema failed")},
		sigmatest.Script{Err: errors.New("json object failed")},
		sigmatest.Script{Err: errors.New("manual json failed")},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{repair: true})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if got, want := results[0].Case, "json_schema"; got != want {
		t.Fatalf("case = %q, want %q", got, want)
	}
	if got, want := results[0].Attempt, "json_schema"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if got, want := results[0].Outcome, "sigma_request_shape"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if got, want := results[0].Error, "thinking_level is not supported by this model"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if !results[0].AvailabilityOKAfterFailure {
		t.Fatal("availability check = false, want true")
	}
	if got, want := results[0].Hint, "minimal_text_available_after_failure"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	assertFailedAttempts(t, results[0].FailedAttempts, []failedAttempt{
		{Attempt: "json_schema", Error: "thinking_level is not supported by this model"},
		{Attempt: "json_schema_more_tokens", Error: "larger schema failed"},
		{Attempt: "json_object_fallback", Error: "json object failed"},
		{Attempt: "manual_json", Error: "manual json failed"},
	})
}

func TestProbeModelDoesNotRepairUpstreamAvailability(t *testing.T) {
	t.Parallel()

	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{
		singleTurnCase("basic_text", "plain text", basicRequest("Reply with exactly: ok."), nil),
	}, sigmatest.Script{Err: errors.New("status=429 provider_rate_limit_exceeded: provider rate limit exceeded")})
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{repair: true})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if got, want := results[0].Outcome, "upstream_availability"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if got, want := results[0].Attempt, "basic_text"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if results[0].Hint != "" || len(results[0].FailedAttempts) != 0 {
		t.Fatalf("upstream availability result unexpectedly repaired: %+v", results[0])
	}
}

func TestRunCaseRetriesIdenticalTransportFailure(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxProvider(
		sigmatest.Script{Err: errors.New("status=503 body=upstream connect error: connection refused")},
		sigmatest.Script{Err: errors.New("connection reset by peer")},
		sigmatest.Script{},
	)
	route := routeSpec{
		Name:      "sigmatest-retry",
		Provider:  sigmatest.ProviderID,
		BaseURL:   "https://example.test",
		APIKeyEnv: "SIGMATEST_API_KEY",
		RegisterProvider: func(registry *sigma.Registry, _ routeSpec) error {
			return registry.RegisterTextProvider(sigmatest.ProviderID, provider)
		},
		Model: func(_ routeSpec, id string) sigma.Model {
			model := sigmatest.TextModel()
			model.ID = sigma.ModelID(id)
			return model
		},
	}
	model := route.Model(route, "model")
	testCase := singleTurnCase("basic_text", "plain text", basicRequest("same request"), []sigma.Option{sigma.WithMaxTokens(64)})
	result := runCaseWithTimeout(context.Background(), time.Second, route, probeClient(route, model), model, testCase, routeCredential{apiKey: "key"}, testCase.Name)
	if got, want := result.Outcome, "ok"; got != want {
		t.Fatalf("outcome = %q, want %q: %+v", got, want, result)
	}
	if got, want := len(result.FailedAttempts), 2; got != want {
		t.Fatalf("failed attempts = %#v, want %d transport failures", result.FailedAttempts, want)
	}
	requests := provider.Requests()
	if got, want := len(requests), 3; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
	for i := 1; i < len(requests); i++ {
		if !reflect.DeepEqual(requests[0], requests[i]) {
			t.Fatalf("retry request %d = %#v, want identical to %#v", i, requests[i], requests[0])
		}
	}
}

func TestProbeReportsRepeatedImage503AsUpstreamAvailability(t *testing.T) {
	t.Parallel()

	transportFailure := errors.New("status=503 body=upstream connect error or disconnect/reset before headers: connection refused")
	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{
		singleTurnCase("image_input", "text plus image input", imageRequest(), nil),
	},
		sigmatest.Script{Err: transportFailure},
		sigmatest.Script{Err: transportFailure},
		sigmatest.Script{Err: transportFailure},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{repair: true})
	if got, want := len(results), 1; got != want {
		t.Fatalf("results = %d, want %d", got, want)
	}
	result := results[0]
	if got, want := result.Outcome, "upstream_availability"; got != want {
		t.Fatalf("outcome = %q, want %q: %+v", got, want, result)
	}
	if result.Attempt != "image_input" || result.Hint != "" || result.AvailabilityOKAfterFailure {
		t.Fatalf("result = %+v, must retain the original image attempt without a false repair", result)
	}
	if _, ok := recommendationFor(result); ok {
		t.Fatalf("repeated image 503 produced a recommendation: %+v", result)
	}
}

func TestProbeModelUsesIndependentCaseTimeouts(t *testing.T) {
	t.Parallel()

	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("json schema"), nil),
		singleTurnCase("basic_text", "plain text", basicRequest("ok"), nil),
	},
		sigmatest.Script{WaitForCancel: true},
		sigmatest.Script{},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{
		repair:      true,
		caseTimeout: 10 * time.Millisecond,
	})
	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	if got, want := results[0].Outcome, "upstream_availability"; got != want {
		t.Fatalf("timed-out case outcome = %q, want %q", got, want)
	}
	if got, want := results[1].Outcome, "ok"; got != want {
		t.Fatalf("following case outcome = %q, want %q", got, want)
	}
}

func TestRepairVariantsCoverTargetedFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "basic_text", want: "basic_text_more_tokens"},
		{name: "cache_ephemeral", want: "cache_none_more_tokens"},
		{name: "image_input", want: "image_url_fallback"},
		{name: "thinking_string_none", want: "thinking_object_disabled_repair"},
		{name: "reasoning_effort_high", want: "typed_reasoning_effort_high"},
		{name: "json_schema", want: "json_object_fallback"},
		{name: "logprobs", want: "logprobs_more_tokens"},
		{name: "tool_required_file_read", want: "tool_auto_more_turns"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !hasRepairVariant(repairVariants(routes["zen"], probeCase{Name: tt.name}), tt.want) {
				t.Fatalf("repairVariants(%q) missing %q", tt.name, tt.want)
			}
		})
	}
}

func TestImageRepairVariantsNeverRemoveImageInput(t *testing.T) {
	t.Parallel()

	for _, variant := range repairVariants(routes["fireworks-openai"], probeCase{Name: "image_input"}) {
		if variant.Name == "minimal_basic_text" {
			continue
		}
		for _, message := range variant.Request.Messages {
			for _, block := range message.Content {
				if block.Type == sigma.ContentBlockImage {
					goto nextVariant
				}
			}
		}
		t.Fatalf("image repair %q removed the capability under test", variant.Name)
	nextVariant:
	}
}

func TestProbeDoesNotCallCapabilityRemovingFallbackARepair(t *testing.T) {
	t.Parallel()

	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("json schema"), nil),
	},
		sigmatest.Script{Err: errors.New("unclassified schema failure")},
		sigmatest.Script{},
		sigmatest.Script{Err: errors.New("larger schema failed")},
		sigmatest.Script{},
		sigmatest.Script{},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{repair: true})
	if got, want := results[0].Outcome, "inconclusive"; got != want {
		t.Fatalf("outcome = %q, want %q: %+v", got, want, results[0])
	}
	if results[0].Attempt != "json_schema" {
		t.Fatalf("attempt = %q, want original json_schema", results[0].Attempt)
	}
	if recommendation, ok := recommendationFor(results[0]); ok && strings.Contains(recommendation.Evidence, "repaired") {
		t.Fatalf("recommendation = %+v, must not call JSON object/manual JSON controls repairs", recommendation)
	}
}

func TestLogprobsRepairVariantsIsolateFieldsAndOutputCap(t *testing.T) {
	t.Parallel()

	route := routes["go"]
	variants := repairVariants(route, probeCase{Name: "logprobs"})
	tests := []struct {
		name            string
		maxTokens       int
		wantLogprobs    bool
		wantTopLogprobs int
	}{
		{name: "logprobs_more_tokens", maxTokens: 512, wantLogprobs: true, wantTopLogprobs: 2},
		{name: "no_logprobs", maxTokens: 16},
		{name: "no_logprobs_more_tokens", maxTokens: 512},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			variant := findProbeCase(t, variants, tt.name)
			options := applyProbeOptions(variant.Options)
			if options.MaxTokens == nil || *options.MaxTokens != tt.maxTokens {
				t.Fatalf("max tokens = %v, want %d", options.MaxTokens, tt.maxTokens)
			}

			extraBody, hasExtraBody := options.ProviderOptions[route.Provider]["extra_body"].(map[string]any)
			if hasExtraBody != tt.wantLogprobs {
				t.Fatalf("has logprobs extra body = %v, want %v", hasExtraBody, tt.wantLogprobs)
			}
			if !tt.wantLogprobs {
				return
			}
			if got, want := extraBody["logprobs"], true; got != want {
				t.Fatalf("logprobs = %#v, want %#v", got, want)
			}
			if got, want := extraBody["top_logprobs"], tt.wantTopLogprobs; got != want {
				t.Fatalf("top_logprobs = %#v, want %#v", got, want)
			}
		})
	}
}

func TestProbeModelDiagnosesLogprobsWithIndependentRepairs(t *testing.T) {
	t.Parallel()

	logprobs := findProbeCase(t, openAICompatibleProbeCases(routes["go"], sigma.Model{}), "logprobs")
	tests := []struct {
		name         string
		scripts      []sigmatest.Script
		wantOutcome  string
		wantAttempt  string
		wantHint     string
		wantControls []string
		wantFailures []failedAttempt
	}{
		{
			name:         "larger cap preserves logprobs",
			scripts:      []sigmatest.Script{{Err: errors.New("request failed")}, {}, {}},
			wantOutcome:  "fixed_by_repair_variant",
			wantAttempt:  "logprobs_more_tokens",
			wantHint:     "logprobs_needs_larger_output_budget",
			wantFailures: []failedAttempt{{Attempt: "logprobs", Error: "request failed"}},
		},
		{
			name: "omitting logprobs is only a control",
			scripts: []sigmatest.Script{
				{Err: errors.New("request failed")},
				{},
				{Err: errors.New("larger logprobs request failed")},
				{},
				{Err: errors.New("larger no-logprobs request failed")},
			},
			wantOutcome:  "inconclusive",
			wantAttempt:  "logprobs",
			wantHint:     "minimal_text_available_after_failure",
			wantControls: []string{"no_logprobs"},
			wantFailures: []failedAttempt{
				{Attempt: "logprobs", Error: "request failed"},
				{Attempt: "logprobs_more_tokens", Error: "larger logprobs request failed"},
				{Attempt: "no_logprobs_more_tokens", Error: "larger no-logprobs request failed"},
			},
		},
		{
			name: "omitting logprobs and raising cap is only a control",
			scripts: []sigmatest.Script{
				{Err: errors.New("request failed")},
				{},
				{Err: errors.New("larger logprobs request failed")},
				{Err: errors.New("no logprobs request failed")},
				{},
			},
			wantOutcome:  "inconclusive",
			wantAttempt:  "logprobs",
			wantHint:     "minimal_text_available_after_failure",
			wantControls: []string{"no_logprobs_more_tokens"},
			wantFailures: []failedAttempt{
				{Attempt: "logprobs", Error: "request failed"},
				{Attempt: "logprobs_more_tokens", Error: "larger logprobs request failed"},
				{Attempt: "no_logprobs", Error: "no logprobs request failed"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			route := openAICompatibleSigmatestProbeRoute(t, []probeCase{logprobs}, tt.scripts...)
			results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{repair: true})
			if len(results) != 1 {
				t.Fatalf("results length = %d, want 1", len(results))
			}
			if got, want := results[0].Outcome, tt.wantOutcome; got != want {
				t.Fatalf("outcome = %q, want %q", got, want)
			}
			if got, want := results[0].Attempt, tt.wantAttempt; got != want {
				t.Fatalf("attempt = %q, want %q", got, want)
			}
			if got, want := results[0].Hint, tt.wantHint; got != want {
				t.Fatalf("hint = %q, want %q", got, want)
			}
			if got, want := results[0].SuccessfulControls, tt.wantControls; !reflect.DeepEqual(got, want) {
				t.Fatalf("successful controls = %v, want %v", got, want)
			}
			assertFailedAttempts(t, results[0].FailedAttempts, tt.wantFailures)
		})
	}
}

func TestImageRepairVariantsPreserveImageInput(t *testing.T) {
	t.Parallel()

	variants := repairVariants(routes["xai"], probeCase{Name: "image_input"})
	if !hasRepairVariant(variants, "image_url_fallback") {
		t.Fatal("image_url_fallback missing from image repair variants")
	}
	if hasRepairVariant(variants, "text_only_fallback") {
		t.Fatal("text_only_fallback must be an availability control, not an image repair")
	}
}

func TestClassifyFailure(t *testing.T) {
	t.Parallel()

	route := routes["zen"]
	model := sigma.Model{Provider: sigma.ProviderOpenCode, ID: "gpt-5.1-codex"}
	if got := classifyFailure(route, model, errors.New("unknown parameter: 'thinking'")); got != "sigma_request_shape" {
		t.Fatalf("unknown parameter classification = %q", got)
	}
	if got := classifyFailure(routes["openai-codex"], model, errors.New("status=400 body={\"detail\":\"Unsupported parameter: max_output_tokens\"}")); got != "sigma_request_shape" {
		t.Fatalf("unsupported-parameter classification = %q", got)
	}
	if got := classifyFailure(routes["openai-codex"], model, errors.New("status=400 body={\"detail\":\"Instructions are required\"}")); got != "sigma_request_shape" {
		t.Fatalf("instructions-required classification = %q", got)
	}
	if got := classifyFailure(routes["openai-codex"], model, errors.New("status=400 body={\"detail\":\"Store must be set to false\"}")); got != "sigma_request_shape" {
		t.Fatalf("store-false classification = %q", got)
	}
	if got := classifyFailure(routes["google-vertex"], model, errors.New("thinking_level is not supported by this model")); got != "sigma_request_shape" {
		t.Fatalf("unsupported-thinking-level classification = %q", got)
	}
	if got := classifyFailure(route, model, errors.New("model does not support image input")); got != "provider_capability_limit" {
		t.Fatalf("image classification = %q", got)
	}
	if got := classifyFailure(route, model, errors.New("status=429 provider_rate_limit_exceeded: provider rate limit exceeded")); got != "upstream_availability" {
		t.Fatalf("rate-limit classification = %q", got)
	}
	if got := classifyFailure(route, model, errors.New("status=503 body=upstream connect error: connection refused")); got != "upstream_availability" {
		t.Fatalf("503 connection-refused classification = %q", got)
	}
	regionErr := &sigma.ProviderError{StatusCode: http.StatusForbidden, ProviderCode: "RegionError", ProviderMessage: "model requires explicit China hosting opt in"}
	if got := classifyFailure(routes["go"], model, regionErr); got != "upstream_availability" {
		t.Fatalf("region-error classification = %q", got)
	}
	if got := classifyFailure(route, model, errors.New("unexpected provider response")); got != "inconclusive" {
		t.Fatalf("ambiguous failure classification = %q", got)
	}
	model.ID = "claude-opus-4-6"
	if got := classifyFailure(route, model, errors.New("No provider available")); got != "upstream_availability" {
		t.Fatalf("availability classification = %q", got)
	}
	model.ID = "accounts/fireworks/routers/kimi-k2p6-turbo"
	if got := classifyFailure(routes["fireworks-anthropic"], model, errors.New("status=404 body={\"error\":{\"code\":\"NOT_FOUND\",\"message\":\"Path not found: /messages\"}}")); got != "upstream_availability" {
		t.Fatalf("path-not-found classification = %q", got)
	}
	if got := classifyFailure(routes[routeGoogleVertex], model, errors.New("status=404 provider_code=NOT_FOUND provider_message=Publisher model was not found")); got != "upstream_availability" {
		t.Fatalf("publisher-model-not-found classification = %q", got)
	}
	model.ID = "gpt-5.1-codex"
	if got := classifyFailure(routes["openai-codex"], model, errors.New("status=400 body={\"detail\":\"The 'gpt-5.1-codex' model is not supported when using Codex with a ChatGPT account.\"}")); got != "upstream_availability" {
		t.Fatalf("chatgpt-account-unsupported classification = %q", got)
	}
}

func TestSummaryCounts(t *testing.T) {
	t.Parallel()

	var totals summary
	for _, outcome := range []string{
		"ok",
		"skipped",
		"sigma_request_shape",
		"provider_capability_limit",
		"upstream_availability",
		"inconclusive",
		"fixed_by_repair_variant",
		"other",
	} {
		totals.add(probeResult{Outcome: outcome})
	}
	totals.add(probeResult{Outcome: "sigma_request_shape", AvailabilityOKAfterFailure: true})
	if totals.Total != 9 || totals.OK != 1 || totals.Skipped != 1 ||
		totals.SigmaRequestShape != 2 || totals.ProviderCapabilityLimit != 1 ||
		totals.UpstreamAvailability != 1 || totals.Inconclusive != 1 || totals.FixedByRepairVariant != 1 ||
		totals.AvailabilityOKAfterFailure != 1 ||
		totals.NoWorkingAttempt != 1 {
		t.Fatalf("summary = %+v", totals)
	}
}

func TestParseModelIDs(t *testing.T) {
	t.Parallel()

	ids, err := parseModelIDs([]byte(`{"data":[{"id":"b"},{"id":"a"}]}`))
	if err != nil {
		t.Fatalf("parseModelIDs returned error: %v", err)
	}
	if got, want := ids[0], "a"; got != want {
		t.Fatalf("first id = %q, want %q", got, want)
	}
	if got, want := ids[1], "b"; got != want {
		t.Fatalf("second id = %q, want %q", got, want)
	}
}

func hasRepairVariant(variants []probeCase, name string) bool {
	for _, variant := range variants {
		if variant.Name == name {
			return true
		}
	}
	return false
}

func probeCaseNames(cases []probeCase) []string {
	names := make([]string, 0, len(cases))
	for _, testCase := range cases {
		names = append(names, testCase.Name)
	}
	return names
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func clearGoogleVertexEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"GOOGLE_CLOUD_PROJECT",
		"GCLOUD_PROJECT",
		"GOOGLE_CLOUD_LOCATION",
		"GOOGLE_CLOUD_REGION",
		"GOOGLE_CLOUD_ACCESS_TOKEN",
		"GOOGLE_CLOUD_API_KEY",
		"GOOGLE_API_KEY",
	} {
		t.Setenv(name, "")
	}
}

func findProbeCase(t *testing.T, cases []probeCase, name string) probeCase {
	t.Helper()

	for _, testCase := range cases {
		if testCase.Name == name {
			return testCase
		}
	}
	t.Fatalf("probe case %q not found", name)
	return probeCase{}
}

func findImageProbeCase(t *testing.T, cases []imageProbeCase, name string) imageProbeCase {
	t.Helper()

	for _, testCase := range cases {
		if testCase.Name == name {
			return testCase
		}
	}
	t.Fatalf("image probe case %q not found", name)
	return imageProbeCase{}
}

type imageProbeRequestRecord struct {
	Path     string
	Model    string
	HasImage bool
	Stream   bool
}

func captureImageProbeRequest(t *testing.T, r *http.Request) imageProbeRequestRecord {
	t.Helper()

	record := imageProbeRequestRecord{Path: r.URL.Path}
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		record.Model = r.FormValue("model")
		record.HasImage = len(r.MultipartForm.File["image"]) > 0
		record.Stream = r.FormValue("stream") == "true"
		return record
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	record.Model, _ = payload["model"].(string)
	record.Stream, _ = payload["stream"].(bool)
	_, record.HasImage = payload["images"]
	return record
}

func assertImageProbeRecord(t *testing.T, record imageProbeRequestRecord, path string, model string, hasImage bool, stream bool) {
	t.Helper()

	if record.Path != path {
		t.Fatalf("path = %q, want %q (record %#v)", record.Path, path, record)
	}
	if record.Model != model {
		t.Fatalf("model = %q, want %q (record %#v)", record.Model, model, record)
	}
	if record.HasImage != hasImage {
		t.Fatalf("hasImage = %v, want %v (record %#v)", record.HasImage, hasImage, record)
	}
	if record.Stream != stream {
		t.Fatalf("stream = %v, want %v (record %#v)", record.Stream, stream, record)
	}
}

func applyProbeOptions(opts []sigma.Option) sigma.Options {
	var options sigma.Options
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

func applyImageProbeOptions(opts []sigma.ImageOption) sigma.Options {
	var options sigma.Options
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

func assertFailedAttempts(t *testing.T, got []failedAttempt, want []failedAttempt) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("failed attempts = %#v, want %#v", got, want)
	}
}

func sigmatestProbeRoute(t *testing.T, scripts ...sigmatest.Script) routeSpec {
	t.Helper()

	return sigmatestProbeRouteWithCases(t, []probeCase{
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("Return JSON exactly {\"answer\":\"ok\"}."), nil),
	}, scripts...)
}

func openAICompatibleSigmatestProbeRoute(t *testing.T, cases []probeCase, scripts ...sigmatest.Script) routeSpec {
	t.Helper()

	provider := openAICompatibleFauxProvider{sigmatest.NewFauxProvider(scripts...)}
	return routeSpec{
		Name:      "sigmatest-openai",
		Provider:  sigmatest.ProviderID,
		BaseURL:   "https://example.test",
		APIKeyEnv: "SIGMATEST_API_KEY",
		RegisterProvider: func(registry *sigma.Registry, _ routeSpec) error {
			return registry.RegisterTextProvider(sigmatest.ProviderID, provider)
		},
		Model: func(_ routeSpec, id string) sigma.Model {
			model := sigmatest.TextModel()
			model.ID = sigma.ModelID(id)
			model.API = sigma.APIOpenAICompletions
			return model
		},
		Cases: func(routeSpec, sigma.Model) []probeCase {
			return cases
		},
	}
}

type openAICompatibleFauxProvider struct {
	*sigmatest.FauxProvider
}

func (p openAICompatibleFauxProvider) API() sigma.API {
	return sigma.APIOpenAICompletions
}

func sigmatestProbeRouteWithCases(t *testing.T, cases []probeCase, scripts ...sigmatest.Script) routeSpec {
	t.Helper()

	provider := sigmatest.NewFauxProvider(scripts...)
	return routeSpec{
		Name:      "sigmatest",
		Provider:  sigmatest.ProviderID,
		BaseURL:   "https://example.test",
		APIKeyEnv: "SIGMATEST_API_KEY",
		RegisterProvider: func(registry *sigma.Registry, _ routeSpec) error {
			return registry.RegisterTextProvider(sigmatest.ProviderID, provider)
		},
		Model: func(_ routeSpec, id string) sigma.Model {
			model := sigmatest.TextModel()
			model.ID = sigma.ModelID(id)
			return model
		},
		Cases: func(routeSpec, sigma.Model) []probeCase {
			return cases
		},
	}
}

func handoffProbeRoute(t *testing.T, name string, scripts ...sigmatest.Script) routeSpec {
	t.Helper()

	providerID := sigma.ProviderID(name)
	provider := sigmatest.NewFauxProvider(scripts...)
	return routeSpec{
		Name:      name,
		Provider:  providerID,
		BaseURL:   "https://example.test",
		APIKeyEnv: "SIGMATEST_API_KEY",
		RegisterProvider: func(registry *sigma.Registry, _ routeSpec) error {
			return registry.RegisterTextProvider(providerID, provider)
		},
		Model: func(route routeSpec, id string) sigma.Model {
			model := sigmatest.TextModel()
			model.ID = sigma.ModelID(id)
			model.Provider = route.Provider
			return model
		},
		Cases: func(routeSpec, sigma.Model) []probeCase {
			return nil
		},
	}
}

func assertMetadataString(t *testing.T, metadata map[string]any, key string, want string) {
	t.Helper()

	if got, ok := metadata[key].(string); !ok || got != want {
		t.Fatalf("metadata[%q] = %#v, want %q", key, metadata[key], want)
	}
}

func assertMetadataStrings(t *testing.T, metadata map[string]any, key string, want []string) {
	t.Helper()

	got, ok := metadata[key].([]string)
	if !ok {
		t.Fatalf("metadata[%q] type = %T, want []string", key, metadata[key])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata[%q] = %v, want %v", key, got, want)
	}
}
