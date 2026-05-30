// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

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
		{route: "zen", id: "kimi-k2.6", want: sigma.APIOpenAICompletions},
		{route: "go", id: "qwen3.7-max", want: sigma.APIAnthropicMessages},
		{route: "go", id: "minimax-m2.5", want: sigma.APIAnthropicMessages},
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

	anthropic := routes["fireworks-anthropic"].Model(routes["fireworks-anthropic"], "accounts/fireworks/models/kimi-k2p6")
	if anthropic.Provider != sigma.ProviderFireworks || anthropic.API != sigma.APIAnthropicMessages {
		t.Fatalf("fireworks-anthropic model provider/API = %q/%q", anthropic.Provider, anthropic.API)
	}
	if anthropic.AnthropicMessagesCompat == nil ||
		anthropic.AnthropicMessagesCompat.SupportsSessionAffinity != sigma.AnthropicCompatSupported ||
		anthropic.AnthropicMessagesCompat.SupportsEagerToolInputStreaming != sigma.AnthropicCompatUnsupported ||
		anthropic.AnthropicMessagesCompat.SupportsLongCacheRetention != sigma.AnthropicCompatUnsupported ||
		anthropic.AnthropicMessagesCompat.SupportsCacheControlOnTools != sigma.AnthropicCompatUnsupported {
		t.Fatalf("fireworks-anthropic compat = %#v, want Fireworks Anthropic compat", anthropic.AnthropicMessagesCompat)
	}
	assertMetadataString(t, anthropic.ProviderMetadata, "baseURL", "https://api.fireworks.ai/inference")
	assertMetadataStrings(t, anthropic.ProviderMetadata, "apiKeyEnvVars", []string{"FIREWORKS_API_KEY"})
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

func TestModelsForRouteUsesSelectedModelsWithoutDiscovery(t *testing.T) {
	t.Parallel()

	models, err := modelsForRoute(context.Background(), routes["fireworks-anthropic"], "key", map[string]bool{
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

func TestOpenAICompatibleProbeCasesUseRouteProviderOptions(t *testing.T) {
	t.Parallel()

	testCase := findProbeCase(t, openAICompatibleProbeCases(routes["fireworks-openai"]), "json_object")
	options := applyProbeOptions(testCase.Options)
	if _, ok := options.ProviderOptions[sigma.ProviderFireworks]["extra_body"]; !ok {
		t.Fatalf("fireworks provider options = %#v, want extra_body", options.ProviderOptions[sigma.ProviderFireworks])
	}
	if _, ok := options.ProviderOptions[sigma.ProviderOpenCode]; ok {
		t.Fatalf("unexpected OpenCode provider options: %#v", options.ProviderOptions[sigma.ProviderOpenCode])
	}
}

func TestXAIProbeCasesUseRouteProviderOptions(t *testing.T) {
	t.Parallel()

	testCase := findProbeCase(t, openAICompatibleProbeCases(routes["xai"]), "json_object")
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

func TestAnthropicProbeCasesDoNotSendRawOpenAIExtraBody(t *testing.T) {
	t.Parallel()

	for _, testCase := range anthropicCompatibleProbeCases(routes["fireworks-anthropic"]) {
		options := applyProbeOptions(testCase.Options)
		if providerOptions := options.ProviderOptions[sigma.ProviderFireworks]; providerOptions != nil {
			if _, ok := providerOptions["extra_body"]; ok {
				t.Fatalf("%s set raw extra_body for Anthropic route: %#v", testCase.Name, providerOptions)
			}
		}
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
		result := runCase(context.Background(), route, client, model, singleTurnCase("basic", "", basicRequest("hi"), nil), "key", "basic")
		if result.Route != routeName {
			t.Fatalf("route = %q, want %q", result.Route, routeName)
		}
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
		{name: "image_input", want: "text_only_fallback"},
		{name: "thinking_string_none", want: "thinking_object_disabled_repair"},
		{name: "reasoning_effort_high", want: "typed_reasoning_effort_high"},
		{name: "json_schema", want: "json_object_fallback"},
		{name: "logprobs", want: "no_logprobs_more_tokens"},
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

func TestClassifyFailure(t *testing.T) {
	t.Parallel()

	route := routes["zen"]
	model := sigma.Model{Provider: sigma.ProviderOpenCode, ID: "gpt-5.1-codex"}
	if got := classifyFailure(route, model, errors.New("unknown parameter: 'thinking'")); got != "sigma_request_shape" {
		t.Fatalf("unknown parameter classification = %q", got)
	}
	if got := classifyFailure(route, model, errors.New("model does not support image input")); got != "provider_capability_limit" {
		t.Fatalf("image classification = %q", got)
	}
	model.ID = "claude-opus-4-6"
	if got := classifyFailure(route, model, errors.New("No provider available")); got != "upstream_availability" {
		t.Fatalf("availability classification = %q", got)
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
		"fixed_by_repair_variant",
		"other",
	} {
		totals.add(probeResult{Outcome: outcome})
	}
	if totals.Total != 7 || totals.OK != 1 || totals.Skipped != 1 ||
		totals.SigmaRequestShape != 1 || totals.ProviderCapabilityLimit != 1 ||
		totals.UpstreamAvailability != 1 || totals.FixedByRepairVariant != 1 ||
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

func applyProbeOptions(opts []sigma.Option) sigma.Options {
	var options sigma.Options
	for _, opt := range opts {
		opt(&options)
	}
	return options
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
