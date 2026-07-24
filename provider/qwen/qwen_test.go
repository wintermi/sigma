// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package qwen_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/qwen"
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
		{name: "international", provider: sigma.ProviderQwenTokenPlan, register: func(registry *sigma.Registry) error {
			return qwen.Register(registry)
		}},
		{name: "china", provider: sigma.ProviderQwenTokenPlanCN, register: func(registry *sigma.Registry) error {
			return qwen.RegisterCN(registry)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := sigma.NewRegistry()
			if err := tt.register(registry); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			if err := registry.RegisterModel(qwenTestModel(tt.provider)); err != nil {
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

	model := qwenTestModel(sigma.ProviderQwenTokenPlanCN)
	registry := sigma.NewRegistry()
	if err := qwen.RegisterCN(registry, qwen.WithBaseURL(server.URL+"/v1")); err != nil {
		t.Fatalf("RegisterCN returned error: %v", err)
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
	if got, want := request.Path, "/v1/chat/completions"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("Authorization"), "Bearer request-key"; got != want {
		t.Fatalf("Authorization header = %q, want %q", got, want)
	}
}

func TestCompleteUsesRegionalDefaultBaseURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider sigma.ProviderID
		baseURL  string
		register func(*sigma.Registry, *http.Client) error
	}{
		{
			name:     "international",
			provider: sigma.ProviderQwenTokenPlan,
			baseURL:  qwen.DefaultBaseURL,
			register: func(registry *sigma.Registry, client *http.Client) error {
				return qwen.Register(registry, qwen.WithHTTPClient(client))
			},
		},
		{
			name:     "china",
			provider: sigma.ProviderQwenTokenPlanCN,
			baseURL:  qwen.DefaultCNBaseURL,
			register: func(registry *sigma.Registry, client *http.Client) error {
				return qwen.RegisterCN(registry, qwen.WithHTTPClient(client))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var request *http.Request
			httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				request = r.Clone(r.Context())
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(completedStream)),
					Request:    r,
				}, nil
			})}
			registry := sigma.NewRegistry()
			if err := tt.register(registry, httpClient); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			model := qwenTestModel(tt.provider)
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}

			final, err := sigma.NewClient(sigma.WithRegistry(registry)).Complete(
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
			if request == nil {
				t.Fatal("provider did not send a request")
			}
			if got, want := request.URL.String(), tt.baseURL+"/chat/completions"; got != want {
				t.Fatalf("request URL = %q, want %q", got, want)
			}
			if got, want := request.Header.Get("Authorization"), "Bearer request-key"; got != want {
				t.Fatalf("Authorization header = %q, want %q", got, want)
			}
		})
	}
}

func TestRegistersCatalogTokenPlanModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider sigma.ProviderID
		register func(*sigma.Registry) error
	}{
		{name: "international", provider: sigma.ProviderQwenTokenPlan, register: func(registry *sigma.Registry) error {
			return qwen.Register(registry)
		}},
		{name: "china", provider: sigma.ProviderQwenTokenPlanCN, register: func(registry *sigma.Registry) error {
			return qwen.RegisterCN(registry)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, modelID := range []sigma.ModelID{"qwen3.7-max", "qwen3.8-max-preview"} {
				model, ok := sigma.DefaultRegistry().Model(tt.provider, modelID)
				if !ok {
					t.Fatalf("default registry missing %s model %q", tt.provider, modelID)
				}
				registry := sigma.NewRegistry()
				if err := tt.register(registry); err != nil {
					t.Fatalf("register returned error: %v", err)
				}
				if err := registry.RegisterModel(model); err != nil {
					t.Fatalf("RegisterModel(%s, %s) returned error: %v", tt.provider, modelID, err)
				}
			}
		})
	}
}

func qwenTestModel(provider sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:               "qwen3.7-max",
		Provider:         provider,
		API:              sigma.APIOpenAICompletions,
		Name:             "Qwen3.7 Max",
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText},
		SupportsTools:    true,
		SupportsThinking: true,
	}
}

func captureRequest(t *testing.T, requests chan<- capturedRequest, r *http.Request) {
	t.Helper()

	requests <- capturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
	}
}

func receiveRequest(t *testing.T, requests <-chan capturedRequest) capturedRequest {
	t.Helper()

	select {
	case request := <-requests:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request")
		return capturedRequest{}
	}
}

func writeCompleted(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, completedStream)
}

const completedStream = `data: {"id":"chatcmpl_test","model":"qwen3.7-max","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}

data: {"id":"chatcmpl_test","model":"qwen3.7-max","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
