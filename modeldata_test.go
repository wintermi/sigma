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
	if err := registerBuiltinEmbeddingModels(registry); err != nil {
		t.Fatalf("registerBuiltinEmbeddingModels returned error: %v", err)
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

	fireworksKimiCode, ok := registry.Model(ProviderFireworks, "accounts/fireworks/models/kimi-k2p7-code")
	if !ok {
		t.Fatal("fresh registry missing generated Fireworks Kimi K2.7 Code model")
	}
	if fireworksKimiCode.API != APIOpenAICompletions {
		t.Fatalf("Fireworks Kimi K2.7 Code API = %q, want %q", fireworksKimiCode.API, APIOpenAICompletions)
	}
	if !fireworksKimiCode.SupportsTools || !fireworksKimiCode.SupportsImages() || !fireworksKimiCode.SupportsReasoning() {
		t.Fatalf("Fireworks Kimi K2.7 Code capabilities were not generated: %+v", fireworksKimiCode)
	}
	if fireworksKimiCode.OpenAICompletionsCompat == nil ||
		fireworksKimiCode.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningFireworks ||
		fireworksKimiCode.OpenAICompletionsCompat.SupportsStreamingUsage != OpenAICompatSupported ||
		fireworksKimiCode.OpenAICompletionsCompat.SupportsStrictTools != OpenAICompatSupported ||
		fireworksKimiCode.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxTokens {
		t.Fatalf("Fireworks Kimi K2.7 Code compat = %#v, want Fireworks OpenAI completions compat", fireworksKimiCode.OpenAICompletionsCompat)
	}
	assertMetadataString(t, fireworksKimiCode.ProviderMetadata, "baseURL", "https://api.fireworks.ai/inference/v1")
	assertMetadataString(t, fireworksKimiCode.ProviderMetadata, "fireworksSurface", "openai")
	assertMetadataString(t, fireworksKimiCode.ProviderMetadata, "pricingStatus", "unverified")
	assertMetadataStrings(t, fireworksKimiCode.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"FIREWORKS_API_KEY"})

	for _, id := range []ModelID{
		"accounts/fireworks/models/glm-5p2",
		"accounts/fireworks/routers/glm-5p2-fast",
	} {
		fireworksOpenAI, ok := registry.Model(ProviderFireworks, id)
		if !ok {
			t.Fatalf("fresh registry missing generated Fireworks OpenAI-compatible model %s", id)
		}
		if fireworksOpenAI.API != APIOpenAICompletions {
			t.Fatalf("Fireworks OpenAI-compatible %s API = %q, want %q", id, fireworksOpenAI.API, APIOpenAICompletions)
		}
		if !fireworksOpenAI.SupportsTools || fireworksOpenAI.SupportsImages() {
			t.Fatalf("Fireworks OpenAI-compatible %s capabilities = %+v, want tools without image input", id, fireworksOpenAI)
		}
		if !fireworksOpenAI.SupportsReasoning() || !fireworksOpenAI.SupportsThinkingLevel(ThinkingLevelMedium) {
			t.Fatalf("Fireworks OpenAI-compatible %s reasoning metadata was not generated: %+v", id, fireworksOpenAI)
		}
		if fireworksOpenAI.OpenAICompletionsCompat == nil ||
			fireworksOpenAI.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningFireworks ||
			fireworksOpenAI.OpenAICompletionsCompat.SupportsStreamingUsage != OpenAICompatSupported ||
			fireworksOpenAI.OpenAICompletionsCompat.SupportsStrictTools != OpenAICompatSupported ||
			fireworksOpenAI.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxTokens {
			t.Fatalf("Fireworks OpenAI-compatible %s compat = %#v, want Fireworks OpenAI completions compat", id, fireworksOpenAI.OpenAICompletionsCompat)
		}
		assertMetadataString(t, fireworksOpenAI.ProviderMetadata, "baseURL", "https://api.fireworks.ai/inference/v1")
		assertMetadataString(t, fireworksOpenAI.ProviderMetadata, "fireworksSurface", "openai")
		assertMetadataString(t, fireworksOpenAI.ProviderMetadata, "modelFamily", "glm")
		assertMetadataString(t, fireworksOpenAI.ProviderMetadata, "pricingStatus", "unverified")
		assertMetadataStrings(t, fireworksOpenAI.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"FIREWORKS_API_KEY"})
	}

	fireworksAnthropicRows := []struct {
		id         ModelID
		family     string
		wantImages bool
		wantRouter bool
	}{
		{id: "accounts/fireworks/models/deepseek-v4-flash", family: "deepseek"},
		{id: "accounts/fireworks/models/deepseek-v4-pro", family: "deepseek"},
		{id: "accounts/fireworks/models/glm-5p1", family: "glm"},
		{id: "accounts/fireworks/models/gpt-oss-120b", family: "gpt-oss"},
		{id: "accounts/fireworks/models/gpt-oss-20b", family: "gpt-oss"},
		{id: "accounts/fireworks/models/kimi-k2p6", family: "kimi", wantImages: true},
		{id: "accounts/fireworks/models/kimi-k2p7-code", family: "kimi", wantImages: true},
		{id: "accounts/fireworks/models/minimax-m2p7", family: "minimax"},
		{id: "accounts/fireworks/models/minimax-m3", family: "minimax"},
		{id: "accounts/fireworks/models/qwen3p7-plus", family: "qwen", wantImages: true},
		{id: "accounts/fireworks/routers/glm-5p1-fast", family: "glm", wantRouter: true},
		{id: "accounts/fireworks/routers/kimi-k2p6-fast", family: "kimi", wantImages: true, wantRouter: true},
		{id: "accounts/fireworks/routers/kimi-k2p6-turbo", family: "kimi", wantImages: true, wantRouter: true},
		{id: "accounts/fireworks/routers/kimi-k2p7-code-fast", family: "kimi", wantImages: true, wantRouter: true},
	}
	for _, id := range fireworksAnthropicRows {
		fireworksAnthropic, ok := registry.Model(ProviderFireworksAnthropic, id.id)
		if !ok {
			t.Fatalf("fresh registry missing generated Fireworks Anthropic model %s", id.id)
		}
		if fireworksAnthropic.API != APIAnthropicMessages {
			t.Fatalf("Fireworks Anthropic %s API = %q, want %q", id.id, fireworksAnthropic.API, APIAnthropicMessages)
		}
		if !fireworksAnthropic.SupportsTools || fireworksAnthropic.SupportsImages() != id.wantImages {
			t.Fatalf("Fireworks Anthropic %s capabilities = %+v, want tools and images %v", id.id, fireworksAnthropic, id.wantImages)
		}
		if !fireworksAnthropic.SupportsReasoning() || !fireworksAnthropic.SupportsThinkingLevel(ThinkingLevelHigh) {
			t.Fatalf("Fireworks Anthropic %s reasoning metadata was not generated: %+v", id.id, fireworksAnthropic)
		}
		if fireworksAnthropic.AnthropicMessagesCompat == nil ||
			fireworksAnthropic.AnthropicMessagesCompat.SupportsSessionAffinity != AnthropicCompatSupported ||
			fireworksAnthropic.AnthropicMessagesCompat.SupportsEagerToolInputStreaming != AnthropicCompatUnsupported ||
			fireworksAnthropic.AnthropicMessagesCompat.SupportsLongCacheRetention != AnthropicCompatUnsupported ||
			fireworksAnthropic.AnthropicMessagesCompat.SupportsCacheControlOnTools != AnthropicCompatUnsupported ||
			fireworksAnthropic.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingBudget {
			t.Fatalf("Fireworks Anthropic %s compat = %#v, want Messages compatibility overrides", id.id, fireworksAnthropic.AnthropicMessagesCompat)
		}
		assertMetadataString(t, fireworksAnthropic.ProviderMetadata, "baseURL", "https://api.fireworks.ai/inference/v1")
		assertMetadataString(t, fireworksAnthropic.ProviderMetadata, "fireworksSurface", "anthropic")
		assertMetadataString(t, fireworksAnthropic.ProviderMetadata, "modelFamily", id.family)
		assertMetadataString(t, fireworksAnthropic.ProviderMetadata, "pricingStatus", "unverified")
		assertMetadataStrings(t, fireworksAnthropic.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"FIREWORKS_API_KEY"})
		if id.wantRouter {
			if got, ok := fireworksAnthropic.ProviderMetadata["router"].(bool); !ok || !got {
				t.Fatalf("Fireworks Anthropic %s router metadata = %#v, want true", id.id, fireworksAnthropic.ProviderMetadata["router"])
			}
		}
	}

	anthropic, ok := registry.Model(ProviderAnthropic, "claude-3-5-sonnet-20241022")
	if !ok {
		t.Fatal("fresh registry missing generated Anthropic text model")
	}
	if anthropic.ContextWindow == 0 || anthropic.MaxOutputTokens == 0 {
		t.Fatalf("Anthropic limits were not generated: %+v", anthropic)
	}
	assertMetadataString(t, anthropic.ProviderMetadata, "baseURL", "https://api.anthropic.com/v1")
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
	sonnet5, ok := registry.Model(ProviderAnthropic, "claude-sonnet-5")
	if !ok {
		t.Fatal("fresh registry missing generated Claude Sonnet 5 model")
	}
	if sonnet5.API != APIAnthropicMessages || !sonnet5.SupportsTools || !sonnet5.SupportsImages() || !sonnet5.SupportsReasoning() {
		t.Fatalf("Claude Sonnet 5 metadata = %+v, want Anthropic Messages with tools, images, and reasoning", sonnet5)
	}
	if sonnet5.AnthropicMessagesCompat == nil || sonnet5.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive {
		t.Fatalf("Claude Sonnet 5 compat = %#v, want adaptive thinking", sonnet5.AnthropicMessagesCompat)
	}
	if sonnet5.ContextWindow != 1000000 || sonnet5.MaxOutputTokens != 128000 {
		t.Fatalf("Claude Sonnet 5 limits = context %d max %d, want 1000000/128000", sonnet5.ContextWindow, sonnet5.MaxOutputTokens)
	}
	if sonnet5.InputCostPerMillion != 2 || sonnet5.OutputCostPerMillion != 10 ||
		sonnet5.CacheReadInputCostPerMillion != 0.2 || sonnet5.CacheWriteInputCostPerMillion != 2.5 {
		t.Fatalf("Claude Sonnet 5 costs = %f/%f/%f/%f, want 2/10/0.2/2.5",
			sonnet5.InputCostPerMillion,
			sonnet5.OutputCostPerMillion,
			sonnet5.CacheReadInputCostPerMillion,
			sonnet5.CacheWriteInputCostPerMillion)
	}
	assertMetadataStrings(t, sonnet5.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"ANTHROPIC_API_KEY"})
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
	for _, tt := range []struct {
		id             ModelID
		inputCost      float64
		outputCost     float64
		cacheReadCost  float64
		cacheWriteCost float64
	}{
		{id: "gpt-5.6-luna", inputCost: 1, outputCost: 6, cacheReadCost: 0.1, cacheWriteCost: 1.25},
		{id: "gpt-5.6-sol", inputCost: 5, outputCost: 30, cacheReadCost: 0.5, cacheWriteCost: 6.25},
		{id: "gpt-5.6-terra", inputCost: 2.5, outputCost: 15, cacheReadCost: 0.25, cacheWriteCost: 3.125},
	} {
		model, ok := registry.Model(ProviderOpenAI, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated %s model", tt.id)
		}
		if model.API != APIOpenAIResponses || !model.SupportsTools || !model.SupportsImages() || !model.SupportsReasoning() {
			t.Fatalf("%s metadata = %+v, want Responses with tools, images, and reasoning", tt.id, model)
		}
		if model.ContextWindow != 272_000 || model.MaxOutputTokens != 128_000 {
			t.Fatalf("%s limits = %d/%d, want 272000/128000", tt.id, model.ContextWindow, model.MaxOutputTokens)
		}
		if model.InputCostPerMillion != tt.inputCost || model.OutputCostPerMillion != tt.outputCost ||
			model.CacheReadInputCostPerMillion != tt.cacheReadCost || model.CacheWriteInputCostPerMillion != tt.cacheWriteCost {
			t.Fatalf("%s costs = %f/%f/%f/%f, want %f/%f/%f/%f", tt.id,
				model.InputCostPerMillion, model.OutputCostPerMillion, model.CacheReadInputCostPerMillion, model.CacheWriteInputCostPerMillion,
				tt.inputCost, tt.outputCost, tt.cacheReadCost, tt.cacheWriteCost)
		}
		for level, want := range map[ThinkingLevel]string{
			ThinkingLevelOff:     "none",
			ThinkingLevelXHigh:   "xhigh",
			ThinkingLevel("max"): "max",
		} {
			if got, ok := model.ThinkingLevelMap[level]; !ok || got != want {
				t.Fatalf("%s %s thinking level = %q, %v; want %q, true", tt.id, level, got, ok, want)
			}
		}
	}
	for _, tt := range []struct {
		provider       ProviderID
		id             ModelID
		api            API
		contextWindow  int
		inputCost      float64
		outputCost     float64
		cacheReadCost  float64
		cacheWriteCost float64
		thinkingLevels map[ThinkingLevel]string
	}{
		{provider: ProviderAzureOpenAIResponses, id: "gpt-5.6-luna", api: APIAzureOpenAIResponses, contextWindow: 1_050_000, inputCost: 1, outputCost: 6, cacheReadCost: 0.1, cacheWriteCost: 1.25, thinkingLevels: map[ThinkingLevel]string{ThinkingLevelXHigh: "xhigh", ThinkingLevel("max"): "max"}},
		{provider: ProviderAzureOpenAIResponses, id: "gpt-5.6-sol", api: APIAzureOpenAIResponses, contextWindow: 1_050_000, inputCost: 5, outputCost: 30, cacheReadCost: 0.5, cacheWriteCost: 6.25, thinkingLevels: map[ThinkingLevel]string{ThinkingLevelXHigh: "xhigh", ThinkingLevel("max"): "max"}},
		{provider: ProviderAzureOpenAIResponses, id: "gpt-5.6-terra", api: APIAzureOpenAIResponses, contextWindow: 1_050_000, inputCost: 2.5, outputCost: 15, cacheReadCost: 0.25, cacheWriteCost: 3.125, thinkingLevels: map[ThinkingLevel]string{ThinkingLevelXHigh: "xhigh", ThinkingLevel("max"): "max"}},
		{provider: ProviderOpenAICodex, id: "gpt-5.6-luna", api: APIOpenAICodexResponses, contextWindow: 372_000, inputCost: 1, outputCost: 6, cacheReadCost: 0.1, cacheWriteCost: 1.25, thinkingLevels: map[ThinkingLevel]string{ThinkingLevelMinimal: "low", ThinkingLevelXHigh: "xhigh", ThinkingLevel("max"): "max"}},
		{provider: ProviderOpenAICodex, id: "gpt-5.6-sol", api: APIOpenAICodexResponses, contextWindow: 372_000, inputCost: 5, outputCost: 30, cacheReadCost: 0.5, cacheWriteCost: 6.25, thinkingLevels: map[ThinkingLevel]string{ThinkingLevelMinimal: "low", ThinkingLevelXHigh: "xhigh", ThinkingLevel("max"): "max"}},
		{provider: ProviderOpenAICodex, id: "gpt-5.6-terra", api: APIOpenAICodexResponses, contextWindow: 372_000, inputCost: 2.5, outputCost: 15, cacheReadCost: 0.25, cacheWriteCost: 3.125, thinkingLevels: map[ThinkingLevel]string{ThinkingLevelMinimal: "low", ThinkingLevelXHigh: "xhigh", ThinkingLevel("max"): "max"}},
	} {
		model, ok := registry.Model(tt.provider, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated %s/%s model", tt.provider, tt.id)
		}
		if model.API != tt.api || !model.SupportsTools || !model.SupportsImages() || !model.SupportsReasoning() {
			t.Fatalf("%s/%s capabilities = %+v, want route API with tools, images, and reasoning", tt.provider, tt.id, model)
		}
		if model.ContextWindow != tt.contextWindow || model.MaxOutputTokens != 128_000 {
			t.Fatalf("%s/%s limits = %d/%d, want %d/128000", tt.provider, tt.id, model.ContextWindow, model.MaxOutputTokens, tt.contextWindow)
		}
		if model.InputCostPerMillion != tt.inputCost || model.OutputCostPerMillion != tt.outputCost ||
			model.CacheReadInputCostPerMillion != tt.cacheReadCost || model.CacheWriteInputCostPerMillion != tt.cacheWriteCost {
			t.Fatalf("%s/%s costs = %f/%f/%f/%f, want %f/%f/%f/%f", tt.provider, tt.id,
				model.InputCostPerMillion, model.OutputCostPerMillion, model.CacheReadInputCostPerMillion, model.CacheWriteInputCostPerMillion,
				tt.inputCost, tt.outputCost, tt.cacheReadCost, tt.cacheWriteCost)
		}
		for level, want := range tt.thinkingLevels {
			if got, ok := model.ProviderThinkingLevel(level); !ok || got != want {
				t.Fatalf("%s/%s %s thinking level = %q, %v; want %q, true", tt.provider, tt.id, level, got, ok, want)
			}
		}
		if tt.provider == ProviderAzureOpenAIResponses {
			if model.SupportsThinkingLevel(ThinkingLevelOff) {
				t.Fatalf("%s/%s unexpectedly supports disabled thinking", tt.provider, tt.id)
			}
			if model.AzureOpenAIResponses == nil || model.AzureOpenAIResponses.Deployment != string(tt.id) ||
				model.AzureOpenAIResponses.APIKeyEnvVar != "AZURE_OPENAI_API_KEY" {
				t.Fatalf("%s/%s Azure metadata = %#v, want deployment and API key metadata", tt.provider, tt.id, model.AzureOpenAIResponses)
			}
			assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"AZURE_OPENAI_API_KEY"})
			continue
		}
		if model.OpenAICodexResponses == nil || model.OpenAICodexResponses.Model != string(tt.id) {
			t.Fatalf("%s/%s Codex metadata = %#v, want model mapping", tt.provider, tt.id, model.OpenAICodexResponses)
		}
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENAI_CODEX_OAUTH_TOKEN"})
	}
	assertGeneratedCostTiers(t, registry)

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
	if nova.API != APIBedrockConverseStream || !nova.SupportsTools || !nova.SupportsImages() || !nova.SupportsReasoning() {
		t.Fatalf("Bedrock Nova 2 Lite metadata = %+v, want Converse Stream tools, images, and reasoning", nova)
	}
	for _, level := range []ThinkingLevel{ThinkingLevelLow, ThinkingLevelMedium, ThinkingLevelHigh} {
		if !nova.SupportsThinkingLevel(level) {
			t.Fatalf("Bedrock Nova 2 Lite does not support reasoning level %q", level)
		}
	}
	for _, level := range []ThinkingLevel{ThinkingLevelMinimal, ThinkingLevelXHigh} {
		if nova.SupportsThinkingLevel(level) {
			t.Fatalf("Bedrock Nova 2 Lite unexpectedly supports reasoning level %q", level)
		}
	}
	assertMetadataString(t, nova.ProviderMetadata, "modelFamily", "nova")

	curatedBedrockModels := []struct {
		id              ModelID
		supportsImages  bool
		contextWindow   int
		maxOutputTokens int
		inputCost       float64
		outputCost      float64
		cacheReadCost   float64
		modelFamily     string
	}{
		{id: "google.gemma-3-27b-it", supportsImages: true, contextWindow: 202752, maxOutputTokens: 8192, inputCost: 0.12, outputCost: 0.2, modelFamily: "gemma"},
		{id: "google.gemma-3-4b-it", supportsImages: true, contextWindow: 128000, maxOutputTokens: 4096, inputCost: 0.04, outputCost: 0.08, modelFamily: "gemma"},
		{id: "meta.llama3-1-70b-instruct-v1:0", contextWindow: 128000, maxOutputTokens: 4096, inputCost: 0.72, outputCost: 0.72, modelFamily: "llama"},
		{id: "meta.llama3-1-8b-instruct-v1:0", contextWindow: 128000, maxOutputTokens: 4096, inputCost: 0.22, outputCost: 0.22, modelFamily: "llama"},
		{id: "meta.llama3-3-70b-instruct-v1:0", contextWindow: 128000, maxOutputTokens: 4096, inputCost: 0.72, outputCost: 0.72, modelFamily: "llama"},
		{id: "meta.llama4-maverick-17b-instruct-v1:0", supportsImages: true, contextWindow: 1000000, maxOutputTokens: 16384, inputCost: 0.24, outputCost: 0.97, modelFamily: "llama"},
		{id: "meta.llama4-scout-17b-instruct-v1:0", supportsImages: true, contextWindow: 3500000, maxOutputTokens: 16384, inputCost: 0.17, outputCost: 0.66, modelFamily: "llama"},
		{id: "nvidia.nemotron-nano-12b-v2", supportsImages: true, contextWindow: 128000, maxOutputTokens: 4096, inputCost: 0.2, outputCost: 0.6, modelFamily: "nemotron"},
		{id: "nvidia.nemotron-nano-3-30b", contextWindow: 128000, maxOutputTokens: 4096, inputCost: 0.06, outputCost: 0.24, modelFamily: "nemotron"},
		{id: "nvidia.nemotron-nano-9b-v2", contextWindow: 128000, maxOutputTokens: 4096, inputCost: 0.06, outputCost: 0.23, modelFamily: "nemotron"},
		{id: "nvidia.nemotron-super-3-120b", contextWindow: 262144, maxOutputTokens: 131072, inputCost: 0.15, outputCost: 0.65, modelFamily: "nemotron"},
		{id: "openai.gpt-5.4", supportsImages: true, contextWindow: 272000, maxOutputTokens: 128000, inputCost: 2.75, outputCost: 16.5, cacheReadCost: 0.275, modelFamily: "o-series"},
		{id: "openai.gpt-5.5", supportsImages: true, contextWindow: 272000, maxOutputTokens: 128000, inputCost: 5.5, outputCost: 33, cacheReadCost: 0.55, modelFamily: "o-series"},
		{id: "writer.palmyra-x4-v1:0", contextWindow: 122880, maxOutputTokens: 8192, inputCost: 2.5, outputCost: 10, modelFamily: "palmyra"},
		{id: "writer.palmyra-x5-v1:0", contextWindow: 1040000, maxOutputTokens: 8192, inputCost: 0.6, outputCost: 6, modelFamily: "palmyra"},
		{id: "xai.grok-4.3", supportsImages: true, contextWindow: 1000000, maxOutputTokens: 131072, inputCost: 1.25, outputCost: 2.5, cacheReadCost: 0.2, modelFamily: "grok"},
	}
	for _, tt := range curatedBedrockModels {
		model, ok := registry.Model(ProviderAmazonBedrock, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing curated Bedrock model %s", tt.id)
		}
		if model.API != APIBedrockConverseStream || !model.SupportsTools || model.SupportsImages() != tt.supportsImages || model.SupportsReasoning() {
			t.Fatalf("curated Bedrock model %s capabilities = %+v", tt.id, model)
		}
		if model.ContextWindow != tt.contextWindow || model.MaxOutputTokens != tt.maxOutputTokens {
			t.Fatalf("curated Bedrock model %s limits = %d/%d, want %d/%d", tt.id, model.ContextWindow, model.MaxOutputTokens, tt.contextWindow, tt.maxOutputTokens)
		}
		if model.InputCostPerMillion != tt.inputCost || model.OutputCostPerMillion != tt.outputCost || model.CacheReadInputCostPerMillion != tt.cacheReadCost || model.CacheWriteInputCostPerMillion != 0 {
			t.Fatalf("curated Bedrock model %s costs = %f/%f/%f/%f, want %f/%f/%f/0", tt.id, model.InputCostPerMillion, model.OutputCostPerMillion, model.CacheReadInputCostPerMillion, model.CacheWriteInputCostPerMillion, tt.inputCost, tt.outputCost, tt.cacheReadCost)
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://bedrock-runtime.{region}.amazonaws.com")
		assertMetadataString(t, model.ProviderMetadata, "modelFamily", tt.modelFamily)
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"})
	}

	directBedrockModels := []struct {
		id              string
		contextWindow   int
		maxOutputTokens int
		inputCost       float64
		outputCost      float64
		cacheReadCost   float64
		cacheWriteCost  float64
		thinkingFormat  AnthropicThinkingFormat
		xhigh           string
	}{
		{id: "anthropic.claude-fable-5", contextWindow: 1000000, maxOutputTokens: 128000, inputCost: 10, outputCost: 50, cacheReadCost: 1, cacheWriteCost: 12.5, thinkingFormat: AnthropicThinkingAdaptive, xhigh: "xhigh"},
		{id: "anthropic.claude-sonnet-5", contextWindow: 1000000, maxOutputTokens: 128000, inputCost: 2, outputCost: 10, cacheReadCost: 0.2, cacheWriteCost: 2.5, thinkingFormat: AnthropicThinkingBudget},
	}
	for _, tt := range directBedrockModels {
		model, ok := registry.Model(ProviderAmazonBedrock, ModelID(tt.id))
		if !ok {
			t.Fatalf("fresh registry missing generated direct Bedrock model %s", tt.id)
		}
		if model.API != APIBedrockConverseStream || !model.SupportsTools || !model.SupportsImages() || !model.SupportsReasoning() {
			t.Fatalf("direct Bedrock model %s metadata = %+v, want Converse Stream tools, images, and reasoning", tt.id, model)
		}
		if model.ContextWindow != tt.contextWindow || model.MaxOutputTokens != tt.maxOutputTokens {
			t.Fatalf("direct Bedrock model %s limits = context %d max %d, want %d/%d", tt.id, model.ContextWindow, model.MaxOutputTokens, tt.contextWindow, tt.maxOutputTokens)
		}
		if model.InputCostPerMillion != tt.inputCost ||
			model.OutputCostPerMillion != tt.outputCost ||
			model.CacheReadInputCostPerMillion != tt.cacheReadCost ||
			model.CacheWriteInputCostPerMillion != tt.cacheWriteCost {
			t.Fatalf("direct Bedrock model %s costs = %f/%f/%f/%f, want %f/%f/%f/%f",
				tt.id,
				model.InputCostPerMillion,
				model.OutputCostPerMillion,
				model.CacheReadInputCostPerMillion,
				model.CacheWriteInputCostPerMillion,
				tt.inputCost,
				tt.outputCost,
				tt.cacheReadCost,
				tt.cacheWriteCost)
		}
		if model.AnthropicMessagesCompat == nil || model.AnthropicMessagesCompat.ThinkingFormat != tt.thinkingFormat {
			t.Fatalf("direct Bedrock model %s compat = %#v, want %s thinking", tt.id, model.AnthropicMessagesCompat, tt.thinkingFormat)
		}
		if tt.xhigh != "" {
			if got, ok := model.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != tt.xhigh {
				t.Fatalf("direct Bedrock model %s xhigh level = %q, %v; want %q, true", tt.id, got, ok, tt.xhigh)
			}
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://bedrock-runtime.{region}.amazonaws.com")
		assertMetadataString(t, model.ProviderMetadata, "modelFamily", "claude")
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"})
	}

	euBedrockModels := []struct {
		id              string
		contextWindow   int
		maxOutputTokens int
		thinkingFormat  AnthropicThinkingFormat
		xhigh           string
	}{
		{id: "eu.anthropic.claude-fable-5", contextWindow: 1000000, maxOutputTokens: 128000, thinkingFormat: AnthropicThinkingAdaptive, xhigh: "xhigh"},
		{id: "eu.anthropic.claude-haiku-4-5-20251001-v1:0", contextWindow: 200000, maxOutputTokens: 64000, thinkingFormat: AnthropicThinkingBudget},
		{id: "eu.anthropic.claude-opus-4-5-20251101-v1:0", contextWindow: 200000, maxOutputTokens: 64000, thinkingFormat: AnthropicThinkingBudget},
		{id: "eu.anthropic.claude-opus-4-6-v1", contextWindow: 1000000, maxOutputTokens: 128000, thinkingFormat: AnthropicThinkingBudget, xhigh: "max"},
		{id: "eu.anthropic.claude-opus-4-7", contextWindow: 1000000, maxOutputTokens: 128000, thinkingFormat: AnthropicThinkingBudget, xhigh: "xhigh"},
		{id: "eu.anthropic.claude-opus-4-8", contextWindow: 1000000, maxOutputTokens: 128000, thinkingFormat: AnthropicThinkingBudget, xhigh: "xhigh"},
		{id: "eu.anthropic.claude-sonnet-4-6", contextWindow: 1000000, maxOutputTokens: 64000, thinkingFormat: AnthropicThinkingBudget},
	}
	for _, tt := range euBedrockModels {
		model, ok := registry.Model(ProviderAmazonBedrock, ModelID(tt.id))
		if !ok {
			t.Fatalf("fresh registry missing generated EU Bedrock model %s", tt.id)
		}
		if model.API != APIBedrockConverseStream || !model.SupportsTools || !model.SupportsImages() || !model.SupportsReasoning() {
			t.Fatalf("EU Bedrock model %s metadata = %+v, want Converse Stream tools, images, and reasoning", tt.id, model)
		}
		if model.ContextWindow != tt.contextWindow || model.MaxOutputTokens != tt.maxOutputTokens {
			t.Fatalf("EU Bedrock model %s limits = context %d max %d, want %d/%d", tt.id, model.ContextWindow, model.MaxOutputTokens, tt.contextWindow, tt.maxOutputTokens)
		}
		if model.AnthropicMessagesCompat == nil || model.AnthropicMessagesCompat.ThinkingFormat != tt.thinkingFormat {
			t.Fatalf("EU Bedrock model %s compat = %#v, want %s thinking", tt.id, model.AnthropicMessagesCompat, tt.thinkingFormat)
		}
		if tt.xhigh != "" {
			if got, ok := model.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != tt.xhigh {
				t.Fatalf("EU Bedrock model %s xhigh level = %q, %v; want %q, true", tt.id, got, ok, tt.xhigh)
			}
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://bedrock-runtime.{region}.amazonaws.com")
		assertMetadataString(t, model.ProviderMetadata, "modelFamily", "claude")
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"})
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
	openRouterModels := []struct {
		id                    ModelID
		supportsImages        bool
		contextWindow         int
		maxOutputTokens       int
		inputCost             float64
		outputCost            float64
		cacheReadCost         float64
		cacheWriteCost        float64
		thinkingLevels        map[ThinkingLevel]string
		unsupportedLevels     []ThinkingLevel
		modelFamily           string
		routedProvider        string
		supportsDeveloperRole OpenAICompatSupport
		cacheControlFormat    OpenAICompletionsCacheControlFormat
		requiresReasoning     OpenAICompatSupport
	}{
		{
			id:                 "anthropic/claude-sonnet-5",
			supportsImages:     true,
			contextWindow:      1_000_000,
			maxOutputTokens:    128_000,
			inputCost:          2,
			outputCost:         10,
			cacheReadCost:      0.2,
			cacheWriteCost:     2.5,
			thinkingLevels:     map[ThinkingLevel]string{ThinkingLevelXHigh: "xhigh", ThinkingLevel("max"): "max"},
			modelFamily:        "claude",
			routedProvider:     "anthropic",
			cacheControlFormat: OpenAICompletionsCacheControlAnthropic,
		},
		{
			id:                    "deepseek/deepseek-v4-pro",
			contextWindow:         1_048_576,
			maxOutputTokens:       384_000,
			inputCost:             0.435,
			outputCost:            0.87,
			cacheReadCost:         0.003625,
			thinkingLevels:        map[ThinkingLevel]string{ThinkingLevelHigh: "high", ThinkingLevelXHigh: "xhigh"},
			unsupportedLevels:     []ThinkingLevel{ThinkingLevelMinimal, ThinkingLevelLow, ThinkingLevelMedium, ThinkingLevel("max")},
			modelFamily:           "deepseek",
			routedProvider:        "deepseek",
			supportsDeveloperRole: OpenAICompatUnsupported,
			requiresReasoning:     OpenAICompatSupported,
		},
		{
			id:                    "google/gemini-3.5-flash",
			supportsImages:        true,
			contextWindow:         1_048_576,
			maxOutputTokens:       65_536,
			inputCost:             1.5,
			outputCost:            9,
			cacheReadCost:         0.15,
			cacheWriteCost:        0.083333,
			unsupportedLevels:     []ThinkingLevel{ThinkingLevelOff},
			modelFamily:           "gemini",
			routedProvider:        "google",
			supportsDeveloperRole: OpenAICompatUnsupported,
		},
		{
			id:                "openai/gpt-5.2-codex",
			supportsImages:    true,
			contextWindow:     400_000,
			maxOutputTokens:   128_000,
			inputCost:         1.75,
			outputCost:        14,
			cacheReadCost:     0.175,
			thinkingLevels:    map[ThinkingLevel]string{ThinkingLevelXHigh: "xhigh"},
			unsupportedLevels: []ThinkingLevel{ThinkingLevelOff},
			modelFamily:       "gpt",
			routedProvider:    "openai",
		},
	}
	for _, tt := range openRouterModels {
		model, ok := registry.Model(ProviderOpenRouter, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated OpenRouter model %s", tt.id)
		}
		if model.API != APIOpenAICompletions || !model.SupportsTools || model.SupportsImages() != tt.supportsImages || !model.SupportsReasoning() {
			t.Fatalf("OpenRouter model %s capabilities = %+v", tt.id, model)
		}
		if model.ContextWindow != tt.contextWindow || model.MaxOutputTokens != tt.maxOutputTokens {
			t.Fatalf("OpenRouter model %s limits = %d/%d, want %d/%d", tt.id, model.ContextWindow, model.MaxOutputTokens, tt.contextWindow, tt.maxOutputTokens)
		}
		if model.InputCostPerMillion != tt.inputCost || model.OutputCostPerMillion != tt.outputCost ||
			model.CacheReadInputCostPerMillion != tt.cacheReadCost || model.CacheWriteInputCostPerMillion != tt.cacheWriteCost {
			t.Fatalf("OpenRouter model %s costs = %f/%f/%f/%f, want %f/%f/%f/%f", tt.id,
				model.InputCostPerMillion, model.OutputCostPerMillion, model.CacheReadInputCostPerMillion, model.CacheWriteInputCostPerMillion,
				tt.inputCost, tt.outputCost, tt.cacheReadCost, tt.cacheWriteCost)
		}
		if len(model.ThinkingLevelMap) != len(tt.thinkingLevels) || len(model.UnsupportedThinkingLevels) != len(tt.unsupportedLevels) {
			t.Fatalf("OpenRouter model %s thinking metadata = %#v/%#v, want %#v/%#v", tt.id, model.ThinkingLevelMap, model.UnsupportedThinkingLevels, tt.thinkingLevels, tt.unsupportedLevels)
		}
		for level, want := range tt.thinkingLevels {
			if got, ok := model.ProviderThinkingLevel(level); !ok || got != want {
				t.Fatalf("OpenRouter model %s thinking level %q = %q, %v; want %q, true", tt.id, level, got, ok, want)
			}
		}
		for index, want := range tt.unsupportedLevels {
			if model.UnsupportedThinkingLevels[index] != want {
				t.Fatalf("OpenRouter model %s unsupported thinking level %d = %q, want %q", tt.id, index, model.UnsupportedThinkingLevels[index], want)
			}
		}
		if model.OpenAICompletionsCompat == nil ||
			model.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningObject ||
			model.OpenAICompletionsCompat.SupportsDeveloperRole != tt.supportsDeveloperRole ||
			model.OpenAICompletionsCompat.CacheControlFormat != tt.cacheControlFormat ||
			model.OpenAICompletionsCompat.RequiresReasoningContentOnAssistantMessages != tt.requiresReasoning {
			t.Fatalf("OpenRouter model %s compat = %#v", tt.id, model.OpenAICompletionsCompat)
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://openrouter.ai/api/v1")
		assertMetadataString(t, model.ProviderMetadata, "modelFamily", tt.modelFamily)
		assertMetadataString(t, model.ProviderMetadata, "routedProvider", tt.routedProvider)
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENROUTER_API_KEY"})
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
	assertOpenCodeAPI(t, registry, ProviderOpenCode, "claude-fable-5", APIAnthropicMessages)
	assertOpenCodeAPI(t, registry, ProviderOpenCode, "claude-opus-4-7", APIAnthropicMessages)
	assertOpenCodeAPI(t, registry, ProviderOpenCode, "claude-sonnet-5", APIAnthropicMessages)
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
	openCodeFable, ok := registry.Model(ProviderOpenCode, "claude-fable-5")
	if !ok {
		t.Fatal("fresh registry missing generated OpenCode Zen Claude Fable model")
	}
	if openCodeFable.AnthropicMessagesCompat == nil ||
		openCodeFable.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive ||
		openCodeFable.AnthropicMessagesCompat.SupportsDisabledThinking != AnthropicCompatUnsupported {
		t.Fatalf("OpenCode Zen Fable compat = %#v, want adaptive thinking without disabled payload", openCodeFable.AnthropicMessagesCompat)
	}
	if got, ok := openCodeFable.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "xhigh" {
		t.Fatalf("OpenCode Zen Fable xhigh level = %q, %v; want xhigh, true", got, ok)
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
	openCodeGoGLM, ok := registry.Model(ProviderOpenCodeGo, "glm-5.2")
	if !ok {
		t.Fatal("fresh registry missing generated OpenCode Go GLM-5.2 model")
	}
	if !openCodeGoGLM.SupportsTools || openCodeGoGLM.SupportsImages() || !openCodeGoGLM.SupportsReasoning() {
		t.Fatalf("OpenCode Go GLM-5.2 capabilities = %+v, want text/tools/reasoning", openCodeGoGLM)
	}
	if openCodeGoGLM.ContextWindow != 1000000 || openCodeGoGLM.MaxOutputTokens != 131072 {
		t.Fatalf("OpenCode Go GLM-5.2 limits = %d/%d, want 1000000/131072", openCodeGoGLM.ContextWindow, openCodeGoGLM.MaxOutputTokens)
	}
	if openCodeGoGLM.InputCostPerMillion != 1.4 ||
		openCodeGoGLM.OutputCostPerMillion != 4.4 ||
		openCodeGoGLM.CacheReadInputCostPerMillion != 0.26 {
		t.Fatalf("OpenCode Go GLM-5.2 costs = %v/%v/%v, want 1.4/4.4/0.26", openCodeGoGLM.InputCostPerMillion, openCodeGoGLM.OutputCostPerMillion, openCodeGoGLM.CacheReadInputCostPerMillion)
	}
	if openCodeGoGLM.OpenAICompletionsCompat == nil ||
		openCodeGoGLM.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxTokens {
		t.Fatalf("OpenCode Go GLM-5.2 compat = %#v, want max_tokens", openCodeGoGLM.OpenAICompletionsCompat)
	}
	for _, level := range []ThinkingLevel{ThinkingLevelOff, ThinkingLevelMinimal, ThinkingLevelLow, ThinkingLevelMedium} {
		if openCodeGoGLM.SupportsThinkingLevel(level) {
			t.Fatalf("OpenCode Go GLM-5.2 unexpectedly supports %q thinking", level)
		}
	}
	if got, ok := openCodeGoGLM.ProviderThinkingLevel(ThinkingLevelHigh); !ok || got != "high" {
		t.Fatalf("OpenCode Go GLM-5.2 high thinking level = %q, %v; want high, true", got, ok)
	}
	if got, ok := openCodeGoGLM.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "max" {
		t.Fatalf("OpenCode Go GLM-5.2 xhigh thinking level = %q, %v; want max, true", got, ok)
	}
	assertOpenCodeAPI(t, registry, ProviderOpenCodeGo, "minimax-m2.5", APIAnthropicMessages)
	assertOpenCodeAPI(t, registry, ProviderOpenCodeGo, "minimax-m3", APIAnthropicMessages)
	assertOpenCodeAPI(t, registry, ProviderOpenCodeGo, "qwen3.7-max", APIAnthropicMessages)
	assertOpenCodeAPI(t, registry, ProviderOpenCodeGo, "qwen3.7-plus", APIAnthropicMessages)
	openCodeGoQwen, ok := registry.Model(ProviderOpenCodeGo, "qwen3.7-plus")
	if !ok {
		t.Fatal("fresh registry missing generated OpenCode Go Qwen3.7 Plus model")
	}
	if !openCodeGoQwen.SupportsTools || !openCodeGoQwen.SupportsImages() || !openCodeGoQwen.SupportsReasoning() {
		t.Fatalf("OpenCode Go Qwen3.7 Plus capabilities = %+v, want text/image/tools/reasoning", openCodeGoQwen)
	}
	if openCodeGoQwen.ContextWindow != 1000000 || openCodeGoQwen.MaxOutputTokens != 65536 {
		t.Fatalf("OpenCode Go Qwen3.7 Plus limits = %d/%d, want 1000000/65536", openCodeGoQwen.ContextWindow, openCodeGoQwen.MaxOutputTokens)
	}
	if openCodeGoQwen.InputCostPerMillion != 0.4 ||
		openCodeGoQwen.OutputCostPerMillion != 1.6 ||
		openCodeGoQwen.CacheReadInputCostPerMillion != 0.04 ||
		openCodeGoQwen.CacheWriteInputCostPerMillion != 0.5 {
		t.Fatalf("OpenCode Go Qwen3.7 Plus costs = %v/%v/%v/%v, want 0.4/1.6/0.04/0.5", openCodeGoQwen.InputCostPerMillion, openCodeGoQwen.OutputCostPerMillion, openCodeGoQwen.CacheReadInputCostPerMillion, openCodeGoQwen.CacheWriteInputCostPerMillion)
	}

	for _, id := range []ModelID{"kimi-k2.5", "kimi-k2.6", "kimi-k2.7-code"} {
		model, ok := registry.Model(ProviderOpenCodeGo, id)
		if !ok {
			t.Fatalf("fresh registry missing generated OpenCode Go Kimi model %s", id)
		}
		if model.OpenAICompletionsCompat == nil ||
			model.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningEffort {
			t.Fatalf("OpenCode Go Kimi %s compat = %#v, want reasoning effort", id, model.OpenAICompletionsCompat)
		}
		if !model.SupportsTools || !model.SupportsImages() || !model.SupportsReasoning() {
			t.Fatalf("OpenCode Go Kimi %s capabilities were not generated: %+v", id, model)
		}
	}

	openCodeGoKimi, _ := registry.Model(ProviderOpenCodeGo, "kimi-k2.6")
	if !openCodeGoKimi.SupportsThinkingLevel(ThinkingLevelLow) ||
		!openCodeGoKimi.SupportsThinkingLevel(ThinkingLevelMedium) ||
		!openCodeGoKimi.SupportsThinkingLevel(ThinkingLevelHigh) {
		t.Fatalf("OpenCode Go Kimi K2.6 thinking support = %+v / %+v, want low, medium, and high", openCodeGoKimi.ThinkingLevelMap, openCodeGoKimi.UnsupportedThinkingLevels)
	}

	openCodeGoKimiCode, _ := registry.Model(ProviderOpenCodeGo, "kimi-k2.7-code")
	if openCodeGoKimiCode.OpenAICompletionsCompat.SupportsRequiredToolChoice != OpenAICompatUnsupported {
		t.Fatalf("OpenCode Go Kimi K2.7 Code required tool choice support = %q, want unsupported", openCodeGoKimiCode.OpenAICompletionsCompat.SupportsRequiredToolChoice)
	}
	if !openCodeGoKimiCode.SupportsThinkingLevel(ThinkingLevelLow) ||
		!openCodeGoKimiCode.SupportsThinkingLevel(ThinkingLevelMedium) ||
		!openCodeGoKimiCode.SupportsThinkingLevel(ThinkingLevelHigh) {
		t.Fatalf("OpenCode Go Kimi K2.7 Code thinking support = %+v / %+v, want low, medium, and high", openCodeGoKimiCode.ThinkingLevelMap, openCodeGoKimiCode.UnsupportedThinkingLevels)
	}

	assertProviderConstantsHaveGeneratedTextMetadata(t, registry)
	assertGeneratedOpenAICompatibleProviderMetadata(t, registry)
	assertGeneratedAnthropicCompatibleProviderMetadata(t, registry)
	assertGeneratedVertexMetadata(t, registry)
	assertGeneratedNVIDIAEmbeddingMetadata(t, registry)

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

func assertGeneratedCostTiers(t *testing.T, registry *Registry) {
	t.Helper()

	tests := []struct {
		provider ProviderID
		id       ModelID
		want     ModelCostTier
	}{
		{provider: ProviderOpenAI, id: "gpt-5.4", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 5, OutputCostPerMillion: 22.5, CacheReadInputCostPerMillion: 0.5}},
		{provider: ProviderOpenAI, id: "gpt-5.4-pro", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 60, OutputCostPerMillion: 270}},
		{provider: ProviderOpenAI, id: "gpt-5.5", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 10, OutputCostPerMillion: 45, CacheReadInputCostPerMillion: 1}},
		{provider: ProviderOpenAI, id: "gpt-5.5-pro", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 60, OutputCostPerMillion: 270}},
		{provider: ProviderOpenAI, id: "gpt-5.6-luna", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 2, OutputCostPerMillion: 9, CacheReadInputCostPerMillion: 0.2, CacheWriteInputCostPerMillion: 2.5}},
		{provider: ProviderOpenAI, id: "gpt-5.6-sol", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 10, OutputCostPerMillion: 45, CacheReadInputCostPerMillion: 1, CacheWriteInputCostPerMillion: 12.5}},
		{provider: ProviderOpenAI, id: "gpt-5.6-terra", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 5, OutputCostPerMillion: 22.5, CacheReadInputCostPerMillion: 0.5, CacheWriteInputCostPerMillion: 6.25}},
		{provider: ProviderOpenAICodex, id: "gpt-5.4", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 5, OutputCostPerMillion: 22.5, CacheReadInputCostPerMillion: 0.5}},
		{provider: ProviderOpenAICodex, id: "gpt-5.5", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 10, OutputCostPerMillion: 45, CacheReadInputCostPerMillion: 1}},
		{provider: ProviderOpenAICodex, id: "gpt-5.6-luna", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 2, OutputCostPerMillion: 9, CacheReadInputCostPerMillion: 0.2, CacheWriteInputCostPerMillion: 2.5}},
		{provider: ProviderOpenAICodex, id: "gpt-5.6-sol", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 10, OutputCostPerMillion: 45, CacheReadInputCostPerMillion: 1, CacheWriteInputCostPerMillion: 12.5}},
		{provider: ProviderOpenAICodex, id: "gpt-5.6-terra", want: ModelCostTier{InputTokensAbove: 272_000, InputCostPerMillion: 5, OutputCostPerMillion: 22.5, CacheReadInputCostPerMillion: 0.5, CacheWriteInputCostPerMillion: 6.25}},
	}
	for _, tt := range tests {
		model, ok := registry.Model(tt.provider, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing %s/%s", tt.provider, tt.id)
		}
		if len(model.CostTiers) != 1 || model.CostTiers[0] != tt.want {
			t.Fatalf("%s/%s cost tiers = %#v, want %#v", tt.provider, tt.id, model.CostTiers, []ModelCostTier{tt.want})
		}
	}
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
		ProviderHuggingFace,
		ProviderKimi,
		ProviderKimiCoding,
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
		ProviderXiaomiTokenPlanAMS,
		ProviderXiaomiTokenPlanCN,
		ProviderXiaomiTokenPlanSGP,
		ProviderZAI,
		ProviderZAICodingCN,
	} {
		if !providers[provider] {
			t.Fatalf("generated text metadata missing provider %q", provider)
		}
	}
}

func assertGeneratedNVIDIAEmbeddingMetadata(t *testing.T, registry *Registry) {
	t.Helper()

	model, ok := registry.EmbeddingModel(ProviderNVIDIA, "nvidia/nv-embedqa-e5-v5")
	if !ok {
		t.Fatal("fresh registry missing generated NVIDIA embedding model")
	}
	if model.API != EmbeddingAPIOpenAIEmbeddings {
		t.Fatalf("NVIDIA embedding API = %q, want %q", model.API, EmbeddingAPIOpenAIEmbeddings)
	}
	if model.DefaultDimensions != 1024 || model.MinDimensions != 1024 || model.MaxDimensions != 1024 {
		t.Fatalf("NVIDIA embedding dimensions = %d/%d/%d, want 1024/1024/1024",
			model.DefaultDimensions,
			model.MinDimensions,
			model.MaxDimensions)
	}
	if model.MaxInputTokens != 8192 || model.MaxBatchInputs != 100 {
		t.Fatalf("NVIDIA embedding limits = tokens %d batch %d, want 8192/100", model.MaxInputTokens, model.MaxBatchInputs)
	}
	assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://integrate.api.nvidia.com/v1")
	assertMetadataString(t, model.ProviderMetadata, "modelFamily", "nvidia-embedding")
	assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"NVIDIA_API_KEY"})
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
	xiaomiUltra, ok := registry.Model(ProviderXiaomi, "mimo-v2.5-pro-ultraspeed")
	if !ok {
		t.Fatal("fresh registry missing generated Xiaomi ultraspeed model")
	}
	if xiaomiUltra.ContextWindow != 1048576 || xiaomiUltra.MaxOutputTokens != 131072 {
		t.Fatalf("Xiaomi ultraspeed limits = context %d max %d, want 1048576/131072", xiaomiUltra.ContextWindow, xiaomiUltra.MaxOutputTokens)
	}
	if xiaomiUltra.InputCostPerMillion != 1.305 ||
		xiaomiUltra.OutputCostPerMillion != 2.61 ||
		xiaomiUltra.CacheReadInputCostPerMillion != 0.0108 {
		t.Fatalf("Xiaomi ultraspeed costs = input %v output %v cache read %v, want 1.305/2.61/0.0108",
			xiaomiUltra.InputCostPerMillion,
			xiaomiUltra.OutputCostPerMillion,
			xiaomiUltra.CacheReadInputCostPerMillion)
	}

	for _, tt := range []struct {
		provider ProviderID
		baseURL  string
		envVars  []string
	}{
		{provider: ProviderXiaomiTokenPlanCN, baseURL: "https://token-plan-cn.xiaomimimo.com/v1", envVars: []string{"XIAOMI_TOKEN_PLAN_CN_API_KEY"}},
		{provider: ProviderXiaomiTokenPlanAMS, baseURL: "https://token-plan-ams.xiaomimimo.com/v1", envVars: []string{"XIAOMI_TOKEN_PLAN_AMS_API_KEY"}},
		{provider: ProviderXiaomiTokenPlanSGP, baseURL: "https://token-plan-sgp.xiaomimimo.com/v1", envVars: []string{"XIAOMI_TOKEN_PLAN_SGP_API_KEY"}},
	} {
		if _, ok := registry.Model(tt.provider, "mimo-v2-flash"); ok {
			t.Fatalf("%s should not include mimo-v2-flash", tt.provider)
		}
		tokenPlan, ok := registry.Model(tt.provider, "mimo-v2.5-pro-ultraspeed")
		if !ok {
			t.Fatalf("fresh registry missing generated %s ultraspeed model", tt.provider)
		}
		assertMetadataString(t, tokenPlan.ProviderMetadata, "baseURL", tt.baseURL)
		assertMetadataStrings(t, tokenPlan.ProviderMetadata, MetadataAPIKeyEnvVars, tt.envVars)
		if tokenPlan.OpenAICompletionsCompat == nil ||
			tokenPlan.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
			tokenPlan.OpenAICompletionsCompat.RequiresReasoningContentOnAssistantMessages != OpenAICompatSupported {
			t.Fatalf("%s compat = %#v, want deepseek reasoning content replay", tt.provider, tokenPlan.OpenAICompletionsCompat)
		}
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

	for _, tt := range []struct {
		provider ProviderID
		id       ModelID
		baseURL  string
	}{
		{provider: ProviderMoonshotAI, id: "kimi-k2.7-code", baseURL: "https://api.moonshot.ai/v1"},
		{provider: ProviderMoonshotAI, id: "kimi-k2.7-code-highspeed", baseURL: "https://api.moonshot.ai/v1"},
		{provider: ProviderMoonshotAICN, id: "kimi-k2.7-code", baseURL: "https://api.moonshot.cn/v1"},
		{provider: ProviderMoonshotAICN, id: "kimi-k2.7-code-highspeed", baseURL: "https://api.moonshot.cn/v1"},
	} {
		model, ok := registry.Model(tt.provider, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated %s %s model", tt.provider, tt.id)
		}
		if !model.SupportsTools || !model.SupportsImages() || !model.SupportsReasoning() {
			t.Fatalf("%s %s capabilities were not generated: %+v", tt.provider, tt.id, model)
		}
		if model.SupportsThinkingLevel(ThinkingLevelOff) ||
			!model.SupportsThinkingLevel(ThinkingLevelHigh) {
			t.Fatalf("%s %s thinking support = %+v / %+v, want high without off", tt.provider, tt.id, model.ThinkingLevelMap, model.UnsupportedThinkingLevels)
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", tt.baseURL)
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"MOONSHOT_API_KEY"})
		if model.OpenAICompletionsCompat == nil ||
			model.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningDeepSeek ||
			model.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported ||
			model.OpenAICompletionsCompat.SupportsStreamingUsage != OpenAICompatSupported ||
			model.OpenAICompletionsCompat.SupportsStrictTools != OpenAICompatUnsupported ||
			model.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxTokens {
			t.Fatalf("%s %s compat = %#v, want Moonshot OpenAI-compatible overrides", tt.provider, tt.id, model.OpenAICompletionsCompat)
		}
	}

	moonshotK26, ok := registry.Model(ProviderMoonshotAI, "kimi-k2.6")
	if !ok {
		t.Fatal("fresh registry missing generated Moonshot AI Kimi K2.6 model")
	}
	if !moonshotK26.SupportsThinkingLevel(ThinkingLevelOff) {
		t.Fatalf("Moonshot AI Kimi K2.6 thinking support = %+v / %+v, want off supported", moonshotK26.ThinkingLevelMap, moonshotK26.UnsupportedThinkingLevels)
	}

	for _, id := range []ModelID{
		"grok-3",
		"grok-3-fast",
		"grok-4.20-0309-non-reasoning",
		"grok-4.20-0309-reasoning",
		"grok-4.3",
		"grok-4.5",
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
		provider       ProviderID
		id             ModelID
		baseURL        string
		envVar         string
		modelFamily    string
		contextWindow  int
		maxOutput      int
		inputCost      float64
		outputCost     float64
		cacheReadCost  float64
		supportsImages bool
	}{
		{provider: ProviderCerebras, id: "gemma-4-31b", baseURL: "https://api.cerebras.ai/v1", envVar: "CEREBRAS_API_KEY", modelFamily: "gemma", contextWindow: 131072, maxOutput: 40960, inputCost: 0.99, outputCost: 1.49, supportsImages: true},
		{provider: ProviderXAI, id: "grok-4.5", baseURL: "https://api.x.ai/v1", envVar: "XAI_API_KEY", modelFamily: "grok", contextWindow: 500000, maxOutput: 500000, inputCost: 2, outputCost: 6, cacheReadCost: 0.5, supportsImages: true},
		{provider: ProviderNVIDIA, id: "minimaxai/minimax-m3", baseURL: "https://integrate.api.nvidia.com/v1", envVar: "NVIDIA_API_KEY", modelFamily: "minimax", contextWindow: 1000000, maxOutput: 16384, supportsImages: true},
		{provider: ProviderNVIDIA, id: "z-ai/glm-5.2", baseURL: "https://integrate.api.nvidia.com/v1", envVar: "NVIDIA_API_KEY", modelFamily: "glm", contextWindow: 1000000, maxOutput: 131072},
	} {
		model, ok := registry.Model(tt.provider, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated %s model %s", tt.provider, tt.id)
		}
		if model.API != APIOpenAICompletions || !model.SupportsTools || !model.SupportsReasoning() || model.SupportsImages() != tt.supportsImages {
			t.Fatalf("%s %s capabilities = %+v", tt.provider, tt.id, model)
		}
		if model.ContextWindow != tt.contextWindow || model.MaxOutputTokens != tt.maxOutput {
			t.Fatalf("%s %s limits = %d/%d, want %d/%d", tt.provider, tt.id, model.ContextWindow, model.MaxOutputTokens, tt.contextWindow, tt.maxOutput)
		}
		if model.InputCostPerMillion != tt.inputCost || model.OutputCostPerMillion != tt.outputCost || model.CacheReadInputCostPerMillion != tt.cacheReadCost {
			t.Fatalf("%s %s costs = %f/%f/%f, want %f/%f/%f", tt.provider, tt.id, model.InputCostPerMillion, model.OutputCostPerMillion, model.CacheReadInputCostPerMillion, tt.inputCost, tt.outputCost, tt.cacheReadCost)
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", tt.baseURL)
		assertMetadataString(t, model.ProviderMetadata, "modelFamily", tt.modelFamily)
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{tt.envVar})
		switch tt.provider {
		case ProviderCerebras:
			if model.OpenAICompletionsCompat != nil {
				t.Fatalf("Cerebras %s compat = %#v, want provider defaults", tt.id, model.OpenAICompletionsCompat)
			}
		case ProviderXAI:
			if model.OpenAICompletionsCompat == nil ||
				model.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported ||
				model.OpenAICompletionsCompat.SupportsStreamingUsage != OpenAICompatSupported ||
				model.OpenAICompletionsCompat.SupportsStrictTools != OpenAICompatSupported ||
				model.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxCompletionTokens {
				t.Fatalf("xAI %s compat = %#v, want xAI OpenAI-compatible overrides", tt.id, model.OpenAICompletionsCompat)
			}
		case ProviderNVIDIA:
			if model.OpenAICompletionsCompat == nil ||
				model.OpenAICompletionsCompat.SupportsStore != OpenAICompatUnsupported ||
				model.OpenAICompletionsCompat.SupportsDeveloperRole != OpenAICompatUnsupported ||
				model.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported ||
				model.OpenAICompletionsCompat.SupportsStreamingUsage != OpenAICompatSupported ||
				model.OpenAICompletionsCompat.SupportsStrictTools != OpenAICompatUnsupported ||
				model.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxTokens {
				t.Fatalf("NVIDIA %s compat = %#v, want NIM OpenAI-compatible overrides", tt.id, model.OpenAICompletionsCompat)
			}
			headers, ok := model.ProviderMetadata["headers"].(map[string]string)
			if !ok || headers["NVCF-POLL-SECONDS"] != "3600" {
				t.Fatalf("NVIDIA %s headers = %#v, want NVCF-POLL-SECONDS", tt.id, model.ProviderMetadata["headers"])
			}
		}
	}

	nvidiaSuper, ok := registry.Model(ProviderNVIDIA, "nvidia/nemotron-3-super-120b-a12b")
	if !ok {
		t.Fatal("fresh registry missing generated NVIDIA Nemotron Super model")
	}
	if nvidiaSuper.OpenAICompletionsCompat == nil ||
		nvidiaSuper.OpenAICompletionsCompat.SupportsStreamingUsage != OpenAICompatSupported ||
		nvidiaSuper.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported ||
		nvidiaSuper.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxTokens {
		t.Fatalf("NVIDIA Nemotron Super compat = %#v, want streaming usage with max_tokens", nvidiaSuper.OpenAICompletionsCompat)
	}

	nvidiaUltra, ok := registry.Model(ProviderNVIDIA, "nvidia/nemotron-3-ultra-550b-a55b")
	if !ok {
		t.Fatal("fresh registry missing generated NVIDIA Nemotron Ultra model")
	}
	if !nvidiaUltra.SupportsTools || !nvidiaUltra.SupportsReasoning() || nvidiaUltra.ContextWindow != 1000000 || nvidiaUltra.MaxOutputTokens != 65536 {
		t.Fatalf("NVIDIA Nemotron Ultra metadata = %+v, want tools, reasoning, and reviewed limits", nvidiaUltra)
	}
	if nvidiaUltra.InputCostPerMillion != 0.5 || nvidiaUltra.OutputCostPerMillion != 2.5 || nvidiaUltra.CacheReadInputCostPerMillion != 0.15 {
		t.Fatalf("NVIDIA Nemotron Ultra costs = %f/%f/%f, want 0.5/2.5/0.15",
			nvidiaUltra.InputCostPerMillion,
			nvidiaUltra.OutputCostPerMillion,
			nvidiaUltra.CacheReadInputCostPerMillion)
	}

	nvidiaGPTOSS, ok := registry.Model(ProviderNVIDIA, "openai/gpt-oss-120b")
	if !ok {
		t.Fatal("fresh registry missing generated NVIDIA GPT-OSS 120B model")
	}
	if !nvidiaGPTOSS.SupportsTools || !nvidiaGPTOSS.SupportsReasoning() || nvidiaGPTOSS.ContextWindow != 128000 || nvidiaGPTOSS.MaxOutputTokens != 8192 {
		t.Fatalf("NVIDIA GPT-OSS 120B metadata = %+v, want tools, reasoning, and reviewed limits", nvidiaGPTOSS)
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
		{provider: ProviderHuggingFace, id: "Qwen/Qwen3-Coder-480B-A35B-Instruct", baseURL: "https://router.huggingface.co/v1", envVars: []string{"HF_TOKEN"}},
		{provider: ProviderMoonshotAI, id: "kimi-k2-thinking", baseURL: "https://api.moonshot.ai/v1", envVars: []string{"MOONSHOT_API_KEY"}},
		{provider: ProviderNVIDIA, id: "openai/gpt-oss-20b", baseURL: "https://integrate.api.nvidia.com/v1", envVars: []string{"NVIDIA_API_KEY"}},
		{provider: ProviderXAI, id: "grok-3", baseURL: "https://api.x.ai/v1", envVars: []string{"XAI_API_KEY"}},
		{provider: ProviderGitHubCopilot, id: "gpt-5.2-codex", baseURL: "https://api.individual.githubcopilot.com", envVars: []string{"COPILOT_GITHUB_TOKEN"}},
		{provider: ProviderGitHubCopilot, id: "claude-sonnet-4.6", baseURL: "https://api.individual.githubcopilot.com/v1", envVars: []string{"COPILOT_GITHUB_TOKEN"}},
		{provider: ProviderGitHubCopilot, id: "claude-sonnet-5", baseURL: "https://api.individual.githubcopilot.com/v1", envVars: []string{"COPILOT_GITHUB_TOKEN"}},
		{provider: ProviderZAI, id: "glm-5.1", baseURL: "https://api.z.ai/api/coding/paas/v4", envVars: []string{"ZAI_API_KEY"}},
		{provider: ProviderZAICodingCN, id: "glm-5.2", baseURL: "https://open.bigmodel.cn/api/coding/paas/v4", envVars: []string{"ZAI_CODING_CN_API_KEY"}},
	} {
		model, ok := registry.Model(tt.provider, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated %s model %s", tt.provider, tt.id)
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", tt.baseURL)
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, tt.envVars)
	}

	for _, id := range []ModelID{"Qwen/Qwen3-Coder-480B-A35B-Instruct", "moonshotai/Kimi-K2.6", "zai-org/GLM-5.1"} {
		model, ok := registry.Model(ProviderHuggingFace, id)
		if !ok {
			t.Fatalf("fresh registry missing generated Hugging Face model %s", id)
		}
		if model.OpenAICompletionsCompat == nil ||
			model.OpenAICompletionsCompat.SupportsDeveloperRole != OpenAICompatUnsupported ||
			model.OpenAICompletionsCompat.MaxTokensField != OpenAICompletionsMaxTokens {
			t.Fatalf("Hugging Face %s compat = %#v, want developer role disabled and max_tokens", id, model.OpenAICompletionsCompat)
		}
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
	zai52, ok := registry.Model(ProviderZAI, "glm-5.2")
	if !ok {
		t.Fatal("fresh registry missing generated Z.ai GLM-5.2 model")
	}
	assertZAI52Model(t, zai52, "ZAI_API_KEY")
	zaiCN52, ok := registry.Model(ProviderZAICodingCN, "glm-5.2")
	if !ok {
		t.Fatal("fresh registry missing generated Z.ai Coding CN GLM-5.2 model")
	}
	assertZAI52Model(t, zaiCN52, "ZAI_CODING_CN_API_KEY")

	cloudflare, ok := registry.Model(ProviderCloudflareAIGateway, "gpt-5.4")
	if !ok {
		t.Fatal("fresh registry missing generated Cloudflare AI Gateway model")
	}
	if cloudflare.API != APIOpenAIResponses || !cloudflare.SupportsTools || !cloudflare.SupportsImages() || !cloudflare.SupportsReasoning() {
		t.Fatalf("Cloudflare AI Gateway model capabilities = %+v, want Responses tools, images, and reasoning", cloudflare)
	}
	assertMetadataString(t, cloudflare.ProviderMetadata, "baseURL", "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai")
	assertMetadataStrings(t, cloudflare.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"CLOUDFLARE_API_KEY"})

	cloudflareAnthropic, ok := registry.Model(ProviderCloudflareAIGateway, "claude-sonnet-4-6")
	if !ok {
		t.Fatal("fresh registry missing generated Cloudflare AI Gateway Anthropic model")
	}
	if cloudflareAnthropic.API != APIAnthropicMessages || !cloudflareAnthropic.SupportsTools || !cloudflareAnthropic.SupportsImages() || !cloudflareAnthropic.SupportsReasoning() {
		t.Fatalf("Cloudflare AI Gateway Anthropic model capabilities = %+v, want Messages tools, images, and reasoning", cloudflareAnthropic)
	}
	assertMetadataString(t, cloudflareAnthropic.ProviderMetadata, "baseURL", "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic/v1")
	assertMetadataStrings(t, cloudflareAnthropic.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"CLOUDFLARE_API_KEY"})

	for _, tt := range []struct {
		id              ModelID
		contextWindow   int
		maxOutputTokens int
		inputCost       float64
		outputCost      float64
		xhigh           string
	}{
		{id: "claude-fable-5", contextWindow: 1000000, maxOutputTokens: 128000, inputCost: 10, outputCost: 50, xhigh: "xhigh"},
		{id: "claude-sonnet-5", contextWindow: 1000000, maxOutputTokens: 128000, inputCost: 2, outputCost: 10},
	} {
		model, ok := registry.Model(ProviderCloudflareAIGateway, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated Cloudflare AI Gateway Anthropic model %s", tt.id)
		}
		if model.API != APIAnthropicMessages || !model.SupportsTools || !model.SupportsImages() || !model.SupportsReasoning() {
			t.Fatalf("Cloudflare AI Gateway Anthropic model %s metadata = %+v, want Messages tools, images, and reasoning", tt.id, model)
		}
		if model.AnthropicMessagesCompat == nil || model.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive {
			t.Fatalf("Cloudflare AI Gateway Anthropic model %s compat = %#v, want adaptive thinking", tt.id, model.AnthropicMessagesCompat)
		}
		if model.ContextWindow != tt.contextWindow || model.MaxOutputTokens != tt.maxOutputTokens {
			t.Fatalf("Cloudflare AI Gateway Anthropic model %s limits = %d/%d, want %d/%d", tt.id, model.ContextWindow, model.MaxOutputTokens, tt.contextWindow, tt.maxOutputTokens)
		}
		if model.InputCostPerMillion != tt.inputCost || model.OutputCostPerMillion != tt.outputCost {
			t.Fatalf("Cloudflare AI Gateway Anthropic model %s costs = %f/%f, want %f/%f", tt.id, model.InputCostPerMillion, model.OutputCostPerMillion, tt.inputCost, tt.outputCost)
		}
		if tt.xhigh != "" {
			if got, ok := model.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != tt.xhigh {
				t.Fatalf("Cloudflare AI Gateway Anthropic model %s xhigh level = %q, %v; want %q, true", tt.id, got, ok, tt.xhigh)
			}
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic/v1")
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"CLOUDFLARE_API_KEY"})
	}

	copilotSonnet5, ok := registry.Model(ProviderGitHubCopilot, "claude-sonnet-5")
	if !ok {
		t.Fatal("fresh registry missing generated GitHub Copilot Claude Sonnet 5 model")
	}
	if copilotSonnet5.API != APIAnthropicMessages || !copilotSonnet5.SupportsTools || !copilotSonnet5.SupportsImages() || !copilotSonnet5.SupportsReasoning() {
		t.Fatalf("GitHub Copilot Claude Sonnet 5 metadata = %+v, want Anthropic Messages tools, images, and reasoning", copilotSonnet5)
	}
	if copilotSonnet5.AnthropicMessagesCompat == nil || copilotSonnet5.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive {
		t.Fatalf("GitHub Copilot Claude Sonnet 5 compat = %#v, want adaptive thinking", copilotSonnet5.AnthropicMessagesCompat)
	}
	if copilotSonnet5.ContextWindow != 1000000 || copilotSonnet5.MaxOutputTokens != 128000 {
		t.Fatalf("GitHub Copilot Claude Sonnet 5 limits = %d/%d, want 1000000/128000", copilotSonnet5.ContextWindow, copilotSonnet5.MaxOutputTokens)
	}
	copilotFable, ok := registry.Model(ProviderGitHubCopilot, "claude-fable-5")
	if !ok {
		t.Fatal("fresh registry missing generated GitHub Copilot Claude Fable 5 model")
	}
	if copilotFable.API != APIOpenAICompletions || !copilotFable.SupportsTools || !copilotFable.SupportsImages() || !copilotFable.SupportsReasoning() {
		t.Fatalf("GitHub Copilot Claude Fable 5 metadata = %+v, want Chat Completions tools, images, and reasoning", copilotFable)
	}
	if copilotFable.ContextWindow != 1000000 || copilotFable.MaxOutputTokens != 128000 ||
		copilotFable.InputCostPerMillion != 10 || copilotFable.OutputCostPerMillion != 50 ||
		copilotFable.CacheReadInputCostPerMillion != 1 || copilotFable.CacheWriteInputCostPerMillion != 12.5 {
		t.Fatalf("GitHub Copilot Claude Fable 5 metadata = %+v, want reviewed limits and costs", copilotFable)
	}
	if copilotFable.OpenAICompletionsCompat == nil ||
		copilotFable.OpenAICompletionsCompat.SupportsStore != OpenAICompatUnsupported ||
		copilotFable.OpenAICompletionsCompat.SupportsDeveloperRole != OpenAICompatUnsupported ||
		copilotFable.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported {
		t.Fatalf("GitHub Copilot Claude Fable 5 compat = %#v, want conservative Chat Completions support", copilotFable.OpenAICompletionsCompat)
	}
	for level, want := range map[ThinkingLevel]string{ThinkingLevelXHigh: "xhigh", ThinkingLevel("max"): "max"} {
		if got, ok := copilotFable.ProviderThinkingLevel(level); !ok || got != want {
			t.Fatalf("GitHub Copilot Claude Fable 5 thinking level %s = %q, %v; want %q, true", level, got, ok, want)
		}
	}
	if copilotFable.SupportsThinkingLevel(ThinkingLevelOff) {
		t.Fatalf("GitHub Copilot Claude Fable 5 thinking support = %+v / %+v, want disabled thinking unsupported", copilotFable.ThinkingLevelMap, copilotFable.UnsupportedThinkingLevels)
	}
	assertMetadataString(t, copilotFable.ProviderMetadata, "baseURL", "https://api.individual.githubcopilot.com")
	assertMetadataStrings(t, copilotFable.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"COPILOT_GITHUB_TOKEN"})

	for _, tt := range []struct {
		id              ModelID
		imageCapable    bool
		maxOutputTokens int
		inputCost       float64
		outputCost      float64
		cacheReadCost   float64
		modelFamily     string
	}{
		{id: "kimi-k2.7-code", imageCapable: true, maxOutputTokens: 32000, inputCost: 0.95, outputCost: 4, cacheReadCost: 0.19, modelFamily: "kimi"},
		{id: "mai-code-1-flash-picker", imageCapable: false, maxOutputTokens: 128000, inputCost: 0.75, outputCost: 4.5, cacheReadCost: 0.075, modelFamily: "mai"},
	} {
		model, ok := registry.Model(ProviderGitHubCopilot, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated GitHub Copilot model %s", tt.id)
		}
		if model.API != APIOpenAICompletions || !model.SupportsTools || model.SupportsImages() != tt.imageCapable || !model.SupportsReasoning() {
			t.Fatalf("GitHub Copilot %s capabilities = %+v, want Chat Completions tools, images %v, and reasoning", tt.id, model, tt.imageCapable)
		}
		if model.ContextWindow != 256000 || model.MaxOutputTokens != tt.maxOutputTokens ||
			model.InputCostPerMillion != tt.inputCost || model.OutputCostPerMillion != tt.outputCost ||
			model.CacheReadInputCostPerMillion != tt.cacheReadCost || model.CacheWriteInputCostPerMillion != 0 {
			t.Fatalf("GitHub Copilot %s metadata = %+v, want reviewed limits and costs", tt.id, model)
		}
		if model.OpenAICompletionsCompat == nil ||
			model.OpenAICompletionsCompat.SupportsStore != OpenAICompatUnsupported ||
			model.OpenAICompletionsCompat.SupportsDeveloperRole != OpenAICompatUnsupported ||
			model.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatUnsupported {
			t.Fatalf("GitHub Copilot %s compat = %#v, want conservative Chat Completions support", tt.id, model.OpenAICompletionsCompat)
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://api.individual.githubcopilot.com")
		assertMetadataString(t, model.ProviderMetadata, "modelFamily", tt.modelFamily)
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"COPILOT_GITHUB_TOKEN"})
		headers, ok := model.ProviderMetadata["headers"].(map[string]string)
		if !ok || headers["Copilot-Integration-Id"] != "vscode-chat" {
			t.Fatalf("GitHub Copilot %s headers = %#v, want Copilot headers", tt.id, model.ProviderMetadata["headers"])
		}
	}

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
	if codex.API != APIOpenAICodexResponses || codex.OpenAICodexResponses == nil ||
		codex.OpenAICodexResponses.Model != "gpt-5.4" || !codex.OpenAICodexResponses.SupportsToolSearch {
		t.Fatalf("OpenAI Codex metadata = %+v, want Codex Responses model mapping", codex)
	}
	assertMetadataStrings(t, codex.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"OPENAI_CODEX_OAUTH_TOKEN"})

	for _, id := range []ModelID{
		"gpt-5.4", "gpt-5.4-mini", "gpt-5.4-pro", "gpt-5.5",
		"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra",
	} {
		responses, ok := registry.Model(ProviderOpenAI, id)
		if !ok {
			t.Fatalf("fresh registry missing generated OpenAI Responses model %s", id)
		}
		if responses.OpenAIResponsesCompat == nil || !responses.OpenAIResponsesCompat.SupportsToolSearch {
			t.Fatalf("OpenAI Responses %s metadata = %#v, want tool search enabled", id, responses.OpenAIResponsesCompat)
		}
	}
	for _, id := range []ModelID{
		"gpt-5.4", "gpt-5.4-mini", "gpt-5.5",
		"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra",
	} {
		responses, ok := registry.Model(ProviderOpenAICodex, id)
		if !ok {
			t.Fatalf("fresh registry missing generated OpenAI Codex Responses model %s", id)
		}
		if responses.OpenAICodexResponses == nil || !responses.OpenAICodexResponses.SupportsToolSearch {
			t.Fatalf("OpenAI Codex Responses %s metadata = %#v, want tool search enabled", id, responses.OpenAICodexResponses)
		}
	}

	for _, tt := range []struct {
		provider ProviderID
		id       ModelID
	}{
		{provider: ProviderOpenAI, id: "gpt-5.3-codex-spark"},
		{provider: ProviderOpenAICodex, id: "gpt-5.3-codex-spark"},
	} {
		model, ok := registry.Model(tt.provider, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing %s/%s", tt.provider, tt.id)
		}
		if model.OpenAIResponsesCompat != nil && model.OpenAIResponsesCompat.SupportsToolSearch {
			t.Fatalf("%s/%s Responses compatibility = %#v, want tool search disabled", tt.provider, tt.id, model.OpenAIResponsesCompat)
		}
		if model.OpenAICodexResponses != nil && model.OpenAICodexResponses.SupportsToolSearch {
			t.Fatalf("%s/%s Codex compatibility = %#v, want tool search disabled", tt.provider, tt.id, model.OpenAICodexResponses)
		}
	}

	for _, id := range []ModelID{
		"claude-fable-5", "claude-opus-4-5", "claude-opus-4-5-20251101",
		"claude-opus-4-6", "claude-opus-4-7", "claude-opus-4-8",
		"claude-sonnet-4-5", "claude-sonnet-4-5-20250929", "claude-sonnet-4-6", "claude-sonnet-5",
	} {
		directAnthropic, ok := registry.Model(ProviderAnthropic, id)
		if !ok {
			t.Fatalf("fresh registry missing direct Anthropic model %s", id)
		}
		if directAnthropic.AnthropicMessagesCompat == nil ||
			directAnthropic.AnthropicMessagesCompat.SupportsToolReferences != AnthropicCompatSupported {
			t.Fatalf("direct Anthropic %s compatibility = %#v, want tool references enabled", id, directAnthropic.AnthropicMessagesCompat)
		}
	}
	for _, tt := range []struct {
		provider ProviderID
		id       ModelID
	}{
		{provider: ProviderAnthropic, id: "claude-haiku-4-5"},
		{provider: ProviderGitHubCopilot, id: "claude-sonnet-5"},
	} {
		model, ok := registry.Model(tt.provider, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing %s/%s", tt.provider, tt.id)
		}
		if model.AnthropicMessagesCompat != nil &&
			model.AnthropicMessagesCompat.SupportsToolReferences == AnthropicCompatSupported {
			t.Fatalf("%s/%s compatibility = %#v, want tool references disabled", tt.provider, tt.id, model.AnthropicMessagesCompat)
		}
	}
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

	for _, tt := range []struct {
		id         ModelID
		wantImages bool
	}{
		{id: "k2p7", wantImages: true},
		{id: "kimi-for-coding", wantImages: true},
		{id: "kimi-k2-thinking"},
	} {
		kimiCoding, ok := registry.Model(ProviderKimiCoding, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated Kimi Coding model %q", tt.id)
		}
		if kimiCoding.API != APIAnthropicMessages || !kimiCoding.SupportsTools || !kimiCoding.SupportsReasoning() {
			t.Fatalf("Kimi Coding model %q capabilities were not generated: %+v", tt.id, kimiCoding)
		}
		if got := kimiCoding.SupportsImages(); got != tt.wantImages {
			t.Fatalf("Kimi Coding model %q SupportsImages() = %v, want %v", tt.id, got, tt.wantImages)
		}
		if kimiCoding.AnthropicMessagesCompat == nil ||
			kimiCoding.AnthropicMessagesCompat.SupportsSessionAffinity != AnthropicCompatSupported ||
			kimiCoding.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive {
			t.Fatalf("Kimi Coding model %q compat = %#v, want adaptive Anthropic-compatible metadata", tt.id, kimiCoding.AnthropicMessagesCompat)
		}
		assertMetadataString(t, kimiCoding.ProviderMetadata, "baseURL", "https://api.kimi.com/coding/v1")
		assertMetadataStrings(t, kimiCoding.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"KIMI_API_KEY"})
		headers, ok := kimiCoding.ProviderMetadata["headers"].(map[string]string)
		if !ok {
			t.Fatalf("Kimi Coding model %q headers metadata type = %T, want map[string]string", tt.id, kimiCoding.ProviderMetadata["headers"])
		}
		if got, want := headers["User-Agent"], "KimiCLI/1.5"; got != want {
			t.Fatalf("Kimi Coding model %q User-Agent metadata = %q, want %q", tt.id, got, want)
		}
	}

	minimax, ok := registry.Model(ProviderMiniMax, "MiniMax-M3")
	if !ok {
		t.Fatal("fresh registry missing generated MiniMax model")
	}
	if minimax.API != APIAnthropicMessages || !minimax.SupportsTools || !minimax.SupportsReasoning() {
		t.Fatalf("MiniMax model capabilities were not generated: %+v", minimax)
	}
	assertMetadataString(t, minimax.ProviderMetadata, "baseURL", "https://api.minimax.io/anthropic/v1")
	assertMetadataStrings(t, minimax.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"MINIMAX_API_KEY"})

	minimaxCN, ok := registry.Model(ProviderMiniMaxCN, "MiniMax-M3")
	if !ok {
		t.Fatal("fresh registry missing generated MiniMax CN model")
	}
	if minimaxCN.API != APIAnthropicMessages || !minimaxCN.SupportsTools || !minimaxCN.SupportsImages() || !minimaxCN.SupportsReasoning() {
		t.Fatalf("MiniMax CN model capabilities were not generated: %+v", minimaxCN)
	}
	assertMetadataString(t, minimaxCN.ProviderMetadata, "baseURL", "https://api.minimaxi.com/anthropic/v1")
	assertMetadataStrings(t, minimaxCN.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"MINIMAX_CN_API_KEY"})

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
	assertMetadataString(t, vercel.ProviderMetadata, "baseURL", "https://ai-gateway.vercel.sh/v1")
	assertMetadataStrings(t, vercel.ProviderMetadata, MetadataAPIKeyEnvVars, []string{defaultVercelAIGatewayKeyEnv})

	for _, tt := range []struct {
		id              ModelID
		contextWindow   int
		maxOutputTokens int
		xhigh           string
	}{
		{id: "anthropic/claude-fable-5", contextWindow: 1000000, maxOutputTokens: 128000, xhigh: "xhigh"},
		{id: "anthropic/claude-sonnet-5", contextWindow: 1000000, maxOutputTokens: 128000},
	} {
		model, ok := registry.Model(ProviderVercelAIGateway, tt.id)
		if !ok {
			t.Fatalf("fresh registry missing generated Vercel AI Gateway model %s", tt.id)
		}
		if model.API != APIAnthropicMessages || !model.SupportsTools || !model.SupportsImages() || !model.SupportsReasoning() {
			t.Fatalf("Vercel AI Gateway model %s capabilities were not generated: %+v", tt.id, model)
		}
		if model.AnthropicMessagesCompat == nil || model.AnthropicMessagesCompat.ThinkingFormat != AnthropicThinkingAdaptive {
			t.Fatalf("Vercel AI Gateway model %s compat = %#v, want adaptive thinking", tt.id, model.AnthropicMessagesCompat)
		}
		if model.ContextWindow != tt.contextWindow || model.MaxOutputTokens != tt.maxOutputTokens {
			t.Fatalf("Vercel AI Gateway model %s limits = %d/%d, want %d/%d", tt.id, model.ContextWindow, model.MaxOutputTokens, tt.contextWindow, tt.maxOutputTokens)
		}
		if tt.xhigh != "" {
			if got, ok := model.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != tt.xhigh {
				t.Fatalf("Vercel AI Gateway model %s xhigh level = %q, %v; want %q, true", tt.id, got, ok, tt.xhigh)
			}
		}
		assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://ai-gateway.vercel.sh/v1")
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{defaultVercelAIGatewayKeyEnv})
	}
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

func assertZAI52Model(t *testing.T, model Model, envVar string) {
	t.Helper()

	if model.ContextWindow != 1000000 || model.MaxOutputTokens != 131072 {
		t.Fatalf("Z.ai GLM-5.2 limits = %d/%d, want 1000000/131072", model.ContextWindow, model.MaxOutputTokens)
	}
	if model.OpenAICompletionsCompat == nil ||
		model.OpenAICompletionsCompat.ReasoningFormat != OpenAICompletionsReasoningZAI ||
		model.OpenAICompletionsCompat.SupportsToolStream != OpenAICompatSupported ||
		model.OpenAICompletionsCompat.SupportsReasoningEffort != OpenAICompatSupported {
		t.Fatalf("Z.ai GLM-5.2 compat = %#v, want zai reasoning, reasoning_effort, and tool_stream", model.OpenAICompletionsCompat)
	}
	assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{envVar})
	if got, ok := model.ProviderThinkingLevel(ThinkingLevelMinimal); !ok || got != "" {
		t.Fatalf("Z.ai GLM-5.2 minimal level = %q, %v; want empty string, true", got, ok)
	}
	if got, ok := model.ProviderThinkingLevel(ThinkingLevelHigh); !ok || got != "high" {
		t.Fatalf("Z.ai GLM-5.2 high level = %q, %v; want high, true", got, ok)
	}
	if got, ok := model.ProviderThinkingLevel(ThinkingLevelXHigh); !ok || got != "max" {
		t.Fatalf("Z.ai GLM-5.2 xhigh level = %q, %v; want max, true", got, ok)
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
