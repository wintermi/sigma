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
	"math"
	"strings"
	"sync"
	"time"
)

// Harness runs one evaluation input.
type Harness[I, O any] interface {
	HarnessName() string
	Run(context.Context, I, *RunContext) (RunResult[O], error)
}

// HarnessFunc adapts a function into a named Harness.
type HarnessFunc[I, O any] struct {
	Name string
	Func func(context.Context, I, *RunContext) (RunResult[O], error)
}

// HarnessName returns the stable harness name.
func (h HarnessFunc[I, O]) HarnessName() string {
	return h.Name
}

// Run invokes the harness function.
func (h HarnessFunc[I, O]) Run(ctx context.Context, input I, run *RunContext) (RunResult[O], error) {
	if h.Func == nil {
		return RunResult[O]{}, errors.New("evals: harness function is required")
	}
	return h.Func(ctx, input, run)
}

// TranscriptEvent is a normalized model or tool trace event.
type TranscriptEvent struct {
	Type       string         `json:"type"`
	Role       string         `json:"role,omitempty"`
	Content    any            `json:"content,omitempty"`
	ID         string         `json:"id,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
	Name       string         `json:"name,omitempty"`
	Arguments  map[string]any `json:"arguments,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// Usage records comparable telemetry for one harness run.
type Usage struct {
	Provider         string         `json:"provider,omitempty"`
	Model            string         `json:"model,omitempty"`
	InputTokens      int            `json:"inputTokens,omitempty"`
	OutputTokens     int            `json:"outputTokens,omitempty"`
	TotalTokens      int            `json:"totalTokens,omitempty"`
	ToolCalls        int            `json:"toolCalls,omitempty"`
	EstimatedCostUSD *float64       `json:"estimatedCostUsd,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// Timings records harness latency.
type Timings struct {
	Total time.Duration `json:"-"`
}

// MarshalJSON reports timing values in milliseconds.
func (t Timings) MarshalJSON() ([]byte, error) {
	type timingJSON struct {
		TotalMS float64 `json:"totalMs"`
	}
	return json.Marshal(timingJSON{TotalMS: float64(t.Total) / float64(time.Millisecond)})
}

// RunResult is the output and normalized trace from one harness run.
type RunResult[O any] struct {
	Output  O                 `json:"output"`
	Events  []TranscriptEvent `json:"events,omitempty"`
	Usage   Usage             `json:"usage"`
	Timings Timings           `json:"timings"`
}

// JudgmentInput supplies a run and its original input to a Judge.
type JudgmentInput[I, O any] struct {
	Input  I
	Result RunResult[O]
}

// Judge scores one completed harness run.
type Judge[I, O any] struct {
	Name  string
	Score func(context.Context, JudgmentInput[I, O]) (JudgeResult, error)
}

// JudgeResult is one judge's numeric score and optional explanation.
type JudgeResult struct {
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
}

// Case configures one harness execution.
type Case[I, O any] struct {
	EvalSet        string
	Input          I
	Harness        Harness[I, O]
	Judges         []Judge[I, O]
	JudgeThreshold *float64
}

// Execution is the immediate result returned by Run.
type Execution[O any] struct {
	Result       RunResult[O]
	Judgments    []JudgeResult
	AverageScore *float64
	Err          error
}

// AttachmentCategory selects the private artifact subdirectory.
type AttachmentCategory string

const (
	// AttachmentTranscript stores complete model transcripts.
	AttachmentTranscript AttachmentCategory = "transcripts"
	// AttachmentSource stores source material captured by an evaluation.
	AttachmentSource AttachmentCategory = "sources"
	// AttachmentFile stores other caller attachments.
	AttachmentFile AttachmentCategory = "attachments"
)

type attachment struct {
	Category    AttachmentCategory
	Name        string
	ContentType string
	Body        []byte
}

// RunContext collects JSON metadata and private attachments for one run.
type RunContext struct {
	runID string

	mu          sync.Mutex
	metadata    map[string]any
	attachments []attachment
}

func newRunContext(runID string) *RunContext {
	return &RunContext{runID: runID, metadata: make(map[string]any)}
}

// RunID returns the unique identifier for this evaluation run.
func (r *RunContext) RunID() string {
	if r == nil {
		return ""
	}
	return r.runID
}

// SetMetadata stores a JSON-safe copy of value under name.
func (r *RunContext) SetMetadata(name string, value any) error {
	if r == nil {
		return errors.New("evals: run context is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("evals: metadata name must not be empty")
	}
	value, err := cloneJSONValue(value)
	if err != nil {
		return fmt.Errorf("evals: metadata %q: %w", name, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metadata[name] = value
	return nil
}

// Attach records a private artifact to persist after the test finishes.
func (r *RunContext) Attach(category AttachmentCategory, name, contentType string, body []byte) error {
	if r == nil {
		return errors.New("evals: run context is required")
	}
	if err := validateAttachment(category, name, contentType); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attachments = append(r.attachments, attachment{
		Category:    category,
		Name:        name,
		ContentType: contentType,
		Body:        append([]byte(nil), body...),
	})
	return nil
}

func (r *RunContext) snapshot() (map[string]any, []attachment) {
	r.mu.Lock()
	defer r.mu.Unlock()

	metadata := make(map[string]any, len(r.metadata))
	for name, value := range r.metadata {
		metadata[name] = value
	}
	attachments := make([]attachment, len(r.attachments))
	for i, item := range r.attachments {
		attachments[i] = item
		attachments[i].Body = append([]byte(nil), item.Body...)
	}
	return metadata, attachments
}

func cloneJSONValue(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode JSON value: %w", err)
	}
	var cloned any
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return nil, fmt.Errorf("decode JSON value: %w", err)
	}
	return cloned, nil
}

func validateJudgeResult(name string, result JudgeResult) error {
	if math.IsNaN(result.Score) || math.IsInf(result.Score, 0) {
		return fmt.Errorf("evals: judge %q returned a non-finite score", name)
	}
	return nil
}
