// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral_test

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
	"github.com/wintermi/sigma/provider/mistral"
)

type capturedRequest struct {
	Method  string
	Path    string
	Query   string
	Headers http.Header
	Body    string
}

func TestRegisterReportsConversationsAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("mistral-compatible")
	if err := mistral.Register(registry, providerID); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(mistralTestModel(providerID)); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIMistralConversations; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestCompleteSendsTextPayloadWithHooksHeadersAndOptions(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMistralSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	payloadHookCalled := false
	responseHookCalled := false
	providerID := sigma.ProviderID("mistral-payload-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(
		t,
		providerID,
		model,
		server.URL,
		mistral.WithHeader("X-Provider", "provider"),
		mistral.WithPayloadHook(func(_ context.Context, _ sigma.Model, _ sigma.Request, _ sigma.Options, payload map[string]any) error {
			payloadHookCalled = true
			payload["name"] = "hooked"
			return nil
		}),
		mistral.WithResponseHook(func(_ context.Context, _ sigma.Model, _ sigma.Options, resp *http.Response) error {
			responseHookCalled = true
			if got, want := resp.Header.Get("X-Response"), "seen"; got != want {
				t.Fatalf("response hook header = %q, want %q", got, want)
			}
			return nil
		}),
	)

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			SystemPrompt: "You are helpful.",
			Messages:     []sigma.Message{sigma.UserText("Hello")},
		},
		sigma.WithAPIKey("request-key"),
		sigma.WithTemperature(0.2),
		sigma.WithMaxTokens(123),
		sigma.WithHeader("X-Custom", "custom"),
		sigma.WithMetadata(map[string]any{"trace": "abc"}),
		sigma.WithProviderOptions(providerID, map[string]any{
			"completion_args":   map[string]any{"top_p": 0.9},
			"tool_choice":       "auto",
			"store":             false,
			"handoff_execution": "client",
			"extra_body":        map[string]any{"description": "fixture"},
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if !payloadHookCalled {
		t.Fatal("payload hook was not called")
	}
	if !responseHookCalled {
		t.Fatal("response hook was not called")
	}
	if got, want := final.ProviderMetadata["conversation_id"], "conv_complete"; got != want {
		t.Fatalf("conversation id = %v, want %v", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/conversations"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Query, ""; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer request-key")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	goldentest.AssertJSON(t, request.Body, "provider/mistral/conversations/rich_text_payload.json")
}

func TestConversationPayloadDropsUnansweredToolCallsBeforeUserTurn(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMistralSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-drop-unanswered-tool-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_abandoned", "lookup", map[string]any{"query": "weather"}),
				},
			},
			sigma.UserText("Skip the lookup."),
		}},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	inputs := payload["inputs"].([]any)
	if got, want := len(inputs), 1; got != want {
		t.Fatalf("input count = %d, want %d", got, want)
	}
	input := inputs[0].(map[string]any)
	if got, want := input["role"], "user"; got != want {
		t.Fatalf("input role = %v, want %q", got, want)
	}
}

func TestConversationPayloadNormalizesProviderText(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMistralSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-normalized-payload-test")
	model := mistralTestModel(providerID)
	model.SupportsThinking = true
	client := mistralTestClient(t, providerID, model, server.URL)
	invalid := invalidProviderText()

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			SystemPrompt: "system" + invalid,
			Messages: []sigma.Message{
				{Role: sigma.RoleDeveloper, Content: []sigma.ContentBlock{sigma.Text("developer" + invalid)}},
				sigma.UserText("user" + invalid),
				{
					Role: sigma.RoleAssistant,
					Content: []sigma.ContentBlock{
						sigma.Text("assistant" + invalid),
						sigma.Thinking("thinking"+invalid, "sig"),
						sigma.ToolCallBlock("call_invalid", "lookup", map[string]any{"query": "weather"}),
					},
				},
				{Role: sigma.RoleTool, ToolCallID: "call_invalid", ToolName: "lookup", Content: []sigma.ContentBlock{sigma.Text("tool" + invalid)}},
			},
			Tools: []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}},
		},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	if got, want := payload["instructions"], "systemclean"; got != want {
		t.Fatalf("instructions = %v, want %q", got, want)
	}
	inputs := payload["inputs"].([]any)
	if got, want := inputs[0].(map[string]any)["content"], "developerclean"; got != want {
		t.Fatalf("developer content = %v, want %q", got, want)
	}
	if got, want := inputs[1].(map[string]any)["content"], "userclean"; got != want {
		t.Fatalf("user content = %v, want %q", got, want)
	}
	assistant := inputs[2].(map[string]any)
	content := assistant["content"].([]any)
	if got, want := content[0].(map[string]any)["text"], "assistantclean"; got != want {
		t.Fatalf("assistant text = %v, want %q", got, want)
	}
	thinking := content[1].(map[string]any)["thinking"].([]any)
	if got, want := thinking[0].(map[string]any)["text"], "thinkingclean"; got != want {
		t.Fatalf("thinking = %v, want %q", got, want)
	}
	tool := inputs[4].(map[string]any)
	if got, want := tool["result"], "toolclean"; got != want {
		t.Fatalf("tool result = %v, want %q", got, want)
	}
}

func TestCompleteSetsSessionAffinityHeaderUnlessCallerOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		opts               []sigma.Option
		wantAffinity       string
		wantPromptCacheKey string
	}{
		{
			name:               "session id",
			opts:               []sigma.Option{sigma.WithSessionID("session-123")},
			wantAffinity:       "session-123",
			wantPromptCacheKey: "session-123",
		},
		{
			name: "cache disabled",
			opts: []sigma.Option{
				sigma.WithSessionID("session-123"),
				sigma.WithCacheRetention(sigma.CacheRetentionNone),
			},
		},
		{
			name: "caller header wins",
			opts: []sigma.Option{
				sigma.WithSessionID("session-123"),
				sigma.WithHeader("X-Affinity", "caller-affinity"),
			},
			wantAffinity:       "caller-affinity",
			wantPromptCacheKey: "session-123",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeMistralSSE(t, w, completedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("mistral-session-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := mistralTestModel(providerID)
			client := mistralTestClient(t, providerID, model, server.URL)

			if _, err := client.Complete(context.Background(), model, sigma.Request{
				Messages: []sigma.Message{sigma.UserText("Hello")},
			}, tt.opts...); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			request := receiveRequest(t, requests)
			if tt.wantAffinity == "" {
				assertHeaderAbsent(t, request.Headers, "X-Affinity")
			} else {
				assertHeader(t, request.Headers, "X-Affinity", tt.wantAffinity)
			}
			payload := decodePayload(t, request.Body)
			if tt.wantPromptCacheKey == "" {
				if _, ok := payload["prompt_cache_key"]; ok {
					t.Fatalf("prompt_cache_key = %v, want absent", payload["prompt_cache_key"])
				}
			} else if got := payload["prompt_cache_key"]; got != tt.wantPromptCacheKey {
				t.Fatalf("prompt_cache_key = %v, want %q", got, tt.wantPromptCacheKey)
			}
		})
	}
}

func TestTypedMistralToolChoicePayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts sigma.MistralOptions
		want string
	}{
		{
			name: "auto",
			opts: sigma.MistralOptions{ToolChoice: &sigma.MistralToolChoice{Type: sigma.MistralToolChoiceAuto}},
			want: "auto",
		},
		{
			name: "required",
			opts: sigma.MistralOptions{ToolChoice: &sigma.MistralToolChoice{Type: sigma.MistralToolChoiceRequired}},
			want: "required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeMistralSSE(t, w, completedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("mistral-tool-choice-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := mistralTestModel(providerID)
			client := mistralTestClient(t, providerID, model, server.URL)

			if _, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{
					Messages: []sigma.Message{sigma.UserText("Use a tool.")},
					Tools:    []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}},
				},
				sigma.WithMistralOptions(tt.opts),
			); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			payload := decodePayload(t, receiveRequest(t, requests).Body)
			completionArgs := payload["completion_args"].(map[string]any)
			if got := completionArgs["tool_choice"]; got != tt.want {
				t.Fatalf("tool choice = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestTypedMistralNamedToolChoiceRejected(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMistralSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-named-tool-choice-rejected")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("Use a tool.")},
			Tools:    []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}},
		},
		sigma.WithMistralOptions(sigma.MistralOptions{
			ToolChoice: &sigma.MistralToolChoice{Type: sigma.MistralToolChoiceTool, Name: "lookup"},
		}),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !strings.Contains(err.Error(), "mistral tool choice must be auto, none, any, or required") {
		t.Fatalf("error = %v, want Mistral tool choice validation", err)
	}
	select {
	case request := <-requests:
		t.Fatalf("unexpected provider request: %#v", request)
	default:
	}
}

func TestTypedMistralToolChoiceOverridesRawProviderOption(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMistralSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-typed-tool-choice-precedence")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("Use a tool.")},
			Tools:    []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}},
		},
		sigma.WithProviderOption(providerID, "tool_choice", "none"),
		sigma.WithMistralOptions(sigma.MistralOptions{
			ToolChoice: &sigma.MistralToolChoice{Type: sigma.MistralToolChoiceRequired},
		}),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	completionArgs := payload["completion_args"].(map[string]any)
	if got, want := completionArgs["tool_choice"], "required"; got != want {
		t.Fatalf("tool choice = %#v, want %#v", got, want)
	}
}

func TestConversationsRejectsProviderDefinedTools(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMistralSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-provider-tools-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("Search current docs.")},
		Tools: []sigma.Tool{{
			Name:                "web_search",
			ProviderDefinedType: "web_search",
		}},
	})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if got, want := sigmaErr.Code, sigma.ErrorUnsupported; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if !strings.Contains(err.Error(), "provider-defined tool") {
		t.Fatalf("error = %v, want provider-defined tool message", err)
	}
	select {
	case request := <-requests:
		t.Fatalf("unexpected provider request: %#v", request)
	default:
	}
}

func TestConversationGoldenPayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		req    sigma.Request
		golden string
	}{
		{
			name: "text only",
			req: sigma.Request{
				Messages: []sigma.Message{sigma.UserText("Hello")},
			},
			golden: "provider/mistral/conversations/text_only_payload.json",
		},
		{
			name: "tools",
			req: sigma.Request{
				Messages: []sigma.Message{sigma.UserText("Weather?")},
				Tools: []sigma.Tool{{
					Name:        "weather",
					Description: "Get weather",
					InputSchema: sigma.Schema{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
						"required":             []any{"city"},
						"additionalProperties": false,
					},
				}},
			},
			golden: "provider/mistral/conversations/tools_payload.json",
		},
		{
			name: "replayed assistant messages",
			req: sigma.Request{
				Messages: []sigma.Message{
					sigma.UserText("Weather?"),
					{
						Role: sigma.RoleAssistant,
						Content: []sigma.ContentBlock{
							sigma.Text("Let me check."),
							sigma.ToolCallBlock("call_weather", "weather", map[string]any{"city": "Melbourne"}),
						},
					},
				},
			},
			golden: "provider/mistral/conversations/tool_call_replay_payload.json",
		},
		{
			name: "tool results",
			req: sigma.Request{
				Messages: []sigma.Message{
					sigma.UserText("Weather?"),
					{
						Role: sigma.RoleAssistant,
						Content: []sigma.ContentBlock{
							sigma.ToolCallBlock("call_weather", "weather", map[string]any{"city": "Melbourne"}),
						},
					},
					{
						Role:       sigma.RoleTool,
						ToolCallID: "call_weather",
						ToolName:   "weather",
						Content:    []sigma.ContentBlock{sigma.Text("Sunny")},
					},
				},
			},
			golden: "provider/mistral/conversations/tool_result_payload.json",
		},
		{
			name: "normalized tool ids",
			req: sigma.Request{
				Messages: []sigma.Message{
					sigma.UserText("Weather?"),
					{
						Role: sigma.RoleAssistant,
						Content: []sigma.ContentBlock{
							sigma.ToolCallBlock("foreign|tool-call-id-that-is-too-long", "weather", map[string]any{"city": "Melbourne"}),
						},
					},
					{
						Role:       sigma.RoleTool,
						ToolCallID: "foreign|tool-call-id-that-is-too-long",
						ToolName:   "weather",
						Content:    []sigma.ContentBlock{sigma.Text("Sunny")},
					},
				},
			},
			golden: "provider/mistral/conversations/normalized_tool_ids_payload.json",
		},
		{
			name: "thinking replay",
			req: sigma.Request{
				Messages: []sigma.Message{
					sigma.UserText("Solve this."),
					{
						Role: sigma.RoleAssistant,
						Content: []sigma.ContentBlock{
							sigma.Thinking("Check constraints.", ""),
							sigma.Text("Answer."),
						},
					},
				},
			},
			golden: "provider/mistral/conversations/thinking_replay_payload.json",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeMistralSSE(t, w, completedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("mistral-golden-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := mistralTestModel(providerID)
			if tt.name == "thinking replay" {
				model.SupportsThinking = true
			}
			client := mistralTestClient(t, providerID, model, server.URL)

			if _, err := client.Complete(context.Background(), model, tt.req); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}
			request := receiveRequest(t, requests)
			goldentest.AssertJSON(t, request.Body, tt.golden)
		})
	}
}

func TestConversationImagePayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		req    sigma.Request
		golden string
	}{
		{
			name: "user image input",
			req: sigma.Request{
				Messages: []sigma.Message{
					sigma.UserContent(
						sigma.Text("Describe this image."),
						sigma.ImageBase64("image/png", "aW1hZ2U="),
					),
				},
			},
			golden: "provider/mistral/conversations/image_payload.json",
		},
		{
			name: "user URL image input",
			req: sigma.Request{
				Messages: []sigma.Message{
					sigma.UserContent(
						sigma.Text("Describe this image."),
						sigma.ImageURL("image/png", "https://example.test/image.png"),
					),
				},
			},
			golden: "provider/mistral/conversations/image_url_payload.json",
		},
		{
			name: "image only tool result",
			req: sigma.Request{
				Messages: []sigma.Message{
					sigma.UserText("Inspect the screenshot."),
					{
						Role: sigma.RoleAssistant,
						Content: []sigma.ContentBlock{
							sigma.ToolCallBlock("call_screenshot", "screenshot", map[string]any{"target": "screen"}),
						},
					},
					{
						Role:       sigma.RoleTool,
						ToolCallID: "call_screenshot",
						ToolName:   "screenshot",
						Content:    []sigma.ContentBlock{sigma.ImageBase64("image/png", "aW1hZ2U=")},
					},
				},
			},
			golden: "provider/mistral/conversations/tool_result_image_payload.json",
		},
		{
			name: "URL image only tool result",
			req: sigma.Request{
				Messages: []sigma.Message{
					sigma.UserText("Inspect the screenshot."),
					{
						Role: sigma.RoleAssistant,
						Content: []sigma.ContentBlock{
							sigma.ToolCallBlock("call_screenshot", "screenshot", map[string]any{"target": "screen"}),
						},
					},
					{
						Role:       sigma.RoleTool,
						ToolCallID: "call_screenshot",
						ToolName:   "screenshot",
						Content:    []sigma.ContentBlock{sigma.ImageURL("image/png", "https://example.test/screenshot.png")},
					},
				},
			},
			golden: "provider/mistral/conversations/tool_result_url_image_payload.json",
		},
		{
			name: "text plus image tool result",
			req: sigma.Request{
				Messages: []sigma.Message{
					sigma.UserText("Inspect the screenshot."),
					{
						Role: sigma.RoleAssistant,
						Content: []sigma.ContentBlock{
							sigma.ToolCallBlock("call_screenshot", "screenshot", map[string]any{"target": "screen"}),
						},
					},
					{
						Role:       sigma.RoleTool,
						ToolCallID: "call_screenshot",
						ToolName:   "screenshot",
						Content: []sigma.ContentBlock{
							sigma.Text("Screenshot captured."),
							sigma.ImageBase64("image/png", "aW1hZ2U="),
						},
					},
				},
			},
			golden: "provider/mistral/conversations/tool_result_text_image_payload.json",
		},
		{
			name: "text plus URL image tool result",
			req: sigma.Request{
				Messages: []sigma.Message{
					sigma.UserText("Inspect the screenshot."),
					{
						Role: sigma.RoleAssistant,
						Content: []sigma.ContentBlock{
							sigma.ToolCallBlock("call_screenshot", "screenshot", map[string]any{"target": "screen"}),
						},
					},
					{
						Role:       sigma.RoleTool,
						ToolCallID: "call_screenshot",
						ToolName:   "screenshot",
						Content: []sigma.ContentBlock{
							sigma.Text("Screenshot captured."),
							sigma.ImageURL("image/png", "https://example.test/screenshot.png"),
						},
					},
				},
			},
			golden: "provider/mistral/conversations/tool_result_text_url_image_payload.json",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeMistralSSE(t, w, completedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("mistral-image-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := mistralTestModel(providerID)
			model.ID = "pixtral-12b"
			model.SupportedInputs = []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage}
			client := mistralTestClient(t, providerID, model, server.URL)

			if _, err := client.Complete(context.Background(), model, tt.req); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}
			request := receiveRequest(t, requests)
			goldentest.AssertJSON(t, request.Body, tt.golden)
		})
	}
}

func TestConversationImageChunkUsesSnakeCaseField(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMistralSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-image-field-shape")
	model := mistralTestModel(providerID)
	model.ID = "pixtral-12b"
	model.SupportedInputs = []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage}
	client := mistralTestClient(t, providerID, model, server.URL)

	if _, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserContent(
				sigma.Text("Describe this image."),
				sigma.ImageBase64("image/png", "aW1hZ2U="),
			),
		},
	}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	inputs := payload["inputs"].([]any)
	first := inputs[0].(map[string]any)
	content := first["content"].([]any)
	image := content[1].(map[string]any)
	if got, want := image["image_url"], "data:image/png;base64,aW1hZ2U="; got != want {
		t.Fatalf("image_url = %v, want %q", got, want)
	}
	if _, ok := image["imageUrl"]; ok {
		t.Fatalf("imageUrl = %v, want absent", image["imageUrl"])
	}
}

func TestConversationToolResultUsesSchemaFields(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeMistralSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-tool-result-field-shape")
	model := mistralTestModel(providerID)
	model.ID = "pixtral-12b"
	model.SupportedInputs = []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage}
	client := mistralTestClient(t, providerID, model, server.URL)

	if _, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("Inspect the screenshot."),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_screenshot", "screenshot", map[string]any{"target": "screen"}),
				},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call_screenshot",
				ToolName:   "screenshot",
				IsError:    true,
				Content: []sigma.ContentBlock{
					sigma.Text("Screenshot captured."),
					sigma.ImageURL("image/png", "https://example.test/screenshot.png"),
				},
			},
		},
	}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodePayload(t, receiveRequest(t, requests).Body)
	inputs := payload["inputs"].([]any)
	result := inputs[2].(map[string]any)
	if _, ok := result["name"]; ok {
		t.Fatalf("name = %v, want absent", result["name"])
	}
	if _, ok := result["is_error"]; ok {
		t.Fatalf("is_error = %v, want absent", result["is_error"])
	}
	text, ok := result["result"].(string)
	if !ok {
		t.Fatalf("result type = %T, want string", result["result"])
	}
	if got, want := text, "Screenshot captured.\nImage: https://example.test/screenshot.png"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
}

func TestConversationReasoningPayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		model  sigma.Model
		opts   []sigma.Option
		golden string
	}{
		{
			name: "adjustable reasoning",
			model: func() sigma.Model {
				model := mistralTestModel("mistral-adjustable-reasoning")
				model.ID = "mistral-small-latest"
				model.SupportsThinking = true
				model.ThinkingLevelMap = map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "high"}
				model.ProviderMetadata = map[string]any{"mistral_reasoning_mode": "reasoning_effort"}
				return model
			}(),
			opts:   []sigma.Option{sigma.WithReasoningLevel(sigma.ThinkingLevelHigh)},
			golden: "provider/mistral/conversations/adjustable_reasoning_payload.json",
		},
		{
			name: "native reasoning",
			model: func() sigma.Model {
				model := mistralTestModel("mistral-native-reasoning")
				model.ID = "magistral-medium-latest"
				model.SupportsThinking = true
				model.ProviderMetadata = map[string]any{"mistral_reasoning_mode": "prompt_mode"}
				return model
			}(),
			opts:   []sigma.Option{sigma.WithReasoningLevel(sigma.ThinkingLevelMedium)},
			golden: "provider/mistral/conversations/native_reasoning_payload.json",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeMistralSSE(t, w, completedEvent)
			}))
			t.Cleanup(server.Close)

			client := mistralTestClient(t, tt.model.Provider, tt.model, server.URL)
			if _, err := client.Complete(context.Background(), tt.model, sigma.Request{
				Messages: []sigma.Message{sigma.UserText("Think carefully.")},
			}, tt.opts...); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}
			request := receiveRequest(t, requests)
			goldentest.AssertJSON(t, request.Body, tt.golden)
			payload := decodePayload(t, request.Body)
			switch tt.name {
			case "adjustable reasoning":
				completionArgs := payload["completion_args"].(map[string]any)
				if got, want := completionArgs["reasoning_effort"], "high"; got != want {
					t.Fatalf("reasoning_effort = %v, want %q", got, want)
				}
				if _, ok := payload["prompt_mode"]; ok {
					t.Fatalf("prompt_mode = %v, want absent", payload["prompt_mode"])
				}
			case "native reasoning":
				if got, want := payload["prompt_mode"], "reasoning"; got != want {
					t.Fatalf("prompt_mode = %v, want %q", got, want)
				}
				if completionArgs, ok := payload["completion_args"].(map[string]any); ok {
					if _, ok := completionArgs["prompt_mode"]; ok {
						t.Fatalf("completion_args.prompt_mode = %v, want absent", completionArgs["prompt_mode"])
					}
				}
			}
		})
	}
}

func TestStreamingMapsTextUsageAndMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMistralSSE(t, w, `event: conversation.response.started
data: {"type":"conversation.response.started","conversation_id":"conv_stream"}

event: message.output.delta
data: {"type":"message.output.delta","output_index":0,"id":"msg_1","content_index":0,"model":"mistral-test-version","role":"assistant","content":"Hello"}

event: message.output.delta
data: {"type":"message.output.delta","output_index":0,"id":"msg_1","content_index":0,"content":" world"}

event: conversation.response.done
data: {"type":"conversation.response.done","usage":{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18,"connector_tokens":4}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-stream-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

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
		sigma.EventKindTextDelta,
		sigma.EventKindTextEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.Content[0].Text, "Hello world"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := final.ProviderMetadata["conversation_id"], "conv_stream"; got != want {
		t.Fatalf("conversation id = %v, want %v", got, want)
	}
	if got, want := final.ProviderMetadata["model"], "mistral-test-version"; got != want {
		t.Fatalf("provider model = %v, want %v", got, want)
	}
	if final.Usage == nil {
		t.Fatal("final usage was nil")
	}
	if got, want := final.Usage.InputTokens, 10; got != want {
		t.Fatalf("input tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.ToolUseInputTokens, 4; got != want {
		t.Fatalf("tool use input tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.Provider, providerID; got != want {
		t.Fatalf("usage provider = %q, want %q", got, want)
	}
	if got, want := final.Usage.Model, model.ID; got != want {
		t.Fatalf("usage model = %q, want %q", got, want)
	}
	if got, want := final.Usage.Raw["connector_tokens"], float64(4); got != want {
		t.Fatalf("raw connector tokens = %v, want %v", got, want)
	}
	if events[len(events)-1].Usage == nil || events[len(events)-1].Usage.ToolUseInputTokens != 4 {
		t.Fatalf("terminal usage = %#v, want tool use tokens", events[len(events)-1].Usage)
	}
}

func TestStreamingMapsCachedPromptTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		usage      string
		rawKey     string
		rawWant    any
		wantCached int
	}{
		{
			name:       "snake prompt tokens details",
			usage:      `{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18,"prompt_tokens_details":{"cached_tokens":3}}`,
			rawKey:     "prompt_tokens_details",
			wantCached: 3,
		},
		{
			name:       "camel prompt tokens details",
			usage:      `{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18,"promptTokensDetails":{"cachedTokens":4}}`,
			rawKey:     "promptTokensDetails",
			wantCached: 4,
		},
		{
			name:       "snake cached token count",
			usage:      `{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18,"num_cached_tokens":15}`,
			rawKey:     "num_cached_tokens",
			rawWant:    float64(15),
			wantCached: 10,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeMistralSSE(t, w, `event: conversation.response.started
data: {"type":"conversation.response.started","conversation_id":"conv_cached"}

event: message.output.delta
data: {"type":"message.output.delta","output_index":0,"id":"msg_1","content_index":0,"content":"cached"}

event: conversation.response.done
data: {"type":"conversation.response.done","usage":`+tt.usage+`}
`)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("mistral-cache-usage-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := mistralTestModel(providerID)
			client := mistralTestClient(t, providerID, model, server.URL)

			stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
			events := collectEvents(t, stream)
			if err := stream.Err(); err != nil {
				t.Fatalf("stream error = %v", err)
			}
			final, ok := stream.Final()
			if !ok {
				t.Fatal("stream final was not recorded")
			}
			if final.Usage == nil {
				t.Fatal("final usage was nil")
			}
			if got, want := final.Usage.InputTokens, 10-tt.wantCached; got != want {
				t.Fatalf("input tokens = %d, want %d", got, want)
			}
			if got := final.Usage.CacheReadInputTokens; got != tt.wantCached {
				t.Fatalf("cache read input tokens = %d, want %d", got, tt.wantCached)
			}
			if got, want := final.Usage.TotalTokens, 18; got != want {
				t.Fatalf("total tokens = %d, want %d", got, want)
			}
			if got, want := final.Cost.InputCost, float64(10-tt.wantCached)/1_000_000; got != want {
				t.Fatalf("input cost = %v, want %v", got, want)
			}
			if _, ok := final.Usage.Raw[tt.rawKey]; !ok {
				t.Fatalf("raw usage missing %q: %#v", tt.rawKey, final.Usage.Raw)
			}
			if tt.rawWant != nil && final.Usage.Raw[tt.rawKey] != tt.rawWant {
				t.Fatalf("raw %s = %#v, want %#v", tt.rawKey, final.Usage.Raw[tt.rawKey], tt.rawWant)
			}
			if events[len(events)-1].Usage == nil || events[len(events)-1].Usage.CacheReadInputTokens != tt.wantCached {
				t.Fatalf("terminal usage = %#v, want cache read tokens", events[len(events)-1].Usage)
			}
		})
	}
}

func TestStreamingMapsThinkingAndText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMistralSSE(t, w, `event: conversation.response.started
data: {"type":"conversation.response.started","conversation_id":"conv_thinking"}

event: message.output.delta
data: {"type":"message.output.delta","output_index":0,"id":"msg_1","content_index":0,"content":{"type":"thinking","thinking":[{"type":"text","text":"Checked constraints. "}]}}

event: message.output.delta
data: {"type":"message.output.delta","output_index":0,"id":"msg_1","content_index":1,"content":{"type":"text","text":"The answer"}}

event: message.output.delta
data: {"type":"message.output.delta","output_index":0,"id":"msg_1","content_index":1,"content":{"type":"text","text":" is 42."}}

event: conversation.response.done
data: {"type":"conversation.response.done","usage":{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-thinking-stream-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

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
		sigma.EventKindThinkingStart,
		sigma.EventKindThinkingDelta,
		sigma.EventKindTextStart,
		sigma.EventKindTextDelta,
		sigma.EventKindTextDelta,
		sigma.EventKindThinkingEnd,
		sigma.EventKindTextEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.Content[0].ThinkingText, "Checked constraints. "; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "The answer is 42."; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestStreamingMapsFunctionCall(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMistralSSE(t, w, `event: conversation.response.started
data: {"type":"conversation.response.started","conversation_id":"conv_tool"}

event: function.call.delta
data: {"type":"function.call.delta","output_index":0,"id":"fc_1","tool_call_id":"call_weather","name":"weather","arguments":"{\"city\""}

event: function.call.delta
data: {"type":"function.call.delta","output_index":0,"id":"fc_1","tool_call_id":"call_weather","name":"weather","arguments":":\"Melbourne\"}"}

event: conversation.response.done
data: {"type":"conversation.response.done","usage":{"prompt_tokens":5,"completion_tokens":6,"total_tokens":11}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-tool-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("weather")}})
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
		sigma.EventKindToolCallStart,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	delta := events[2].PartialToolCall
	if delta == nil {
		t.Fatal("tool-call delta was nil")
	}
	if got, want := delta.ID, "call_weather"; got != want {
		t.Fatalf("partial id = %q, want %q", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonToolCalls; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ToolCallID, "call_weather"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ToolName, "weather"; got != want {
		t.Fatalf("tool call name = %q, want %q", got, want)
	}
	args := final.Content[0].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("tool city = %v, want %v", got, want)
	}
}

func TestProviderErrorIsTypedAndRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-mistral-request-id", "req_123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"bad key sk-secret123","type":"unauthorized"}`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-error-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.Diagnostics[0].API, sigma.APIMistralConversations; got != want {
		t.Fatalf("diagnostic API = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "sk-secret123") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestStreamErrorEventIsTypedAndRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeMistralSSE(t, w, `event: conversation.response.error
data: {"type":"conversation.response.error","error":{"message":"bad key sk-secret123","type":"provider"}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-stream-error-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

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
	if strings.Contains(err.Error(), "sk-secret123") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestRetryResendsRequestAfterRetryableStatus(t *testing.T) {
	t.Parallel()

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"message":"rate limited"}`)
			return
		}
		writeMistralSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-retry-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithMaxRetries(1),
		sigma.WithMaxRetryDelay(0),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := attempts, 2; got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
}

func TestUnsupportedCapabilitiesFailBeforeNetworkCall(t *testing.T) {
	t.Parallel()

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-unsupported-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserContent(sigma.ImageBase64("image/png", "aW1hZ2U="))},
	})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorUnsupported {
		t.Fatalf("error = %v, want ErrorUnsupported", err)
	}
	if got, want := requests, 0; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
}

func TestCancellationAbortsStreamingRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `event: conversation.response.started
data: {"type":"conversation.response.started","conversation_id":"conv_cancel"}

event: message.output.delta
data: {"type":"message.output.delta","output_index":0,"content_index":0,"content":"partial text"}

event: function.call.delta
data: {"type":"function.call.delta","output_index":1,"tool_call_id":"call_partial","name":"lookup","arguments":"{\"city\":\"Melbourne\"}"}

`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("mistral-cancel-test")
	model := mistralTestModel(providerID)
	client := mistralTestClient(t, providerID, model, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	stream := client.Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	for {
		event := receiveEvent(t, stream)
		if event.Kind == sigma.EventKindToolCallDelta {
			break
		}
	}
	cancel()

	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorAborted {
		t.Fatalf("Collect error = %v, want ErrorAborted", err)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Text, "partial text"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
	if got, want := final.Content[1].ToolCallID, "call_partial"; got != want {
		t.Fatalf("partial tool id = %q, want %q", got, want)
	}
	args := final.Content[1].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("partial tool city = %v, want %v", got, want)
	}
}

func mistralTestClient(t *testing.T, providerID sigma.ProviderID, model sigma.Model, baseURL string, opts ...mistral.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]mistral.ProviderOption{mistral.WithBaseURL(baseURL)}, opts...)
	if err := registry.RegisterTextProvider(providerID, mistral.NewProvider(providerOpts...)); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-key"}, nil
	})
	return sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(resolver),
		sigma.WithDefaultHeader("X-Client", "client"),
	)
}

func mistralTestModel(providerID sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:                   "mistral-test",
		Provider:             providerID,
		API:                  sigma.APIMistralConversations,
		SupportedInputs:      []sigma.ContentBlockType{sigma.ContentBlockText},
		SupportsTools:        true,
		InputCostPerMillion:  1,
		OutputCostPerMillion: 2,
	}
}

func captureRequest(t *testing.T, requests chan<- capturedRequest, r *http.Request) {
	t.Helper()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("ReadAll request body returned error: %v", err)
	}
	requests <- capturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Query:   r.URL.RawQuery,
		Headers: r.Header.Clone(),
		Body:    string(body),
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

func decodePayload(t *testing.T, body string) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload
}

func invalidProviderText() string {
	return string([]byte{0xff}) + "clean"
}

func writeMistralSSE(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Response", "seen")
	_, _ = io.WriteString(w, body)
}

func collectEvents(t *testing.T, stream *sigma.Stream) []sigma.Event {
	t.Helper()

	var events []sigma.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events
}

func receiveEvent(t *testing.T, stream *sigma.Stream) sigma.Event {
	t.Helper()

	event, ok := <-stream.Events()
	if !ok {
		t.Fatal("stream closed before event")
	}
	return event
}

func eventKinds(events []sigma.Event) []sigma.EventKind {
	kinds := make([]sigma.EventKind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	return kinds
}

func assertHeader(t *testing.T, headers http.Header, key string, want string) {
	t.Helper()

	if got := headers.Get(key); got != want {
		t.Fatalf("%s header = %q, want %q", key, got, want)
	}
}

func assertHeaderAbsent(t *testing.T, headers http.Header, key string) {
	t.Helper()

	if got := headers.Get(key); got != "" {
		t.Fatalf("%s header = %q, want absent", key, got)
	}
}

const completedEvent = `event: conversation.response.started
data: {"type":"conversation.response.started","conversation_id":"conv_complete"}

event: message.output.delta
data: {"type":"message.output.delta","output_index":0,"id":"msg_complete","content_index":0,"model":"mistral-test","role":"assistant","content":"ok"}

event: conversation.response.done
data: {"type":"conversation.response.done","usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}
`
