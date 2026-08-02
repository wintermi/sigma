// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestSigmaHarnessRunsConversationAndAggregatesTelemetry(t *testing.T) {
	t.Parallel()

	model := sigmatest.TextModel()
	model.InputCostPerMillion = 2
	model.OutputCostPerMillion = 4
	provider := sigmatest.NewFauxProvider(
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Thinking("work", "signature"), sigma.Text("first")},
			Usage:   &sigma.Usage{InputTokens: 4, OutputTokens: 2, TotalTokens: 6, CacheReadInputTokens: 1},
		}},
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("second")},
			Usage:   &sigma.Usage{InputTokens: 8, OutputTokens: 3, TotalTokens: 11, CacheWriteInputTokens: 2},
		}},
	)
	client := sigmaHarnessTestClient(t, provider, model)
	harness, err := NewSigmaHarness(SigmaHarnessConfig{
		Name:        "conversation",
		Client:      client,
		Model:       model,
		BaseRequest: sigma.Request{SystemPrompt: "base"},
		TransformSystemPrompt: func(prompt string) (string, error) {
			return prompt + " transformed", nil
		},
	}, func(_ context.Context, run SigmaRun) (map[string]any, error) {
		return map[string]any{"response": run.Response, "messages": len(run.Messages)}, nil
	})
	if err != nil {
		t.Fatalf("NewSigmaHarness returned error: %v", err)
	}
	run := newRunContext("conversation-run")
	result, err := harness.Run(context.Background(), Conversation("first prompt", "second prompt"), run)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Output["response"] != "second" || result.Output["messages"] != 4 {
		t.Fatalf("output = %#v", result.Output)
	}
	if result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 5 || result.Usage.TotalTokens != 17 {
		t.Fatalf("usage = %#v", result.Usage)
	}
	if result.Usage.EstimatedCostUSD == nil || *result.Usage.EstimatedCostUSD <= 0 {
		t.Fatalf("estimated cost = %v", result.Usage.EstimatedCostUSD)
	}
	requests := provider.Requests()
	if len(requests) != 2 || requests[0].Request.SystemPrompt != "base transformed" {
		t.Fatalf("requests = %#v", requests)
	}
	if len(requests[1].Request.Messages) != 3 || requests[1].Request.Messages[1].Role != sigma.RoleAssistant {
		t.Fatalf("second request history = %#v", requests[1].Request.Messages)
	}
	_, attachments := run.snapshot()
	if len(attachments) != 1 || attachments[0].Name != "transcript.jsonl" ||
		!strings.Contains(string(attachments[0].Body), "second prompt") {
		t.Fatalf("transcript attachment = %#v", attachments)
	}
}

func TestSigmaHarnessPreservesToolTraceWithoutExecutingIt(t *testing.T) {
	t.Parallel()

	model := sigmatest.TextModel()
	provider := sigmatest.NewFauxProvider(sigmatest.Script{Final: sigma.AssistantMessage{
		Content: []sigma.ContentBlock{sigma.Text("done")},
		Usage:   &sigma.Usage{TotalTokens: 1},
	}})
	client := sigmaHarnessTestClient(t, provider, model)
	base := sigma.Request{Messages: []sigma.Message{
		{
			Role:       sigma.RoleAssistant,
			Content:    []sigma.ContentBlock{sigma.ToolCallBlock("call-1", "lookup", map[string]any{"q": "sigma"})},
			Provider:   model.Provider,
			API:        model.API,
			Model:      model.ID,
			StopReason: sigma.StopReasonToolCalls,
		},
		{Role: sigma.RoleTool, ToolCallID: "call-1", ToolName: "lookup", Content: []sigma.ContentBlock{sigma.Text("result")}},
	}}
	harness, err := NewSigmaTextHarness(SigmaHarnessConfig{Client: client, Model: model, BaseRequest: base})
	if err != nil {
		t.Fatalf("NewSigmaTextHarness returned error: %v", err)
	}
	result, err := harness.Run(context.Background(), Prompt("continue"), newRunContext("tools"))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	var hasCall, hasResult bool
	for _, event := range result.Events {
		hasCall = hasCall || event.Type == "tool_call" && event.Name == "lookup"
		hasResult = hasResult || event.Type == "tool_result" && event.ToolCallID == "call-1"
	}
	if !hasCall || !hasResult {
		t.Fatalf("events = %#v", result.Events)
	}
	if len(provider.Requests()) != 1 {
		t.Fatalf("provider request count = %d, want 1", len(provider.Requests()))
	}
}

func TestSigmaHarnessFailureBoundariesAndCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      SigmaInput
		transform  func(string) (string, error)
		script     sigmatest.Script
		wantError  string
		useTimeout bool
	}{
		{name: "no prompts", input: SigmaInput{}, script: sigmatest.Script{}, wantError: "at least one prompt"},
		{name: "empty prompt", input: Prompt("   "), script: sigmatest.Script{}, wantError: "must not be empty"},
		{
			name:      "system prompt transform",
			input:     Prompt("hello"),
			transform: func(string) (string, error) { return "", errors.New("transform failed") },
			script:    sigmatest.Script{},
			wantError: "transform failed",
		},
		{
			name:  "unexpected stop",
			input: Prompt("hello"),
			script: sigmatest.Script{Final: sigma.AssistantMessage{
				Content:    []sigma.ContentBlock{sigma.ToolCallBlock("call", "lookup", map[string]any{})},
				StopReason: sigma.StopReasonToolCalls,
			}},
			wantError: "unexpected stop reason",
		},
		{
			name:      "missing text",
			input:     Prompt("hello"),
			script:    sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Thinking("work", "signature")}}},
			wantError: "produced no assistant text",
		},
		{
			name:      "whitespace text",
			input:     Prompt("hello"),
			script:    sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("   ")}}},
			wantError: "produced no assistant text",
		},
		{
			name:       "cancellation",
			input:      Prompt("hello"),
			script:     sigmatest.Script{WaitForCancel: true},
			wantError:  "deadline exceeded",
			useTimeout: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := sigmatest.TextModel()
			provider := sigmatest.NewFauxProvider(tt.script)
			client := sigmaHarnessTestClient(t, provider, model)
			harness, err := NewSigmaTextHarness(SigmaHarnessConfig{
				Client: client, Model: model, TransformSystemPrompt: tt.transform,
			})
			if err != nil {
				t.Fatalf("NewSigmaTextHarness returned error: %v", err)
			}
			ctx := context.Background()
			if tt.useTimeout {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}
			run := newRunContext("failure")
			_, err = harness.Run(ctx, tt.input, run)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Run error = %v, want %q", err, tt.wantError)
			}
			_, attachments := run.snapshot()
			if len(attachments) != 1 {
				t.Fatalf("failure transcript attachments = %d, want 1", len(attachments))
			}
		})
	}
}

func TestSigmaHarnessReturnsPartialProviderFailure(t *testing.T) {
	t.Parallel()

	model := sigmatest.TextModel()
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("partial")}},
		Err:   errors.New("upstream failed"),
	})
	harness, err := NewSigmaTextHarness(SigmaHarnessConfig{
		Client: sigmaHarnessTestClient(t, provider, model),
		Model:  model,
	})
	if err != nil {
		t.Fatalf("NewSigmaTextHarness returned error: %v", err)
	}
	run := newRunContext("partial")
	result, err := harness.Run(context.Background(), Prompt("hello"), run)
	if err == nil || !strings.Contains(err.Error(), "upstream failed") {
		t.Fatalf("Run error = %v", err)
	}
	encoded, err := json.Marshal(result.Events)
	if err != nil {
		t.Fatalf("marshal partial events: %v", err)
	}
	if !strings.Contains(string(encoded), "partial") {
		t.Fatalf("partial events = %s", encoded)
	}
}

func sigmaHarnessTestClient(t *testing.T, provider *sigmatest.FauxProvider, model sigma.Model) *sigma.Client {
	t.Helper()
	registry, err := sigmatest.Registry(provider, model)
	if err != nil {
		t.Fatalf("sigmatest.Registry returned error: %v", err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry))
}
