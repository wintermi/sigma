// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

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
	"github.com/wintermi/sigma/internal/goldentest"
)

func TestRegisterVertexReportsVertexAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := RegisterVertex(registry, sigma.ProviderGoogleVertex); err != nil {
		t.Fatalf("RegisterVertex returned error: %v", err)
	}
	if err := registry.RegisterModel(vertexTestModel()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIGoogleVertex; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestVertexEndpointBuildsPublisherAndEndpointResources(t *testing.T) {
	t.Parallel()

	provider := NewVertexProvider(WithVertexConfig(VertexConfig{
		ProjectID:  "test-project",
		Location:   "us-central1",
		APIVersion: "v1beta1",
	}))
	model := vertexTestModel()

	endpoint, err := provider.endpoint(model, sigma.Options{}, vertexRequestConfig{
		ProjectID:  "test-project",
		Location:   "us-central1",
		Publisher:  defaultVertexPublisher,
		APIVersion: "v1beta1",
	})
	if err != nil {
		t.Fatalf("endpoint returned error: %v", err)
	}
	want := "https://us-central1-aiplatform.googleapis.com/v1beta1/projects/test-project/locations/us-central1/publishers/google/models/gemini-test:streamGenerateContent?alt=sse"
	if endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}

	endpoint, err = provider.endpoint(model, sigma.Options{}, vertexRequestConfig{
		ProjectID:     "test-project",
		Location:      "global",
		Publisher:     defaultVertexPublisher,
		ModelEndpoint: "tuned-endpoint",
		APIVersion:    "v1",
	})
	if err != nil {
		t.Fatalf("endpoint returned error: %v", err)
	}
	want = "https://aiplatform.googleapis.com/v1/projects/test-project/locations/global/endpoints/tuned-endpoint:streamGenerateContent?alt=sse"
	if endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}
}

func TestVertexCompleteSendsAPIKeyRequest(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedVertexRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureVertexRequest(t, requests, r)
		writeVertexSSE(t, w, vertexCompletedEvent)
	}))
	t.Cleanup(server.Close)

	client, model := vertexTestClient(t,
		WithVertexConfig(VertexConfig{
			ProjectID:  "test-project",
			Location:   "us-central1",
			APIVersion: "v1",
		}),
		WithVertexBaseURL(server.URL+"/v1"),
	)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "ok"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}

	request := receiveVertexRequest(t, requests)
	if got, want := request.Path, "/v1/projects/test-project/locations/us-central1/publishers/google/models/gemini-test:streamGenerateContent"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Query, "alt=sse"; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("X-Goog-Api-Key"), "vertex-api-key"; got != want {
		t.Fatalf("api key header = %q, want %q", got, want)
	}
	if got := request.Headers.Get("Authorization"); got != "" {
		t.Fatalf("authorization header = %q, want empty", got)
	}
	goldentest.AssertJSON(t, request.Body, "provider/google/vertex/basic_payload.json")
}

func TestVertexOmitsFunctionCallIDs(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedVertexRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureVertexRequest(t, requests, r)
		writeVertexSSE(t, w, vertexCompletedEvent)
	}))
	t.Cleanup(server.Close)

	model := vertexTestModel()
	model.ID = "gemini-3.5-flash"
	client := vertexTestClientWithModel(
		t,
		model,
		vertexAPIKeyResolver("vertex-api-key"),
		WithVertexConfig(VertexConfig{
			ProjectID:  "test-project",
			Location:   "us-central1",
			APIVersion: "v1",
		}),
		WithVertexBaseURL(server.URL+"/v1"),
	)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("read"),
			{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_prev", "read", map[string]any{"path": "a.txt"})},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call_prev",
				ToolName:   "read",
				Content:    []sigma.ContentBlock{sigma.Text("alpha")},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(receiveVertexRequest(t, requests).Body), &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	contents := payload["contents"].([]any)
	modelTurn := contents[1].(map[string]any)
	modelParts := modelTurn["parts"].([]any)
	call := modelParts[0].(map[string]any)["functionCall"].(map[string]any)
	if _, ok := call["id"]; ok {
		t.Fatalf("function call id = %v, want omitted", call["id"])
	}
	toolTurn := contents[2].(map[string]any)
	toolParts := toolTurn["parts"].([]any)
	response := toolParts[0].(map[string]any)["functionResponse"].(map[string]any)
	if _, ok := response["id"]; ok {
		t.Fatalf("function response id = %v, want omitted", response["id"])
	}
}

func TestVertexTokenModeUsesTokenProvider(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedVertexRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureVertexRequest(t, requests, r)
		writeVertexSSE(t, w, vertexCompletedEvent)
	}))
	t.Cleanup(server.Close)

	tokenCalls := 0
	client, model := vertexTestClient(t,
		WithVertexConfig(VertexConfig{
			ProjectID:      "test-project",
			Location:       "us-central1",
			CredentialMode: VertexCredentialToken,
		}),
		WithVertexBaseURL(server.URL+"/v1"),
		WithVertexTokenProvider(sigma.OAuthTokenProviderFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
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
	request := receiveVertexRequest(t, requests)
	if got, want := request.Headers.Get("Authorization"), "Bearer vertex-token"; got != want {
		t.Fatalf("authorization header = %q, want %q", got, want)
	}
	if got := request.Headers.Get("X-Goog-Api-Key"); got != "" {
		t.Fatalf("api key header = %q, want empty", got)
	}
}

func TestVertexAutoCredentialPrefersResolvedAPIKeyBeforeTokenProvider(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedVertexRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureVertexRequest(t, requests, r)
		writeVertexSSE(t, w, vertexCompletedEvent)
	}))
	t.Cleanup(server.Close)

	tokenCalls := 0
	client, model := vertexTestClient(t,
		WithVertexConfig(VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		WithVertexBaseURL(server.URL+"/v1"),
		WithVertexTokenProvider(sigma.OAuthTokenProviderFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			tokenCalls++
			return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "vertex-token"}, nil
		})),
	)

	if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if tokenCalls != 0 {
		t.Fatalf("token provider calls = %d, want 0", tokenCalls)
	}
	request := receiveVertexRequest(t, requests)
	if got, want := request.Headers.Get("X-Goog-Api-Key"), "vertex-api-key"; got != want {
		t.Fatalf("api key header = %q, want %q", got, want)
	}
}

func TestVertexAutoCredentialFallsBackToTokenForAPIKeyPlaceholders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "angle bracket marker", value: "<authenticated>"},
		{name: "local sentinel", value: "gcp-vertex-credentials"},
		{name: "blank", value: "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedVertexRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureVertexRequest(t, requests, r)
				writeVertexSSE(t, w, vertexCompletedEvent)
			}))
			t.Cleanup(server.Close)

			tokenCalls := 0
			client, model := vertexTestClientWithAuth(t,
				vertexAPIKeyResolver(tt.value),
				WithVertexConfig(VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
				WithVertexBaseURL(server.URL+"/v1"),
				WithVertexTokenProvider(sigma.OAuthTokenProviderFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
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
			request := receiveVertexRequest(t, requests)
			if got, want := request.Headers.Get("Authorization"), "Bearer vertex-token"; got != want {
				t.Fatalf("authorization header = %q, want %q", got, want)
			}
			if got := request.Headers.Get("X-Goog-Api-Key"); got != "" {
				t.Fatalf("api key header = %q, want empty", got)
			}
		})
	}
}

func TestVertexAPIKeyModeRejectsAPIKeyPlaceholderWithoutTokenFallback(t *testing.T) {
	t.Parallel()

	tokenCalls := 0
	client, model := vertexTestClientWithAuth(t,
		vertexAPIKeyResolver("<authenticated>"),
		WithVertexConfig(VertexConfig{
			ProjectID:      "test-project",
			Location:       "us-central1",
			CredentialMode: VertexCredentialAPIKey,
		}),
		WithVertexBaseURL("https://example.invalid/v1"),
		WithVertexTokenProvider(sigma.OAuthTokenProviderFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			tokenCalls++
			return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "vertex-token"}, nil
		})),
	)

	_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrCredentialUnavailable) {
		t.Fatalf("error = %v, want ErrCredentialUnavailable", err)
	}
	if tokenCalls != 0 {
		t.Fatalf("token provider calls = %d, want 0", tokenCalls)
	}
}

func TestVertexMissingProjectAndLocationReturnTypedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  VertexConfig
		message string
	}{
		{name: "project", config: VertexConfig{Location: "us-central1"}, message: "project ID is required"},
		{name: "location", config: VertexConfig{ProjectID: "test-project"}, message: "location is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, model := vertexTestClient(t, WithVertexConfig(tt.config), WithVertexBaseURL("https://example.invalid/v1"))
			_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
			if err == nil {
				t.Fatal("Complete returned nil error")
			}
			if !errors.Is(err, sigma.ErrInvalidOptions) {
				t.Fatalf("error = %v, want ErrInvalidOptions", err)
			}
			if !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("error = %v, want %q", err, tt.message)
			}
		})
	}
}

func TestVertexUsesConcreteModelBaseURLAndHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedVertexRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureVertexRequest(t, requests, r)
		writeVertexSSE(t, w, vertexCompletedEvent)
	}))
	t.Cleanup(server.Close)

	model := vertexTestModel()
	model.ProviderMetadata = map[string]any{
		"baseURL": server.URL + "/model-base",
		"headers": map[string]any{
			"Authorization":  "Bearer metadata-secret",
			"X-Goog-Api-Key": "metadata-key",
			"X-Model":        "model",
			"X-Shared":       "model",
		},
	}
	client := vertexTestClientWithModel(
		t,
		model,
		vertexAPIKeyResolver("vertex-api-key"),
		WithVertexConfig(VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		WithVertexHeader("X-Shared", "provider"),
	)

	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithHeaders(map[string]string{"X-Shared": "request"}),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveVertexRequest(t, requests)
	if got, want := request.Path, "/model-base/projects/test-project/locations/us-central1/publishers/google/models/gemini-test:streamGenerateContent"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("X-Goog-Api-Key"), "vertex-api-key"; got != want {
		t.Fatalf("api key header = %q, want %q", got, want)
	}
	if got := request.Headers.Get("Authorization"); got != "" {
		t.Fatalf("authorization header = %q, want empty", got)
	}
	if got, want := request.Headers.Get("X-Model"), "model"; got != want {
		t.Fatalf("model header = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("X-Shared"), "request"; got != want {
		t.Fatalf("shared header = %q, want %q", got, want)
	}
}

func TestVertexIgnoresTemplatedModelBaseURL(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedVertexRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureVertexRequest(t, requests, r)
		writeVertexSSE(t, w, vertexCompletedEvent)
	}))
	t.Cleanup(server.Close)

	model := vertexTestModel()
	model.ProviderMetadata = map[string]any{"baseURL": "https://{location}-aiplatform.googleapis.com"}
	client := vertexTestClientWithModel(
		t,
		model,
		vertexAPIKeyResolver("vertex-api-key"),
		WithVertexConfig(VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		WithVertexBaseURL(server.URL+"/v1"),
	)

	if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveVertexRequest(t, requests)
	if got, want := request.Path, "/v1/projects/test-project/locations/us-central1/publishers/google/models/gemini-test:streamGenerateContent"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestVertexStreamingReusesGoogleParser(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeVertexSSE(t, w, `data: {"responseId":"resp_vertex","modelVersion":"gemini-test-version","candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}
`)
	}))
	t.Cleanup(server.Close)

	client, model := vertexTestClient(t,
		WithVertexConfig(VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		WithVertexBaseURL(server.URL+"/v1"),
	)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	events := collectVertexEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}

	if got, want := vertexEventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindTextStart,
		sigma.EventKindTextDelta,
		sigma.EventKindTextEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.Content[0].Text, "Hello"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if final.Usage == nil || final.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v, want total tokens 5", final.Usage)
	}
}

func TestVertexProviderErrorIsTypedAndRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-goog-request-id", "req_vertex")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key AIzaSyD1234567890123","status":"PERMISSION_DENIED"}}`)
	}))
	t.Cleanup(server.Close)

	client, model := vertexTestClient(t,
		WithVertexConfig(VertexConfig{ProjectID: "test-project", Location: "us-central1"}),
		WithVertexBaseURL(server.URL+"/v1"),
	)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.Diagnostics[0].API, sigma.APIGoogleVertex; got != want {
		t.Fatalf("diagnostic API = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "AIzaSyD1234567890123") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestVertexOptionsDoNotAffectGenerativeAIProvider(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedVertexRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureVertexRequest(t, requests, r)
		writeVertexSSE(t, w, vertexCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-generative-test")
	model := sigma.Model{
		ID:       "gemini-test",
		Provider: providerID,
		API:      sigma.APIGoogleGenerativeAI,
	}
	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(providerID, NewProvider(WithBaseURL(server.URL))); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(vertexAPIKeyResolver("google-api-key")),
	)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithProviderOptions(sigma.ProviderGoogleVertex, map[string]any{
			providerOptionEndpoint: "https://example.invalid/should-not-be-used",
			vertexOptionProjectID:  "wrong-project",
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveVertexRequest(t, requests)
	if got, want := request.Path, "/models/gemini-test:streamGenerateContent"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

type capturedVertexRequest struct {
	Path    string
	Query   string
	Headers http.Header
	Body    string
}

func vertexTestClient(t *testing.T, opts ...VertexProviderOption) (*sigma.Client, sigma.Model) {
	t.Helper()

	model := vertexTestModel()
	return vertexTestClientWithModel(t, model, vertexAPIKeyResolver("vertex-api-key"), opts...), model
}

func vertexTestClientWithAuth(t *testing.T, resolver sigma.AuthResolver, opts ...VertexProviderOption) (*sigma.Client, sigma.Model) {
	t.Helper()

	model := vertexTestModel()
	return vertexTestClientWithModel(t, model, resolver, opts...), model
}

func vertexTestClientWithModel(t *testing.T, model sigma.Model, resolver sigma.AuthResolver, opts ...VertexProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(sigma.ProviderGoogleVertex, NewVertexProvider(opts...)); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(resolver),
	)
	return client
}

func vertexTestModel() sigma.Model {
	return sigma.Model{
		ID:              "gemini-test",
		Provider:        sigma.ProviderGoogleVertex,
		API:             sigma.APIGoogleVertex,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:   true,
	}
}

func vertexAPIKeyResolver(value string) sigma.AuthResolver {
	return sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: value}, nil
	})
}

func captureVertexRequest(t *testing.T, requests chan<- capturedVertexRequest, r *http.Request) {
	t.Helper()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("ReadAll request body returned error: %v", err)
	}
	requests <- capturedVertexRequest{
		Path:    r.URL.Path,
		Query:   r.URL.RawQuery,
		Headers: r.Header.Clone(),
		Body:    string(body),
	}
}

func receiveVertexRequest(t *testing.T, requests <-chan capturedVertexRequest) capturedVertexRequest {
	t.Helper()

	select {
	case request := <-requests:
		return request
	default:
		t.Fatal("server did not receive request")
		return capturedVertexRequest{}
	}
}

func writeVertexSSE(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, body)
}

func collectVertexEvents(t *testing.T, stream *sigma.Stream) []sigma.Event {
	t.Helper()

	var events []sigma.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events
}

func vertexEventKinds(events []sigma.Event) []sigma.EventKind {
	kinds := make([]sigma.EventKind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	return kinds
}

const vertexCompletedEvent = `data: {"responseId":"resp_complete","candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}
`
