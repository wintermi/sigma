// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package antling_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/antling"
)

func TestRegisterReportsOpenAICompletionsAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := antling.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(antLingTestModel(t, "Ling-3.1-flash")); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].ID, sigma.ProviderAntLing; got != want {
		t.Fatalf("provider ID = %q, want %q", got, want)
	}
	if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestRegisterAcceptsGeneratedCatalogModels(t *testing.T) {
	t.Parallel()

	for _, modelID := range []sigma.ModelID{"Ling-3.1-flash"} {
		modelID := modelID
		t.Run(string(modelID), func(t *testing.T) {
			t.Parallel()

			registry := sigma.NewRegistry()
			if err := antling.Register(registry); err != nil {
				t.Fatalf("Register returned error: %v", err)
			}
			if err := registry.RegisterModel(antLingTestModel(t, modelID)); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}
		})
	}
}

func TestLingSendsAntLingRequestShape(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeCompleted(t, w, "Ling-3.1-flash")
	}))
	t.Cleanup(server.Close)

	model := antLingTestModel(t, "Ling-3.1-flash")
	model = withBaseURL(model, server.URL)
	client := antLingTestClient(t, model, server.URL, antling.WithHeader("X-Provider", "provider"))
	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
		sigma.WithHeader("X-Custom", "custom"),
		sigma.WithMaxTokens(123),
		sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
		sigma.WithCacheRetention(sigma.CacheRetentionLong),
		sigma.WithSessionID("ant-ling-session"),
		sigma.WithProviderOptions(sigma.ProviderAntLing, map[string]any{
			"extra_body": map[string]any{"store": true},
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "ok"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
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
	assertJSONPath(t, request.Body, []string{"max_tokens"}, float64(123))
	assertJSONPath(t, request.Body, []string{"thinking", "type"}, "enabled")
	assertNoJSONPath(t, request.Body, []string{"reasoning"})
	assertNoJSONPath(t, request.Body, []string{"max_completion_tokens"})
	assertNoJSONPath(t, request.Body, []string{"reasoning_effort"})
	assertNoJSONPath(t, request.Body, []string{"store"})
	assertNoJSONPath(t, request.Body, []string{"prompt_cache_key"})
	assertNoJSONPath(t, request.Body, []string{"prompt_cache_retention"})
}

// Ling 3.x thinks by default, so turning reasoning off must be sent explicitly.
func TestLingDisablesThinkingWhenReasoningIsOff(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeCompleted(t, w, "Ling-3.1-flash")
	}))
	t.Cleanup(server.Close)

	model := antLingTestModel(t, "Ling-3.1-flash")
	model = withBaseURL(model, server.URL)
	client := antLingTestClient(t, model, server.URL)
	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
		sigma.WithReasoningLevel(sigma.ThinkingLevelOff),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	assertJSONPath(t, request.Body, []string{"thinking", "type"}, "disabled")
	assertNoJSONPath(t, request.Body, []string{"reasoning"})
	assertNoJSONPath(t, request.Body, []string{"reasoning_effort"})
}

func TestStreamCloseCancelsStreamingRequest(t *testing.T) {
	t.Parallel()

	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_antling_close","model":"Ling-3.1-flash","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(requestCanceled)
	}))
	t.Cleanup(server.Close)

	model := antLingTestModel(t, "Ling-3.1-flash")
	model = withBaseURL(model, server.URL)
	client := antLingTestClient(t, model, server.URL)
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

func antLingTestClient(t *testing.T, model sigma.Model, baseURL string, opts ...antling.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]antling.ProviderOption{antling.WithBaseURL(baseURL)}, opts...)
	if err := antling.Register(registry, providerOpts...); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-key"}, nil
	})
	return sigma.NewClient(sigma.WithRegistry(registry), sigma.WithAuthResolver(resolver))
}

func antLingTestModel(t *testing.T, modelID sigma.ModelID) sigma.Model {
	t.Helper()

	model, ok := sigma.DefaultRegistry().Model(sigma.ProviderAntLing, modelID)
	if !ok {
		t.Fatalf("default registry missing Ant Ling model %s", modelID)
	}
	return model
}

func withBaseURL(model sigma.Model, baseURL string) sigma.Model {
	metadata := make(map[string]any, len(model.ProviderMetadata)+1)
	for key, value := range model.ProviderMetadata {
		metadata[key] = value
	}
	metadata["baseURL"] = baseURL
	model.ProviderMetadata = metadata
	return model
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

func writeCompleted(t *testing.T, w http.ResponseWriter, model string) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"id":"chatcmpl_antling","model":"`+model+`","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`+"\n\n")
	_, _ = io.WriteString(w, `data: {"id":"chatcmpl_antling","model":"`+model+`","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`+"\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
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
