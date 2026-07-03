// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
	"github.com/wintermi/sigma/provider/openai"
)

func TestRegisterAzureResponsesReportsAzureResponsesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("azure-openai-responses")
	if err := openai.RegisterAzureResponses(registry, providerID); err != nil {
		t.Fatalf("RegisterAzureResponses returned error: %v", err)
	}
	if err := registry.RegisterModel(azureResponsesTestModel(providerID)); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIAzureOpenAIResponses; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestAzureResponsesCompleteSendsDeploymentURLHeadersAndPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan azureCapturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureAzureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("azure-responses-payload-test")
	model := azureResponsesTestModel(providerID)
	model.AzureOpenAIResponses.Endpoint = "https://model-endpoint.example"
	model.AzureOpenAIResponses.Deployment = "model-deployment"
	model.AzureOpenAIResponses.APIVersion = "model-version"
	client := azureResponsesTestClient(t, providerID, model, azureAPIKeyResolver("resolved-key"))

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			SystemPrompt: "You are helpful.",
			Messages:     []sigma.Message{sigma.UserText("hi")},
		},
		sigma.WithMaxTokens(25),
		sigma.WithHeader("X-Custom", "custom"),
		openai.WithAzureResponsesEndpoint(providerID, server.URL),
		openai.WithAzureResponsesDeployment(providerID, "request-deployment"),
		openai.WithAzureResponsesAPIVersion(providerID, "2025-04-01-preview"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.ProviderMetadata["id"], "resp_complete"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}

	request := receiveAzureRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/openai/v1/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Query.Get("api-version"), "2025-04-01-preview"; got != want {
		t.Fatalf("api-version = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "api-key", "resolved-key")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	if got := request.Headers.Get("Authorization"); got != "" {
		t.Fatalf("Authorization header = %q, want empty", got)
	}
	goldentest.AssertJSON(t, request.Body, "provider/openai/azure_responses/basic_payload.json")
}

func TestAzureResponsesDoesNotUseSessionIDAsPreviousResponseID(t *testing.T) {
	t.Parallel()

	requests := make(chan azureCapturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureAzureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("azure-responses-session-test")
	model := azureResponsesTestModel(providerID)
	model.AzureOpenAIResponses.Endpoint = server.URL
	client := azureResponsesTestClient(t, providerID, model, azureAPIKeyResolver("resolved-key"))

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID("session-123"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receiveAzureRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if _, ok := payload["previous_response_id"]; ok {
		t.Fatalf("previous_response_id was sent from session id: %#v", payload)
	}
}

func TestAzureResponsesUsesTokenCredentialHeader(t *testing.T) {
	t.Parallel()

	requests := make(chan azureCapturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureAzureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	var gotScopes []string
	providerID := sigma.ProviderID("azure-responses-token-test")
	model := azureResponsesTestModel(providerID)
	model.AzureOpenAIResponses.Endpoint = server.URL
	client := azureResponsesTestClient(t, providerID, model, nil)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		openai.WithAzureResponsesCredentialSource(providerID, "token"),
		openai.WithAzureResponsesTokenCredential(providerID, openai.AzureTokenCredentialFunc(
			func(_ context.Context, req openai.AzureTokenRequest) (openai.AzureAccessToken, error) {
				gotScopes = append([]string(nil), req.Scopes...)
				return openai.AzureAccessToken{Token: "entra-token"}, nil
			},
		)),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveAzureRequest(t, requests)
	assertHeader(t, request.Headers, "Authorization", "Bearer entra-token")
	if got := request.Headers.Get("api-key"); got != "" {
		t.Fatalf("api-key header = %q, want empty", got)
	}
	if got, want := gotScopes, []string{"https://cognitiveservices.azure.com/.default"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("token scopes = %v, want %v", got, want)
	}
}

func TestAzureResponsesUsesConfiguredAPIKeyEnvironmentVariable(t *testing.T) {
	t.Setenv("SIGMA_AZURE_OPENAI_TEST_KEY", "env-key")

	requests := make(chan azureCapturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureAzureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("azure-responses-env-test")
	model := azureResponsesTestModel(providerID)
	model.AzureOpenAIResponses.Endpoint = server.URL
	model.AzureOpenAIResponses.APIKeyEnvVar = "SIGMA_AZURE_OPENAI_TEST_KEY"
	client := azureResponsesTestClient(t, providerID, model, nil)

	_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveAzureRequest(t, requests)
	assertHeader(t, request.Headers, "api-key", "env-key")
}

func TestAzureResponsesMissingConfigurationReturnsTypedError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config sigma.AzureOpenAIResponsesConfig
		want   string
	}{
		{
			name:   "endpoint",
			config: sigma.AzureOpenAIResponsesConfig{Deployment: "deploy", APIVersion: "preview"},
			want:   "endpoint is required",
		},
		{
			name:   "deployment",
			config: sigma.AzureOpenAIResponsesConfig{Endpoint: "https://example.test", APIVersion: "preview"},
			want:   "deployment is required",
		},
		{
			name:   "api version",
			config: sigma.AzureOpenAIResponsesConfig{Endpoint: "https://example.test", Deployment: "deploy"},
			want:   "api version is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			providerID := sigma.ProviderID("azure-responses-missing-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := azureResponsesTestModel(providerID)
			model.AzureOpenAIResponses = &tt.config
			client := azureResponsesTestClient(t, providerID, model, azureAPIKeyResolver("unused"))

			_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
			if !errors.Is(err, sigma.ErrInvalidOptions) {
				t.Fatalf("error = %v, want ErrInvalidOptions", err)
			}
			var sigmaErr *sigma.Error
			if !errors.As(err, &sigmaErr) {
				t.Fatalf("error type = %T, want *sigma.Error", err)
			}
			if sigmaErr.Provider != providerID {
				t.Fatalf("error provider = %q, want %q", sigmaErr.Provider, providerID)
			}
			if sigmaErr.Model != model.ID {
				t.Fatalf("error model = %q, want %q", sigmaErr.Model, model.ID)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.want)
			}
		})
	}
}

func TestAzureResponsesProviderErrorUsesAzureAPIAndRedacts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_azure")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key sk-secret123"}}`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("azure-responses-error-test")
	model := azureResponsesTestModel(providerID)
	model.AzureOpenAIResponses.Endpoint = server.URL
	client := azureResponsesTestClient(t, providerID, model, azureAPIKeyResolver("resolved-key"))

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Diagnostics[0].API, sigma.APIAzureOpenAIResponses; got != want {
		t.Fatalf("diagnostic API = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "sk-secret123") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestAzureResponsesModelConfigJSONOmitEmptyAndRoundTrip(t *testing.T) {
	t.Parallel()

	plain := sigma.Model{
		ID:       "gpt-test",
		Provider: sigma.ProviderOpenAI,
		API:      sigma.APIOpenAIResponses,
	}
	data, err := json.Marshal(plain)
	if err != nil {
		t.Fatalf("Marshal plain model returned error: %v", err)
	}
	if strings.Contains(string(data), "azureOpenAIResponses") {
		t.Fatalf("plain model JSON contains Azure config: %s", data)
	}

	azure := azureResponsesTestModel("azure-json-test")
	encoded, err := json.Marshal(azure)
	if err != nil {
		t.Fatalf("Marshal Azure model returned error: %v", err)
	}
	var roundTripped sigma.Model
	if err := json.Unmarshal(encoded, &roundTripped); err != nil {
		t.Fatalf("Unmarshal Azure model returned error: %v", err)
	}
	if roundTripped.AzureOpenAIResponses == nil {
		t.Fatal("round-tripped Azure config was nil")
	}
	if got, want := roundTripped.AzureOpenAIResponses.Deployment, "deployment-test"; got != want {
		t.Fatalf("deployment = %q, want %q", got, want)
	}
}

func azureResponsesTestClient(t *testing.T, providerID sigma.ProviderID, model sigma.Model, resolver sigma.AuthResolver) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(providerID, openai.NewAzureResponsesProvider(openai.WithHeader("X-Provider", "provider"))); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	opts := []sigma.ClientOption{
		sigma.WithRegistry(registry),
		sigma.WithDefaultHeader("X-Client", "client"),
	}
	if resolver != nil {
		opts = append(opts, sigma.WithAuthResolver(resolver))
	}
	return sigma.NewClient(opts...)
}

func azureResponsesTestModel(providerID sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:       "gpt-test",
		Provider: providerID,
		API:      sigma.APIAzureOpenAIResponses,
		SupportedInputs: []sigma.ContentBlockType{
			sigma.ContentBlockText,
			sigma.ContentBlockImage,
		},
		SupportsTools:    true,
		SupportsThinking: true,
		ThinkingLevelMap: map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "high"},
		AzureOpenAIResponses: &sigma.AzureOpenAIResponsesConfig{
			Endpoint:   "https://azure-openai.example",
			Deployment: "deployment-test",
			APIVersion: "preview",
		},
	}
}

func azureAPIKeyResolver(value string) sigma.AuthResolver {
	return sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: value}, nil
	})
}

type azureCapturedRequest struct {
	Method  string
	Path    string
	Query   url.Values
	Headers http.Header
	Body    []byte
}

func captureAzureRequest(t *testing.T, ch chan<- azureCapturedRequest, r *http.Request) {
	t.Helper()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("ReadAll request body returned error: %v", err)
	}
	ch <- azureCapturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Query:   r.URL.Query(),
		Headers: r.Header.Clone(),
		Body:    body,
	}
}

func receiveAzureRequest(t *testing.T, ch <-chan azureCapturedRequest) azureCapturedRequest {
	t.Helper()

	select {
	case request := <-ch:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request")
		return azureCapturedRequest{}
	}
}
