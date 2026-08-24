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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

func TestResponsesDeferredLifecycle(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/responses":
			_, _ = w.Write([]byte(`{"id":"resp_background","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/responses/resp_background":
			_, _ = w.Write([]byte(`{
				"id":"resp_background",
				"model":"gpt-test-routed",
				"status":"completed",
				"service_tier":"priority",
				"output":[
					{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Checked constraints."}],"encrypted_content":"enc_1"},
					{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","id":"txt_1","text":"Run the tests."}]},
					{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"parse","input":"go test"}
				],
				"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":3},"output_tokens":8,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":18}
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/responses/resp_background/cancel":
			_, _ = w.Write([]byte(`{"id":"resp_background","status":"cancelled"}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	model := responsesTestModel(sigma.ProviderOpenAI)
	model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{SupportsGrammarTools: true}
	client := responsesTestClient(t, sigma.ProviderOpenAI, model, server.URL)
	request := sigma.Request{
		Messages: []sigma.Message{sigma.UserText("run tests")},
		Tools:    []sigma.Tool{responsesGrammarTool("parse", sigma.OpenAIGrammarRegex, ".+")},
	}
	var payloadDebugCount atomic.Int32
	var responseDebugCount atomic.Int32
	options := []sigma.Option{
		sigma.WithHeader("X-Deferred", "enabled"),
		sigma.WithProviderOption(sigma.ProviderOpenAI, "store", true),
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{
			EnableGrammarTools: boolPointer(true),
			ServiceTier:        "priority",
		}),
		sigma.WithTextPayloadDebugHook(func(context.Context, sigma.TextPayloadDebug) error {
			payloadDebugCount.Add(1)
			return nil
		}),
		sigma.WithTextResponseDebugHook(func(context.Context, sigma.TextResponseDebug) error {
			responseDebugCount.Add(1)
			return nil
		}),
	}

	submitted, err := client.SubmitDeferred(context.Background(), model, request, options...)
	if err != nil {
		t.Fatalf("SubmitDeferred returned error: %v", err)
	}
	if submitted.Status != sigma.DeferredResponseQueued || submitted.Message != nil {
		t.Fatalf("submitted response = %#v, want queued without message", submitted)
	}
	if got, want := submitted.Handle.ID, "resp_background"; got != want {
		t.Fatalf("handle id = %q, want %q", got, want)
	}

	handleJSON, err := json.Marshal(submitted.Handle)
	if err != nil {
		t.Fatalf("Marshal handle returned error: %v", err)
	}
	var restored sigma.DeferredResponseHandle
	if err := json.Unmarshal(handleJSON, &restored); err != nil {
		t.Fatalf("Unmarshal handle returned error: %v", err)
	}
	fetched, err := client.FetchDeferred(context.Background(), restored, options...)
	if err != nil {
		t.Fatalf("FetchDeferred returned error: %v", err)
	}
	if fetched.Status != sigma.DeferredResponseCompleted || fetched.Message == nil {
		t.Fatalf("fetched response = %#v, want completed message", fetched)
	}
	final := fetched.Message
	if got, want := final.StopReason, sigma.StopReasonToolCalls; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if len(final.Content) != 3 {
		t.Fatalf("content count = %d, want 3", len(final.Content))
	}
	if got, want := final.Content[0].ThinkingText, "Checked constraints."; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "Run the tests."; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	arguments, ok := final.Content[2].ToolArguments.(map[string]any)
	if !ok || arguments["command"] != "go test" {
		t.Fatalf("tool arguments = %#v, want command", final.Content[2].ToolArguments)
	}
	if final.Usage == nil || final.Usage.TotalTokens != 18 || final.Usage.CacheReadInputTokens != 3 || final.Usage.ThinkingTokens != 2 {
		t.Fatalf("usage = %#v", final.Usage)
	}
	if final.Cost == nil || final.Cost.TotalCost <= 0 {
		t.Fatalf("cost = %#v, want positive estimate", final.Cost)
	}
	if got, want := final.ProviderMetadata["id"], "resp_background"; got != want {
		t.Fatalf("response id = %#v, want %q", got, want)
	}
	if got, want := final.ProviderMetadata["model"], "gpt-test-routed"; got != want {
		t.Fatalf("response model = %#v, want %q", got, want)
	}

	cancelled, err := client.CancelDeferred(context.Background(), restored, options...)
	if err != nil {
		t.Fatalf("CancelDeferred returned error: %v", err)
	}
	if cancelled.Status != sigma.DeferredResponseCancelled || cancelled.Message != nil {
		t.Fatalf("cancelled response = %#v, want cancelled without message", cancelled)
	}

	submitRequest := receiveRequest(t, requests)
	if submitRequest.Method != http.MethodPost || submitRequest.Path != "/responses" {
		t.Fatalf("submit request = %s %s", submitRequest.Method, submitRequest.Path)
	}
	payload := decodeResponsesPayload(t, submitRequest.Body)
	if payload["background"] != true || payload["stream"] != false || payload["store"] != true {
		t.Fatalf("deferred payload flags = %#v", payload)
	}
	assertHeader(t, submitRequest.Headers, "Accept", "application/json")
	assertHeader(t, submitRequest.Headers, "Authorization", "Bearer resolved-key")
	assertHeader(t, submitRequest.Headers, "X-Client", "client")
	assertHeader(t, submitRequest.Headers, "X-Deferred", "enabled")
	for _, want := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/responses/resp_background"},
		{method: http.MethodPost, path: "/responses/resp_background/cancel"},
	} {
		captured := receiveRequest(t, requests)
		if captured.Method != want.method || captured.Path != want.path {
			t.Fatalf("request = %s %s, want %s %s", captured.Method, captured.Path, want.method, want.path)
		}
		assertHeader(t, captured.Headers, "Accept", "application/json")
		assertHeader(t, captured.Headers, "Authorization", "Bearer resolved-key")
		if len(captured.Body) != 0 {
			t.Fatalf("%s body = %q, want empty", want.path, captured.Body)
		}
	}
	if got, want := payloadDebugCount.Load(), int32(1); got != want {
		t.Fatalf("payload debug count = %d, want %d", got, want)
	}
	if got, want := responseDebugCount.Load(), int32(3); got != want {
		t.Fatalf("response debug count = %d, want %d", got, want)
	}
}

func TestResponsesFetchDeferredMapsLifecycleStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantStatus sigma.DeferredResponseStatus
		wantStop   sigma.StopReason
		wantText   string
		wantErr    bool
	}{
		{name: "queued", body: `{"id":"resp_state","status":"queued"}`, wantStatus: sigma.DeferredResponseQueued},
		{name: "in progress", body: `{"id":"resp_state","status":"in_progress"}`, wantStatus: sigma.DeferredResponseInProgress},
		{name: "cancelled", body: `{"id":"resp_state","status":"cancelled"}`, wantStatus: sigma.DeferredResponseCancelled},
		{
			name:       "known incomplete",
			body:       `{"id":"resp_state","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}]}`,
			wantStatus: sigma.DeferredResponseIncomplete,
			wantStop:   sigma.StopReasonMaxTokens,
			wantText:   "partial",
		},
		{
			name:       "unknown incomplete",
			body:       `{"id":"resp_state","status":"incomplete","incomplete_details":{"reason":"max_time_limit"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}]}`,
			wantStatus: sigma.DeferredResponseIncomplete,
			wantStop:   sigma.StopReasonError,
			wantText:   "partial",
			wantErr:    true,
		},
		{
			name:       "failed",
			body:       `{"id":"resp_state","status":"failed","error":{"code":"generation_failed","message":"failed safely"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}]}`,
			wantStatus: sigma.DeferredResponseFailed,
			wantStop:   sigma.StopReasonError,
			wantText:   "partial",
			wantErr:    true,
		},
		{name: "unknown status", body: `{"id":"resp_state","status":"pausing"}`, wantErr: true},
		{name: "mismatched id", body: `{"id":"resp_other","status":"queued"}`, wantErr: true},
		{name: "malformed body", body: `{`, wantErr: true},
		{name: "http error", statusCode: http.StatusBadRequest, body: `{"error":{"code":"bad_request","message":"bad request"}}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				statusCode := tt.statusCode
				if statusCode == 0 {
					statusCode = http.StatusOK
				}
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			model := responsesTestModel(sigma.ProviderOpenAI)
			client := responsesTestClient(t, sigma.ProviderOpenAI, model, server.URL)
			response, err := client.FetchDeferred(context.Background(), sigma.DeferredResponseHandle{
				Provider: sigma.ProviderOpenAI,
				Model:    model.ID,
				API:      model.API,
				ID:       "resp_state",
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("FetchDeferred error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantStatus != "" && response.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", response.Status, tt.wantStatus)
			}
			if tt.wantStop == "" {
				if response.Message != nil {
					t.Fatalf("message = %#v, want nil", response.Message)
				}
				return
			}
			if response.Message == nil {
				t.Fatal("message is nil")
			}
			if response.Message.StopReason != tt.wantStop {
				t.Fatalf("stop reason = %q, want %q", response.Message.StopReason, tt.wantStop)
			}
			if response.Message.Content[0].Text != tt.wantText {
				t.Fatalf("text = %q, want %q", response.Message.Content[0].Text, tt.wantText)
			}
		})
	}
}

func TestResponsesCancelDeferredReturnsAlreadyCompletedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses/resp_complete/cancel" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"resp_complete","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"already done"}]}]}`))
	}))
	t.Cleanup(server.Close)

	model := responsesTestModel(sigma.ProviderOpenAI)
	client := responsesTestClient(t, sigma.ProviderOpenAI, model, server.URL)
	response, err := client.CancelDeferred(context.Background(), sigma.DeferredResponseHandle{
		Provider: sigma.ProviderOpenAI,
		Model:    model.ID,
		API:      model.API,
		ID:       "resp_complete",
	})
	if err != nil {
		t.Fatalf("CancelDeferred returned error: %v", err)
	}
	if response.Status != sigma.DeferredResponseCompleted || response.Message == nil || response.Message.Content[0].Text != "already done" {
		t.Fatalf("cancel response = %#v", response)
	}
}

func TestResponsesSubmitDeferredRetriesTransientHTTPFailure(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, `{"error":{"message":"retry"}}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"id":"resp_retry","status":"queued"}`))
	}))
	t.Cleanup(server.Close)

	model := responsesTestModel(sigma.ProviderOpenAI)
	client := responsesTestClient(t, sigma.ProviderOpenAI, model, server.URL)
	response, err := client.SubmitDeferred(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("retry")}},
		sigma.WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("SubmitDeferred returned error: %v", err)
	}
	if response.Status != sigma.DeferredResponseQueued || attempts.Load() != 2 {
		t.Fatalf("response = %#v, attempts = %d", response, attempts.Load())
	}
}

func TestResponsesSubmitDeferredClosesResponseBody(t *testing.T) {
	t.Parallel()

	body := &deferredTrackingBody{Reader: strings.NewReader(`{"id":"resp_closed","status":"queued"}`)}
	httpClient := &http.Client{Transport: deferredRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       body,
			Request:    request,
		}, nil
	})}
	model := responsesTestModel(sigma.ProviderOpenAI)
	client := responsesTestClient(t, sigma.ProviderOpenAI, model, "https://example.test")
	_, err := client.SubmitDeferred(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("close")}},
		sigma.WithRequestHTTPClient(httpClient),
	)
	if err != nil {
		t.Fatalf("SubmitDeferred returned error: %v", err)
	}
	if !body.closed.Load() {
		t.Fatal("response body was not closed")
	}
}

func TestResponsesDeferredLifecycleRejectsCompatibleRoutes(t *testing.T) {
	t.Parallel()

	providerID := sigma.ProviderID("openai-compatible-deferred")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, "https://example.test")
	_, err := client.SubmitDeferred(context.Background(), model, sigma.Request{})
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorUnsupported {
		t.Fatalf("SubmitDeferred error = %T %[1]v, want unsupported", err)
	}
	if _, ok := any(openai.NewAzureResponsesProvider()).(sigma.DeferredTextProvider); ok {
		t.Fatal("Azure Responses unexpectedly implements DeferredTextProvider")
	}
	if _, ok := any(openai.NewCodexResponsesProvider()).(sigma.DeferredTextProvider); ok {
		t.Fatal("Codex Responses unexpectedly implements DeferredTextProvider")
	}
}

func boolPointer(value bool) *bool {
	return &value
}

type deferredRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn deferredRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type deferredTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *deferredTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}
