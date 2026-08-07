// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package baseten_test

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

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/baseten"
)

const completedStream = `data: {"id":"chatcmpl_baseten","model":"test","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}

data: {"id":"chatcmpl_baseten","model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}

data: [DONE]

`

type capturedRequest struct {
	Path    string
	Headers http.Header
	Body    []byte
}

func TestRegisterReportsOpenAICompletionsAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := baseten.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	model := basetenModel(t, "zai-org/GLM-5.2")
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].ID, sigma.ProviderBaseten; got != want {
		t.Fatalf("provider ID = %q, want %q", got, want)
	}
	if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestRegisterRejectsNilRegistry(t *testing.T) {
	t.Parallel()

	if err := baseten.Register(nil); err == nil {
		t.Fatal("Register returned nil error")
	}
}

func TestCompleteUsesDefaultBaseURLAndBearerAuthentication(t *testing.T) {
	t.Parallel()

	var request *http.Request
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		request = r.Clone(r.Context())
		return completedResponse(r), nil
	})}
	model := basetenModel(t, "zai-org/GLM-5.2")
	client := basetenClient(t, model, baseten.WithHTTPClient(httpClient))

	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if request == nil {
		t.Fatal("provider did not send a request")
	}
	if got, want := request.URL.String(), baseten.DefaultBaseURL+"/chat/completions"; got != want {
		t.Fatalf("request URL = %q, want %q", got, want)
	}
	if got, want := request.Header.Get("Authorization"), "Bearer request-key"; got != want {
		t.Fatalf("Authorization header = %q, want %q", got, want)
	}
}

func TestCompleteUsesConfiguredBaseURLAndHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		requests <- capturedRequest{Path: r.URL.Path, Headers: r.Header.Clone(), Body: body}
		writeCompleted(w)
	}))
	t.Cleanup(server.Close)

	model := withoutModelBaseURL(basetenModel(t, "zai-org/GLM-5.2"))
	client := basetenClient(
		t,
		model,
		baseten.WithBaseURL(server.URL+"/v1"),
		baseten.WithHeader("X-Provider", "provider"),
	)
	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
		sigma.WithHeader("X-Custom", "custom"),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := <-requests
	if got, want := request.Path, "/v1/chat/completions"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"Authorization": "Bearer request-key",
		"X-Provider":    "provider",
		"X-Custom":      "custom",
	} {
		if got := request.Headers.Get(key); got != want {
			t.Fatalf("%s header = %q, want %q", key, got, want)
		}
	}
}

func TestRegistersFocusedCatalog(t *testing.T) {
	t.Parallel()

	wantIDs := []sigma.ModelID{"moonshotai/Kimi-K2.6", "zai-org/GLM-5.2"}
	var gotIDs []sigma.ModelID
	for _, model := range sigma.DefaultRegistry().ListModels() {
		if model.Provider == sigma.ProviderBaseten {
			gotIDs = append(gotIDs, model.ID)
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("Baseten model IDs = %v, want %v", gotIDs, wantIDs)
	}

	registry := sigma.NewRegistry()
	if err := baseten.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	for _, modelID := range wantIDs {
		if err := registry.RegisterModel(basetenModel(t, modelID)); err != nil {
			t.Fatalf("RegisterModel(%s) returned error: %v", modelID, err)
		}
	}
}

func TestCompleteUsesBasetenThinkingControls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		modelID     sigma.ModelID
		level       sigma.ThinkingLevel
		wantEnabled bool
		wantEffort  string
	}{
		{name: "glm default", modelID: "zai-org/GLM-5.2", wantEffort: "none"},
		{name: "glm off", modelID: "zai-org/GLM-5.2", level: sigma.ThinkingLevelOff, wantEffort: "none"},
		{name: "glm high", modelID: "zai-org/GLM-5.2", level: sigma.ThinkingLevelHigh, wantEnabled: true, wantEffort: "high"},
		{name: "glm max", modelID: "zai-org/GLM-5.2", level: sigma.ThinkingLevel("max"), wantEnabled: true, wantEffort: "max"},
		{name: "kimi default", modelID: "moonshotai/Kimi-K2.6"},
		{name: "kimi off", modelID: "moonshotai/Kimi-K2.6", level: sigma.ThinkingLevelOff},
		{name: "kimi high", modelID: "moonshotai/Kimi-K2.6", level: sigma.ThinkingLevelHigh, wantEnabled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
					return
				}
				requests <- capturedRequest{Body: body}
				writeCompleted(w)
			}))
			t.Cleanup(server.Close)

			model := withoutModelBaseURL(basetenModel(t, tt.modelID))
			client := basetenClient(t, model, baseten.WithBaseURL(server.URL))
			options := []sigma.Option{sigma.WithAPIKey("request-key")}
			if tt.level != "" {
				options = append(options, sigma.WithReasoningLevel(tt.level))
			}
			if _, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				options...,
			); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			var body map[string]any
			if err := json.Unmarshal((<-requests).Body, &body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			args, ok := body["chat_template_args"].(map[string]any)
			if !ok || args["enable_thinking"] != tt.wantEnabled {
				t.Fatalf("chat_template_args = %#v, want enable_thinking %t", body["chat_template_args"], tt.wantEnabled)
			}
			if tt.wantEffort == "" {
				if _, ok := body["reasoning_effort"]; ok {
					t.Fatalf("reasoning_effort = %#v, want absent", body["reasoning_effort"])
				}
			} else if got := body["reasoning_effort"]; got != tt.wantEffort {
				t.Fatalf("reasoning_effort = %#v, want %q", got, tt.wantEffort)
			}
		})
	}
}

func TestCompleteRejectsUnsupportedBasetenThinkingLevelBeforeDispatch(t *testing.T) {
	t.Parallel()

	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return completedResponse(r), nil
	})}
	model := basetenModel(t, "zai-org/GLM-5.2")
	client := basetenClient(t, model, baseten.WithHTTPClient(httpClient))

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
		sigma.WithReasoningLevel(sigma.ThinkingLevelLow),
	)
	if !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("Complete error = %v, want ErrInvalidOptions", err)
	}
	if calls != 0 {
		t.Fatalf("HTTP calls = %d, want 0", calls)
	}
}

func basetenClient(t *testing.T, model sigma.Model, opts ...baseten.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := baseten.Register(registry, opts...); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func basetenModel(t *testing.T, modelID sigma.ModelID) sigma.Model {
	t.Helper()

	model, ok := sigma.DefaultRegistry().Model(sigma.ProviderBaseten, modelID)
	if !ok {
		t.Fatalf("default registry missing Baseten model %q", modelID)
	}
	return model
}

func withoutModelBaseURL(model sigma.Model) sigma.Model {
	metadata := make(map[string]any, len(model.ProviderMetadata))
	for key, value := range model.ProviderMetadata {
		metadata[key] = value
	}
	delete(metadata, "baseURL")
	model.ProviderMetadata = metadata
	return model
}

func completedResponse(request *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(completedStream)),
		Request:    request,
	}
}

func writeCompleted(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, completedStream)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
