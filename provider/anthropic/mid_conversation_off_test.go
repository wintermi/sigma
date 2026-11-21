// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestMidConversationEffortOffDisablesThinking(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ID: "claude-opus-5", Provider: sigma.ProviderAnthropic, API: sigma.APIAnthropicMessages,
		SupportsThinking:        true,
		AnthropicMessagesCompat: &sigma.AnthropicMessagesCompat{SupportsMidConversationEffort: sigma.AnthropicCompatSupported},
	}
	compat := anthropicMessagesCompat(model, "https://api.anthropic.com", nil)
	opts := sigma.Options{ReasoningLevel: sigma.ThinkingLevelOff}
	history := sigma.Message{
		Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID,
		ProviderThinkingLevel: "high", Content: []sigma.ContentBlock{sigma.Text("earlier")},
	}
	payload, err := messagesPayload(model, sigma.Request{Messages: []sigma.Message{
		sigma.UserText("first"), history, sigma.UserText("again"),
	}}, opts, compat)
	if err != nil {
		t.Fatalf("messagesPayload returned error: %v", err)
	}
	if got := payload["thinking"]; got == nil || got.(map[string]any)["type"] != "disabled" {
		t.Fatalf("thinking = %#v, want disabled", got)
	}
	if got, ok := payload["output_config"]; ok {
		t.Fatalf("output_config = %#v, want omitted", got)
	}
	for _, message := range payload["messages"].([]map[string]any) {
		if _, ok := message["output_config"]; ok {
			t.Fatalf("messages = %#v, want no effort markers", payload["messages"])
		}
	}
	if beta := anthropicBeta(model, opts, compat, false); strings.Contains(beta, midConversationEffortBeta) {
		t.Fatalf("anthropic-beta = %q, want no mid-conversation effort beta", beta)
	}
}
