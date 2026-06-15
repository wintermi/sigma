// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
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
	}

	if got, want := usage.Total(), 600; got != want {
		t.Fatalf("total tokens = %d, want computed total %d", got, want)
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

func TestCostForUsagePricesLongCacheWritesAtInputMultiplier(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		InputCostPerMillion:           5,
		CacheWriteInputCostPerMillion: 1,
	}
	usage := sigma.Usage{
		CacheWriteInputTokens:     1_000_000,
		LongCacheWriteInputTokens: 400_000,
	}

	cost := sigma.CostForUsage(model, usage)
	if got, want := cost.CacheWriteInputCost, 4.6; got != want {
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
