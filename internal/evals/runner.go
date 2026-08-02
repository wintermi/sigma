// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Test is the subset of testing.T used by the evaluation runner.
type Test interface {
	Cleanup(func())
	Errorf(string, ...any)
	Failed() bool
	Helper()
	Name() string
	Skipped() bool
}

// RunnerConfig configures artifact storage for a Runner.
type RunnerConfig struct {
	// ArtifactDir is the exact directory for this invocation. When empty, the
	// runner uses SIGMA_EVAL_ARTIFACT_DIR or creates a unique directory beneath
	// the module root's ignored .eval directory.
	ArtifactDir string
}

// Runner coordinates Go-test lifecycle recording and comparison reporting.
type Runner struct {
	artifactDirectory string
	moduleRoot        string

	mu           sync.Mutex
	observations []Observation
	closed       bool
}

type pendingRun[O any] struct {
	runID        string
	testName     string
	file         string
	harness      string
	result       RunResult[O]
	judgments    []JudgeResult
	averageScore *float64
	runErr       error
	context      *RunContext
}

type runRecord struct {
	SchemaVersion int                 `json:"schemaVersion"`
	RunID         string              `json:"runId"`
	Test          runTestRecord       `json:"test"`
	Harness       string              `json:"harness"`
	Usage         Usage               `json:"usage"`
	Timings       Timings             `json:"timings"`
	Judgments     []JudgeResult       `json:"judgments,omitempty"`
	AverageScore  *float64            `json:"averageScore,omitempty"`
	Errors        []string            `json:"errors,omitempty"`
	Artifacts     []artifactReference `json:"artifacts,omitempty"`
	Metadata      map[string]any      `json:"metadata,omitempty"`
}

type runTestRecord struct {
	File   string `json:"file"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// NewRunner constructs a private artifact-backed evaluation runner.
func NewRunner(config RunnerConfig) (*Runner, error) {
	artifactDirectory := strings.TrimSpace(config.ArtifactDir)
	moduleRoot, err := findModuleRoot()
	if err != nil {
		return nil, err
	}
	if artifactDirectory == "" {
		artifactDirectory, moduleRoot, err = createDefaultArtifactDirectory()
		if err != nil {
			return nil, err
		}
	} else {
		artifactDirectory, err = filepath.Abs(artifactDirectory)
		if err != nil {
			return nil, fmt.Errorf("evals: resolve artifact directory: %w", err)
		}
	}
	if err := ensurePrivateDirectory(artifactDirectory); err != nil {
		return nil, err
	}
	return &Runner{artifactDirectory: artifactDirectory, moduleRoot: moduleRoot}, nil
}

// ArtifactDir returns the exact private artifact directory for this invocation.
func (r *Runner) ArtifactDir() string {
	if r == nil {
		return ""
	}
	return r.artifactDirectory
}

// Observations returns a copy of the comparative observations recorded so far.
func (r *Runner) Observations() []Observation {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneObservations(r.observations)
}

// Run executes one harness and schedules final artifact recording with test cleanup.
func Run[I, O any](ctx context.Context, runner *Runner, test Test, eval Case[I, O]) Execution[O] {
	if test != nil {
		test.Helper()
	}
	execution := Execution[O]{}
	if runner == nil {
		execution.Err = errors.New("evals: runner is required")
		failTest(test, execution.Err)
		return execution
	}
	if test == nil {
		execution.Err = errors.New("evals: test is required")
		return execution
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runID, err := newRunID()
	if err != nil {
		execution.Err = err
		failTest(test, execution.Err)
		return execution
	}
	file := callerFile(runner.moduleRoot)
	pending := &pendingRun[O]{
		runID:    runID,
		testName: test.Name(),
		file:     file,
		context:  newRunContext(runID),
	}
	test.Cleanup(func() {
		if cleanupErr := runner.finishRun(test, pending); cleanupErr != nil {
			test.Errorf("eval artifact recording failed: %v", cleanupErr)
		}
	})

	if err := validateCase(eval); err != nil {
		pending.runErr = err
		execution.Err = err
		failTest(test, err)
		return execution
	}
	pending.harness = strings.TrimSpace(eval.Harness.HarnessName())
	startedAt := time.Now()
	result, runErr := eval.Harness.Run(ctx, eval.Input, pending.context)
	if result.Timings.Total <= 0 {
		result.Timings.Total = time.Since(startedAt)
	}
	pending.result = result
	if _, err := json.Marshal(result); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("evals: harness result must be JSON-serializable: %w", err))
	}
	if runErr == nil {
		for _, judge := range eval.Judges {
			judgment, judgeErr := judge.Score(ctx, JudgmentInput[I, O]{Input: eval.Input, Result: result})
			if judgeErr != nil {
				runErr = errors.Join(runErr, fmt.Errorf("evals: judge %q: %w", judge.Name, judgeErr))
				continue
			}
			if err := validateJudgeResult(judge.Name, judgment); err != nil {
				runErr = errors.Join(runErr, err)
				continue
			}
			pending.judgments = append(pending.judgments, judgment)
		}
	}
	if len(pending.judgments) > 0 {
		var total float64
		for _, judgment := range pending.judgments {
			total += judgment.Score
		}
		average := total / float64(len(pending.judgments))
		pending.averageScore = &average
		if eval.JudgeThreshold != nil && average < *eval.JudgeThreshold {
			test.Errorf(
				"eval average score %.4f is below threshold %.4f",
				average,
				*eval.JudgeThreshold,
			)
		}
	}
	pending.runErr = runErr
	execution = Execution[O]{
		Result:       result,
		Judgments:    append([]JudgeResult(nil), pending.judgments...),
		AverageScore: cloneFloat64(pending.averageScore),
		Err:          runErr,
	}
	if runErr != nil {
		failTest(test, runErr)
	}
	return execution
}

func validateCase[I, O any](eval Case[I, O]) error {
	if strings.TrimSpace(eval.EvalSet) == "" {
		return errors.New("evals: eval set must not be empty")
	}
	if eval.Harness == nil {
		return errors.New("evals: harness is required")
	}
	if strings.TrimSpace(eval.Harness.HarnessName()) == "" {
		return errors.New("evals: harness name must not be empty")
	}
	if eval.JudgeThreshold != nil && (math.IsNaN(*eval.JudgeThreshold) || math.IsInf(*eval.JudgeThreshold, 0)) {
		return errors.New("evals: judge threshold must be finite")
	}
	if eval.JudgeThreshold != nil && len(eval.Judges) == 0 {
		return errors.New("evals: judge threshold requires at least one judge")
	}
	for _, judge := range eval.Judges {
		if strings.TrimSpace(judge.Name) == "" {
			return errors.New("evals: judge name must not be empty")
		}
		if judge.Score == nil {
			return fmt.Errorf("evals: judge %q score function is required", judge.Name)
		}
	}
	return nil
}

func failTest(test Test, err error) {
	if test != nil && err != nil {
		test.Errorf("eval run failed: %v", err)
	}
}

func (r *Runner) finishRun(test Test, pending interface {
	finishData() finishRunData
},
) error {
	return r.finish(test, pending.finishData())
}

type finishRunData struct {
	runID        string
	testName     string
	file         string
	harness      string
	usage        Usage
	timings      Timings
	judgments    []JudgeResult
	averageScore *float64
	runErr       error
	context      *RunContext
}

func (p *pendingRun[O]) finishData() finishRunData {
	return finishRunData{
		runID:        p.runID,
		testName:     p.testName,
		file:         p.file,
		harness:      p.harness,
		usage:        p.result.Usage,
		timings:      p.result.Timings,
		judgments:    append([]JudgeResult(nil), p.judgments...),
		averageScore: cloneFloat64(p.averageScore),
		runErr:       p.runErr,
		context:      p.context,
	}
}

func (r *Runner) finish(test Test, pending finishRunData) error {
	metadata, attachments := pending.context.snapshot()
	references, artifactErr := persistAttachments(r.artifactDirectory, pending.runID, attachments)
	status := "passed"
	if test.Skipped() {
		status = "skipped"
	} else if test.Failed() || artifactErr != nil {
		status = "failed"
	}
	errorsList := errorStrings(pending.runErr)
	if artifactErr != nil {
		errorsList = append(errorsList, artifactErr.Error())
	}
	record := runRecord{
		SchemaVersion: 1,
		RunID:         pending.runID,
		Test:          runTestRecord{File: pending.file, Name: pending.testName, Status: status},
		Harness:       pending.harness,
		Usage:         pending.usage,
		Timings:       pending.timings,
		Judgments:     pending.judgments,
		AverageScore:  pending.averageScore,
		Errors:        errorsList,
		Artifacts:     references,
		Metadata:      metadata,
	}
	appendErr := r.appendRunRecord(record)
	persistErr := errors.Join(artifactErr, appendErr)
	if iteration, ok := parseIteration(metadata[iterationMetadataKey]); ok {
		r.addObservation(observationFromRun(iteration, pending, test, persistErr))
	}
	if persistErr != nil {
		return errors.Join(pending.runErr, persistErr)
	}
	return nil
}

func observationFromRun(iteration Iteration, pending finishRunData, test Test, persistErr error) Observation {
	outcome := OutcomeUnscored
	switch {
	case pending.runErr != nil || persistErr != nil:
		outcome = OutcomeErrored
	case pending.averageScore != nil:
		outcome = OutcomeScored
	case test.Skipped():
		outcome = OutcomeSkipped
	case test.Failed():
		outcome = OutcomeErrored
	}
	totalTokens := float64(pending.usage.TotalTokens)
	totalMS := float64(pending.timings.Total) / float64(time.Millisecond)
	return Observation{
		EvalSet:          iteration.EvalSet,
		GroupKey:         iteration.GroupKey,
		TestName:         pending.testName,
		File:             pending.file,
		Harness:          iteration.Harness,
		Baseline:         iteration.Baseline,
		Candidates:       append([]string(nil), iteration.Candidates...),
		Repetition:       iteration.Repetition,
		Outcome:          outcome,
		Score:            cloneFloat64(pending.averageScore),
		TotalTokens:      &totalTokens,
		TotalMS:          &totalMS,
		EstimatedCostUSD: cloneFloat64(pending.usage.EstimatedCostUSD),
	}
}

func (r *Runner) appendRunRecord(record runRecord) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("evals: encode run record: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("evals: runner is closed")
	}
	path := filepath.Join(r.artifactDirectory, "runs.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("evals: open run report: %w", err)
	}
	_, writeErr := file.Write(append(encoded, '\n'))
	closeErr := file.Close()
	chmodErr := os.Chmod(path, 0o600)
	if err := errors.Join(writeErr, closeErr, chmodErr); err != nil {
		return fmt.Errorf("evals: append run report: %w", err)
	}
	return nil
}

func (r *Runner) addObservation(observation Observation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations = append(r.observations, observation)
}

// Close prints the final comparison report and prevents further records.
func (r *Runner) Close(output io.Writer) error {
	if r == nil {
		return errors.New("evals: runner is required")
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	observations := cloneObservations(r.observations)
	r.mu.Unlock()

	formatted := FormatComparisonReport(SummarizeComparisons(observations))
	if formatted == "" || output == nil {
		return nil
	}
	if _, err := fmt.Fprintln(output, formatted); err != nil {
		return fmt.Errorf("evals: write comparison report: %w", err)
	}
	return nil
}

func cloneObservations(observations []Observation) []Observation {
	cloned := make([]Observation, len(observations))
	for i, observation := range observations {
		cloned[i] = observation
		cloned[i].Candidates = append([]string(nil), observation.Candidates...)
		cloned[i].Score = cloneFloat64(observation.Score)
		cloned[i].TotalTokens = cloneFloat64(observation.TotalTokens)
		cloned[i].TotalMS = cloneFloat64(observation.TotalMS)
		cloned[i].EstimatedCostUSD = cloneFloat64(observation.EstimatedCostUSD)
	}
	return cloned
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func errorStrings(err error) []string {
	if err == nil {
		return nil
	}
	type multiUnwrapper interface {
		Unwrap() []error
	}
	if joined, ok := err.(multiUnwrapper); ok {
		var messages []string
		for _, child := range joined.Unwrap() {
			messages = append(messages, errorStrings(child)...)
		}
		return messages
	}
	return []string{err.Error()}
}

func callerFile(moduleRoot string) string {
	_, file, _, ok := runtime.Caller(2)
	if !ok {
		return "unknown"
	}
	relative, err := filepath.Rel(moduleRoot, file)
	if err != nil || strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(file)
	}
	return filepath.ToSlash(relative)
}
