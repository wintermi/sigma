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

// Each earlier turn of this model is preceded by the effort it ran with and
// the requested effort follows the conversation, so the input grows as a
// stable prefix and a thinking-level change keeps the prompt cache.
func TestConfigurationUpdatesFollowRecordedEfforts(t *testing.T) {
	t.Parallel()
	model := sigma.Model{
		ID: "gpt-6-sol", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses, SupportsThinking: true,
		ThinkingLevelMap:      map[sigma.ThinkingLevel]string{sigma.ThinkingLevelOff: "none"},
		OpenAIResponsesCompat: &sigma.OpenAIResponsesCompat{SupportsMidConversationEffort: true},
	}
	own := sigma.Message{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, ProviderThinkingLevel: "low", Content: []sigma.ContentBlock{sigma.Text("first")}}
	other := sigma.Message{Role: sigma.RoleAssistant, Provider: sigma.ProviderAnthropic, API: sigma.APIAnthropicMessages, Model: "claude", ProviderThinkingLevel: "max", Content: []sigma.ContentBlock{sigma.Text("second")}}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("one"), own, sigma.UserText("two"), other, sigma.UserText("three")}}
	payload, err := responsesPayload(model, req, sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, item := range payload["input"].([]map[string]any) {
		switch {
		case item["type"] == "configuration_update":
			kinds = append(kinds, "update:"+item["reasoning"].(map[string]any)["effort"].(string))
		case item["role"] != nil:
			kinds = append(kinds, item["role"].(string))
		default:
			kinds = append(kinds, item["type"].(string))
		}
	}
	want := []string{"user", "update:low", "assistant", "user", "assistant", "user", "update:high"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("input = %v, want %v", kinds, want)
	}
	if effort := payload["reasoning"].(map[string]any)["effort"]; effort != "medium" {
		t.Fatalf("reasoning.effort = %v, want the fixed medium", effort)
	}
	options, err := openAIResponsesStreamOptions(model, req, sigma.Options{ReasoningLevel: sigma.ThinkingLevelOff})
	if err != nil || options.providerThinkingLevel != "none" {
		t.Fatalf("recorded effort = %q, %v; want none", options.providerThinkingLevel, err)
	}
}
