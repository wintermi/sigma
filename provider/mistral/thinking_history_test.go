// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestConversationPayloadAcceptsThinkingHistoryForNonThinkingModel(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ID: "codestral-latest", Provider: sigma.ProviderMistral, API: sigma.APIMistralConversations}
	payload, err := conversationPayload(model, sigma.Request{Messages: []sigma.Message{
		sigma.UserText("start"),
		{
			Role: sigma.RoleAssistant, Provider: sigma.ProviderAnthropic, API: sigma.APIAnthropicMessages, Model: "claude-test",
			Content: []sigma.ContentBlock{sigma.Thinking("foreign plan", "sig"), sigma.Text("first answer")},
		},
		sigma.UserText("continue"),
	}}, sigma.Options{})
	if err != nil {
		t.Fatalf("conversationPayload returned error: %v", err)
	}
	inputs := fmt.Sprint(payload["inputs"])
	if strings.Contains(inputs, "foreign plan") {
		t.Fatalf("inputs = %s, want foreign thinking dropped", inputs)
	}
	if !strings.Contains(inputs, "first answer") {
		t.Fatalf("inputs = %s, want assistant text kept", inputs)
	}
}
