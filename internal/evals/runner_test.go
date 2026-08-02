// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeTest struct {
	mu       sync.Mutex
	name     string
	failed   bool
	skipped  bool
	errors   []string
	cleanups []func()
}

func (t *fakeTest) Cleanup(cleanup func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cleanups = append(t.cleanups, cleanup)
}

func (t *fakeTest) Errorf(format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed = true
	t.errors = append(t.errors, fmt.Sprintf(format, args...))
}

func (t *fakeTest) Failed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failed
}

func (*fakeTest) Helper() {}

func (t *fakeTest) Name() string {
	return t.name
}

func (t *fakeTest) Skipped() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.skipped
}

func (t *fakeTest) runCleanups() {
	t.mu.Lock()
	cleanups := append([]func(){}, t.cleanups...)
	t.mu.Unlock()
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
}

func TestRunnerRecordsArtifactsJudgesAndComparisons(t *testing.T) {
	t.Parallel()

	artifactDirectory := t.TempDir()
	runner, err := NewRunner(RunnerConfig{ArtifactDir: artifactDirectory})
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	newHarness := func(name string, tokens int) Harness[identifiedInput, string] {
		return HarnessFunc[identifiedInput, string]{
			Name: name,
			Func: func(_ context.Context, input identifiedInput, run *RunContext) (RunResult[string], error) {
				if err := run.Attach(AttachmentSource, name+".txt", "text/plain", []byte(input.Value)); err != nil {
					return RunResult[string]{}, err
				}
				return RunResult[string]{
					Output:  name,
					Usage:   Usage{TotalTokens: tokens},
					Timings: Timings{Total: time.Duration(tokens) * time.Millisecond},
				}, nil
			},
		}
	}
	rows, err := NewHarnessTable(
		"runner comparison",
		newHarness("baseline", 10),
		[]Harness[identifiedInput, string]{newHarness("candidate", 8)},
		1,
	)
	if err != nil {
		t.Fatalf("NewHarnessTable returned error: %v", err)
	}
	for _, row := range rows {
		row := row
		fake := &fakeTest{name: "TestRunnerComparison"}
		execution := Run(context.Background(), runner, fake, Case[identifiedInput, string]{
			EvalSet: "runner comparison",
			Input:   identifiedInput{ID: "input", Value: "source"},
			Harness: row.Harness,
			Judges: []Judge[identifiedInput, string]{
				{
					Name: "exact",
					Score: func(_ context.Context, input JudgmentInput[identifiedInput, string]) (JudgeResult, error) {
						if input.Result.Output == "candidate" {
							return JudgeResult{Score: 1}, nil
						}
						return JudgeResult{Score: 0}, nil
					},
				},
			},
		})
		if execution.Err != nil || fake.Failed() {
			t.Fatalf("row %s execution = %#v, errors = %#v", row.Name, execution, fake.errors)
		}
		fake.runCleanups()
		if fake.Failed() {
			t.Fatalf("row %s cleanup errors = %#v", row.Name, fake.errors)
		}
	}

	var report bytes.Buffer
	if err := runner.Close(&report); err != nil {
		t.Fatalf("Runner.Close returned error: %v", err)
	}
	if !strings.Contains(report.String(), "+100.0 pp") || !strings.Contains(report.String(), "-2.0") {
		t.Fatalf("comparison report = %q", report.String())
	}
	runs := readJSONLines(t, filepath.Join(artifactDirectory, "runs.jsonl"))
	if len(runs) != 2 {
		t.Fatalf("run record count = %d, want 2", len(runs))
	}
	paths := make(map[string]struct{}, len(runs))
	for _, record := range runs {
		artifacts, ok := record["artifacts"].([]any)
		if !ok || len(artifacts) != 1 {
			t.Fatalf("record artifacts = %#v", record["artifacts"])
		}
		path := artifacts[0].(map[string]any)["path"].(string)
		if _, exists := paths[path]; exists {
			t.Fatalf("artifact path %q was reused across runs", path)
		}
		paths[path] = struct{}{}
		body, err := os.ReadFile(filepath.Join(artifactDirectory, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read artifact: %v", err)
		}
		if string(body) != "source" {
			t.Fatalf("artifact body = %q", body)
		}
		absolutePath := filepath.Join(artifactDirectory, filepath.FromSlash(path))
		assertPrivateMode(t, absolutePath, 0o600)
		assertPrivateMode(t, filepath.Dir(absolutePath), 0o700)
	}
	assertPrivateMode(t, artifactDirectory, 0o700)
	assertPrivateMode(t, filepath.Join(artifactDirectory, "runs.jsonl"), 0o600)
}

func TestRunnerThresholdFailureRetainsScoredObservation(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	harness := HarnessFunc[identifiedInput, string]{
		Name: "baseline",
		Func: func(context.Context, identifiedInput, *RunContext) (RunResult[string], error) {
			return RunResult[string]{Output: "no"}, nil
		},
	}
	rows, err := NewHarnessTable("threshold", harness, []Harness[identifiedInput, string]{
		HarnessFunc[identifiedInput, string]{
			Name: "candidate",
			Func: func(context.Context, identifiedInput, *RunContext) (RunResult[string], error) {
				return RunResult[string]{Output: "no"}, nil
			},
		},
	}, 1)
	if err != nil {
		t.Fatalf("NewHarnessTable returned error: %v", err)
	}
	threshold := 1.0
	fake := &fakeTest{name: "TestThreshold"}
	Run(context.Background(), runner, fake, Case[identifiedInput, string]{
		EvalSet:        "threshold",
		Input:          identifiedInput{ID: "input"},
		Harness:        rows[0].Harness,
		JudgeThreshold: &threshold,
		Judges: []Judge[identifiedInput, string]{
			{Name: "score", Score: func(context.Context, JudgmentInput[identifiedInput, string]) (JudgeResult, error) {
				return JudgeResult{Score: 0.5}, nil
			}},
		},
	})
	if !fake.Failed() {
		t.Fatal("threshold miss did not fail the test")
	}
	fake.runCleanups()
	observations := runner.Observations()
	if len(observations) != 1 || observations[0].Outcome != OutcomeScored ||
		observations[0].Score == nil || *observations[0].Score != 0.5 {
		t.Fatalf("threshold observation = %#v", observations)
	}
}

func TestRunnerAppendsConcurrentJSONLines(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	const count = 20
	var wait sync.WaitGroup
	errCh := make(chan error, count)
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			errCh <- runner.appendRunRecord(runRecord{
				SchemaVersion: 1,
				RunID:         fmt.Sprintf("run-%d", index),
				Test:          runTestRecord{Name: "concurrent", Status: "passed"},
				Harness:       "fake",
			})
		}(i)
	}
	wait.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("appendRunRecord returned error: %v", err)
		}
	}
	lines := readJSONLines(t, filepath.Join(runner.ArtifactDir(), "runs.jsonl"))
	if len(lines) != count {
		t.Fatalf("run record count = %d, want %d", len(lines), count)
	}
}

func TestRunContextRejectsUnsafeOrNonJSONArtifacts(t *testing.T) {
	t.Parallel()

	run := newRunContext("run")
	for _, invalid := range []struct {
		category    AttachmentCategory
		name        string
		contentType string
	}{
		{category: AttachmentFile, name: "../secret", contentType: "text/plain"},
		{category: AttachmentFile, name: "nested/secret", contentType: "text/plain"},
		{category: AttachmentCategory("unknown"), name: "secret", contentType: "text/plain"},
		{category: AttachmentFile, name: "secret", contentType: ""},
	} {
		if err := run.Attach(invalid.category, invalid.name, invalid.contentType, []byte("x")); err == nil {
			t.Fatalf("Attach accepted category %q, name %q, content type %q", invalid.category, invalid.name, invalid.contentType)
		}
	}
	if err := run.SetMetadata("callback", func() {}); err == nil {
		t.Fatal("SetMetadata accepted a function")
	}
	body := []byte("original")
	if err := run.Attach(AttachmentFile, "copy.txt", "text/plain", body); err != nil {
		t.Fatalf("Attach returned error: %v", err)
	}
	body[0] = 'X'
	_, attachments := run.snapshot()
	if string(attachments[0].Body) != "original" {
		t.Fatalf("attachment body was not copied: %q", attachments[0].Body)
	}
}

func TestRunnerJoinsHarnessAndPersistenceFailures(t *testing.T) {
	t.Parallel()

	artifactDirectory := t.TempDir()
	runner, err := NewRunner(RunnerConfig{ArtifactDir: artifactDirectory})
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	fake := &fakeTest{name: "TestJoinedFailure"}
	harnessErr := errors.New("harness failed")
	execution := Run(context.Background(), runner, fake, Case[string, string]{
		EvalSet: "joined failures",
		Input:   "input",
		Harness: HarnessFunc[string, string]{
			Name: "failing",
			Func: func(context.Context, string, *RunContext) (RunResult[string], error) {
				return RunResult[string]{Output: "partial"}, harnessErr
			},
		},
	})
	if !errors.Is(execution.Err, harnessErr) {
		t.Fatalf("execution error = %v, want harness failure", execution.Err)
	}
	if err := os.Mkdir(filepath.Join(artifactDirectory, "runs.jsonl"), 0o700); err != nil {
		t.Fatalf("create conflicting run report directory: %v", err)
	}
	initialErrors := len(fake.errors)
	fake.runCleanups()
	joined := strings.Join(fake.errors[initialErrors:], "\n")
	if !strings.Contains(joined, harnessErr.Error()) || !strings.Contains(joined, "run report") {
		t.Fatalf("cleanup errors = %q, want harness and persistence failures", joined)
	}
}

func TestRunnerRejectsNonJSONOutputAndNonFiniteJudgeScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		harness   Harness[string, any]
		judges    []Judge[string, any]
		threshold *float64
		want      string
	}{
		{
			name: "output",
			harness: HarnessFunc[string, any]{
				Name: "invalid-output",
				Func: func(context.Context, string, *RunContext) (RunResult[any], error) {
					return RunResult[any]{Output: make(chan int)}, nil
				},
			},
			want: "JSON-serializable",
		},
		{
			name: "judge",
			harness: HarnessFunc[string, any]{
				Name: "invalid-judge",
				Func: func(context.Context, string, *RunContext) (RunResult[any], error) {
					return RunResult[any]{Output: "ok"}, nil
				},
			},
			judges: []Judge[string, any]{
				{Name: "nan", Score: func(context.Context, JudgmentInput[string, any]) (JudgeResult, error) {
					return JudgeResult{Score: math.NaN()}, nil
				}},
			},
			want: "non-finite",
		},
		{
			name: "threshold without judge",
			harness: HarnessFunc[string, any]{
				Name: "missing-judge",
				Func: func(context.Context, string, *RunContext) (RunResult[any], error) {
					return RunResult[any]{Output: "ok"}, nil
				},
			},
			threshold: floatPointer(1),
			want:      "requires at least one judge",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner, err := NewRunner(RunnerConfig{ArtifactDir: t.TempDir()})
			if err != nil {
				t.Fatalf("NewRunner returned error: %v", err)
			}
			fake := &fakeTest{name: "TestInvalid"}
			execution := Run(context.Background(), runner, fake, Case[string, any]{
				EvalSet:        "invalid",
				Input:          "input",
				Harness:        tt.harness,
				Judges:         tt.judges,
				JudgeThreshold: tt.threshold,
			})
			if execution.Err == nil || !strings.Contains(execution.Err.Error(), tt.want) || !fake.Failed() {
				t.Fatalf("execution error = %v, test errors = %#v", execution.Err, fake.errors)
			}
			fake.runCleanups()
		})
	}
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	decoded := make([]map[string]any, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal([]byte(line), &decoded[i]); err != nil {
			t.Fatalf("decode JSONL line %d: %v", i, err)
		}
	}
	return decoded
}

func assertPrivateMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %#o, want %#o", path, got, want)
	}
}
