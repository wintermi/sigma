// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package azure_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/azure"
)

func TestRegisterReportsAzureResponsesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := azure.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(azureTestModel()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := len(providers), 1; got != want {
		t.Fatalf("providers length = %d, want %d", got, want)
	}
	if got, want := providers[0].ID, sigma.ProviderAzureOpenAIResponses; got != want {
		t.Fatalf("provider ID = %q, want %q", got, want)
	}
	if got, want := providers[0].TextAPI, sigma.APIAzureOpenAIResponses; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestRegisterRejectsNilRegistry(t *testing.T) {
	t.Parallel()

	err := azure.Register(nil)
	if err == nil {
		t.Fatal("Register returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if got, want := sigmaErr.Code, sigma.ErrorUnsupported; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
}

func TestProviderScopedOptionsConfigureAzureResponsesRequest(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	var gotScopes []string
	client := azureTestClient(t, azure.WithHeader("X-Provider", "provider"))
	_, err := client.Complete(
		context.Background(),
		azureTestModel(),
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		azure.WithEndpoint(server.URL),
		azure.WithDeployment("request-deployment"),
		azure.WithAPIVersion("2025-04-01-preview"),
		azure.WithCredentialSource(azure.CredentialSourceToken),
		azure.WithTokenCredential(azure.AzureTokenCredentialFunc(
			func(_ context.Context, req azure.AzureTokenRequest) (azure.AzureAccessToken, error) {
				gotScopes = append([]string(nil), req.Scopes...)
				return azure.AzureAccessToken{Token: "entra-token"}, nil
			},
		)),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/openai/v1/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Query.Get("api-version"), "2025-04-01-preview"; got != want {
		t.Fatalf("api-version = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer entra-token")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	if got := request.Headers.Get("api-key"); got != "" {
		t.Fatalf("api-key header = %q, want empty", got)
	}
	if got, want := gotScopes, []string{"https://cognitiveservices.azure.com/.default"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("token scopes = %v, want %v", got, want)
	}

	var body map[string]any
	if err := json.Unmarshal(request.Body, &body); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := body["model"], "request-deployment"; got != want {
		t.Fatalf("model payload = %v, want %v", got, want)
	}
	if got, want := body["stream"], true; got != want {
		t.Fatalf("stream payload = %v, want %v", got, want)
	}
}

func azureTestClient(t *testing.T, opts ...azure.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := azure.Register(registry, opts...); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(azureTestModel()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	return sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithDefaultHeader("X-Client", "client"),
	)
}

func azureTestModel() sigma.Model {
	return sigma.Model{
		ID:              "gpt-test",
		Provider:        sigma.ProviderAzureOpenAIResponses,
		API:             sigma.APIAzureOpenAIResponses,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
		AzureOpenAIResponses: &sigma.AzureOpenAIResponsesConfig{
			Endpoint:   "https://azure-openai.example",
			Deployment: "deployment-test",
			APIVersion: "preview",
		},
	}
}

type capturedRequest struct {
	Method  string
	Path    string
	Query   url.Values
	Headers http.Header
	Body    []byte
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
		Query:   r.URL.Query(),
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
		t.Fatalf("header %s = %q, want %q", key, got, want)
	}
}

const responsesCompletedEvent = `data: {"type":"response.completed","response":{"id":"resp_complete","model":"gpt-test","status":"completed","output":[{"type":"message","id":"msg_complete","role":"assistant","content":[{"type":"output_text","id":"text_complete","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}

`
