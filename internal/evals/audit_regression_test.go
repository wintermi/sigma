// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func regressionHarness(name string) Harness[any, string] {
	return HarnessFunc[any, string]{Name: name, Func: func(context.Context, any, *RunContext) (RunResult[string], error) {
		return RunResult[string]{Output: "answer", Usage: Usage{TotalTokens: intPointer(10)}}, nil
	}}
}

func regressionJudge(score float64) Judge[any, string] {
	return Judge[any, string]{Name: "correctness", Score: func(context.Context, JudgmentInput[any, string]) (JudgeResult, error) {
		return JudgeResult{Score: score}, nil
	}}
}

func TestRunnerFinalStatusExcludesIndependentFailuresAndSkips(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                       string
		failed, skipped, threshold bool
		wantOutcome                ObservationOutcome
		wantStatus                 string
		wantEligible               int
	}{
		{name: "scored", wantOutcome: OutcomeScored, wantStatus: "passed", wantEligible: 1},
		{name: "independent failure", failed: true, wantOutcome: OutcomeErrored, wantStatus: "failed"},
		{name: "skipped", skipped: true, wantOutcome: OutcomeSkipped, wantStatus: "skipped"},
		{name: "threshold only", threshold: true, wantOutcome: OutcomeScored, wantStatus: "failed", wantEligible: 1},
		{name: "threshold and failure", threshold: true, failed: true, wantOutcome: OutcomeErrored, wantStatus: "failed"},
		{name: "threshold and skip", threshold: true, skipped: true, wantOutcome: OutcomeSkipped, wantStatus: "skipped"},
		{name: "failed and skipped", failed: true, skipped: true, wantOutcome: OutcomeErrored, wantStatus: "failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := NewHarnessTable("status", regressionHarness("baseline"), []Harness[any, string]{regressionHarness("candidate")}, 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				fake := &fakeTest{name: row.Name}
				score := 1.0
				var threshold *float64
				if row.Name == "candidate" && tt.threshold {
					score, threshold = 0.5, floatPointer(1)
				}
				execution := Run(t.Context(), runner, fake, Case[any, string]{
					ID: "case", EvalSet: "status", Input: "input", Harness: row.Harness,
					Judges: []Judge[any, string]{regressionJudge(score)}, JudgeThreshold: threshold,
				})
				if execution.Err != nil || fake.Failed() {
					t.Fatalf("execution=%+v failed=%v", execution, fake.Failed())
				}
				if row.Name == "candidate" {
					if tt.failed {
						fake.Errorf("independent invariant failed")
					}
					fake.skipped = tt.skipped
				}
				fake.runCleanups()
				if row.Name == "candidate" && fake.Failed() != (tt.wantStatus == "failed") {
					t.Fatalf("test failed=%v", fake.Failed())
				}
			}
			observations := runner.Observations()
			if len(observations) != 2 || observations[1].Outcome != tt.wantOutcome || observations[1].Score == nil {
				t.Fatalf("observations=%+v", observations)
			}
			report := SummarizeComparisons(observations)
			comparison := report.EvalSets[0].Comparisons[0]
			if comparison.Correctness.EligiblePairs != tt.wantEligible || comparison.TotalTokens.EligiblePairs != tt.wantEligible || comparison.TotalMS.EligiblePairs != tt.wantEligible {
				t.Fatalf("comparison=%+v", comparison)
			}
			if (len(report.Diagnostics) > 0) != (tt.wantEligible == 0) {
				t.Fatalf("diagnostics=%+v", report.Diagnostics)
			}
			record := readJSONLines(t, filepath.Join(runner.ArtifactDir(), "runs.jsonl"))[1]
			if record["test"].(map[string]any)["status"] != tt.wantStatus || record["averageScore"] == nil {
				t.Fatalf("record=%+v", record)
			}
		})
	}
}

type regressionIdentifiedInput struct{ Value any }

func (regressionIdentifiedInput) EvalGroupID() string { return "stable" }

func TestRunnerRetainsPreDispatchFailuresInComparisons(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"invalid input", "invalid judge", "one arm invalid input", "missing case id", "missing id and invalid input"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			dispatched := 0
			harness := func(name string) Harness[any, string] {
				return HarnessFunc[any, string]{Name: name, Func: func(context.Context, any, *RunContext) (RunResult[string], error) {
					dispatched++
					return RunResult[string]{Output: "answer"}, nil
				}}
			}
			rows, err := NewHarnessTable("submission", harness("baseline"), []Harness[any, string]{harness("candidate")}, 2)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"valid", "invalid"} {
				for _, row := range rows {
					fake := &fakeTest{name: id}
					eval := Case[any, string]{ID: id, EvalSet: "submission", Input: "valid", Harness: row.Harness, Judges: []Judge[any, string]{regressionJudge(1)}}
					wantError := id == "invalid"
					if wantError {
						switch mode {
						case "invalid input":
							eval.Input = make(chan int)
						case "invalid judge":
							eval.Judges[0].Score = nil
						case "one arm invalid input":
							// Both arms have an explicit identity; only one is unserializable.
							eval.Input = regressionIdentifiedInput{Value: "valid"}
							if row.Name == "candidate" {
								eval.Input = regressionIdentifiedInput{Value: make(chan int)}
							} else {
								wantError = false
							}
						case "missing case id":
							eval.ID = ""
						case "missing id and invalid input":
							eval.ID, eval.Input = "", make(chan int)
						}
					}
					execution := Run(t.Context(), runner, fake, eval)
					if (execution.Err != nil) != wantError {
						t.Fatalf("execution error=%v wantError=%v", execution.Err, wantError)
					}
					fake.runCleanups()
				}
			}
			observations := runner.Observations()
			if len(observations) != 8 {
				t.Fatalf("lost submissions: %+v", observations)
			}
			report := SummarizeComparisons(observations)
			comparison := report.EvalSets[0].Comparisons[0]
			wantDiagnostics := 4
			wantDispatched := 4
			if mode == "one arm invalid input" {
				wantDiagnostics = 2
				wantDispatched = 6
			}
			if dispatched != wantDispatched {
				t.Fatalf("dispatches=%d want=%d", dispatched, wantDispatched)
			}
			if comparison.Correctness.TotalPairs != 4 || comparison.Correctness.EligiblePairs != 2 || len(report.Diagnostics) != wantDiagnostics {
				t.Fatalf("report=%+v comparison=%+v", report, comparison)
			}
			records := readJSONLines(t, filepath.Join(runner.ArtifactDir(), "runs.jsonl"))
			if len(records) != 8 {
				t.Fatalf("records=%+v", records)
			}
			for _, record := range records {
				if record["harness"] == "" || record["metadata"] == nil {
					t.Fatalf("identity missing: %+v", record)
				}
			}
		})
	}
}

type countingGroupInput struct {
	Calls *int `json:"-"`
}

func (i countingGroupInput) EvalGroupID() string {
	*i.Calls++
	return "counted"
}

func TestIterationPreparationRunsOnceAndSeparatesDiagnosticKeys(t *testing.T) {
	t.Parallel()
	runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := NewHarnessTable("once", regressionHarness("baseline"), []Harness[any, string]{regressionHarness("candidate")}, 1)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	fake := &fakeTest{name: "once"}
	execution := Run(t.Context(), runner, fake, Case[any, string]{ID: "case", EvalSet: "once", Input: countingGroupInput{Calls: &calls}, Harness: rows[0].Harness})
	fake.runCleanups()
	if execution.Err != nil || calls != 1 {
		t.Fatalf("error=%v identity calls=%d", execution.Err, calls)
	}
	run := newRunContext("invalid")
	plan := rows[0].Harness.(iterationPlanner).iterationPlan()
	if err := prepareIteration(run, plan, make(chan int), "case"); err == nil {
		t.Fatal("invalid input accepted")
	}
	metadata, _ := run.snapshot()
	iteration, ok := parseIteration(metadata[iterationMetadataKey])
	if !ok {
		t.Fatal("diagnostic iteration unavailable")
	}
	var key []any
	if err := json.Unmarshal([]byte(iteration.GroupKey), &key); err != nil {
		t.Fatal(err)
	}
	if len(key) != 3 || !strings.Contains(iteration.GroupKey, "case") {
		t.Fatalf("diagnostic key=%s", iteration.GroupKey)
	}
	if err := prepareIteration(run, plan, "now valid", "case"); err == nil {
		t.Fatal("preparation failure was not retained")
	}
}
