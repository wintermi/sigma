// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerPairsRealSubtestsByCaseIdentity(t *testing.T) {
	t.Parallel()
	runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	harness := func(name string) Harness[string, string] {
		return HarnessFunc[string, string]{Name: name, Func: func(context.Context, string, *RunContext) (RunResult[string], error) {
			return RunResult[string]{Output: "ok"}, nil
		}}
	}
	rows, err := NewHarnessTable("identity", harness("baseline"), []Harness[string, string]{harness("candidate"), harness("second")}, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Both scenarios deliberately reuse the same input. Harness and repetition
	// are present in the Go names but must not become scenario identity.
	for _, id := range []string{"first", "second"} {
		for _, row := range rows {
			t.Run(fmt.Sprintf("%s/%s/%d", id, row.Name, row.Repetition), func(t *testing.T) {
				Run(t.Context(), runner, t, Case[string, string]{
					ID: id, EvalSet: "identity", Input: "same", Harness: row.Harness,
					Judges: []Judge[string, string]{{Name: "correct", Score: func(context.Context, JudgmentInput[string, string]) (JudgeResult, error) {
						return JudgeResult{Score: 1}, nil
					}}},
				})
			})
		}
	}
	report := SummarizeComparisons(runner.Observations())
	if len(report.Diagnostics) != 0 || len(report.EvalSets) != 1 || len(report.EvalSets[0].Comparisons) != 2 {
		t.Fatalf("report = %+v", report)
	}
	for _, comparison := range report.EvalSets[0].Comparisons {
		if comparison.Correctness.EligiblePairs != 4 || comparison.Correctness.TotalPairs != 4 || comparison.TotalTokens.EligiblePairs != 0 {
			t.Fatalf("comparison = %+v", comparison)
		}
	}
}

func TestRunnerValidatorFailureRetainsEvidenceAndSkipsJudges(t *testing.T) {
	t.Parallel()
	runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	harness := HarnessFunc[string, string]{Name: "baseline", Func: func(context.Context, string, *RunContext) (RunResult[string], error) {
		return RunResult[string]{Output: "answer"}, nil
	}}
	rows, err := NewHarnessTable("validation", harness, []Harness[string, string]{HarnessFunc[string, string]{Name: "candidate"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("missing telemetry")
	judged := false
	fake := &fakeTest{name: "validation/baseline"}
	execution := Run(context.Background(), runner, fake, Case[string, string]{
		ID: "answer", EvalSet: "validation", Input: "question", Harness: rows[0].Harness,
		Validate: func(_ context.Context, input JudgmentInput[string, string]) error {
			if input.Input != "question" || input.Result.Output != "answer" {
				t.Fatalf("validator input = %+v", input)
			}
			return wantErr
		},
		Judges: []Judge[string, string]{{Name: "correct", Score: func(context.Context, JudgmentInput[string, string]) (JudgeResult, error) {
			judged = true
			return JudgeResult{Score: 1}, nil
		}}},
	})
	fake.runCleanups()
	if !errors.Is(execution.Err, wantErr) || judged || !fake.Failed() || execution.AverageScore != nil {
		t.Fatalf("execution=%+v judged=%v errors=%v", execution, judged, fake.errors)
	}
	observations := runner.Observations()
	if len(observations) != 1 || observations[0].Outcome != OutcomeErrored || observations[0].TotalTokens != nil {
		t.Fatalf("observations = %+v", observations)
	}
	record := readJSONLines(t, filepath.Join(runner.ArtifactDir(), "runs.jsonl"))[0]
	if record["output"] != "answer" || !strings.Contains(fmt.Sprint(record["errors"]), wantErr.Error()) {
		t.Fatalf("record = %+v", record)
	}
}

func TestRunnerSnapshotsInputOutputAndUsageBeforeCallerMutation(t *testing.T) {
	t.Parallel()
	runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]string{"value": "original input"}
	output := map[string]string{"value": "judged output"}
	tokens := 10
	fake := &fakeTest{name: "snapshot"}
	execution := Run(context.Background(), runner, fake, Case[map[string]string, map[string]string]{
		ID: "snapshot", EvalSet: "evidence", Input: input,
		Harness: HarnessFunc[map[string]string, map[string]string]{Name: "snapshot", Func: func(_ context.Context, input map[string]string, _ *RunContext) (RunResult[map[string]string], error) {
			input["value"] = "harness mutation"
			return RunResult[map[string]string]{Output: output, Usage: Usage{TotalTokens: &tokens}}, nil
		}},
		Judges: []Judge[map[string]string, map[string]string]{{Name: "domain check", Score: func(_ context.Context, input JudgmentInput[map[string]string, map[string]string]) (JudgeResult, error) {
			if input.Result.Output["value"] != "judged output" {
				t.Fatal("judge did not receive original output")
			}
			return JudgeResult{Score: 1}, nil
		}}},
	})
	if execution.Err != nil {
		t.Fatal(execution.Err)
	}
	output["value"], tokens = "caller mutation", 99
	fake.runCleanups()
	record := readJSONLines(t, filepath.Join(runner.ArtifactDir(), "runs.jsonl"))[0]
	if record["schemaVersion"] != float64(2) || record["caseId"] != "snapshot" ||
		record["input"].(map[string]any)["value"] != "original input" ||
		record["output"].(map[string]any)["value"] != "judged output" ||
		record["usage"].(map[string]any)["totalTokens"] != float64(10) ||
		record["judgments"].([]any)[0].(map[string]any)["name"] != "domain check" {
		t.Fatalf("record = %+v", record)
	}
}

func TestRunnerInvalidInputAndCaseIDPreventDispatch(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		id    string
		input any
	}{
		{name: "missing id", id: "  ", input: "valid"},
		{name: "non-JSON input", id: "valid", input: make(chan int)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			fake := &fakeTest{name: tt.name}
			execution := Run(context.Background(), runner, fake, Case[any, string]{
				ID: tt.id, EvalSet: "invalid", Input: tt.input,
				Harness: HarnessFunc[any, string]{Name: "unused", Func: func(context.Context, any, *RunContext) (RunResult[string], error) {
					t.Fatal("invalid case dispatched")
					return RunResult[string]{}, nil
				}},
			})
			fake.runCleanups()
			if execution.Err == nil || !fake.Failed() {
				t.Fatalf("execution = %+v", execution)
			}
			if _, err := os.Stat(filepath.Join(runner.ArtifactDir(), "runs.jsonl")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestObservationDistinguishesMissingAndMeasuredZeroTokens(t *testing.T) {
	t.Parallel()
	for _, tokens := range []*int{nil, intPointer(0), intPointer(10)} {
		observation := observationFromRun(Iteration{}, finishRunData{usage: Usage{TotalTokens: tokens}}, &fakeTest{}, nil)
		if tokens == nil {
			if observation.TotalTokens != nil {
				t.Fatal("missing tokens became zero")
			}
		} else if observation.TotalTokens == nil || *observation.TotalTokens != float64(*tokens) {
			t.Fatalf("measured tokens lost: %+v", observation)
		}
	}
}

func TestRunnerInvalidOutputPreservesOtherEvidence(t *testing.T) {
	t.Parallel()
	runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeTest{name: "invalid-output"}
	execution := Run(context.Background(), runner, fake, Case[string, any]{
		ID: "invalid-output", EvalSet: "evidence", Input: "original input",
		Harness: HarnessFunc[string, any]{Name: "invalid-output", Func: func(_ context.Context, _ string, run *RunContext) (RunResult[any], error) {
			if err := run.Attach(AttachmentTranscript, "partial.txt", "text/plain", []byte("partial response")); err != nil {
				return RunResult[any]{}, err
			}
			return RunResult[any]{Output: make(chan int), Usage: Usage{TotalTokens: intPointer(10)}}, nil
		}},
		Validate: func(context.Context, JudgmentInput[string, any]) error {
			t.Fatal("validator ran on an unserializable result")
			return nil
		},
	})
	fake.runCleanups()
	if execution.Err == nil || !fake.Failed() {
		t.Fatalf("execution = %+v", execution)
	}
	record := readJSONLines(t, filepath.Join(runner.ArtifactDir(), "runs.jsonl"))[0]
	if record["input"] != "original input" || record["output"] != nil ||
		record["usage"].(map[string]any)["totalTokens"] != float64(10) ||
		len(record["artifacts"].([]any)) != 1 || !strings.Contains(fmt.Sprint(record["errors"]), "JSON-serializable") {
		t.Fatalf("valid evidence discarded: %+v", record)
	}
}
