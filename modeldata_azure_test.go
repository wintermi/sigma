// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import "testing"

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
