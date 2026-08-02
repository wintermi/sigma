// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const iterationMetadataKey = "evalHarnessIteration"

// EvalGroupIDer supplies a stable identity for comparative input pairing.
type EvalGroupIDer interface {
	EvalGroupID() string
}

// Iteration identifies one baseline or candidate repetition.
type Iteration struct {
	SchemaVersion int      `json:"schemaVersion"`
	EvalSet       string   `json:"evalSet"`
	GroupKey      string   `json:"groupKey"`
	Harness       string   `json:"harness"`
	Baseline      string   `json:"baseline"`
	Candidates    []string `json:"candidates"`
	Repetition    int      `json:"repetition"`
}

// HarnessTableRow is one planned comparative harness repetition.
type HarnessTableRow[I, O any] struct {
	Harness    Harness[I, O]
	Name       string
	Repetition int
}

type iterationHarness[I, O any] struct {
	inner Harness[I, O]
	plan  Iteration
}

func (h iterationHarness[I, O]) HarnessName() string {
	return h.inner.HarnessName()
}

func (h iterationHarness[I, O]) Run(
	ctx context.Context,
	input I,
	run *RunContext,
) (RunResult[O], error) {
	groupKey, err := DeriveGroupKey(input, h.plan.Repetition)
	if err != nil {
		return RunResult[O]{}, err
	}
	iteration := h.plan
	iteration.GroupKey = groupKey
	if err := run.SetMetadata(iterationMetadataKey, iteration); err != nil {
		return RunResult[O]{}, err
	}
	result, err := h.inner.Run(ctx, input, run)
	if err != nil {
		return result, fmt.Errorf("evals: harness %q: %w", h.inner.HarnessName(), err)
	}
	return result, nil
}

// NewHarnessTable plans baseline and candidate repetitions in declaration order.
func NewHarnessTable[I, O any](
	evalSet string,
	baseline Harness[I, O],
	candidates []Harness[I, O],
	repetitions int,
) ([]HarnessTableRow[I, O], error) {
	evalSet = strings.TrimSpace(evalSet)
	if evalSet == "" {
		return nil, errors.New("evals: eval set must not be empty")
	}
	if baseline == nil {
		return nil, errors.New("evals: baseline harness is required")
	}
	if len(candidates) == 0 {
		return nil, errors.New("evals: at least one candidate harness is required")
	}
	if repetitions < 1 {
		return nil, errors.New("evals: repetitions must be positive")
	}

	harnesses := make([]Harness[I, O], 0, len(candidates)+1)
	harnesses = append(harnesses, baseline)
	harnesses = append(harnesses, candidates...)
	names := make(map[string]struct{}, len(harnesses))
	for _, harness := range harnesses {
		if harness == nil {
			return nil, errors.New("evals: harness is required")
		}
		name := strings.TrimSpace(harness.HarnessName())
		if name == "" {
			return nil, errors.New("evals: harness name must not be empty")
		}
		if _, exists := names[name]; exists {
			return nil, fmt.Errorf("evals: harness name %q is not unique", name)
		}
		names[name] = struct{}{}
	}

	candidateNames := make([]string, len(candidates))
	for i, harness := range candidates {
		candidateNames[i] = strings.TrimSpace(harness.HarnessName())
	}
	baselineName := strings.TrimSpace(baseline.HarnessName())
	rows := make([]HarnessTableRow[I, O], 0, repetitions*len(harnesses))
	for repetition := 1; repetition <= repetitions; repetition++ {
		for _, harness := range harnesses {
			name := strings.TrimSpace(harness.HarnessName())
			plan := Iteration{
				SchemaVersion: 1,
				EvalSet:       evalSet,
				Harness:       name,
				Baseline:      baselineName,
				Candidates:    append([]string(nil), candidateNames...),
				Repetition:    repetition,
			}
			rows = append(rows, HarnessTableRow[I, O]{
				Harness:    iterationHarness[I, O]{inner: harness, plan: plan},
				Name:       name,
				Repetition: repetition,
			})
		}
	}
	return rows, nil
}

// DeriveGroupKey combines a stable input identity with one repetition.
func DeriveGroupKey(input any, repetition int) (string, error) {
	if repetition < 1 {
		return "", errors.New("evals: repetition must be positive")
	}
	inputKey := ""
	if identified, ok := input.(EvalGroupIDer); ok {
		inputKey = strings.TrimSpace(identified.EvalGroupID())
	}
	if inputKey == "" {
		encoded, err := json.Marshal(input)
		if err != nil {
			return "", fmt.Errorf("evals: input must be JSON-serializable: %w", err)
		}
		digest := sha256.Sum256(encoded)
		inputKey = hex.EncodeToString(digest[:])
	}
	groupKey, err := json.Marshal([]any{inputKey, repetition})
	if err != nil {
		return "", fmt.Errorf("evals: encode group key: %w", err)
	}
	return string(groupKey), nil
}

func parseIteration(value any) (Iteration, bool) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return Iteration{}, false
	}
	var iteration Iteration
	if err := json.Unmarshal(encoded, &iteration); err != nil {
		return Iteration{}, false
	}
	if iteration.SchemaVersion != 1 || strings.TrimSpace(iteration.EvalSet) == "" ||
		strings.TrimSpace(iteration.GroupKey) == "" || strings.TrimSpace(iteration.Harness) == "" ||
		strings.TrimSpace(iteration.Baseline) == "" || len(iteration.Candidates) == 0 || iteration.Repetition < 1 {
		return Iteration{}, false
	}
	return iteration, true
}
