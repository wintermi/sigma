// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"reflect"
	"testing"
)

// Azure requests use the /openai/v1 path, whose api-version accepts only "v1"
// (the default) or "preview"; dated versions belong to the legacy route.
func TestGeneratedAzureRowsUseV1APIVersion(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, model := range registry.ListModels() {
		if model.Provider != ProviderAzureOpenAIResponses {
			continue
		}
		checked++
		if model.AzureOpenAIResponses == nil || model.AzureOpenAIResponses.APIVersion != "v1" {
			t.Errorf("%s api version = %#v, want v1", model.ID, model.AzureOpenAIResponses)
		}
	}
	if checked == 0 {
		t.Fatal("no Azure rows checked")
	}
}

// Azure bills the same long-context tiers as OpenAI for the same model, so
// rows must not drop them: long prompts would be under-costed.
func TestGeneratedAzureRowsKeepOpenAICostTiers(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, model := range registry.ListModels() {
		if model.Provider != ProviderAzureOpenAIResponses {
			continue
		}
		direct, ok := registry.Model(ProviderOpenAI, model.ID)
		if !ok || len(direct.CostTiers) == 0 || direct.InputCostPerMillion != model.InputCostPerMillion {
			continue
		}
		checked++
		if !reflect.DeepEqual(model.CostTiers, direct.CostTiers) {
			t.Errorf("azure %s tiers = %+v, want OpenAI tiers %+v", model.ID, model.CostTiers, direct.CostTiers)
		}
	}
	if checked == 0 {
		t.Fatal("no Azure rows checked")
	}
}
