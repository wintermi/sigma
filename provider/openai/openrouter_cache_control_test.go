// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestOpenRouterNonAnthropicModelsOmitMessageCacheControl(t *testing.T) {
	t.Parallel()

	for _, retention := range []sigma.CacheRetention{sigma.CacheRetentionShort, sigma.CacheRetentionLong} {
		t.Run(string(retention), func(t *testing.T) {
			t.Parallel()

			model := sigma.Model{Provider: sigma.ProviderOpenRouter, API: sigma.APIOpenAICompletions, ID: "openai/gpt-5"}
			compat := detectedCompletionsCompat(model, "https://openrouter.ai/api/v1")
			req := sigma.Request{SystemPrompt: "be brief", Messages: []sigma.Message{sigma.UserText("hi")}}
			payload, err := chatCompletionsPayload(model, req, sigma.Options{CacheRetention: retention}, compat)
			if err != nil {
				t.Fatalf("chatCompletionsPayload returned error: %v", err)
			}
			messages, _ := payload["messages"].([]map[string]any)
			for _, message := range messages {
				if _, ok := message["cache_control"]; ok {
					t.Fatalf("%s message has cache_control: %#v", message["role"], message)
				}
				if parts, ok := message["content"].([]map[string]any); ok {
					for _, part := range parts {
						if _, ok := part["cache_control"]; ok {
							t.Fatalf("%s content part has cache_control: %#v", message["role"], part)
						}
					}
				}
			}
		})
	}
}
