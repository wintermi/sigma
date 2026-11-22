// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"encoding/json"
	"testing"

	"github.com/wintermi/sigma"
)

// GitHub Copilot reports reasoning tokens at the top level of usage rather
// than in completion_tokens_details.
func TestChatUsageReadsTopLevelReasoningTokens(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		raw  string
		want int
	}{
		{name: "top level", raw: `{"prompt_tokens":10,"completion_tokens":20,"reasoning_tokens":12}`, want: 12},
		{name: "nested wins", raw: `{"prompt_tokens":10,"completion_tokens":20,"reasoning_tokens":12,"completion_tokens_details":{"reasoning_tokens":7}}`, want: 7},
		{name: "absent", raw: `{"prompt_tokens":10,"completion_tokens":20}`, want: 0},
	} {
		var usage openAIUsage
		if err := json.Unmarshal([]byte(tt.raw), &usage); err != nil {
			t.Fatal(err)
		}
		if got := usage.sigmaUsage().ThinkingTokens; got != tt.want {
			t.Errorf("%s: thinking tokens = %d, want %d", tt.name, got, tt.want)
		}
	}
}

// GitHub Copilot accepts reasoning_effort for the Gemini Flash and Kimi K3
// models whose catalog levels list it.
func TestGitHubCopilotSendsReasoningEffort(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		id    sigma.ModelID
		level sigma.ThinkingLevel
	}{
		{id: "gemini-3.8-flash", level: sigma.ThinkingLevelMedium},
		{id: "kimi-k3", level: sigma.ThinkingLevel("max")},
	} {
		model, ok := sigma.GetModel(sigma.ProviderGitHubCopilot, tt.id)
		if !ok {
			t.Fatalf("missing github-copilot/%s", tt.id)
		}
		baseURL, _ := model.ProviderMetadata["baseURL"].(string)
		compat := openAICompletionsCompat(model, baseURL)
		payload, err := chatCompletionsPayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, sigma.Options{ReasoningLevel: tt.level}, compat)
		if err != nil {
			t.Fatal(err)
		}
		if got := payload["reasoning_effort"]; got != string(tt.level) {
			t.Errorf("%s reasoning_effort = %#v, want %q", tt.id, got, tt.level)
		}
	}
}
