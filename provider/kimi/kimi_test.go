// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package kimi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/kimi"
)

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

type providerCase struct {
	name     string
	provider sigma.ProviderID
	modelIDs []sigma.ModelID
	register func(*sigma.Registry, ...kimi.ProviderOption) error
}

func providerCases() []providerCase {
	return []providerCase{
		{
			name:     "kimi",
			provider: sigma.ProviderKimi,
			modelIDs: []sigma.ModelID{"kimi-for-coding"},
			register: kimi.Register,
		},
		{
			name:     "kimi coding",
			provider: sigma.ProviderKimiCoding,
			modelIDs: []sigma.ModelID{"k2p7", "k3", "kimi-for-coding", "kimi-for-coding-highspeed", "kimi-k2-thinking"},
			register: kimi.RegisterCoding,
		},
	}
}

func TestRegisterReportsMessagesAPI(t *testing.T) {
	t.Parallel()

	for _, tt := range providerCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := sigma.NewRegistry()
			if err := tt.register(registry); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			model := kimiTestModel(tt.provider)
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}

			providers := registry.ListProviders()
			if got, want := providers[0].ID, tt.provider; got != want {
				t.Fatalf("provider ID = %q, want %q", got, want)
			}
			if got, want := providers[0].TextAPI, sigma.APIAnthropicMessages; got != want {
				t.Fatalf("provider API = %q, want %q", got, want)
			}
		})
	}
}

func TestRegisterAcceptsCatalogModels(t *testing.T) {
	t.Parallel()

	for _, tt := range providerCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, modelID := range tt.modelIDs {
				model, ok := sigma.DefaultRegistry().Model(tt.provider, modelID)
				if !ok {
					t.Fatalf("default registry missing %s model %q", tt.name, modelID)
				}
				registry := sigma.NewRegistry()
				if err := tt.register(registry); err != nil {
					t.Fatalf("register returned error: %v", err)
				}
				if err := registry.RegisterModel(model); err != nil {
					t.Fatalf("RegisterModel(%q) returned error: %v", modelID, err)
				}
			}
		})
	}
}

func TestKimiCodingGeneratedCompatibilityShapesPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modelID    sigma.ModelID
		reasoning  sigma.ThinkingLevel
		wantEffort string
	}{
		{name: "k3", modelID: "k3", reasoning: sigma.ThinkingLevel("max"), wantEffort: "max"},
		{name: "kimi for coding", modelID: "kimi-for-coding", reasoning: sigma.ThinkingLevelMedium, wantEffort: "medium"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeCompleted(t, w)
			}))
			t.Cleanup(server.Close)

			model, ok := sigma.DefaultRegistry().Model(sigma.ProviderKimiCoding, tt.modelID)
			if !ok {
				t.Fatalf("default registry missing Kimi Coding model %q", tt.modelID)
			}
			model.ProviderMetadata = map[string]any{"baseURL": server.URL}
			registry := sigma.NewRegistry()
			if err := kimi.RegisterCoding(registry, kimi.WithBaseURL(server.URL)); err != nil {
				t.Fatalf("RegisterCoding returned error: %v", err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}

			client := sigma.NewClient(sigma.WithRegistry(registry))
			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{
					{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.Thinking("internal reasoning", " ")}},
					sigma.UserText("continue"),
				}},
				sigma.WithAPIKey("request-key"),
				sigma.WithReasoningLevel(tt.reasoning),
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			request := receiveRequest(t, requests)
			var payload map[string]any
			if err := json.Unmarshal(request.Body, &payload); err != nil {
				t.Fatalf("Unmarshal request payload: %v", err)
			}
			thinking, ok := payload["thinking"].(map[string]any)
			if !ok || thinking["type"] != "adaptive" || thinking["display"] != "summarized" {
				t.Fatalf("thinking payload = %#v, want adaptive summarized thinking", payload["thinking"])
			}
			outputConfig, ok := payload["output_config"].(map[string]any)
			if !ok || outputConfig["effort"] != tt.wantEffort {
				t.Fatalf("output_config = %#v, want effort %q", payload["output_config"], tt.wantEffort)
			}
			messages, ok := payload["messages"].([]any)
			if !ok || len(messages) == 0 {
				t.Fatalf("messages payload = %#v, want assistant replay", payload["messages"])
			}
			assistant, ok := messages[0].(map[string]any)
			if !ok {
				t.Fatalf("assistant replay = %#v, want object", messages[0])
			}
			content, ok := assistant["content"].([]any)
			if !ok || len(content) != 1 {
				t.Fatalf("assistant content = %#v, want one thinking block", assistant["content"])
			}
			block, ok := content[0].(map[string]any)
			if !ok || block["type"] != "thinking" || block["signature"] != "" {
				t.Fatalf("assistant thinking replay = %#v, want empty-signature thinking block", content[0])
			}
		})
	}
}

func TestCompleteUsesDefaults(t *testing.T) {
	t.Parallel()

	for _, tt := range providerCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeCompleted(t, w)
			}))
			t.Cleanup(server.Close)

			model := kimiTestModel(tt.provider)
			registry := sigma.NewRegistry()
			if err := tt.register(registry, kimi.WithBaseURL(server.URL+"/coding/v1")); err != nil {
				t.Fatalf("register returned error: %v", err)
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
			if got, want := request.Path, "/coding/v1/messages"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			if got, want := request.Headers.Get("User-Agent"), kimi.DefaultUserAgent; got != want {
				t.Fatalf("User-Agent = %q, want %q", got, want)
			}
			if got, want := request.Headers.Get("X-Api-Key"), "request-key"; got != want {
				t.Fatalf("X-Api-Key header = %q, want %q", got, want)
			}
		})
	}
}

func TestRequestHeaderOverridesDefaultUserAgent(t *testing.T) {
	t.Parallel()

	for _, tt := range providerCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeCompleted(t, w)
			}))
			t.Cleanup(server.Close)

			model := kimiTestModel(tt.provider)
			registry := sigma.NewRegistry()
			if err := tt.register(registry, kimi.WithBaseURL(server.URL)); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry))

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithAPIKey("request-key"),
				sigma.WithHeader("User-Agent", "caller-agent/1.0"),
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			request := receiveRequest(t, requests)
			if got, want := request.Headers.Get("User-Agent"), "caller-agent/1.0"; got != want {
				t.Fatalf("User-Agent = %q, want %q", got, want)
			}
		})
	}
}

func TestProviderErrorsAreTyped(t *testing.T) {
	t.Parallel()

	for _, tt := range providerCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				http.Error(w, `{"error":{"type":"authentication_error","message":"bad key"}}`, http.StatusUnauthorized)
			}))
			t.Cleanup(server.Close)

			model := kimiTestModel(tt.provider)
			registry := sigma.NewRegistry()
			if err := tt.register(registry, kimi.WithBaseURL(server.URL)); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry))

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithAPIKey("bad-key"),
			)
			if err == nil {
				t.Fatal("Complete returned nil error")
			}
			if !errors.Is(err, sigma.ErrProviderResponse) {
				t.Fatalf("error %T does not match ErrProviderResponse: %v", err, err)
			}
			if got, want := sigma.ClassifyError(err).Class, sigma.ErrorClassAuth; got != want {
				t.Fatalf("error class = %q, want %q", got, want)
			}
		})
	}
}

func TestCancellationIsReportedAsAborted(t *testing.T) {
	t.Parallel()

	for _, tt := range providerCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)

			model := kimiTestModel(tt.provider)
			registry := sigma.NewRegistry()
			if err := tt.register(registry, kimi.WithBaseURL(server.URL)); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry))

			ctx, cancel := context.WithCancel(context.Background())
			stream := client.Stream(
				ctx,
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithAPIKey("request-key"),
			)
			defer stream.Close()

			_ = receiveRequest(t, requests)
			cancel()

			timeout := time.After(2 * time.Second)
			for {
				select {
				case event, ok := <-stream.Events():
					if !ok {
						t.Fatal("stream closed before error event")
					}
					if event.Kind != sigma.EventKindError {
						continue
					}
					if got, want := event.StopReason, sigma.StopReasonAborted; got != want {
						t.Fatalf("stop reason = %q, want %q", got, want)
					}
					if !errors.Is(stream.Err(), context.Canceled) {
						t.Fatalf("stream error %T does not match context.Canceled: %v", stream.Err(), stream.Err())
					}
					return
				case <-timeout:
					t.Fatal("timed out waiting for cancellation error event")
				}
			}
		})
	}
}

func kimiTestModel(provider sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:               "kimi-for-coding",
		Provider:         provider,
		API:              sigma.APIAnthropicMessages,
		Name:             "Kimi For Coding",
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:    true,
		SupportsThinking: true,
	}
}

func captureRequest(t *testing.T, ch chan<- capturedRequest, r *http.Request) {
	t.Helper()

	body, _ := io.ReadAll(r.Body)
	ch <- capturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
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

func writeCompleted(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"kimi-for-coding","content":[]}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

data: {"type":"message_stop"}

`)
}
