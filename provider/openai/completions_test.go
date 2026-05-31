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
