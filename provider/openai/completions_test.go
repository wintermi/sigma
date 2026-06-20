// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
	"github.com/wintermi/sigma/provider/openai"
)

func TestRegisterReportsChatCompletionsAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("compatible")
	if err := openai.Register(registry, providerID); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(sigma.Model{
		ID:       "gpt-compatible",
		Provider: providerID,
		API:      sigma.APIOpenAICompletions,
	}); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestCompleteStreamsTextAndSendsGoldenPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL, openai.WithHeader("X-Provider", "provider"))

	final, err := client.Complete(
		context.Background(),
		model,
		richRequest(),
		sigma.WithTemperature(0.2),
		sigma.WithMaxTokens(123),
		sigma.WithCacheRetention(sigma.CacheRetentionEphemeral),
		sigma.WithSessionID("session-123"),
		sigma.WithHeader("X-Custom", "custom"),
		sigma.WithMetadataValue("trace", "abc"),
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{
			ReasoningEffort: sigma.ThinkingLevelHigh,
			ServiceTier:     "default",
			ToolChoice:      "required",
		}),
		sigma.WithProviderOptions(providerID, map[string]any{
			"session_id_header": "X-Session-ID",
			"extra_body": map[string]any{
				"parallel_tool_calls": false,
			},
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "Hello world"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonEndTurn; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if final.Usage == nil {
		t.Fatal("final usage was nil")
	}
	if got, want := final.Usage.InputTokens, 5; got != want {
		t.Fatalf("input tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.CacheReadInputTokens, 3; got != want {
		t.Fatalf("cache read tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.CacheWriteInputTokens, 2; got != want {
		t.Fatalf("cache write tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.ThinkingTokens, 2; got != want {
		t.Fatalf("thinking tokens = %d, want %d", got, want)
	}
	if final.Cost == nil || final.Cost.TotalCost == 0 {
		t.Fatalf("final cost = %#v, want populated cost", final.Cost)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/chat/completions"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer resolved-key")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	assertHeader(t, request.Headers, "X-Session-ID", "session-123")
	goldentest.AssertJSON(t, request.Body, "provider/openai/completions/rich_payload.json")
}

func TestChatCompletionsSendsTypedResponseFormatAndLogprobs(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-options-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)
	responseFormat := map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "judge",
			"strict": true,
			"schema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
		},
	}

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("judge")}},
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{
			ResponseFormat: responseFormat,
			TopLogprobs:    2,
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if !reflect.DeepEqual(payload["response_format"], responseFormat) {
		t.Fatalf("response_format = %#v, want %#v", payload["response_format"], responseFormat)
	}
	if got, want := payload["logprobs"], true; got != want {
		t.Fatalf("logprobs = %v, want %v", got, want)
	}
	if got, want := payload["top_logprobs"], float64(2); got != want {
		t.Fatalf("top_logprobs = %v, want %v", got, want)
	}
}

func TestChatCompletionsRequestShapeGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       sigma.Request
		modelFunc func(sigma.Model) sigma.Model
		opts      []sigma.Option
		wantField string
		wantValue float64
	}{
		{
			name: "omitted tools and default max tokens",
			req:  sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		},
		{
			name: "empty tools",
			req: sigma.Request{
				Messages: []sigma.Message{sigma.UserText("hi")},
				Tools:    []sigma.Tool{},
			},
		},
		{
			name: "explicit max completion tokens",
			req:  sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			modelFunc: func(model sigma.Model) sigma.Model {
				compat := *model.OpenAICompletionsCompat
				compat.MaxTokensField = sigma.OpenAICompletionsMaxCompletionTokens
				model.OpenAICompletionsCompat = &compat
				return model
			},
			opts:      []sigma.Option{sigma.WithMaxTokens(1234)},
			wantField: "max_completion_tokens",
			wantValue: 1234,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeFixture(t, w, "text_usage.sse")
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("openai-request-shape-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := openAITestModel(providerID)
			if tt.modelFunc != nil {
				model = tt.modelFunc(model)
			}
			client := openAITestClient(t, providerID, model, server.URL)

			if _, err := client.Complete(context.Background(), model, tt.req, tt.opts...); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			var payload map[string]any
			if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
				t.Fatalf("Unmarshal request body returned error: %v", err)
			}
			if _, ok := payload["tools"]; ok {
				t.Fatalf("tools = %#v, want absent", payload["tools"])
			}
			for _, field := range []string{"max_tokens", "max_completion_tokens"} {
				if field == tt.wantField {
					continue
				}
				if _, ok := payload[field]; ok {
					t.Fatalf("%s = %#v, want absent", field, payload[field])
				}
			}
			if tt.wantField != "" {
				if got := payload[tt.wantField]; got != tt.wantValue {
					t.Fatalf("%s = %v, want %v", tt.wantField, got, tt.wantValue)
				}
			}
		})
	}
}

func TestChatCompletionsPreservesRequestedAndProviderReportedModel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_route","model":"provider/routed-model","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_route","model":"provider/routed-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-routed-model-test")
	model := openAITestModel(providerID)
	model.ID = "router/auto"
	client := openAITestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Model, sigma.ModelID("router/auto"); got != want {
		t.Fatalf("final model = %q, want %q", got, want)
	}
	if got, want := final.ProviderMetadata["model"], "provider/routed-model"; got != want {
		t.Fatalf("provider model metadata = %v, want %q", got, want)
	}
}

func TestChatCompletionsDerivesPromptCacheFields(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-cache-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)
	sessionID := strings.Repeat("x", 70)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID(sessionID),
		sigma.WithCacheRetention(sigma.CacheRetentionPersistent),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := payload["prompt_cache_key"], strings.Repeat("x", 64); got != want {
		t.Fatalf("prompt_cache_key = %v, want %q", got, want)
	}
	if got, want := payload["prompt_cache_retention"], "24h"; got != want {
		t.Fatalf("prompt_cache_retention = %v, want %q", got, want)
	}
}

func TestChatCompletionsNormalizesProviderTextInPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-normalized-payload-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)
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
						sigma.Thinking("thinking"+invalid, ""),
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

	var payload struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	assertPayloadText(t, payload.Messages[0]["content"], "systemclean")
	assertPayloadText(t, payload.Messages[1]["content"], "developerclean")
	assertPayloadText(t, payload.Messages[2]["content"], "userclean")
	assertPayloadText(t, payload.Messages[3]["content"], "assistantclean\nthinkingclean")
	assertPayloadText(t, payload.Messages[4]["content"], "toolclean")
}

func TestChatCompletionsDropsUnansweredToolCallsBeforeUserTurn(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-drop-unanswered-tool-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

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

	var payload struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := len(payload.Messages), 1; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	if got, want := payload.Messages[0]["role"], "user"; got != want {
		t.Fatalf("message role = %v, want %q", got, want)
	}
	if _, ok := payload.Messages[0]["tool_calls"]; ok {
		t.Fatalf("payload kept abandoned tool call: %#v", payload.Messages[0])
	}
}

func TestChatCompletionsStreamingNormalizesInvalidUTF8(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(append([]byte(`data: {"choices":[{"index":0,"delta":{"content":"bad`), append([]byte{0xff}, []byte(`text"},"finish_reason":"stop"}]}`+"\n\n")...)...))
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-normalized-stream-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "badtext"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
}

func TestChatCompletionsCopilotDynamicHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		request     sigma.Request
		options     []sigma.Option
		wantHeaders map[string]string
		wantAbsent  []string
	}{
		{
			name:    "user initiated",
			request: sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			wantHeaders: map[string]string{
				"X-Initiator":   "user",
				"Openai-Intent": "conversation-edits",
			},
			wantAbsent: []string{"Copilot-Vision-Request"},
		},
		{
			name: "agent initiated with images",
			request: sigma.Request{Messages: []sigma.Message{
				sigma.UserText("inspect"),
				{Role: sigma.RoleTool, ToolCallID: "call_1", Content: []sigma.ContentBlock{sigma.ImageBase64("image/png", "aGk=")}},
			}},
			wantHeaders: map[string]string{
				"X-Initiator":            "agent",
				"Openai-Intent":          "conversation-edits",
				"Copilot-Vision-Request": "true",
			},
		},
		{
			name:    "caller override",
			request: sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			options: []sigma.Option{
				sigma.WithHeader("X-Initiator", "override"),
				sigma.WithHeader("Openai-Intent", "override-intent"),
				sigma.WithHeader("Copilot-Vision-Request", "override-vision"),
			},
			wantHeaders: map[string]string{
				"X-Initiator":            "override",
				"Openai-Intent":          "override-intent",
				"Copilot-Vision-Request": "override-vision",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeFixture(t, w, "text_usage.sse")
			}))
			t.Cleanup(server.Close)

			model := openAITestModel(sigma.ProviderGitHubCopilot)
			client := openAITestClient(t, sigma.ProviderGitHubCopilot, model, server.URL)

			_, err := client.Complete(context.Background(), model, tt.request, tt.options...)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			headers := receiveRequest(t, requests).Headers
			for key, value := range tt.wantHeaders {
				assertHeader(t, headers, key, value)
			}
			for _, key := range tt.wantAbsent {
				assertHeaderAbsent(t, headers, key)
			}
		})
	}
}

func TestChatCompletionsCloudflareGatewayBaseURLAndAuthHeader(t *testing.T) {
	t.Setenv("CLOUDFLARE_GATEWAY_ID", "compat")

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("cloudflare-ai-gateway")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL+"/{CLOUDFLARE_GATEWAY_ID}")

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithHeader("cf-aig-authorization", "Bearer override"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/compat/chat/completions"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "cf-aig-authorization", "Bearer override")
	assertHeaderAbsent(t, request.Headers, "Authorization")
}

func TestChatCompletionsSessionAffinityHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		retention   sigma.CacheRetention
		support     sigma.OpenAICompatSupport
		options     []sigma.Option
		wantHeaders map[string]string
		wantAbsent  []string
	}{
		{
			name:      "generated when supported and caching enabled",
			retention: sigma.CacheRetentionShort,
			support:   sigma.OpenAICompatSupported,
			wantHeaders: map[string]string{
				"session_id":          "session-affinity",
				"x-client-request-id": "session-affinity",
				"x-session-affinity":  "session-affinity",
			},
		},
		{
			name:      "suppressed when caching disabled",
			retention: sigma.CacheRetentionNone,
			support:   sigma.OpenAICompatSupported,
			wantAbsent: []string{
				"session_id",
				"x-client-request-id",
				"x-session-affinity",
			},
		},
		{
			name:      "suppressed when unsupported",
			retention: sigma.CacheRetentionShort,
			support:   sigma.OpenAICompatUnsupported,
			wantAbsent: []string{
				"session_id",
				"x-client-request-id",
				"x-session-affinity",
			},
		},
		{
			name:      "caller headers override generated values",
			retention: sigma.CacheRetentionShort,
			support:   sigma.OpenAICompatSupported,
			options: []sigma.Option{
				sigma.WithHeader("session_id", "override-session"),
				sigma.WithHeader("x-client-request-id", "override-request"),
				sigma.WithHeader("x-session-affinity", "override-affinity"),
			},
			wantHeaders: map[string]string{
				"session_id":          "override-session",
				"x-client-request-id": "override-request",
				"x-session-affinity":  "override-affinity",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeFixture(t, w, "text_usage.sse")
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("openai-affinity-test-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := openAITestModel(providerID)
			model.OpenAICompletionsCompat.SupportsSessionAffinity = tt.support
			client := openAITestClient(t, providerID, model, server.URL)
			options := []sigma.Option{
				sigma.WithSessionID("session-affinity"),
				sigma.WithCacheRetention(tt.retention),
			}
			options = append(options, tt.options...)

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				options...,
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			headers := receiveRequest(t, requests).Headers
			for key, value := range tt.wantHeaders {
				assertHeader(t, headers, key, value)
			}
			for _, key := range tt.wantAbsent {
				assertHeaderAbsent(t, headers, key)
			}
		})
	}
}

func TestChatCompletionsNormalizesReplayToolIDsAndCarriesToolResultImages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		inputs      []sigma.ContentBlockType
		wantSidecar bool
	}{
		{
			name: "image capable",
			inputs: []sigma.ContentBlockType{
				sigma.ContentBlockText,
				sigma.ContentBlockImage,
			},
			wantSidecar: true,
		},
		{
			name:        "text only",
			inputs:      []sigma.ContentBlockType{sigma.ContentBlockText},
			wantSidecar: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeFixture(t, w, "text_usage.sse")
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("openai-replay-test-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := openAITestModel(providerID)
			model.SupportedInputs = tt.inputs
			client := openAITestClient(t, providerID, model, server.URL)
			toolCallID := "call/with+bad|fc_item_with_provider_suffix"

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{
					sigma.UserText("inspect"),
					{
						Role: sigma.RoleAssistant,
						Content: []sigma.ContentBlock{
							sigma.ToolCallBlock(toolCallID, "lookup", map[string]any{"query": "weather"}),
						},
					},
					{
						Role:       sigma.RoleTool,
						ToolCallID: toolCallID,
						Content: []sigma.ContentBlock{
							sigma.Text("Sunny"),
							sigma.ImageBase64("image/png", "aGk="),
						},
					},
				}},
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			var payload struct {
				Messages []map[string]any `json:"messages"`
			}
			if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
				t.Fatalf("Unmarshal request body returned error: %v", err)
			}
			assistant := payload.Messages[1]
			toolCalls, ok := assistant["tool_calls"].([]any)
			if !ok || len(toolCalls) != 1 {
				t.Fatalf("tool_calls = %#v, want one call", assistant["tool_calls"])
			}
			toolCall := toolCalls[0].(map[string]any)
			if got, want := toolCall["id"], "call_with_bad"; got != want {
				t.Fatalf("assistant tool id = %v, want %q", got, want)
			}
			toolResult := payload.Messages[2]
			if got, want := toolResult["tool_call_id"], "call_with_bad"; got != want {
				t.Fatalf("tool result id = %v, want %q", got, want)
			}
			if !tt.wantSidecar {
				if got, want := len(payload.Messages), 3; got != want {
					t.Fatalf("message count = %d, want %d", got, want)
				}
				return
			}
			if got, want := len(payload.Messages), 4; got != want {
				t.Fatalf("message count = %d, want %d", got, want)
			}
			sidecar := payload.Messages[3]
			if got, want := sidecar["role"], "user"; got != want {
				t.Fatalf("sidecar role = %v, want %q", got, want)
			}
			parts, ok := sidecar["content"].([]any)
			if !ok || len(parts) != 2 {
				t.Fatalf("sidecar content = %#v, want text plus image", sidecar["content"])
			}
			image := parts[1].(map[string]any)
			if got, want := image["type"], "image_url"; got != want {
				t.Fatalf("sidecar image type = %v, want %q", got, want)
			}
		})
	}
}

func TestChatCompletionsBatchesConsecutiveToolResultImages(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-batched-tool-images-test")
	model := openAITestModel(providerID)
	model.SupportedInputs = []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage}
	client := openAITestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{
			sigma.UserText("inspect both"),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_1", "lookup", map[string]any{"path": "one.png"}),
					sigma.ToolCallBlock("call_2", "lookup", map[string]any{"path": "two.png"}),
				},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call_1",
				Content:    []sigma.ContentBlock{sigma.Text("one"), sigma.ImageBase64("image/png", "b25l")},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call_2",
				Content:    []sigma.ContentBlock{sigma.Text("two"), sigma.ImageBase64("image/png", "dHdv")},
			},
		}},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	roles := make([]any, len(payload.Messages))
	for i, message := range payload.Messages {
		roles[i] = message["role"]
	}
	if want := []any{"user", "assistant", "tool", "tool", "user"}; !reflect.DeepEqual(roles, want) {
		t.Fatalf("roles = %#v, want %#v", roles, want)
	}
	sidecar := payload.Messages[4]
	parts, ok := sidecar["content"].([]any)
	if !ok || len(parts) != 3 {
		t.Fatalf("sidecar content = %#v, want text plus two images", sidecar["content"])
	}
}

func TestChatCompletionsReplaysThinkingAsAssistantText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     []sigma.ContentBlock
		wantContent string
	}{
		{
			name: "thinking with visible text",
			content: []sigma.ContentBlock{
				sigma.Thinking("internal reasoning\n", ""),
				sigma.Text("visible answer"),
			},
			wantContent: "internal reasoning\nvisible answer",
		},
		{
			name:        "thinking only",
			content:     []sigma.ContentBlock{sigma.Thinking("internal reasoning", "")},
			wantContent: "internal reasoning",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeFixture(t, w, "text_usage.sse")
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("openai-thinking-replay-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := openAITestModel(providerID)
			model.OpenAICompletionsCompat.RequiresReasoningContentOnAssistantMessages = sigma.OpenAICompatUnsupported
			client := openAITestClient(t, providerID, model, server.URL)

			_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{
				sigma.UserText("hello"),
				{
					Role:     sigma.RoleAssistant,
					Provider: providerID,
					API:      sigma.APIOpenAICompletions,
					Model:    model.ID,
					Content:  tt.content,
				},
				sigma.UserText("continue"),
			}})
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			var payload struct {
				Messages []map[string]any `json:"messages"`
			}
			if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
				t.Fatalf("Unmarshal request body returned error: %v", err)
			}
			if got, want := payload.Messages[1]["role"], "assistant"; got != want {
				t.Fatalf("assistant role = %v, want %q", got, want)
			}
			if got := payload.Messages[1]["reasoning_content"]; got != nil {
				t.Fatalf("reasoning_content = %v, want absent", got)
			}
			if got := payload.Messages[1]["content"]; got != tt.wantContent {
				t.Fatalf("assistant content = %#v, want %q", got, tt.wantContent)
			}
		})
	}
}

func TestChatCompletionsStreamingParsesReasoningText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"reasoning_text":"Check "},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"reasoning_text":"constraints."},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"Done"},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-reasoning-text-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].ThinkingText, "Check constraints."; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "Done"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestChatCompletionsRejectsProviderDefinedTools(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-completions-provider-tools-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("Search current docs.")},
		Tools:    []sigma.Tool{openai.Tools.WebSearch()},
	})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if got, want := sigmaErr.Code, sigma.ErrorUnsupported; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if !strings.Contains(err.Error(), "provider-defined tool") {
		t.Fatalf("error = %v, want provider-defined tool message", err)
	}
	select {
	case request := <-requests:
		t.Fatalf("unexpected provider request: %#v", request)
	default:
	}
}

func TestToolCallStreamingProducesFinalArguments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(t, w, "tool_call.sse")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-tool-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

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
	if got, want := final.Content[0].ToolCallID, "call_1"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ToolName, "weather"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
	args, ok := final.Content[0].ToolArguments.(map[string]any)
	if !ok {
		t.Fatalf("tool arguments type = %T, want map", final.Content[0].ToolArguments)
	}
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("tool city = %v, want %v", got, want)
	}

	var sawDecodedPartial bool
	for _, event := range events {
		if event.Kind != sigma.EventKindToolCallDelta || event.PartialToolCall == nil {
			continue
		}
		if _, ok := event.PartialToolCall.ProviderMetadata["arguments"].(map[string]any); ok {
			sawDecodedPartial = true
		}
	}
	if !sawDecodedPartial {
		t.Fatal("tool-call deltas did not expose decoded partial arguments when JSON became valid")
	}
}

func TestStreamingParsesOpenAICompatibleMetadataAndFallbackToolID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_meta","model":"gpt-provider","choices":[{"index":0,"delta":{"content":[{"type":"text","text":"Hello"},{"type":"text","text":" array"}],"annotations":[{"type":"url_citation","url_citation":{"url":"https://annotation.example","title":"Annotation","start_index":0,"end_index":5}}]},"logprobs":{"content":[{"token":"Hello","logprob":-0.1}]}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup","arguments":""}}]},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"real_call","function":{"arguments":"{\"city\":\"Melbourne\"}"}}]},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_meta","model":"gpt-provider","choices":[{"index":0,"delta":{},"logprobs":{"content":[{"token":" array","logprob":-0.2}]}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_meta","model":"gpt-provider","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"citations":["https://top.example"],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"completion_tokens_details":{"reasoning_tokens":3,"accepted_prediction_tokens":4,"rejected_prediction_tokens":5}}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-metadata-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("weather")}})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}
	if got, want := final.Content[0].Text, "Hello array"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := final.Content[1].ToolCallID, "real_call"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	if got, want := final.Content[1].ToolName, "lookup"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
	args := final.Content[1].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("tool city = %v, want %v", got, want)
	}
	var sawFallback bool
	for _, event := range events {
		if event.Kind == sigma.EventKindToolCallStart && event.PartialToolCall != nil && event.PartialToolCall.ID == "call_0" {
			sawFallback = true
		}
	}
	if !sawFallback {
		t.Fatal("tool-call start did not use fallback id call_0")
	}
	if got, want := final.ProviderMetadata["id"], "chatcmpl_meta"; got != want {
		t.Fatalf("metadata id = %v, want %v", got, want)
	}
	if got, want := final.ProviderMetadata["model"], "gpt-provider"; got != want {
		t.Fatalf("metadata model = %v, want %v", got, want)
	}
	if got, want := final.ProviderMetadata["acceptedPredictionTokens"], 4; got != want {
		t.Fatalf("accepted prediction tokens = %v, want %v", got, want)
	}
	if got, want := final.ProviderMetadata["rejectedPredictionTokens"], 5; got != want {
		t.Fatalf("rejected prediction tokens = %v, want %v", got, want)
	}
	logprobs, ok := final.ProviderMetadata["logprobs"].(map[string]any)
	if !ok {
		t.Fatalf("logprobs metadata type = %T, want map", final.ProviderMetadata["logprobs"])
	}
	content, ok := logprobs["content"].([]any)
	if !ok {
		t.Fatalf("logprobs content type = %T, want []any", logprobs["content"])
	}
	if got, want := len(content), 2; got != want {
		t.Fatalf("logprobs content length = %d, want %d", got, want)
	}
	sources, ok := final.ProviderMetadata["sources"].([]map[string]any)
	if !ok {
		t.Fatalf("sources metadata type = %T, want []map[string]any", final.ProviderMetadata["sources"])
	}
	if got, want := len(sources), 2; got != want {
		t.Fatalf("source count = %d, want %d", got, want)
	}
	if got, want := sources[0]["url"], "https://annotation.example"; got != want {
		t.Fatalf("annotation source URL = %v, want %v", got, want)
	}
	if got, want := sources[1]["url"], "https://top.example"; got != want {
		t.Fatalf("top-level source URL = %v, want %v", got, want)
	}
	resultSources := final.Sources()
	if got, want := len(resultSources), 2; got != want {
		t.Fatalf("result source count = %d, want %d", got, want)
	}
	if got, want := resultSources[0].URL, "https://annotation.example"; got != want {
		t.Fatalf("result annotation source URL = %q, want %q", got, want)
	}
	if resultSources[0].StartIndex == nil || *resultSources[0].StartIndex != 0 {
		t.Fatalf("result annotation start index = %#v, want 0", resultSources[0].StartIndex)
	}
	if resultSources[0].EndIndex == nil || *resultSources[0].EndIndex != 5 {
		t.Fatalf("result annotation end index = %#v, want 5", resultSources[0].EndIndex)
	}
	if got, want := resultSources[1].URL, "https://top.example"; got != want {
		t.Fatalf("result top-level source URL = %q, want %q", got, want)
	}
}

func TestStreamingParsesChoiceUsage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_choice_usage","model":"gpt-provider","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_choice_usage","model":"gpt-provider","choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":3,"cache_write_tokens":2},"completion_tokens_details":{"reasoning_tokens":4},"cost":0.125,"currency":"USD"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-choice-usage-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}
	if final.Usage == nil {
		t.Fatal("final usage was nil")
	}
	if got, want := final.Usage.InputTokens, 5; got != want {
		t.Fatalf("input tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.CacheReadInputTokens, 3; got != want {
		t.Fatalf("cache read tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.CacheWriteInputTokens, 2; got != want {
		t.Fatalf("cache write tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.OutputTokens, 5; got != want {
		t.Fatalf("output tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.ThinkingTokens, 4; got != want {
		t.Fatalf("thinking tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.Provider, providerID; got != want {
		t.Fatalf("usage provider = %q, want %q", got, want)
	}
	if got, want := final.Usage.Model, model.ID; got != want {
		t.Fatalf("usage model = %q, want %q", got, want)
	}
	if got, want := final.Usage.Raw["prompt_tokens"], float64(10); got != want {
		t.Fatalf("raw prompt tokens = %v, want %v", got, want)
	}
	if events[len(events)-1].Usage == nil || events[len(events)-1].Usage.Raw["prompt_tokens"] != float64(10) {
		t.Fatalf("terminal usage = %#v, want raw prompt tokens", events[len(events)-1].Usage)
	}
	if final.Cost == nil || final.Cost.TotalCost == 0 {
		t.Fatalf("final cost = %#v, want populated cost", final.Cost)
	}
	if final.Cost.ProviderReportedCost == nil || *final.Cost.ProviderReportedCost != 0.125 {
		t.Fatalf("provider reported cost = %#v, want 0.125", final.Cost.ProviderReportedCost)
	}
	if got, want := final.Cost.ProviderReportedCurrency, "USD"; got != want {
		t.Fatalf("provider reported currency = %q, want %q", got, want)
	}
}

func TestProviderErrorResponseEndsStreamWithProviderError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_123")
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-error-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Diagnostics[0].StatusCode, http.StatusTooManyRequests; got != want {
		t.Fatalf("diagnostic status = %d, want %d", got, want)
	}
	if got, want := sigma.ClassifyError(err).Class, sigma.ErrorClassRateLimited; got != want {
		t.Fatalf("class = %q, want %q", got, want)
	}
}

func TestStreamErrorEventIsTypedProviderError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"error":{"type":"server_error","message":"overloaded"}}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-stream-error-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	classification := sigma.ClassifyError(err)
	if got, want := classification.Class, sigma.ErrorClassTransient; got != want {
		t.Fatalf("class = %q, want %q", got, want)
	}
	if got, want := classification.ProviderCode, "server_error"; got != want {
		t.Fatalf("provider code = %q, want %q", got, want)
	}
}

func TestStreamFinishReasonErrorsAreProviderErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		reason    string
		wantCause error
		wantClass sigma.ErrorClass
	}{
		{
			name:      "network error",
			reason:    "network_error",
			wantCause: sigma.ErrProviderResponse,
			wantClass: sigma.ErrorClassTransient,
		},
		{
			name:      "context overflow",
			reason:    "model_context_window_exceeded",
			wantCause: sigma.ErrContextOverflow,
			wantClass: sigma.ErrorClassContextOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"`+tt.reason+`"}]}`+"\n\n")
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("openai-finish-error-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := openAITestModel(providerID)
			client := openAITestClient(t, providerID, model, server.URL)

			final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
			if err == nil {
				t.Fatal("Complete returned nil error")
			}
			if !errors.Is(err, tt.wantCause) {
				t.Fatalf("error = %v, want cause %v", err, tt.wantCause)
			}
			if got, want := final.StopReason, sigma.StopReasonError; got != want {
				t.Fatalf("stop reason = %q, want %q", got, want)
			}
			if got := sigma.ClassifyError(err).Class; got != tt.wantClass {
				t.Fatalf("class = %q, want %q", got, tt.wantClass)
			}
		})
	}
}

func TestCancellationBeforeRequestSendDoesNotReachServer(t *testing.T) {
	t.Parallel()

	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests <- struct{}{}
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-cancel-before-send-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stream := client.Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	if !errors.Is(err, sigma.ErrAborted) {
		t.Fatalf("Collect error = %v, want ErrAborted", err)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	select {
	case <-requests:
		t.Fatal("server received request after context was canceled")
	default:
	}
}

func TestCancellationDuringHTTPRequest(t *testing.T) {
	t.Parallel()

	requested := make(chan struct{})
	unblock := make(chan struct{})
	var unblockOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(requested)
		select {
		case <-r.Context().Done():
		case <-unblock:
		}
	}))
	t.Cleanup(func() {
		unblockOnce.Do(func() { close(unblock) })
	})
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-cancel-during-request-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	stream := client.Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	receiveSignal(t, requested)
	cancel()
	unblockOnce.Do(func() { close(unblock) })

	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	if !errors.Is(err, sigma.ErrAborted) {
		t.Fatalf("Collect error = %v, want ErrAborted", err)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
}

func TestCancellationAbortsStreamingRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_cancel","model":"gpt-test","choices":[{"index":0,"delta":{"content":"partial "},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_cancel","model":"gpt-test","choices":[{"index":0,"delta":{"reasoning_content":"plan "},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_cancel","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_partial","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Melbourne\"}"}}]},"finish_reason":null}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-cancel-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	stream := client.Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	for {
		event := receiveEvent(t, stream)
		if event.Kind == sigma.EventKindToolCallDelta {
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
	if got, want := final.Content[0].Text, "partial "; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
	if got, want := final.Content[1].ThinkingText, "plan "; got != want {
		t.Fatalf("partial thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[2].ToolCallID, "call_partial"; got != want {
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
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_close","model":"gpt-test","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(requestCanceled)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("openai-close-test")
	model := openAITestModel(providerID)
	client := openAITestClient(t, providerID, model, server.URL)

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

func TestChatCompletionsProviderReasoningFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		format   sigma.OpenAICompletionsReasoningFormat
		level    sigma.ThinkingLevel
		tool     bool
		assert   func(t *testing.T, body map[string]any)
		thinking map[sigma.ThinkingLevel]string
	}{
		{
			name:   "together toggles reasoning and sends effort",
			format: sigma.OpenAICompletionsReasoningTogether,
			level:  sigma.ThinkingLevelHigh,
			assert: func(t *testing.T, body map[string]any) {
				t.Helper()
				reasoning, ok := body["reasoning"].(map[string]any)
				if !ok || reasoning["enabled"] != true {
					t.Fatalf("reasoning = %#v, want enabled true", body["reasoning"])
				}
				if got, want := body["reasoning_effort"], "provider-high"; got != want {
					t.Fatalf("reasoning_effort = %#v, want %q", got, want)
				}
			},
			thinking: map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "provider-high"},
		},
		{
			name:   "zai sends enabled thinking object and tool stream",
			format: sigma.OpenAICompletionsReasoningZAI,
			level:  sigma.ThinkingLevelHigh,
			tool:   true,
			assert: func(t *testing.T, body map[string]any) {
				t.Helper()
				thinking, ok := body["thinking"].(map[string]any)
				if !ok || thinking["type"] != "enabled" {
					t.Fatalf("thinking = %#v, want enabled type", body["thinking"])
				}
				if _, ok := body["enable_thinking"]; ok {
					t.Fatalf("enable_thinking = %#v, want absent", body["enable_thinking"])
				}
				if got := body["tool_stream"]; got != true {
					t.Fatalf("tool_stream = %#v, want true", got)
				}
			},
		},
		{
			name:   "zai disables thinking when no level is requested",
			format: sigma.OpenAICompletionsReasoningZAI,
			assert: func(t *testing.T, body map[string]any) {
				t.Helper()
				thinking, ok := body["thinking"].(map[string]any)
				if !ok || thinking["type"] != "disabled" {
					t.Fatalf("thinking = %#v, want disabled type", body["thinking"])
				}
				if _, ok := body["enable_thinking"]; ok {
					t.Fatalf("enable_thinking = %#v, want absent", body["enable_thinking"])
				}
				if _, ok := body["reasoning_effort"]; ok {
					t.Fatalf("reasoning_effort = %#v, want absent", body["reasoning_effort"])
				}
			},
		},
		{
			name:   "zai glm 5.2 sends mapped high reasoning effort",
			format: sigma.OpenAICompletionsReasoningZAI,
			level:  sigma.ThinkingLevelHigh,
			assert: func(t *testing.T, body map[string]any) {
				t.Helper()
				thinking, ok := body["thinking"].(map[string]any)
				if !ok || thinking["type"] != "enabled" {
					t.Fatalf("thinking = %#v, want enabled type", body["thinking"])
				}
				if got, want := body["reasoning_effort"], "high"; got != want {
					t.Fatalf("reasoning_effort = %#v, want %q", got, want)
				}
			},
			thinking: map[sigma.ThinkingLevel]string{
				sigma.ThinkingLevelMinimal: "",
				sigma.ThinkingLevelLow:     "high",
				sigma.ThinkingLevelMedium:  "high",
				sigma.ThinkingLevelHigh:    "high",
				sigma.ThinkingLevelXHigh:   "max",
			},
		},
		{
			name:   "zai glm 5.2 sends mapped xhigh reasoning effort",
			format: sigma.OpenAICompletionsReasoningZAI,
			level:  sigma.ThinkingLevelXHigh,
			assert: func(t *testing.T, body map[string]any) {
				t.Helper()
				thinking, ok := body["thinking"].(map[string]any)
				if !ok || thinking["type"] != "enabled" {
					t.Fatalf("thinking = %#v, want enabled type", body["thinking"])
				}
				if got, want := body["reasoning_effort"], "max"; got != want {
					t.Fatalf("reasoning_effort = %#v, want %q", got, want)
				}
			},
			thinking: map[sigma.ThinkingLevel]string{
				sigma.ThinkingLevelMinimal: "",
				sigma.ThinkingLevelLow:     "high",
				sigma.ThinkingLevelMedium:  "high",
				sigma.ThinkingLevelHigh:    "high",
				sigma.ThinkingLevelXHigh:   "max",
			},
		},
		{
			name:   "zai glm 5.2 minimal enables thinking without reasoning effort",
			format: sigma.OpenAICompletionsReasoningZAI,
			level:  sigma.ThinkingLevelMinimal,
			assert: func(t *testing.T, body map[string]any) {
				t.Helper()
				thinking, ok := body["thinking"].(map[string]any)
				if !ok || thinking["type"] != "enabled" {
					t.Fatalf("thinking = %#v, want enabled type", body["thinking"])
				}
				if _, ok := body["reasoning_effort"]; ok {
					t.Fatalf("reasoning_effort = %#v, want absent", body["reasoning_effort"])
				}
			},
			thinking: map[sigma.ThinkingLevel]string{
				sigma.ThinkingLevelMinimal: "",
				sigma.ThinkingLevelLow:     "high",
				sigma.ThinkingLevelMedium:  "high",
				sigma.ThinkingLevelHigh:    "high",
				sigma.ThinkingLevelXHigh:   "max",
			},
		},
		{
			name:   "qwen disables thinking when no level is requested",
			format: sigma.OpenAICompletionsReasoningQwen,
			assert: func(t *testing.T, body map[string]any) {
				t.Helper()
				if got := body["enable_thinking"]; got != false {
					t.Fatalf("enable_thinking = %#v, want false", got)
				}
			},
		},
		{
			name:   "ant ling sends mapped explicit effort",
			format: sigma.OpenAICompletionsReasoningAntLing,
			level:  sigma.ThinkingLevelXHigh,
			assert: func(t *testing.T, body map[string]any) {
				t.Helper()
				reasoning, ok := body["reasoning"].(map[string]any)
				if !ok || reasoning["effort"] != "xhigh" {
					t.Fatalf("reasoning = %#v, want xhigh effort", body["reasoning"])
				}
			},
			thinking: map[sigma.ThinkingLevel]string{sigma.ThinkingLevelXHigh: "xhigh"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeFixture(t, w, "text_usage.sse")
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("reasoning-format-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := openAITestModel(providerID)
			model.OpenAICompletionsCompat.ReasoningFormat = tt.format
			model.OpenAICompletionsCompat.SupportsReasoningEffort = sigma.OpenAICompatSupported
			model.OpenAICompletionsCompat.SupportsToolStream = sigma.OpenAICompatSupported
			if tt.thinking != nil {
				model.ThinkingLevelMap = tt.thinking
			}
			client := openAITestClient(t, providerID, model, server.URL)

			req := sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}
			if tt.tool {
				req.Tools = []sigma.Tool{{Name: "lookup", Description: "Lookup", InputSchema: sigma.Schema{"type": "object"}}}
			}
			opts := []sigma.Option{}
			if tt.level != "" {
				opts = append(opts, sigma.WithReasoningLevel(tt.level))
			}
			if _, err := client.Complete(context.Background(), model, req, opts...); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			var body map[string]any
			if err := json.Unmarshal(receiveRequest(t, requests).Body, &body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			tt.assert(t, body)
		})
	}
}

func openAITestClient(t *testing.T, providerID sigma.ProviderID, model sigma.Model, baseURL string, opts ...openai.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]openai.ProviderOption{openai.WithBaseURL(baseURL)}, opts...)
	if err := registry.RegisterTextProvider(providerID, openai.NewProvider(providerOpts...)); err != nil {
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

func openAITestModel(providerID sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:       "gpt-test",
		Provider: providerID,
		API:      sigma.APIOpenAICompletions,
		SupportedInputs: []sigma.ContentBlockType{
			sigma.ContentBlockText,
			sigma.ContentBlockImage,
		},
		SupportsTools:    true,
		SupportsThinking: true,
		ThinkingLevelMap: map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "high"},
		OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
			SupportsDeveloperRole:   sigma.OpenAICompatSupported,
			ReasoningFormat:         sigma.OpenAICompletionsReasoningEffort,
			SupportsStreamingUsage:  sigma.OpenAICompatSupported,
			MaxTokensField:          sigma.OpenAICompletionsMaxTokens,
			CacheControlFormat:      sigma.OpenAICompletionsCacheControlMessage,
			SupportsSessionAffinity: sigma.OpenAICompatSupported,
		},
		InputCostPerMillion:          1,
		OutputCostPerMillion:         2,
		CacheReadInputCostPerMillion: 0.5,
	}
}

func richRequest() sigma.Request {
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
					sigma.ToolCallBlock("call_prev", "lookup", map[string]any{"query": "weather"}),
				},
			},
			sigma.ToolResult("call_prev", "Sunny"),
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

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

func captureRequest(t *testing.T, ch chan<- capturedRequest, r *http.Request) {
	t.Helper()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("ReadAll request body returned error: %v", err)
	}
	ch <- capturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
		Body:    body,
	}
}

func receiveRequest(t *testing.T, ch <-chan capturedRequest) capturedRequest {
	t.Helper()

	select {
	case request := <-ch:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request")
		return capturedRequest{}
	}
}

func writeFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()

	data, err := os.ReadFile("../../internal/sse/testdata/openai/" + name)
	if err != nil {
		t.Fatalf("ReadFile(%s) returned error: %v", name, err)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = w.Write(data)
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

	select {
	case event, ok := <-stream.Events():
		if !ok {
			t.Fatal("stream closed before event")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return sigma.Event{}
	}
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

func assertHeader(t *testing.T, headers http.Header, key, value string) {
	t.Helper()

	if got := headers.Get(key); got != value {
		t.Fatalf("header %q = %q, want %q", key, got, value)
	}
}

func assertHeaderAbsent(t *testing.T, headers http.Header, key string) {
	t.Helper()

	if got := headers.Get(key); got != "" {
		t.Fatalf("header %q = %q, want absent", key, got)
	}
}

func invalidProviderText() string {
	return string([]byte{0xff}) + "clean"
}

func assertPayloadText(t *testing.T, value any, want string) {
	t.Helper()

	got, ok := value.(string)
	if !ok {
		t.Fatalf("payload text type = %T, want string", value)
	}
	if got != want {
		t.Fatalf("payload text = %q, want %q", got, want)
	}
}

func assertResponsesInputText(t *testing.T, item map[string]any, want string) {
	t.Helper()

	content, ok := item["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("responses content = %#v, want content parts", item["content"])
	}
	part, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("responses content part type = %T, want map", content[0])
	}
	assertPayloadText(t, part["text"], want)
}

func assertResponsesOutputText(t *testing.T, item map[string]any, want string) {
	t.Helper()
	assertResponsesInputText(t, item, want)
}

func assertResponsesReasoningText(t *testing.T, item map[string]any, want string) {
	t.Helper()

	summary, ok := item["summary"].([]any)
	if !ok || len(summary) == 0 {
		t.Fatalf("responses summary = %#v, want summary parts", item["summary"])
	}
	part, ok := summary[0].(map[string]any)
	if !ok {
		t.Fatalf("responses summary part type = %T, want map", summary[0])
	}
	assertPayloadText(t, part["text"], want)
}

func assertResponsesToolOutputText(t *testing.T, item map[string]any, want string) {
	t.Helper()
	assertPayloadText(t, item["output"], want)
}
