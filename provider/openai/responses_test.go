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
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
	"github.com/wintermi/sigma/provider/openai"
)

func TestRegisterResponsesReportsResponsesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("responses-compatible")
	if err := openai.RegisterResponses(registry, providerID); err != nil {
		t.Fatalf("RegisterResponses returned error: %v", err)
	}
	if err := registry.RegisterModel(responsesTestModel(providerID)); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIOpenAIResponses; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestResponsesCompleteSendsGoldenPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-payload-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL, openai.WithHeader("X-Provider", "provider"))
	parallelToolCalls := false

	final, err := client.Complete(
		context.Background(),
		model,
		responsesRichRequest(),
		sigma.WithTemperature(0.2),
		sigma.WithMaxTokens(123),
		sigma.WithSessionID("resp_prev"),
		sigma.WithHeader("X-Custom", "custom"),
		sigma.WithMetadataValue("trace", "abc"),
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{
			ReasoningEffort:      sigma.ThinkingLevelHigh,
			ReasoningSummary:     "auto",
			ServiceTier:          "default",
			ToolChoice:           "auto",
			PromptCacheRetention: "24h",
			ParallelToolCalls:    &parallelToolCalls,
			TextVerbosity:        "low",
		}),
		sigma.WithProviderOptions(providerID, map[string]any{
			"session_id_header": "X-Session-ID",
			"store":             false,
			"include":           []any{"reasoning.encrypted_content"},
			"text":              map[string]any{"format": map[string]any{"type": "text"}},
			"truncation":        "auto",
			"prompt_cache_key":  "cache-key",
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.ProviderMetadata["id"], "resp_complete"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer resolved-key")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	assertHeader(t, request.Headers, "X-Session-ID", "resp_prev")
	goldentest.AssertJSON(t, request.Body, "provider/openai/responses/rich_payload.json")
}

func TestResponsesDerivesPromptCacheKeyWithoutOverridingProviderOption(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-cache-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID(strings.Repeat("x", 70)),
		sigma.WithCacheRetention(sigma.CacheRetentionLong),
		sigma.WithProviderOption(providerID, "prompt_cache_key", "explicit-cache-key"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := payload["prompt_cache_key"], "explicit-cache-key"; got != want {
		t.Fatalf("prompt_cache_key = %v, want %q", got, want)
	}
	if got, want := payload["prompt_cache_retention"], "24h"; got != want {
		t.Fatalf("prompt_cache_retention = %v, want %q", got, want)
	}
	if got, want := payload["previous_response_id"], strings.Repeat("x", 70); got != want {
		t.Fatalf("previous_response_id = %v, want session id", got)
	}
}

func TestResponsesNormalizesProviderTextInPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-normalized-payload-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
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
						sigma.Thinking("thinking"+invalid, ""),
						sigma.ToolCallBlock("call_invalid", "lookup", map[string]any{"query": "weather"}),
					},
				},
				sigma.ToolResult("call_invalid", "tool"+invalid),
			},
			Tools: []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}},
		},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Instructions string           `json:"instructions"`
		Input        []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := payload.Instructions, "systemclean"; got != want {
		t.Fatalf("instructions = %q, want %q", got, want)
	}
	assertResponsesInputText(t, payload.Input[0], "developerclean")
	assertResponsesInputText(t, payload.Input[1], "userclean")
	assertResponsesOutputText(t, payload.Input[2], "assistantclean")
	assertResponsesReasoningText(t, payload.Input[3], "thinkingclean")
	assertResponsesToolOutputText(t, payload.Input[5], "toolclean")
}

func TestResponsesDropsUnansweredToolCallsBeforeUserTurn(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-drop-unanswered-tool-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

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

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := len(payload.Input), 1; got != want {
		t.Fatalf("input count = %d, want %d", got, want)
	}
	if got, want := payload.Input[0]["role"], "user"; got != want {
		t.Fatalf("input role = %v, want %q", got, want)
	}
	if got := payload.Input[0]["type"]; got != nil {
		t.Fatalf("payload kept provider item type = %v", got)
	}
}

func TestResponsesStreamingNormalizesInvalidUTF8(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(append([]byte(`data: {"type":"response.output_text.delta","response_id":"resp_bad","output_index":0,"item_id":"msg_bad","delta":"bad`), append([]byte{0xff}, []byte(`text"}`+"\n\n")...)...))
		_, _ = w.Write(append([]byte(`data: {"type":"response.completed","response":{"id":"resp_bad","model":"gpt-test","status":"completed","output":[{"type":"message","id":"msg_bad","role":"assistant","content":[{"type":"output_text","id":"text_bad","text":"bad`), append([]byte{0xff}, []byte(`text"}]}]}}`+"\n\n")...)...))
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-normalized-stream-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "badtext"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
}

func TestResponsesCopilotDynamicHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		request     sigma.Request
		options     []sigma.Option
		wantHeaders map[string]string
		wantAbsent  []string
	}{
		{
			name:    "user initiated",
			request: sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			wantHeaders: map[string]string{
				"X-Initiator":   "user",
				"Openai-Intent": "conversation-edits",
			},
			wantAbsent: []string{"Copilot-Vision-Request"},
		},
		{
			name: "agent initiated with images",
			request: sigma.Request{Messages: []sigma.Message{
				sigma.UserText("inspect"),
				{Role: sigma.RoleTool, ToolCallID: "call_1", Content: []sigma.ContentBlock{sigma.ImageBase64("image/png", "aGk=")}},
			}},
			wantHeaders: map[string]string{
				"X-Initiator":            "agent",
				"Openai-Intent":          "conversation-edits",
				"Copilot-Vision-Request": "true",
			},
		},
		{
			name:    "caller override",
			request: sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			options: []sigma.Option{
				sigma.WithHeader("X-Initiator", "override"),
				sigma.WithHeader("Openai-Intent", "override-intent"),
				sigma.WithHeader("Copilot-Vision-Request", "override-vision"),
			},
			wantHeaders: map[string]string{
				"X-Initiator":            "override",
				"Openai-Intent":          "override-intent",
				"Copilot-Vision-Request": "override-vision",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			model := responsesTestModel(sigma.ProviderGitHubCopilot)
			client := responsesTestClient(t, sigma.ProviderGitHubCopilot, model, server.URL)

			_, err := client.Complete(context.Background(), model, tt.request, tt.options...)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			headers := receiveRequest(t, requests).Headers
			for key, value := range tt.wantHeaders {
				assertHeader(t, headers, key, value)
			}
			for _, key := range tt.wantAbsent {
				assertHeaderAbsent(t, headers, key)
			}
		})
	}
}

func TestResponsesCloudflareGatewayBaseURLAndAuthHeader(t *testing.T) {
	t.Setenv("CLOUDFLARE_GATEWAY_ID", "openai")

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("cloudflare-ai-gateway")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL+"/{CLOUDFLARE_GATEWAY_ID}")

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithHeader("cf-aig-authorization", "Bearer override"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/openai/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "cf-aig-authorization", "Bearer override")
	assertHeaderAbsent(t, request.Headers, "Authorization")
}

func TestResponsesSessionAffinityHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-affinity-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID("responses-session"),
		sigma.WithCacheRetention(sigma.CacheRetentionShort),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	headers := receiveRequest(t, requests).Headers
	assertHeader(t, headers, "session_id", "responses-session")
	assertHeader(t, headers, "x-client-request-id", "responses-session")
}

func TestResponsesOmitsSessionAffinityHeadersWhenCacheDisabled(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-affinity-disabled-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID("responses-session"),
		sigma.WithCacheRetention(sigma.CacheRetentionNone),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	headers := receiveRequest(t, requests).Headers
	assertHeaderAbsent(t, headers, "session_id")
	assertHeaderAbsent(t, headers, "x-client-request-id")
}

func TestResponsesSendsTypedResponseFormatAsTextFormat(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-format-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("judge")}},
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{
			ResponseFormat: map[string]any{
				"type": "json_schema",
				"json_schema": map[string]any{
					"name":   "judge",
					"strict": true,
					"schema": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
					},
				},
			},
			TextVerbosity: "low",
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	text, ok := payload["text"].(map[string]any)
	if !ok {
		t.Fatalf("text type = %T, want map", payload["text"])
	}
	if got, want := text["verbosity"], "low"; got != want {
		t.Fatalf("text.verbosity = %v, want %q", got, want)
	}
	format, ok := text["format"].(map[string]any)
	if !ok {
		t.Fatalf("text.format type = %T, want map", text["format"])
	}
	if got, want := format["type"], "json_schema"; got != want {
		t.Fatalf("text.format.type = %v, want %q", got, want)
	}
	if got, want := format["name"], "judge"; got != want {
		t.Fatalf("text.format.name = %v, want %q", got, want)
	}
	if got, want := format["strict"], true; got != want {
		t.Fatalf("text.format.strict = %v, want %v", got, want)
	}
	if _, ok := format["json_schema"]; ok {
		t.Fatalf("text.format contains unflattened json_schema: %#v", format)
	}
	wantSchema := map[string]any{"type": "object", "additionalProperties": false}
	if !reflect.DeepEqual(format["schema"], wantSchema) {
		t.Fatalf("text.format.schema = %#v, want %#v", format["schema"], wantSchema)
	}
}

func TestResponsesNormalizesOpenAIOptionsFunctionToolChoice(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-tool-choice-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("call a tool")}},
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{ToolChoice: map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "read_file"},
		}}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	assertResponsesFunctionToolChoice(t, receiveRequest(t, requests).Body)
}

func TestResponsesNormalizesProviderOptionFunctionToolChoice(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-provider-tool-choice-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("call a tool")}},
		sigma.WithProviderOption(providerID, "tool_choice", map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "read_file"},
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	assertResponsesFunctionToolChoice(t, receiveRequest(t, requests).Body)
}

func TestResponsesUsesModelBaseURLAndHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-model-metadata-test")
	model := responsesTestModel(providerID)
	model.ProviderMetadata = map[string]any{
		"baseURL": server.URL + "/model-base",
		"headers": map[string]string{
			"Authorization": "Bearer metadata-secret",
			"X-Model":       "model",
		},
	}
	client := responsesTestClient(t, providerID, model, "https://provider-base.invalid")

	if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/model-base/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer resolved-key")
	assertHeader(t, request.Headers, "X-Model", "model")
}

func TestResponsesReplayNormalizesMissingAndForeignIDs(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-replay-id-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	foreignItemID := strings.Repeat("foreign/item+", 8)
	toolCallID := "call_foreign|" + foreignItemID

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("continue"),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.Text("first"),
					sigma.Text("second"),
					sigma.Thinking("prior reasoning", ""),
					sigma.ToolCallBlock(toolCallID, "lookup", map[string]any{"query": "weather"}),
				},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: toolCallID,
				Content: []sigma.ContentBlock{
					sigma.Text("A red circle."),
					sigma.ImageBase64("image/png", "aGk="),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var messageID, textPartID, reasoningID, functionItemID, functionCallID string
	var toolOutput []any
	for _, item := range payload.Input {
		switch item["type"] {
		case "message":
			messageID, _ = item["id"].(string)
			content, _ := item["content"].([]any)
			if len(content) > 0 {
				part, _ := content[0].(map[string]any)
				textPartID, _ = part["id"].(string)
			}
		case "reasoning":
			reasoningID, _ = item["id"].(string)
		case "function_call":
			functionItemID, _ = item["id"].(string)
			functionCallID, _ = item["call_id"].(string)
		case "function_call_output":
			toolOutput, _ = item["output"].([]any)
		}
	}

	assertResponsesID(t, messageID, "msg_")
	assertResponsesID(t, textPartID, "text_")
	assertResponsesID(t, reasoningID, "rs_")
	assertResponsesID(t, functionItemID, "fc_")
	if got, want := functionCallID, "call_foreign"; got != want {
		t.Fatalf("function call_id = %q, want %q", got, want)
	}
	if len(toolOutput) != 2 {
		t.Fatalf("tool output parts = %d, want 2", len(toolOutput))
	}
	firstOutput, _ := toolOutput[0].(map[string]any)
	if got, want := firstOutput["type"], "input_text"; got != want {
		t.Fatalf("first tool output type = %v, want %v", got, want)
	}
	secondOutput, _ := toolOutput[1].(map[string]any)
	if got, want := secondOutput["type"], "input_image"; got != want {
		t.Fatalf("second tool output type = %v, want %v", got, want)
	}
}

func TestResponsesReplayGeneratesDistinctMessageIDsAroundReasoning(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-replay-split-message-id-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("continue"),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.Text("visible first"),
					sigma.Thinking("private reasoning", ""),
					sigma.Text("visible second"),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var messageIDs []string
	var contentIDs []string
	for _, item := range payload.Input {
		if item["type"] != "message" || item["role"] != "assistant" {
			continue
		}
		id, _ := item["id"].(string)
		messageIDs = append(messageIDs, id)
		content, _ := item["content"].([]any)
		if len(content) != 1 {
			t.Fatalf("message content = %#v, want one text part", content)
		}
		part, _ := content[0].(map[string]any)
		contentID, _ := part["id"].(string)
		contentIDs = append(contentIDs, contentID)
	}
	if got, want := len(messageIDs), 2; got != want {
		t.Fatalf("assistant message items = %d, want %d: %#v", got, want, payload.Input)
	}
	if messageIDs[0] == messageIDs[1] {
		t.Fatalf("assistant message IDs are not distinct: %v", messageIDs)
	}
	for _, id := range messageIDs {
		assertResponsesID(t, id, "msg_")
	}
	for _, id := range contentIDs {
		assertResponsesID(t, id, "text_")
	}
}

func assertResponsesID(t *testing.T, id string, prefix string) {
	t.Helper()
	if !strings.HasPrefix(id, prefix) {
		t.Fatalf("id %q does not have prefix %q", id, prefix)
	}
	if len(id) > 64 {
		t.Fatalf("id %q length = %d, want <= 64", id, len(id))
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			t.Fatalf("id %q contains invalid rune %q", id, r)
		}
	}
}

func TestResponsesCompleteSendsProviderDefinedToolsPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-provider-tools-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

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
			openai.Tools.WebSearch(
				openai.WithSearchContextSize("low"),
				openai.WithSearchFilters(openai.WebSearchFilters{AllowedDomains: []string{"example.com"}}),
			),
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/openai/responses/provider_defined_tools_payload.json")
}

func TestResponsesStreamingMapsTextReasoningUsageAndMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`event: response.created
data: {"type":"response.created","response":{"id":"resp_stream","model":"gpt-test-2026","status":"in_progress"}}

event: response.output_item.added
data: {"type":"response.output_item.added","response_id":"resp_stream","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[]}}

event: response.reasoning_summary_text.delta
data: {"type":"response.reasoning_summary_text.delta","response_id":"resp_stream","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"Checked "}

event: response.output_item.added
data: {"type":"response.output_item.added","response_id":"resp_stream","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","response_id":"resp_stream","item_id":"msg_1","output_index":1,"content_index":0,"delta":"Hello"}

event: response.output_text.delta
data: {"type":"response.output_text.delta","response_id":"resp_stream","item_id":"msg_1","output_index":1,"content_index":0,"delta":" world"}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_stream","model":"gpt-test-2026","status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Checked constraints.","signature":"think_sig"}],"encrypted_content":"enc_think"},{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","id":"text_1","text":"Hello world","signature":"text_sig"}]}],"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":3},"output_tokens":8,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":18}}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-stream-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

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
	if got, want := final.Content[0].ThinkingText, "Checked constraints."; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Signature, "think_sig"; got != want {
		t.Fatalf("thinking signature = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderSignature, "enc_think"; got != want {
		t.Fatalf("thinking provider signature = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "Hello world"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Signature, "text_sig"; got != want {
		t.Fatalf("text signature = %q, want %q", got, want)
	}
	if got, want := final.ProviderMetadata["id"], "resp_stream"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}
	if got, want := final.ProviderMetadata["model"], "gpt-test-2026"; got != want {
		t.Fatalf("provider model = %v, want %v", got, want)
	}
	if final.Usage == nil {
		t.Fatal("final usage was nil")
	}
	if got, want := final.Usage.CacheReadInputTokens, 3; got != want {
		t.Fatalf("cache read tokens = %d, want %d", got, want)
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
	if got, want := final.Usage.Raw["input_tokens"], float64(10); got != want {
		t.Fatalf("raw input tokens = %v, want %v", got, want)
	}
	if events[len(events)-1].Usage == nil || events[len(events)-1].Usage.Raw["input_tokens"] != float64(10) {
		t.Fatalf("terminal usage = %#v, want raw input tokens", events[len(events)-1].Usage)
	}
}

func TestResponsesStreamingParsesReasoningTextAndRefusal(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.output_item.added","response_id":"resp_refusal","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}

data: {"type":"response.reasoning_text.delta","response_id":"resp_refusal","item_id":"rs_1","output_index":0,"delta":"Check "}

data: {"type":"response.reasoning_text.delta","response_id":"resp_refusal","item_id":"rs_1","output_index":0,"delta":"policy."}

data: {"type":"response.output_item.added","response_id":"resp_refusal","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}

data: {"type":"response.content_part.added","response_id":"resp_refusal","item_id":"msg_1","output_index":1,"content_index":0,"part":{"type":"refusal","id":"refusal_1","refusal":""}}

data: {"type":"response.refusal.delta","response_id":"resp_refusal","item_id":"msg_1","output_index":1,"content_index":0,"delta":"I cannot"}

data: {"type":"response.refusal.delta","response_id":"resp_refusal","item_id":"msg_1","output_index":1,"content_index":0,"delta":" help."}

data: {"type":"response.completed","response":{"id":"resp_refusal","status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Check policy."}]},{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"refusal","id":"refusal_1","refusal":"I cannot help."}]}]}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-refusal-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].ThinkingText, "Check policy."; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "I cannot help."; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := final.Content[1].ProviderMetadata["content_id"], "refusal_1"; got != want {
		t.Fatalf("refusal content id = %v, want %q", got, want)
	}
}

func TestResponsesToolCallStreamingProducesFinalArguments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.output_item.added","response_id":"resp_tool","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":""}}

data: {"type":"response.function_call_arguments.delta","response_id":"resp_tool","item_id":"fc_1","output_index":0,"delta":"{\"city\""}

data: {"type":"response.function_call_arguments.delta","response_id":"resp_tool","item_id":"fc_1","output_index":0,"delta":":\"Melbourne\"}"}

data: {"type":"response.function_call_arguments.done","response_id":"resp_tool","item_id":"fc_1","output_index":0,"arguments":"{\"city\":\"Melbourne\"}"}

data: {"type":"response.output_item.done","response_id":"resp_tool","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{\"city\":\"Melbourne\"}"}}

data: {"type":"response.completed","response":{"id":"resp_tool","status":"completed","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{\"city\":\"Melbourne\"}"}]}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-tool-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

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
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonToolCalls; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ToolCallID, "call_1"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderMetadata["id"], "fc_1"; got != want {
		t.Fatalf("tool item id = %v, want %v", got, want)
	}
	args := final.Content[0].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("tool city = %v, want %v", got, want)
	}
}

func TestResponsesAppliesServiceTierCostMultiplier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		modelID     sigma.ModelID
		serviceTier string
		multiplier  float64
	}{
		{name: "flex", modelID: "gpt-test", serviceTier: "flex", multiplier: 0.5},
		{name: "priority", modelID: "gpt-test", serviceTier: "priority", multiplier: 2},
		{name: "gpt-5.5 priority", modelID: "gpt-5.5", serviceTier: "priority", multiplier: 2.5},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeResponsesSSE(t, w, responsesUsageEvent(tt.serviceTier))
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-cost-test-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := responsesTestModel(providerID)
			model.ID = tt.modelID
			client := responsesTestClient(t, providerID, model, server.URL)

			final, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithOpenAIOptions(sigma.OpenAIOptions{ServiceTier: tt.serviceTier}),
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}
			if final.Cost == nil {
				t.Fatal("final cost was nil")
			}
			if got, want := final.Cost.InputCost, 1*tt.multiplier; got != want {
				t.Fatalf("input cost = %v, want %v", got, want)
			}
			if got, want := final.Cost.OutputCost, 2*tt.multiplier; got != want {
				t.Fatalf("output cost = %v, want %v", got, want)
			}
			if got, want := final.Cost.TotalCost, 3*tt.multiplier; got != want {
				t.Fatalf("total cost = %v, want %v", got, want)
			}
		})
	}
}

func TestResponsesReplayOmitsToolItemIDForSameProviderDifferentModel(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-replay-handoff-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{
			sigma.UserText("start"),
			{
				Role:     sigma.RoleAssistant,
				Provider: providerID,
				API:      sigma.APIOpenAIResponses,
				Model:    "different-responses-model",
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_prev|fc_prev", "lookup", map[string]any{"query": "weather"}),
				},
			},
		}},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	input, ok := payload["input"].([]any)
	if !ok {
		t.Fatalf("input type = %T, want []any", payload["input"])
	}
	functionCall, ok := input[1].(map[string]any)
	if !ok {
		t.Fatalf("function call type = %T, want map", input[1])
	}
	if got, want := functionCall["type"], "function_call"; got != want {
		t.Fatalf("type = %v, want %q", got, want)
	}
	if got, want := functionCall["call_id"], "call_prev"; got != want {
		t.Fatalf("call_id = %v, want %q", got, want)
	}
	if got, want := functionCall["name"], "lookup"; got != want {
		t.Fatalf("name = %v, want %q", got, want)
	}
	if _, ok := functionCall["id"]; ok {
		t.Fatalf("function_call.id was sent for same-provider different-model replay: %#v", functionCall)
	}
}

func TestResponsesStreamingParsesImageGenerationOutput(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.created","response":{"id":"resp_image","status":"in_progress"}}

data: {"type":"response.output_item.added","response_id":"resp_image","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","status":"in_progress"}}

data: {"type":"response.image_generation_call.partial_image","response_id":"resp_image","item_id":"ig_1","output_index":0,"partial_image_b64":"cGFydGlhbA=="}

data: {"type":"response.output_item.done","response_id":"resp_image","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","status":"completed","result":"ZmluYWw="}}

data: {"type":"response.completed","response":{"id":"resp_image","status":"completed","output":[{"type":"image_generation_call","id":"ig_1","status":"completed","result":"ZmluYWw="}]}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-image-generation-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("draw")},
		Tools:    []sigma.Tool{openai.Tools.ImageGeneration(openai.WithPartialImages(1))},
	})
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
		sigma.EventKindImageStart,
		sigma.EventKindImageDelta,
		sigma.EventKindImageEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if len(final.Content) != 1 || final.Content[0].Type != sigma.ContentBlockImage {
		t.Fatalf("content = %#v, want one image block", final.Content)
	}
	if got, want := final.Content[0].Data, "ZmluYWw="; got != want {
		t.Fatalf("image data = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderMetadata["id"], "ig_1"; got != want {
		t.Fatalf("image id metadata = %v, want %q", got, want)
	}
}

func TestResponsesProviderErrorIsTypedAndRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key sk-secret123"}}`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-error-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

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
	if got, want := final.Diagnostics[0].API, sigma.APIOpenAIResponses; got != want {
		t.Fatalf("diagnostic API = %q, want %q", got, want)
	}
	if got, want := sigma.ClassifyError(err).Class, sigma.ErrorClassAuth; got != want {
		t.Fatalf("class = %q, want %q", got, want)
	}
	if errorsContains(err, "sk-secret123") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestResponsesStreamErrorEventIsTypedProviderError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w, `event: error
data: {"type":"error","error":{"code":"rate_limit_exceeded","message":"rate limited"}}

`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-stream-error-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

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
	classification := sigma.ClassifyError(err)
	if got, want := classification.Class, sigma.ErrorClassRateLimited; got != want {
		t.Fatalf("class = %q, want %q", got, want)
	}
	if got, want := classification.ProviderCode, "rate_limit_exceeded"; got != want {
		t.Fatalf("provider code = %q, want %q", got, want)
	}
}

func TestResponsesCancellationAbortsStreamingRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"id":"resp_cancel","status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_text.delta","response_id":"resp_cancel","output_index":0,"item_id":"msg_partial","delta":"partial"}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-cancel-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	stream := client.Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	for {
		event := receiveEvent(t, stream)
		if event.Kind == sigma.EventKindTextDelta {
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
	if got, want := final.Content[0].Text, "partial"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
}

func TestResponsesRetriesRetryableStatus(t *testing.T) {
	t.Parallel()

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"retry later"}}`)
			return
		}
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-retry-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

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

func TestResponsesDefaultsStoreAndReasoningReplayInclude(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		opts          []sigma.Option
		wantStore     any
		wantInclude   any
		wantSummary   any
		wantNoInclude bool
	}{
		{
			name:        "reasoning defaults store include and summary",
			opts:        []sigma.Option{sigma.WithReasoningLevel(sigma.ThinkingLevelHigh)},
			wantStore:   false,
			wantInclude: []any{"reasoning.encrypted_content"},
			wantSummary: "auto",
		},
		{
			name: "explicit include and store are preserved",
			opts: []sigma.Option{
				sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
				sigma.WithProviderOptions(sigma.ProviderID("responses-defaults-test"), map[string]any{
					"include": []any{"file_search_call.results"},
					"store":   true,
				}),
			},
			wantStore:   true,
			wantInclude: []any{"file_search_call.results"},
			wantSummary: "auto",
		},
		{
			name:          "no reasoning keeps include absent",
			wantStore:     false,
			wantNoInclude: true,
		},
		{
			name: "explicit reasoning summary is preserved",
			opts: []sigma.Option{
				sigma.WithOpenAIOptions(sigma.OpenAIOptions{
					ReasoningEffort:  sigma.ThinkingLevelHigh,
					ReasoningSummary: "detailed",
				}),
			},
			wantStore:   false,
			wantInclude: []any{"reasoning.encrypted_content"},
			wantSummary: "detailed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-defaults-test")
			model := responsesTestModel(providerID)
			client := responsesTestClient(t, providerID, model, server.URL)

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				tt.opts...,
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			var payload map[string]any
			if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
				t.Fatalf("Unmarshal request body returned error: %v", err)
			}
			if got := payload["store"]; got != tt.wantStore {
				t.Fatalf("store = %#v, want %#v", got, tt.wantStore)
			}
			if tt.wantNoInclude {
				if _, ok := payload["include"]; ok {
					t.Fatalf("include = %#v, want absent", payload["include"])
				}
				return
			}
			if !reflect.DeepEqual(payload["include"], tt.wantInclude) {
				t.Fatalf("include = %#v, want %#v", payload["include"], tt.wantInclude)
			}
			reasoning, ok := payload["reasoning"].(map[string]any)
			if !ok {
				t.Fatalf("reasoning = %#v, want object", payload["reasoning"])
			}
			if got := reasoning["summary"]; got != tt.wantSummary {
				t.Fatalf("reasoning.summary = %#v, want %#v", got, tt.wantSummary)
			}
		})
	}
}

func responsesTestClient(t *testing.T, providerID sigma.ProviderID, model sigma.Model, baseURL string, opts ...openai.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]openai.ProviderOption{openai.WithBaseURL(baseURL)}, opts...)
	if err := registry.RegisterTextProvider(providerID, openai.NewResponsesProvider(providerOpts...)); err != nil {
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

func responsesTestModel(providerID sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:       "gpt-test",
		Provider: providerID,
		API:      sigma.APIOpenAIResponses,
		SupportedInputs: []sigma.ContentBlockType{
			sigma.ContentBlockText,
			sigma.ContentBlockImage,
		},
		SupportsTools:                true,
		SupportsThinking:             true,
		ThinkingLevelMap:             map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "high"},
		InputCostPerMillion:          1,
		OutputCostPerMillion:         2,
		CacheReadInputCostPerMillion: 0.5,
	}
}

func assertResponsesFunctionToolChoice(t *testing.T, body []byte) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	choice, ok := payload["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice type = %T, want map", payload["tool_choice"])
	}
	if got, want := choice["type"], "function"; got != want {
		t.Fatalf("tool_choice.type = %v, want %q", got, want)
	}
	if got, want := choice["name"], "read_file"; got != want {
		t.Fatalf("tool_choice.name = %v, want %q", got, want)
	}
	if _, ok := choice["function"]; ok {
		t.Fatalf("tool_choice.function was not normalized: %#v", choice)
	}
}

func responsesRichRequest() sigma.Request {
	thinking := sigma.Thinking("Internal summary.", "think_prev_sig")
	thinking.ProviderSignature = "enc_prev"
	thinking.ProviderMetadata = map[string]any{"id": "rs_prev"}
	text := sigma.Text("Earlier answer.")
	text.Signature = "text_prev_sig"
	text.ProviderMetadata = map[string]any{"id": "msg_prev", "content_id": "text_prev"}
	toolCall := sigma.ToolCallBlock("call_prev", "lookup", map[string]any{"query": "weather"})
	toolCall.ProviderMetadata = map[string]any{"id": "fc_prev"}

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
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{text, thinking, toolCall},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call_prev",
				Content: []sigma.ContentBlock{
					sigma.Text("Sunny"),
					sigma.ImageBase64("image/png", "aGk="),
				},
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
			ProviderMetadata: map[string]any{"strict": true},
		}},
	}
}

func writeResponsesSSE(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, body)
}

func errorsContains(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}

const responsesCompletedEvent = `data: {"type":"response.completed","response":{"id":"resp_complete","model":"gpt-test","status":"completed","output":[{"type":"message","id":"msg_complete","role":"assistant","content":[{"type":"output_text","id":"text_complete","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}
`

func responsesUsageEvent(serviceTier string) string {
	return `data: {"type":"response.completed","response":{"id":"resp_usage","model":"gpt-test","status":"completed","service_tier":"` + serviceTier + `","output":[{"type":"message","id":"msg_usage","role":"assistant","content":[{"type":"output_text","id":"text_usage","text":"ok"}]}],"usage":{"input_tokens":1000000,"output_tokens":1000000,"total_tokens":2000000,"input_tokens_details":{"cached_tokens":0}}}}
`
}
