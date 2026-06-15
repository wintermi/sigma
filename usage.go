// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import "encoding/json"

const defaultCostCurrency = "USD"

const (
	rawCostKey     = "cost"
	rawCurrencyKey = "currency"
)

type usageAccountingConfig struct {
	raw                      any
	providerReportedCost     *float64
	providerReportedCurrency string
	estimatedCostAdjustment  func(*Cost)
}

// UsageAccountingOption configures AccountUsage.
type UsageAccountingOption func(*usageAccountingConfig)

// WithRawUsage preserves the provider usage payload as JSON-like debug data.
func WithRawUsage(raw any) UsageAccountingOption {
	return func(config *usageAccountingConfig) {
		config.raw = raw
	}
}

// WithProviderReportedCost records a provider-reported cost separately from
// Sigma's model-metadata estimate.
func WithProviderReportedCost(cost float64, currency string) UsageAccountingOption {
	return func(config *usageAccountingConfig) {
		config.providerReportedCost = cloneFloat64Ptr(&cost)
		config.providerReportedCurrency = currency
	}
}

// WithEstimatedCostAdjustment adjusts Sigma's estimated cost after model
// pricing has been applied. Providers use this for request-specific pricing
// modifiers such as service tiers.
func WithEstimatedCostAdjustment(adjust func(*Cost)) UsageAccountingOption {
	return func(config *usageAccountingConfig) {
		config.estimatedCostAdjustment = adjust
	}
}

// AccountUsage stamps usage with model identity, preserves raw provider usage,
// and calculates Sigma's estimated cost from model metadata.
func AccountUsage(model Model, usage Usage, opts ...UsageAccountingOption) (Usage, Cost) {
	config := usageAccountingConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}

	usage.Provider = model.Provider
	usage.Model = model.ID
	if raw := usageRawMap(config.raw); raw != nil {
		usage.Raw = raw
	} else {
		usage.Raw = usageRawMap(usage.Raw)
	}

	cost := CostForUsage(model, usage)
	if config.providerReportedCost != nil {
		reported := *config.providerReportedCost
		cost.ProviderReportedCost = &reported
		cost.ProviderReportedCurrency = config.providerReportedCurrency
	} else if reported, currency, ok := providerReportedCostFromRaw(usage.Raw); ok {
		cost.ProviderReportedCost = &reported
		cost.ProviderReportedCurrency = currency
	}
	if config.estimatedCostAdjustment != nil {
		config.estimatedCostAdjustment(&cost)
	}
	return usage, cost
}

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
		usage.CacheWriteInputTokens +
		usage.ToolUseInputTokens
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
	shortCacheWriteTokens := usage.CacheWriteInputTokens - usage.LongCacheWriteInputTokens
	if shortCacheWriteTokens < 0 {
		shortCacheWriteTokens = 0
	}
	cacheWriteCost := costForTokens(shortCacheWriteTokens, model.CacheWriteInputCostPerMillion) +
		costForTokens(usage.LongCacheWriteInputTokens, model.InputCostPerMillion*2)

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

func usageRawMap(raw any) map[string]any {
	switch typed := raw.(type) {
	case nil:
		return nil
	case map[string]any:
		return cloneUsageRawMap(typed)
	default:
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			return nil
		}
		return cloneUsageRawMap(decoded)
	}
}

func cloneUsageRawMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneUsageRawValue(value)
	}
	return cloned
}

func cloneUsageRawValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneUsageRawMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i, value := range typed {
			cloned[i] = cloneUsageRawValue(value)
		}
		return cloned
	default:
		return typed
	}
}

func providerReportedCostFromRaw(raw map[string]any) (float64, string, bool) {
	if len(raw) == 0 {
		return 0, "", false
	}
	for _, key := range []string{rawCostKey, "total_cost", "totalCost"} {
		cost, ok := numericRawValue(raw[key])
		if ok {
			return cost, rawCurrency(raw), true
		}
	}
	return 0, "", false
}

func numericRawValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case json.Number:
		value, err := typed.Float64()
		return value, err == nil
	default:
		return 0, false
	}
}

func rawCurrency(raw map[string]any) string {
	for _, key := range []string{rawCurrencyKey, "cost_currency", "costCurrency"} {
		if value, ok := raw[key].(string); ok {
			return value
		}
	}
	return ""
}
