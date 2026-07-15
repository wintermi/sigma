// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google_test

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
	"github.com/wintermi/sigma/provider/google"
)

type capturedRequest struct {
	Method  string
	Path    string
	Query   string
	Headers http.Header
	Body    string
}

func TestRegisterReportsGenerativeAIAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("google-compatible")
	if err := google.Register(registry, providerID); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(googleTestModel(providerID)); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIGoogleGenerativeAI; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestCompleteUsesModelBaseURLAndHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-model-metadata-test")
	model := googleTestModel(providerID)
	model.ProviderMetadata = map[string]any{
		"baseURL": server.URL + "/model-base",
		"headers": map[string]any{
			"Authorization":  "Bearer metadata-secret",
			"X-Goog-Api-Key": "metadata-key",
			"X-Model":        "model",
			"X-Shared":       "model",
		},
	}
	client := googleTestClient(
		t,
		providerID,
		model,
		"https://provider-base.invalid",
		google.WithHeader("X-Shared", "provider"),
	)

	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithHeaders(map[string]string{"X-Shared": "request"}),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/model-base/models/gemini-test:streamGenerateContent"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "X-Goog-Api-Key", "resolved-key")
	assertHeader(t, request.Headers, "X-Model", "model")
	assertHeader(t, request.Headers, "X-Shared", "request")
	if got := request.Headers.Get("Authorization"); got != "" {
		t.Fatalf("authorization header = %q, want empty", got)
	}
}

func TestCompleteProviderBaseURLOptionOverridesModelMetadata(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-model-base-url-override-test")
	model := googleTestModel(providerID)
	model.ProviderMetadata = map[string]any{"baseURL": "https://model-base.invalid"}
	client := googleTestClient(t, providerID, model, "https://provider-base.invalid")

	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithProviderOptions(providerID, map[string]any{"base_url": server.URL + "/option-base"}),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/option-base/models/gemini-test:streamGenerateContent"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestGenerativePayloadSynthesizesUnansweredToolCallsBeforeUserTurn(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-drop-unanswered-tool-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

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

	var payload map[string]any
	if err := json.Unmarshal([]byte(receiveRequest(t, requests).Body), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	contents := payload["contents"].([]any)
	if got, want := len(contents), 3; got != want {
		t.Fatalf("content count = %d, want %d", got, want)
	}
	assistant := contents[0].(map[string]any)
	if got, want := assistant["role"], "model"; got != want {
		t.Fatalf("assistant role = %v, want %q", got, want)
	}
	toolResult := contents[1].(map[string]any)
	if got, want := toolResult["role"], "user"; got != want {
		t.Fatalf("tool result role = %v, want %q", got, want)
	}
	response := toolResult["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if got, want := response["name"], "lookup"; got != want {
		t.Fatalf("synthetic response name = %v, want %q", got, want)
	}
	if got, want := response["response"].(map[string]any)["error"], "No result provided"; got != want {
		t.Fatalf("synthetic response error = %v, want %q", got, want)
	}
	user := contents[2].(map[string]any)
	if got, want := user["role"], "user"; got != want {
		t.Fatalf("user role = %v, want %q", got, want)
	}
}

func TestGenerativePayloadNormalizesProviderText(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-normalized-payload-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)
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

	var payload map[string]any
	if err := json.Unmarshal([]byte(receiveRequest(t, requests).Body), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	system := payload["systemInstruction"].(map[string]any)
	systemParts := system["parts"].([]any)
	if got, want := systemParts[0].(map[string]any)["text"], "systemclean"; got != want {
		t.Fatalf("system text = %v, want %q", got, want)
	}
	contents := payload["contents"].([]any)
	assertGoogleTextPart(t, contents[0], "developerclean")
	assertGoogleTextPart(t, contents[1], "userclean")
	assistant := contents[2].(map[string]any)
	assistantParts := assistant["parts"].([]any)
	if got, want := assistantParts[0].(map[string]any)["text"], "assistantclean"; got != want {
		t.Fatalf("assistant text = %v, want %q", got, want)
	}
	if got, want := assistantParts[1].(map[string]any)["text"], "thinkingclean"; got != want {
		t.Fatalf("thinking text = %v, want %q", got, want)
	}
	tool := contents[3].(map[string]any)
	toolParts := tool["parts"].([]any)
	functionResponse := toolParts[0].(map[string]any)["functionResponse"].(map[string]any)
	response := functionResponse["response"].(map[string]any)
	if got, want := response["output"], "toolclean"; got != want {
		t.Fatalf("tool output = %v, want %q", got, want)
	}
}

func TestCompleteSendsGoldenPayloadWithImagesToolsThinkingAndHooks(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	payloadHookCalled := false
	responseHookCalled := false
	providerID := sigma.ProviderID("google-payload-test")
	model := googleTestModel(providerID)
	client := googleTestClient(
		t,
		providerID,
		model,
		server.URL,
		google.WithHeader("X-Provider", "provider"),
		google.WithPayloadHook(func(_ context.Context, _ sigma.Model, _ sigma.Request, _ sigma.Options, payload map[string]any) error {
			payloadHookCalled = true
			payload["labels"] = map[string]any{"hooked": true}
			return nil
		}),
		google.WithResponseHook(func(_ context.Context, _ sigma.Model, _ sigma.Options, resp *http.Response) error {
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
		richRequest(),
		sigma.WithTemperature(0.2),
		sigma.WithMaxTokens(123),
		sigma.WithHeader("X-Custom", "custom"),
		sigma.WithGoogleOptions(sigma.GoogleOptions{ThinkingBudgetTokens: intPtr(2048)}),
		sigma.WithProviderOptions(providerID, map[string]any{
			"function_calling_config": map[string]any{"mode": "AUTO"},
			"response_mime_type":      "text/plain",
			"extra_body":              map[string]any{"cachedContent": "cachedContents/abc"},
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
	if got, want := final.ProviderMetadata["id"], "resp_complete"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/models/gemini-test:streamGenerateContent"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Query, "alt=sse"; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "X-Goog-Api-Key", "resolved-key")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	goldentest.AssertJSON(t, request.Body, "provider/google/generative/rich_payload.json")
}

func TestCompleteSendsProviderDefinedToolsPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-provider-tools-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("Search current docs.")},
		Tools: []sigma.Tool{
			{
				Name:        "lookup",
				Description: "Lookup local records",
				InputSchema: sigma.Schema{
					"type":       "object",
					"properties": map[string]any{"query": map[string]any{"type": "string"}},
					"required":   []any{"query"},
				},
			},
			google.Tools.GoogleSearch(
				google.WithWebSearch(),
				google.WithTimeRange("2026-01-01T00:00:00Z", "2026-01-31T23:59:59Z"),
			),
			google.Tools.URLContext(),
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/google/generative/provider_defined_tools_payload.json")
}

func TestCompleteSendsTypedGoogleToolChoice(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-tool-choice-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("Pick a tool.")},
			Tools: []sigma.Tool{{
				Name:        "lookup",
				Description: "Lookup records",
				InputSchema: sigma.Schema{"type": "object"},
			}},
		},
		sigma.WithGoogleOptions(sigma.GoogleOptions{ToolChoice: "any"}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodeRequestPayload(t, receiveRequest(t, requests).Body)
	toolConfig := payload["toolConfig"].(map[string]any)
	functionCalling := toolConfig["functionCallingConfig"].(map[string]any)
	if got, want := functionCalling["mode"], "ANY"; got != want {
		t.Fatalf("tool choice mode = %v, want %v", got, want)
	}
}

func TestInvalidGoogleToolChoiceFailsBeforeNetwork(t *testing.T) {
	t.Parallel()

	providerID := sigma.ProviderID("google-invalid-tool-choice-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, "https://example.invalid")

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithGoogleOptions(sigma.GoogleOptions{ToolChoice: "required"}),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
}

func TestGenerativePayloadOmitsEmptyAssistantReplayBlocks(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-empty-assistant-replay-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("first"),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.Text(""),
					sigma.Thinking("", ""),
				},
			},
			sigma.UserText("second"),
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodeRequestPayload(t, receiveRequest(t, requests).Body)
	contents := payload["contents"].([]any)
	if got, want := len(contents), 2; got != want {
		t.Fatalf("contents = %#v, want %d turns", contents, want)
	}
	for _, content := range contents {
		typed := content.(map[string]any)
		if got := typed["role"]; got == "model" {
			t.Fatalf("empty model turn was serialized: %#v", contents)
		}
	}
}

func TestDisabledThinkingPayloadsForGeminiFamilies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		modelID   sigma.ModelID
		wantKey   string
		wantValue any
	}{
		{name: "gemini 2", modelID: "gemini-2.5-flash", wantKey: "thinkingBudget", wantValue: float64(0)},
		{name: "gemini 3 pro", modelID: "gemini-3.1-pro", wantKey: "thinkingLevel", wantValue: "LOW"},
		{name: "gemini 3 flash", modelID: "gemini-3-flash", wantKey: "thinkingLevel", wantValue: "MINIMAL"},
		{name: "gemma 4", modelID: "gemma-4-preview", wantKey: "thinkingLevel", wantValue: "MINIMAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeGoogleSSE(t, w, completedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("google-disable-thinking-test-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := googleTestModel(providerID)
			model.ID = tt.modelID
			client := googleTestClient(t, providerID, model, server.URL)

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithReasoningLevel(sigma.ThinkingLevelOff),
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			payload := decodeRequestPayload(t, receiveRequest(t, requests).Body)
			generation := payload["generationConfig"].(map[string]any)
			thinking := generation["thinkingConfig"].(map[string]any)
			if got := thinking[tt.wantKey]; got != tt.wantValue {
				t.Fatalf("thinking %s = %v, want %v", tt.wantKey, got, tt.wantValue)
			}
			if _, ok := thinking["includeThoughts"]; ok {
				t.Fatalf("includeThoughts = %v, want omitted", thinking["includeThoughts"])
			}
		})
	}
}

func TestAssistantReplayOnlyKeepsSameModelBase64ThoughtSignatures(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-signature-replay-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)
	validSignature := "AAAAAAAAAAAAAAAAAAAAAA=="

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			{
				Role:     sigma.RoleAssistant,
				Provider: providerID,
				API:      sigma.APIGoogleGenerativeAI,
				Model:    model.ID,
				Content: []sigma.ContentBlock{
					withProviderSignature(sigma.Text("same model"), validSignature),
					withProviderSignature(sigma.Text("invalid signature"), "not_base64"),
				},
			},
			{
				Role:     sigma.RoleAssistant,
				Provider: sigma.ProviderAnthropic,
				API:      sigma.APIAnthropicMessages,
				Model:    "claude-sonnet",
				Content:  []sigma.ContentBlock{withProviderSignature(sigma.Text("other provider"), validSignature)},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodeRequestPayload(t, receiveRequest(t, requests).Body)
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if got, want := strings.Count(string(data), "thoughtSignature"), 1; got != want {
		t.Fatalf("thoughtSignature count = %d, want %d\n%s", got, want, data)
	}
}

func TestAssistantToolCallReplayOnlyKeepsSameModelBase64ThoughtSignatures(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-tool-signature-replay-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)
	validSignature := "AAAAAAAAAAAAAAAAAAAAAA=="

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			{
				Role:     sigma.RoleAssistant,
				Provider: providerID,
				API:      sigma.APIGoogleGenerativeAI,
				Model:    model.ID,
				Content: []sigma.ContentBlock{
					withProviderSignature(sigma.ToolCallBlock("call_valid", "lookup", map[string]any{"query": "valid"}), validSignature),
					withProviderSignature(sigma.ToolCallBlock("call_invalid", "lookup", map[string]any{"query": "invalid"}), "not_base64"),
					sigma.ToolCallBlock("call_unsigned", "lookup", map[string]any{"query": "unsigned"}),
				},
			},
			{
				Role:     sigma.RoleAssistant,
				Provider: sigma.ProviderAnthropic,
				API:      sigma.APIAnthropicMessages,
				Model:    "claude-sonnet",
				Content: []sigma.ContentBlock{
					withProviderSignature(sigma.ToolCallBlock("call_foreign", "lookup", map[string]any{"query": "foreign"}), validSignature),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodeRequestPayload(t, receiveRequest(t, requests).Body)
	contents := payload["contents"].([]any)
	toolParts := make(map[string]map[string]any)
	for _, content := range contents {
		parts, ok := content.(map[string]any)["parts"].([]any)
		if !ok {
			continue
		}
		for _, part := range parts {
			typed, ok := part.(map[string]any)
			if !ok {
				continue
			}
			call, ok := typed["functionCall"].(map[string]any)
			if !ok {
				continue
			}
			id, _ := call["id"].(string)
			toolParts[id] = typed
		}
	}
	if got, want := len(toolParts), 4; got != want {
		t.Fatalf("tool-call parts = %d, want %d", got, want)
	}
	if got, want := toolParts["call_valid"]["thoughtSignature"], validSignature; got != want {
		t.Fatalf("valid tool signature = %v, want %q", got, want)
	}
	for _, id := range []string{"call_invalid", "call_unsigned", "call_foreign"} {
		if _, ok := toolParts[id]["thoughtSignature"]; ok {
			t.Fatalf("tool call %q retained a non-native thought signature: %#v", id, toolParts[id])
		}
	}
}

func TestToolResultsMergeAndRouteImagesByGeminiVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		modelID       sigma.ModelID
		wantContents  int
		wantNested    bool
		wantSidecar   bool
		wantResponses int
	}{
		{name: "gemini 2 sidecar", modelID: "gemini-2.5-flash", wantContents: 5, wantSidecar: true, wantResponses: 2},
		{name: "gemini 3 nested", modelID: "gemini-3-pro", wantContents: 3, wantNested: true, wantResponses: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeGoogleSSE(t, w, completedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("google-image-tool-result-test-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := googleTestModel(providerID)
			model.ID = tt.modelID
			client := googleTestClient(t, providerID, model, server.URL)

			_, err := client.Complete(context.Background(), model, sigma.Request{
				Messages: []sigma.Message{
					sigma.UserText("read files"),
					{
						Role: sigma.RoleAssistant,
						Content: []sigma.ContentBlock{
							sigma.ToolCallBlock("call_a", "read", map[string]any{"path": "a.txt"}),
							sigma.ToolCallBlock("call_img", "read", map[string]any{"path": "image.png"}),
							sigma.ToolCallBlock("call_b", "read", map[string]any{"path": "b.txt"}),
						},
					},
					{Role: sigma.RoleTool, ToolCallID: "call_a", ToolName: "read", Content: []sigma.ContentBlock{sigma.Text("alpha")}},
					{Role: sigma.RoleTool, ToolCallID: "call_img", ToolName: "read", Content: []sigma.ContentBlock{sigma.ImageBase64("image/png", "aW1hZ2U=")}},
					{Role: sigma.RoleTool, ToolCallID: "call_b", ToolName: "read", Content: []sigma.ContentBlock{sigma.Text("beta")}},
				},
			})
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			payload := decodeRequestPayload(t, receiveRequest(t, requests).Body)
			contents := payload["contents"].([]any)
			if got := len(contents); got != tt.wantContents {
				t.Fatalf("contents = %d, want %d", got, tt.wantContents)
			}
			toolTurn := contents[2].(map[string]any)
			parts := toolTurn["parts"].([]any)
			if got := len(parts); got != tt.wantResponses {
				t.Fatalf("function responses = %d, want %d", got, tt.wantResponses)
			}
			imageResponse := parts[1].(map[string]any)["functionResponse"].(map[string]any)
			if _, ok := imageResponse["parts"]; ok != tt.wantNested {
				t.Fatalf("nested image parts present = %v, want %v", ok, tt.wantNested)
			}
			if tt.wantSidecar {
				sidecar := contents[3].(map[string]any)
				sidecarParts := sidecar["parts"].([]any)
				if got, want := sidecarParts[0].(map[string]any)["text"], "Tool result image:"; got != want {
					t.Fatalf("sidecar text = %v, want %v", got, want)
				}
			}
		})
	}
}

func TestNativeGeminiOmitsEmptyFunctionResponseID(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-empty-tool-id-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("read"),
			{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.ToolCallBlock("", "read", map[string]any{"path": "a.txt"})},
			},
			{
				Role:     sigma.RoleTool,
				ToolName: "read",
				Content:  []sigma.ContentBlock{sigma.Text("alpha")},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodeRequestPayload(t, receiveRequest(t, requests).Body)
	contents := payload["contents"].([]any)
	toolTurn := contents[2].(map[string]any)
	parts := toolTurn["parts"].([]any)
	response := parts[0].(map[string]any)["functionResponse"].(map[string]any)
	if _, ok := response["id"]; ok {
		t.Fatalf("function response id = %v, want omitted", response["id"])
	}
}

func TestHostedGoogleToolCallIDsAreNormalizedForReplay(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-hosted-tool-id-test")
	model := googleTestModel(providerID)
	model.ID = "claude-test"
	client := googleTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("read"),
			{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.ToolCallBlock("call:prev/with spaces", "read", map[string]any{"path": "a.txt"})},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call:prev/with spaces",
				ToolName:   "read",
				Content:    []sigma.ContentBlock{sigma.Text("alpha")},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodeRequestPayload(t, receiveRequest(t, requests).Body)
	contents := payload["contents"].([]any)
	modelTurn := contents[1].(map[string]any)
	modelParts := modelTurn["parts"].([]any)
	call := modelParts[0].(map[string]any)["functionCall"].(map[string]any)
	if got, want := call["id"], "call_prev_with_spaces"; got != want {
		t.Fatalf("function call id = %v, want %v", got, want)
	}
	toolTurn := contents[2].(map[string]any)
	toolParts := toolTurn["parts"].([]any)
	response := toolParts[0].(map[string]any)["functionResponse"].(map[string]any)
	if got, want := response["id"], "call_prev_with_spaces"; got != want {
		t.Fatalf("function response id = %v, want %v", got, want)
	}
}

func TestLegacyGoogleToolSchemaFormatSanitizesJSONSchemaMeta(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-legacy-schema-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)
	originalSchema := sigma.Schema{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"$comment":    "caller-owned schema metadata",
		"$defs":       map[string]any{"unused": map[string]any{"type": "string"}},
		"definitions": map[string]any{"legacy": map[string]any{"type": "number"}},
		"type":        "object",
		"properties": map[string]any{
			"query": map[string]any{
				"$id":      "urn:query",
				"$comment": "nested metadata",
				"$ref":     "#/$defs/query",
				"type":     "string",
			},
		},
	}
	wantOriginal := sigma.Schema{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"$comment":    "caller-owned schema metadata",
		"$defs":       map[string]any{"unused": map[string]any{"type": "string"}},
		"definitions": map[string]any{"legacy": map[string]any{"type": "number"}},
		"type":        "object",
		"properties": map[string]any{
			"query": map[string]any{
				"$id":      "urn:query",
				"$comment": "nested metadata",
				"$ref":     "#/$defs/query",
				"type":     "string",
			},
		},
	}

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("lookup")},
			Tools: []sigma.Tool{{
				Name:        "lookup",
				Description: "Lookup records",
				InputSchema: originalSchema,
			}},
		},
		sigma.WithProviderOptions(providerID, map[string]any{"tool_schema_format": "parameters"}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodeRequestPayload(t, receiveRequest(t, requests).Body)
	declaration := payload["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	if _, ok := declaration["parametersJsonSchema"]; ok {
		t.Fatal("parametersJsonSchema present, want legacy parameters")
	}
	parameters := declaration["parameters"].(map[string]any)
	if _, ok := parameters["$schema"]; ok {
		t.Fatal("$schema was not removed")
	}
	if _, ok := parameters["$comment"]; ok {
		t.Fatal("$comment was not removed")
	}
	if _, ok := parameters["$defs"]; ok {
		t.Fatal("$defs was not removed")
	}
	if _, ok := parameters["definitions"]; ok {
		t.Fatal("definitions was not removed")
	}
	property := parameters["properties"].(map[string]any)["query"].(map[string]any)
	if _, ok := property["$id"]; ok {
		t.Fatal("nested $id was not removed")
	}
	if _, ok := property["$comment"]; ok {
		t.Fatal("nested $comment was not removed")
	}
	if got, want := property["$ref"], "#/$defs/query"; got != want {
		t.Fatalf("property $ref = %v, want %v", got, want)
	}
	if got, want := property["type"], "string"; got != want {
		t.Fatalf("property type = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(originalSchema, wantOriginal) {
		t.Fatalf("original schema was mutated: got %#v want %#v", originalSchema, wantOriginal)
	}
}

func TestImagePayloadUsesGooglePartShapes(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-image-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserContent(
			sigma.ImageBase64("image/png", "aGk="),
			sigma.ImageURL("image/jpeg", "https://example.test/cat.jpg"),
		)},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/google/generative/image_payload.json")
}

func TestStreamingMapsTextThinkingUsageAndMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGoogleSSE(t, w, `data: {"responseId":"resp_stream","modelVersion":"gemini-test-version","candidates":[{"content":{"role":"model","parts":[{"thought":true,"text":"Checked ","thoughtSignature":"think_sig"}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":" world"}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":"","thoughtSignature":"text_sig"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":8,"totalTokenCount":18,"cachedContentTokenCount":3,"thoughtsTokenCount":2}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-stream-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

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
	if got, want := final.Content[0].ThinkingText, "Checked "; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderSignature, "think_sig"; got != want {
		t.Fatalf("thinking signature = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "Hello world"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := final.Content[1].ProviderSignature, "text_sig"; got != want {
		t.Fatalf("text signature = %q, want %q", got, want)
	}
	if got, want := final.ProviderMetadata["modelVersion"], "gemini-test-version"; got != want {
		t.Fatalf("model version = %v, want %v", got, want)
	}
	if final.Usage == nil {
		t.Fatal("final usage was nil")
	}
	if got, want := final.Usage.CacheReadInputTokens, 3; got != want {
		t.Fatalf("cache read tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.InputTokens, 7; got != want {
		t.Fatalf("input tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.OutputTokens, 10; got != want {
		t.Fatalf("output tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.ThinkingTokens, 2; got != want {
		t.Fatalf("thinking tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.Provider, providerID; got != want {
		t.Fatalf("usage provider = %q, want %q", got, want)
	}
	if got, want := final.Usage.Model, model.ID; got != want {
		t.Fatalf("usage model = %q, want %q", got, want)
	}
	if got, want := final.Usage.Raw["promptTokenCount"], float64(10); got != want {
		t.Fatalf("raw prompt token count = %v, want %v", got, want)
	}
	if events[len(events)-1].Usage == nil || events[len(events)-1].Usage.Raw["promptTokenCount"] != float64(10) {
		t.Fatalf("terminal usage = %#v, want raw prompt token count", events[len(events)-1].Usage)
	}
}

func TestStreamingThoughtSignatureOnlyPartUpdatesCurrentBlock(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGoogleSSE(t, w, `data: {"responseId":"resp_signature","candidates":[{"content":{"role":"model","parts":[{"text":"Hello","thoughtSignature":"sig_initial"}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":" world"}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":"","thoughtSignature":""}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"thoughtSignature":"sig_updated"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-signature-only-stream-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

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
	if got, want := len(final.Content), 1; got != want {
		t.Fatalf("content count = %d, want %d", got, want)
	}
	if got, want := final.Content[0].Text, "Hello world"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderSignature, "sig_updated"; got != want {
		t.Fatalf("provider signature = %q, want %q", got, want)
	}
}

func TestCompleteFunctionCallArgumentsEmitToolCallEvents(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGoogleSSE(t, w, `data: {"responseId":"resp_tool","candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"call_weather","name":"weather","args":{"city":"Melbourne"}},"thoughtSignature":"tool_sig"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":6}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-tool-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

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
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	delta := events[2].PartialToolCall
	if delta == nil {
		t.Fatal("tool-call delta was nil")
	}
	if got, want := delta.ArgumentsDelta, `{"city":"Melbourne"}`; got != want {
		t.Fatalf("arguments delta = %q, want %q", got, want)
	}
	if got, want := delta.ProviderSignature, "tool_sig"; got != want {
		t.Fatalf("partial signature = %q, want %q", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonToolCalls; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ToolCallID, "call_weather"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	args := final.Content[0].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("tool city = %v, want %v", got, want)
	}
	if got, want := final.Content[0].ProviderSignature, "tool_sig"; got != want {
		t.Fatalf("tool signature = %q, want %q", got, want)
	}
}

func TestCompletePreservesGroundingMetadataSources(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGoogleSSE(t, w, `data: {"responseId":"resp_grounded","candidates":[{"content":{"role":"model","parts":[{"text":"Grounded answer."}]},"finishReason":"STOP","groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://example.com","title":"Example"}},{"retrievedContext":{"uri":"gs://bucket/doc.pdf","title":"Doc"}}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-grounding-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("ground")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "Grounded answer."; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if final.ProviderMetadata["groundingMetadata"] == nil {
		t.Fatal("grounding metadata missing")
	}
	sources, ok := final.ProviderMetadata["sources"].([]map[string]any)
	if !ok || len(sources) != 2 {
		t.Fatalf("sources = %#v, want two normalized sources", final.ProviderMetadata["sources"])
	}
	if got, want := sources[0]["url"], "https://example.com"; got != want {
		t.Fatalf("source url = %v, want %v", got, want)
	}
	if got, want := sources[1]["uri"], "gs://bucket/doc.pdf"; got != want {
		t.Fatalf("source uri = %v, want %v", got, want)
	}
	resultSources := final.Sources()
	if got, want := len(resultSources), 2; got != want {
		t.Fatalf("result source count = %d, want %d", got, want)
	}
	if got, want := resultSources[0].Type, "web"; got != want {
		t.Fatalf("result source type = %q, want %q", got, want)
	}
	if got, want := resultSources[0].URL, "https://example.com"; got != want {
		t.Fatalf("result source url = %q, want %q", got, want)
	}
	if got, want := resultSources[1].Type, "retrievedContext"; got != want {
		t.Fatalf("result retrieved source type = %q, want %q", got, want)
	}
	if got, want := resultSources[1].URI, "gs://bucket/doc.pdf"; got != want {
		t.Fatalf("result retrieved source uri = %q, want %q", got, want)
	}
}

func TestFunctionCallArgumentsGenerateSyntheticIDsWhenMissingOrDuplicate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGoogleSSE(t, w, `data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"query":"one"}}},{"functionCall":{"id":"call_existing","name":"lookup","args":{"query":"two"}}},{"functionCall":{"id":"call_existing","name":"lookup","args":{"query":"three"}}}]},"finishReason":"STOP"}]}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-synthetic-tool-id-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("lookup")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.StopReason, sigma.StopReasonToolCalls; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := len(final.Content), 3; got != want {
		t.Fatalf("tool calls = %d, want %d", got, want)
	}
	if got, want := final.Content[0].ToolCallID, "google_tool_call_1"; got != want {
		t.Fatalf("first tool call id = %q, want %q", got, want)
	}
	if got, want := final.Content[1].ToolCallID, "call_existing"; got != want {
		t.Fatalf("second tool call id = %q, want %q", got, want)
	}
	if got, want := final.Content[2].ToolCallID, "google_tool_call_2"; got != want {
		t.Fatalf("third tool call id = %q, want %q", got, want)
	}
}

func TestThinkingLevelMustBeSupportedByModelMetadata(t *testing.T) {
	t.Parallel()

	providerID := sigma.ProviderID("google-thinking-test")
	model := googleTestModel(providerID)
	model.ThinkingLevelMap = map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "HIGH"}
	client := googleTestClient(t, providerID, model, "https://example.invalid")

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithReasoningLevel(sigma.ThinkingLevelLow),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
}

func TestProviderErrorIsTypedAndRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-goog-request-id", "req_123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key sk-secret123","status":"UNAUTHENTICATED"}}`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-error-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.Diagnostics[0].API, sigma.APIGoogleGenerativeAI; got != want {
		t.Fatalf("diagnostic API = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "sk-secret123") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestStreamErrorEventEndsWithError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGoogleSSE(t, w, `data: {"error":{"code":500,"status":"INTERNAL","message":"overloaded"}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-stream-error-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	classification := sigma.ClassifyError(err)
	if got, want := classification.Class, sigma.ErrorClassTransient; got != want {
		t.Fatalf("class = %q, want %q", got, want)
	}
	if got, want := classification.ProviderCode, "INTERNAL"; got != want {
		t.Fatalf("provider code = %q, want %q", got, want)
	}
	if !strings.Contains(err.Error(), "INTERNAL") {
		t.Fatalf("error = %v, want INTERNAL", err)
	}
}

func TestMalformedFunctionCallFinishReasonMapsToErrorWithoutToolCalls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGoogleSSE(t, w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]},"finishReason":"MALFORMED_FUNCTION_CALL"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-malformed-function-call-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got := len(final.Content); got != 1 || final.Content[0].Type != sigma.ContentBlockText {
		t.Fatalf("content = %#v, want text only", final.Content)
	}
}

func TestRetryResendsRequestAfterRetryableStatus(t *testing.T) {
	t.Parallel()

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
			return
		}
		writeGoogleSSE(t, w, completedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-retry-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

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

func TestCancellationAbortsStreamingRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"responseId":"resp_cancel"}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"thought":true,"text":"partial plan"}]}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"partial text"}]}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"call_partial","name":"lookup","args":{"city":"Melbourne"}}}]}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-cancel-test")
	model := googleTestModel(providerID)
	client := googleTestClient(t, providerID, model, server.URL)

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
	if got, want := final.Content[0].ThinkingText, "partial plan"; got != want {
		t.Fatalf("partial thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "partial text"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
	if got, want := final.Content[2].ToolCallID, "call_partial"; got != want {
		t.Fatalf("partial tool id = %q, want %q", got, want)
	}
	args := final.Content[2].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("partial tool city = %v, want %v", got, want)
	}
}

func googleTestClient(t *testing.T, providerID sigma.ProviderID, model sigma.Model, baseURL string, opts ...google.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]google.ProviderOption{google.WithBaseURL(baseURL)}, opts...)
	if err := registry.RegisterTextProvider(providerID, google.NewProvider(providerOpts...)); err != nil {
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

func googleTestModel(providerID sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:       "gemini-test",
		Provider: providerID,
		API:      sigma.APIGoogleGenerativeAI,
		SupportedInputs: []sigma.ContentBlockType{
			sigma.ContentBlockText,
			sigma.ContentBlockImage,
		},
		SupportsTools:        true,
		SupportsThinking:     true,
		ThinkingLevelMap:     map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "HIGH"},
		InputCostPerMillion:  1,
		OutputCostPerMillion: 2,
	}
}

func richRequest() sigma.Request {
	previousCall := sigma.ToolCallBlock("call_prev", "lookup", map[string]any{"query": "weather"})
	previousCall.ProviderSignature = "tool_previous_sig"
	previousText := sigma.Text("Earlier answer.")
	previousText.ProviderSignature = "text_previous_sig"
	previousThinking := sigma.Thinking("Internal summary.", "")
	previousThinking.ProviderSignature = "think_previous_sig"

	return sigma.Request{
		SystemPrompt: "You are helpful.",
		Messages: []sigma.Message{
			{
				Role:    sigma.RoleDeveloper,
				Content: []sigma.ContentBlock{sigma.Text("Use terse answers.")},
			},
			sigma.UserContent(
				sigma.Text("Describe this"),
				sigma.ImageURL("image/png", "https://example.test/cat.png"),
				sigma.ImageBase64("image/png", "aGk="),
			),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					previousText,
					previousThinking,
					previousCall,
				},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call_prev",
				Content:    []sigma.ContentBlock{sigma.Text("Sunny")},
			},
		},
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

func invalidProviderText() string {
	return string([]byte{0xff}) + "clean"
}

func assertGoogleTextPart(t *testing.T, content any, want string) {
	t.Helper()

	typed := content.(map[string]any)
	parts := typed["parts"].([]any)
	if got := parts[0].(map[string]any)["text"]; got != want {
		t.Fatalf("text part = %v, want %q", got, want)
	}
}

func writeGoogleSSE(t *testing.T, w http.ResponseWriter, body string) {
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

func decodeRequestPayload(t *testing.T, body string) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode request payload: %v", err)
	}
	return payload
}

func withProviderSignature(block sigma.ContentBlock, signature string) sigma.ContentBlock {
	block.ProviderSignature = signature
	return block
}

func intPtr(value int) *int {
	return &value
}

const completedEvent = `data: {"responseId":"resp_complete","candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}
`
