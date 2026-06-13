// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package minimax_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/minimax"
)

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
}

func TestRegisterReportsMessagesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := minimax.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	model := minimaxTestModel(sigma.ProviderMiniMax)
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].ID, sigma.ProviderMiniMax; got != want {
		t.Fatalf("provider ID = %q, want %q", got, want)
	}
	if got, want := providers[0].TextAPI, sigma.APIAnthropicMessages; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestRegisterCNReportsMessagesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := minimax.RegisterCN(registry); err != nil {
		t.Fatalf("RegisterCN returned error: %v", err)
	}
	model := minimaxTestModel(sigma.ProviderMiniMaxCN)
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].ID, sigma.ProviderMiniMaxCN; got != want {
		t.Fatalf("provider ID = %q, want %q", got, want)
	}
	if got, want := providers[0].TextAPI, sigma.APIAnthropicMessages; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestRegisterAcceptsCatalogMiniMaxModel(t *testing.T) {
	t.Parallel()

	model, ok := sigma.DefaultRegistry().Model(sigma.ProviderMiniMax, "MiniMax-M3")
	if !ok {
		t.Fatal("default registry missing MiniMax-M3")
	}
	registry := sigma.NewRegistry()
	if err := minimax.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
}

func TestCompleteUsesConfiguredAnthropicV1BaseURL(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeCompleted(t, w)
	}))
	t.Cleanup(server.Close)

	model := minimaxTestModel(sigma.ProviderMiniMax)
	registry := sigma.NewRegistry()
	if err := minimax.Register(registry, minimax.WithBaseURL(server.URL+"/anthropic/v1")); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "ok"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/anthropic/v1/messages"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("X-Api-Key"), "request-key"; got != want {
		t.Fatalf("X-Api-Key header = %q, want %q", got, want)
	}
}

func minimaxTestModel(provider sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:               "MiniMax-M3",
		Provider:         provider,
		API:              sigma.APIAnthropicMessages,
		Name:             "MiniMax-M3",
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:    true,
		SupportsThinking: true,
	}
}

func captureRequest(t *testing.T, ch chan<- capturedRequest, r *http.Request) {
	t.Helper()

	_, _ = io.Copy(io.Discard, r.Body)
	ch <- capturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
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

func writeCompleted(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_complete","type":"message","role":"assistant","model":"MiniMax-M3","content":[]}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}

data: {"type":"message_stop"}
`)
}
