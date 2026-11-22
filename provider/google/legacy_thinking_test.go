// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"testing"

	"github.com/wintermi/sigma"
)

// Models before Gemini 3 take thinkingBudget and reject thinkingLevel, so a
// custom one without a level mapping gets a budget for the requested level.
func TestThinkingConfigForModelsWithoutLevelMapping(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		id         sigma.ModelID
		wantBudget any
		wantLevel  any
	}{
		{id: "gemini-2.5-flash", wantBudget: 24576},
		{id: "gemini-2.5-pro-custom", wantBudget: 32768},
		{id: "gemini-2.0-experimental", wantBudget: -1},
		{id: "vendor-gemini-2.5-flash-lite", wantBudget: 24576},
		{id: "gemini-3-flash-custom", wantLevel: "HIGH"},
	} {
		model := sigma.Model{ID: tt.id, Provider: sigma.ProviderGoogle, API: sigma.APIGoogleGenerativeAI, SupportsThinking: true}
		thinking, err := googleThinkingConfig(model, sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh})
		if err != nil {
			t.Fatalf("%s: %v", tt.id, err)
		}
		if thinking["thinkingBudget"] != tt.wantBudget || thinking["thinkingLevel"] != tt.wantLevel {
			t.Errorf("%s: thinking = %#v, want budget %v level %v", tt.id, thinking, tt.wantBudget, tt.wantLevel)
		}
	}
}

// Some hosts prefix Gemini IDs; the version still decides tool call IDs.
func TestGeminiVersionDetectedInPrefixedIDs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		id   sigma.ModelID
		want int
		ok   bool
	}{
		{id: "gemini-3.8-flash", want: 3, ok: true},
		{id: "vendor-gemini-4-argon", want: 4, ok: true},
		{id: "gemini-live-2.5-flash", want: 2, ok: true},
		{id: "gemma-4-27b"},
	} {
		got, ok := geminiMajorVersion(tt.id)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: version = %d, %v; want %d, %v", tt.id, got, ok, tt.want, tt.ok)
		}
	}
}
