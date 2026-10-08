// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

func TestCodexResponsesResolvesCodexEndpointFromBaseURL(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"", "/", "/codex", "/codex/", "/codex/responses"} {
		t.Run("base"+suffix, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("codex-endpoint-test")
			model := codexResponsesTestModel(providerID)
			client := codexResponsesTestClient(t, providerID, model, server.URL+suffix, codexTokenProvider("codex-oauth-token"))

			if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}
			if got, want := receiveRequest(t, requests).Path, "/codex/responses"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
		})
	}
}

func TestCodexResponsesGeneratedModelsUseChatGPTCodexEndpoint(t *testing.T) {
	t.Parallel()

	var model sigma.Model
	for _, candidate := range sigma.Models() {
		if candidate.Provider == sigma.ProviderOpenAICodex {
			model = candidate
			break
		}
	}
	if model.ID == "" {
		t.Fatal("no generated openai-codex model")
	}

	urls := make(chan string, 1)
	httpClient := &http.Client{Transport: deferredRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		urls <- request.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(responsesCompletedEvent + "\n\n")),
			Request:    request,
		}, nil
	})}

	registry := sigma.NewRegistry()
	if err := openai.RegisterCodexResponses(registry, sigma.ProviderOpenAICodex); err != nil {
		t.Fatalf("RegisterCodexResponses returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithRequestHTTPClient(httpClient),
		sigma.WithTransport(sigma.TransportSSE),
		openai.WithCodexResponsesOAuthTokenProvider(sigma.ProviderOpenAICodex, codexTokenProvider("codex-oauth-token")),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := <-urls, "https://chatgpt.com/backend-api/codex/responses"; got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}
