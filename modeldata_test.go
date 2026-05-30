// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import "testing"

func TestGeneratedModelMetadataRegistersIntoFreshRegistry(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatalf("registerBuiltinTextModels returned error: %v", err)
	}
	if err := registerBuiltinImageModels(registry); err != nil {
		t.Fatalf("registerBuiltinImageModels returned error: %v", err)
	}

	openAI, ok := registry.Model(ProviderOpenAI, "gpt-4o-mini")
	if !ok {
		t.Fatal("fresh registry missing generated OpenAI text model")
	}
	if openAI.API != APIOpenAIResponses {
		t.Fatalf("OpenAI model API = %q, want %q", openAI.API, APIOpenAIResponses)
	}
	if openAI.InputCostPerMillion == 0 || openAI.OutputCostPerMillion == 0 {
		t.Fatalf("OpenAI model cost fields were not generated: %+v", openAI)
	}
	if !openAI.SupportsTools || !openAI.SupportsImages() {
		t.Fatalf("OpenAI model capabilities were not generated: %+v", openAI)
	}
	assertMetadataString(t, openAI.ProviderMetadata, "baseURL", "https://api.openai.com/v1")
	assertMetadataStrings(t, openAI.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENAI_API_KEY"})

	fireworks, ok := registry.Model(ProviderFireworks, "accounts/fireworks/routers/kimi-k2p6-turbo")
	if !ok {
		t.Fatal("fresh registry missing generated Fireworks Fire Pass model")
	}
	if fireworks.API != APIOpenAICompletions {
		t.Fatalf("Fireworks model API = %q, want %q", fireworks.API, APIOpenAICompletions)
	}
	if !fireworks.SupportsTools || !fireworks.SupportsImages() {
		t.Fatalf("Fireworks model capabilities were not generated: %+v", fireworks)
	}
	if !fireworks.SupportsReasoning() || !fireworks.SupportsThinkingLevel(ThinkingLevelMedium) {
		t.Fatalf("Fireworks reasoning metadata was not generated: %+v", fireworks)
	}
	if fireworks.OpenAICompletionsCompat == nil ||
		fireworks.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningFireworks {
		t.Fatalf("Fireworks reasoning compat = %#v, want fireworks format", fireworks.OpenAICompletionsCompat)
	}
	if fireworks.InputCostPerMillion != 0 || fireworks.OutputCostPerMillion != 0 {
		t.Fatalf("Fireworks Fire Pass costs = input %v output %v, want zero", fireworks.InputCostPerMillion, fireworks.OutputCostPerMillion)
	}
	if got, ok := fireworks.ProviderMetadata["firepass"].(bool); !ok || !got {
		t.Fatalf("Fireworks firepass metadata = %#v, want true", fireworks.ProviderMetadata["firepass"])
	}
	assertMetadataString(t, fireworks.ProviderMetadata, "baseURL", "https://api.fireworks.ai/inference/v1")
	assertMetadataStrings(t, fireworks.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"FIREWORKS_API_KEY"})

	anthropic, ok := registry.Model(ProviderAnthropic, "claude-3-5-sonnet-20241022")
	if !ok {
		t.Fatal("fresh registry missing generated Anthropic text model")
	}
	if anthropic.ContextWindow == 0 || anthropic.MaxOutputTokens == 0 {
		t.Fatalf("Anthropic limits were not generated: %+v", anthropic)
	}
	headers, ok := anthropic.ProviderMetadata["headers"].(map[string]string)
	if !ok {
		t.Fatalf("Anthropic headers metadata type = %T, want map[string]string", anthropic.ProviderMetadata["headers"])
	}
	if got, want := headers["anthropic-version"], "2023-06-01"; got != want {
		t.Fatalf("Anthropic version header = %q, want %q", got, want)
	}
	if anthropic.AnthropicMessagesCompat == nil {
		t.Fatal("Anthropic model missing compatibility metadata")
	}
	if got, want := anthropic.AnthropicMessagesCompat.SupportsEagerToolInputStreaming, AnthropicCompatSupported; got != want {
		t.Fatalf("Anthropic eager streaming compat = %q, want %q", got, want)
	}
	if got, want := anthropic.AnthropicMessagesCompat.ThinkingFormat, AnthropicThinkingBudget; got != want {
		t.Fatalf("Anthropic thinking format = %q, want %q", got, want)
	}

	reasoning, ok := registry.Model(ProviderOpenAI, "o4-mini")
	if !ok {
		t.Fatal("fresh registry missing generated reasoning model")
	}
	if !reasoning.SupportsReasoning() {
		t.Fatal("reasoning model did not advertise reasoning support")
	}
	if got, ok := reasoning.ProviderThinkingLevel(ThinkingLevelHigh); !ok || got != "high" {
		t.Fatalf("reasoning high level = %q, %v; want high, true", got, ok)
	}

	routed, ok := registry.Model(ProviderOpenRouter, "openai/gpt-4o-mini")
	if !ok {
		t.Fatal("fresh registry missing generated OpenRouter model")
	}
	if routed.OpenAICompletionsCompat == nil {
		t.Fatal("OpenRouter model missing compatibility metadata")
	}
	if routed.OpenAICompletionsCompat.SupportsStreamingUsage != OpenAICompatSupported {
		t.Fatalf("OpenRouter streaming usage compat = %q", routed.OpenAICompletionsCompat.SupportsStreamingUsage)
	}
	if routed.OpenAICompletionsCompat.OpenRouterRouting == nil ||
		routed.OpenAICompletionsCompat.OpenRouterRouting.AllowFallbacks == nil ||
		!*routed.OpenAICompletionsCompat.OpenRouterRouting.AllowFallbacks {
		t.Fatal("OpenRouter routing fallback metadata was not generated")
	}

	openCode, ok := registry.Model(ProviderOpenCode, "kimi-k2.6")
	if !ok {
		t.Fatal("fresh registry missing generated OpenCode Zen model")
	}
	if openCode.API != APIOpenAICompletions {
		t.Fatalf("OpenCode Zen model API = %q, want %q", openCode.API, APIOpenAICompletions)
	}
	if !openCode.SupportsTools || !openCode.SupportsImages() || !openCode.SupportsReasoning() {
		t.Fatalf("OpenCode Zen model capabilities were not generated: %+v", openCode)
	}
	if openCode.OpenAICompletionsCompat == nil ||
		openCode.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
		openCode.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported {
		t.Fatalf("OpenCode Zen Kimi compat = %#v, want deepseek reasoning without effort", openCode.OpenAICompletionsCompat)
	}
	assertMetadataString(t, openCode.ProviderMetadata, "baseURL", "https://opencode.ai/zen/v1")
	assertMetadataStrings(t, openCode.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENCODE_API_KEY"})

	grokBuild, ok := registry.Model(ProviderOpenCode, "grok-build-0.1")
	if !ok {
		t.Fatal("fresh registry missing generated OpenCode Zen Grok Build model")
	}
	if grokBuild.API != APIOpenAICompletions || !grokBuild.SupportsTools || !grokBuild.SupportsImages() || !grokBuild.SupportsReasoning() {
		t.Fatalf("OpenCode Zen Grok Build model was not generated as an image-capable completions model: %+v", grokBuild)
	}
	if grokBuild.OpenAICompletionsCompat == nil ||
		grokBuild.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported {
		t.Fatalf("OpenCode Zen Grok Build compat = %#v, want no reasoning effort", grokBuild.OpenAICompletionsCompat)
	}

	openCodeGo, ok := registry.Model(ProviderOpenCodeGo, "deepseek-v4-flash")
	if !ok {
		t.Fatal("fresh registry missing generated OpenCode Go model")
	}
	if openCodeGo.API != APIOpenAICompletions {
		t.Fatalf("OpenCode Go model API = %q, want %q", openCodeGo.API, APIOpenAICompletions)
	}
	if openCodeGo.OpenAICompletionsCompat == nil ||
		openCodeGo.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
		openCodeGo.OpenAICompletionsCompat.RequiresReasoningContentOnAssistantMessages != OpenAICompatSupported {
		t.Fatalf("OpenCode Go DeepSeek compat = %#v, want deepseek reasoning content replay", openCodeGo.OpenAICompletionsCompat)
	}
	if got, ok := openCodeGo.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "max" {
		t.Fatalf("OpenCode Go xhigh level = %q, %v; want max, true", got, ok)
	}
	assertMetadataString(t, openCodeGo.ProviderMetadata, "baseURL", "https://opencode.ai/zen/go/v1")
	assertMetadataStrings(t, openCodeGo.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENCODE_API_KEY"})

	openCodeGoKimi, ok := registry.Model(ProviderOpenCodeGo, "kimi-k2.6")
	if !ok {
		t.Fatal("fresh registry missing generated OpenCode Go Kimi model")
	}
	if openCodeGoKimi.OpenAICompletionsCompat == nil ||
		openCodeGoKimi.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
		openCodeGoKimi.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported {
		t.Fatalf("OpenCode Go Kimi compat = %#v, want deepseek reasoning without effort", openCodeGoKimi.OpenAICompletionsCompat)
	}

	image, ok := registry.ImageModel(ProviderOpenAI, "gpt-image-1")
	if !ok {
		t.Fatal("fresh registry missing generated OpenAI image model")
	}
	if image.API != ImageAPIOpenAIImages {
		t.Fatalf("image model API = %q, want %q", image.API, ImageAPIOpenAIImages)
	}
	if image.MaxWidth == 0 || image.MaxHeight == 0 || len(image.SupportedFormats) == 0 {
		t.Fatalf("image output capabilities were not generated: %+v", image)
	}
	if cost, ok := image.ProviderMetadata["cost"].(map[string]any); !ok || cost["currency"] != "USD" {
		t.Fatalf("image cost metadata = %#v, want USD cost map", image.ProviderMetadata["cost"])
	}
}

func TestGeneratedModelMetadataOrder(t *testing.T) {
	t.Parallel()

	assertSortedModelOrder(t, builtinTextModels)
	assertSortedImageModelOrder(t, builtinImageModels)
}

func assertSortedModelOrder(t *testing.T, models []Model) {
	t.Helper()
	for i := 1; i < len(models); i++ {
		if modelOrderKey(models[i-1].Provider, string(models[i-1].API), models[i-1].ID) > modelOrderKey(models[i].Provider, string(models[i].API), models[i].ID) {
			t.Fatalf("text models are not sorted at %d: %s before %s", i, models[i-1].ID, models[i].ID)
		}
	}
}

func assertSortedImageModelOrder(t *testing.T, models []ImageModel) {
	t.Helper()
	for i := 1; i < len(models); i++ {
		if modelOrderKey(models[i-1].Provider, string(models[i-1].API), models[i-1].ID) > modelOrderKey(models[i].Provider, string(models[i].API), models[i].ID) {
			t.Fatalf("image models are not sorted at %d: %s before %s", i, models[i-1].ID, models[i].ID)
		}
	}
}

func modelOrderKey(provider ProviderID, api string, id ModelID) string {
	return string(provider) + "\x00" + api + "\x00" + string(id)
}

func assertMetadataString(t *testing.T, metadata map[string]any, key string, want string) {
	t.Helper()
	got, ok := metadata[key].(string)
	if !ok {
		t.Fatalf("metadata %q type = %T, want string", key, metadata[key])
	}
	if got != want {
		t.Fatalf("metadata %q = %q, want %q", key, got, want)
	}
}

func assertMetadataStrings(t *testing.T, metadata map[string]any, key string, want []string) {
	t.Helper()
	got, ok := metadata[key].([]string)
	if !ok {
		t.Fatalf("metadata %q type = %T, want []string", key, metadata[key])
	}
	if len(got) != len(want) {
		t.Fatalf("metadata %q length = %d, want %d", key, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("metadata %q[%d] = %q, want %q", key, i, got[i], want[i])
		}
	}
}
