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
	assertMetadataString(t, fireworks.ProviderMetadata, "disabledThinkingFormat", "object-disabled")
	assertMetadataStrings(t, fireworks.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"FIREWORKS_API_KEY"})
	assertMetadataStrings(t, fireworks.ProviderMetadata, "imageInputSources", []string{"url"})

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
	haiku, ok := registry.Model(ProviderAnthropic, "claude-haiku-4-5")
	if !ok {
		t.Fatal("fresh registry missing generated Claude Haiku 4.5 model")
	}
	if !haiku.SupportsReasoning() || haiku.ContextWindow != 200000 || haiku.MaxOutputTokens != 64000 {
		t.Fatalf("Claude Haiku 4.5 metadata = %+v, want reasoning with current limits", haiku)
	}
	fable, ok := registry.Model(ProviderAnthropic, "claude-fable-5")
	if !ok {
		t.Fatal("fresh registry missing generated Claude Fable 5 model")
	}
	if fable.API != APIAnthropicMessages || !fable.SupportsTools || !fable.SupportsImages() || !fable.SupportsReasoning() {
		t.Fatalf("Claude Fable 5 metadata = %+v, want Anthropic Messages with tools, images, and reasoning", fable)
	}
	if fable.AnthropicMessagesCompat == nil || fable.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive {
		t.Fatalf("Claude Fable 5 compat = %#v, want adaptive thinking", fable.AnthropicMessagesCompat)
	}
	if fable.AnthropicMessagesCompat.SupportsDisabledThinking != AnthropicCompatUnsupported {
		t.Fatalf("Claude Fable 5 disabled thinking = %q, want unsupported", fable.AnthropicMessagesCompat.SupportsDisabledThinking)
	}
	if got, ok := fable.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "xhigh" {
		t.Fatalf("Claude Fable 5 xhigh level = %q, %v; want xhigh, true", got, ok)
	}
	if fable.ContextWindow != 1000000 || fable.MaxOutputTokens != 128000 {
		t.Fatalf("Claude Fable 5 limits = context %d max %d, want 1000000/128000", fable.ContextWindow, fable.MaxOutputTokens)
	}
	assertMetadataStrings(t, fable.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"ANTHROPIC_API_KEY"})
	opus, ok := registry.Model(ProviderAnthropic, "claude-opus-4-8")
	if !ok {
		t.Fatal("fresh registry missing generated Claude Opus 4.8 model")
	}
	if opus.AnthropicMessagesCompat == nil || opus.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive {
		t.Fatalf("Claude Opus 4.8 compat = %#v, want adaptive thinking", opus.AnthropicMessagesCompat)
	}
	if got, ok := opus.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "xhigh" {
		t.Fatalf("Claude Opus 4.8 xhigh level = %q, %v; want xhigh, true", got, ok)
	}
	if opus.ContextWindow != 1000000 || opus.MaxOutputTokens != 128000 {
		t.Fatalf("Claude Opus 4.8 limits = context %d max %d, want 1000000/128000", opus.ContextWindow, opus.MaxOutputTokens)
	}
	sonnet, ok := registry.Model(ProviderAnthropic, "claude-sonnet-4-6")
	if !ok {
		t.Fatal("fresh registry missing generated Claude Sonnet 4.6 model")
	}
	if sonnet.AnthropicMessagesCompat == nil || sonnet.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive {
		t.Fatalf("Claude Sonnet 4.6 compat = %#v, want adaptive thinking", sonnet.AnthropicMessagesCompat)
	}
	assertMetadataStrings(t, opus.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"ANTHROPIC_API_KEY"})

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
	gpt54, ok := registry.Model(ProviderOpenAI, "gpt-5.4")
	if !ok {
		t.Fatal("fresh registry missing generated GPT-5.4 model")
	}
	if gpt54.API != APIOpenAIResponses || !gpt54.SupportsTools || !gpt54.SupportsImages() || !gpt54.SupportsReasoning() {
		t.Fatalf("GPT-5.4 metadata = %+v, want Responses with tools, images, and reasoning", gpt54)
	}
	if got, ok := gpt54.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "xhigh" {
		t.Fatalf("GPT-5.4 xhigh level = %q, %v; want xhigh, true", got, ok)
	}

	googleFlash, ok := registry.Model(ProviderGoogle, "gemini-3.5-flash")
	if !ok {
		t.Fatal("fresh registry missing generated Gemini 3.5 Flash model")
	}
	if googleFlash.API != APIGoogleGenerativeAI || !googleFlash.SupportsTools || !googleFlash.SupportsImages() || !googleFlash.SupportsReasoning() {
		t.Fatalf("Gemini 3.5 Flash metadata = %+v, want Generative AI tools, images, and reasoning", googleFlash)
	}

	mistralSmall, ok := registry.Model(ProviderMistral, "mistral-small-latest")
	if !ok {
		t.Fatal("fresh registry missing generated Mistral Small model")
	}
	if mistralSmall.API != APIMistralConversations || !mistralSmall.SupportsTools || !mistralSmall.SupportsReasoning() {
		t.Fatalf("Mistral Small metadata = %+v, want conversations tools and reasoning", mistralSmall)
	}
	if got, ok := mistralSmall.ProviderThinkingLevel(ThinkingLevelMedium); !ok || got != "high" {
		t.Fatalf("Mistral Small medium level = %q, %v; want high, true", got, ok)
	}
	assertMetadataString(t, mistralSmall.ProviderMetadata, "mistral_reasoning_mode", "reasoning_effort")
	assertMetadataStrings(t, mistralSmall.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"MISTRAL_API_KEY"})
	magistral, ok := registry.Model(ProviderMistral, "magistral-medium-latest")
	if !ok {
		t.Fatal("fresh registry missing generated Magistral model")
	}
	if magistral.API != APIMistralConversations || !magistral.SupportsTools || !magistral.SupportsReasoning() {
		t.Fatalf("Magistral metadata = %+v, want conversations tools and reasoning", magistral)
	}
	assertMetadataString(t, magistral.ProviderMetadata, "mistral_reasoning_mode", "prompt_mode")
	mistralMedium, ok := registry.Model(ProviderMistral, "mistral-medium-2604")
	if !ok {
		t.Fatal("fresh registry missing generated Mistral Medium 3.5 model")
	}
	if mistralMedium.API != APIMistralConversations || !mistralMedium.SupportsTools || mistralMedium.SupportsImages() || !mistralMedium.SupportsReasoning() {
		t.Fatalf("Mistral Medium 3.5 metadata = %+v, want text-only conversations tools and reasoning", mistralMedium)
	}
	assertMetadataString(t, mistralMedium.ProviderMetadata, "mistral_reasoning_mode", "reasoning_effort")

	nova, ok := registry.Model(ProviderAmazonBedrock, "amazon.nova-2-lite-v1:0")
	if !ok {
		t.Fatal("fresh registry missing generated Bedrock Nova 2 Lite model")
	}
	if nova.API != APIBedrockConverseStream || !nova.SupportsTools || !nova.SupportsImages() || nova.SupportsReasoning() {
		t.Fatalf("Bedrock Nova 2 Lite metadata = %+v, want Converse Stream tools and images without reasoning", nova)
	}
	assertMetadataString(t, nova.ProviderMetadata, "modelFamily", "nova")

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
	if got := routed.OpenAICompletionsCompat.OpenRouterRouting.Quantizations; len(got) != 2 || got[0] != "fp16" || got[1] != "int8" {
		t.Fatalf("OpenRouter routing quantizations = %#v, want [fp16 int8]", got)
	}
	if got := routed.OpenAICompletionsCompat.OpenRouterRouting.MaxPrice["completion"]; got != "2.5" {
		t.Fatalf("OpenRouter routing max price completion = %#v, want 2.5", got)
	}
	sort, ok := routed.OpenAICompletionsCompat.OpenRouterRouting.Sort.(map[string]any)
	if !ok || sort["by"] != "latency" {
		t.Fatalf("OpenRouter routing sort = %#v, want latency object", routed.OpenAICompletionsCompat.OpenRouterRouting.Sort)
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
	assertOpenCodeAPI(t, registry, ProviderOpenCode, "gemini-3-flash", APIGoogleGenerativeAI)
	assertOpenCodeAPI(t, registry, ProviderOpenCode, "claude-opus-4-7", APIAnthropicMessages)
	assertOpenCodeAPI(t, registry, ProviderOpenCode, "qwen3.6-plus", APIAnthropicMessages)
	assertOpenCodeAPI(t, registry, ProviderOpenCode, "gpt-5.1-codex", APIOpenAIResponses)
	assertOpenCodeAPI(t, registry, ProviderOpenCode, "gpt-5.4", APIOpenAIResponses)
	assertOpenCodeAPI(t, registry, ProviderOpenCode, "minimax-m3-free", APIAnthropicMessages)

	openCodeDeepSeek, ok := registry.Model(ProviderOpenCode, "deepseek-v4-flash")
	if !ok {
		t.Fatal("fresh registry missing generated OpenCode Zen DeepSeek V4 Flash model")
	}
	if openCodeDeepSeek.OpenAICompletionsCompat == nil ||
		openCodeDeepSeek.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
		openCodeDeepSeek.OpenAICompletionsCompat.RequiresReasoningContentOnAssistantMessages != OpenAICompatSupported {
		t.Fatalf("OpenCode Zen DeepSeek compat = %#v, want deepseek reasoning content replay", openCodeDeepSeek.OpenAICompletionsCompat)
	}
	if !openCodeDeepSeek.SupportsThinkingLevel(ThinkingLevelOff) ||
		openCodeDeepSeek.SupportsThinkingLevel(ThinkingLevelMedium) {
		t.Fatalf("OpenCode Zen DeepSeek thinking level support = %+v / %+v, want off without medium", openCodeDeepSeek.ThinkingLevelMap, openCodeDeepSeek.UnsupportedThinkingLevels)
	}

	openCodeClaude, ok := registry.Model(ProviderOpenCode, "claude-opus-4-8")
	if !ok {
		t.Fatal("fresh registry missing generated OpenCode Zen Claude Opus model")
	}
	if openCodeClaude.AnthropicMessagesCompat == nil ||
		openCodeClaude.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive ||
		openCodeClaude.AnthropicMessagesCompat.SupportsTemperature != AnthropicCompatUnsupported {
		t.Fatalf("OpenCode Zen Claude compat = %#v, want adaptive thinking without temperature", openCodeClaude.AnthropicMessagesCompat)
	}

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
	if grokBuild.SupportsThinkingLevel(ThinkingLevelOff) ||
		grokBuild.SupportsThinkingLevel(ThinkingLevelMedium) ||
		!grokBuild.SupportsThinkingLevel(ThinkingLevelHigh) {
		t.Fatalf("OpenCode Zen Grok Build thinking support = %+v / %+v, want high only", grokBuild.ThinkingLevelMap, grokBuild.UnsupportedThinkingLevels)
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
		openCodeGo.OpenAICompletionsCompat.RequiresReasoningContentOnAssistantMessages != OpenAICompatSupported ||
		openCodeGo.OpenAICompletionsCompat.SupportsJSONSchemaResponseFormat != OpenAICompatUnsupported {
		t.Fatalf("OpenCode Go DeepSeek compat = %#v, want deepseek reasoning replay and JSON Schema response downgrade", openCodeGo.OpenAICompletionsCompat)
	}
	if got, ok := openCodeGo.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "max" {
		t.Fatalf("OpenCode Go xhigh level = %q, %v; want max, true", got, ok)
	}
	if !openCodeGo.SupportsThinkingLevel(ThinkingLevelOff) ||
		openCodeGo.SupportsThinkingLevel(ThinkingLevelLow) {
		t.Fatalf("OpenCode Go DeepSeek thinking support = %+v / %+v, want off without low", openCodeGo.ThinkingLevelMap, openCodeGo.UnsupportedThinkingLevels)
	}
	assertMetadataString(t, openCodeGo.ProviderMetadata, "baseURL", "https://opencode.ai/zen/go/v1")
	assertMetadataStrings(t, openCodeGo.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENCODE_API_KEY"})
	assertOpenCodeAPI(t, registry, ProviderOpenCodeGo, "minimax-m2.5", APIAnthropicMessages)
	assertOpenCodeAPI(t, registry, ProviderOpenCodeGo, "minimax-m3", APIAnthropicMessages)
	assertOpenCodeAPI(t, registry, ProviderOpenCodeGo, "qwen3.7-max", APIAnthropicMessages)

	openCodeGoKimi, ok := registry.Model(ProviderOpenCodeGo, "kimi-k2.6")
	if !ok {
		t.Fatal("fresh registry missing generated OpenCode Go Kimi model")
	}
	if openCodeGoKimi.OpenAICompletionsCompat == nil ||
		openCodeGoKimi.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
		openCodeGoKimi.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported {
		t.Fatalf("OpenCode Go Kimi compat = %#v, want deepseek reasoning without effort", openCodeGoKimi.OpenAICompletionsCompat)
	}
	if !openCodeGoKimi.SupportsThinkingLevel(ThinkingLevelOff) ||
		!openCodeGoKimi.SupportsThinkingLevel(ThinkingLevelHigh) ||
		openCodeGoKimi.SupportsThinkingLevel(ThinkingLevelMedium) {
		t.Fatalf("OpenCode Go Kimi thinking support = %+v / %+v, want off and high only", openCodeGoKimi.ThinkingLevelMap, openCodeGoKimi.UnsupportedThinkingLevels)
	}

	assertProviderConstantsHaveGeneratedTextMetadata(t, registry)
	assertGeneratedOpenAICompatibleProviderMetadata(t, registry)
	assertGeneratedAnthropicCompatibleProviderMetadata(t, registry)
	assertGeneratedVertexMetadata(t, registry)

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

	grokImage, ok := registry.ImageModel(ProviderOpenRouter, "x-ai/grok-imagine-image-quality")
	if !ok {
		t.Fatal("fresh registry missing generated OpenRouter Grok image model")
	}
	if grokImage.API != ImageAPIOpenRouterImages {
		t.Fatalf("Grok image API = %q, want %q", grokImage.API, ImageAPIOpenRouterImages)
	}
	assertMetadataString(t, grokImage.ProviderMetadata, "routedProvider", "xai")
	assertMetadataStrings(t, grokImage.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENROUTER_API_KEY"})

	geminiImage, ok := registry.ImageModel(ProviderOpenRouter, "google/gemini-2.5-flash-image")
	if !ok {
		t.Fatal("fresh registry missing generated stable OpenRouter Gemini image model")
	}
	if geminiImage.API != ImageAPIOpenRouterImages {
		t.Fatalf("Gemini image API = %q, want %q", geminiImage.API, ImageAPIOpenRouterImages)
	}
	assertMetadataString(t, geminiImage.ProviderMetadata, "routedProvider", "google")
	assertMetadataStrings(t, geminiImage.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENROUTER_API_KEY"})

	maiImage, ok := registry.ImageModel(ProviderOpenRouter, "microsoft/mai-image-2.5")
	if !ok {
		t.Fatal("fresh registry missing generated OpenRouter MAI image model")
	}
	if maiImage.API != ImageAPIOpenRouterImages {
		t.Fatalf("MAI image API = %q, want %q", maiImage.API, ImageAPIOpenRouterImages)
	}
	assertMetadataString(t, maiImage.ProviderMetadata, "routedProvider", "microsoft")
	assertMetadataString(t, maiImage.ProviderMetadata, "modelFamily", "mai-image")
	assertMetadataStrings(t, maiImage.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENROUTER_API_KEY"})
	cost, ok := maiImage.ProviderMetadata["cost"].(map[string]any)
	if !ok {
		t.Fatalf("MAI image cost metadata type = %T, want map[string]any", maiImage.ProviderMetadata["cost"])
	}
	values, ok := cost["values"].(map[string]float64)
	if !ok || values["input"] != 5 {
		t.Fatalf("MAI image cost values = %#v, want input token cost", cost["values"])
	}

	riverflowImage, ok := registry.ImageModel(ProviderOpenRouter, "sourceful/riverflow-v2.5-pro")
	if !ok {
		t.Fatal("fresh registry missing generated OpenRouter Riverflow image model")
	}
	if riverflowImage.API != ImageAPIOpenRouterImages {
		t.Fatalf("Riverflow image API = %q, want %q", riverflowImage.API, ImageAPIOpenRouterImages)
	}
	assertMetadataString(t, riverflowImage.ProviderMetadata, "routedProvider", "sourceful")
	assertMetadataString(t, riverflowImage.ProviderMetadata, "modelFamily", "riverflow")
	assertMetadataStrings(t, riverflowImage.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENROUTER_API_KEY"})
}

func assertProviderConstantsHaveGeneratedTextMetadata(t *testing.T, registry *Registry) {
	t.Helper()

	providers := map[ProviderID]bool{}
	for _, model := range registry.ListModels() {
		providers[model.Provider] = true
	}
	for _, provider := range []ProviderID{
		ProviderAmazonBedrock,
		ProviderAntLing,
		ProviderAnthropic,
		ProviderAzureOpenAIResponses,
		ProviderCerebras,
		ProviderCloudflareAIGateway,
		ProviderCloudflareWorkersAI,
		ProviderDeepSeek,
		ProviderFireworks,
		ProviderGitHubCopilot,
		ProviderGoogle,
		ProviderGoogleVertex,
		ProviderGoogleVertexAnthropic,
		ProviderGoogleVertexOpenAI,
		ProviderGroq,
		ProviderKimi,
		ProviderMistral,
		ProviderMiniMax,
		ProviderMiniMaxCN,
		ProviderMoonshotAI,
		ProviderMoonshotAICN,
		ProviderNVIDIA,
		ProviderOpenAICodex,
		ProviderOpenAI,
		ProviderOpenCode,
		ProviderOpenCodeGo,
		ProviderOpenRouter,
		ProviderTogether,
		ProviderVercelAIGateway,
		ProviderXAI,
		ProviderXiaomi,
		ProviderZAI,
		ProviderZAICodingCN,
	} {
		if !providers[provider] {
			t.Fatalf("generated text metadata missing provider %q", provider)
		}
	}
}

func assertGeneratedOpenAICompatibleProviderMetadata(t *testing.T, registry *Registry) {
	t.Helper()

	deepSeek, ok := registry.Model(ProviderDeepSeek, "deepseek-v4-flash")
	if !ok {
		t.Fatal("fresh registry missing generated DeepSeek model")
	}
	if deepSeek.OpenAICompletionsCompat == nil ||
		deepSeek.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
		deepSeek.OpenAICompletionsCompat.RequiresReasoningContentOnAssistantMessages != OpenAICompatSupported {
		t.Fatalf("DeepSeek compat = %#v, want deepseek reasoning content replay", deepSeek.OpenAICompletionsCompat)
	}
	if got, ok := deepSeek.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "max" {
		t.Fatalf("DeepSeek xhigh level = %q, %v; want max, true", got, ok)
	}
	deepSeekPro, ok := registry.Model(ProviderDeepSeek, "deepseek-v4-pro")
	if !ok {
		t.Fatal("fresh registry missing generated DeepSeek V4 Pro model")
	}
	if deepSeekPro.OpenAICompletionsCompat == nil ||
		deepSeekPro.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
		deepSeekPro.OpenAICompletionsCompat.RequiresReasoningContentOnAssistantMessages != OpenAICompatSupported {
		t.Fatalf("DeepSeek V4 Pro compat = %#v, want deepseek reasoning content replay", deepSeekPro.OpenAICompletionsCompat)
	}
	if got, ok := deepSeekPro.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "max" {
		t.Fatalf("DeepSeek V4 Pro xhigh level = %q, %v; want max, true", got, ok)
	}

	together, ok := registry.Model(ProviderTogether, "Qwen/Qwen3-Coder-480B-A35B-Instruct-FP8")
	if !ok {
		t.Fatal("fresh registry missing generated Together model")
	}
	if together.OpenAICompletionsCompat == nil ||
		together.OpenAICompletionsCompat.SupportsDeveloperRole != OpenAICompatUnsupported ||
		together.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported ||
		together.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxTokens {
		t.Fatalf("Together compat = %#v, want conservative OpenAI-compatible overrides", together.OpenAICompletionsCompat)
	}

	xiaomi, ok := registry.Model(ProviderXiaomi, "mimo-v2.5")
	if !ok {
		t.Fatal("fresh registry missing generated Xiaomi model")
	}
	if xiaomi.OpenAICompletionsCompat == nil ||
		xiaomi.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
		xiaomi.OpenAICompletionsCompat.RequiresReasoningContentOnAssistantMessages != OpenAICompatSupported {
		t.Fatalf("Xiaomi compat = %#v, want deepseek reasoning content replay", xiaomi.OpenAICompletionsCompat)
	}

	for _, provider := range []ProviderID{ProviderMoonshotAI, ProviderMoonshotAICN} {
		moonshot, ok := registry.Model(provider, "kimi-k2-thinking")
		if !ok {
			t.Fatalf("fresh registry missing generated %s kimi-k2-thinking model", provider)
		}
		if moonshot.OpenAICompletionsCompat == nil ||
			moonshot.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
			moonshot.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported ||
			moonshot.OpenAICompletionsCompat.SupportsStreamingUsage != OpenAICompatSupported {
			t.Fatalf("%s compat = %#v, want deepseek thinking format without reasoning effort", provider, moonshot.OpenAICompletionsCompat)
		}
	}

	moonshotCode, ok := registry.Model(ProviderMoonshotAI, "kimi-k2.7-code")
	if !ok {
		t.Fatal("fresh registry missing generated Moonshot AI Kimi K2.7 Code model")
	}
	if !moonshotCode.SupportsTools || !moonshotCode.SupportsImages() || !moonshotCode.SupportsReasoning() {
		t.Fatalf("Moonshot AI Kimi K2.7 Code capabilities were not generated: %+v", moonshotCode)
	}
	if moonshotCode.OpenAICompletionsCompat == nil ||
		moonshotCode.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
		moonshotCode.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported ||
		moonshotCode.OpenAICompletionsCompat.SupportsStreamingUsage != OpenAICompatSupported ||
		moonshotCode.OpenAICompletionsCompat.SupportsStrictTools != OpenAICompatUnsupported ||
		moonshotCode.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxTokens {
		t.Fatalf("Moonshot AI Kimi K2.7 Code compat = %#v, want Moonshot OpenAI-compatible overrides", moonshotCode.OpenAICompletionsCompat)
	}

	for _, id := range []ModelID{
		"grok-3",
		"grok-3-fast",
		"grok-4.20-0309-non-reasoning",
		"grok-4.20-0309-reasoning",
		"grok-4.3",
		"grok-build-0.1",
		"grok-code-fast-1",
	} {
		model, ok := registry.Model(ProviderXAI, id)
		if !ok {
			t.Fatalf("fresh registry missing generated xAI model %s", id)
		}
		if model.OpenAICompletionsCompat == nil ||
			model.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported ||
			model.OpenAICompletionsCompat.SupportsStreamingUsage != OpenAICompatSupported ||
			model.OpenAICompletionsCompat.SupportsStrictTools != OpenAICompatSupported ||
			model.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxCompletionTokens {
			t.Fatalf("xAI %s compat = %#v, want xAI OpenAI-compatible overrides", id, model.OpenAICompletionsCompat)
		}
	}
	grok43, ok := registry.Model(ProviderXAI, "grok-4.3")
	if !ok {
		t.Fatal("fresh registry missing generated xAI Grok 4.3 model")
	}
	if !grok43.SupportsTools || !grok43.SupportsImages() || !grok43.SupportsReasoning() {
		t.Fatalf("Grok 4.3 capabilities were not generated: %+v", grok43)
	}

	for _, tt := range []struct {
		provider ProviderID
		id       ModelID
		baseURL  string
		envVars  []string
	}{
		{provider: ProviderAntLing, id: "Ring-2.6-1T", baseURL: "https://api.ant-ling.com/v1", envVars: []string{"ANT_LING_API_KEY"}},
		{provider: ProviderCloudflareWorkersAI, id: "@cf/meta/llama-4-scout-17b-16e-instruct", baseURL: "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai/v1", envVars: []string{"CLOUDFLARE_API_KEY"}},
		{provider: ProviderCerebras, id: "llama3.1-8b", baseURL: "https://api.cerebras.ai/v1", envVars: []string{"CEREBRAS_API_KEY"}},
		{provider: ProviderGroq, id: "llama-3.3-70b-versatile", baseURL: "https://api.groq.com/openai/v1", envVars: []string{"GROQ_API_KEY"}},
		{provider: ProviderMoonshotAI, id: "kimi-k2-thinking", baseURL: "https://api.moonshot.ai/v1", envVars: []string{"MOONSHOT_API_KEY"}},
		{provider: ProviderNVIDIA, id: "openai/gpt-oss-20b", baseURL: "https://integrate.api.nvidia.com/v1", envVars: []string{"NVIDIA_API_KEY"}},
		{provider: ProviderXAI, id: "grok-3", baseURL: "https://api.x.ai/v1", envVars: []string{"XAI_API_KEY"}},
		{provider: ProviderGitHubCopilot, id: "gpt-5.2-codex", baseURL: "https://api.individual.githubcopilot.com", envVars: []string{"COPILOT_GITHUB_TOKEN"}},
		{provider: ProviderZAI, id: "glm-5.1", baseURL: "https://api.z.ai/api/coding/paas/v4", envVars: []string{"ZAI_API_KEY"}},
	} {
		model, ok := registry.Model(tt.provider, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated %s model %s", tt.provider, tt.id)
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", tt.baseURL)
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, tt.envVars)
	}

	antLing, ok := registry.Model(ProviderAntLing, "Ring-2.6-1T")
	if !ok {
		t.Fatal("fresh registry missing generated Ant Ling model")
	}
	if antLing.OpenAICompletionsCompat == nil ||
		antLing.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningAntLing ||
		antLing.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported {
		t.Fatalf("Ant Ling compat = %#v, want ant-ling reasoning without reasoning_effort", antLing.OpenAICompletionsCompat)
	}
	if antLing.SupportsThinkingLevel(ThinkingLevelLow) {
		t.Fatal("Ant Ling low reasoning level unexpectedly supported")
	}
	if got, ok := antLing.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "xhigh" {
		t.Fatalf("Ant Ling xhigh level = %q, %v; want xhigh, true", got, ok)
	}

	zai, ok := registry.Model(ProviderZAI, "glm-5.1")
	if !ok {
		t.Fatal("fresh registry missing generated Z.ai model")
	}
	if zai.OpenAICompletionsCompat == nil ||
		zai.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningZAI ||
		zai.OpenAICompletionsCompat.SupportsToolStream != OpenAICompatSupported {
		t.Fatalf("Z.ai compat = %#v, want zai reasoning and tool_stream", zai.OpenAICompletionsCompat)
	}

	cloudflare, ok := registry.Model(ProviderCloudflareAIGateway, "gpt-5.4")
	if !ok {
		t.Fatal("fresh registry missing generated Cloudflare AI Gateway model")
	}
	if cloudflare.API != APIOpenAIResponses || !cloudflare.SupportsTools || !cloudflare.SupportsImages() || !cloudflare.SupportsReasoning() {
		t.Fatalf("Cloudflare AI Gateway model capabilities = %+v, want Responses tools, images, and reasoning", cloudflare)
	}
	assertMetadataString(t, cloudflare.ProviderMetadata, "baseURL", "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai")
	assertMetadataStrings(t, cloudflare.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"CLOUDFLARE_API_KEY"})

	azure, ok := registry.Model(ProviderAzureOpenAIResponses, "gpt-5.4")
	if !ok {
		t.Fatal("fresh registry missing generated Azure OpenAI Responses model")
	}
	if azure.API != APIAzureOpenAIResponses || azure.AzureOpenAIResponses == nil ||
		azure.AzureOpenAIResponses.Deployment != "gpt-5.4" ||
		azure.AzureOpenAIResponses.APIKeyEnvVar != "AZURE_OPENAI_API_KEY" {
		t.Fatalf("Azure OpenAI metadata = %+v, want deployment and API key metadata", azure)
	}
	assertMetadataString(t, azure.ProviderMetadata, "baseURL", "https://{resource}.openai.azure.com")
	assertMetadataStrings(t, azure.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"AZURE_OPENAI_API_KEY"})

	codex, ok := registry.Model(ProviderOpenAICodex, "gpt-5.4")
	if !ok {
		t.Fatal("fresh registry missing generated OpenAI Codex model")
	}
	if codex.API != APIOpenAICodexResponses || codex.OpenAICodexResponses == nil || codex.OpenAICodexResponses.Model != "gpt-5.4" {
		t.Fatalf("OpenAI Codex metadata = %+v, want Codex Responses model mapping", codex)
	}
	assertMetadataStrings(t, codex.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENAI_CODEX_OAUTH_TOKEN"})
}

func assertGeneratedAnthropicCompatibleProviderMetadata(t *testing.T, registry *Registry) {
	t.Helper()

	kimi, ok := registry.Model(ProviderKimi, "kimi-for-coding")
	if !ok {
		t.Fatal("fresh registry missing generated Kimi model")
	}
	if kimi.API != APIAnthropicMessages || !kimi.SupportsTools || !kimi.SupportsImages() || !kimi.SupportsReasoning() {
		t.Fatalf("Kimi model capabilities were not generated: %+v", kimi)
	}
	if kimi.AnthropicMessagesCompat == nil ||
		kimi.AnthropicMessagesCompat.SupportsSessionAffinity != AnthropicCompatSupported ||
		kimi.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive {
		t.Fatalf("Kimi compat = %#v, want adaptive Anthropic-compatible metadata", kimi.AnthropicMessagesCompat)
	}
	assertMetadataString(t, kimi.ProviderMetadata, "baseURL", "https://api.kimi.com/coding/v1")
	assertMetadataStrings(t, kimi.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"KIMI_API_KEY"})

	minimax, ok := registry.Model(ProviderMiniMax, "MiniMax-M3")
	if !ok {
		t.Fatal("fresh registry missing generated MiniMax model")
	}
	if minimax.API != APIAnthropicMessages || !minimax.SupportsTools || !minimax.SupportsReasoning() {
		t.Fatalf("MiniMax model capabilities were not generated: %+v", minimax)
	}
	assertMetadataString(t, minimax.ProviderMetadata, "baseURL", "https://api.minimax.io/anthropic")
	assertMetadataStrings(t, minimax.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"MINIMAX_API_KEY"})

	vercel, ok := registry.Model(ProviderVercelAIGateway, "anthropic/claude-opus-4.8")
	if !ok {
		t.Fatal("fresh registry missing generated Vercel AI Gateway model")
	}
	if vercel.API != APIAnthropicMessages || !vercel.SupportsTools || !vercel.SupportsImages() || !vercel.SupportsReasoning() {
		t.Fatalf("Vercel AI Gateway model capabilities were not generated: %+v", vercel)
	}
	if vercel.AnthropicMessagesCompat == nil ||
		vercel.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive ||
		vercel.AnthropicMessagesCompat.SupportsTemperature != AnthropicCompatUnsupported {
		t.Fatalf("Vercel AI Gateway compat = %#v, want adaptive thinking and temperature suppression", vercel.AnthropicMessagesCompat)
	}
	assertMetadataString(t, vercel.ProviderMetadata, "baseURL", "https://ai-gateway.vercel.sh")
	assertMetadataStrings(t, vercel.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"AI_GATEWAY_API_KEY"})
}

func assertGeneratedVertexMetadata(t *testing.T, registry *Registry) {
	t.Helper()

	vertex, ok := registry.Model(ProviderGoogleVertex, "gemini-2.5-flash")
	if !ok {
		t.Fatal("fresh registry missing generated Vertex model")
	}
	if vertex.API != APIGoogleVertex || !vertex.SupportsTools || !vertex.SupportsImages() || !vertex.SupportsReasoning() {
		t.Fatalf("Vertex model capabilities were not generated: %+v", vertex)
	}
	assertMetadataString(t, vertex.ProviderMetadata, "vertexPublisher", "google")
	assertMetadataStrings(t, vertex.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"GOOGLE_CLOUD_API_KEY", "GOOGLE_API_KEY"})
	vertexPro, ok := registry.Model(ProviderGoogleVertex, "gemini-3.1-pro-preview")
	if !ok {
		t.Fatal("fresh registry missing generated Vertex Gemini 3.1 Pro Preview model")
	}
	if vertexPro.API != APIGoogleVertex || !vertexPro.SupportsTools || !vertexPro.SupportsImages() || !vertexPro.SupportsReasoning() {
		t.Fatalf("Vertex Gemini 3.1 Pro Preview metadata = %+v, want Vertex tools, images, and reasoning", vertexPro)
	}
	assertMetadataString(t, vertexPro.ProviderMetadata, "vertexPublisher", "google")

	vertexClaude, ok := registry.Model(ProviderGoogleVertexAnthropic, "claude-sonnet-4@20250514")
	if !ok {
		t.Fatal("fresh registry missing generated Vertex Claude model")
	}
	if vertexClaude.API != APIAnthropicMessages || !vertexClaude.SupportsTools || !vertexClaude.SupportsImages() || !vertexClaude.SupportsReasoning() {
		t.Fatalf("Vertex Claude metadata = %+v, want Anthropic Messages tools, images, and reasoning", vertexClaude)
	}
	if vertexClaude.AnthropicMessagesCompat == nil ||
		vertexClaude.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingBudget {
		t.Fatalf("Vertex Claude compat = %#v, want budget thinking", vertexClaude.AnthropicMessagesCompat)
	}
	assertMetadataString(t, vertexClaude.ProviderMetadata, "vertexPublisher", "anthropic")
	assertMetadataStrings(t, vertexClaude.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"GOOGLE_CLOUD_API_KEY", "GOOGLE_API_KEY"})

	for _, modelID := range []ModelID{"claude-opus-4-8", "claude-sonnet-4-6"} {
		latestClaude, ok := registry.Model(ProviderGoogleVertexAnthropic, modelID)
		if !ok {
			t.Fatalf("fresh registry missing latest generated Vertex Claude model %s", modelID)
		}
		if latestClaude.API != APIAnthropicMessages || !latestClaude.SupportsTools || !latestClaude.SupportsImages() || !latestClaude.SupportsReasoning() {
			t.Fatalf("latest Vertex Claude metadata for %s = %+v, want Anthropic Messages tools, images, and reasoning", modelID, latestClaude)
		}
		if latestClaude.AnthropicMessagesCompat == nil ||
			latestClaude.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive {
			t.Fatalf("latest Vertex Claude compat for %s = %#v, want adaptive thinking", modelID, latestClaude.AnthropicMessagesCompat)
		}
		assertMetadataString(t, latestClaude.ProviderMetadata, "vertexPublisher", "anthropic")
		assertMetadataStrings(t, latestClaude.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"GOOGLE_CLOUD_API_KEY", "GOOGLE_API_KEY"})
	}

	vertexLlama, ok := registry.Model(ProviderGoogleVertexOpenAI, "meta/llama-3.3-70b-instruct-maas")
	if !ok {
		t.Fatal("fresh registry missing generated Vertex Llama MaaS model")
	}
	if vertexLlama.API != APIOpenAICompletions || !vertexLlama.SupportsTools || vertexLlama.SupportsReasoning() {
		t.Fatalf("Vertex Llama metadata = %+v, want OpenAI-compatible tools without reasoning", vertexLlama)
	}
	if vertexLlama.OpenAICompletionsCompat == nil ||
		vertexLlama.OpenAICompletionsCompat.SupportsDeveloperRole != OpenAICompatUnsupported ||
		vertexLlama.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported {
		t.Fatalf("Vertex Llama compat = %#v, want conservative OpenAI-compatible overrides", vertexLlama.OpenAICompletionsCompat)
	}
	if got, ok := vertexLlama.ProviderMetadata["vertexOpenAICompatible"].(bool); !ok || !got {
		t.Fatalf("vertexOpenAICompatible metadata = %v, %v; want true, true", got, ok)
	}
	assertMetadataStrings(t, vertexLlama.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"GOOGLE_CLOUD_API_KEY", "GOOGLE_API_KEY"})
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

func assertOpenCodeAPI(t *testing.T, registry *Registry, provider ProviderID, id ModelID, want API) {
	t.Helper()
	model, ok := registry.Model(provider, id)
	if !ok {
		t.Fatalf("fresh registry missing generated %s model %s", provider, id)
	}
	if model.API != APIOpenAICompletions {
		t.Fatalf("%s/%s API = %q, want registry-facing %q", provider, id, model.API, APIOpenAICompletions)
	}
	assertMetadataString(t, model.ProviderMetadata, "opencodeAPI", string(want))
}
