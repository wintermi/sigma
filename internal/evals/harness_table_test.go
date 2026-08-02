// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
)

type identifiedInput struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

func (i identifiedInput) EvalGroupID() string {
	return i.ID
}

func TestDeriveGroupKeyUsesIDOrDeterministicJSON(t *testing.T) {
	t.Parallel()

	identified, err := DeriveGroupKey(identifiedInput{ID: " input-1 ", Value: "ignored"}, 2)
	if err != nil {
		t.Fatalf("DeriveGroupKey ID returned error: %v", err)
	}
	if identified != `["input-1",2]` {
		t.Fatalf("identified group key = %q", identified)
	}
	left, err := DeriveGroupKey(map[string]any{"first": 1, "second": []any{true, "value"}}, 1)
	if err != nil {
		t.Fatalf("DeriveGroupKey left returned error: %v", err)
	}
	right, err := DeriveGroupKey(map[string]any{"second": []any{true, "value"}, "first": 1}, 1)
	if err != nil {
		t.Fatalf("DeriveGroupKey right returned error: %v", err)
	}
	if left != right {
		t.Fatalf("canonical group keys differ: %q != %q", left, right)
	}
	if _, err := DeriveGroupKey(math.Inf(1), 1); err == nil {
		t.Fatal("DeriveGroupKey accepted non-JSON input")
	}
}

func TestNewHarnessTablePlansAndAttachesIterations(t *testing.T) {
	t.Parallel()

	newHarness := func(name string) Harness[identifiedInput, string] {
		return HarnessFunc[identifiedInput, string]{
			Name: name,
			Func: func(_ context.Context, input identifiedInput, _ *RunContext) (RunResult[string], error) {
				return RunResult[string]{Output: name + ":" + input.Value}, nil
			},
		}
	}
	rows, err := NewHarnessTable(
		"prompt comparison",
		newHarness("baseline"),
		[]Harness[identifiedInput, string]{newHarness("candidate")},
		2,
	)
	if err != nil {
		t.Fatalf("NewHarnessTable returned error: %v", err)
	}
	wantOrder := []string{"baseline/1", "candidate/1", "baseline/2", "candidate/2"}
	for i, row := range rows {
		got := row.Name + "/" + strconv.Itoa(row.Repetition)
		if got != wantOrder[i] {
			t.Fatalf("row %d = %q, want %q", i, got, wantOrder[i])
		}
		run := newRunContext("run")
		result, err := row.Harness.Run(context.Background(), identifiedInput{ID: "case", Value: "x"}, run)
		if err != nil {
			t.Fatalf("row %d run returned error: %v", i, err)
		}
		if !strings.HasSuffix(result.Output, ":x") {
			t.Fatalf("row %d output = %q", i, result.Output)
		}
		metadata, _ := run.snapshot()
		iteration, ok := parseIteration(metadata[iterationMetadataKey])
		if !ok || iteration.Harness != row.Name || iteration.Repetition != row.Repetition || iteration.GroupKey == "" {
			t.Fatalf("row %d iteration = %#v, %v", i, iteration, ok)
		}
	}
}

func TestNewHarnessTableRejectsUnsafePlans(t *testing.T) {
	t.Parallel()

	harness := HarnessFunc[string, string]{
		Name: "same",
		Func: func(context.Context, string, *RunContext) (RunResult[string], error) {
			return RunResult[string]{}, nil
		},
	}
	if _, err := NewHarnessTable("set", harness, []Harness[string, string]{harness}, 1); err == nil {
		t.Fatal("NewHarnessTable accepted duplicate harness names")
	}
	if _, err := NewHarnessTable("set", harness, []Harness[string, string]{HarnessFunc[string, string]{Name: "other"}}, 0); err == nil {
		t.Fatal("NewHarnessTable accepted zero repetitions")
	}
}
