// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package radius_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/radius"
)

type radiusRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    string
}

func TestRegisterRefreshesDynamicModelsAndRetainsPriorModelsOnFailure(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	requests := make(chan radiusRequest, 3)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- captureRadiusRequest(t, r)
		switch attempts.Add(1) {
		case 2:
			http.Error(w, `{"error":{"code":"unavailable","message":"try again"}}`, http.StatusServiceUnavailable)
			return
		case 3:
			_, _ = io.WriteString(w, `{"baseUrl":"not a URL","models":[]}`)
			return
		}
		_, _ = io.WriteString(w, `{
			"baseUrl": "`+server.URL+`",
			"models": [
				{"id":"radius-test","name":"Radius Test","reasoning":true,"thinkingLevelMap":{"low":"minimal","high":"thorough"},"input":["text","image"],"cost":{"input":1.5,"output":2.5,"cacheRead":0.5,"cacheWrite":3},"contextWindow":128000,"maxTokens":8192},
				{"id":"bad","name":"Bad","input":["image"],"contextWindow":1,"maxTokens":1},
				{"id":"","name":"missing id","input":["text"],"contextWindow":1,"maxTokens":1}
			]
		}`)
	}))
	t.Cleanup(server.Close)

	registry := sigma.NewRegistry()
	if err := radius.Register(registry, radius.WithGatewayURL(server.URL), radius.WithCatalogAPIKey("catalog-key")); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	if got := client.Models(); len(got) != 0 {
		t.Fatalf("models before refresh = %#v, want none", got)
	}
	if err := client.RefreshTextModels(context.Background(), sigma.ProviderRadius); err != nil {
		t.Fatalf("RefreshTextModels returned error: %v", err)
	}

	request := receiveRadiusRequest(t, requests)
	if got, want := request.Method, http.MethodGet; got != want {
		t.Fatalf("catalog method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/v1/config"; got != want {
		t.Fatalf("catalog path = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("Authorization"), "Bearer catalog-key"; got != want {
		t.Fatalf("catalog authorization = %q, want %q", got, want)
	}

	model, ok := client.GetModel(sigma.ProviderRadius, "radius-test")
	if !ok {
		t.Fatal("refreshed model was not registered")
	}
	if got, want := model.API, sigma.APIRadiusMessages; got != want {
		t.Fatalf("API = %q, want %q", got, want)
	}
	if !model.SupportsInput(sigma.ContentBlockImage) || !model.SupportsTools || !model.SupportsThinking {
		t.Fatalf("model capabilities = %#v, want image, tools, and thinking", model)
	}
	if got, want := model.ProviderThinkingLevel(sigma.ThinkingLevelHigh); !want || got != "thorough" {
		t.Fatalf("high thinking level = (%q, %t), want (thorough, true)", got, want)
	}
	if got, want := model.CacheWriteInputCostPerMillion, 3.0; got != want {
		t.Fatalf("cache write cost = %v, want %v", got, want)
	}

	if err := client.RefreshTextModels(context.Background(), sigma.ProviderRadius); err == nil {
		t.Fatal("RefreshTextModels returned nil after catalog failure")
	}
	if _, ok := client.GetModel(sigma.ProviderRadius, "radius-test"); !ok {
		t.Fatal("failed refresh removed the prior dynamic model")
	}
	if err := client.RefreshTextModels(context.Background(), sigma.ProviderRadius); err == nil {
		t.Fatal("RefreshTextModels returned nil for an invalid gateway configuration")
	}
	if _, ok := client.GetModel(sigma.ProviderRadius, "radius-test"); !ok {
		t.Fatal("invalid configuration removed the prior dynamic model")
	}
}

func TestRadiusStreamSendsGatewayPayloadAndMapsEvents(t *testing.T) {
	t.Parallel()

	requests := make(chan radiusRequest, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/config":
			_, _ = io.WriteString(w, radiusCatalog(server.URL))
		case "/messages":
			requests <- captureRadiusRequest(t, r)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"type":"start"}

data: {"type":"thinking_start","contentIndex":0}

data: {"type":"thinking_delta","contentIndex":0,"delta":"checking "}

data: {"type":"thinking_end","contentIndex":0,"content":"checking constraints","contentSignature":"think-sig"}

data: {"type":"text_start","contentIndex":1}

data: {"type":"text_delta","contentIndex":1,"delta":"The answer"}

data: {"type":"text_end","contentIndex":1,"content":"The answer is 42.","contentSignature":"text-sig"}

data: {"type":"toolcall_start","contentIndex":2,"id":"call_1","toolName":"weather"}

data: {"type":"toolcall_delta","contentIndex":2,"delta":"{\"city\":\"Mel"}

data: {"type":"toolcall_delta","contentIndex":2,"delta":"bourne\"}"}

data: {"type":"toolcall_end","contentIndex":2}

data: {"type":"done","reason":"toolUse","usage":{"input":12,"output":8,"cacheRead":3,"cacheWrite":2,"totalTokens":20},"responseId":"radius_response_1"}

`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client, model := refreshedRadiusClient(t, server.URL, radius.WithHeader("X-Provider", "provider"))
	temperature := 0.25
	maxTokens := 300
	final, err := client.Complete(context.Background(), model, sigma.Request{
		SystemPrompt: "Be concise.",
		Messages: []sigma.Message{
			sigma.UserContent(sigma.Text("What is the answer?"), sigma.ImageBase64("image/png", "aW1hZ2U=")),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.Thinking("Check the question.", "previous-think-sig"),
					sigma.ToolCallBlock("old_call", "lookup", map[string]any{"query": "answer"}),
				},
				Provider: sigma.ProviderRadius,
				API:      sigma.APIRadiusMessages,
				Model:    model.ID,
			},
			sigma.ToolResult("old_call", "42"),
		},
		Tools: []sigma.Tool{{
			Name:        "weather",
			Description: "Gets the weather.",
			InputSchema: sigma.Schema{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}},
		}},
	},
		sigma.WithAPIKey("request-key"),
		sigma.WithTemperature(temperature),
		sigma.WithMaxTokens(maxTokens),
		sigma.WithCacheRetention(sigma.CacheRetentionLong),
		sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
		sigma.WithSessionID("session-1"),
		sigma.WithHeader("X-Provider", "caller"),
		sigma.WithSuppressedHeader("X-Client"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRadiusRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/messages"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("Authorization"), "Bearer request-key"; got != want {
		t.Fatalf("authorization = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("X-Provider"), "caller"; got != want {
		t.Fatalf("caller header = %q, want %q", got, want)
	}
	if got := request.Headers.Get("X-Client"); got != "" {
		t.Fatalf("suppressed header = %q, want empty", got)
	}

	payload := decodeRadiusPayload(t, request.Body)
	if got, want := payload["model"], string(model.ID); got != want {
		t.Fatalf("payload model = %v, want %q", got, want)
	}
	contextPayload := payload["context"].(map[string]any)
	if got, want := contextPayload["systemPrompt"], "Be concise."; got != want {
		t.Fatalf("system prompt = %v, want %q", got, want)
	}
	messages := contextPayload["messages"].([]any)
	if got, want := len(messages), 3; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	userContent := messages[0].(map[string]any)["content"].([]any)
	if got, want := userContent[1].(map[string]any)["type"], "image"; got != want {
		t.Fatalf("image type = %v, want %q", got, want)
	}
	assistantContent := messages[1].(map[string]any)["content"].([]any)
	if got, want := assistantContent[0].(map[string]any)["thinkingSignature"], "previous-think-sig"; got != want {
		t.Fatalf("thinking signature = %v, want %q", got, want)
	}
	if got, want := messages[2].(map[string]any)["role"], "toolResult"; got != want {
		t.Fatalf("tool result role = %v, want %q", got, want)
	}
	if got, want := len(contextPayload["tools"].([]any)), 1; got != want {
		t.Fatalf("tool count = %d, want %d", got, want)
	}
	options := payload["options"].(map[string]any)
	if got, want := options["temperature"], temperature; got != want {
		t.Fatalf("temperature = %v, want %v", got, want)
	}
	if got, want := options["maxTokens"], float64(maxTokens); got != want {
		t.Fatalf("max tokens = %v, want %v", got, want)
	}
	if got, want := options["reasoning"], "thorough"; got != want {
		t.Fatalf("reasoning = %v, want %q", got, want)
	}
	if got, want := options["cacheRetention"], string(sigma.CacheRetentionLong); got != want {
		t.Fatalf("cache retention = %v, want %q", got, want)
	}
	if got, want := options["sessionId"], "session-1"; got != want {
		t.Fatalf("session id = %v, want %q", got, want)
	}

	if got, want := final.StopReason, sigma.StopReasonToolCalls; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.ResponseID(), "radius_response_1"; got != want {
		t.Fatalf("response ID = %q, want %q", got, want)
	}
	if got, want := len(final.Content), 3; got != want {
		t.Fatalf("content count = %d, want %d", got, want)
	}
	if got, want := final.Content[0].Signature, "think-sig"; got != want {
		t.Fatalf("thinking signature = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "The answer is 42."; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	arguments, ok := final.Content[2].ToolArguments.(map[string]any)
	if !ok || arguments["city"] != "Melbourne" {
		t.Fatalf("tool arguments = %#v, want decoded city", final.Content[2].ToolArguments)
	}
	if final.Usage == nil || final.Usage.InputTokens != 12 || final.Usage.CacheReadInputTokens != 3 || final.Usage.TotalTokens != 20 {
		t.Fatalf("usage = %#v, want mapped gateway usage", final.Usage)
	}
}

func TestRadiusRejectsUnsupportedContentAndProviderToolsBeforeDispatch(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(server.Close)
	client, model := registeredRadiusClient(t, server.URL)

	for _, request := range []sigma.Request{
		{Messages: []sigma.Message{sigma.UserContent(sigma.DocumentBase64("application/pdf", "note.pdf", "cGRm"))}},
		{Messages: []sigma.Message{sigma.UserText("hi")}, Tools: []sigma.Tool{{Name: "web", ProviderDefinedType: "web_search"}}},
	} {
		_, err := client.Complete(context.Background(), model, request)
		if err == nil {
			t.Fatal("Complete returned nil error")
		}
		var sigmaErr *sigma.Error
		if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorUnsupported {
			t.Fatalf("error = %v, want ErrorUnsupported", err)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("network requests = %d, want 0", got)
	}
}

func TestRadiusStreamFailuresAndCancellation(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name          string
		body          string
		status        int
		providerError bool
	}{
		{name: "non-2xx", status: http.StatusTooManyRequests, body: `{"error":{"code":"rate_limited","message":"slow down"}}`, providerError: true},
		{name: "malformed SSE", status: http.StatusOK, body: "data: {bad json}\n\n"},
		{name: "missing terminal", status: http.StatusOK, body: "data: {\"type\":\"text_start\",\"contentIndex\":0}\n\ndata: {\"type\":\"text_delta\",\"contentIndex\":0,\"delta\":\"partial\"}\n\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.status != http.StatusOK {
					w.Header().Set("X-Request-ID", "radius-request")
					w.WriteHeader(tt.status)
				}
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(server.Close)
			client, model := registeredRadiusClient(t, server.URL)
			_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
			if err == nil {
				t.Fatal("Complete returned nil error")
			}
			var providerErr *sigma.ProviderError
			if got := errors.As(err, &providerErr); got != tt.providerError {
				t.Fatalf("ProviderError match = %t, want %t (error %v)", got, tt.providerError, err)
			}
			if providerErr != nil && providerErr.RequestID != "radius-request" {
				t.Fatalf("request ID = %q, want radius-request", providerErr.RequestID)
			}
		})
	}

	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"start\"}\n\ndata: {\"type\":\"text_start\",\"contentIndex\":0}\n\ndata: {\"type\":\"text_delta\",\"contentIndex\":0,\"delta\":\"partial\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	t.Cleanup(server.Close)
	client, model := registeredRadiusClient(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	stream := client.Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	for event := range stream.Events() {
		if event.Kind == sigma.EventKindTextDelta {
			break
		}
	}
	cancel()
	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error after cancellation")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorAborted {
		t.Fatalf("cancellation error = %v, want ErrorAborted", err)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Text, "partial"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("gateway request context was not cancelled")
	}
}

func TestRadiusUsesEnvironmentAPIKey(t *testing.T) {
	t.Setenv("RADIUS_API_KEY", "environment-key")

	requests := make(chan radiusRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- captureRadiusRequest(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"done\",\"reason\":\"stop\"}\n\n")
	}))
	t.Cleanup(server.Close)
	registry := sigma.NewRegistry()
	if err := radius.Register(registry, radius.WithGatewayURL(server.URL)); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	model := radiusTestModel()
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := receiveRadiusRequest(t, requests).Headers.Get("Authorization"), "Bearer environment-key"; got != want {
		t.Fatalf("environment authorization = %q, want %q", got, want)
	}
}

func refreshedRadiusClient(t *testing.T, gatewayURL string, opts ...radius.ProviderOption) (*sigma.Client, sigma.Model) {
	t.Helper()
	registry := sigma.NewRegistry()
	providerOpts := append([]radius.ProviderOption{radius.WithGatewayURL(gatewayURL), radius.WithCatalogAPIKey("catalog-key")}, opts...)
	if err := radius.Register(registry, providerOpts...); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-key"}, nil
		})),
		sigma.WithDefaultHeader("X-Client", "client"),
	)
	if err := client.RefreshTextModels(context.Background(), sigma.ProviderRadius); err != nil {
		t.Fatalf("RefreshTextModels returned error: %v", err)
	}
	model, ok := client.GetModel(sigma.ProviderRadius, "radius-test")
	if !ok {
		t.Fatal("refreshed Radius model was not registered")
	}
	return client, model
}

func registeredRadiusClient(t *testing.T, gatewayURL string) (*sigma.Client, sigma.Model) {
	t.Helper()
	registry := sigma.NewRegistry()
	if err := radius.Register(registry, radius.WithGatewayURL(gatewayURL)); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	model := radiusTestModel()
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	return sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-key"}, nil
		})),
	), model
}

func radiusTestModel() sigma.Model {
	return sigma.Model{
		ID:                   "radius-test",
		Provider:             sigma.ProviderRadius,
		API:                  sigma.APIRadiusMessages,
		Name:                 "Radius Test",
		ContextWindow:        128000,
		MaxOutputTokens:      8192,
		SupportedInputs:      []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:        true,
		SupportsThinking:     true,
		ThinkingLevels:       []sigma.ThinkingLevel{sigma.ThinkingLevelHigh},
		ThinkingLevelMap:     map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "thorough"},
		DefaultTransport:     sigma.TransportSSE,
		InputCostPerMillion:  1,
		OutputCostPerMillion: 2,
	}
}

func radiusCatalog(baseURL string) string {
	return `{"baseUrl":` + quoteRadius(baseURL) + `,"models":[{"id":"radius-test","name":"Radius Test","reasoning":true,"thinkingLevelMap":{"high":"thorough"},"input":["text","image"],"cost":{"input":1,"output":2,"cacheRead":0.5,"cacheWrite":3},"contextWindow":128000,"maxTokens":8192}]}`
}

func quoteRadius(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func captureRadiusRequest(t *testing.T, request *http.Request) radiusRequest {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("ReadAll request body returned error: %v", err)
	}
	return radiusRequest{Method: request.Method, Path: request.URL.Path, Headers: request.Header.Clone(), Body: string(body)}
}

func receiveRadiusRequest(t *testing.T, requests <-chan radiusRequest) radiusRequest {
	t.Helper()
	select {
	case request := <-requests:
		return request
	default:
		t.Fatal("server did not receive request")
		return radiusRequest{}
	}
}

func decodeRadiusPayload(t *testing.T, body string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode request payload: %v", err)
	}
	return normalizeRadiusNumbers(payload).(map[string]any)
}

func normalizeRadiusNumbers(value any) any {
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			value[key] = normalizeRadiusNumbers(item)
		}
		return value
	case []any:
		for index, item := range value {
			value[index] = normalizeRadiusNumbers(item)
		}
		return value
	case json.Number:
		float, _ := value.Float64()
		return float
	default:
		return value
	}
}
