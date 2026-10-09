// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
)

// OpenAI reasoning models reject max_tokens on Chat Completions.
func TestChatCompletionsUsesMaxCompletionTokensForOpenAI(t *testing.T) {
	t.Parallel()

	for name, model := range map[string]sigma.Model{
		"provider": {ID: "o4-mini", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAICompletions, SupportsThinking: true},
		"host":     {ID: "o4-mini", Provider: "custom-openai", API: sigma.APIOpenAICompletions, SupportsThinking: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			maxTokens := 100
			payload, err := chatCompletionsPayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, sigma.Options{MaxTokens: &maxTokens, ReasoningLevel: sigma.ThinkingLevelHigh}, openAICompletionsCompat(model, DefaultBaseURL))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := payload["max_tokens"]; ok {
				t.Fatalf("payload sent max_tokens: %v", payload)
			}
			if got := payload["max_completion_tokens"]; got != 100 {
				t.Fatalf("max_completion_tokens = %v, want 100", got)
			}
		})
	}
}
