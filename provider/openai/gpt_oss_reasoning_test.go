// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

// gpt-oss accepts reasoning_effort on every OpenAI-compatible route, so a
// requested level must reach the payload instead of being silently dropped.
func TestGPTOSSCompletionsRoutesSendReasoningEffort(t *testing.T) {
	t.Parallel()

	found := 0
	for _, model := range sigma.Models() {
		if model.API != sigma.APIOpenAICompletions || !strings.Contains(string(model.ID), "gpt-oss") || !model.SupportsThinking {
			continue
		}
		switch model.Provider {
		case sigma.ProviderGroq, sigma.ProviderCerebras, sigma.ProviderTogether, "cloudflare-workers-ai":
		default:
			continue
		}
		found++
		compat := openAICompletionsCompat(model, openAICompatibleBaseURL(model, DefaultBaseURL))
		payload, err := chatCompletionsPayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh}, compat)
		if err != nil {
			t.Fatalf("%s/%s: %v", model.Provider, model.ID, err)
		}
		if got := payload["reasoning_effort"]; got != "high" {
			t.Errorf("%s/%s reasoning_effort = %v, want high (format %q)", model.Provider, model.ID, got, compat.reasoningFormat)
		}
	}
	if found != 7 {
		t.Fatalf("checked %d gpt-oss rows, want 7", found)
	}
}
