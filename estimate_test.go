// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestEstimateTextTokensRoundsUp(t *testing.T) {
	t.Parallel()

	if got, want := sigma.EstimateTextTokens("12345"), 2; got != want {
		t.Fatalf("EstimateTextTokens = %d, want %d", got, want)
	}
	if got, want := sigma.EstimateTextTokens("ééééé"), 2; got != want {
		t.Fatalf("EstimateTextTokens unicode = %d, want %d", got, want)
	}
}

func TestEstimateRequestTokensWithoutUsageAnchor(t *testing.T) {
	t.Parallel()

	req := sigma.Request{
		SystemPrompt: "1234",
		Messages: []sigma.Message{
			sigma.UserContent(
				sigma.Text("12345"),
				sigma.ImageURL("image/png", "https://example.test/image.png"),
				sigma.DocumentFileID("application/pdf", "input.pdf", "file_123"),
			),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.Thinking("123456789", "sig"),
					sigma.ToolCallBlock("call_1", "tool", map[string]any{"q": "1234"}),
				},
			},
			sigma.ToolResult("call_1", "1234"),
		},
	}

	estimate := sigma.EstimateRequestTokens(req)
	if got, want := estimate.Tokens, 2411; got != want {
		t.Fatalf("EstimateRequestTokens tokens = %d, want %d", got, want)
	}
	if estimate.LastUsageMessageIndex != nil {
		t.Fatalf("last usage message index = %v, want nil", *estimate.LastUsageMessageIndex)
	}
	if got, want := estimate.TrailingTokens, estimate.Tokens; got != want {
		t.Fatalf("trailing tokens = %d, want %d", got, want)
	}
}

func TestEstimateRequestTokensIncludesTools(t *testing.T) {
	t.Parallel()

	req := sigma.Request{
		Messages: []sigma.Message{sigma.UserText("1234")},
		Tools: []sigma.Tool{{
			Name:        "x",
			InputSchema: sigma.Schema{"type": "object"},
		}},
	}

	estimate := sigma.EstimateRequestTokens(req)
	toolJSON := `[{"name":"x","inputSchema":{"type":"object"}}]`
	want := 1 + ((len(toolJSON) + 3) / 4)
	if got := estimate.Tokens; got != want {
		t.Fatalf("EstimateRequestTokens tokens = %d, want %d", got, want)
	}
}

func TestEstimateRequestTokensUsesLatestSuccessfulUsageAnchor(t *testing.T) {
	t.Parallel()

	req := sigma.Request{
		SystemPrompt: "this should be covered by provider usage",
		Messages: []sigma.Message{
			sigma.UserText("this should be covered by provider usage"),
			{
				Role:       sigma.RoleAssistant,
				Content:    []sigma.ContentBlock{sigma.Text("covered")},
				StopReason: sigma.StopReasonEndTurn,
				Usage:      &sigma.Usage{TotalTokens: 200},
			},
			sigma.UserText("12345"),
			{
				Role:       sigma.RoleAssistant,
				Content:    []sigma.ContentBlock{sigma.Text("12345678")},
				StopReason: sigma.StopReasonAborted,
				Usage:      &sigma.Usage{TotalTokens: 999},
			},
		},
		Tools: []sigma.Tool{{
			Name:        "covered_by_usage",
			InputSchema: sigma.Schema{"type": "object"},
		}},
	}

	estimate := sigma.EstimateRequestTokens(req)
	if got, want := estimate.Tokens, 204; got != want {
		t.Fatalf("EstimateRequestTokens tokens = %d, want %d", got, want)
	}
	if got, want := estimate.UsageTokens, 200; got != want {
		t.Fatalf("usage tokens = %d, want %d", got, want)
	}
	if got, want := estimate.TrailingTokens, 4; got != want {
		t.Fatalf("trailing tokens = %d, want %d", got, want)
	}
	if estimate.LastUsageMessageIndex == nil || *estimate.LastUsageMessageIndex != 1 {
		t.Fatalf("last usage message index = %v, want 1", estimate.LastUsageMessageIndex)
	}
}

func TestEstimateRequestTokensUsesUsageTotalFallback(t *testing.T) {
	t.Parallel()

	req := sigma.Request{
		Messages: []sigma.Message{{
			Role: sigma.RoleAssistant,
			Usage: &sigma.Usage{
				InputTokens:           3,
				OutputTokens:          4,
				CacheReadInputTokens:  5,
				CacheWriteInputTokens: 6,
				ToolUseInputTokens:    7,
			},
		}},
	}

	estimate := sigma.EstimateRequestTokens(req)
	if got, want := estimate.Tokens, 25; got != want {
		t.Fatalf("EstimateRequestTokens tokens = %d, want %d", got, want)
	}
}

func TestMaxTokensForContextUsesRequestedCapWhenContextAllows(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ContextWindow: 5000, MaxOutputTokens: 1000}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if got, want := sigma.MaxTokensForContext(model, req, 100), 100; got != want {
		t.Fatalf("MaxTokensForContext = %d, want %d", got, want)
	}
}

func TestMaxTokensForContextClampsToAvailableContext(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ContextWindow: 5000, MaxOutputTokens: 1000}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if got, want := sigma.MaxTokensForContext(model, req, 1000), 902; got != want {
		t.Fatalf("MaxTokensForContext = %d, want %d", got, want)
	}
}

func TestMaxTokensForContextUsesUsageAnchoredEstimate(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ContextWindow: 5000, MaxOutputTokens: 1000}
	req := sigma.Request{Messages: []sigma.Message{
		sigma.UserText("covered by provider usage"),
		{
			Role:       sigma.RoleAssistant,
			StopReason: sigma.StopReasonEndTurn,
			Usage:      &sigma.Usage{TotalTokens: 800},
		},
		sigma.UserText("12345678"),
	}}

	if got, want := sigma.MaxTokensForContext(model, req, 1000), 102; got != want {
		t.Fatalf("MaxTokensForContext = %d, want %d", got, want)
	}
}

func TestMaxTokensForContextKeepsPositiveBudgetForOverfullContext(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ContextWindow: 4098, MaxOutputTokens: 1000}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("123456789")}}

	if got, want := sigma.MaxTokensForContext(model, req, 1000), 1; got != want {
		t.Fatalf("MaxTokensForContext = %d, want %d", got, want)
	}
}

func TestMaxTokensForContextUnknownContextOnlyAppliesOutputCap(t *testing.T) {
	t.Parallel()

	model := sigma.Model{MaxOutputTokens: 500}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if got, want := sigma.MaxTokensForContext(model, req, 250), 250; got != want {
		t.Fatalf("MaxTokensForContext requested cap = %d, want %d", got, want)
	}
	if got, want := sigma.MaxTokensForContext(model, req, 0), 500; got != want {
		t.Fatalf("MaxTokensForContext model cap = %d, want %d", got, want)
	}
}

func TestMaxTokensForContextWithoutUsableCapReturnsZero(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ContextWindow: 5000}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if got := sigma.MaxTokensForContext(model, req, 0); got != 0 {
		t.Fatalf("MaxTokensForContext = %d, want 0", got)
	}
}
