// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package githubcopilot_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/githubcopilot"
)

func TestGeneratedFableUsesChatCompletionsWithCopilotHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeChatCompleted(w)
	}))
	t.Cleanup(server.Close)

	registry := sigma.DefaultRegistry()
	model, ok := registry.Model(sigma.ProviderGitHubCopilot, "claude-fable-5")
	if !ok {
		t.Fatal("default registry missing GitHub Copilot Claude Fable 5 model")
	}
	if err := githubcopilot.Register(registry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserContent(sigma.Text("hi"), sigma.ImageBase64("image/png", "aGk="))}},
		sigma.WithAPIKey("copilot-token"),
		sigma.WithProviderOption(sigma.ProviderGitHubCopilot, "baseURL", server.URL),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/chat/completions"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer copilot-token")
	assertHeader(t, request.Headers, "User-Agent", "GitHubCopilotChat/0.35.0")
	assertHeader(t, request.Headers, "X-Initiator", "user")
	assertHeader(t, request.Headers, "Openai-Intent", "conversation-edits")
	assertHeader(t, request.Headers, "Copilot-Vision-Request", "true")
}

func TestResponsesWrapperSendsCopilotHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesCompleted(w)
	}))
	t.Cleanup(server.Close)

	model := copilotResponsesModel()
	registry := sigma.NewRegistry()
	if err := githubcopilot.RegisterResponses(registry, githubcopilot.WithBaseURL(server.URL)); err != nil {
		t.Fatalf("RegisterResponses returned error: %v", err)
	}
	registerModel(t, registry, model)

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("copilot-token"),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer copilot-token")
	assertHeader(t, request.Headers, "X-Initiator", "user")
	assertHeader(t, request.Headers, "Openai-Intent", "conversation-edits")
}

func TestAnthropicWrapperUsesBearerAuthAndCopilotHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeAnthropicCompleted(w)
	}))
	t.Cleanup(server.Close)

	model := copilotAnthropicModel()
	registry := sigma.NewRegistry()
	if err := githubcopilot.RegisterAnthropic(registry, githubcopilot.WithAnthropicBaseURL(server.URL+"/v1")); err != nil {
		t.Fatalf("RegisterAnthropic returned error: %v", err)
	}
	registerModel(t, registry, model)

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{
			sigma.UserContent(sigma.Text("inspect"), sigma.ImageBase64("image/png", "aGk=")),
		}},
		sigma.WithAPIKey("copilot-token"),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/v1/messages"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer copilot-token")
	assertHeader(t, request.Headers, "X-Api-Key", "")
	assertHeader(t, request.Headers, "X-Initiator", "user")
	assertHeader(t, request.Headers, "Openai-Intent", "conversation-edits")
	assertHeader(t, request.Headers, "Copilot-Vision-Request", "true")
}

func copilotResponsesModel() sigma.Model {
	return sigma.Model{
		ID:              "gpt-test",
		Provider:        sigma.ProviderGitHubCopilot,
		API:             sigma.APIOpenAIResponses,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
	}
}

func copilotAnthropicModel() sigma.Model {
	return sigma.Model{
		ID:              "claude-test",
		Provider:        sigma.ProviderGitHubCopilot,
		API:             sigma.APIAnthropicMessages,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
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

func writeChatCompleted(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"id":"chatcmpl_test","model":"gpt-test","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`+"\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
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
