// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"fmt"
	"testing"

	"github.com/wintermi/sigma"
)

// OpenRouter routing is an OpenRouter request field; a client-wide routing
// default must not leak into other providers' Chat Completions requests.
func TestOpenRouterRoutingAppliesOnlyToOpenRouterRoutes(t *testing.T) {
	t.Parallel()

	opts := sigma.Options{ProviderOptions: map[sigma.ProviderID]map[string]any{
		sigma.ProviderOpenRouter: {"routing": map[string]any{"order": []string{"anthropic"}}},
	}}
	tests := []struct {
		name    string
		model   sigma.Model
		routing bool
	}{
		{name: "groq", model: sigma.Model{ID: "openai/gpt-oss-120b", Provider: sigma.ProviderGroq, API: sigma.APIOpenAICompletions}},
		{name: "direct openai", model: sigma.Model{ID: "gpt-test", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAICompletions}},
		{
			name: "openrouter",
			model: sigma.Model{
				ID: "anthropic/claude-test", Provider: sigma.ProviderOpenRouter, API: sigma.APIOpenAICompletions,
				ProviderMetadata: map[string]any{"baseURL": "https://openrouter.ai/api/v1"},
			},
			routing: true,
		},
		{
			name: "custom provider on openrouter",
			model: sigma.Model{
				ID: "anthropic/claude-test", Provider: "my-router", API: sigma.APIOpenAICompletions,
				ProviderMetadata: map[string]any{"baseURL": "https://openrouter.ai/api/v1"},
			},
			routing: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compat := openAICompletionsCompat(tt.model, openAICompatibleBaseURL(tt.model, DefaultBaseURL))
			payload, err := chatCompletionsPayload(tt.model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, opts, compat)
			if err != nil {
				t.Fatal(err)
			}
			routing, ok := payload["provider"]
			if ok != tt.routing {
				t.Fatalf("provider routing = %v (present %v), want present %v", routing, ok, tt.routing)
			}
			if ok && fmt.Sprint(routing) != "map[order:[anthropic]]" {
				t.Fatalf("provider routing = %v, want the OpenRouter routing option", routing)
			}
		})
	}
}
