// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

const defaultCostCurrency = "USD"

// Total returns provider-supplied total tokens when available, otherwise it
// computes a deterministic total from input, output, and prompt-cache token
// fields. ThinkingTokens is reported separately and should be included in
// OutputTokens by providers when it is billable as output.
//
// Streaming providers that only receive usage at stream end should leave
// interim Event.Usage nil and attach the final Usage to the terminal
// AssistantMessage. The terminal event will then expose the same final usage.
func (usage Usage) Total() int {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.InputTokens +
		usage.OutputTokens +
		usage.CacheReadInputTokens +
		usage.CacheWriteInputTokens
}

// CostForUsage calculates deterministic per-turn cost from model rates.
//
// Model cost rates are expressed as currency units per one million tokens.
// CostCurrency records the rate currency; when empty, USD is assumed. The
// helper does not round so callers can choose their own display precision.
func CostForUsage(model Model, usage Usage) Cost {
	inputCost := costForTokens(usage.InputTokens, model.InputCostPerMillion)
	outputCost := costForTokens(usage.OutputTokens, model.OutputCostPerMillion)
	cacheReadCost := costForTokens(usage.CacheReadInputTokens, model.CacheReadInputCostPerMillion)
	cacheWriteCost := costForTokens(usage.CacheWriteInputTokens, model.CacheWriteInputCostPerMillion)

	return Cost{
		InputCost:           inputCost,
		OutputCost:          outputCost,
		CacheReadInputCost:  cacheReadCost,
		CacheWriteInputCost: cacheWriteCost,
		TotalCost:           inputCost + outputCost + cacheReadCost + cacheWriteCost,
		Currency:            costCurrency(model),
	}
}

// CostForEmbeddingUsage calculates deterministic embedding request cost from model rates.
func CostForEmbeddingUsage(model EmbeddingModel, usage Usage) Cost {
	inputCost := costForTokens(usage.InputTokens, model.InputCostPerMillion)
	return Cost{
		InputCost: inputCost,
		TotalCost: inputCost,
		Currency:  embeddingCostCurrency(model),
	}
}

func costForTokens(tokens int, perMillion float64) float64 {
	if tokens == 0 || perMillion == 0 {
		return 0
	}
	return float64(tokens) * perMillion / 1_000_000
}

func costCurrency(model Model) string {
	if model.CostCurrency != "" {
		return model.CostCurrency
	}
	return defaultCostCurrency
}

func embeddingCostCurrency(model EmbeddingModel) string {
	if model.CostCurrency != "" {
		return model.CostCurrency
	}
	return defaultCostCurrency
}
