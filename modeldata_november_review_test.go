// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import "testing"

// New rows copy a sibling's thinking metadata, so check each one follows its
// own model's controls rather than the sibling's.
func TestNovemberCatalogReviewThinkingControls(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		provider    ProviderID
		id          ModelID
		off         bool
		offValue    string
		unsupported []ThinkingLevel
	}{
		// Haiku 5.5 can disable thinking; Sonnet 5.5 and Fable 5.2 cannot.
		{provider: ProviderAnthropic, id: "claude-haiku-5-5", off: true},
		{provider: ProviderAnthropic, id: "claude-sonnet-5-5", unsupported: []ThinkingLevel{ThinkingLevelMinimal}},
		{provider: ProviderAnthropic, id: "claude-fable-5-2"},
		{provider: ProviderAmazonBedrock, id: "global.anthropic.claude-haiku-5-5", off: true},
		{provider: ProviderAmazonBedrock, id: "us.anthropic.claude-opus-5", off: true},
		{provider: ProviderGoogleVertexAnthropic, id: "claude-fable-5-2"},
		// GPT-6.1 Terra accepts a none effort; the Sol and Astra releases do not.
		{provider: ProviderOpenAI, id: "gpt-6.1-terra", off: true, offValue: "none"},
		{provider: ProviderOpenAI, id: "gpt-6.1-sol", unsupported: []ThinkingLevel{ThinkingLevelMinimal}},
		{provider: ProviderOpenAI, id: "gpt-6.2-astra"},
		{provider: ProviderOpenAI, id: "gpt-6.2-sol"},
		// Gemini thinkingLevel has no xhigh value to send.
		{provider: ProviderGoogle, id: "gemini-4-neon", unsupported: []ThinkingLevel{ThinkingLevelMinimal, ThinkingLevelXHigh}},
		{provider: ProviderGoogleVertex, id: "gemini-4-hydrogen", unsupported: []ThinkingLevel{ThinkingLevelXHigh}},
		// Mistral Large 4 takes low, medium, or high effort but no none.
		{provider: ProviderMistral, id: "mistral-large-4", unsupported: []ThinkingLevel{ThinkingLevelXHigh}},
		{provider: ProviderGroq, id: "qwen/qwen3.8-27b", off: true, offValue: "none", unsupported: []ThinkingLevel{ThinkingLevelXHigh}},
		{provider: ProviderCerebras, id: "qwen-3.8-27b", off: true, offValue: "none", unsupported: []ThinkingLevel{ThinkingLevelXHigh}},
	} {
		model, ok := registry.Model(tt.provider, tt.id)
		if !ok {
			t.Fatalf("registry missing %s/%s", tt.provider, tt.id)
		}
		if got := model.SupportsThinkingLevel(ThinkingLevelOff); got != tt.off {
			t.Errorf("%s/%s supports off = %v, want %v", tt.provider, tt.id, got, tt.off)
		}
		if got := model.ThinkingLevelMap[ThinkingLevelOff]; got != tt.offValue {
			t.Errorf("%s/%s off value = %q, want %q", tt.provider, tt.id, got, tt.offValue)
		}
		for _, level := range tt.unsupported {
			if model.SupportsThinkingLevel(level) {
				t.Errorf("%s/%s unexpectedly supports %s", tt.provider, tt.id, level)
			}
		}
	}
}

// Sigma keeps direct OpenAI rows at a 272K window so requests stay below the
// long-context price tier unless callers raise the limit themselves.
func TestNovemberCatalogReviewKeepsDirectOpenAIWindow(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatal(err)
	}
	for _, id := range []ModelID{"gpt-6.1-sol", "gpt-6.1-terra", "gpt-6.2-astra", "gpt-6.2-sol"} {
		model, ok := registry.Model(ProviderOpenAI, id)
		if !ok {
			t.Fatalf("registry missing openai/%s", id)
		}
		if model.ContextWindow != 272_000 {
			t.Errorf("openai/%s context window = %d, want 272000", id, model.ContextWindow)
		}
		if len(model.CostTiers) == 0 || model.CostTiers[0].InputTokensAbove != 272_000 {
			t.Errorf("openai/%s cost tiers = %+v, want a tier above 272000 input tokens", id, model.CostTiers)
		}
	}
}
