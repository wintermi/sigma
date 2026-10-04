// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

func TestRegisterLocalEmbeddingsDefaultsToOllamaNomicEmbedText(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	model, err := openai.RegisterLocalEmbeddings(registry, openai.LocalEmbeddingConfig{})
	if err != nil {
		t.Fatalf("RegisterLocalEmbeddings returned error: %v", err)
	}
	if got, want := model.Provider, sigma.ProviderID("ollama"); got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if got, want := model.ID, sigma.ModelID("nomic-embed-text"); got != want {
		t.Fatalf("model id = %q, want %q", got, want)
	}
	if got, want := model.ProviderMetadata[sigma.MetadataOpenAICompatibleBaseURL], "http://127.0.0.1:11434/v1"; got != want {
		t.Fatalf("base url = %q, want %q", got, want)
	}
	if got, want := model.MaxBatchInputs, 1; got != want {
		t.Fatalf("max batch inputs = %d, want %d", got, want)
	}
	if _, ok := model.ProviderMetadata[sigma.MetadataAPIKeyEnvVars]; ok {
		t.Fatalf("api key env metadata = %#v, want absent", model.ProviderMetadata[sigma.MetadataAPIKeyEnvVars])
	}
	if _, ok := registry.EmbeddingProvider("ollama"); !ok {
		t.Fatal("embedding provider was not registered")
	}
	if registered, ok := registry.EmbeddingModel("ollama", "nomic-embed-text"); !ok || registered.ID != model.ID {
		t.Fatalf("registered model = %#v, ok %v, want returned model", registered, ok)
	}
}

func TestRegisterLocalEmbeddingsNormalizesBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "host without scheme", raw: "localhost:11434", want: "http://localhost:11434/v1"},
		{name: "scheme without path", raw: "http://localhost:11434", want: "http://localhost:11434/v1"},
		{name: "existing v1 path", raw: " http://localhost:11434/v1/ ", want: "http://localhost:11434/v1"},
		{name: "query and fragment", raw: "http://localhost:11434/openai?token=secret#frag", want: "http://localhost:11434/openai/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := sigma.NewRegistry()
			model, err := openai.RegisterLocalEmbeddings(registry, openai.LocalEmbeddingConfig{
				Provider: "local",
				ID:       "embed",
				BaseURL:  tt.raw,
			})
			if err != nil {
				t.Fatalf("RegisterLocalEmbeddings returned error: %v", err)
			}
			if got := model.ProviderMetadata[sigma.MetadataOpenAICompatibleBaseURL]; got != tt.want {
				t.Fatalf("base url = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRegisterLocalEmbeddingsUsesMetadataForRunnableEndpoint(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("LOCAL_EMBEDDING_API_KEY", "env-secret")

	headers := map[string]string{"X-Model": "model", "X-Model-Only": "present", "Authorization": "Bearer model"}
	metadata := map[string]any{"family": "local"}
	registry := sigma.NewRegistry()
	model, err := openai.RegisterLocalEmbeddings(registry, openai.LocalEmbeddingConfig{
		Provider:            "local-embeddings",
		ID:                  "local-embed",
		BaseURL:             server.URL,
		APIKeyEnv:           "LOCAL_EMBEDDING_API_KEY",
		Headers:             headers,
		Name:                "Local Embeddings",
		DefaultDimensions:   1024,
		MinDimensions:       1,
		MaxDimensions:       1024,
		MaxInputTokens:      8192,
		MaxBatchInputs:      4,
		MaxBatchBytes:       4096,
		InputCostPerMillion: 0.01,
		CostCurrency:        "USD",
		ProviderMetadata:    metadata,
	})
	if err != nil {
		t.Fatalf("RegisterLocalEmbeddings returned error: %v", err)
	}
	headers["X-Model-Only"] = "mutated"
	metadata["family"] = "mutated"

	client := sigma.NewClient(sigma.WithRegistry(registry))
	_, err = client.Embed(
		context.Background(),
		model,
		sigma.EmbeddingRequest{Inputs: []string{"hi"}, Dimensions: 2},
		sigma.WithEmbeddingHeader("X-Model", "request"),
	)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/v1/embeddings"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer env-secret")
	assertHeader(t, request.Headers, "X-Model", "request")
	assertHeader(t, request.Headers, "X-Model-Only", "present")
	if got, want := model.ProviderMetadata["family"], "local"; got != want {
		t.Fatalf("provider metadata family = %q, want %q", got, want)
	}
	envNames, ok := model.ProviderMetadata[sigma.MetadataAPIKeyEnvVars].([]string)
	if !ok || len(envNames) != 1 || envNames[0] != "LOCAL_EMBEDDING_API_KEY" {
		t.Fatalf("api key env metadata = %#v, want LOCAL_EMBEDDING_API_KEY", model.ProviderMetadata[sigma.MetadataAPIKeyEnvVars])
	}
	if got, want := model.DefaultDimensions, 1024; got != want {
		t.Fatalf("default dimensions = %d, want %d", got, want)
	}
	if got, want := model.MaxBatchInputs, 4; got != want {
		t.Fatalf("max batch inputs = %d, want %d", got, want)
	}
	if got, want := model.MaxBatchBytes, 4096; got != want {
		t.Fatalf("max batch bytes = %d, want %d", got, want)
	}
}

func TestRegisterLocalEmbeddingsRejectsNilRegistry(t *testing.T) {
	t.Parallel()

	_, err := openai.RegisterLocalEmbeddings(nil, openai.LocalEmbeddingConfig{})
	if err == nil {
		t.Fatal("RegisterLocalEmbeddings returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if sigmaErr.Code != sigma.ErrorUnsupported || sigmaErr.Message != "registry is required" {
		t.Fatalf("error = %#v, want registry required unsupported error", sigmaErr)
	}
}
