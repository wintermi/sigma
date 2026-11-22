// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
)

// Z.ai switches thinking with thinking.type; reasoning_effort is sent only to
// models whose metadata declares it.
func TestZAIReasoningEffortRequiresDeclaredSupport(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		id         sigma.ModelID
		wantEffort bool
	}{
		{id: "glm-4.7"},
		{id: "glm-5-turbo"},
		{id: "glm-5.3", wantEffort: true},
	} {
		model, ok := sigma.GetModel(sigma.ProviderZAI, tt.id)
		if !ok {
			t.Fatalf("missing zai/%s", tt.id)
		}
		baseURL, _ := model.ProviderMetadata["baseURL"].(string)
		payload, err := chatCompletionsPayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh}, openAICompletionsCompat(model, baseURL))
		if err != nil {
			t.Fatal(err)
		}
		if thinking, _ := payload["thinking"].(map[string]any); thinking["type"] != "enabled" {
			t.Fatalf("%s thinking = %#v, want enabled", tt.id, payload["thinking"])
		}
		if _, sent := payload["reasoning_effort"]; sent != tt.wantEffort {
			t.Errorf("%s reasoning_effort sent = %v, want %v", tt.id, sent, tt.wantEffort)
		}
	}
}
