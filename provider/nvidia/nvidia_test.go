// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package nvidia_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/nvidia"
)

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

func TestRegisterReportsOpenAICompletionsAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := nvidia.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(nvidiaTextModel()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestRegisterEmbeddingsReportsOpenAIEmbeddingsAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := nvidia.RegisterEmbeddings(registry); err != nil {
		t.Fatalf("RegisterEmbeddings returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModel(nvidiaEmbeddingModel()); err != nil {
		t.Fatalf("RegisterEmbeddingModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].EmbeddingAPI, sigma.EmbeddingAPIOpenAIEmbeddings; got != want {
		t.Fatalf("embedding provider API = %q, want %q", got, want)
	}
}

func TestCompleteStreamsTextWithNVIDIADefaults(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_nvidia","model":"openai/gpt-oss-20b","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_nvidia","model":"openai/gpt-oss-20b","choices":[{"index":0,"delta":{"content":" NIM"},"finish_reason":null}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_nvidia","model":"openai/gpt-oss-20b","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	model := nvidiaTextModel()
	client := nvidiaTextClient(t, model, server.URL, nvidia.WithHeader("X-Provider", "provider"))
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
	if got, want := final.Content[0].Text, "Hello NIM"; got != want {
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
	assertHeader(t, request.Headers, "NVCF-POLL-SECONDS", "3600")
}

func TestEmbedMapsInputTypeAndPreservesExplicitProviderMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       sigma.EmbeddingRequest
		wantInput string
	}{
		{
			name: "query",
			req: sigma.EmbeddingRequest{
				Inputs:    []string{"alpha"},
				InputType: sigma.EmbeddingInputTypeQuery,
			},
			wantInput: "query",
		},
		{
			name: "document",
			req: sigma.EmbeddingRequest{
				Inputs:    []string{"alpha"},
				InputType: sigma.EmbeddingInputTypeDocument,
			},
			wantInput: "passage",
		},
		{
			name: "explicit",
			req: sigma.EmbeddingRequest{
				Inputs:           []string{"alpha"},
				InputType:        sigma.EmbeddingInputTypeQuery,
				ProviderMetadata: map[string]any{"input_type": "passage", "truncate": "END"},
			},
			wantInput: "passage",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`)
			}))
			t.Cleanup(server.Close)

			model := nvidiaEmbeddingModel()
			client := nvidiaEmbeddingClient(t, model, server.URL, nvidia.WithHeader("X-Provider", "provider"))
			got, err := client.Embed(
				context.Background(),
				model,
				tt.req,
				sigma.WithEmbeddingAPIKey("request-key"),
				sigma.WithEmbeddingHeader("X-Custom", "custom"),
			)
			if err != nil {
				t.Fatalf("Embed returned error: %v", err)
			}
			if got.Provider != sigma.ProviderNVIDIA || got.Model != model.ID {
				t.Fatalf("embedding identity = %s/%s, want %s/%s", got.Provider, got.Model, sigma.ProviderNVIDIA, model.ID)
			}

			request := receiveRequest(t, requests)
			if got, want := request.Method, http.MethodPost; got != want {
				t.Fatalf("method = %q, want %q", got, want)
			}
			if got, want := request.Path, "/embeddings"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			assertHeader(t, request.Headers, "Authorization", "Bearer request-key")
			assertHeader(t, request.Headers, "X-Provider", "provider")
			assertHeader(t, request.Headers, "X-Custom", "custom")
			assertHeader(t, request.Headers, "NVCF-POLL-SECONDS", "3600")

			var payload map[string]any
			if err := json.Unmarshal(request.Body, &payload); err != nil {
				t.Fatalf("Unmarshal payload returned error: %v", err)
			}
			if got := payload["input_type"]; got != tt.wantInput {
				t.Fatalf("input_type = %v, want %q; payload = %s", got, tt.wantInput, request.Body)
			}
			if tt.req.ProviderMetadata["truncate"] != nil && payload["truncate"] != "END" {
				t.Fatalf("truncate = %v, want END", payload["truncate"])
			}
		})
	}
}

func TestRegistersCatalogModels(t *testing.T) {
	t.Parallel()

	text, ok := sigma.DefaultRegistry().Model(sigma.ProviderNVIDIA, "openai/gpt-oss-20b")
	if !ok {
		t.Fatal("default registry missing NVIDIA text model")
	}
	embedding, ok := sigma.DefaultRegistry().EmbeddingModel(sigma.ProviderNVIDIA, "nvidia/nv-embedqa-e5-v5")
	if !ok {
		t.Fatal("default registry missing NVIDIA embedding model")
	}

	registry := sigma.NewRegistry()
	if err := nvidia.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := nvidia.RegisterEmbeddings(registry); err != nil {
		t.Fatalf("RegisterEmbeddings returned error: %v", err)
	}
	if err := registry.RegisterModel(text); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModel(embedding); err != nil {
		t.Fatalf("RegisterEmbeddingModel returned error: %v", err)
	}
}

func nvidiaTextClient(t *testing.T, model sigma.Model, baseURL string, opts ...nvidia.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]nvidia.ProviderOption{nvidia.WithBaseURL(baseURL)}, opts...)
	if err := nvidia.Register(registry, providerOpts...); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func nvidiaEmbeddingClient(t *testing.T, model sigma.EmbeddingModel, baseURL string, opts ...nvidia.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]nvidia.ProviderOption{nvidia.WithBaseURL(baseURL)}, opts...)
	if err := nvidia.RegisterEmbeddings(registry, providerOpts...); err != nil {
		t.Fatalf("RegisterEmbeddings returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModel(model); err != nil {
		t.Fatalf("RegisterEmbeddingModel returned error: %v", err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func nvidiaTextModel() sigma.Model {
	return sigma.Model{
		ID:                   "openai/gpt-oss-20b",
		Provider:             sigma.ProviderNVIDIA,
		API:                  sigma.APIOpenAICompletions,
		SupportedInputs:      []sigma.ContentBlockType{sigma.ContentBlockText},
		SupportsTools:        true,
		InputCostPerMillion:  1,
		OutputCostPerMillion: 2,
		CostCurrency:         "USD",
		ProviderMetadata: map[string]any{
			"headers": map[string]string{"NVCF-POLL-SECONDS": "3600"},
		},
	}
}

func nvidiaEmbeddingModel() sigma.EmbeddingModel {
	return sigma.EmbeddingModel{
		ID:                  "nvidia/nv-embedqa-e5-v5",
		Provider:            sigma.ProviderNVIDIA,
		API:                 sigma.EmbeddingAPIOpenAIEmbeddings,
		DefaultDimensions:   1024,
		MinDimensions:       1024,
		MaxDimensions:       1024,
		MaxInputTokens:      8192,
		MaxBatchInputs:      100,
		InputCostPerMillion: 0,
		CostCurrency:        "USD",
		ProviderMetadata: map[string]any{
			"headers": map[string]string{"NVCF-POLL-SECONDS": "3600"},
		},
	}
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

func assertHeader(t *testing.T, headers http.Header, key, want string) {
	t.Helper()

	if got := headers.Get(key); got != want {
		t.Fatalf("%s header = %q, want %q", key, got, want)
	}
}
