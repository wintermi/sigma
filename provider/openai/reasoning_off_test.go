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

func TestResponsesExplicitOffSendsOffEffort(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		model    sigma.Model
		level    sigma.ThinkingLevel
		want     any
		wantSent bool
	}{
		{
			name:     "off defaults to none",
			model:    sigma.Model{ID: "gpt-test", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses, SupportsThinking: true},
			level:    sigma.ThinkingLevelOff,
			want:     map[string]any{"effort": "none"},
			wantSent: true,
		},
		{
			name: "off uses mapped value",
			model: sigma.Model{
				ID: "gpt-test", Provider: sigma.ProviderAzureOpenAIResponses, API: sigma.APIAzureOpenAIResponses,
				SupportsThinking: true, ThinkingLevelMap: map[sigma.ThinkingLevel]string{sigma.ThinkingLevelOff: "minimal"},
			},
			level:    sigma.ThinkingLevelOff,
			want:     map[string]any{"effort": "minimal"},
			wantSent: true,
		},
		{
			name:  "unspecified level leaves the provider default",
			model: sigma.Model{ID: "gpt-test", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses, SupportsThinking: true},
		},
		{
			name:  "non-reasoning model",
			model: sigma.Model{ID: "gpt-test", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses},
			level: sigma.ThinkingLevelOff,
		},
		{
			name:  "copilot does not accept an off effort",
			model: sigma.Model{ID: "gpt-test", Provider: sigma.ProviderGitHubCopilot, API: sigma.APIOpenAIResponses, SupportsThinking: true},
			level: sigma.ThinkingLevelOff,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{}
			addResponsesReasoning(payload, tt.model, sigma.Options{ReasoningLevel: tt.level})
			got, sent := payload["reasoning"]
			if sent != tt.wantSent || (sent && !reflect.DeepEqual(got, tt.want)) {
				t.Fatalf("reasoning = %#v (sent %v), want %#v (sent %v)", got, sent, tt.want, tt.wantSent)
			}
			if _, ok := payload["include"]; ok {
				t.Fatalf("include = %#v, want omitted for off", payload["include"])
			}
		})
	}
}

func TestCompletionsExplicitOffSendsMappedReasoningEffort(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		off  map[sigma.ThinkingLevel]string
		want any
	}{
		{name: "mapped off value", off: map[sigma.ThinkingLevel]string{sigma.ThinkingLevelOff: "none"}, want: "none"},
		{name: "no mapped off value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := sigma.Model{
				ID: "effort-model", Provider: sigma.ProviderCustom, API: sigma.APIOpenAICompletions,
				SupportsThinking: true, ThinkingLevelMap: tt.off,
				OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
					ReasoningFormat:         sigma.OpenAICompletionsReasoningEffort,
					SupportsReasoningEffort: sigma.OpenAICompatSupported,
				},
			}
			payload := map[string]any{}
			addReasoning(payload, model, sigma.Options{ReasoningLevel: sigma.ThinkingLevelOff}, openAICompletionsCompat(model, "https://example.test/v1"))
			if got := payload["reasoning_effort"]; got != tt.want {
				t.Fatalf("reasoning_effort = %#v, want %#v", got, tt.want)
			}
		})
	}
}
