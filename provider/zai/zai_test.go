// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package zai_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/zai"
)

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
}

func TestRegistersReportOpenAICompletionsAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider sigma.ProviderID
		register func(*sigma.Registry) error
	}{
		{name: "zai", provider: sigma.ProviderZAI, register: func(registry *sigma.Registry) error {
			return zai.Register(registry)
		}},
		{name: "coding cn", provider: sigma.ProviderZAICodingCN, register: func(registry *sigma.Registry) error {
			return zai.RegisterCodingCN(registry)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := sigma.NewRegistry()
			if err := tt.register(registry); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			if err := registry.RegisterModel(zaiTestModel(tt.provider)); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}

			providers := registry.ListProviders()
			if got, want := providers[0].ID, tt.provider; got != want {
				t.Fatalf("provider ID = %q, want %q", got, want)
			}
			if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
				t.Fatalf("provider API = %q, want %q", got, want)
			}
		})
	}
}

func TestCompleteUsesConfiguredOpenAICompatibleBaseURL(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeCompleted(t, w)
	}))
	t.Cleanup(server.Close)

	model := zaiTestModel(sigma.ProviderZAICodingCN)
	registry := sigma.NewRegistry()
	if err := zai.RegisterCodingCN(registry, zai.WithBaseURL(server.URL)); err != nil {
		t.Fatalf("RegisterCodingCN returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
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
	if got, want := request.Headers.Get("Authorization"), "Bearer request-key"; got != want {
		t.Fatalf("Authorization header = %q, want %q", got, want)
	}
}

func TestRegistersCatalogModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider sigma.ProviderID
		register func(*sigma.Registry) error
	}{
		{name: "zai", provider: sigma.ProviderZAI, register: func(registry *sigma.Registry) error {
			return zai.Register(registry)
		}},
		{name: "coding cn", provider: sigma.ProviderZAICodingCN, register: func(registry *sigma.Registry) error {
			return zai.RegisterCodingCN(registry)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model, ok := sigma.DefaultRegistry().Model(tt.provider, "glm-5.1")
			if !ok {
				t.Fatalf("default registry missing %s glm-5.1", tt.provider)
			}
			registry := sigma.NewRegistry()
			if err := tt.register(registry); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}
		})
	}
}

func zaiTestModel(provider sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:              "glm-5.1",
		Provider:        provider,
		API:             sigma.APIOpenAICompletions,
		Name:            "GLM-5.1",
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
		SupportsTools:   true,
	}
}

func captureRequest(t *testing.T, ch chan<- capturedRequest, r *http.Request) {
	t.Helper()

	_, _ = io.Copy(io.Discard, r.Body)
	ch <- capturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
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

func writeCompleted(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"id":"chatcmpl_zai","model":"glm-5.1","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`+"\n\n")
	_, _ = io.WriteString(w, `data: {"id":"chatcmpl_zai","model":"glm-5.1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`+"\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}
