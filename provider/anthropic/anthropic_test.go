// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
	"github.com/wintermi/sigma/provider/anthropic"
)

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    string
}

func TestRegisterReportsMessagesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("anthropic-compatible")
	if err := anthropic.Register(registry, providerID); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(anthropicTestModel(providerID)); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIAnthropicMessages; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestCompleteSendsGoldenPayloadWithCacheThinkingImagesAndTools(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-payload-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(
		t,
		providerID,
		model,
		server.URL,
		anthropic.WithHeader("X-Provider", "provider"),
		anthropic.WithMessagesCompat(anthropic.MessagesCompat{
			LongCacheRetention:     true,
			SessionAffinityHeaders: true,
			CacheControlOnTools:    true,
		}),
	)

	final, err := client.Complete(
		context.Background(),
		model,
		richRequest(),
		sigma.WithTemperature(0.2),
		sigma.WithMaxTokens(123),
		sigma.WithCacheRetention(sigma.CacheRetentionLong),
		sigma.WithSessionID("session-123"),
		sigma.WithHeader("X-Custom", "custom"),
		sigma.WithMetadataValue("trace", "abc"),
		sigma.WithAnthropicOptions(sigma.AnthropicOptions{ThinkingBudgetTokens: intPtr(2048)}),
		sigma.WithProviderOptions(providerID, map[string]any{
			"session_id_header": "X-Session-ID",
			"anthropic_beta":    "prompt-caching-2024-07-31",
			"thinking_display":  "hidden",
			"tool_choice":       map[string]any{"type": "auto"},
			"extra_body":        map[string]any{"top_k": 1},
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.ProviderMetadata["id"], "msg_complete"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/messages"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "X-Api-Key", "resolved-key")
	assertHeader(t, request.Headers, "Anthropic-Version", "2023-06-01")
	assertHeader(t, request.Headers, "Anthropic-Beta", "prompt-caching-2024-07-31,fine-grained-tool-streaming-2025-05-14")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	assertHeader(t, request.Headers, "X-Session-ID", "session-123")
	goldentest.AssertJSON(t, request.Body, "provider/anthropic/messages/rich_payload.json")
}

func TestCompleteUsesModelBaseURLAndHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-model-metadata-test")
	model := anthropicTestModel(providerID)
	model.ProviderMetadata = map[string]any{
		"baseURL": server.URL + "/model-base",
		"headers": map[string]string{
			"Authorization": "Bearer metadata-secret",
			"X-Model":       "model",
		},
	}
	client := anthropicTestClient(t, providerID, model, "https://provider-base.invalid")

	if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/model-base/messages"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "X-Api-Key", "resolved-key")
	assertHeader(t, request.Headers, "X-Model", "model")
}

func TestCompleteSendsProviderDefinedToolsPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-provider-tools-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(
		t,
		providerID,
		model,
		server.URL,
		anthropic.WithMessagesCompat(anthropic.MessagesCompat{
			LongCacheRetention:  true,
			CacheControlOnTools: true,
		}),
	)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("Search current docs.")},
			Tools: []sigma.Tool{
				{
					Name:        "lookup",
					Description: "Lookup local records",
					InputSchema: sigma.Schema{
						"type":       "object",
						"properties": map[string]any{"query": map[string]any{"type": "string"}},
						"required":   []any{"query"},
					},
				},
				anthropic.Tools.WebSearch(anthropic.WithMaxUses(2)),
			},
		},
		sigma.WithCacheRetention(sigma.CacheRetentionLong),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/anthropic/messages/provider_defined_tools_payload.json")
}

func TestMessagesPayloadDropsUnansweredToolCallsBeforeUserTurn(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-drop-unanswered-tool-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_abandoned", "lookup", map[string]any{"query": "weather"}),
				},
			},
			sigma.UserText("Skip the lookup."),
		}},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	messages := payload["messages"].([]any)
	if got, want := len(messages), 1; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	message := messages[0].(map[string]any)
	if got, want := message["role"], "user"; got != want {
		t.Fatalf("message role = %v, want %q", got, want)
	}
}

func TestMessagesPayloadNormalizesProviderText(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-normalized-payload-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)
	invalid := invalidProviderText()

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			SystemPrompt: "system" + invalid,
			Messages: []sigma.Message{
				{Role: sigma.RoleDeveloper, Content: []sigma.ContentBlock{sigma.Text("developer" + invalid)}},
				sigma.UserText("user" + invalid),
				{
					Role: sigma.RoleAssistant,
					Content: []sigma.ContentBlock{
						sigma.Text("assistant" + invalid),
						sigma.Thinking("thinking"+invalid, "sig"),
						sigma.ToolCallBlock("call_invalid", "lookup", map[string]any{"query": "weather"}),
					},
				},
				sigma.ToolResult("call_invalid", "tool"+invalid),
			},
			Tools: []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}},
		},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	if got, want := payload["system"], "systemclean"; got != want {
		t.Fatalf("system = %v, want %q", got, want)
	}
	messages := payload["messages"].([]any)
	assertAnthropicTextBlock(t, messages[0], "developerclean")
	assertAnthropicTextBlock(t, messages[1], "userclean")
	assistant := messages[2].(map[string]any)
	content := assistant["content"].([]any)
	text := content[0].(map[string]any)
	if got, want := text["text"], "assistantclean"; got != want {
		t.Fatalf("assistant text = %v, want %q", got, want)
	}
	thinking := content[1].(map[string]any)
	if got, want := thinking["thinking"], "thinkingclean"; got != want {
		t.Fatalf("thinking = %v, want %q", got, want)
	}
	toolMessage := messages[3].(map[string]any)
	toolBlocks := toolMessage["content"].([]any)
	toolResult := toolBlocks[0].(map[string]any)
	if got, want := toolResult["content"], "toolclean"; got != want {
		t.Fatalf("tool content = %v, want %q", got, want)
	}
}

func TestCompleteSendsDisabledThinkingAndEagerToolStreamingPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	model := anthropicTestModel(sigma.ProviderAnthropic)
	client := anthropicTestClient(t, sigma.ProviderAnthropic, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("Use the tool.")},
			Tools: []sigma.Tool{{
				Name:        "lookup",
				Description: "Lookup",
				InputSchema: sigma.Schema{"type": "object"},
			}},
		},
		sigma.WithTemperature(0.4),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got := request.Headers.Get("Anthropic-Beta"); got != "" {
		t.Fatalf("Anthropic-Beta header = %q, want empty", got)
	}
	goldentest.AssertJSON(t, request.Body, "provider/anthropic/messages/disabled_thinking_eager_tools_payload.json")
}

func TestCompleteOmitsDisabledThinkingWhenUnsupported(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	model := anthropicTestModel(sigma.ProviderAnthropic)
	model.ID = "claude-fable-test"
	model.ThinkingLevelMap = map[sigma.ThinkingLevel]string{sigma.ThinkingLevelXHigh: "xhigh"}
	model.AnthropicMessagesCompat = &sigma.AnthropicMessagesCompat{
		SupportsDisabledThinking: sigma.AnthropicCompatUnsupported,
		ThinkingFormat:           sigma.AnthropicThinkingAdaptive,
	}
	client := anthropicTestClient(t, sigma.ProviderAnthropic, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("No thinking, please.")}},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	var body map[string]any
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		t.Fatalf("request body unmarshal: %v", err)
	}
	if thinking, ok := body["thinking"]; ok {
		t.Fatalf("thinking = %#v, want field omitted", thinking)
	}
}

func TestOAuthCredentialUsesClaudeCodeIdentity(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, claudeCodeToolUseEvent)
	}))
	t.Cleanup(server.Close)

	model := anthropicTestModel(sigma.ProviderAnthropic)
	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(sigma.ProviderAnthropic, anthropic.NewProvider(anthropic.WithBaseURL(server.URL))); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "sk-ant-oat01-token"}, nil
	})
	client := sigma.NewClient(sigma.WithRegistry(registry), sigma.WithAuthResolver(resolver))

	message, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			SystemPrompt: "Caller system prompt.",
			Messages: []sigma.Message{
				sigma.UserText("Read the file."),
				{
					Role: sigma.RoleAssistant,
					Content: []sigma.ContentBlock{
						sigma.ToolCallBlock("toolu_prior", "read", map[string]any{"path": "old.txt"}),
					},
				},
				{Role: sigma.RoleTool, ToolCallID: "toolu_prior", ToolName: "read", Content: []sigma.ContentBlock{sigma.Text("prior result")}},
			},
			Tools: []sigma.Tool{{
				Name:        "read",
				Description: "Read a file",
				InputSchema: sigma.Schema{"type": "object"},
			}},
		},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Headers.Get("Authorization"), "Bearer sk-ant-oat01-token"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	if got := request.Headers.Get("X-Api-Key"); got != "" {
		t.Fatalf("X-Api-Key = %q, want empty", got)
	}
	beta := request.Headers.Get("Anthropic-Beta")
	if !strings.Contains(beta, "claude-code-20250219") || !strings.Contains(beta, "oauth-2025-04-20") {
		t.Fatalf("Anthropic-Beta = %q, want Claude Code identity betas", beta)
	}
	if got, want := request.Headers.Get("User-Agent"), "claude-cli/2.1.75"; got != want {
		t.Fatalf("User-Agent = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("X-App"), "cli"; got != want {
		t.Fatalf("X-App = %q, want %q", got, want)
	}

	payload := decodePayload(t, request.Body)
	system, ok := payload["system"].([]any)
	if !ok || len(system) != 2 {
		t.Fatalf("system = %#v, want identity block plus caller prompt", payload["system"])
	}
	identity := system[0].(map[string]any)
	if got, want := identity["text"], "You are Claude Code, Anthropic's official CLI for Claude."; got != want {
		t.Fatalf("identity block = %#v, want Claude Code identity text", identity)
	}
	caller := system[1].(map[string]any)
	if got, want := caller["text"], "Caller system prompt."; got != want {
		t.Fatalf("caller system block = %#v, want caller prompt", caller)
	}

	tools := payload["tools"].([]any)
	tool := tools[0].(map[string]any)
	if got, want := tool["name"], "Read"; got != want {
		t.Fatalf("tool name = %q, want canonical Claude Code casing %q", got, want)
	}

	messages := payload["messages"].([]any)
	assistant := messages[1].(map[string]any)
	replayedTool := assistant["content"].([]any)[0].(map[string]any)
	if got, want := replayedTool["name"], "Read"; got != want {
		t.Fatalf("replayed tool name = %q, want canonical Claude Code casing %q", got, want)
	}

	var streamedTool *sigma.ContentBlock
	for index := range message.Content {
		if message.Content[index].Type == sigma.ContentBlockToolCall {
			streamedTool = &message.Content[index]
			break
		}
	}
	if streamedTool == nil {
		t.Fatalf("message content = %#v, want streamed tool call", message.Content)
	}
	if got, want := streamedTool.ToolName, "read"; got != want {
		t.Fatalf("streamed tool name = %q, want caller casing %q", got, want)
	}
}

func TestAPIKeyCredentialDoesNotUseClaudeCodeIdentity(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	model := anthropicTestModel(sigma.ProviderAnthropic)
	client := anthropicTestClient(t, sigma.ProviderAnthropic, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			SystemPrompt: "Caller system prompt.",
			Messages:     []sigma.Message{sigma.UserText("hello")},
		},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got := request.Headers.Get("Anthropic-Beta"); strings.Contains(got, "claude-code") || strings.Contains(got, "oauth-2025-04-20") {
		t.Fatalf("Anthropic-Beta = %q, want no Claude Code identity betas", got)
	}
	if got, want := request.Headers.Get("User-Agent"), "sigma/anthropic-messages"; got != want {
		t.Fatalf("User-Agent = %q, want %q", got, want)
	}
	payload := decodePayload(t, request.Body)
	if system, ok := payload["system"].(string); !ok || system != "Caller system prompt." {
		t.Fatalf("system = %#v, want caller prompt only", payload["system"])
	}
}

const claudeCodeToolUseEvent = `data: {"type":"message_start","message":{"id":"msg_cc","type":"message","role":"assistant","model":"claude-test","content":[]}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_cc","name":"Read","input":{}}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"file.txt\"}"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":1,"output_tokens":1}}

data: {"type":"message_stop"}
`

func TestCompatibilitySuppressesUnsupportedLongCacheAndToolCache(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-conservative-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			SystemPrompt: "Cache me when supported.",
			Messages:     []sigma.Message{sigma.UserText("hi")},
			Tools: []sigma.Tool{{
				Name:        "lookup",
				Description: "Lookup",
				InputSchema: sigma.Schema{"type": "object"},
			}},
		},
		sigma.WithCacheRetention(sigma.CacheRetentionLong),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/anthropic/messages/conservative_cache_payload.json")
}

func TestAdaptiveThinkingPayloadUsesOutputConfigEffort(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-adaptive-test")
	model := anthropicTestModel(providerID)
	model.ThinkingLevelMap = map[sigma.ThinkingLevel]string{sigma.ThinkingLevelXHigh: "xhigh"}
	model.AnthropicMessagesCompat = &sigma.AnthropicMessagesCompat{
		ThinkingFormat: sigma.AnthropicThinkingAdaptive,
	}
	client := anthropicTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithReasoningLevel(sigma.ThinkingLevelXHigh),
		sigma.WithTemperature(0.9),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/anthropic/messages/adaptive_output_config_payload.json")
	goldentest.AssertNoJSONPath(t, request.Body, "temperature")
}

func TestTypedAnthropicOptionsOverrideRawProviderOptions(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-typed-options-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)
	budget := 2048

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("hi")},
			Tools: []sigma.Tool{{
				Name:        "lookup",
				Description: "Lookup",
				InputSchema: sigma.Schema{"type": "object"},
			}},
		},
		sigma.WithAnthropicOptions(sigma.AnthropicOptions{
			ThinkingBudgetTokens: &budget,
			ToolChoice:           &sigma.AnthropicToolChoice{Type: sigma.AnthropicToolChoiceTool, Name: "lookup"},
			ThinkingDisplay:      sigma.AnthropicThinkingDisplayOmitted,
		}),
		sigma.WithProviderOptions(providerID, map[string]any{
			"thinking_display": "summarized",
			"tool_choice":      map[string]any{"type": "auto"},
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	thinking := payload["thinking"].(map[string]any)
	if got, want := thinking["display"], string(sigma.AnthropicThinkingDisplayOmitted); got != want {
		t.Fatalf("thinking display = %v, want %v", got, want)
	}
	toolChoice := payload["tool_choice"].(map[string]any)
	if got, want := toolChoice["type"], string(sigma.AnthropicToolChoiceTool); got != want {
		t.Fatalf("tool choice type = %v, want %v", got, want)
	}
	if got, want := toolChoice["name"], "lookup"; got != want {
		t.Fatalf("tool choice name = %v, want %v", got, want)
	}
}

func TestTypedAnthropicOutputFormat(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-output-format-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("return json")}},
		sigma.WithAnthropicOptions(sigma.AnthropicOptions{
			OutputFormat: map[string]any{
				"type":   "json_schema",
				"schema": map[string]any{"type": "object"},
			},
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	outputFormat := payload["output_format"].(map[string]any)
	if got, want := outputFormat["type"], "json_schema"; got != want {
		t.Fatalf("output_format.type = %v, want %v", got, want)
	}
	if _, ok := outputFormat["schema"].(map[string]any); !ok {
		t.Fatalf("output_format.schema = %#v, want map", outputFormat["schema"])
	}
}

func TestTypedAnthropicDisableParallelToolUseWithToolChoice(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-disable-parallel-choice-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)
	disableParallel := true

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("use lookup")},
			Tools:    []sigma.Tool{{Name: "lookup", Description: "Lookup", InputSchema: sigma.Schema{"type": "object"}}},
		},
		sigma.WithAnthropicOptions(sigma.AnthropicOptions{
			ToolChoice:             &sigma.AnthropicToolChoice{Type: sigma.AnthropicToolChoiceTool, Name: "lookup"},
			DisableParallelToolUse: &disableParallel,
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	toolChoice := payload["tool_choice"].(map[string]any)
	if got, want := toolChoice["type"], string(sigma.AnthropicToolChoiceTool); got != want {
		t.Fatalf("tool choice type = %v, want %v", got, want)
	}
	if got, want := toolChoice["name"], "lookup"; got != want {
		t.Fatalf("tool choice name = %v, want %v", got, want)
	}
	if got, want := toolChoice["disable_parallel_tool_use"], true; got != want {
		t.Fatalf("disable_parallel_tool_use = %v, want %v", got, want)
	}
}

func TestTypedAnthropicDisableParallelToolUseSynthesizesAutoToolChoice(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-disable-parallel-auto-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)
	disableParallel := true

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("use lookup")},
			Tools:    []sigma.Tool{{Name: "lookup", Description: "Lookup", InputSchema: sigma.Schema{"type": "object"}}},
		},
		sigma.WithAnthropicOptions(sigma.AnthropicOptions{DisableParallelToolUse: &disableParallel}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	toolChoice := payload["tool_choice"].(map[string]any)
	if got, want := toolChoice["type"], string(sigma.AnthropicToolChoiceAuto); got != want {
		t.Fatalf("tool choice type = %v, want %v", got, want)
	}
	if got, want := toolChoice["disable_parallel_tool_use"], true; got != want {
		t.Fatalf("disable_parallel_tool_use = %v, want %v", got, want)
	}
}

func TestTypedAnthropicDisableParallelToolUseRejectsNonMapRawToolChoice(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-disable-parallel-conflict-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)
	disableParallel := true

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("use lookup")},
			Tools:    []sigma.Tool{{Name: "lookup", Description: "Lookup", InputSchema: sigma.Schema{"type": "object"}}},
		},
		sigma.WithAnthropicOptions(sigma.AnthropicOptions{DisableParallelToolUse: &disableParallel}),
		sigma.WithProviderOption(providerID, "tool_choice", "auto"),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if sigmaErr.Code != sigma.ErrorInvalidOptions {
		t.Fatalf("error code = %q, want %q", sigmaErr.Code, sigma.ErrorInvalidOptions)
	}
	if len(requests) != 0 {
		t.Fatal("provider request was sent after invalid options")
	}
}

func TestTypedAnthropicThinkingDisplayAppliesToAdaptiveThinking(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-adaptive-display-test")
	model := anthropicTestModel(providerID)
	model.AnthropicMessagesCompat = &sigma.AnthropicMessagesCompat{
		ThinkingFormat: sigma.AnthropicThinkingAdaptive,
	}
	client := anthropicTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
		sigma.WithAnthropicOptions(sigma.AnthropicOptions{
			ThinkingDisplay: sigma.AnthropicThinkingDisplayOmitted,
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	thinking := payload["thinking"].(map[string]any)
	if got, want := thinking["display"], string(sigma.AnthropicThinkingDisplayOmitted); got != want {
		t.Fatalf("thinking display = %v, want %v", got, want)
	}
}

func TestTypedAnthropicInterleavedThinkingBeta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		adaptive   bool
		interleave bool
		wantHeader string
	}{
		{
			name:       "enabled for budget thinking",
			interleave: true,
			wantHeader: "interleaved-thinking-2025-05-14",
		},
		{
			name:       "explicit false suppresses header",
			interleave: false,
		},
		{
			name:       "adaptive thinking skips beta",
			adaptive:   true,
			interleave: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeMessagesSSE(t, w, completedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("anthropic-interleaved-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := anthropicTestModel(providerID)
			if tt.adaptive {
				model.AnthropicMessagesCompat = &sigma.AnthropicMessagesCompat{
					ThinkingFormat: sigma.AnthropicThinkingAdaptive,
				}
			}
			client := anthropicTestClient(t, providerID, model, server.URL)

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
				sigma.WithAnthropicOptions(sigma.AnthropicOptions{
					InterleavedThinking: &tt.interleave,
				}),
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			request := receiveRequest(t, requests)
			if got := request.Headers.Get("Anthropic-Beta"); got != tt.wantHeader {
				t.Fatalf("Anthropic-Beta = %q, want %q", got, tt.wantHeader)
			}
		})
	}
}

func TestModelCompatSuppressesUnsupportedTemperature(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-temperature-test")
	model := anthropicTestModel(providerID)
	model.AnthropicMessagesCompat = &sigma.AnthropicMessagesCompat{
		SupportsTemperature: sigma.AnthropicCompatUnsupported,
	}
	client := anthropicTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithTemperature(0.1),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/anthropic/messages/temperature_unsupported_payload.json")
	goldentest.AssertNoJSONPath(t, request.Body, "temperature")
}

func TestSupportedTemperatureStillEmitsTemperature(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-temperature-control-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithTemperature(0.1),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/anthropic/messages/temperature_supported_payload.json")
}

func TestToolResultsAreGroupedAndEmptyThinkingSignatureFallsBackToText(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-tool-result-group-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{
			sigma.UserText("first"),
			{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.Thinking("internal reasoning", "")},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "toolu_1",
				Content:    []sigma.ContentBlock{sigma.Text("one")},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "toolu_2",
				IsError:    true,
				Content:    []sigma.ContentBlock{sigma.Text("two")},
			},
		}},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/anthropic/messages/grouped_tool_results_empty_signature_payload.json")
}

func TestEmptyThinkingSignatureCanBePreservedForCompatibleEndpoints(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-empty-signature-test")
	model := anthropicTestModel(providerID)
	model.AnthropicMessagesCompat = &sigma.AnthropicMessagesCompat{
		SupportsEmptyThinkingSignature: sigma.AnthropicCompatSupported,
	}
	client := anthropicTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{{
			Role:    sigma.RoleAssistant,
			Content: []sigma.ContentBlock{sigma.Thinking("internal reasoning", " ")},
		}}},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/anthropic/messages/empty_signature_preserved_payload.json")
}

func TestDetectedCompatibleVariantsAddAdaptiveThinkingAndSessionHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider sigma.ProviderID
	}{
		{name: "kimi", provider: sigma.ProviderKimi},
		{name: "fireworks", provider: sigma.ProviderFireworks},
		{name: "xiaomi", provider: sigma.ProviderXiaomi},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeMessagesSSE(t, w, completedEvent)
			}))
			t.Cleanup(server.Close)

			model := sigma.Model{
				ID:               "compatible-claude-test",
				Provider:         tt.provider,
				API:              sigma.APIAnthropicMessages,
				SupportsThinking: true,
			}
			client := anthropicTestClient(t, tt.provider, model, server.URL)

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
				sigma.WithCacheRetention(sigma.CacheRetentionShort),
				sigma.WithSessionID("affinity-123"),
				sigma.WithProviderOption(tt.provider, "session_id_header", "X-Affinity"),
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			request := receiveRequest(t, requests)
			assertHeader(t, request.Headers, "X-Affinity", "affinity-123")
			goldentest.AssertJSON(t, request.Body, "provider/anthropic/messages/adaptive_thinking_payload.json")
		})
	}
}

func TestStreamingMapsTextThinkingUsageAndMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMessagesSSE(t, w, `event: message_start
data: {"type":"message_start","message":{"id":"msg_stream","type":"message","role":"assistant","model":"claude-test-2026","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Checked "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"think_sig"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":" world"}}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"redacted_thinking","data":"redacted_payload"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"cache_creation":{"ephemeral_5m_input_tokens":4,"ephemeral_1h_input_tokens":2},"cache_read_input_tokens":3,"output_tokens":8}}

event: message_stop
data: {"type":"message_stop"}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-stream-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}

	if got, want := eventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindThinkingStart,
		sigma.EventKindThinkingDelta,
		sigma.EventKindTextStart,
		sigma.EventKindTextDelta,
		sigma.EventKindTextDelta,
		sigma.EventKindThinkingEnd,
		sigma.EventKindTextEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.Content[0].ThinkingText, "Checked "; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Signature, "think_sig"; got != want {
		t.Fatalf("thinking signature = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "Hello world"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if !final.Content[2].Redacted || final.Content[2].ProviderSignature != "redacted_payload" {
		t.Fatalf("redacted thinking = %#v, want redacted payload", final.Content[2])
	}
	if got, want := final.ProviderMetadata["id"], "msg_stream"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}
	if got, want := final.ProviderMetadata["model"], "claude-test-2026"; got != want {
		t.Fatalf("provider model = %v, want %v", got, want)
	}
	if final.Usage == nil {
		t.Fatal("final usage was nil")
	}
	if got, want := final.Usage.CacheWriteInputTokens, 6; got != want {
		t.Fatalf("cache write tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.LongCacheWriteInputTokens, 2; got != want {
		t.Fatalf("long cache write tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.CacheReadInputTokens, 3; got != want {
		t.Fatalf("cache read tokens = %d, want %d", got, want)
	}
}

func TestStreamingPreservesHostedToolAndProviderMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMessagesSSE(t, w, `data: {"type":"message_start","message":{"id":"msg_metadata","type":"message","role":"assistant","model":"claude-test","content":[]}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Grounded"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://example.com","cited_text":"fact"}}}

data: {"type":"content_block_start","index":1,"content_block":{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"go 1.26"}}}

data: {"type":"message_delta","delta":{"stop_reason":"tool_use","container":{"id":"ctr_1"}},"usage":{"output_tokens":9,"output_tokens_details":{"thinking_tokens":4}},"context_management":{"applied_edits":[{"type":"clear_thinking","cleared_input_tokens":12}]}}

data: {"type":"message_stop"}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-metadata-stream-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("search")}})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}

	if got, want := eventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindTextStart,
		sigma.EventKindTextDelta,
		sigma.EventKindToolCallStart,
		sigma.EventKindToolCallDelta,
		sigma.EventKindTextEnd,
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	citations, ok := final.Content[0].ProviderMetadata["citations"].([]map[string]any)
	if !ok || len(citations) != 1 {
		t.Fatalf("citations = %#v, want one citation", final.Content[0].ProviderMetadata["citations"])
	}
	if got, want := citations[0]["url"], "https://example.com"; got != want {
		t.Fatalf("citation url = %v, want %v", got, want)
	}
	if got, want := final.Content[1].ProviderMetadata["type"], "server_tool_use"; got != want {
		t.Fatalf("tool metadata type = %v, want %v", got, want)
	}
	if final.Usage == nil || final.Usage.ThinkingTokens != 4 {
		t.Fatalf("usage = %#v, want thinking tokens", final.Usage)
	}
	container, ok := final.ProviderMetadata["container"].(map[string]any)
	if !ok || container["id"] != "ctr_1" {
		t.Fatalf("container metadata = %#v", final.ProviderMetadata["container"])
	}
	contextManagement, ok := final.ProviderMetadata["context_management"].(map[string]any)
	if !ok || contextManagement["applied_edits"] == nil {
		t.Fatalf("context management metadata = %#v", final.ProviderMetadata["context_management"])
	}
}

func TestHostedToolUseReplaysAsServerToolUse(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-server-tool-replay-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)
	toolCall := sigma.ToolCallBlock("srvtoolu_1", "web_search", map[string]any{"query": "go 1.26"})
	toolCall.ProviderMetadata = map[string]any{"type": "server_tool_use"}

	_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{
		{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{toolCall}},
		sigma.UserText("continue"),
	}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	messages := payload["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	replayed := content[0].(map[string]any)
	if got, want := replayed["type"], "server_tool_use"; got != want {
		t.Fatalf("replayed tool type = %v, want %v", got, want)
	}
	if got, want := replayed["id"], "srvtoolu_1"; got != want {
		t.Fatalf("replayed tool id = %v, want %v", got, want)
	}
}

func TestStreamUsageMergeContentBlockStopAndStopReasons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		stopReason string
		want       sigma.StopReason
	}{
		{name: "pause turn", stopReason: "pause_turn", want: sigma.StopReasonEndTurn},
		{name: "sensitive", stopReason: "sensitive", want: sigma.StopReasonContentFilter},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeMessagesSSE(t, w, `data: {"type":"message_start","message":{"id":"msg_usage","type":"message","role":"assistant","model":"claude-test","content":[],"usage":{"input_tokens":7,"cache_read_input_tokens":2}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"`+tt.stopReason+`"},"usage":{"output_tokens":3}}

data: {"type":"message_stop"}
`)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("anthropic-stop-test-" + tt.name)
			model := anthropicTestModel(providerID)
			client := anthropicTestClient(t, providerID, model, server.URL)

			stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
			events := collectEvents(t, stream)
			if err := stream.Err(); err != nil {
				t.Fatalf("stream error = %v", err)
			}
			final, ok := stream.Final()
			if !ok {
				t.Fatal("stream final was not recorded")
			}

			if got, want := eventKinds(events), []sigma.EventKind{
				sigma.EventKindStart,
				sigma.EventKindTextStart,
				sigma.EventKindTextDelta,
				sigma.EventKindTextEnd,
				sigma.EventKindDone,
			}; !reflect.DeepEqual(got, want) {
				t.Fatalf("event kinds = %v, want %v", got, want)
			}
			if got := final.StopReason; got != tt.want {
				t.Fatalf("stop reason = %q, want %q", got, tt.want)
			}
			if final.Usage == nil {
				t.Fatal("final usage was nil")
			}
			if got, want := final.Usage.InputTokens, 7; got != want {
				t.Fatalf("input tokens = %d, want %d", got, want)
			}
			if got, want := final.Usage.OutputTokens, 3; got != want {
				t.Fatalf("output tokens = %d, want %d", got, want)
			}
			if got, want := final.Usage.CacheReadInputTokens, 2; got != want {
				t.Fatalf("cache read tokens = %d, want %d", got, want)
			}
		})
	}
}

func TestEagerToolInputStreamingBeforeContentBlockStart(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMessagesSSE(t, w, `data: {"type":"message_start","message":{"id":"msg_eager","type":"message","role":"assistant","model":"kimi-claude-test","content":[]}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"Melbourne\"}"}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_eager","name":"weather","input":{}}}

data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":5,"output_tokens":6}}

data: {"type":"message_stop"}
`)
	}))
	t.Cleanup(server.Close)

	model := sigma.Model{
		ID:       "kimi-claude-test",
		Provider: sigma.ProviderKimi,
		API:      sigma.APIAnthropicMessages,
	}
	client := anthropicTestClient(t, sigma.ProviderKimi, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("weather")}})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}

	if got, want := eventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindToolCallStart,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.Content[0].ToolCallID, "toolu_eager"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ToolName, "weather"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
	args := final.Content[0].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("tool city = %v, want %v", got, want)
	}
}

func TestToolCallPartialJSONStreamingProducesFinalArguments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMessagesSSE(t, w, `data: {"type":"message_start","message":{"id":"msg_tool","type":"message","role":"assistant","model":"claude-test","content":[]}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"weather","input":{}}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":":\"Melbourne\"}"}}

data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":5,"output_tokens":6}}

data: {"type":"message_stop"}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-tool-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("weather")}})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}

	if got, want := eventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindToolCallStart,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonToolCalls; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ToolCallID, "toolu_1"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	args := final.Content[0].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("tool city = %v, want %v", got, want)
	}
}

func TestStreamRepairsMalformedJSONAndToolArguments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMessagesSSE(t, w, `event: message_start
data: {"type":"message_start","message":{"id":"msg_repair","type":"message","role":"assistant","model":"claude-test","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_repair","name":"edit","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"A\H\",\"text\":\"col1	col2\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}

event: message_stop
data: {"type":"message_stop"}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-repair-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("edit")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.StopReason, sigma.StopReasonToolCalls; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if len(final.Content) != 1 || final.Content[0].Type != sigma.ContentBlockToolCall {
		t.Fatalf("content = %#v, want one tool call", final.Content)
	}
	args := final.Content[0].ToolArguments.(map[string]any)
	if got, want := args["path"], `A\H`; got != want {
		t.Fatalf("tool path = %q, want %q", got, want)
	}
	if got, want := args["text"], "col1\tcol2"; got != want {
		t.Fatalf("tool text = %q, want %q", got, want)
	}
}

func TestStreamStopsAtMessageStopBeforeTrailingProxyEvents(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMessagesSSE(t, w, completedEvent+`
event: done
data: [DONE]

event: proxy.stats
data: not json
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-trailing-event-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "ok"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestStreamEOFBeforeMessageStopReturnsPartialFinal(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMessagesSSE(t, w, `data: {"type":"message_start","message":{"id":"msg_truncated","type":"message","role":"assistant","model":"claude-test","content":[]}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-truncated-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !strings.Contains(err.Error(), "stream ended before message_stop") {
		t.Fatalf("error = %v, want stream ended before message_stop", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Text, "partial"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
	if got, want := final.ProviderMetadata["id"], "msg_truncated"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}
}

func TestProviderErrorIsTypedAndRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("request-id", "req_123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key sk-secret123"}}`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-error-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.Diagnostics[0].API, sigma.APIAnthropicMessages; got != want {
		t.Fatalf("diagnostic API = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "sk-secret123") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestContextOverflowProviderErrorIsInspectable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"context length exceeds maximum"}}`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-context-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrContextOverflow) {
		t.Fatalf("error = %v, want ErrContextOverflow", err)
	}
}

func TestStreamErrorEventEndsWithError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMessagesSSE(t, w, `data: {"type":"message_start","message":{"id":"msg_error","type":"message","role":"assistant","model":"claude-test","content":[]}}

data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-stream-error-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	classification := sigma.ClassifyError(err)
	if got, want := classification.Class, sigma.ErrorClassTransient; got != want {
		t.Fatalf("class = %q, want %q", got, want)
	}
	if got, want := classification.ProviderCode, "overloaded_error"; got != want {
		t.Fatalf("provider code = %q, want %q", got, want)
	}
	if !strings.Contains(err.Error(), "overloaded_error") {
		t.Fatalf("error = %v, want overloaded_error", err)
	}
}

func TestCancellationAbortsStreamingRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_cancel","type":"message","role":"assistant","model":"claude-test","content":[]}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"partial plan"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"partial text"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_partial","name":"lookup","input":{}}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"Melbourne\"}"}}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-cancel-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	stream := client.Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	for {
		event := receiveEvent(t, stream)
		if event.Kind == sigma.EventKindToolCallDelta &&
			event.PartialToolCall != nil &&
			event.PartialToolCall.ArgumentsDelta != "" {
			break
		}
	}
	cancel()

	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorAborted {
		t.Fatalf("Collect error = %v, want ErrorAborted", err)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ThinkingText, "partial plan"; got != want {
		t.Fatalf("partial thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "partial text"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
	if got, want := final.Content[2].ToolCallID, "toolu_partial"; got != want {
		t.Fatalf("partial tool id = %q, want %q", got, want)
	}
	args := final.Content[2].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("partial tool city = %v, want %v", got, want)
	}
}

func TestStreamCloseCancelsStreamingRequest(t *testing.T) {
	t.Parallel()

	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_close","type":"message","role":"assistant","model":"claude-test","content":[]}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(requestCanceled)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("anthropic-close-test")
	model := anthropicTestModel(providerID)
	client := anthropicTestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	for {
		event := receiveEvent(t, stream)
		if event.Kind == sigma.EventKindTextDelta {
			break
		}
	}
	stream.Close()

	receiveSignal(t, requestCanceled)
	receiveSignal(t, stream.Done())
}

func anthropicTestClient(t *testing.T, providerID sigma.ProviderID, model sigma.Model, baseURL string, opts ...anthropic.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]anthropic.ProviderOption{anthropic.WithBaseURL(baseURL)}, opts...)
	if err := registry.RegisterTextProvider(providerID, anthropic.NewProvider(providerOpts...)); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-key"}, nil
	})
	return sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(resolver),
		sigma.WithDefaultHeader("X-Client", "client"),
	)
}

func decodePayload(t *testing.T, body string) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload
}

func invalidProviderText() string {
	return string([]byte{0xff}) + "clean"
}

func assertAnthropicTextBlock(t *testing.T, message any, want string) {
	t.Helper()

	typed := message.(map[string]any)
	content := typed["content"].([]any)
	block := content[0].(map[string]any)
	if got := block["text"]; got != want {
		t.Fatalf("text block = %v, want %q", got, want)
	}
}

func anthropicTestModel(providerID sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:       "claude-test",
		Provider: providerID,
		API:      sigma.APIAnthropicMessages,
		SupportedInputs: []sigma.ContentBlockType{
			sigma.ContentBlockText,
			sigma.ContentBlockImage,
		},
		SupportsTools:                true,
		SupportsThinking:             true,
		ThinkingLevelMap:             map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "4096"},
		InputCostPerMillion:          1,
		OutputCostPerMillion:         2,
		CacheReadInputCostPerMillion: 0.5,
	}
}

func richRequest() sigma.Request {
	redacted := sigma.Thinking("", "")
	redacted.Redacted = true
	redacted.ProviderSignature = "redacted_previous"

	return sigma.Request{
		SystemPrompt: "You are helpful.",
		Messages: []sigma.Message{
			{
				Role:    sigma.RoleDeveloper,
				Content: []sigma.ContentBlock{sigma.Text("Use terse answers.")},
			},
			sigma.UserContent(
				sigma.Text("Describe this"),
				sigma.ImageURL("image/png", "https://example.test/cat.png"),
				sigma.ImageBase64("image/png", "aGk="),
			),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.Text("Earlier answer."),
					sigma.Thinking("Internal summary.", "think_prev_sig"),
					redacted,
					sigma.ToolCallBlock("toolu_prev", "lookup", map[string]any{"query": "weather"}),
				},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "toolu_prev",
				Content: []sigma.ContentBlock{
					sigma.Text("Sunny"),
					sigma.ImageBase64("image/png", "aGk="),
				},
			},
		},
		Tools: []sigma.Tool{{
			Name:        "weather",
			Description: "Get weather",
			InputSchema: sigma.Schema{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
				"required":             []any{"city"},
				"additionalProperties": false,
			},
		}},
	}
}

func captureRequest(t *testing.T, requests chan<- capturedRequest, r *http.Request) {
	t.Helper()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("ReadAll request body returned error: %v", err)
	}
	requests <- capturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
		Body:    string(body),
	}
}

func receiveRequest(t *testing.T, requests <-chan capturedRequest) capturedRequest {
	t.Helper()

	select {
	case request := <-requests:
		return request
	default:
		t.Fatal("server did not receive request")
		return capturedRequest{}
	}
}

func writeMessagesSSE(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, body)
}

func collectEvents(t *testing.T, stream *sigma.Stream) []sigma.Event {
	t.Helper()

	var events []sigma.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events
}

func receiveEvent(t *testing.T, stream *sigma.Stream) sigma.Event {
	t.Helper()

	event, ok := <-stream.Events()
	if !ok {
		t.Fatal("stream closed before event")
	}
	return event
}

func receiveSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func eventKinds(events []sigma.Event) []sigma.EventKind {
	kinds := make([]sigma.EventKind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	return kinds
}

func assertHeader(t *testing.T, headers http.Header, key string, want string) {
	t.Helper()

	if got := headers.Get(key); got != want {
		t.Fatalf("%s header = %q, want %q", key, got, want)
	}
}

func intPtr(value int) *int {
	return &value
}

const completedEvent = `data: {"type":"message_start","message":{"id":"msg_complete","type":"message","role":"assistant","model":"claude-test","content":[]}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}

data: {"type":"message_stop"}
`
