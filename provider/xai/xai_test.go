// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package xai_test

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
	"github.com/wintermi/sigma/provider/xai"
)

func TestRegisterReportsOpenAICompletionsAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := xai.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(xaiTestModel()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestCompleteStreamsTextWithXAIDefaults(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_xai","model":"grok-3","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_xai","model":"grok-3","choices":[{"index":0,"delta":{"content":" Grok"},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_xai","model":"grok-3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	model := xaiTestModel()
	client := xaiTestClient(t, model, server.URL, xai.WithHeader("X-Provider", "provider"))
	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
		sigma.WithHeader("X-Custom", "custom"),
		sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "Hello Grok"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
	if final.Usage == nil || final.Usage.InputTokens != 7 || final.Usage.OutputTokens != 3 {
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
	assertNoJSONPath(t, request.Body, []string{"reasoning_effort"})
}

func TestToolCallStreamingProducesFinalArguments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_xai_tool","model":"grok-3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{\"city\""}}]},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_xai_tool","model":"grok-3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Melbourne\"}"}}]},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_xai_tool","model":"grok-3","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	model := xaiTestModel()
	client := xaiTestClient(t, model, server.URL)
	stream := client.Stream(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("weather")},
		Tools: []sigma.Tool{{
			Name:        "weather",
			Description: "Get weather",
			InputSchema: sigma.Schema{"type": "object"},
		}},
	})
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

func TestProviderErrorIsTypedRedactedAndDetectsContextOverflow(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"This model's maximum prompt length is 131072 but the request contains 200000 tokens"}}`)
	}))
	t.Cleanup(server.Close)

	model := xaiTestModel()
	client := xaiTestClient(t, model, server.URL)
	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("xai-secret-key"),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if !errors.Is(err, sigma.ErrContextOverflow) {
		t.Fatalf("error = %v, want ErrContextOverflow", err)
	}
	if strings.Contains(err.Error(), "xai-secret-key") {
		t.Fatalf("error leaked request API key: %v", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
}

func TestStreamCloseCancelsStreamingRequest(t *testing.T) {
	t.Parallel()

	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_xai_close","model":"grok-3","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(requestCanceled)
	}))
	t.Cleanup(server.Close)

	model := xaiTestModel()
	client := xaiTestClient(t, model, server.URL)
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

func xaiTestClient(t *testing.T, model sigma.Model, baseURL string, opts ...xai.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]xai.ProviderOption{xai.WithBaseURL(baseURL)}, opts...)
	if err := registry.RegisterTextProvider(sigma.ProviderXAI, xai.NewProvider(providerOpts...)); err != nil {
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

func xaiTestModel() sigma.Model {
	return sigma.Model{
		ID:                   "grok-3",
		Provider:             sigma.ProviderXAI,
		API:                  sigma.APIOpenAICompletions,
		SupportedInputs:      []sigma.ContentBlockType{sigma.ContentBlockText},
		SupportsTools:        true,
		SupportsThinking:     true,
		InputCostPerMillion:  3,
		OutputCostPerMillion: 15,
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
	select {
	case ch <- capturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
		Body:    body,
	}:
	case <-time.After(time.Second):
		t.Fatal("timed out capturing request")
	}
}

func receiveRequest(t *testing.T, ch <-chan capturedRequest) capturedRequest {
	t.Helper()

	select {
	case request := <-ch:
		return request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
		return capturedRequest{}
	}
}

func collectEvents(t *testing.T, stream *sigma.Stream) []sigma.Event {
	t.Helper()

	var events []sigma.Event
	for {
		event := receiveEvent(t, stream)
		events = append(events, event)
		if event.Kind == sigma.EventKindDone || event.Kind == sigma.EventKindError {
			return events
		}
	}
}

func receiveEvent(t *testing.T, stream *sigma.Stream) sigma.Event {
	t.Helper()

	select {
	case event := <-stream.Events():
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream event")
		return sigma.Event{}
	}
}

func receiveSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func eventKinds(events []sigma.Event) []sigma.EventKind {
	kinds := make([]sigma.EventKind, len(events))
	for index, event := range events {
		kinds[index] = event.Kind
	}
	return kinds
}

func assertHeader(t *testing.T, headers http.Header, key string, want string) {
	t.Helper()

	if got := headers.Get(key); got != want {
		t.Fatalf("header %s = %q, want %q", key, got, want)
	}
}

func assertNoJSONPath(t *testing.T, data []byte, path []string) {
	t.Helper()

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return
		}
		next, ok := object[key]
		if !ok {
			return
		}
		value = next
	}
	t.Fatalf("json path %v exists with value %#v", path, value)
}

func assertJSONPath(t *testing.T, data []byte, path []string, want any) {
	t.Helper()

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("json path %v stopped at non-object %#v", path, value)
		}
		next, ok := object[key]
		if !ok {
			t.Fatalf("json path %v missing key %q", path, key)
		}
		value = next
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("json path %v = %#v, want %#v", path, value, want)
	}
}
