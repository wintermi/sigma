// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"errors"
	"time"
)

// ErrNoRouteCandidates reports that a routing policy has no usable candidate
// for the classified tier or any other tier.
var ErrNoRouteCandidates = errors.New("no route candidates")

// RouteTier is a deterministic request complexity tier.
type RouteTier string

const (
	// RouteTierSimple identifies short lookups, definitions, and greetings.
	RouteTierSimple RouteTier = "simple"
	// RouteTierStandard identifies everyday requests without strong
	// complexity signals.
	RouteTierStandard RouteTier = "standard"
	// RouteTierComplex identifies technical, code-heavy, or multi-part
	// requests.
	RouteTierComplex RouteTier = "complex"
	// RouteTierReasoning identifies requests with explicit deep-reasoning
	// cues such as step-by-step analysis or trade-off comparisons.
	RouteTierReasoning RouteTier = "reasoning"
)

// routeTierOrder lists tiers from least to most capable. Candidate search
// escalates upward through this order before falling back downward.
var routeTierOrder = []RouteTier{
	RouteTierSimple,
	RouteTierStandard,
	RouteTierComplex,
	RouteTierReasoning,
}

// RoutePolicy maps tiers to ordered model candidates. Earlier candidates are
// preferred. The policy is a plain value constructed by the caller; sigma does
// not load routing configuration from files or the environment.
type RoutePolicy struct {
	Tiers map[RouteTier][]ModelRef `json:"tiers"`
}

// RouteDecision reports the model selected for a request and why.
type RouteDecision struct {
	Model          ModelRef            `json:"model"`
	Tier           RouteTier           `json:"tier"`
	Classification RouteClassification `json:"classification"`
}

// RouteAction is the advised next step after a routed request failed.
type RouteAction string

const (
	// RouteActionRetry advises retrying the same model, waiting RetryAfter
	// first when it is non-zero.
	RouteActionRetry RouteAction = "retry"
	// RouteActionFallback advises sending the request to the advice Model
	// instead. Use TransformRequestForModel before replaying a conversation
	// on a different provider.
	RouteActionFallback RouteAction = "fallback"
	// RouteActionAbort advises surfacing the error without retrying.
	RouteActionAbort RouteAction = "abort"
)

// RouteAdvice is a deterministic fallback recommendation. Sigma only decides;
// the caller executes the retry or fallback and tracks attempt state.
type RouteAdvice struct {
	Action         RouteAction
	Model          ModelRef
	RetryAfter     time.Duration
	Reason         string
	Classification ErrorClassification
}

// Select classifies req and returns the first usable candidate for the
// classified tier. When the classified tier has no usable candidate the
// search escalates to more capable tiers first, then falls back to less
// capable tiers. Candidates excluded via WithRouteExclusions or failing
// ValidateModelRef are skipped. ErrNoRouteCandidates is returned when no
// candidate remains.
func (p RoutePolicy) Select(req Request, opts ...RouteOption) (RouteDecision, error) {
	config := newRouteConfig(opts...)
	classification := classifyRouteRequest(req, config)

	for _, tier := range routeTierSearchOrder(classification.Tier) {
		if ref, ok := p.firstCandidate(tier, config, nil); ok {
			return RouteDecision{
				Model:          ref,
				Tier:           tier,
				Classification: classification,
			}, nil
		}
	}
	return RouteDecision{}, ErrNoRouteCandidates
}

// Fallback classifies err and advises the next step for a failed decision.
//
// The advice is stateless: attempted lists models the caller has already
// tried, and the failed decision model is always skipped when searching for
// a fallback candidate. The decision table is:
//
//   - invalid-request and unknown errors abort.
//   - transient errors retry the same model, honoring the provider retry hint.
//   - rate-limited errors fall back to the next candidate, or retry the same
//     model after the hinted delay when no candidate remains.
//   - auth, quota, billing, and provider errors fall back to the next
//     candidate, or abort when no candidate remains.
//   - context-overflow errors fall back to the next candidate with a larger
//     known context window, or abort when none exists.
func (p RoutePolicy) Fallback(decision RouteDecision, attempted []ModelRef, err error, opts ...RouteOption) RouteAdvice {
	config := newRouteConfig(opts...)
	classification := ClassifyError(err)
	skip := routeSkipSet(decision.Model, attempted)
	advice := RouteAdvice{Classification: classification}

	switch classification.Class {
	case ErrorClassTransient:
		advice.Action = RouteActionRetry
		advice.Model = decision.Model
		advice.RetryAfter = classification.RetryHint.After
		advice.Reason = "transient error; retry the same model"
	case ErrorClassRateLimited:
		if ref, ok := p.nextCandidate(decision.Tier, config, skip); ok {
			advice.Action = RouteActionFallback
			advice.Model = ref
			advice.Reason = "rate limited; fall back to the next candidate"
			return advice
		}
		advice.Action = RouteActionRetry
		advice.Model = decision.Model
		advice.RetryAfter = classification.RetryHint.After
		advice.Reason = "rate limited with no remaining candidate; retry after delay"
	case ErrorClassAuth, ErrorClassQuota, ErrorClassBilling, ErrorClassProvider:
		if ref, ok := p.nextCandidate(decision.Tier, config, skip); ok {
			advice.Action = RouteActionFallback
			advice.Model = ref
			advice.Reason = string(classification.Class) + " error; fall back to the next candidate"
			return advice
		}
		advice.Action = RouteActionAbort
		advice.Reason = string(classification.Class) + " error with no remaining candidate"
	case ErrorClassContextOverflow:
		if ref, ok := p.largerContextCandidate(decision, config, skip); ok {
			advice.Action = RouteActionFallback
			advice.Model = ref
			advice.Reason = "context overflow; fall back to a larger-context candidate"
			return advice
		}
		advice.Action = RouteActionAbort
		advice.Reason = "context overflow with no larger-context candidate"
	default:
		advice.Action = RouteActionAbort
		advice.Reason = string(classification.Class) + " error; not retryable"
	}
	return advice
}

// firstCandidate returns the first usable candidate within a single tier.
func (p RoutePolicy) firstCandidate(tier RouteTier, config routeConfig, skip map[ModelRef]struct{}) (ModelRef, bool) {
	for _, ref := range p.Tiers[tier] {
		if _, excluded := config.exclusions[ref]; excluded {
			continue
		}
		if _, skipped := skip[ref]; skipped {
			continue
		}
		if ValidateModelRef(ref) != nil {
			continue
		}
		return ref, true
	}
	return ModelRef{}, false
}

// nextCandidate searches from tier through the standard escalation order.
func (p RoutePolicy) nextCandidate(tier RouteTier, config routeConfig, skip map[ModelRef]struct{}) (ModelRef, bool) {
	for _, candidateTier := range routeTierSearchOrder(tier) {
		if ref, ok := p.firstCandidate(candidateTier, config, skip); ok {
			return ref, true
		}
	}
	return ModelRef{}, false
}

// largerContextCandidate searches the failed tier and more capable tiers for
// the first candidate with a known context window larger than the failed
// model. An unknown failed context window is treated as zero, so any
// candidate with a known positive context window qualifies.
func (p RoutePolicy) largerContextCandidate(decision RouteDecision, config routeConfig, skip map[ModelRef]struct{}) (ModelRef, bool) {
	failedWindow := 0
	if model, ok := config.lookup(decision.Model); ok {
		failedWindow = model.ContextWindow
	}

	for _, tier := range routeTierEscalationOrder(decision.Tier) {
		for _, ref := range p.Tiers[tier] {
			if _, excluded := config.exclusions[ref]; excluded {
				continue
			}
			if _, skipped := skip[ref]; skipped {
				continue
			}
			if ValidateModelRef(ref) != nil {
				continue
			}
			model, ok := config.lookup(ref)
			if !ok || model.ContextWindow <= failedWindow {
				continue
			}
			return ref, true
		}
	}
	return ModelRef{}, false
}

// routeTierSearchOrder returns tier first, then more capable tiers ascending,
// then less capable tiers descending.
func routeTierSearchOrder(tier RouteTier) []RouteTier {
	start := routeTierIndex(tier)
	order := make([]RouteTier, 0, len(routeTierOrder))
	for i := start; i < len(routeTierOrder); i++ {
		order = append(order, routeTierOrder[i])
	}
	for i := start - 1; i >= 0; i-- {
		order = append(order, routeTierOrder[i])
	}
	return order
}

// routeTierEscalationOrder returns tier first, then more capable tiers only.
func routeTierEscalationOrder(tier RouteTier) []RouteTier {
	start := routeTierIndex(tier)
	return routeTierOrder[start:]
}

func routeTierIndex(tier RouteTier) int {
	for i, candidate := range routeTierOrder {
		if candidate == tier {
			return i
		}
	}
	return 1 // unknown tiers search from the standard tier
}

func routeSkipSet(failed ModelRef, attempted []ModelRef) map[ModelRef]struct{} {
	skip := make(map[ModelRef]struct{}, len(attempted)+1)
	skip[failed] = struct{}{}
	for _, ref := range attempted {
		skip[ref] = struct{}{}
	}
	return skip
}

// RouteOption configures classification and candidate selection.
type RouteOption func(*routeConfig)

// WithRouteWeight overrides the weight of one classifier dimension. Setting a
// dimension weight to zero removes it from scoring. The tokenCount dimension
// defaults to zero weight because accumulated agentic context would otherwise
// bias every late-turn request toward heavier tiers; enable it only for
// single-turn workloads.
func WithRouteWeight(dimension string, weight float64) RouteOption {
	return func(config *routeConfig) {
		config.weights[dimension] = weight
	}
}

// WithRouteBoundaries overrides the ascending tier score boundaries. Scores
// below simpleStandard classify as simple, below standardComplex as standard,
// below complexReasoning as complex, and reasoning otherwise.
func WithRouteBoundaries(simpleStandard float64, standardComplex float64, complexReasoning float64) RouteOption {
	return func(config *routeConfig) {
		config.boundarySimpleStandard = simpleStandard
		config.boundaryStandardComplex = standardComplex
		config.boundaryComplexReasoning = complexReasoning
	}
}

// WithRouteExclusions skips the supplied candidates during selection and
// fallback. Callers own health tracking; models on cooldown should be passed
// here on every call until the caller considers them healthy again.
func WithRouteExclusions(refs ...ModelRef) RouteOption {
	return func(config *routeConfig) {
		for _, ref := range refs {
			config.exclusions[ref] = struct{}{}
		}
	}
}

// WithRouteModelLookup overrides how candidate metadata is resolved for
// context-overflow fallback. The default lookup uses DefaultRegistry.
func WithRouteModelLookup(lookup func(ModelRef) (Model, bool)) RouteOption {
	return func(config *routeConfig) {
		if lookup != nil {
			config.lookup = lookup
		}
	}
}

type routeConfig struct {
	weights                  map[string]float64
	boundarySimpleStandard   float64
	boundaryStandardComplex  float64
	boundaryComplexReasoning float64
	exclusions               map[ModelRef]struct{}
	lookup                   func(ModelRef) (Model, bool)
}

func newRouteConfig(opts ...RouteOption) routeConfig {
	config := routeConfig{
		weights: map[string]float64{
			RouteDimensionReasoningMarkers:   defaultRouteWeightReasoningMarkers,
			RouteDimensionTechnicalTerms:     defaultRouteWeightTechnicalTerms,
			RouteDimensionSimpleIndicators:   defaultRouteWeightSimpleIndicators,
			RouteDimensionCodePresence:       defaultRouteWeightCodePresence,
			RouteDimensionMultiStepPatterns:  defaultRouteWeightMultiStepPatterns,
			RouteDimensionQuestionComplexity: defaultRouteWeightQuestionComplexity,
			RouteDimensionTokenCount:         defaultRouteWeightTokenCount,
		},
		boundarySimpleStandard:   defaultRouteBoundarySimpleStandard,
		boundaryStandardComplex:  defaultRouteBoundaryStandardComplex,
		boundaryComplexReasoning: defaultRouteBoundaryComplexReasoning,
		exclusions:               make(map[ModelRef]struct{}),
		lookup: func(ref ModelRef) (Model, bool) {
			return DefaultRegistry().Model(ref.Provider, ref.ID)
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}
	return config
}
