# Routing Decisions

Sigma provides deterministic routing decisions: given a request and a caller-
defined policy, it answers "which model should serve this?" and, after a
failure, "what should happen next?". Sigma only decides — the caller executes
the request, tracks attempt state, and owns health tracking. There is no proxy,
no execution loop, and no configuration file format; harnesses map their own
configuration onto a `RoutePolicy` value.

## Tiers

Requests classify into four ordered tiers:

| Tier | Typical requests |
|------|------------------|
| `RouteTierSimple` | Lookups, definitions, greetings |
| `RouteTierStandard` | Everyday requests without strong complexity signals |
| `RouteTierComplex` | Technical, code-heavy, or multi-part requests |
| `RouteTierReasoning` | Explicit deep-reasoning cues (step-by-step, trade-offs) |

## Classification

`ClassifyRequest` scores a request with weighted rule-based dimensions. It is a
pure function: no model calls, no randomness, and the same request always
classifies to the same tier.

```go
classification := sigma.ClassifyRequest(req)
log.Println(classification.Tier, classification.Score, classification.Signals)
```

Only the system prompt and the latest user message are scored, so accumulated
agentic context does not drift late-turn requests into heavier tiers. Reasoning
markers are scored against the user message alone, and two or more markers
classify directly as `RouteTierReasoning`.

The dimensions, with defaults tuned for agentic workloads:

| Dimension | Weight | Signal |
|-----------|--------|--------|
| `reasoningMarkers` | 0.40 | "step by step", "trade-off", "root cause", ... |
| `technicalTerms` | 0.25 | "concurrency", "schema", "race condition", ... |
| `simpleIndicators` | 0.20 | "what is", "define", "summarize" (negative) |
| `codePresence` | 0.10 | Code fences and code keywords |
| `multiStepPatterns` | 0.03 | "first ... then", "step 1", "after that" |
| `questionComplexity` | 0.02 | Two or more questions |
| `tokenCount` | 0.0 | Estimated request tokens (disabled by default) |

`tokenCount` defaults to zero weight because it counts the whole request:
enabling it biases long agentic sessions toward heavier tiers regardless of
what the user asked. Enable it only for single-turn workloads:

```go
classification := sigma.ClassifyRequest(req,
	sigma.WithRouteWeight(sigma.RouteDimensionTokenCount, 0.15))
```

Tier boundaries are ascending weighted-score cutoffs and can be overridden:

```go
classification := sigma.ClassifyRequest(req,
	sigma.WithRouteBoundaries(0.10, 0.25, 0.55))
```

## Selecting A Model

A `RoutePolicy` maps tiers to ordered model candidates. `Select` classifies the
request and returns the first usable candidate, escalating to more capable
tiers when the classified tier has no candidate, then falling back to less
capable tiers. `ErrNoRouteCandidates` is returned when nothing remains.

```go
policy := sigma.RoutePolicy{
	Tiers: map[sigma.RouteTier][]sigma.ModelRef{
		sigma.RouteTierSimple:    {{Provider: "google", ID: "gemini-flash"}},
		sigma.RouteTierComplex:   {{Provider: "moonshot", ID: "kimi"}},
		sigma.RouteTierReasoning: {{Provider: "anthropic", ID: "claude-opus"}},
	},
}

decision, err := policy.Select(req)
if err != nil {
	return err
}
final, err := client.Complete(ctx, sigma.Model{
	Provider: decision.Model.Provider,
	ID:       decision.Model.ID,
}, req)
```

Prefer resolving the decision through the registry so the full model metadata
and defaults apply:

```go
model, ok := client.GetModel(decision.Model.Provider, decision.Model.ID)
```

Callers own health tracking. Pass models on cooldown as exclusions on every
call until they are considered healthy again:

```go
decision, err := policy.Select(req, sigma.WithRouteExclusions(unhealthy...))
```

## Fallback Advice

After a routed request fails, `Fallback` classifies the error with
`ClassifyError` and returns stateless advice. `attempted` lists models the
caller already tried; the failed model is always skipped.

```go
advice := policy.Fallback(decision, attempted, err)
switch advice.Action {
case sigma.RouteActionRetry:
	time.Sleep(advice.RetryAfter)
	// Retry the same model.
case sigma.RouteActionFallback:
	// Send the request to advice.Model instead.
case sigma.RouteActionAbort:
	return err
}
```

The decision table:

| Error class | Advice |
|-------------|--------|
| `transient` | Retry the same model, honoring the provider retry hint |
| `rate-limited` | Fall back to the next candidate; retry after the hinted delay when none remains |
| `auth`, `quota`, `billing`, `provider` | Fall back to the next candidate; abort when none remains |
| `context-overflow` | Fall back to the next candidate with a larger known context window; abort when none exists |
| `invalid-request`, `unknown` | Abort |

Context-overflow fallback resolves candidate context windows through
`DefaultRegistry` by default; override the lookup for custom catalogs:

```go
advice := policy.Fallback(decision, attempted, err,
	sigma.WithRouteModelLookup(func(ref sigma.ModelRef) (sigma.Model, bool) {
		return registry.Model(ref.Provider, ref.ID)
	}))
```

When a fallback crosses providers, adapt the conversation before replaying it:

```go
result, err := sigma.TransformRequestForModel(target, req)
```

`RouteDecision` and `RouteAdvice` carry the classification, selected tier, and
a human-readable reason, so harnesses can log every routing decision.
