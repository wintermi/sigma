// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package vercel_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/vercel"
)

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

func TestRegisterReportsMessagesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := vercel.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	model := vercelTestModel(t)
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].ID, sigma.ProviderVercelAIGateway; got != want {
		t.Fatalf("provider ID = %q, want %q", got, want)
	}
	if got, want := providers[0].TextAPI, sigma.APIAnthropicMessages; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestRegisterAcceptsCatalogModels(t *testing.T) {
	t.Parallel()

	for _, modelID := range []sigma.ModelID{
		"anthropic/claude-opus-4.8",
		"google/gemini-3-pro-preview",
		"openai/gpt-5.4",
	} {
		modelID := modelID
		t.Run(string(modelID), func(t *testing.T) {
			t.Parallel()

			model, ok := sigma.DefaultRegistry().Model(sigma.ProviderVercelAIGateway, modelID)
			if !ok {
				t.Fatalf("default registry missing Vercel AI Gateway model %q", modelID)
			}
			registry := sigma.NewRegistry()
			if err := vercel.Register(registry); err != nil {
				t.Fatalf("Register returned error: %v", err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel(%q) returned error: %v", modelID, err)
			}
		})
	}
}

func TestCompleteUsesVercelDefaultsAndModelMetadata(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeCompleted(t, w, "anthropic/claude-opus-4.8")
	}))
	t.Cleanup(server.Close)

	model := vercelTestModel(t)
	model.ProviderMetadata = copyProviderMetadata(model.ProviderMetadata)
	model.ProviderMetadata["baseURL"] = server.URL
	client := vercelTestClient(t, model)

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
		sigma.WithHeader("X-Custom", "custom"),
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
	if got, want := request.Path, "/messages"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "X-Api-Key", "request-key")
	assertHeader(t, request.Headers, "Authorization", "")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	assertJSONPath(t, request.Body, []string{"model"}, "anthropic/claude-opus-4.8")
}

func TestProviderErrorsAreTypedAndRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		http.Error(w, `{"error":{"type":"authentication_error","message":"bad key"}}`, http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	model := vercelTestModel(t)
	model.ProviderMetadata = copyProviderMetadata(model.ProviderMetadata)
	model.ProviderMetadata["baseURL"] = server.URL
	client := vercelTestClient(t, model)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("secret-request-key"),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error %T does not match ErrProviderResponse: %v", err, err)
	}
	if got, want := sigma.ClassifyError(err).Class, sigma.ErrorClassAuth; got != want {
		t.Fatalf("error class = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "secret-request-key") {
		t.Fatalf("error leaked request API key: %v", err)
	}
}

func TestStreamCloseCancelsStreamingRequest(t *testing.T) {
	t.Parallel()

	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_close","type":"message","role":"assistant","model":"anthropic/claude-opus-4.8","content":[]}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(requestCanceled)
	}))
	t.Cleanup(server.Close)

	model := vercelTestModel(t)
	model.ProviderMetadata = copyProviderMetadata(model.ProviderMetadata)
	model.ProviderMetadata["baseURL"] = server.URL
	client := vercelTestClient(t, model)

	stream := client.Stream(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
	)
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

func vercelTestClient(t *testing.T, model sigma.Model, opts ...vercel.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := vercel.Register(registry, opts...); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func vercelTestModel(t *testing.T) sigma.Model {
	t.Helper()

	model, ok := sigma.DefaultRegistry().Model(sigma.ProviderVercelAIGateway, "anthropic/claude-opus-4.8")
	if !ok {
		t.Fatal("default registry missing Vercel AI Gateway model")
	}
	return model
}

func copyProviderMetadata(values map[string]any) map[string]any {
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func captureRequest(t *testing.T, ch chan<- capturedRequest, r *http.Request) {
	t.Helper()

	body, _ := io.ReadAll(r.Body)
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

func receiveEvent(t *testing.T, stream *sigma.Stream) sigma.Event {
	t.Helper()

	select {
	case event, ok := <-stream.Events():
		if !ok {
			t.Fatal("stream closed before expected event")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stream event")
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

func assertHeader(t *testing.T, headers http.Header, key string, want string) {
	t.Helper()

	if got := headers.Get(key); got != want {
		t.Fatalf("%s header = %q, want %q", key, got, want)
	}
}

func assertJSONPath(t *testing.T, body []byte, path []string, want any) {
	t.Helper()

	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	value := decoded
	for _, segment := range path {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("JSON path %v stopped at non-object %#v", path, value)
		}
		value = object[segment]
	}
	if value != want {
		t.Fatalf("JSON path %v = %#v, want %#v", path, value, want)
	}
}

func writeCompleted(t *testing.T, w http.ResponseWriter, model string) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"`+model+`","content":[]}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}

data: {"type":"message_stop"}

`)
}
