// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"strconv"
	"unicode/utf8"
)

const (
	estimateCharsPerToken       = 4
	estimateImageTokens         = 1200
	estimateDocumentTokens      = 1200
	estimateContextSafetyTokens = 4096
)

// TokenEstimate reports an approximate request token count.
type TokenEstimate struct {
	Tokens                int  `json:"tokens"`
	UsageTokens           int  `json:"usageTokens,omitempty"`
	TrailingTokens        int  `json:"trailingTokens,omitempty"`
	LastUsageMessageIndex *int `json:"lastUsageMessageIndex,omitempty"`
}

// ReasoningBudget reports an opt-in output and thinking budget plan.
type ReasoningBudget struct {
	MaxTokens            int `json:"maxTokens,omitempty"`
	ThinkingBudgetTokens int `json:"thinkingBudgetTokens,omitempty"`
}

// EstimateTextTokens returns a deterministic approximate token count for text.
func EstimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	characters := utf8.RuneCountInString(text)
	return (characters + estimateCharsPerToken - 1) / estimateCharsPerToken
}

// EstimateContentTokens returns a deterministic approximate token count for
// message content blocks.
func EstimateContentTokens(blocks []ContentBlock) int {
	tokens := 0
	for _, block := range blocks {
		switch block.Type {
		case ContentBlockText:
			tokens += EstimateTextTokens(block.Text)
		case ContentBlockThinking:
			tokens += EstimateTextTokens(block.ThinkingText)
		case ContentBlockImage:
			tokens += estimateImageTokens
		case ContentBlockDocument:
			tokens += estimateDocumentTokens
		case ContentBlockToolCall:
			tokens += EstimateTextTokens(block.ToolName)
			tokens += EstimateTextTokens(stableEstimateJSON(block.ToolArguments))
		}
	}
	return tokens
}

// EstimateMessageTokens returns a deterministic approximate token count for a
// persisted message.
func EstimateMessageTokens(message Message) int {
	return EstimateContentTokens(message.Content)
}

// EstimateRequestTokens returns a deterministic approximate token count for a
// request.
//
// When the latest successful assistant message carries provider-reported usage,
// the estimate uses that usage as the context anchor and estimates only
// messages after it. Otherwise it estimates the whole request from the system
// prompt, tools, and messages.
func EstimateRequestTokens(req Request) TokenEstimate {
	if usageTokens, index, ok := latestUsageAnchor(req.Messages); ok {
		trailingTokens := 0
		for _, message := range req.Messages[index+1:] {
			trailingTokens += EstimateMessageTokens(message)
		}
		return TokenEstimate{
			Tokens:                usageTokens + trailingTokens,
			UsageTokens:           usageTokens,
			TrailingTokens:        trailingTokens,
			LastUsageMessageIndex: intPtr(index),
		}
	}

	tokens := EstimateTextTokens(req.SystemPrompt)
	if len(req.Tools) > 0 {
		tokens += EstimateTextTokens(stableEstimateJSON(req.Tools))
	}
	for _, message := range req.Messages {
		tokens += EstimateMessageTokens(message)
	}
	return TokenEstimate{Tokens: tokens, TrailingTokens: tokens}
}

// MaxTokensForContext returns an opt-in max output token cap for req and model.
//
// requestedMaxTokens is used when positive, clamped to model.MaxOutputTokens
// when the catalog reports one; otherwise model.MaxOutputTokens is used. A
// zero return means no usable output cap was available. The helper uses
// EstimateRequestTokens and a fixed safety margin; it does not call provider
// tokenizers or affect dispatch unless the caller applies the returned value.
func MaxTokensForContext(model Model, req Request, requestedMaxTokens int) int {
	maxTokens := requestedMaxTokens
	if maxTokens <= 0 {
		maxTokens = model.MaxOutputTokens
	}
	if maxTokens <= 0 {
		return 0
	}
	if model.MaxOutputTokens > 0 && maxTokens > model.MaxOutputTokens {
		maxTokens = model.MaxOutputTokens
	}
	if model.ContextWindow <= 0 {
		return maxTokens
	}

	available := model.ContextWindow - EstimateRequestTokens(req).Tokens - estimateContextSafetyTokens
	if available < 1 {
		available = 1
	}
	if available < maxTokens {
		return available
	}
	return maxTokens
}

// WithMaxTokensForContext configures MaxTokens from MaxTokensForContext.
//
// If MaxTokensForContext returns zero, this option leaves MaxTokens unset.
func WithMaxTokensForContext(model Model, req Request, requestedMaxTokens int) Option {
	return func(options *Options) {
		if maxTokens := MaxTokensForContext(model, req, requestedMaxTokens); maxTokens > 0 {
			options.MaxTokens = intPtr(maxTokens)
		}
	}
}

// ReasoningBudgetForContext returns an opt-in max output and thinking budget
// plan for req, model, and level.
//
// requestedMaxTokens is treated as the caller's desired visible output cap when
// positive; otherwise model.MaxOutputTokens is used. Non-off reasoning levels
// reserve a thinking budget inside the final max token cap while preserving at
// least 1024 visible output tokens when possible. The helper uses
// EstimateRequestTokens and a fixed safety margin; it does not call provider
// tokenizers or affect dispatch unless the caller applies the returned values.
func ReasoningBudgetForContext(model Model, req Request, level ThinkingLevel, requestedMaxTokens int) ReasoningBudget {
	maxTokens := requestedMaxTokens
	if maxTokens <= 0 {
		maxTokens = model.MaxOutputTokens
	}
	if maxTokens <= 0 {
		return ReasoningBudget{}
	}

	thinkingBudget := defaultThinkingBudget(model, level)
	if thinkingBudget > 0 {
		maxTokens += thinkingBudget
	}
	if model.MaxOutputTokens > 0 && maxTokens > model.MaxOutputTokens {
		maxTokens = model.MaxOutputTokens
	}
	if model.ContextWindow > 0 {
		available := model.ContextWindow - EstimateRequestTokens(req).Tokens - estimateContextSafetyTokens
		if available < 1 {
			available = 1
		}
		if available < maxTokens {
			maxTokens = available
		}
	}
	if thinkingBudget > 0 {
		thinkingBudget = minInt(thinkingBudget, maxInt(0, maxTokens-1024))
	}
	return ReasoningBudget{
		MaxTokens:            maxTokens,
		ThinkingBudgetTokens: thinkingBudget,
	}
}

// WithReasoningBudgetForContext configures ReasoningLevel, MaxTokens, and
// ThinkingBudgetTokens from ReasoningBudgetForContext.
//
// If ReasoningBudgetForContext returns zero values, this option only applies
// the requested reasoning level.
func WithReasoningBudgetForContext(model Model, req Request, level ThinkingLevel, requestedMaxTokens int) Option {
	return func(options *Options) {
		options.ReasoningLevel = level
		budget := ReasoningBudgetForContext(model, req, level, requestedMaxTokens)
		if budget.MaxTokens > 0 {
			options.MaxTokens = intPtr(budget.MaxTokens)
		}
		if budget.ThinkingBudgetTokens > 0 {
			options.ThinkingBudgetTokens = intPtr(budget.ThinkingBudgetTokens)
		}
	}
}

func defaultThinkingBudget(model Model, level ThinkingLevel) int {
	if level == "" || level == ThinkingLevelOff {
		return 0
	}
	if value, ok := model.ProviderThinkingLevel(level); ok {
		if tokens, err := strconv.Atoi(value); err == nil && tokens > 0 {
			return tokens
		}
	}
	switch level {
	case ThinkingLevelMinimal:
		return 1024
	case ThinkingLevelLow:
		return 2048
	case ThinkingLevelMedium:
		return 8192
	case ThinkingLevelHigh, ThinkingLevelXHigh:
		return 16384
	default:
		return 0
	}
}

func latestUsageAnchor(messages []Message) (int, int, bool) {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role != RoleAssistant || message.Usage == nil {
			continue
		}
		if message.StopReason == StopReasonError || message.StopReason == StopReasonAborted {
			continue
		}
		if tokens := message.Usage.Total(); tokens > 0 {
			return tokens, index, true
		}
	}
	return 0, 0, false
}

func stableEstimateJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "[unserializable]"
	}
	return string(encoded)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
