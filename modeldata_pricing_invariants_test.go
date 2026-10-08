// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"math"
	"strings"
	"testing"
)

func roundedCost(value float64) float64 {
	return math.Round(value*1e6) / 1e6
}

// AWS prices geographic Claude inference profiles at 1.1x the global profile.
func TestBedrockRegionalClaudeProfilesCarryRegionalPremium(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, model := range registry.ListModels() {
		id := string(model.ID)
		geographic := false
		for _, prefix := range []string{"us.", "eu.", "jp.", "au.", "apac."} {
			geographic = geographic || strings.HasPrefix(id, prefix)
		}
		if model.Provider != ProviderAmazonBedrock || !strings.Contains(id, "anthropic.claude") || !geographic {
			continue
		}
		suffix := id[strings.Index(id, "anthropic.claude"):]
		global, ok := registry.Model(ProviderAmazonBedrock, ModelID("global."+suffix))
		if !ok {
			continue
		}
		checked++
		for _, pair := range [][2]float64{
			{model.InputCostPerMillion, global.InputCostPerMillion},
			{model.OutputCostPerMillion, global.OutputCostPerMillion},
			{model.CacheReadInputCostPerMillion, global.CacheReadInputCostPerMillion},
			{model.CacheWriteInputCostPerMillion, global.CacheWriteInputCostPerMillion},
		} {
			if roundedCost(pair[0]) != roundedCost(pair[1]*1.1) {
				t.Errorf("%s cost %v, want 1.1x global %v", id, pair[0], pair[1])
			}
		}
	}
	if checked == 0 {
		t.Fatal("no regional Bedrock Claude profiles checked")
	}
}

// Mistral bills cached prompt tokens at a tenth of the input rate. The listed
// rows have no published cached rate in the reference catalog.
func TestMistralRowsPriceCachedInput(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatal(err)
	}
	withoutCachedRate := map[ModelID]bool{"magistral-small": true, "mistral-medium-3.5": true}
	for _, model := range registry.ListModels() {
		if model.Provider == ProviderMistral && model.InputCostPerMillion > 0 && !withoutCachedRate[model.ID] && roundedCost(model.CacheReadInputCostPerMillion) != roundedCost(model.InputCostPerMillion/10) {
			t.Errorf("%s cache read = %v, want %v", model.ID, model.CacheReadInputCostPerMillion, model.InputCostPerMillion/10)
		}
	}
}

func TestDirectMiniMaxM3PricingAndLimits(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []ProviderID{ProviderMiniMax, ProviderMiniMaxCN} {
		model, ok := registry.Model(provider, "MiniMax-M3")
		if !ok {
			t.Fatalf("missing %s/MiniMax-M3", provider)
		}
		want := ModelCostTier{InputTokensAbove: 512_000, InputCostPerMillion: 0.6, OutputCostPerMillion: 2.4, CacheReadInputCostPerMillion: 0.12}
		if model.InputCostPerMillion != 0.3 || model.OutputCostPerMillion != 1.2 || model.CacheReadInputCostPerMillion != 0.06 ||
			len(model.CostTiers) != 1 || model.CostTiers[0] != want ||
			model.ContextWindow != 1_000_000 || model.MaxOutputTokens != 512_000 {
			t.Fatalf("%s/MiniMax-M3 = base %v/%v/%v tiers %#v context %d output %d", provider,
				model.InputCostPerMillion, model.OutputCostPerMillion, model.CacheReadInputCostPerMillion, model.CostTiers, model.ContextWindow, model.MaxOutputTokens)
		}
	}
}
