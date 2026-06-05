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
	"github.com/wintermi/sigma/internal/goldentest"
	"github.com/wintermi/sigma/provider/openai"
)

func TestRegisterVertexReportsChatCompletionsAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := openai.RegisterVertex(registry, sigma.ProviderGoogleVertexOpenAI); err != nil {
		t.Fatalf("RegisterVertex returned error: %v", err)
	}
	if err := registry.RegisterModel(vertexOpenAITestModel()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestVertexOpenAICompleteSendsAPIKeyRequest(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	model := vertexOpenAITestModel()
	model.ProviderMetadata = map[string]any{
		"headers": map[string]string{
			"Authorization": "Bearer metadata-secret",
			"X-Model":       "model",
			"X-Shared":      "model",
		},
	}
	client := vertexOpenAITestClient(
		t,
		model,
		vertexCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"),
		openai.WithVertexConfig(openai.VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		openai.WithVertexBaseURL(server.URL+"/v1beta1"),
		openai.WithVertexHeader("X-Shared", "provider"),
	)

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithMaxTokens(64),
		sigma.WithHeader("X-Shared", "request"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "Hello world"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/v1beta1/projects/test-project/locations/us-central1/endpoints/openapi/chat/completions"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "X-Goog-Api-Key", "vertex-api-key")
	assertHeaderAbsent(t, request.Headers, "Authorization")
	assertHeader(t, request.Headers, "X-Model", "model")
	assertHeader(t, request.Headers, "X-Shared", "request")
	goldentest.AssertJSON(t, request.Body, "provider/openai/vertex/basic_payload.json")
}

func TestVertexOpenAIEndpointSupportsGlobalAndOverride(t *testing.T) {
	t.Parallel()

	t.Run("global", func(t *testing.T) {
		t.Parallel()

		requests := make(chan capturedRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captureRequest(t, requests, r)
			writeFixture(t, w, "text_usage.sse")
		}))
		t.Cleanup(server.Close)

		model := vertexOpenAITestModel()
		client := vertexOpenAITestClient(
			t,
			model,
			vertexCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"),
			openai.WithVertexConfig(openai.VertexConfig{ProjectID: "test-project", Location: "global", APIVersion: "v1"}),
			openai.WithVertexBaseURL(server.URL+"/v1"),
		)

		if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}); err != nil {
			t.Fatalf("Complete returned error: %v", err)
		}
		request := receiveRequest(t, requests)
		if got, want := request.Path, "/v1/projects/test-project/locations/global/endpoints/openapi/chat/completions"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
	})

	t.Run("full endpoint override", func(t *testing.T) {
		t.Parallel()

		requests := make(chan capturedRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captureRequest(t, requests, r)
			writeFixture(t, w, "text_usage.sse")
		}))
		t.Cleanup(server.Close)

		model := vertexOpenAITestModel()
		client := vertexOpenAITestClient(t, model, vertexCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"))

		if _, err := client.Complete(
			context.Background(),
			model,
			sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			sigma.WithProviderOptions(model.Provider, map[string]any{"endpoint": server.URL + "/custom/chat/completions"}),
		); err != nil {
			t.Fatalf("Complete returned error: %v", err)
		}
		request := receiveRequest(t, requests)
		if got, want := request.Path, "/custom/chat/completions"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
	})
}

func TestVertexOpenAIAutoCredentialFallsBackToTokenForPlaceholders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	t.Cleanup(server.Close)

	tokenCalls := 0
	model := vertexOpenAITestModel()
	client := vertexOpenAITestClient(
		t,
		model,
		vertexCredentialResolver(sigma.CredentialTypeAPIKey, "gcp-vertex-credentials"),
		openai.WithVertexConfig(openai.VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		openai.WithVertexBaseURL(server.URL+"/v1beta1"),
		openai.WithVertexTokenProvider(sigma.OAuthTokenProviderFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			tokenCalls++
			return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "vertex-token"}, nil
		})),
	)

	if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token provider calls = %d, want 1", tokenCalls)
	}
	request := receiveRequest(t, requests)
	assertHeader(t, request.Headers, "Authorization", "Bearer vertex-token")
	assertHeaderAbsent(t, request.Headers, "X-Goog-Api-Key")
}

func TestVertexOpenAITokenModeRequiresOAuthCredential(t *testing.T) {
	t.Parallel()

	model := vertexOpenAITestModel()
	client := vertexOpenAITestClient(
		t,
		model,
		vertexCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"),
		openai.WithVertexConfig(openai.VertexConfig{
			ProjectID:      "test-project",
			Location:       "us-central1",
			CredentialMode: openai.VertexCredentialToken,
		}),
		openai.WithVertexBaseURL("https://example.invalid/v1beta1"),
	)

	_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
}

func TestVertexOpenAIStreamingReusesChatCompletionsParser(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_vertex","model":"meta/llama","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	model := vertexOpenAITestModel()
	client := vertexOpenAITestClient(
		t,
		model,
		vertexCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"),
		openai.WithVertexConfig(openai.VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		openai.WithVertexBaseURL(server.URL+"/v1beta1"),
	)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "Hello"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if final.Usage == nil || final.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v, want total tokens 5", final.Usage)
	}
}

func vertexOpenAITestClient(t *testing.T, model sigma.Model, resolver sigma.AuthResolver, opts ...openai.VertexProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(model.Provider, openai.NewVertexProvider(opts...)); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	return sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(resolver),
		sigma.WithDefaultHeader("X-Client", "client"),
	)
}

func vertexOpenAITestModel() sigma.Model {
	return sigma.Model{
		ID:            "meta/llama-3.3-70b-instruct-maas",
		Provider:      sigma.ProviderGoogleVertexOpenAI,
		API:           sigma.APIOpenAICompletions,
		SupportsTools: true,
	}
}

func vertexCredentialResolver(typ sigma.CredentialType, value string) sigma.AuthResolver {
	return sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: typ, Value: value}, nil
	})
}
