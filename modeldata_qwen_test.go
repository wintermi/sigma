// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"reflect"
	"sort"
	"testing"
)

// Token Plan Individual on the China endpoint serves the same model list as
// the international Individual plan, from the China gateway and with its own
// key variable.
func TestQwenTokenPlanIndividualCNMirrorsIndividualOnChinaGateway(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatal(err)
	}
	ids := func(provider ProviderID) []string {
		var out []string
		for _, model := range registry.ListModels() {
			if model.Provider == provider {
				out = append(out, string(model.ID))
			}
		}
		sort.Strings(out)
		return out
	}
	international, china := ids(ProviderQwenTokenPlanIndividual), ids(ProviderQwenTokenPlanIndividualCN)
	if len(china) == 0 || !reflect.DeepEqual(international, china) {
		t.Fatalf("Individual CN models = %v, want %v", china, international)
	}
	for _, id := range china {
		model, _ := registry.Model(ProviderQwenTokenPlanIndividualCN, ModelID(id))
		assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://token-plan.maas.qianwenaiapi.com/compatible-mode/v1")
		assertMetadataStrings(t, model.ProviderMetadata, MetadataAPIKeyEnvVars, []string{"QWEN_TOKEN_PLAN_INDIVIDUAL_CN_API_KEY", "QWEN_TOKEN_PLAN_CN_API_KEY"})
	}
}
