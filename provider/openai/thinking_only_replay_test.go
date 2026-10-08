// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestChatSkipsThinkingOnlyAssistantTurns(t *testing.T) {
	t.Parallel()

	model := sigma.Model{Provider: sigma.ProviderDeepSeek, API: sigma.APIOpenAICompletions, ID: "deepseek-v4"}
	assistant := sigma.Message{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, Content: []sigma.ContentBlock{sigma.Thinking("considering", "")}}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("first"), assistant, sigma.UserText("second")}}
	compat := completionsCompat{requiresReasoningContentOnAssistantMessages: true}

	payload, err := chatCompletionsPayload(model, req, sigma.Options{}, compat)
	if err != nil {
		t.Fatalf("chatCompletionsPayload returned error: %v", err)
	}
	messages, _ := payload["messages"].([]map[string]any)
	for _, message := range messages {
		if message["role"] == "assistant" {
			t.Fatalf("thinking-only assistant turn replayed without content: %#v", message)
		}
	}
}
