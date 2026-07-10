// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"math"
	"testing"

	"github.com/wintermi/sigma"
)

func TestUsageTotalPrefersProviderTotal(t *testing.T) {
	t.Parallel()

	usage := sigma.Usage{
		InputTokens:           10,
		OutputTokens:          20,
		CacheReadInputTokens:  30,
		CacheWriteInputTokens: 40,
		TotalTokens:           99,
	}

	if got, want := usage.Total(), 99; got != want {
		t.Fatalf("total tokens = %d, want provider total %d", got, want)
	}
}

func TestUsageTotalIncludesCacheTokensWhenProviderTotalIsMissing(t *testing.T) {
	t.Parallel()

	usage := sigma.Usage{
		InputTokens:               100,
		OutputTokens:              25,
		CacheReadInputTokens:      400,
		CacheWriteInputTokens:     75,
		LongCacheWriteInputTokens: 25,
		ToolUseInputTokens:        10,
	}

	if got, want := usage.Total(), 610; got != want {
		t.Fatalf("total tokens = %d, want computed total %d", got, want)
	}
}

func TestAccountUsageStampsModelAndClonesRawUsage(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"input_tokens":  float64(10),
		"nested":        map[string]any{"cached_tokens": float64(3)},
		"token_details": []any{map[string]any{"reasoning_tokens": float64(2)}},
	}
	usage, cost := sigma.AccountUsage(sigma.Model{
		ID:                   "model-a",
		Provider:             "provider-a",
		InputCostPerMillion:  1,
		OutputCostPerMillion: 2,
	}, sigma.Usage{
		InputTokens:  1_000_000,
		OutputTokens: 500_000,
	}, sigma.WithRawUsage(raw))

	raw["input_tokens"] = float64(99)
	raw["nested"].(map[string]any)["cached_tokens"] = float64(99)
	raw["token_details"].([]any)[0].(map[string]any)["reasoning_tokens"] = float64(99)

	if got, want := usage.Provider, sigma.ProviderID("provider-a"); got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if got, want := usage.Model, sigma.ModelID("model-a"); got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
	if got, want := usage.Raw["input_tokens"], float64(10); got != want {
		t.Fatalf("raw input tokens = %v, want %v", got, want)
	}
	nested := usage.Raw["nested"].(map[string]any)
	if got, want := nested["cached_tokens"], float64(3); got != want {
		t.Fatalf("raw nested cached tokens = %v, want %v", got, want)
	}
	details := usage.Raw["token_details"].([]any)[0].(map[string]any)
	if got, want := details["reasoning_tokens"], float64(2); got != want {
		t.Fatalf("raw reasoning tokens = %v, want %v", got, want)
	}
	if got, want := cost.TotalCost, 2.0; got != want {
		t.Fatalf("estimated total cost = %v, want %v", got, want)
	}
}

func TestAccountUsagePreservesProviderReportedCost(t *testing.T) {
	t.Parallel()

	_, cost := sigma.AccountUsage(sigma.Model{
		ID:           "model-a",
		Provider:     "provider-a",
		CostCurrency: "EUR",
	}, sigma.Usage{}, sigma.WithProviderReportedCost(0.125, "USD"))

	if cost.ProviderReportedCost == nil {
		t.Fatal("provider reported cost was nil")
	}
	if got, want := *cost.ProviderReportedCost, 0.125; got != want {
		t.Fatalf("provider reported cost = %v, want %v", got, want)
	}
	if got, want := cost.ProviderReportedCurrency, "USD"; got != want {
		t.Fatalf("provider reported currency = %q, want %q", got, want)
	}
	if got, want := cost.Currency, "EUR"; got != want {
		t.Fatalf("estimated currency = %q, want %q", got, want)
	}
}

func TestAccountUsageReadsProviderReportedCostFromRawUsage(t *testing.T) {
	t.Parallel()

	_, cost := sigma.AccountUsage(sigma.Model{}, sigma.Usage{
		Raw: map[string]any{
			"total_cost": float64(0.25),
			"currency":   "USD",
		},
	})

	if cost.ProviderReportedCost == nil {
		t.Fatal("provider reported cost was nil")
	}
	if got, want := *cost.ProviderReportedCost, 0.25; got != want {
		t.Fatalf("provider reported cost = %v, want %v", got, want)
	}
	if got, want := cost.ProviderReportedCurrency, "USD"; got != want {
		t.Fatalf("provider reported currency = %q, want %q", got, want)
	}
}

func TestCostForUsageStandardAndCacheHeavyUsage(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		InputCostPerMillion:           3,
		OutputCostPerMillion:          15,
		CacheReadInputCostPerMillion:  0.3,
		CacheWriteInputCostPerMillion: 3.75,
	}
	usage := sigma.Usage{
		InputTokens:           1_000_000,
		OutputTokens:          2_000_000,
		CacheReadInputTokens:  3_000_000,
		CacheWriteInputTokens: 4_000_000,
	}

	cost := sigma.CostForUsage(model, usage)
	if got, want := cost.InputCost, 3.0; got != want {
		t.Fatalf("input cost = %v, want %v", got, want)
	}
	if got, want := cost.OutputCost, 30.0; got != want {
		t.Fatalf("output cost = %v, want %v", got, want)
	}
	if got, want := cost.CacheReadInputCost, 0.9; got != want {
		t.Fatalf("cache read cost = %v, want %v", got, want)
	}
	if got, want := cost.CacheWriteInputCost, 15.0; got != want {
		t.Fatalf("cache write cost = %v, want %v", got, want)
	}
	if got, want := cost.TotalCost, 48.9; got != want {
		t.Fatalf("total cost = %v, want %v", got, want)
	}
	if got, want := cost.Currency, "USD"; got != want {
		t.Fatalf("currency = %q, want %q", got, want)
	}
}

func TestCostForUsageUsesHighestExceededTier(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		InputCostPerMillion:           1,
		OutputCostPerMillion:          2,
		CacheReadInputCostPerMillion:  0.1,
		CacheWriteInputCostPerMillion: 0.5,
		CostTiers: []sigma.ModelCostTier{
			{
				InputTokensAbove:              272_000,
				InputCostPerMillion:           2,
				OutputCostPerMillion:          3,
				CacheReadInputCostPerMillion:  0.2,
				CacheWriteInputCostPerMillion: 1,
			},
			{
				InputTokensAbove:              400_000,
				InputCostPerMillion:           4,
				OutputCostPerMillion:          5,
				CacheReadInputCostPerMillion:  0.4,
				CacheWriteInputCostPerMillion: 2,
			},
		},
	}

	tests := []struct {
		name  string
		usage sigma.Usage
		want  float64
	}{
		{
			name:  "below threshold",
			usage: sigma.Usage{InputTokens: 271_999, OutputTokens: 1_000_000},
			want:  2.271999,
		},
		{
			name:  "at threshold",
			usage: sigma.Usage{InputTokens: 272_000, OutputTokens: 1_000_000},
			want:  2.272,
		},
		{
			name:  "cache input crosses threshold",
			usage: sigma.Usage{InputTokens: 272_000, CacheReadInputTokens: 1, OutputTokens: 1_000_000},
			want:  3.5440002,
		},
		{
			name:  "highest matching tier",
			usage: sigma.Usage{InputTokens: 401_000, OutputTokens: 1_000_000},
			want:  6.604,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := sigma.CostForUsage(model, tt.usage).TotalCost; math.Abs(got-tt.want) > 1e-12 {
				t.Fatalf("total cost = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCostForUsagePricesLongCacheWritesAtInputMultiplier(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		InputCostPerMillion:           5,
		CacheWriteInputCostPerMillion: 1,
		CostTiers: []sigma.ModelCostTier{{
			InputTokensAbove:              0,
			InputCostPerMillion:           6,
			CacheWriteInputCostPerMillion: 1.5,
		}},
	}
	usage := sigma.Usage{
		CacheWriteInputTokens:     1_000_000,
		LongCacheWriteInputTokens: 400_000,
	}

	cost := sigma.CostForUsage(model, usage)
	if got, want := cost.CacheWriteInputCost, 5.7; got != want {
		t.Fatalf("cache write cost = %v, want %v", got, want)
	}
	if got, want := usage.Total(), 1_000_000; got != want {
		t.Fatalf("total tokens = %d, want cache write total %d", got, want)
	}
}

func TestCostForUsageHandlesMissingCacheFields(t *testing.T) {
	t.Parallel()

	cost := sigma.CostForUsage(sigma.Model{
		InputCostPerMillion:           1,
		OutputCostPerMillion:          2,
		CacheReadInputCostPerMillion:  100,
		CacheWriteInputCostPerMillion: 100,
		CostCurrency:                  "EUR",
	}, sigma.Usage{
		InputTokens:  500_000,
		OutputTokens: 250_000,
	})

	if got, want := cost.TotalCost, 1.0; got != want {
		t.Fatalf("total cost = %v, want %v", got, want)
	}
	if got, want := cost.Currency, "EUR"; got != want {
		t.Fatalf("currency = %q, want %q", got, want)
	}
}

func TestCostForUsageZeroCostModel(t *testing.T) {
	t.Parallel()

	cost := sigma.CostForUsage(sigma.Model{}, sigma.Usage{
		InputTokens:           1_000_000,
		OutputTokens:          1_000_000,
		CacheReadInputTokens:  1_000_000,
		CacheWriteInputTokens: 1_000_000,
	})

	if cost.InputCost != 0 || cost.OutputCost != 0 || cost.CacheReadInputCost != 0 ||
		cost.CacheWriteInputCost != 0 || cost.TotalCost != 0 {
		t.Fatalf("zero-cost model produced non-zero cost: %+v", cost)
	}
}
