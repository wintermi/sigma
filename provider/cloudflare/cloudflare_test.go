// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package cloudflare_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/cloudflare"
)

func TestAIGatewayResponsesResolvesPlaceholdersAndUsesGatewayAuth(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesCompleted(w)
	}))
	t.Cleanup(server.Close)

	model := cloudflareResponsesModel()
	registry := sigma.NewRegistry()
	if err := cloudflare.RegisterAIGatewayResponses(
		registry,
		cloudflare.WithBaseURL(server.URL+"/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai"),
	); err != nil {
		t.Fatalf("RegisterAIGatewayResponses returned error: %v", err)
	}
	registerModel(t, registry, model)

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("cf-token"),
		cloudflare.WithAIGatewayAccountID("account"),
		cloudflare.WithAIGatewayID("gateway"),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/account/gateway/openai/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "cf-aig-authorization", "Bearer cf-token")
	assertHeader(t, request.Headers, "Authorization", "")
}

func TestAIGatewayAnthropicResolvesPlaceholdersAndUsesGatewayAuth(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeAnthropicCompleted(w)
	}))
	t.Cleanup(server.Close)

	model := cloudflareAnthropicModel()
	registry := sigma.NewRegistry()
	if err := cloudflare.RegisterAIGatewayAnthropic(
		registry,
		cloudflare.WithAnthropicBaseURL(server.URL+"/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic"),
	); err != nil {
		t.Fatalf("RegisterAIGatewayAnthropic returned error: %v", err)
	}
	registerModel(t, registry, model)

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("cf-token"),
		cloudflare.WithAIGatewayAccountID("account"),
		cloudflare.WithAIGatewayID("gateway"),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/account/gateway/anthropic/messages"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "cf-aig-authorization", "Bearer cf-token")
	assertHeader(t, request.Headers, "X-Api-Key", "")
}

func TestAIGatewayResponsesFallsBackToEnvironmentPlaceholders(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "env-account")
	t.Setenv("CLOUDFLARE_GATEWAY_ID", "env-gateway")

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesCompleted(w)
	}))
	t.Cleanup(server.Close)

	model := cloudflareResponsesModel()
	registry := sigma.NewRegistry()
	if err := cloudflare.RegisterAIGatewayResponses(
		registry,
		cloudflare.WithBaseURL(server.URL+"/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai"),
	); err != nil {
		t.Fatalf("RegisterAIGatewayResponses returned error: %v", err)
	}
	registerModel(t, registry, model)

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("cf-token"),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/env-account/env-gateway/openai/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestAIGatewayResponsesReportsMissingPlaceholderBeforeNetwork(t *testing.T) {
	model := cloudflareResponsesModel()
	registry := sigma.NewRegistry()
	if err := cloudflare.RegisterAIGatewayResponses(
		registry,
		cloudflare.WithBaseURL("https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_MISSING_TEST_ID}/openai"),
	); err != nil {
		t.Fatalf("RegisterAIGatewayResponses returned error: %v", err)
	}
	registerModel(t, registry, model)

	client := sigma.NewClient(sigma.WithRegistry(registry))
	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("cf-token"),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !strings.Contains(err.Error(), "CLOUDFLARE_MISSING_TEST_ID is required") {
		t.Fatalf("error = %v, want missing placeholder", err)
	}
}

func cloudflareResponsesModel() sigma.Model {
	return sigma.Model{
		ID:              "gpt-test",
		Provider:        sigma.ProviderCloudflareAIGateway,
		API:             sigma.APIOpenAIResponses,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
	}
}

func cloudflareAnthropicModel() sigma.Model {
	return sigma.Model{
		ID:              "claude-test",
		Provider:        sigma.ProviderCloudflareAIGateway,
		API:             sigma.APIAnthropicMessages,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
	}
}

func registerModel(t *testing.T, registry *sigma.Registry, model sigma.Model) {
	t.Helper()

	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
}

type capturedRequest struct {
	Path    string
	Headers http.Header
}

func captureRequest(t *testing.T, ch chan<- capturedRequest, r *http.Request) {
	t.Helper()

	_, _ = io.Copy(io.Discard, r.Body)
	ch <- capturedRequest{Path: r.URL.Path, Headers: r.Header.Clone()}
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

func assertHeader(t *testing.T, headers http.Header, key string, want string) {
	t.Helper()

	if got := headers.Get(key); got != want {
		t.Fatalf("%s header = %q, want %q", key, got, want)
	}
}

func writeResponsesCompleted(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"id":"resp_test","model":"gpt-test","status":"completed","output":[{"type":"message","id":"msg_test","role":"assistant","content":[{"type":"output_text","id":"text_test","text":"ok"}]}]}}`+"\n\n")
}

func writeAnthropicCompleted(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[]}}`+"\n\n")
	_, _ = io.WriteString(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
	_, _ = io.WriteString(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`+"\n\n")
	_, _ = io.WriteString(w, `data: {"type":"content_block_stop","index":0}`+"\n\n")
	_, _ = io.WriteString(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
	_, _ = io.WriteString(w, `data: {"type":"message_stop"}`+"\n\n")
}
