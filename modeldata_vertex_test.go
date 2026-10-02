// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"math"
	"testing"
)

func TestVertexCatalogLongContextAccounting(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		id        ModelID
		at, above float64
	}{
		{"gemini-2.5-pro", .24875, .4925025},
		{"gemini-3.1-pro-preview", .394, .782004},
		{"gemini-3.1-pro-preview-customtools", .394, .782004},
	} {
		t.Run(string(tt.id), func(t *testing.T) {
			t.Parallel()
			model, ok := GetModel(ProviderGoogleVertex, tt.id)
			if !ok {
				t.Fatal("missing model")
			}
			// Cached tokens participate in the threshold; the higher tier applies to
			// the whole request only after 200,000 input tokens.
			for _, extra := range []int{0, 1} {
				cost := CostForUsage(model, Usage{InputTokens: 190000 + extra, CacheReadInputTokens: 10000, OutputTokens: 1000})
				want := tt.at
				if extra == 1 {
					want = tt.above
				}
				if math.Abs(cost.TotalCost-want) > 1e-10 {
					t.Fatalf("extra=%d cost=%g, want %g", extra, cost.TotalCost, want)
				}
			}
		})
	}
	for _, id := range []ModelID{"claude-opus-4-6", "claude-opus-4-7", "claude-opus-4-8", "claude-sonnet-4-6", "claude-opus-5-5", "claude-sonnet-5-5"} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()
			model, ok := GetModel(ProviderGoogleVertexAnthropic, id)
			if !ok {
				t.Fatal("missing model")
			}
			usage := Usage{InputTokens: 300000, CacheReadInputTokens: 10000, CacheWriteInputTokens: 2000, OutputTokens: 1000}
			cost := CostForUsage(model, usage)
			want := .3*model.InputCostPerMillion + .01*model.CacheReadInputCostPerMillion + .002*model.CacheWriteInputCostPerMillion + .001*model.OutputCostPerMillion
			if math.Abs(cost.TotalCost-want) > 1e-10 {
				t.Fatalf("unexpected long-context premium: %g, want %g", cost.TotalCost, want)
			}
		})
	}
}

// Catalog removals are scoped to Vertex discovery, not to model identifiers
// on other services or the ability to register caller-owned metadata.
func TestVertexCatalogCleanupBoundaries(t *testing.T) {
	t.Parallel()
	registry := DefaultRegistry()
	for _, id := range []ModelID{"text-embedding-004", "text-embedding-005", "text-multilingual-embedding-002"} {
		if _, ok := registry.EmbeddingModel(ProviderGoogleVertex, id); ok {
			t.Errorf("removed Vertex embedding remains discoverable: %s", id)
		}
	}
	if _, ok := registry.ImageModel(ProviderGoogleVertex, "gemini-3.1-flash-lite-image"); ok {
		t.Error("removed Vertex image remains discoverable")
	}
	if _, ok := registry.ImageModel(ProviderOpenRouter, "google/gemini-3.1-flash-lite-image"); !ok {
		t.Error("Vertex cleanup removed the independent OpenRouter image route")
	}
	if _, ok := registry.EmbeddingModel(ProviderGoogleVertex, "gemini-embedding-001"); !ok {
		t.Error("retained Vertex embedding missing")
	}
	for _, id := range []ModelID{"claude-sonnet-4", "claude-sonnet-4@20250514", "claude-opus-5"} {
		if _, ok := registry.Model(ProviderGoogleVertexAnthropic, id); !ok {
			t.Errorf("cleanup removed a represented Claude model or alias: %s", id)
		}
	}
}
