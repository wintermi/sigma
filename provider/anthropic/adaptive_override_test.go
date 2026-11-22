// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"testing"

	"github.com/wintermi/sigma"
)

// A router that only accepts adaptive thinking opts in with MessagesCompat;
// the model's own thinking format still takes precedence.
func TestMessagesCompatAdaptiveThinkingOverride(t *testing.T) {
	t.Parallel()
	override := &MessagesCompat{AdaptiveThinking: true}
	model := sigma.Model{ID: "claude-router", Provider: "router", API: sigma.APIAnthropicMessages, SupportsThinking: true}
	if got := thinkingFormat(model, anthropicMessagesCompat(model, "https://router.example.test", override)); got != sigma.AnthropicThinkingAdaptive {
		t.Fatalf("format = %q, want adaptive", got)
	}
	if got := thinkingFormat(model, anthropicMessagesCompat(model, "https://router.example.test", nil)); got != sigma.AnthropicThinkingBudget {
		t.Fatalf("format without override = %q, want budget", got)
	}
	model.AnthropicMessagesCompat = &sigma.AnthropicMessagesCompat{ThinkingFormat: sigma.AnthropicThinkingBudget}
	if got := thinkingFormat(model, anthropicMessagesCompat(model, "https://router.example.test", override)); got != sigma.AnthropicThinkingBudget {
		t.Fatalf("format with model metadata = %q, want budget", got)
	}
}
