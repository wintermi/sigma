// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"fmt"
	"strings"
)

// Classifier dimension names accepted by WithRouteWeight.
const (
	RouteDimensionReasoningMarkers   = "reasoningMarkers"
	RouteDimensionTechnicalTerms     = "technicalTerms"
	RouteDimensionSimpleIndicators   = "simpleIndicators"
	RouteDimensionCodePresence       = "codePresence"
	RouteDimensionMultiStepPatterns  = "multiStepPatterns"
	RouteDimensionQuestionComplexity = "questionComplexity"
	RouteDimensionTokenCount         = "tokenCount"
)

// Default dimension weights. Content signals drive routing; tokenCount is
// disabled by default so accumulated agentic context does not bias every
// late-turn request toward heavier tiers.
const (
	defaultRouteWeightReasoningMarkers   = 0.40
	defaultRouteWeightTechnicalTerms     = 0.25
	defaultRouteWeightSimpleIndicators   = 0.20
	defaultRouteWeightCodePresence       = 0.10
	defaultRouteWeightMultiStepPatterns  = 0.03
	defaultRouteWeightQuestionComplexity = 0.02
	defaultRouteWeightTokenCount         = 0.0
)

// Default ascending tier score boundaries.
const (
	defaultRouteBoundarySimpleStandard   = 0.10
	defaultRouteBoundaryStandardComplex  = 0.25
	defaultRouteBoundaryComplexReasoning = 0.55
)

// Token thresholds used when the tokenCount dimension is enabled.
const (
	routeTokenSimpleThreshold  = 50
	routeTokenComplexThreshold = 500
)

// RouteSignal is one scored classifier dimension. Score is the raw dimension
// score in [-1, 1] before weighting.
type RouteSignal struct {
	Dimension string  `json:"dimension"`
	Score     float64 `json:"score"`
	Weight    float64 `json:"weight"`
	Detail    string  `json:"detail,omitempty"`
}

// RouteClassification is a deterministic complexity classification. Score is
// the weighted sum of all signals; Signals lists non-zero contributions for
// observability.
type RouteClassification struct {
	Tier    RouteTier     `json:"tier"`
	Score   float64       `json:"score"`
	Signals []RouteSignal `json:"signals,omitempty"`
}

// ClassifyRequest classifies req into a route tier using weighted rule-based
// scoring. Classification is pure and deterministic: no model calls, no
// randomness, and the same request always classifies to the same tier.
//
// Only the system prompt and the latest user message are scored, so prior
// conversation turns and tool results do not drift long agentic sessions into
// heavier tiers. Reasoning markers are scored against the user message alone
// so a system prompt cannot force every request into the reasoning tier. Two
// or more reasoning markers in the user message classify directly as the
// reasoning tier unless the dimension weight is zero.
func ClassifyRequest(req Request, opts ...RouteOption) RouteClassification {
	return classifyRouteRequest(req, newRouteConfig(opts...))
}

func classifyRouteRequest(req Request, config routeConfig) RouteClassification {
	userText := strings.ToLower(lastRouteUserText(req.Messages))
	combinedText := strings.ToLower(strings.TrimSpace(req.SystemPrompt + "\n" + userText))

	signals := []RouteSignal{
		scoreRouteReasoningMarkers(userText),
		scoreRouteTechnicalTerms(combinedText),
		scoreRouteSimpleIndicators(userText),
		scoreRouteCodePresence(combinedText),
		scoreRouteMultiStepPatterns(combinedText),
		scoreRouteQuestionComplexity(userText),
		scoreRouteTokenCount(req, config),
	}

	classification := RouteClassification{}
	forceReasoning := false
	for _, signal := range signals {
		signal.Weight = config.weights[signal.Dimension]
		if signal.Dimension == RouteDimensionReasoningMarkers &&
			signal.Score >= 1.0 && signal.Weight > 0 {
			forceReasoning = true
		}
		if signal.Score == 0 || signal.Weight == 0 {
			continue
		}
		classification.Score += signal.Score * signal.Weight
		classification.Signals = append(classification.Signals, signal)
	}
	classification.Tier = routeTierForScore(classification.Score, config)
	if forceReasoning && routeTierIndex(classification.Tier) < routeTierIndex(RouteTierReasoning) {
		classification.Tier = RouteTierReasoning
	}
	return classification
}

func routeTierForScore(score float64, config routeConfig) RouteTier {
	switch {
	case score < config.boundarySimpleStandard:
		return RouteTierSimple
	case score < config.boundaryStandardComplex:
		return RouteTierStandard
	case score < config.boundaryComplexReasoning:
		return RouteTierComplex
	default:
		return RouteTierReasoning
	}
}

// lastRouteUserText returns the concatenated text blocks of the latest user
// message. Tool results, assistant turns, and developer messages are ignored.
func lastRouteUserText(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != RoleUser || messages[i].ToolCallID != "" {
			continue
		}
		var builder strings.Builder
		for _, block := range messages[i].Content {
			if block.Type == ContentBlockText {
				builder.WriteString(block.Text)
				builder.WriteString("\n")
			}
		}
		return builder.String()
	}
	return ""
}

var routeReasoningMarkers = []string{
	"step by step",
	"step-by-step",
	"think through",
	"think carefully",
	"reason about",
	"chain of thought",
	"walk me through",
	"walk through",
	"explain why",
	"explain your reasoning",
	"from first principles",
	"trade-off",
	"tradeoff",
	"pros and cons",
	"compare and contrast",
	"root cause",
	"prove",
	"derive",
}

func scoreRouteReasoningMarkers(userText string) RouteSignal {
	matches := countRouteKeywords(userText, routeReasoningMarkers)
	signal := RouteSignal{Dimension: RouteDimensionReasoningMarkers}
	switch {
	case matches >= 2:
		signal.Score = 1.0
	case matches == 1:
		signal.Score = 0.6
	}
	if matches > 0 {
		signal.Detail = fmt.Sprintf("%d reasoning markers", matches)
	}
	return signal
}

var routeTechnicalTerms = []string{
	"algorithm",
	"architecture",
	"concurrency",
	"distributed",
	"kubernetes",
	"microservice",
	"latency",
	"throughput",
	"encryption",
	"authentication",
	"database",
	"schema",
	"compiler",
	"runtime",
	"memory leak",
	"race condition",
	"deadlock",
	"optimization",
	"optimisation",
	"scalability",
	"protocol",
	"regression",
	"benchmark",
	"vulnerability",
	"migration",
	"transaction",
	"idempotent",
	"serialization",
	"observability",
}

func scoreRouteTechnicalTerms(text string) RouteSignal {
	matches := countRouteKeywords(text, routeTechnicalTerms)
	signal := RouteSignal{Dimension: RouteDimensionTechnicalTerms}
	if matches > 0 {
		signal.Score = min(1.0, float64(matches)*0.34)
		signal.Detail = fmt.Sprintf("%d technical terms", matches)
	}
	return signal
}

var routeSimpleIndicators = []string{
	"what is",
	"what's",
	"what are",
	"who is",
	"when was",
	"when did",
	"where is",
	"how many",
	"define",
	"definition of",
	"meaning of",
	"list the",
	"translate",
	"summarize",
	"summarise",
	"tl;dr",
	"hello",
	"hi there",
	"thanks",
	"thank you",
}

func scoreRouteSimpleIndicators(userText string) RouteSignal {
	signal := RouteSignal{Dimension: RouteDimensionSimpleIndicators}
	if countRouteKeywords(userText, routeSimpleIndicators) > 0 {
		signal.Score = -1.0
		signal.Detail = "simple indicator present"
	}
	return signal
}

var routeCodeKeywords = []string{
	"func",
	"function",
	"class",
	"struct",
	"interface",
	"async",
	"import",
	"return",
	"implement",
	"refactor",
	"unit test",
	"stack trace",
	"compile",
	"regex",
	"endpoint",
	"json",
	"yaml",
	"sql",
}

func scoreRouteCodePresence(text string) RouteSignal {
	signal := RouteSignal{Dimension: RouteDimensionCodePresence}
	if strings.Contains(text, "```") {
		signal.Score = 1.0
		signal.Detail = "code fence present"
		return signal
	}
	matches := countRouteKeywords(text, routeCodeKeywords)
	if matches > 0 {
		signal.Score = min(1.0, float64(matches)*0.5)
		signal.Detail = fmt.Sprintf("%d code keywords", matches)
	}
	return signal
}

var routeMultiStepPatterns = []string{
	"step 1",
	"step one",
	"and then",
	"after that",
	"followed by",
	"finally",
}

func scoreRouteMultiStepPatterns(text string) RouteSignal {
	signal := RouteSignal{Dimension: RouteDimensionMultiStepPatterns}
	matched := countRouteKeywords(text, routeMultiStepPatterns) > 0 ||
		(containsRouteKeyword(text, "first") && containsRouteKeyword(text, "then"))
	if matched {
		signal.Score = 1.0
		signal.Detail = "multi-step pattern present"
	}
	return signal
}

func scoreRouteQuestionComplexity(userText string) RouteSignal {
	signal := RouteSignal{Dimension: RouteDimensionQuestionComplexity}
	questions := strings.Count(userText, "?")
	switch {
	case questions >= 3:
		signal.Score = 1.0
	case questions == 2:
		signal.Score = 0.5
	}
	if signal.Score != 0 {
		signal.Detail = fmt.Sprintf("%d questions", questions)
	}
	return signal
}

func scoreRouteTokenCount(req Request, config routeConfig) RouteSignal {
	signal := RouteSignal{Dimension: RouteDimensionTokenCount}
	if config.weights[RouteDimensionTokenCount] == 0 {
		return signal
	}
	tokens := EstimateRequestTokens(req).Tokens
	switch {
	case tokens < routeTokenSimpleThreshold:
		signal.Score = -1.0
		signal.Detail = fmt.Sprintf("short (%d estimated tokens)", tokens)
	case tokens > routeTokenComplexThreshold:
		signal.Score = 1.0
		signal.Detail = fmt.Sprintf("long (%d estimated tokens)", tokens)
	}
	return signal
}

func countRouteKeywords(text string, keywords []string) int {
	if text == "" {
		return 0
	}
	matches := 0
	for _, keyword := range keywords {
		if containsRouteKeyword(text, keyword) {
			matches++
		}
	}
	return matches
}

// containsRouteKeyword matches multi-word keywords by substring and
// single-word keywords on word boundaries, so "class" does not match
// "classical" and "prove" does not match "improvement".
func containsRouteKeyword(text string, keyword string) bool {
	if strings.Contains(keyword, " ") {
		return strings.Contains(text, keyword)
	}
	for start := 0; start < len(text); {
		index := strings.Index(text[start:], keyword)
		if index < 0 {
			return false
		}
		index += start
		end := index + len(keyword)
		if (index == 0 || !isRouteWordChar(text[index-1])) &&
			(end == len(text) || !isRouteWordChar(text[end])) {
			return true
		}
		start = index + 1
	}
	return false
}

func isRouteWordChar(char byte) bool {
	return char == '_' ||
		(char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9')
}
