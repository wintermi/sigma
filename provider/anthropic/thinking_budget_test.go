// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestDefaultThinkingBudgets(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ID: "claude-sonnet-4-5", Provider: sigma.ProviderAnthropic, API: sigma.APIAnthropicMessages, SupportsThinking: true}
	for level, want := range map[sigma.ThinkingLevel]int{
		sigma.ThinkingLevelMinimal: 1024,
		sigma.ThinkingLevelLow:     2048,
		sigma.ThinkingLevelMedium:  8192,
		sigma.ThinkingLevelHigh:    16384,
		sigma.ThinkingLevelXHigh:   16384,
	} {
		if got := thinkingBudget(model, sigma.Options{ReasoningLevel: level}); got != want {
			t.Fatalf("thinkingBudget(%s) = %d, want %d", level, got, want)
		}
	}
}
