// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package fireworks_test

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
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/fireworks"
)

func TestRegisterReportsOpenAICompletionsAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := fireworks.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(fireworksTestModel()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestCompleteStreamsTextWithFireworksDefaults(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	model := fireworksTestModel()
	client := fireworksTestClient(t, model, server.URL, fireworks.WithHeader("X-Provider", "provider"))
	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("hi")},
			Tools: []sigma.Tool{{
				Name:             "weather",
				Description:      "Get weather",
				InputSchema:      sigma.Schema{"type": "object"},
				ProviderMetadata: map[string]any{"strict": true},
			}},
		},
		sigma.WithAPIKey("request-key"),
		sigma.WithHeader("X-Custom", "custom"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "Hello world"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
	if final.Usage == nil || final.Usage.InputTokens != 5 || final.Usage.OutputTokens != 5 ||
		final.Usage.CacheReadInputTokens != 3 || final.Usage.CacheWriteInputTokens != 2 {
		t.Fatalf("final usage = %#v, want input/output usage", final.Usage)
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
	assertHeader(t, request.Headers, "Authorization", "Bearer request-key")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	assertJSONPath(t, request.Body, []string{"stream_options", "include_usage"}, true)
	assertJSONPath(t, request.Body, []string{"tools", "0", "function", "strict"}, true)
}

func TestToolCallStreamingProducesFinalArguments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeFixture(t, w, "tool_call.sse")
	}))
	t.Cleanup(server.Close)

	model := fireworksTestModel()
	client := fireworksTestClient(t, model, server.URL)
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
	args, ok := final.Content[0].ToolArguments.(map[string]any)
	if !ok {
		t.Fatalf("tool arguments type = %T, want map", final.Content[0].ToolArguments)
	}
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("tool city = %v, want %v", got, want)
	}
}

func TestReasoningControlsAndStreamedThinking(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_reasoning","model":"accounts/fireworks/routers/kimi-k2p6-turbo","choices":[{"index":0,"delta":{"reasoning_content":"thinking "},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_reasoning","model":"accounts/fireworks/routers/kimi-k2p6-turbo","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_reasoning","model":"accounts/fireworks/routers/kimi-k2p6-turbo","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7,"completion_tokens_details":{"reasoning_tokens":2}}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	model := fireworksTestModel()
	client := fireworksTestClient(t, model, server.URL)
	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("think")}},
		sigma.WithReasoningLevel(sigma.ThinkingLevelMedium),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].ThinkingText, "thinking "; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "answer"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if final.Usage == nil || final.Usage.ThinkingTokens != 2 {
		t.Fatalf("thinking usage = %#v, want 2 tokens", final.Usage)
	}
	assertJSONPath(t, receiveRequest(t, requests).Body, []string{"reasoning_effort"}, "medium")

	_, err = client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("think with budget")}},
		sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
		sigma.WithThinkingBudgetTokens(4096),
	)
	if err != nil {
		t.Fatalf("Complete with budget returned error: %v", err)
	}
	request := receiveRequest(t, requests)
	assertJSONPath(t, request.Body, []string{"thinking", "type"}, "enabled")
	assertJSONPath(t, request.Body, []string{"thinking", "budget_tokens"}, float64(4096))
	assertNoJSONPath(t, request.Body, []string{"reasoning_effort"})
}

func TestRejectsTooSmallThinkingBudget(t *testing.T) {
	t.Parallel()

	model := fireworksTestModel()
	client := fireworksTestClient(t, model, "http://127.0.0.1:1")
	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("think")}},
		sigma.WithThinkingBudgetTokens(512),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !strings.Contains(err.Error(), "fireworks thinking budget tokens must be at least 1024") {
		t.Fatalf("error = %v, want local Fireworks budget validation", err)
	}
}

func TestProviderErrorIsTypedAndRedactsRequestAPIKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad request"}}`)
	}))
	t.Cleanup(server.Close)

	model := fireworksTestModel()
	client := fireworksTestClient(t, model, server.URL)
	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("fireworks-secret-key"),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if strings.Contains(err.Error(), "fireworks-secret-key") {
		t.Fatalf("error leaked request API key: %v", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
}

func fireworksTestClient(t *testing.T, model sigma.Model, baseURL string, opts ...fireworks.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]fireworks.ProviderOption{fireworks.WithBaseURL(baseURL)}, opts...)
	if err := registry.RegisterTextProvider(sigma.ProviderFireworks, fireworks.NewProvider(providerOpts...)); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-key"}, nil
	})
	return sigma.NewClient(sigma.WithRegistry(registry), sigma.WithAuthResolver(resolver))
}

func fireworksTestModel() sigma.Model {
	return sigma.Model{
		ID:                   "accounts/fireworks/routers/kimi-k2p6-turbo",
		Provider:             sigma.ProviderFireworks,
		API:                  sigma.APIOpenAICompletions,
		SupportedInputs:      []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:        true,
		SupportsThinking:     true,
		ThinkingLevels:       []sigma.ThinkingLevel{sigma.ThinkingLevelLow, sigma.ThinkingLevelMedium, sigma.ThinkingLevelHigh},
		InputCostPerMillion:  1,
		OutputCostPerMillion: 2,
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

func assertJSONPath(t *testing.T, data []byte, path []string, want any) {
	t.Helper()

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	got := pathValue(t, value, path)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON path %s = %#v, want %#v", strings.Join(path, "."), got, want)
	}
}

func pathValue(t *testing.T, value any, path []string) any {
	t.Helper()

	current := value
	for _, segment := range path {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[segment]
		case []any:
			if segment != "0" {
				t.Fatalf("unsupported array path segment %q", segment)
			}
			if len(typed) == 0 {
				t.Fatalf("empty array at segment %q", segment)
			}
			current = typed[0]
		default:
			t.Fatalf("JSON path %s reached %T", strings.Join(path, "."), current)
		}
	}
	return current
}

func assertNoJSONPath(t *testing.T, data []byte, path []string) {
	t.Helper()

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	current := value
	for _, segment := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return
		}
		next, ok := object[segment]
		if !ok {
			return
		}
		current = next
	}
	t.Fatalf("JSON path %s was present", strings.Join(path, "."))
}
