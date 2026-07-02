// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func routeRef(provider string, id string) sigma.ModelRef {
	return sigma.ModelRef{Provider: sigma.ProviderID(provider), ID: sigma.ModelID(id)}
}

func userRequest(text string) sigma.Request {
	return sigma.Request{Messages: []sigma.Message{sigma.UserText(text)}}
}

func testRoutePolicy() sigma.RoutePolicy {
	return sigma.RoutePolicy{
		Tiers: map[sigma.RouteTier][]sigma.ModelRef{
			sigma.RouteTierSimple:    {routeRef("fast", "flash-1"), routeRef("fast-alt", "flash-2")},
			sigma.RouteTierStandard:  {routeRef("mid", "standard-1")},
			sigma.RouteTierComplex:   {routeRef("big", "coder-1"), routeRef("big-alt", "coder-2")},
			sigma.RouteTierReasoning: {routeRef("big", "thinker-1")},
		},
	}
}

func TestClassifyRequestTiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want sigma.RouteTier
	}{
		{
			name: "simple lookup",
			text: "What is the capital of France?",
			want: sigma.RouteTierSimple,
		},
		{
			name: "greeting",
			text: "hello",
			want: sigma.RouteTierSimple,
		},
		{
			name: "technical multi-term request is complex",
			text: "Design the database schema and migration plan for the payments service, including transaction handling.",
			want: sigma.RouteTierComplex,
		},
		{
			name: "two reasoning markers force reasoning tier",
			text: "Think through the trade-offs of both designs step by step before recommending one.",
			want: sigma.RouteTierReasoning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			classification := sigma.ClassifyRequest(userRequest(tt.text))
			if classification.Tier != tt.want {
				t.Fatalf("tier = %q (score %.3f, signals %v), want %q",
					classification.Tier, classification.Score, classification.Signals, tt.want)
			}
		})
	}
}

func TestClassifyRequestIgnoresAccumulatedContext(t *testing.T) {
	t.Parallel()

	prompt := "What is a goroutine?"
	bare := sigma.ClassifyRequest(userRequest(prompt))

	// The same question after a long agentic session with large prior turns
	// must classify identically: only the latest user message is scored.
	filler := strings.Repeat("kubernetes architecture deadlock encryption benchmark ", 500)
	longSession := sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText(filler),
			{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{{Type: sigma.ContentBlockText, Text: filler}}},
			sigma.UserText(prompt),
		},
	}
	loaded := sigma.ClassifyRequest(longSession)

	if loaded.Tier != bare.Tier || loaded.Score != bare.Score {
		t.Fatalf("long session classified (%q, %.3f), want same as bare request (%q, %.3f)",
			loaded.Tier, loaded.Score, bare.Tier, bare.Score)
	}
}

func TestClassifyRequestSystemPromptCannotForceReasoning(t *testing.T) {
	t.Parallel()

	req := userRequest("What is the capital of France?")
	req.SystemPrompt = "Always think through problems step by step and reason about trade-offs."
	classification := sigma.ClassifyRequest(req)
	if classification.Tier == sigma.RouteTierReasoning {
		t.Fatalf("system prompt reasoning markers forced tier %q", classification.Tier)
	}
}

func TestClassifyRequestTokenCountDisabledByDefault(t *testing.T) {
	t.Parallel()

	long := userRequest(strings.Repeat("please describe the weather in paris today ", 300))
	base := sigma.ClassifyRequest(long)
	for _, signal := range base.Signals {
		if signal.Dimension == sigma.RouteDimensionTokenCount {
			t.Fatalf("tokenCount contributed %v with default zero weight", signal)
		}
	}

	weighted := sigma.ClassifyRequest(long, sigma.WithRouteWeight(sigma.RouteDimensionTokenCount, 0.5))
	if weighted.Score <= base.Score {
		t.Fatalf("enabling tokenCount on a long request scored %.3f, want above %.3f", weighted.Score, base.Score)
	}
}

func TestClassifyRequestBoundaryOverride(t *testing.T) {
	t.Parallel()

	req := userRequest("Fix the race condition in the connection pool and add a regression test.")
	base := sigma.ClassifyRequest(req)
	strict := sigma.ClassifyRequest(req, sigma.WithRouteBoundaries(0.9, 0.95, 0.99))
	if strict.Tier != sigma.RouteTierSimple {
		t.Fatalf("tier = %q with raised boundaries, want %q (base score %.3f)",
			strict.Tier, sigma.RouteTierSimple, base.Score)
	}
}

func TestSelectRoutesClassifiedTier(t *testing.T) {
	t.Parallel()

	policy := testRoutePolicy()
	decision, err := policy.Select(userRequest("What is the capital of France?"))
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Tier != sigma.RouteTierSimple || decision.Model != routeRef("fast", "flash-1") {
		t.Fatalf("decision = %+v, want first simple-tier candidate", decision)
	}
	if decision.Classification.Tier != sigma.RouteTierSimple {
		t.Fatalf("classification tier = %q, want %q", decision.Classification.Tier, sigma.RouteTierSimple)
	}
}

func TestSelectEscalatesWhenTierEmpty(t *testing.T) {
	t.Parallel()

	policy := sigma.RoutePolicy{
		Tiers: map[sigma.RouteTier][]sigma.ModelRef{
			sigma.RouteTierComplex: {routeRef("big", "coder-1")},
		},
	}
	decision, err := policy.Select(userRequest("What is the capital of France?"))
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Tier != sigma.RouteTierComplex || decision.Model != routeRef("big", "coder-1") {
		t.Fatalf("decision = %+v, want escalation to the complex tier", decision)
	}
	if decision.Classification.Tier != sigma.RouteTierSimple {
		t.Fatalf("classification tier = %q, want the original %q", decision.Classification.Tier, sigma.RouteTierSimple)
	}
}

func TestSelectFallsBackDownwardWhenNoHigherTier(t *testing.T) {
	t.Parallel()

	policy := sigma.RoutePolicy{
		Tiers: map[sigma.RouteTier][]sigma.ModelRef{
			sigma.RouteTierSimple: {routeRef("fast", "flash-1")},
		},
	}
	decision, err := policy.Select(userRequest(
		"Think through the trade-offs of both designs step by step before recommending one."))
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Tier != sigma.RouteTierSimple {
		t.Fatalf("decision tier = %q, want downward fallback to %q", decision.Tier, sigma.RouteTierSimple)
	}
}

func TestSelectSkipsExcludedAndInvalidCandidates(t *testing.T) {
	t.Parallel()

	policy := sigma.RoutePolicy{
		Tiers: map[sigma.RouteTier][]sigma.ModelRef{
			sigma.RouteTierSimple: {
				{Provider: "fast"}, // invalid: missing model id
				routeRef("fast", "flash-1"),
				routeRef("fast-alt", "flash-2"),
			},
		},
	}
	decision, err := policy.Select(userRequest("hello"),
		sigma.WithRouteExclusions(routeRef("fast", "flash-1")))
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Model != routeRef("fast-alt", "flash-2") {
		t.Fatalf("model = %+v, want invalid and excluded candidates skipped", decision.Model)
	}
}

func TestSelectNoCandidates(t *testing.T) {
	t.Parallel()

	policy := sigma.RoutePolicy{}
	if _, err := policy.Select(userRequest("hello")); !errors.Is(err, sigma.ErrNoRouteCandidates) {
		t.Fatalf("Select() error = %v, want ErrNoRouteCandidates", err)
	}
}

func rateLimitError(retryAfter time.Duration) error {
	return sigma.NewProviderError("fast", "openai-chat", "flash-1", 429, "req-1", retryAfter, nil, nil)
}

func TestFallbackRateLimitedPrefersNextCandidate(t *testing.T) {
	t.Parallel()

	policy := testRoutePolicy()
	decision := sigma.RouteDecision{Model: routeRef("fast", "flash-1"), Tier: sigma.RouteTierSimple}
	advice := policy.Fallback(decision, nil, rateLimitError(0))
	if advice.Action != sigma.RouteActionFallback || advice.Model != routeRef("fast-alt", "flash-2") {
		t.Fatalf("advice = %+v, want fallback to fast-alt/flash-2", advice)
	}
}

func TestFallbackRateLimitedExhaustedRetriesAfterDelay(t *testing.T) {
	t.Parallel()

	policy := sigma.RoutePolicy{
		Tiers: map[sigma.RouteTier][]sigma.ModelRef{
			sigma.RouteTierSimple: {routeRef("fast", "flash-1")},
		},
	}
	decision := sigma.RouteDecision{Model: routeRef("fast", "flash-1"), Tier: sigma.RouteTierSimple}
	advice := policy.Fallback(decision, nil, rateLimitError(3*time.Second))
	if advice.Action != sigma.RouteActionRetry || advice.Model != decision.Model {
		t.Fatalf("advice = %+v, want retry of the same model", advice)
	}
	if advice.RetryAfter != 3*time.Second {
		t.Fatalf("RetryAfter = %v, want provider hint of 3s", advice.RetryAfter)
	}
}

func TestFallbackTransientRetriesSameModel(t *testing.T) {
	t.Parallel()

	policy := testRoutePolicy()
	decision := sigma.RouteDecision{Model: routeRef("fast", "flash-1"), Tier: sigma.RouteTierSimple}
	err := sigma.NewProviderError("fast", "openai-chat", "flash-1", 503, "", 0, nil, nil)
	advice := policy.Fallback(decision, nil, err)
	if advice.Action != sigma.RouteActionRetry || advice.Model != decision.Model {
		t.Fatalf("advice = %+v, want retry of the same model", advice)
	}
}

func TestFallbackAuthSkipsAttemptedAndEscalates(t *testing.T) {
	t.Parallel()

	policy := testRoutePolicy()
	decision := sigma.RouteDecision{Model: routeRef("fast-alt", "flash-2"), Tier: sigma.RouteTierSimple}
	err := sigma.NewProviderError("fast-alt", "openai-chat", "flash-2", 401, "", 0, nil, nil)
	advice := policy.Fallback(decision, []sigma.ModelRef{routeRef("fast", "flash-1")}, err)
	if advice.Action != sigma.RouteActionFallback || advice.Model != routeRef("mid", "standard-1") {
		t.Fatalf("advice = %+v, want escalation to mid/standard-1", advice)
	}
}

func TestFallbackAuthExhaustedAborts(t *testing.T) {
	t.Parallel()

	policy := sigma.RoutePolicy{
		Tiers: map[sigma.RouteTier][]sigma.ModelRef{
			sigma.RouteTierSimple: {routeRef("fast", "flash-1")},
		},
	}
	decision := sigma.RouteDecision{Model: routeRef("fast", "flash-1"), Tier: sigma.RouteTierSimple}
	err := sigma.NewProviderError("fast", "openai-chat", "flash-1", 401, "", 0, nil, nil)
	advice := policy.Fallback(decision, nil, err)
	if advice.Action != sigma.RouteActionAbort {
		t.Fatalf("advice = %+v, want abort when no candidate remains", advice)
	}
}

func TestFallbackInvalidRequestAborts(t *testing.T) {
	t.Parallel()

	policy := testRoutePolicy()
	decision := sigma.RouteDecision{Model: routeRef("fast", "flash-1"), Tier: sigma.RouteTierSimple}
	err := sigma.NewProviderError("fast", "openai-chat", "flash-1", 400, "", 0, nil, nil)
	advice := policy.Fallback(decision, nil, err)
	if advice.Action != sigma.RouteActionAbort {
		t.Fatalf("advice = %+v, want abort for invalid request", advice)
	}
}

func TestFallbackContextOverflowPicksLargerContextCandidate(t *testing.T) {
	t.Parallel()

	windows := map[sigma.ModelRef]int{
		routeRef("fast", "flash-1"):     32_000,
		routeRef("fast-alt", "flash-2"): 32_000,
		routeRef("mid", "standard-1"):   128_000,
	}
	lookup := func(ref sigma.ModelRef) (sigma.Model, bool) {
		window, ok := windows[ref]
		if !ok {
			return sigma.Model{}, false
		}
		return sigma.Model{Provider: ref.Provider, ID: ref.ID, ContextWindow: window}, true
	}

	policy := testRoutePolicy()
	decision := sigma.RouteDecision{Model: routeRef("fast", "flash-1"), Tier: sigma.RouteTierSimple}
	err := sigma.NewProviderError("fast", "openai-chat", "flash-1", 413, "", 0, nil, nil)
	advice := policy.Fallback(decision, nil, err, sigma.WithRouteModelLookup(lookup))
	if advice.Action != sigma.RouteActionFallback || advice.Model != routeRef("mid", "standard-1") {
		t.Fatalf("advice = %+v, want fallback past the same-window candidate to mid/standard-1", advice)
	}
}

func TestFallbackContextOverflowWithoutLargerCandidateAborts(t *testing.T) {
	t.Parallel()

	lookup := func(ref sigma.ModelRef) (sigma.Model, bool) {
		return sigma.Model{Provider: ref.Provider, ID: ref.ID, ContextWindow: 32_000}, true
	}
	policy := sigma.RoutePolicy{
		Tiers: map[sigma.RouteTier][]sigma.ModelRef{
			sigma.RouteTierSimple: {routeRef("fast", "flash-1"), routeRef("fast-alt", "flash-2")},
		},
	}
	decision := sigma.RouteDecision{Model: routeRef("fast", "flash-1"), Tier: sigma.RouteTierSimple}
	err := sigma.NewProviderError("fast", "openai-chat", "flash-1", 413, "", 0, nil, nil)
	advice := policy.Fallback(decision, nil, err, sigma.WithRouteModelLookup(lookup))
	if advice.Action != sigma.RouteActionAbort {
		t.Fatalf("advice = %+v, want abort when no larger-context candidate exists", advice)
	}
}

func TestFallbackClassificationExposed(t *testing.T) {
	t.Parallel()

	policy := testRoutePolicy()
	decision := sigma.RouteDecision{Model: routeRef("fast", "flash-1"), Tier: sigma.RouteTierSimple}
	advice := policy.Fallback(decision, nil, rateLimitError(0))
	if advice.Classification.Class != sigma.ErrorClassRateLimited {
		t.Fatalf("classification class = %q, want %q", advice.Classification.Class, sigma.ErrorClassRateLimited)
	}
	if advice.Reason == "" {
		t.Fatal("advice reason is empty, want a routing explanation")
	}
}
