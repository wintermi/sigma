// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

func TestGenerateEmbeddingsSendsPayloadAndMapsResponse(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"object": "list",
			"model": "text-embedding-3-small",
			"data": [
				{"object": "embedding", "index": 1, "embedding": [0.3, 0.4]},
				{"object": "embedding", "index": 0, "embedding": [0.1, 0.2]}
			],
			"usage": {"prompt_tokens": 5, "total_tokens": 5}
		}`)
	}))
	t.Cleanup(server.Close)

	client := openAIEmbeddingsTestClient(t, server.URL, openai.WithHeader("X-Provider", "provider"))
	got, err := client.Embed(
		context.Background(),
		openAIEmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"alpha", "beta"}, Dimensions: 128},
		sigma.WithEmbeddingAPIKey("request-key"),
		sigma.WithEmbeddingHeader("X-Custom", "custom"),
		sigma.WithEmbeddingProviderOption(sigma.ProviderOpenAI, "organization", "org_123"),
	)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if got.Model != "text-embedding-3-small" {
		t.Fatalf("model = %q, want text-embedding-3-small", got.Model)
	}
	if got.Provider != sigma.ProviderOpenAI {
		t.Fatalf("provider = %q, want openai", got.Provider)
	}
	wantVectors := []sigma.Embedding{
		{Index: 0, Vector: []float32{0.1, 0.2}},
		{Index: 1, Vector: []float32{0.3, 0.4}},
	}
	if !reflect.DeepEqual(got.Vectors, wantVectors) {
		t.Fatalf("vectors = %#v, want %#v", got.Vectors, wantVectors)
	}
	if got.Usage == nil || got.Usage.InputTokens != 5 || got.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v, want input and total tokens", got.Usage)
	}
	if got.Cost == nil || math.Abs(got.Cost.InputCost-0.0000001) > 1e-12 ||
		math.Abs(got.Cost.TotalCost-0.0000001) > 1e-12 || got.Cost.Currency != "USD" {
		t.Fatalf("cost = %#v, want embedding input cost", got.Cost)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/embeddings"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer request-key")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	assertHeader(t, request.Headers, "OpenAI-Organization", "org_123")
	body := string(request.Body)
	if !strings.Contains(body, `"encoding_format":"float"`) {
		t.Fatalf("payload = %s, want float encoding format", request.Body)
	}
	if !strings.Contains(body, `"dimensions":128`) {
		t.Fatalf("payload = %s, want dimensions", request.Body)
	}
	if !strings.Contains(body, `"input":["alpha","beta"]`) {
		t.Fatalf("payload = %s, want input array", request.Body)
	}
}

func TestGenerateEmbeddingsProviderErrorIsTypedAndRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_123")
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key sk-secret123","code":"unauthorized"}}`)
	}))
	t.Cleanup(server.Close)

	client := openAIEmbeddingsTestClient(t, server.URL)
	response, err := client.Embed(context.Background(), openAIEmbeddingModel(), sigma.EmbeddingRequest{Inputs: []string{"hi"}})
	if err == nil {
		t.Fatal("Embed returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if len(response.Vectors) != 0 {
		t.Fatalf("vectors = %#v, want none", response.Vectors)
	}
	var providerErr *sigma.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *sigma.ProviderError", err)
	}
	if got, want := providerErr.API, sigma.API(sigma.EmbeddingAPIOpenAIEmbeddings); got != want {
		t.Fatalf("provider error API = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "sk-secret123") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestGenerateEmbeddingsDebugHooksAreRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "embedding=secret")
		_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`)
	}))
	t.Cleanup(server.Close)

	var payloadDebug sigma.EmbeddingPayloadDebug
	var responseDebug sigma.EmbeddingResponseDebug
	client := openAIEmbeddingsTestClient(t, server.URL)
	_, err := client.Embed(
		context.Background(),
		openAIEmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"sk-proj-promptsecret"}},
		sigma.WithEmbeddingAPIKey("sk-proj-embeddingkey"),
		sigma.WithEmbeddingPayloadDebugHook(func(_ context.Context, debug sigma.EmbeddingPayloadDebug) error {
			payloadDebug = debug
			return nil
		}),
		sigma.WithEmbeddingResponseDebugHook(func(_ context.Context, debug sigma.EmbeddingResponseDebug) error {
			responseDebug = debug
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if payloadDebug.Provider != sigma.ProviderOpenAI || payloadDebug.API != sigma.EmbeddingAPIOpenAIEmbeddings || payloadDebug.Model != openAIEmbeddingModel().ID {
		t.Fatalf("payload debug metadata = %#v, want embedding metadata", payloadDebug)
	}
	if got := payloadDebug.Headers.Get("Authorization"); got != "[redacted]" {
		t.Fatalf("authorization header = %q, want redacted", got)
	}
	if strings.Contains(payloadDebug.PayloadPreview, "sk-proj-promptsecret") {
		t.Fatalf("payload preview leaked secret: %q", payloadDebug.PayloadPreview)
	}
	if responseDebug.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want 200", responseDebug.StatusCode)
	}
	if got := responseDebug.Headers.Get("Set-Cookie"); got != "[redacted]" {
		t.Fatalf("response Set-Cookie = %q, want redacted", got)
	}
}

func openAIEmbeddingsTestClient(t *testing.T, baseURL string, opts ...openai.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]openai.ProviderOption{openai.WithBaseURL(baseURL), openai.WithHeader("X-Client", "client")}, opts...)
	if err := openai.RegisterEmbeddings(registry, sigma.ProviderOpenAI, providerOpts...); err != nil {
		t.Fatalf("RegisterEmbeddings returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModel(openAIEmbeddingModel()); err != nil {
		t.Fatalf("RegisterEmbeddingModel returned error: %v", err)
	}
	return sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "test-key", Source: "test"}, nil
		})),
	)
}

func openAIEmbeddingModel() sigma.EmbeddingModel {
	return sigma.EmbeddingModel{
		ID:                  "text-embedding-3-small",
		Provider:            sigma.ProviderOpenAI,
		API:                 sigma.EmbeddingAPIOpenAIEmbeddings,
		Name:                "Text Embedding 3 Small",
		DefaultDimensions:   1536,
		MaxInputTokens:      8192,
		InputCostPerMillion: 0.02,
		CostCurrency:        "USD",
		ProviderMetadata: map[string]any{
			"headers": map[string]string{},
		},
	}
}
