// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
)

func TestOpenAICompletionsCompatPayloadFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		compat sigma.OpenAICompletionsCompat
		req    sigma.Request
		opts   sigma.Options
		golden string
	}{
		{
			name: "max completion tokens and streaming usage",
			compat: sigma.OpenAICompletionsCompat{
				SupportsStreamingUsage: sigma.OpenAICompatSupported,
				MaxTokensField:         sigma.OpenAICompletionsMaxCompletionTokens,
			},
			req:    sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			opts:   sigma.Options{MaxTokens: compatIntPtr(12)},
			golden: "provider/openai/compat/max_completion_tokens.json",
		},
		{
			name: "developer role fallback and content-part cache control",
			compat: sigma.OpenAICompletionsCompat{
				SupportsDeveloperRole: sigma.OpenAICompatUnsupported,
				CacheControlFormat:    sigma.OpenAICompletionsCacheControlContentPart,
			},
			req: sigma.Request{
				SystemPrompt: "policy",
				Messages: []sigma.Message{{
					Role:    sigma.RoleDeveloper,
					Content: []sigma.ContentBlock{sigma.Text("style")},
				}},
			},
			opts:   sigma.Options{CacheRetention: sigma.CacheRetentionEphemeral},
			golden: "provider/openai/compat/developer_role_fallback_cache_content_part.json",
		},
		{
			name: "reasoning object with summary",
			compat: sigma.OpenAICompletionsCompat{
				ReasoningFormat: sigma.OpenAICompletionsReasoningObject,
			},
			req: sigma.Request{Messages: []sigma.Message{sigma.UserText("think")}},
			opts: sigma.Options{
				ReasoningLevel: sigma.ThinkingLevelHigh,
				OpenAIOptions:  &sigma.OpenAIOptions{ReasoningSummary: "auto"},
			},
			golden: "provider/openai/compat/reasoning_object.json",
		},
		{
			name: "strict tools supported",
			compat: sigma.OpenAICompletionsCompat{
				SupportsStrictTools: sigma.OpenAICompatSupported,
			},
			req: sigma.Request{
				Messages: []sigma.Message{sigma.UserText("weather")},
				Tools: []sigma.Tool{{
					Name:             "weather",
					InputSchema:      sigma.Schema{"type": "object"},
					ProviderMetadata: map[string]any{"strict": true},
				}},
			},
			golden: "provider/openai/compat/strict_tools_supported.json",
		},
		{
			name: "anthropic cache control",
			compat: sigma.OpenAICompletionsCompat{
				CacheControlFormat: sigma.OpenAICompletionsCacheControlAnthropic,
			},
			req: sigma.Request{
				SystemPrompt: "policy",
				Messages: []sigma.Message{
					sigma.UserText("first"),
					{
						Role:    sigma.RoleAssistant,
						Content: []sigma.ContentBlock{sigma.Text("answer")},
					},
					sigma.UserText("last"),
				},
				Tools: []sigma.Tool{{
					Name:        "lookup",
					Description: "Lookup records",
					InputSchema: sigma.Schema{"type": "object"},
				}},
			},
			opts:   sigma.Options{CacheRetention: sigma.CacheRetentionLong},
			golden: "provider/openai/compat/anthropic_cache_control.json",
		},
		{
			name: "tool stream supported",
			compat: sigma.OpenAICompletionsCompat{
				SupportsToolStream: sigma.OpenAICompatSupported,
			},
			req: sigma.Request{
				Messages: []sigma.Message{sigma.UserText("weather")},
				Tools: []sigma.Tool{{
					Name:        "weather",
					Description: "Get weather",
					InputSchema: sigma.Schema{"type": "object"},
				}},
			},
			golden: "provider/openai/compat/tool_stream_supported.json",
		},
		{
			name: "store field supported",
			compat: sigma.OpenAICompletionsCompat{
				SupportsStore: sigma.OpenAICompatSupported,
			},
			req: sigma.Request{Messages: []sigma.Message{sigma.UserText("remember")}},
			opts: sigma.Options{ProviderOptions: map[sigma.ProviderID]map[string]any{
				sigma.ProviderCustom: {"extra_body": map[string]any{"store": true}},
			}},
			golden: "provider/openai/compat/store_supported.json",
		},
		{
			name: "routing preferences",
			compat: sigma.OpenAICompletionsCompat{
				OpenRouterRouting: &sigma.OpenRouterRoutingPreference{
					Order:             []string{"anthropic", "openai"},
					AllowFallbacks:    boolPtr(false),
					RequireParameters: boolPtr(true),
					DataCollection:    "deny",
				},
				VercelAIGatewayRouting: &sigma.VercelAIGatewayRoutingPreference{
					Order:  []string{"bedrock", "anthropic"},
					Only:   []string{"bedrock", "anthropic"},
					Models: []string{"anthropic/claude-sonnet-4.6"},
				},
			},
			req:    sigma.Request{Messages: []sigma.Message{sigma.UserText("route")}},
			golden: "provider/openai/compat/routing_preferences.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model := compatTestModel(tt.compat)
			payload, err := chatCompletionsPayload(model, tt.req, tt.opts, openAICompletionsCompat(model, "https://custom.example/v1"))
			if err != nil {
				t.Fatalf("chatCompletionsPayload returned error: %v", err)
			}
			goldentest.AssertJSON(t, payload, tt.golden)
		})
	}
}

func TestOpenAICompletionsCompatRepairsToolMessages(t *testing.T) {
	t.Parallel()

	model := compatTestModel(sigma.OpenAICompletionsCompat{
		RequiresToolResultName:           sigma.OpenAICompatSupported,
		RequiresAssistantAfterToolResult: sigma.OpenAICompatSupported,
	})
	req := sigma.Request{Messages: []sigma.Message{
		{
			Role:    sigma.RoleAssistant,
			Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_1", "weather", map[string]any{"city": "Melbourne"})},
		},
		sigma.ToolResult("call_1", "sunny"),
		sigma.UserText("thanks"),
	}}

	payload, err := chatCompletionsPayload(model, req, sigma.Options{}, openAICompletionsCompat(model, "https://custom.example/v1"))
	if err != nil {
		t.Fatalf("chatCompletionsPayload returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/repairs_tool_messages.json")
}

func TestOpenAICompletionsCompatDefaultsAreConservativeForCustomEndpoints(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ID:               "local-model",
		Provider:         sigma.ProviderCustom,
		API:              sigma.APIOpenAICompletions,
		SupportsThinking: true,
	}
	req := sigma.Request{
		SystemPrompt: "policy",
		Messages: []sigma.Message{{
			Role:    sigma.RoleDeveloper,
			Content: []sigma.ContentBlock{sigma.Text("style")},
		}},
		Tools: []sigma.Tool{{
			Name:             "tool",
			InputSchema:      sigma.Schema{"type": "object"},
			ProviderMetadata: map[string]any{"strict": true},
		}},
	}
	opts := sigma.Options{
		MaxTokens:      compatIntPtr(9),
		ReasoningLevel: sigma.ThinkingLevelHigh,
		CacheRetention: sigma.CacheRetentionEphemeral,
		ProviderOptions: map[sigma.ProviderID]map[string]any{
			sigma.ProviderCustom: {"extra_body": map[string]any{"store": true}},
		},
	}

	payload, err := chatCompletionsPayload(model, req, opts, openAICompletionsCompat(model, "http://localhost:11434/v1"))
	if err != nil {
		t.Fatalf("chatCompletionsPayload returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/custom_endpoint_conservative_defaults.json")
	goldentest.AssertNoJSONPath(t, payload, "reasoning_effort")
	goldentest.AssertNoJSONPath(t, payload, "reasoning")
	goldentest.AssertNoJSONPath(t, payload, "store")
	goldentest.AssertNoJSONPath(t, payload, "stream_options")
}

func TestOpenAICompletionsCompatDetectsKnownRoutingEndpoints(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ID:       "router-model",
		Provider: sigma.ProviderCustom,
		API:      sigma.APIOpenAICompletions,
	}
	payload, err := chatCompletionsPayload(
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.Options{},
		openAICompletionsCompat(model, "https://openrouter.ai/api/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/detected_openrouter_endpoint.json")
}

func TestOpenAICompletionsCompatDetectsOpenRouterAnthropicCacheControl(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ID:       "anthropic/claude-sonnet-4.6",
		Provider: sigma.ProviderOpenRouter,
		API:      sigma.APIOpenAICompletions,
	}
	payload, err := chatCompletionsPayload(
		model,
		sigma.Request{
			SystemPrompt: "policy",
			Messages:     []sigma.Message{sigma.UserText("hi")},
			Tools: []sigma.Tool{{
				Name:        "lookup",
				Description: "Lookup records",
				InputSchema: sigma.Schema{"type": "object"},
			}},
		},
		sigma.Options{CacheRetention: sigma.CacheRetentionEphemeral},
		openAICompletionsCompat(model, "https://openrouter.ai/api/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/detected_openrouter_anthropic_cache.json")
}

func TestOpenAICompletionsCompatDetectsFireworksEndpoint(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ID:               "accounts/fireworks/routers/kimi-k2p6-turbo",
		Provider:         sigma.ProviderFireworks,
		API:              sigma.APIOpenAICompletions,
		SupportsTools:    true,
		SupportsThinking: true,
	}
	payload, err := chatCompletionsPayload(
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("hi")},
			Tools: []sigma.Tool{{
				Name:             "weather",
				InputSchema:      sigma.Schema{"type": "object"},
				ProviderMetadata: map[string]any{"strict": true},
			}},
		},
		sigma.Options{},
		openAICompletionsCompat(model, "https://api.fireworks.ai/inference/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/detected_fireworks_endpoint.json")
}

func TestOpenAICompletionsCompatDetectsOpenCodeEndpoint(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ID:       "kimi-k2.6",
		Provider: sigma.ProviderOpenCodeGo,
		API:      sigma.APIOpenAICompletions,
	}
	payload, err := chatCompletionsPayload(
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.Options{MaxTokens: compatIntPtr(12)},
		openAICompletionsCompat(model, "https://opencode.ai/zen/go/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/detected_opencode_go_endpoint.json")
	goldentest.AssertNoJSONPath(t, payload, "store")
}

func TestOpenAICompletionsCompatMapsFireworksReasoning(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ID:               "accounts/fireworks/routers/kimi-k2p6-turbo",
		Provider:         sigma.ProviderFireworks,
		API:              sigma.APIOpenAICompletions,
		SupportsThinking: true,
	}

	payload, err := chatCompletionsPayload(
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.Options{ReasoningLevel: sigma.ThinkingLevelMedium},
		openAICompletionsCompat(model, "https://api.fireworks.ai/inference/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/fireworks_reasoning_effort.json")

	budget := 4096
	payload, err = chatCompletionsPayload(
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.Options{
			ReasoningLevel:       sigma.ThinkingLevelHigh,
			ThinkingBudgetTokens: &budget,
		},
		openAICompletionsCompat(model, "https://api.fireworks.ai/inference/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload with budget returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/fireworks_thinking_budget.json")
}

func TestOpenAICompletionsCompatMapsOpenCodeReasoning(t *testing.T) {
	t.Parallel()

	deepSeek := sigma.Model{
		ID:               "deepseek-v4-flash",
		Provider:         sigma.ProviderOpenCodeGo,
		API:              sigma.APIOpenAICompletions,
		SupportsThinking: true,
		ThinkingLevelMap: map[sigma.ThinkingLevel]string{
			sigma.ThinkingLevelHigh:  "high",
			sigma.ThinkingLevelXHigh: "max",
		},
		OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
			ReasoningFormat: sigma.OpenAICompletionsReasoningDeepSeek,
		},
	}

	payload, err := chatCompletionsPayload(
		deepSeek,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.Options{},
		openAICompletionsCompat(deepSeek, "https://opencode.ai/zen/go/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload for disabled DeepSeek reasoning returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/opencode_go_deepseek_reasoning_disabled.json")

	payload, err = chatCompletionsPayload(
		deepSeek,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh},
		openAICompletionsCompat(deepSeek, "https://opencode.ai/zen/go/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload for enabled DeepSeek reasoning returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/opencode_go_deepseek_reasoning_enabled.json")

	kimi := sigma.Model{
		ID:               "kimi-k2.6",
		Provider:         sigma.ProviderOpenCodeGo,
		API:              sigma.APIOpenAICompletions,
		SupportsThinking: true,
		ThinkingLevelMap: map[sigma.ThinkingLevel]string{
			sigma.ThinkingLevelOff: "none",
		},
		OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
			ReasoningFormat: sigma.OpenAICompletionsReasoningStringThinking,
		},
	}

	payload, err = chatCompletionsPayload(
		kimi,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.Options{},
		openAICompletionsCompat(kimi, "https://opencode.ai/zen/go/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload for disabled Kimi reasoning returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/opencode_go_kimi_string_thinking_disabled.json")

	payload, err = chatCompletionsPayload(
		kimi,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh},
		openAICompletionsCompat(kimi, "https://opencode.ai/zen/go/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload for enabled Kimi reasoning returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/opencode_go_kimi_string_thinking_enabled.json")
}

func TestOpenAICompletionsCompatReplaysReasoningContent(t *testing.T) {
	t.Parallel()

	model := compatTestModel(sigma.OpenAICompletionsCompat{
		RequiresReasoningContentOnAssistantMessages: sigma.OpenAICompatSupported,
	})
	payload, err := chatCompletionsPayload(
		model,
		sigma.Request{Messages: []sigma.Message{{
			Role: sigma.RoleAssistant,
			Content: []sigma.ContentBlock{
				sigma.Thinking("inspect state", ""),
				sigma.Text("I will call the tool."),
				sigma.ToolCallBlock("call_1", "read", map[string]any{"path": "README.md"}),
			},
		}}},
		sigma.Options{},
		openAICompletionsCompat(model, "https://opencode.ai/zen/go/v1"),
	)
	if err != nil {
		t.Fatalf("chatCompletionsPayload returned error: %v", err)
	}
	goldentest.AssertJSON(t, payload, "provider/openai/compat/replays_reasoning_content.json")
}

func compatTestModel(compat sigma.OpenAICompletionsCompat) sigma.Model {
	return sigma.Model{
		ID:                      "compat-model",
		Provider:                sigma.ProviderCustom,
		API:                     sigma.APIOpenAICompletions,
		SupportsThinking:        true,
		SupportsTools:           true,
		OpenAICompletionsCompat: &compat,
	}
}

func compatIntPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
