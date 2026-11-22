// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

// Turning thinking off on an OpenRouter reasoning model without an off
// mapping sends "none"; otherwise OpenRouter keeps the model reasoning.
func TestOpenRouterThinkingOffWithoutMappingSendsNone(t *testing.T) {
	t.Parallel()
	model := sigma.Model{ID: "vendor/reasoner", Provider: sigma.ProviderOpenRouter, API: sigma.APIOpenAICompletions, SupportsThinking: true}
	compat := openAICompletionsCompat(model, "https://openrouter.ai/api/v1")
	for _, tt := range []struct {
		level sigma.ThinkingLevel
		want  any
	}{
		{level: sigma.ThinkingLevelOff, want: map[string]any{"effort": "none"}},
		{level: sigma.ThinkingLevelHigh, want: map[string]any{"effort": "high"}},
		{level: "", want: nil},
	} {
		payload, err := chatCompletionsPayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, sigma.Options{ReasoningLevel: tt.level}, compat)
		if err != nil {
			t.Fatal(err)
		}
		if got := payload["reasoning"]; !reflect.DeepEqual(got, tt.want) {
			t.Errorf("level %q: reasoning = %#v, want %#v", tt.level, got, tt.want)
		}
	}
}
