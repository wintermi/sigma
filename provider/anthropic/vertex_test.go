// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
	"github.com/wintermi/sigma/provider/anthropic"
)

func TestRegisterVertexReportsMessagesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := anthropic.RegisterVertex(registry, sigma.ProviderGoogleVertexAnthropic); err != nil {
		t.Fatalf("RegisterVertex returned error: %v", err)
	}
	if err := registry.RegisterModel(vertexAnthropicTestModel()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIAnthropicMessages; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestVertexAnthropicCompleteSendsAPIKeyRequest(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	model := vertexAnthropicTestModel()
	model.ProviderMetadata = map[string]any{
		"headers": map[string]string{
			"Authorization": "Bearer metadata-secret",
			"X-Model":       "model",
			"X-Shared":      "model",
		},
	}
	client := vertexAnthropicTestClient(
		t,
		model,
		vertexAnthropicCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"),
		anthropic.WithVertexConfig(anthropic.VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		anthropic.WithVertexBaseURL(server.URL+"/v1"),
		anthropic.WithVertexHeader("X-Shared", "provider"),
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
	if got, want := final.Content[0].Text, "ok"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/v1/projects/test-project/locations/us-central1/publishers/anthropic/models/claude-sonnet-4@20250514:streamRawPredict"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "X-Goog-Api-Key", "vertex-api-key")
	assertHeader(t, request.Headers, "Authorization", "")
	assertHeader(t, request.Headers, "Anthropic-Version", "")
	assertHeader(t, request.Headers, "X-Model", "model")
	assertHeader(t, request.Headers, "X-Shared", "request")
	goldentest.AssertJSON(t, []byte(request.Body), "provider/anthropic/vertex/basic_payload.json")
}

func TestVertexAnthropicEndpointSupportsPublisherGlobalAndOverride(t *testing.T) {
	t.Parallel()

	t.Run("global publisher", func(t *testing.T) {
		t.Parallel()

		requests := make(chan capturedRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captureRequest(t, requests, r)
			writeMessagesSSE(t, w, completedEvent)
		}))
		t.Cleanup(server.Close)

		model := vertexAnthropicTestModel()
		client := vertexAnthropicTestClient(
			t,
			model,
			vertexAnthropicCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"),
			anthropic.WithVertexConfig(anthropic.VertexConfig{
				ProjectID:  "test-project",
				Location:   "global",
				Publisher:  "anthropic",
				APIVersion: "v1",
			}),
			anthropic.WithVertexBaseURL(server.URL+"/v1"),
		)

		if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}); err != nil {
			t.Fatalf("Complete returned error: %v", err)
		}
		request := receiveRequest(t, requests)
		if got, want := request.Path, "/v1/projects/test-project/locations/global/publishers/anthropic/models/claude-sonnet-4@20250514:streamRawPredict"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
	})

	t.Run("full endpoint override", func(t *testing.T) {
		t.Parallel()

		requests := make(chan capturedRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captureRequest(t, requests, r)
			writeMessagesSSE(t, w, completedEvent)
		}))
		t.Cleanup(server.Close)

		model := vertexAnthropicTestModel()
		client := vertexAnthropicTestClient(t, model, vertexAnthropicCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"))

		if _, err := client.Complete(
			context.Background(),
			model,
			sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			sigma.WithProviderOptions(model.Provider, map[string]any{"endpoint": server.URL + "/custom/raw"}),
		); err != nil {
			t.Fatalf("Complete returned error: %v", err)
		}
		request := receiveRequest(t, requests)
		if got, want := request.Path, "/custom/raw"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
	})
}

func TestVertexAnthropicAutoCredentialFallsBackToTokenForPlaceholders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	tokenCalls := 0
	model := vertexAnthropicTestModel()
	client := vertexAnthropicTestClient(
		t,
		model,
		vertexAnthropicCredentialResolver(sigma.CredentialTypeAPIKey, "<authenticated>"),
		anthropic.WithVertexConfig(anthropic.VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		anthropic.WithVertexBaseURL(server.URL+"/v1"),
		anthropic.WithVertexTokenProvider(sigma.OAuthTokenProviderFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
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
	assertHeader(t, request.Headers, "X-Goog-Api-Key", "")
}

func TestVertexAnthropicTokenModeRequiresOAuthCredential(t *testing.T) {
	t.Parallel()

	model := vertexAnthropicTestModel()
	client := vertexAnthropicTestClient(
		t,
		model,
		vertexAnthropicCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"),
		anthropic.WithVertexConfig(anthropic.VertexConfig{
			ProjectID:      "test-project",
			Location:       "us-central1",
			CredentialMode: anthropic.VertexCredentialToken,
		}),
		anthropic.WithVertexBaseURL("https://example.invalid/v1"),
	)

	_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
}

func TestVertexAnthropicStreamingReusesMessagesParser(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	model := vertexAnthropicTestModel()
	client := vertexAnthropicTestClient(
		t,
		model,
		vertexAnthropicCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"),
		anthropic.WithVertexConfig(anthropic.VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		anthropic.WithVertexBaseURL(server.URL+"/v1"),
	)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}

	if got, want := eventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindTextStart,
		sigma.EventKindTextDelta,
		sigma.EventKindTextEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.Content[0].Text, "ok"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func vertexAnthropicTestClient(t *testing.T, model sigma.Model, resolver sigma.AuthResolver, opts ...anthropic.VertexProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(model.Provider, anthropic.NewVertexProvider(opts...)); err != nil {
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

func vertexAnthropicTestModel() sigma.Model {
	return sigma.Model{
		ID:            "claude-sonnet-4@20250514",
		Provider:      sigma.ProviderGoogleVertexAnthropic,
		API:           sigma.APIAnthropicMessages,
		SupportsTools: true,
	}
}

func vertexAnthropicCredentialResolver(typ sigma.CredentialType, value string) sigma.AuthResolver {
	return sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: typ, Value: value}, nil
	})
}

func TestVertexAnthropicOverrideVersionInBody(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMessagesSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	model := vertexAnthropicTestModel()
	client := vertexAnthropicTestClient(
		t,
		model,
		vertexAnthropicCredentialResolver(sigma.CredentialTypeAPIKey, "vertex-api-key"),
		anthropic.WithVertexConfig(anthropic.VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		anthropic.WithVertexBaseURL(server.URL+"/v1"),
	)

	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithProviderOptions(model.Provider, map[string]any{"anthropic_version": "vertex-test-version"}),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	body := receiveRequest(t, requests).Body
	if !strings.Contains(body, `"anthropic_version":"vertex-test-version"`) {
		t.Fatalf("body = %s, want overridden anthropic_version", body)
	}
}
