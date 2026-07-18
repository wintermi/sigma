// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package opencode_test

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/opencode"
)

func TestRegisterZenReportsRegistryAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := opencode.RegisterZen(registry); err != nil {
		t.Fatalf("RegisterZen returned error: %v", err)
	}
	providers := registry.ListProviders()
	if got, want := len(providers), 1; got != want {
		t.Fatalf("providers length = %d, want %d", got, want)
	}
	if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestOpenCodeDispatchRoutesByModelMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		provider      sigma.ProviderID
		modelID       sigma.ModelID
		opencodeAPI   sigma.API
		register      func(*sigma.Registry, ...opencode.ProviderOption) error
		wantPath      string
		wantQuery     string
		wantAuthKey   string
		response      string
		supported     []sigma.ContentBlockType
		supportsTools bool
	}{
		{
			name:        "zen gemini uses google generative ai",
			provider:    sigma.ProviderOpenCode,
			modelID:     "gemini-3-flash",
			opencodeAPI: sigma.APIGoogleGenerativeAI,
			register:    opencode.RegisterZen,
			wantPath:    "/models/gemini-3-flash:streamGenerateContent",
			wantQuery:   "alt=sse",
			wantAuthKey: "X-Goog-Api-Key",
			response:    googleCompletedEvent,
			supported:   []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		},
		{
			name:        "zen claude uses anthropic messages",
			provider:    sigma.ProviderOpenCode,
			modelID:     "claude-opus-4-7",
			opencodeAPI: sigma.APIAnthropicMessages,
			register:    opencode.RegisterZen,
			wantPath:    "/messages",
			wantAuthKey: "X-Api-Key",
			response:    anthropicCompletedEvent,
			supported:   []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		},
		{
			name:        "zen gpt uses responses",
			provider:    sigma.ProviderOpenCode,
			modelID:     "gpt-5.1-codex",
			opencodeAPI: sigma.APIOpenAIResponses,
			register:    opencode.RegisterZen,
			wantPath:    "/responses",
			wantAuthKey: "Authorization",
			response:    responsesCompletedEvent,
			supported:   []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		},
		{
			name:          "zen kimi defaults to chat completions",
			provider:      sigma.ProviderOpenCode,
			modelID:       "kimi-k2.6",
			register:      opencode.RegisterZen,
			wantPath:      "/chat/completions",
			wantAuthKey:   "Authorization",
			response:      chatCompletedEvent,
			supported:     []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
			supportsTools: true,
		},
		{
			name:        "go qwen uses anthropic messages",
			provider:    sigma.ProviderOpenCodeGo,
			modelID:     "qwen3.7-max",
			opencodeAPI: sigma.APIAnthropicMessages,
			register:    opencode.RegisterGo,
			wantPath:    "/messages",
			wantAuthKey: "X-Api-Key",
			response:    anthropicCompletedEvent,
			supported:   []sigma.ContentBlockType{sigma.ContentBlockText},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tt.response)
			}))
			t.Cleanup(server.Close)

			client := opencodeTestClient(t, tt.provider, tt.modelID, tt.opencodeAPI, tt.supported, tt.supportsTools, tt.register, server.URL)
			_, err := client.Complete(
				context.Background(),
				sigma.Model{Provider: tt.provider, ID: tt.modelID},
				sigma.Request{Messages: []sigma.Message{sigma.UserText("Reply with ok.")}},
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			request := receiveRequest(t, requests)
			if got := request.Path; got != tt.wantPath {
				t.Fatalf("request path = %q, want %q", got, tt.wantPath)
			}
			if got := request.Query; got != tt.wantQuery {
				t.Fatalf("request query = %q, want %q", got, tt.wantQuery)
			}
			if got := request.Headers.Get(tt.wantAuthKey); got == "" {
				t.Fatalf("missing auth header %q in %#v", tt.wantAuthKey, request.Headers)
			}
			if got := request.Headers.Get("X-Provider"); got != "provider" {
				t.Fatalf("provider header = %q, want provider", got)
			}
		})
	}
}

func TestOpenCodeRoutedResponsesRejectsLogprobsBeforeRequest(t *testing.T) {
	t.Parallel()

	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	client := opencodeTestClient(
		t,
		sigma.ProviderOpenCode,
		"gpt-5.1-codex",
		sigma.APIOpenAIResponses,
		[]sigma.ContentBlockType{sigma.ContentBlockText},
		false,
		opencode.RegisterZen,
		server.URL,
	)
	_, err := client.Complete(
		context.Background(),
		sigma.Model{Provider: sigma.ProviderOpenCode, ID: "gpt-5.1-codex"},
		sigma.Request{Messages: []sigma.Message{sigma.UserText("Reply with ok.")}},
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{TopLogprobs: 2}),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	var sigmaErr *sigma.Error
	if !stderrors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if sigmaErr.Code != sigma.ErrorInvalidOptions {
		t.Fatalf("error code = %q, want %q", sigmaErr.Code, sigma.ErrorInvalidOptions)
	}
	select {
	case <-requests:
		t.Fatal("server received request for invalid logprobs option")
	default:
	}
}

func TestOpenCodeGoGeneratedModelsDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		modelID     sigma.ModelID
		wantPath    string
		wantAuthKey string
		response    string
		maxTokens   int
	}{
		{
			name:        "glm uses chat completions and max tokens",
			modelID:     "glm-5.2",
			wantPath:    "/chat/completions",
			wantAuthKey: "Authorization",
			response:    chatCompletedEvent,
			maxTokens:   321,
		},
		{
			name:        "grok uses chat completions and max tokens",
			modelID:     "grok-4.5",
			wantPath:    "/chat/completions",
			wantAuthKey: "Authorization",
			response:    chatCompletedEvent,
			maxTokens:   321,
		},
		{
			name:        "kimi k3 uses chat completions and max tokens",
			modelID:     "kimi-k3",
			wantPath:    "/chat/completions",
			wantAuthKey: "Authorization",
			response:    chatCompletedEvent,
			maxTokens:   321,
		},
		{
			name:        "qwen uses anthropic messages",
			modelID:     "qwen3.7-plus",
			wantPath:    "/messages",
			wantAuthKey: "X-Api-Key",
			response:    anthropicCompletedEvent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tt.response)
			}))
			t.Cleanup(server.Close)

			client := generatedOpenCodeGoTestClient(t, tt.modelID, server.URL)
			opts := []sigma.Option(nil)
			if tt.maxTokens != 0 {
				opts = append(opts, sigma.WithMaxTokens(tt.maxTokens))
			}
			_, err := client.Complete(
				context.Background(),
				sigma.Model{Provider: sigma.ProviderOpenCodeGo, ID: tt.modelID},
				sigma.Request{Messages: []sigma.Message{sigma.UserText("Reply with ok.")}},
				opts...,
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			request := receiveRequest(t, requests)
			if got := request.Path; got != tt.wantPath {
				t.Fatalf("request path = %q, want %q", got, tt.wantPath)
			}
			if got := request.Headers.Get(tt.wantAuthKey); got == "" {
				t.Fatalf("missing auth header %q in %#v", tt.wantAuthKey, request.Headers)
			}
			if tt.maxTokens != 0 {
				var body map[string]any
				if err := json.Unmarshal(request.Body, &body); err != nil {
					t.Fatalf("Unmarshal request body: %v", err)
				}
				if got, ok := body["max_tokens"].(float64); !ok || got != float64(tt.maxTokens) {
					t.Fatalf("max_tokens = %v, %v; want %d, true", got, ok, tt.maxTokens)
				}
				if _, ok := body["max_completion_tokens"]; ok {
					t.Fatalf("request body unexpectedly includes max_completion_tokens: %s", request.Body)
				}
			}
		})
	}
}

func TestOpenCodeZenGeneratedGPT56ResponsesAffinity(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	client := generatedOpenCodeZenTestClient(t, "gpt-5.6-luna", server.URL)
	_, err := client.Complete(
		context.Background(),
		sigma.Model{Provider: sigma.ProviderOpenCode, ID: "gpt-5.6-luna"},
		sigma.Request{Messages: []sigma.Message{sigma.UserText("Reply with ok.")}},
		sigma.WithSessionID("zen-session"),
		sigma.WithCacheRetention(sigma.CacheRetentionShort),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/responses"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got := request.Headers.Get("Authorization"); got == "" {
		t.Fatalf("missing Authorization header in %#v", request.Headers)
	}
	if got, want := request.Headers.Get("X-Provider"), "provider"; got != want {
		t.Fatalf("provider header = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("x-client-request-id"), "zen-session"; got != want {
		t.Fatalf("request ID = %q, want %q", got, want)
	}
	if got := request.Headers.Get("session_id"); got != "" {
		t.Fatalf("session_id = %q, want omitted", got)
	}
}

func opencodeTestClient(
	t *testing.T,
	providerID sigma.ProviderID,
	modelID sigma.ModelID,
	api sigma.API,
	inputs []sigma.ContentBlockType,
	supportsTools bool,
	register func(*sigma.Registry, ...opencode.ProviderOption) error,
	baseURL string,
) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := register(registry, opencode.WithBaseURL(baseURL), opencode.WithHeader("X-Provider", "provider")); err != nil {
		t.Fatalf("register returned error: %v", err)
	}
	metadata := map[string]any{}
	if api != "" {
		metadata["opencodeAPI"] = string(api)
	}
	if err := registry.RegisterModel(sigma.Model{
		ID:               modelID,
		Provider:         providerID,
		API:              sigma.APIOpenAICompletions,
		SupportedInputs:  inputs,
		SupportsTools:    supportsTools,
		ProviderMetadata: metadata,
	}); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-key"}, nil
	})
	return sigma.NewClient(sigma.WithRegistry(registry), sigma.WithAuthResolver(resolver))
}

func generatedOpenCodeGoTestClient(t *testing.T, modelID sigma.ModelID, baseURL string) *sigma.Client {
	t.Helper()

	registry := sigma.DefaultRegistry()
	model, ok := registry.Model(sigma.ProviderOpenCodeGo, modelID)
	if !ok {
		t.Fatalf("generated registry missing OpenCode Go model %q", modelID)
	}
	if err := opencode.RegisterGo(registry, opencode.WithHeader("X-Provider", "provider")); err != nil {
		t.Fatalf("RegisterGo returned error: %v", err)
	}
	metadata := make(map[string]any, len(model.ProviderMetadata)+1)
	for key, value := range model.ProviderMetadata {
		metadata[key] = value
	}
	metadata["baseURL"] = baseURL
	model.ProviderMetadata = metadata
	if err := registry.RegisterModel(model, sigma.WithOverride()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-key"}, nil
	})
	return sigma.NewClient(sigma.WithRegistry(registry), sigma.WithAuthResolver(resolver))
}

func generatedOpenCodeZenTestClient(t *testing.T, modelID sigma.ModelID, baseURL string) *sigma.Client {
	t.Helper()

	registry := sigma.DefaultRegistry()
	model, ok := registry.Model(sigma.ProviderOpenCode, modelID)
	if !ok {
		t.Fatalf("generated registry missing OpenCode Zen model %q", modelID)
	}
	if err := opencode.RegisterZen(registry, opencode.WithHeader("X-Provider", "provider")); err != nil {
		t.Fatalf("RegisterZen returned error: %v", err)
	}
	metadata := make(map[string]any, len(model.ProviderMetadata)+1)
	for key, value := range model.ProviderMetadata {
		metadata[key] = value
	}
	metadata["baseURL"] = baseURL
	model.ProviderMetadata = metadata
	if err := registry.RegisterModel(model, sigma.WithOverride()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-key"}, nil
	})
	return sigma.NewClient(sigma.WithRegistry(registry), sigma.WithAuthResolver(resolver))
}

type capturedRequest struct {
	Path    string
	Query   string
	Headers http.Header
	Body    []byte
}

func captureRequest(t *testing.T, requests chan<- capturedRequest, r *http.Request) {
	t.Helper()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("ReadAll request body: %v", err)
		return
	}
	requests <- capturedRequest{
		Path:    r.URL.Path,
		Query:   r.URL.RawQuery,
		Headers: r.Header.Clone(),
		Body:    body,
	}
}

func receiveRequest(t *testing.T, requests <-chan capturedRequest) capturedRequest {
	t.Helper()

	select {
	case request := <-requests:
		return request
	default:
		t.Fatal("server did not receive request")
		return capturedRequest{}
	}
}

const googleCompletedEvent = `data: {"responseId":"resp_complete","candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}

`

const anthropicCompletedEvent = `data: {"type":"message_start","message":{"id":"msg_complete","type":"message","role":"assistant","model":"claude-test","content":[]}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}

data: {"type":"message_stop"}

`

const responsesCompletedEvent = `data: {"type":"response.completed","response":{"id":"resp_complete","model":"gpt-test","status":"completed","output":[{"type":"message","id":"msg_complete","role":"assistant","content":[{"type":"output_text","id":"text_complete","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}

`

const chatCompletedEvent = `data: {"id":"chatcmpl_complete","model":"kimi-test","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}

data: {"id":"chatcmpl_complete","model":"kimi-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}

data: [DONE]

`
