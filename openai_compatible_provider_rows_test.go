// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/cerebras"
	"github.com/wintermi/sigma/provider/deepseek"
	"github.com/wintermi/sigma/provider/groq"
	"github.com/wintermi/sigma/provider/together"
)

type openAICompatibleProviderRow struct {
	name       string
	providerID sigma.ProviderID
	modelID    sigma.ModelID
	register   func(*sigma.Registry, string) error
}

func TestOpenAICompatibleProviderRowsRegisterTextProviders(t *testing.T) {
	t.Parallel()

	for _, row := range openAICompatibleProviderRows() {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			registry := sigma.NewRegistry()
			if err := row.register(registry, "http://example.test"); err != nil {
				t.Fatalf("Register returned error: %v", err)
			}
			if err := registry.RegisterModel(openAICompatibleRowModel(row.providerID, row.modelID)); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}

			providers := registry.ListProviders()
			if got, want := providers[0].ID, row.providerID; got != want {
				t.Fatalf("provider ID = %q, want %q", got, want)
			}
			if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
				t.Fatalf("provider API = %q, want %q", got, want)
			}
		})
	}
}

func TestOpenAICompatibleProviderRowsStreamTextWithDefaults(t *testing.T) {
	t.Parallel()

	for _, row := range openAICompatibleProviderRows() {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedOpenAICompatibleRowRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureOpenAICompatibleRowRequest(t, requests, r)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, `data: {"id":"chatcmpl_row","model":"`+string(row.modelID)+`","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`+"\n\n")
				_, _ = io.WriteString(w, `data: {"id":"chatcmpl_row","model":"`+string(row.modelID)+`","choices":[{"index":0,"delta":{"content":" row"},"finish_reason":null}]}`+"\n\n")
				_, _ = io.WriteString(w, `data: {"id":"chatcmpl_row","model":"`+string(row.modelID)+`","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`+"\n\n")
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)

			model := openAICompatibleRowModel(row.providerID, row.modelID)
			client := openAICompatibleRowClient(t, row, model, server.URL)
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
			if got, want := final.Content[0].Text, "Hello row"; got != want {
				t.Fatalf("final text = %q, want %q", got, want)
			}
			if final.Usage == nil || final.Usage.InputTokens != 7 || final.Usage.OutputTokens != 3 {
				t.Fatalf("final usage = %#v, want input/output usage", final.Usage)
			}
			if final.Cost == nil || final.Cost.TotalCost == 0 {
				t.Fatalf("final cost = %#v, want populated cost", final.Cost)
			}

			request := receiveOpenAICompatibleRowRequest(t, requests)
			if got, want := request.Method, http.MethodPost; got != want {
				t.Fatalf("method = %q, want %q", got, want)
			}
			if got, want := request.Path, "/chat/completions"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			assertOpenAICompatibleRowHeader(t, request.Headers, "Authorization", "Bearer request-key")
			assertOpenAICompatibleRowHeader(t, request.Headers, "X-Provider", "provider")
			assertOpenAICompatibleRowHeader(t, request.Headers, "X-Custom", "custom")
		})
	}
}

func TestOpenAICompatibleProviderRowsReturnRedactedContextOverflowErrors(t *testing.T) {
	t.Parallel()

	for _, row := range openAICompatibleProviderRows() {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"message":"This model's maximum context length is 131072 tokens, but the request contains 200000 tokens."}}`)
			}))
			t.Cleanup(server.Close)

			model := openAICompatibleRowModel(row.providerID, row.modelID)
			client := openAICompatibleRowClient(t, row, model, server.URL)
			final, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithAPIKey("row-secret-key"),
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
			if strings.Contains(err.Error(), "row-secret-key") {
				t.Fatalf("error leaked request API key: %v", err)
			}
			if got, want := final.StopReason, sigma.StopReasonError; got != want {
				t.Fatalf("stop reason = %q, want %q", got, want)
			}
		})
	}
}

func TestOpenAICompatibleProviderRowsCloseCancelsStreamingRequest(t *testing.T) {
	t.Parallel()

	for _, row := range openAICompatibleProviderRows() {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			requestCanceled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, `data: {"id":"chatcmpl_row_close","model":"`+string(row.modelID)+`","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}`+"\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(requestCanceled)
			}))
			t.Cleanup(server.Close)

			model := openAICompatibleRowModel(row.providerID, row.modelID)
			client := openAICompatibleRowClient(t, row, model, server.URL)
			stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
			for {
				event := receiveOpenAICompatibleRowEvent(t, stream)
				if event.Kind == sigma.EventKindTextDelta {
					break
				}
			}
			stream.Close()

			receiveOpenAICompatibleRowSignal(t, requestCanceled)
			receiveOpenAICompatibleRowSignal(t, stream.Done())
		})
	}
}

func openAICompatibleProviderRows() []openAICompatibleProviderRow {
	return []openAICompatibleProviderRow{
		{
			name:       "deepseek",
			providerID: sigma.ProviderDeepSeek,
			modelID:    "deepseek-v4-flash",
			register: func(registry *sigma.Registry, baseURL string) error {
				return deepseek.Register(registry, deepseek.WithBaseURL(baseURL), deepseek.WithHeader("X-Provider", "provider"))
			},
		},
		{
			name:       "groq",
			providerID: sigma.ProviderGroq,
			modelID:    "llama-3.3-70b-versatile",
			register: func(registry *sigma.Registry, baseURL string) error {
				return groq.Register(registry, groq.WithBaseURL(baseURL), groq.WithHeader("X-Provider", "provider"))
			},
		},
		{
			name:       "cerebras",
			providerID: sigma.ProviderCerebras,
			modelID:    "llama3.1-8b",
			register: func(registry *sigma.Registry, baseURL string) error {
				return cerebras.Register(registry, cerebras.WithBaseURL(baseURL), cerebras.WithHeader("X-Provider", "provider"))
			},
		},
		{
			name:       "together",
			providerID: sigma.ProviderTogether,
			modelID:    "Qwen/Qwen3-Coder-480B-A35B-Instruct-FP8",
			register: func(registry *sigma.Registry, baseURL string) error {
				return together.Register(registry, together.WithBaseURL(baseURL), together.WithHeader("X-Provider", "provider"))
			},
		},
	}
}

func openAICompatibleRowClient(t *testing.T, row openAICompatibleProviderRow, model sigma.Model, baseURL string) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := row.register(registry, baseURL); err != nil {
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

func openAICompatibleRowModel(providerID sigma.ProviderID, modelID sigma.ModelID) sigma.Model {
	return sigma.Model{
		ID:                   modelID,
		Provider:             providerID,
		API:                  sigma.APIOpenAICompletions,
		SupportedInputs:      []sigma.ContentBlockType{sigma.ContentBlockText},
		SupportsTools:        true,
		ContextWindow:        131072,
		InputCostPerMillion:  1,
		OutputCostPerMillion: 2,
	}
}

type capturedOpenAICompatibleRowRequest struct {
	Method  string
	Path    string
	Headers http.Header
}

func captureOpenAICompatibleRowRequest(t *testing.T, ch chan<- capturedOpenAICompatibleRowRequest, r *http.Request) {
	t.Helper()

	_, _ = io.Copy(io.Discard, r.Body)
	select {
	case ch <- capturedOpenAICompatibleRowRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
	}:
	case <-time.After(time.Second):
		t.Fatal("timed out capturing request")
	}
}

func receiveOpenAICompatibleRowRequest(t *testing.T, ch <-chan capturedOpenAICompatibleRowRequest) capturedOpenAICompatibleRowRequest {
	t.Helper()

	select {
	case request := <-ch:
		return request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
		return capturedOpenAICompatibleRowRequest{}
	}
}

func receiveOpenAICompatibleRowEvent(t *testing.T, stream *sigma.Stream) sigma.Event {
	t.Helper()

	select {
	case event := <-stream.Events():
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream event")
		return sigma.Event{}
	}
}

func receiveOpenAICompatibleRowSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func assertOpenAICompatibleRowHeader(t *testing.T, headers http.Header, key string, want string) {
	t.Helper()

	if got := headers.Get(key); got != want {
		t.Fatalf("header %s = %q, want %q", key, got, want)
	}
}
