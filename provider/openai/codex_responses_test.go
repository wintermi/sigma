// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
	"github.com/wintermi/sigma/provider/openai"
)

func TestRegisterCodexResponsesReportsCodexResponsesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("openai-codex-responses")
	if err := openai.RegisterCodexResponses(registry, providerID); err != nil {
		t.Fatalf("RegisterCodexResponses returned error: %v", err)
	}
	if err := registry.RegisterModel(codexResponsesTestModel(providerID)); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIOpenAICodexResponses; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestCodexResponsesInjectsBearerTokenAndUsesCodexModelName(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-auth-test")
	model := codexResponsesTestModel(providerID)
	model.OpenAICodexResponses.Model = "codex-mini-latest"
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{
			TextVerbosity:        "low",
			PromptCacheRetention: "24h",
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.ProviderMetadata["id"], "resp_complete"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer codex-oauth-token")
	assertHeader(t, request.Headers, "chatgpt-account-id", "acct_codex")
	assertHeader(t, request.Headers, "OpenAI-Beta", "responses=experimental")
	assertHeader(t, request.Headers, "originator", "sigma")
	assertHeader(t, request.Headers, "X-Client", "client")
	goldentest.AssertJSON(t, request.Body, "provider/openai/codex_responses/basic_payload.json")
}

func TestCodexResponsesPreservesSystemPromptInstructionsAndForcesStoreFalse(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-instructions-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			SystemPrompt: "Use concise replies.",
			Messages:     []sigma.Message{sigma.UserText("hi")},
		},
		sigma.WithProviderOption(providerID, "store", true),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	var payload map[string]any
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := payload["instructions"], "Use concise replies."; got != want {
		t.Fatalf("instructions = %v, want %q", got, want)
	}
	if got, want := payload["store"], false; got != want {
		t.Fatalf("store = %v, want %v", got, want)
	}
}

func TestCodexResponsesOmitsUnsupportedMaxOutputTokens(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-max-tokens-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithMaxTokens(128),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	var payload map[string]any
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if _, ok := payload["max_output_tokens"]; ok {
		t.Fatalf("max_output_tokens was sent in Codex payload: %#v", payload)
	}
}

func TestCodexResponsesDerivesPromptCacheKey(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-cache-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))
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

	request := receiveRequest(t, requests)
	var payload map[string]any
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := payload["prompt_cache_key"], strings.Repeat("x", 64); got != want {
		t.Fatalf("prompt_cache_key = %v, want %q", got, want)
	}
	if got, want := payload["prompt_cache_retention"], "24h"; got != want {
		t.Fatalf("prompt_cache_retention = %v, want %q", got, want)
	}
	if _, ok := payload["previous_response_id"]; ok {
		t.Fatalf("previous_response_id was sent in Codex payload: %#v", payload)
	}
	assertHeader(t, request.Headers, "session-id", sessionID)
	assertHeader(t, request.Headers, "x-client-request-id", sessionID)
}

func TestCodexResponsesMissingOAuthProviderFailsBeforeNetwork(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-missing-oauth-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, nil)

	_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if !errors.Is(err, sigma.ErrCredentialUnavailable) {
		t.Fatalf("error = %v, want ErrCredentialUnavailable", err)
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want 0", calls)
	}
}

func TestCodexResponsesMissingAccountIDFailsBeforeNetwork(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-missing-account-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, sigma.OAuthTokenProviderFunc(
		func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{
				Type:  sigma.CredentialTypeOAuthToken,
				Value: "not-a-jwt",
			}, nil
		},
	))

	_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !strings.Contains(err.Error(), "account id") {
		t.Fatalf("error = %q, want account id context", err.Error())
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want 0", calls)
	}
}

func TestCodexResponsesUnsupportedHTTPTransportFailsBeforeNetwork(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-transport-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithTransport(sigma.TransportHTTP),
	)
	if !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
	if !strings.Contains(err.Error(), "http") {
		t.Fatalf("error = %q, want http context", err.Error())
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want 0", calls)
	}
}

func TestCodexResponsesWebSocketStreamsAndSendsRequest(t *testing.T) {
	openai.CloseCodexResponsesWebSocketSessions()
	t.Cleanup(openai.CloseCodexResponsesWebSocketSessions)

	requests := make(chan codexWebSocketTestRequest, 1)
	server := newCodexWebSocketTestServer(t, func(req *http.Request, ws *codexWebSocketTestConn) {
		body := ws.readJSON(t)
		requests <- codexWebSocketTestRequest{Path: req.URL.Path, Headers: req.Header.Clone(), Body: body}
		ws.writeJSON(t, map[string]any{
			"type":         "response.output_item.added",
			"response_id":  "ws_resp",
			"model":        "codex-provider-model",
			"output_index": 0,
			"item": map[string]any{
				"type":    "message",
				"id":      "msg_ws",
				"role":    "assistant",
				"content": []any{},
			},
		})
		ws.writeJSON(t, map[string]any{
			"type":         "response.output_text.delta",
			"response_id":  "ws_resp",
			"item_id":      "msg_ws",
			"output_index": 0,
			"delta":        "Hello",
		})
		ws.writeJSON(t, map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":     "ws_resp",
				"model":  "codex-provider-model",
				"status": "completed",
				"output": []any{map[string]any{
					"type": "message",
					"id":   "msg_ws",
					"role": "assistant",
					"content": []any{map[string]any{
						"type": "output_text",
						"id":   "txt_ws",
						"text": "Hello",
					}},
				}},
				"usage": map[string]any{
					"input_tokens":  6,
					"output_tokens": 2,
					"total_tokens":  8,
					"input_tokens_details": map[string]any{
						"cached_tokens": 4,
					},
				},
			},
		})
	})
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-ws-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithTransport(sigma.TransportWebSocket),
		sigma.WithSessionID("ws-session"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "Hello"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if final.Usage == nil {
		t.Fatal("usage was nil")
	}
	if got, want := final.Usage.InputTokens, 2; got != want {
		t.Fatalf("input tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.CacheReadInputTokens, 4; got != want {
		t.Fatalf("cache read tokens = %d, want %d", got, want)
	}

	request := receiveCodexWebSocketRequest(t, requests)
	if got, want := request.Path, "/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer codex-oauth-token")
	assertHeader(t, request.Headers, "chatgpt-account-id", "acct_codex")
	assertHeader(t, request.Headers, "OpenAI-Beta", "responses_websockets=2026-02-06")
	assertHeader(t, request.Headers, "session-id", "ws-session")
	assertHeader(t, request.Headers, "x-client-request-id", "ws-session")
	if got, want := request.Body["type"], "response.create"; got != want {
		t.Fatalf("request type = %v, want %v", got, want)
	}
	if got, want := request.Body["tool_choice"], "auto"; got != want {
		t.Fatalf("tool choice = %v, want %v", got, want)
	}
	if got, want := request.Body["parallel_tool_calls"], true; got != want {
		t.Fatalf("parallel tool calls = %v, want %v", got, want)
	}
}

func TestCodexResponsesWebSocketSessionCacheSendsInputDelta(t *testing.T) {
	openai.CloseCodexResponsesWebSocketSessions()
	t.Cleanup(openai.CloseCodexResponsesWebSocketSessions)

	requests := make(chan map[string]any, 2)
	server := newCodexWebSocketTestServer(t, func(_ *http.Request, ws *codexWebSocketTestConn) {
		first := ws.readJSON(t)
		requests <- first
		writeCodexWebSocketTextResponse(t, ws, "resp_1", "msg_1", "txt_1", "First")
		second := ws.readJSON(t)
		requests <- second
		writeCodexWebSocketTextResponse(t, ws, "resp_2", "msg_2", "txt_2", "Second")
	})
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-ws-cache-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))
	sessionID := "cache-session"

	first, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("first")}},
		sigma.WithTransport(sigma.TransportWebSocket),
		sigma.WithSessionID(sessionID),
	)
	if err != nil {
		t.Fatalf("first Complete returned error: %v", err)
	}
	assistant := sigma.Message{
		Role:       sigma.RoleAssistant,
		Content:    first.Content,
		Provider:   first.Provider,
		Model:      first.Model,
		StopReason: first.StopReason,
	}
	_, err = client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{
			sigma.UserText("first"),
			assistant,
			sigma.UserText("second"),
		}},
		sigma.WithTransport(sigma.TransportWebSocket),
		sigma.WithSessionID(sessionID),
	)
	if err != nil {
		t.Fatalf("second Complete returned error: %v", err)
	}

	_ = receiveMap(t, requests)
	second := receiveMap(t, requests)
	if got, want := second["previous_response_id"], "resp_1"; got != want {
		t.Fatalf("previous response id = %v, want %v", got, want)
	}
	input, ok := second["input"].([]any)
	if !ok {
		t.Fatalf("second input type = %T, want []any", second["input"])
	}
	if len(input) != 1 {
		t.Fatalf("second input length = %d, want delta of 1", len(input))
	}
	item := input[0].(map[string]any)
	if got, want := item["role"], "user"; got != want {
		t.Fatalf("delta role = %v, want %v", got, want)
	}
}

func TestCodexResponsesWebSocketFallsBackToSSEBeforeStart(t *testing.T) {
	openai.CloseCodexResponsesWebSocketSessions()
	t.Cleanup(openai.CloseCodexResponsesWebSocketSessions)

	var postCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("response writer cannot hijack")
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Fatalf("Hijack returned error: %v", err)
			}
			_ = conn.Close()
			return
		}
		postCalls++
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-ws-fallback-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithTransport(sigma.TransportWebSocket),
		sigma.WithSessionID("fallback-session"),
	)
	if err != nil {
		t.Fatalf("Complete returned error after fallback: %v", err)
	}
	if got, want := postCalls, 1; got != want {
		t.Fatalf("fallback POST calls = %d, want %d", got, want)
	}
}

func TestCodexResponsesWebSocketErrorsAfterStreamStart(t *testing.T) {
	openai.CloseCodexResponsesWebSocketSessions()
	t.Cleanup(openai.CloseCodexResponsesWebSocketSessions)

	server := newCodexWebSocketTestServer(t, func(_ *http.Request, ws *codexWebSocketTestConn) {
		_ = ws.readJSON(t)
		ws.writeJSON(t, map[string]any{
			"type":         "response.output_text.delta",
			"response_id":  "partial_resp",
			"item_id":      "msg_partial",
			"output_index": 0,
			"delta":        "partial",
		})
		ws.Close()
	})
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-ws-error-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithTransport(sigma.TransportWebSocket),
		sigma.WithSessionID("error-session"),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if len(final.Content) == 0 || final.Content[0].Text != "partial" {
		t.Fatalf("final content = %#v, want partial text", final.Content)
	}
}

func TestCodexResponsesWebSocketCancellationClosesCachedSession(t *testing.T) {
	openai.CloseCodexResponsesWebSocketSessions()
	t.Cleanup(openai.CloseCodexResponsesWebSocketSessions)

	requests := make(chan map[string]any, 2)
	server := newCodexWebSocketTestServer(t, func(_ *http.Request, ws *codexWebSocketTestConn) {
		requests <- ws.readJSON(t)
		time.Sleep(200 * time.Millisecond)
	})
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-ws-cancel-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.Complete(
		ctx,
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithTransport(sigma.TransportWebSocket),
		sigma.WithSessionID("cancel-session"),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	openai.CloseCodexResponsesWebSocketSession("cancel-session")
}

func TestCodexResponsesTextStreamingMapsMetadataAndUsage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.output_item.added","response_id":"codex_resp","model":"codex-provider-model","output_index":0,"item":{"type":"message","id":"msg_codex","role":"assistant","content":[]}}

data: {"type":"response.output_text.delta","response_id":"codex_resp","model":"codex-provider-model","item_id":"msg_codex","output_index":0,"content_index":0,"delta":"Codex"}

data: {"type":"response.output_text.delta","response_id":"codex_resp","model":"codex-provider-model","item_id":"msg_codex","output_index":0,"content_index":0,"delta":" ready"}

data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","id":"msg_codex","role":"assistant","content":[{"type":"output_text","id":"txt_codex","text":"Codex ready"}]}],"usage":{"input_tokens":4,"output_tokens":3,"total_tokens":7}}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-stream-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))

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
		sigma.EventKindTextDelta,
		sigma.EventKindTextEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.Content[0].Text, "Codex ready"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := final.ProviderMetadata["id"], "codex_resp"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}
	if got, want := final.ProviderMetadata["model"], "codex-provider-model"; got != want {
		t.Fatalf("provider model = %v, want %v", got, want)
	}
	if final.Usage == nil {
		t.Fatal("final usage was nil")
	}
	if got, want := final.Usage.TotalTokens, 7; got != want {
		t.Fatalf("total tokens = %d, want %d", got, want)
	}
}

func TestCodexResponsesToolCallStreaming(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.output_item.added","response_id":"codex_tool","output_index":0,"item":{"type":"function_call","id":"fc_codex","call_id":"call_codex","name":"shell","arguments":""}}

data: {"type":"response.function_call_arguments.delta","response_id":"codex_tool","item_id":"fc_codex","output_index":0,"delta":"{\"cmd\""}

data: {"type":"response.function_call_arguments.delta","response_id":"codex_tool","item_id":"fc_codex","output_index":0,"delta":":\"go test\"}"}

data: {"type":"response.output_item.done","response_id":"codex_tool","output_index":0,"item":{"type":"function_call","id":"fc_codex","call_id":"call_codex","name":"shell","arguments":"{\"cmd\":\"go test\"}"}}

data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","id":"fc_codex","call_id":"call_codex","name":"shell","arguments":"{\"cmd\":\"go test\"}"}]}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-tool-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("run tests")}})
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
	if got, want := final.Content[0].ToolCallID, "call_codex"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderMetadata["id"], "fc_codex"; got != want {
		t.Fatalf("tool item id = %v, want %v", got, want)
	}
	args := final.Content[0].ToolArguments.(map[string]any)
	if got, want := args["cmd"], "go test"; got != want {
		t.Fatalf("tool cmd = %v, want %v", got, want)
	}
}

func TestCodexResponsesProviderErrorUsesCodexAPIAndRedacts(t *testing.T) {
	t.Parallel()

	const token = "codex-oauth-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_codex")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad token","access_token":"`+token+`"}}`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-error-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider(token))

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.Diagnostics[0].API, sigma.APIOpenAICodexResponses; got != want {
		t.Fatalf("diagnostic API = %q, want %q", got, want)
	}
	if errorsContains(err, token) || strings.Contains(final.Diagnostics[0].BodyPreview, token) {
		t.Fatalf("provider error leaked token: err=%v diagnostic=%+v", err, final.Diagnostics[0])
	}
}

func TestCodexResponsesTokenProviderErrorIsRedacted(t *testing.T) {
	t.Parallel()

	const token = "codex-oauth-secret"
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("server should not be called")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("codex-responses-token-error-test")
	model := codexResponsesTestModel(providerID)
	client := codexResponsesTestClient(t, providerID, model, server.URL, sigma.OAuthTokenProviderFunc(
		func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{}, fmt.Errorf("refresh failed for %s", token)
		},
	))

	_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if errorsContains(err, token) {
		t.Fatalf("token provider error leaked token: %v", err)
	}
}

func codexResponsesTestClient(t *testing.T, providerID sigma.ProviderID, model sigma.Model, baseURL string, tokenProvider sigma.OAuthTokenProvider) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(providerID, openai.NewCodexResponsesProvider(openai.WithBaseURL(baseURL))); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	var defaults []sigma.Option
	if tokenProvider != nil {
		defaults = append(defaults, openai.WithCodexResponsesOAuthTokenProvider(providerID, tokenProvider))
	}
	return sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithDefaultOptions(defaults...),
		sigma.WithDefaultHeader("X-Client", "client"),
	)
}

func codexResponsesTestModel(providerID sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:       "codex-test",
		Provider: providerID,
		API:      sigma.APIOpenAICodexResponses,
		SupportedInputs: []sigma.ContentBlockType{
			sigma.ContentBlockText,
			sigma.ContentBlockImage,
		},
		SupportsTools:                true,
		SupportsThinking:             true,
		DefaultTransport:             sigma.TransportSSE,
		OpenAICodexResponses:         &sigma.OpenAICodexResponsesConfig{},
		InputCostPerMillion:          1,
		OutputCostPerMillion:         2,
		ProviderMetadata:             map[string]any{"requiresOAuth": true},
		ThinkingLevelMap:             map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "high"},
		MaxOutputTokens:              8192,
		CacheReadInputCostPerMillion: 0.5,
	}
}

func codexTokenProvider(token string) sigma.OAuthTokenProvider {
	return sigma.OAuthTokenProviderFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{
			Type:  sigma.CredentialTypeOAuthToken,
			Value: token,
			Metadata: map[string]any{
				"accountID": "acct_codex",
			},
		}, nil
	})
}

type codexWebSocketTestRequest struct {
	Path    string
	Headers http.Header
	Body    map[string]any
}

type codexWebSocketTestServer struct {
	URL string
	ln  net.Listener
}

type codexWebSocketTestConn struct {
	conn   net.Conn
	reader *bufio.Reader
}

func newCodexWebSocketTestServer(t *testing.T, handler func(*http.Request, *codexWebSocketTestConn)) *codexWebSocketTestServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	server := &codexWebSocketTestServer{
		URL: "http://" + ln.Addr().String(),
		ln:  ln,
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleCodexWebSocketTestConn(t, conn, handler)
		}
	}()
	return server
}

func (s *codexWebSocketTestServer) Close() {
	_ = s.ln.Close()
}

func handleCodexWebSocketTestConn(t *testing.T, conn net.Conn, handler func(*http.Request, *codexWebSocketTestConn)) {
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		t.Errorf("ReadRequest returned error: %v", err)
		_ = conn.Close()
		return
	}
	defer req.Body.Close()
	key := req.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		t.Errorf("missing Sec-WebSocket-Key")
		_ = conn.Close()
		return
	}
	accept := codexWebSocketTestAccept(key)
	_, _ = fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	handler(req, &codexWebSocketTestConn{conn: conn, reader: reader})
	_ = conn.Close()
}

func (c *codexWebSocketTestConn) readJSON(t *testing.T) map[string]any {
	t.Helper()

	data, err := readCodexWebSocketClientText(c.reader)
	if err != nil {
		t.Fatalf("read websocket text returned error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(data), &body); err != nil {
		t.Fatalf("Unmarshal websocket body returned error: %v", err)
	}
	return body
}

func (c *codexWebSocketTestConn) writeJSON(t *testing.T, value map[string]any) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal websocket event returned error: %v", err)
	}
	if err := writeCodexWebSocketServerText(c.conn, string(data)); err != nil {
		t.Fatalf("write websocket text returned error: %v", err)
	}
}

func (c *codexWebSocketTestConn) Close() {
	_ = c.conn.Close()
}

func readCodexWebSocketClientText(reader *bufio.Reader) (string, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", err
	}
	opcode := header[0] & 0x0f
	if opcode != 0x1 {
		return "", fmt.Errorf("opcode = %d, want text", opcode)
	}
	masked := header[1]&0x80 != 0
	if !masked {
		return "", fmt.Errorf("client frame was not masked")
	}
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var extended uint16
		if err := binary.Read(reader, binary.BigEndian, &extended); err != nil {
			return "", err
		}
		length = uint64(extended)
	case 127:
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			return "", err
		}
	}
	var mask [4]byte
	if _, err := io.ReadFull(reader, mask[:]); err != nil {
		return "", err
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return "", err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return string(payload), nil
}

func writeCodexWebSocketTextResponse(t *testing.T, ws *codexWebSocketTestConn, responseID string, messageID string, textID string, text string) {
	t.Helper()

	ws.writeJSON(t, map[string]any{
		"type":         "response.output_item.added",
		"response_id":  responseID,
		"output_index": 0,
		"item": map[string]any{
			"type":    "message",
			"id":      messageID,
			"role":    "assistant",
			"content": []any{},
		},
	})
	ws.writeJSON(t, map[string]any{
		"type":         "response.output_text.delta",
		"response_id":  responseID,
		"item_id":      messageID,
		"output_index": 0,
		"delta":        text,
	})
	ws.writeJSON(t, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     responseID,
			"status": "completed",
			"output": []any{map[string]any{
				"type": "message",
				"id":   messageID,
				"role": "assistant",
				"content": []any{map[string]any{
					"type": "output_text",
					"id":   textID,
					"text": text,
				}},
			}},
		},
	})
}

func writeCodexWebSocketServerText(conn net.Conn, text string) error {
	payload := []byte(text)
	var frame bytes.Buffer
	frame.WriteByte(0x81)
	switch length := len(payload); {
	case length < 126:
		frame.WriteByte(byte(length))
	case length <= 0xffff:
		frame.WriteByte(126)
		_ = binary.Write(&frame, binary.BigEndian, uint16(length))
	default:
		frame.WriteByte(127)
		_ = binary.Write(&frame, binary.BigEndian, uint64(length))
	}
	frame.Write(payload)
	_, err := conn.Write(frame.Bytes())
	return err
}

func codexWebSocketTestAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func receiveCodexWebSocketRequest(t *testing.T, requests <-chan codexWebSocketTestRequest) codexWebSocketTestRequest {
	t.Helper()

	select {
	case request := <-requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket request")
		return codexWebSocketTestRequest{}
	}
}

func receiveMap(t *testing.T, requests <-chan map[string]any) map[string]any {
	t.Helper()

	select {
	case request := <-requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for map request")
		return nil
	}
}
