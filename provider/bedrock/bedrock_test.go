// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
)

func TestRegisterReportsConverseStreamAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("bedrock-compatible")
	if err := Register(registry, providerID, WithConverseStreamClient(&fakeConverseClient{}), WithCredentialDetector(fakeCredentialDetector{})); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.RegisterModel(bedrockTestModel(providerID)); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIBedrockConverseStream; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestConversePayloadMapsMessagesToolsImagesThinkingAndCache(t *testing.T) {
	t.Parallel()

	imageData := "aGVsbG8="
	model := bedrockTestModel(sigma.ProviderAmazonBedrock)
	model.Name = "Claude Sonnet 4.5"
	maxTokens := 256
	temperature := 0.2
	req := sigma.Request{
		SystemPrompt: "You are helpful.",
		Messages: []sigma.Message{
			sigma.UserContent(
				sigma.Text("Describe this"),
				sigma.ImageBase64("image/png", imageData),
			),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.Thinking("checking", "sig_123"),
					sigma.Text("I can inspect it."),
					sigma.ToolCallBlock("tool_1", "lookup", map[string]any{"id": "abc"}),
				},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "tool_1",
				Content:    []sigma.ContentBlock{sigma.Text("found")},
			},
		},
		Tools: []sigma.Tool{{
			Name:        "lookup",
			Description: "Look up an item",
			InputSchema: sigma.Schema{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
				"required":   []any{"id"},
			},
		}},
	}
	payload, err := conversePayload(model, req, sigma.Options{
		MaxTokens:            &maxTokens,
		Temperature:          &temperature,
		CacheRetention:       sigma.CacheRetentionEphemeral,
		ThinkingBudgetTokens: intPtr(1024),
		Metadata:             map[string]any{"trace": "abc", "ignored": 12},
	}, Config{
		ModelID: "anthropic.claude-3-5-sonnet-20240620-v1:0",
	})
	if err != nil {
		t.Fatalf("conversePayload returned error: %v", err)
	}

	goldentest.AssertJSON(t, payload, "provider/bedrock/converse/rich_payload.json")
}

func TestBedrockOptionsOverrideProviderOptionsAndMapToolChoice(t *testing.T) {
	t.Parallel()

	topP := 0.7
	payload, err := conversePayload(
		bedrockTestModel(sigma.ProviderAmazonBedrock),
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("hi")},
			Tools: []sigma.Tool{{
				Name:        "lookup",
				Description: "Look up records",
				InputSchema: sigma.Schema{"type": "object"},
			}},
		},
		sigma.Options{
			BedrockOptions: &sigma.BedrockOptions{
				ToolChoice:                   &sigma.BedrockToolChoice{Type: sigma.BedrockToolChoiceTool, Name: "lookup"},
				TopP:                         &topP,
				StopSequences:                []string{"typed-stop"},
				RequestMetadata:              map[string]string{"trace": "typed"},
				AdditionalModelRequestFields: map[string]any{"field": "typed"},
				AdditionalModelResponseFieldPaths: []string{
					"/stop_sequence",
				},
			},
			ProviderOptions: map[sigma.ProviderID]map[string]any{
				sigma.ProviderAmazonBedrock: {
					providerOptionToolChoice:                   "auto",
					providerOptionTopP:                         0.2,
					providerOptionStopSequences:                []string{"provider-stop"},
					providerOptionRequestMetadata:              map[string]any{"trace": "provider"},
					providerOptionAdditionalModelRequestFields: map[string]any{"field": "provider"},
					providerOptionAdditionalResponsePaths:      []string{"/provider"},
				},
			},
		},
		Config{ModelID: "model"},
	)
	if err != nil {
		t.Fatalf("conversePayload returned error: %v", err)
	}
	if payload.ToolChoice == nil || payload.ToolChoice.Type != sigma.BedrockToolChoiceTool || payload.ToolChoice.Name != "lookup" {
		t.Fatalf("tool choice = %+v, want named lookup", payload.ToolChoice)
	}
	if payload.InferenceConfig == nil || payload.InferenceConfig.TopP == nil || *payload.InferenceConfig.TopP != 0.7 {
		t.Fatalf("top_p = %+v, want 0.7", payload.InferenceConfig)
	}
	if got, want := payload.InferenceConfig.StopSequences, []string{"typed-stop"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stop sequences = %v, want %v", got, want)
	}
	if got, want := payload.RequestMetadata["trace"], "typed"; got != want {
		t.Fatalf("request metadata trace = %q, want %q", got, want)
	}
	if got, want := payload.AdditionalModelRequestFields["field"], "typed"; got != want {
		t.Fatalf("additional field = %v, want %v", got, want)
	}
	if got, want := payload.AdditionalModelResponseFieldPaths, []string{"/stop_sequence"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("response field paths = %v, want %v", got, want)
	}

	input, err := awsConverseInput(payload)
	if err != nil {
		t.Fatalf("awsConverseInput returned error: %v", err)
	}
	toolConfig := input["toolConfig"].(map[string]any)
	if got, want := toolConfig["toolChoice"], map[string]any{"tool": map[string]any{"name": "lookup"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tool choice body = %#v, want %#v", got, want)
	}
}

func TestBedrockToolChoiceNoneOmitsToolsBeforeCapabilityCheck(t *testing.T) {
	t.Parallel()

	model := bedrockTestModel(sigma.ProviderAmazonBedrock)
	model.SupportsTools = false
	payload, err := conversePayload(
		model,
		sigma.Request{
			Messages: []sigma.Message{sigma.UserText("hi")},
			Tools:    []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}},
		},
		sigma.Options{BedrockOptions: &sigma.BedrockOptions{
			ToolChoice: &sigma.BedrockToolChoice{Type: sigma.BedrockToolChoiceNone},
		}},
		Config{ModelID: "model"},
	)
	if err != nil {
		t.Fatalf("conversePayload returned error: %v", err)
	}
	if len(payload.Tools) != 0 || payload.ToolChoice != nil {
		t.Fatalf("tools = %v choice = %+v, want omitted", payload.Tools, payload.ToolChoice)
	}
}

func TestBedrockThinkingPayloadVariants(t *testing.T) {
	t.Parallel()

	disabled := false
	tests := []struct {
		name   string
		model  sigma.Model
		opts   sigma.Options
		config Config
		want   map[string]any
	}{
		{
			name: "adaptive claude",
			model: func() sigma.Model {
				model := bedrockTestModel(sigma.ProviderAmazonBedrock)
				model.ID = "global.anthropic.claude-opus-4-8-v1"
				model.Name = "Claude Opus 4.8"
				return model
			}(),
			opts: sigma.Options{ReasoningLevel: sigma.ThinkingLevelXHigh},
			want: map[string]any{
				"thinking":      map[string]any{"type": "adaptive", "display": "summarized"},
				"output_config": map[string]any{"effort": "xhigh"},
			},
		},
		{
			name: "fixed budget claude",
			model: func() sigma.Model {
				model := bedrockTestModel(sigma.ProviderAmazonBedrock)
				model.ID = "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
				model.Name = "Claude Sonnet 4.5"
				return model
			}(),
			opts: sigma.Options{ReasoningLevel: sigma.ThinkingLevelMedium},
			want: map[string]any{
				"thinking":       map[string]any{"type": "enabled", "budget_tokens": 8192, "display": "summarized"},
				"anthropic_beta": []string{"interleaved-thinking-2025-05-14"},
			},
		},
		{
			name: "explicit budget without interleaved beta",
			model: func() sigma.Model {
				model := bedrockTestModel(sigma.ProviderAmazonBedrock)
				model.ID = "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
				model.Name = "Claude Sonnet 4.5"
				return model
			}(),
			opts: sigma.Options{
				ThinkingBudgetTokens: intPtr(4096),
				BedrockOptions:       &sigma.BedrockOptions{InterleavedThinking: &disabled},
			},
			want: map[string]any{
				"thinking": map[string]any{"type": "enabled", "budget_tokens": 4096, "display": "summarized"},
			},
		},
		{
			name: "govcloud omits display",
			model: func() sigma.Model {
				model := bedrockTestModel(sigma.ProviderAmazonBedrock)
				model.ID = "us-gov.anthropic.claude-sonnet-4-5-20250929-v1:0"
				model.Name = "Claude Sonnet 4.5"
				return model
			}(),
			opts:   sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh},
			config: Config{Region: "us-gov-west-1"},
			want: map[string]any{
				"thinking":       map[string]any{"type": "enabled", "budget_tokens": 16384},
				"anthropic_beta": []string{"interleaved-thinking-2025-05-14"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload, err := conversePayload(tt.model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, tt.opts, tt.config)
			if err != nil {
				t.Fatalf("conversePayload returned error: %v", err)
			}
			if !reflect.DeepEqual(payload.AdditionalModelRequestFields, tt.want) {
				t.Fatalf("thinking fields = %#v, want %#v", payload.AdditionalModelRequestFields, tt.want)
			}
		})
	}
}

func TestToolResultsGroupAndPreserveImages(t *testing.T) {
	t.Parallel()

	payload, err := conversePayload(
		bedrockTestModel(sigma.ProviderAmazonBedrock),
		sigma.Request{Messages: []sigma.Message{
			{
				Role:       sigma.RoleTool,
				ToolCallID: "tool_a",
				Content:    []sigma.ContentBlock{sigma.Text("alpha")},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "tool_b",
				Content: []sigma.ContentBlock{
					sigma.Text("beta"),
					sigma.ImageBase64("image/png", "aW1hZ2U="),
				},
			},
		}},
		sigma.Options{},
		Config{ModelID: "model"},
	)
	if err != nil {
		t.Fatalf("conversePayload returned error: %v", err)
	}
	if len(payload.Messages) != 1 {
		t.Fatalf("messages = %d, want grouped single message", len(payload.Messages))
	}
	if len(payload.Messages[0].Content) != 2 {
		t.Fatalf("tool result blocks = %d, want 2", len(payload.Messages[0].Content))
	}
	input, err := awsConverseInput(payload)
	if err != nil {
		t.Fatalf("awsConverseInput returned error: %v", err)
	}
	messages := input["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	second := content[1]["toolResult"].(map[string]any)
	resultContent := second["content"].([]map[string]any)
	if len(resultContent) != 2 {
		t.Fatalf("second tool result content = %d, want text and image", len(resultContent))
	}
	if _, ok := resultContent[1]["image"]; !ok {
		t.Fatalf("second tool result image missing: %#v", resultContent[1])
	}
}

func TestAWSConverseInputRejectsMaxTokensOutsideInt32(t *testing.T) {
	t.Parallel()

	maxTokens := math.MaxInt32 + 1
	_, err := awsConverseInput(ConverseRequest{
		ModelID: "bedrock-test-model",
		InferenceConfig: &ConverseInferenceConfig{
			MaxTokens: &maxTokens,
		},
	})
	if err == nil {
		t.Fatal("awsConverseInput returned nil error")
	}
	if !strings.Contains(err.Error(), "exceeds int32 range") {
		t.Fatalf("error = %v, want int32 range error", err)
	}
}

func TestCompleteUsesFakeCredentialDetectorAndClient(t *testing.T) {
	t.Parallel()

	fakeClient := &fakeConverseClient{
		stream: fakeStream(
			ConverseEvent{Kind: ConverseEventMessageStart, Role: "assistant"},
			ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 0, TextDelta: "ok"},
			ConverseEvent{Kind: ConverseEventMessageStop, StopReason: "end_turn"},
			ConverseEvent{Kind: ConverseEventMetadata, Usage: &ConverseUsage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}},
		),
	}
	providerID := sigma.ProviderID("bedrock-fake")
	model := bedrockTestModel(providerID)
	client := bedrockTestClient(t, providerID, model, fakeClient, fakeCredentialDetector{
		info: CredentialInfo{Source: CredentialSourceDefaultChain, AccessKeyID: "AKIAFAKE"},
	})

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "ok"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if fakeClient.request.ModelID == "" {
		t.Fatal("fake client did not receive request")
	}
	if final.Usage == nil || final.Usage.TotalTokens != 3 {
		t.Fatalf("usage = %+v, want total tokens 3", final.Usage)
	}
}

func TestConverseRejectsProviderDefinedTools(t *testing.T) {
	t.Parallel()

	fakeClient := &fakeConverseClient{}
	providerID := sigma.ProviderID("bedrock-provider-tools-test")
	model := bedrockTestModel(providerID)
	client := bedrockTestClient(t, providerID, model, fakeClient, fakeCredentialDetector{})

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("Search current docs.")},
		Tools: []sigma.Tool{{
			Name:                "web_search",
			ProviderDefinedType: "web_search_20250305",
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
	if fakeClient.request.ModelID != "" {
		t.Fatalf("unexpected provider request: %#v", fakeClient.request)
	}
}

func TestHTTPConverseStreamClientSignsRequestAndParsesEventStream(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuthorization string
	var gotCustomHeader string
	var gotAmzDate string
	var gotSecurityToken string
	var gotPayload []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotAuthorization = r.Header.Get("Authorization")
		gotCustomHeader = r.Header.Get("X-Custom")
		gotAmzDate = r.Header.Get("X-Amz-Date")
		gotSecurityToken = r.Header.Get("X-Amz-Security-Token")
		var err error
		gotPayload, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(bedrockEventStream(
			bedrockEventStreamFrame("messageStart", []byte(`{"role":"assistant"}`)),
			bedrockEventStreamFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"ok"}}`)),
			bedrockEventStreamFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)),
			bedrockEventStreamFrame("metadata", []byte(`{"usage":{"inputTokens":2,"outputTokens":1,"totalTokens":3}}`)),
		))
	}))
	defer server.Close()

	client, err := newHTTPConverseStreamClient(context.Background(), Config{
		Region:   "us-east-1",
		Endpoint: server.URL,
	}, sigma.Options{Headers: map[string]string{
		"X-Custom":      "custom",
		"Authorization": "evil",
		"X-Amz-Date":    "evil",
	}}, CredentialInfo{
		Source:          CredentialSourceAuthResolver,
		AccessKeyID:     "AKIAFAKE",
		SecretAccessKey: "secret",
		SessionToken:    "session",
	})
	if err != nil {
		t.Fatalf("newHTTPConverseStreamClient returned error: %v", err)
	}
	stream, err := client.ConverseStream(context.Background(), ConverseRequest{
		ModelID:  "anthropic.claude-3-5-sonnet-20240620-v1:0",
		Messages: []ConverseMessage{{Role: "user", Content: []ConverseContentBlock{{Type: converseBlockText, Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("ConverseStream returned error: %v", err)
	}
	events := readConverseEvents(stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream err = %v", err)
	}

	if got, want := gotPath, "/model/anthropic.claude-3-5-sonnet-20240620-v1:0/converse-stream"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if !strings.HasPrefix(gotAuthorization, "AWS4-HMAC-SHA256 Credential=AKIAFAKE/") {
		t.Fatalf("Authorization = %q, want SigV4 credential", gotAuthorization)
	}
	if got, want := gotCustomHeader, "custom"; got != want {
		t.Fatalf("custom header = %q, want %q", got, want)
	}
	if gotAmzDate == "evil" {
		t.Fatalf("reserved x-amz-date header was overwritten by caller")
	}
	if got, want := gotSecurityToken, "session"; got != want {
		t.Fatalf("security token = %q, want %q", got, want)
	}
	goldentest.AssertJSON(t, gotPayload, "provider/bedrock/converse/http_payload.json")
	if got, want := eventKindsFromConverse(events), []ConverseEventKind{
		ConverseEventMessageStart,
		ConverseEventContentBlockDelta,
		ConverseEventMessageStop,
		ConverseEventMetadata,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if got, want := events[1].TextDelta, "ok"; got != want {
		t.Fatalf("text delta = %q, want %q", got, want)
	}
	if events[3].Usage == nil || events[3].Usage.TotalTokens != 3 {
		t.Fatalf("usage = %+v, want total tokens 3", events[3].Usage)
	}
}

func TestHTTPConverseStreamClientRetriesAndRunsResponseHooks(t *testing.T) {
	t.Parallel()

	var attempts int
	var statuses []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"retry"}`))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(bedrockEventStream(bedrockEventStreamFrame("messageStop", []byte(`{"stopReason":"end_turn"}`))))
	}))
	defer server.Close()

	client, err := newHTTPConverseStreamClient(context.Background(), Config{
		Region:   "us-east-1",
		Endpoint: server.URL,
	}, sigma.Options{
		MaxRetries:    intPtr(1),
		MaxRetryDelay: durationPtrForTest(0),
		TextResponseDebugHooks: []sigma.TextResponseDebugHook{
			func(_ context.Context, debug sigma.TextResponseDebug) error {
				statuses = append(statuses, debug.StatusCode)
				return nil
			},
		},
	}, CredentialInfo{BearerToken: "bedrock-token"})
	if err != nil {
		t.Fatalf("newHTTPConverseStreamClient returned error: %v", err)
	}
	stream, err := client.ConverseStream(context.Background(), ConverseRequest{ModelID: "model"})
	if err != nil {
		t.Fatalf("ConverseStream returned error: %v", err)
	}
	_ = readConverseEvents(stream)

	if got, want := attempts, 2; got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
	if got, want := statuses, []int{http.StatusTooManyRequests, http.StatusOK}; !reflect.DeepEqual(got, want) {
		t.Fatalf("response hook statuses = %v, want %v", got, want)
	}
}

func TestHTTPConverseStreamClientUsesBearerToken(t *testing.T) {
	t.Parallel()

	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(bedrockEventStream(bedrockEventStreamFrame("messageStop", []byte(`{"stopReason":"end_turn"}`))))
	}))
	defer server.Close()

	client, err := newHTTPConverseStreamClient(context.Background(), Config{
		Region:   "us-east-1",
		Endpoint: server.URL,
	}, sigma.Options{}, CredentialInfo{BearerToken: "bedrock-token"})
	if err != nil {
		t.Fatalf("newHTTPConverseStreamClient returned error: %v", err)
	}
	stream, err := client.ConverseStream(context.Background(), ConverseRequest{ModelID: "model"})
	if err != nil {
		t.Fatalf("ConverseStream returned error: %v", err)
	}
	_ = readConverseEvents(stream)

	if got, want := gotAuthorization, "Bearer bedrock-token"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func TestEventStreamDecoderReportsMalformedFrame(t *testing.T) {
	t.Parallel()

	stream := newHTTPConverseStream(io.NopCloser(bytes.NewReader([]byte{0, 0, 0, 16})))
	_ = readConverseEvents(stream)
	if err := stream.Err(); err == nil {
		t.Fatal("stream error = nil, want malformed frame error")
	}
}

func TestStreamingMapsThinkingToolCallsUsageAndStopReason(t *testing.T) {
	t.Parallel()

	stream := fakeStream(
		ConverseEvent{Kind: ConverseEventMessageStart, Role: "assistant"},
		ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 0, ThinkingDelta: "plan"},
		ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 0, ThinkingSignature: "sig"},
		ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 1, TextDelta: "Use "},
		ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 1, TextDelta: "tool"},
		ConverseEvent{Kind: ConverseEventContentBlockStart, ContentBlockIndex: 2, ToolUseID: "tool_1", ToolName: "lookup"},
		ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 2, ToolInputDelta: "{\"id\""},
		ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 2, ToolInputDelta: ":\"abc\"}"},
		ConverseEvent{Kind: ConverseEventMetadata, Usage: &ConverseUsage{InputTokens: 7, OutputTokens: 5, TotalTokens: 12, CacheReadInputTokens: 3}},
		ConverseEvent{Kind: ConverseEventMessageStop, StopReason: "tool_use"},
	)
	fakeClient := &fakeConverseClient{stream: stream}
	providerID := sigma.ProviderID("bedrock-stream")
	model := bedrockTestModel(providerID)
	client := bedrockTestClient(t, providerID, model, fakeClient, fakeCredentialDetector{})

	sigmaStream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	events := collectEvents(t, sigmaStream)
	if err := sigmaStream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := sigmaStream.Final()
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
		sigma.EventKindToolCallStart,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallDelta,
		sigma.EventKindTextEnd,
		sigma.EventKindThinkingEnd,
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonToolCalls; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ThinkingText, "plan"; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Signature, "sig"; got != want {
		t.Fatalf("thinking signature = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "Use tool"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	args := final.Content[2].ToolArguments.(map[string]any)
	if got, want := args["id"], "abc"; got != want {
		t.Fatalf("tool id = %v, want %v", got, want)
	}
	if final.Usage == nil || final.Usage.CacheReadInputTokens != 3 {
		t.Fatalf("usage = %+v, want cache read tokens 3", final.Usage)
	}
}

func TestCancellationPreservesPartialConverseContent(t *testing.T) {
	t.Parallel()

	events := make(chan ConverseEvent, 5)
	events <- ConverseEvent{Kind: ConverseEventMessageStart, Role: "assistant"}
	events <- ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 0, ThinkingDelta: "partial plan"}
	events <- ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 1, TextDelta: "partial text"}
	events <- ConverseEvent{Kind: ConverseEventContentBlockStart, ContentBlockIndex: 2, ToolUseID: "tool_partial", ToolName: "lookup"}
	events <- ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 2, ToolInputDelta: "{\"city\":\"Melbourne\"}"}

	fakeClient := &fakeConverseClient{stream: &fakeConverseStream{events: events}}
	providerID := sigma.ProviderID("bedrock-cancel")
	model := bedrockTestModel(providerID)
	client := bedrockTestClient(t, providerID, model, fakeClient, fakeCredentialDetector{})

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
	if !errors.Is(err, sigma.ErrAborted) {
		t.Fatalf("Collect error = %v, want ErrAborted", err)
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
	if got, want := final.Content[2].ToolCallID, "tool_partial"; got != want {
		t.Fatalf("partial tool id = %q, want %q", got, want)
	}
	args := final.Content[2].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("partial tool city = %v, want %v", got, want)
	}
}

func TestCredentialErrorsAreTypedAndRedacted(t *testing.T) {
	t.Parallel()

	model := bedrockTestModel(sigma.ProviderAmazonBedrock)
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{
			Type:   sigma.CredentialTypeCloudCredential,
			Value:  "SECRET_ACCESS_KEY_SHOULD_NOT_LEAK",
			Source: "test",
		}, nil
	})

	_, err := (DefaultCredentialDetector{}).Detect(context.Background(), model, sigma.Options{
		AuthResolver: resolver,
	}, Config{CredentialSource: CredentialSourceAuthResolver})
	if err == nil {
		t.Fatal("Detect returned nil error")
	}
	if !errors.Is(err, sigma.ErrCredentialUnavailable) {
		t.Fatalf("error = %v, want ErrCredentialUnavailable", err)
	}
	if strings.Contains(err.Error(), "SECRET_ACCESS_KEY_SHOULD_NOT_LEAK") {
		t.Fatalf("error leaked credential: %v", err)
	}
}

func TestDefaultCredentialDetectorUsesBedrockBearerTokenFromEnvironment(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "bearer-token")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIGNORED")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ignored-secret")

	model := bedrockTestModel(sigma.ProviderAmazonBedrock)
	info, err := (DefaultCredentialDetector{}).Detect(context.Background(), model, sigma.Options{}, Config{
		Region:           "us-east-1",
		CredentialSource: CredentialSourceDefaultChain,
	})
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if got, want := info.BearerToken, "bearer-token"; got != want {
		t.Fatalf("bearer token = %q, want %q", got, want)
	}
	if info.AccessKeyID != "" || info.SecretAccessKey != "" {
		t.Fatalf("static credentials = %q/%q, want bearer token precedence", info.AccessKeyID, info.SecretAccessKey)
	}
}

func TestDefaultCredentialDetectorUsesStaticEnvironmentCredentials(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIASTATIC")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "static-secret")
	t.Setenv("AWS_SESSION_TOKEN", "session-token")

	model := bedrockTestModel(sigma.ProviderAmazonBedrock)
	info, err := (DefaultCredentialDetector{}).Detect(context.Background(), model, sigma.Options{}, Config{
		Region:           "us-east-1",
		CredentialSource: CredentialSourceDefaultChain,
	})
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if got, want := info.AccessKeyID, "AKIASTATIC"; got != want {
		t.Fatalf("access key = %q, want %q", got, want)
	}
	if got, want := info.SecretAccessKey, "static-secret"; got != want {
		t.Fatalf("secret key = %q, want %q", got, want)
	}
	if got, want := info.SessionToken, "session-token"; got != want {
		t.Fatalf("session token = %q, want %q", got, want)
	}
}

func TestEffectiveConfigUsesRegionEnvironmentFallback(t *testing.T) {
	t.Setenv("AWS_REGION", "ap-southeast-2")
	t.Setenv("AWS_DEFAULT_REGION", "us-west-2")

	config := effectiveConfig(Config{}, bedrockTestModel(sigma.ProviderAmazonBedrock), sigma.Options{})
	if got, want := config.Region, "ap-southeast-2"; got != want {
		t.Fatalf("region = %q, want %q", got, want)
	}

	t.Setenv("AWS_REGION", "")
	config = effectiveConfig(Config{}, bedrockTestModel(sigma.ProviderAmazonBedrock), sigma.Options{})
	if got, want := config.Region, "us-west-2"; got != want {
		t.Fatalf("region = %q, want %q", got, want)
	}
}

func TestAuthResolverOAuthTokenBecomesBearerToken(t *testing.T) {
	t.Parallel()

	model := bedrockTestModel(sigma.ProviderAmazonBedrock)
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{
			Type:  sigma.CredentialTypeOAuthToken,
			Value: "resolver-token",
		}, nil
	})

	info, err := (DefaultCredentialDetector{}).Detect(context.Background(), model, sigma.Options{
		AuthResolver: resolver,
	}, Config{CredentialSource: CredentialSourceAuthResolver})
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if got, want := info.BearerToken, "resolver-token"; got != want {
		t.Fatalf("bearer token = %q, want %q", got, want)
	}
}

func TestLiveConverseStreamScaffold(t *testing.T) {
	if os.Getenv("SIGMA_BEDROCK_LIVE") != "1" {
		t.Skip("set SIGMA_BEDROCK_LIVE=1, AWS_REGION, and BEDROCK_MODEL_ID to run")
	}
	region := os.Getenv("AWS_REGION")
	modelID := os.Getenv("BEDROCK_MODEL_ID")
	if region == "" || modelID == "" {
		t.Skip("AWS_REGION and BEDROCK_MODEL_ID are required")
	}

	providerID := sigma.ProviderAmazonBedrock
	model := bedrockTestModel(providerID)
	model.ID = sigma.ModelID(modelID)
	client := bedrockTestClient(t, providerID, model, nil, nil, WithRegion(region), WithModelID(modelID))

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("Reply with ok.")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if len(final.Content) == 0 {
		t.Fatal("final content was empty")
	}
}

func bedrockTestClient(t *testing.T, providerID sigma.ProviderID, model sigma.Model, client ConverseStreamClient, detector CredentialDetector, opts ...ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]ProviderOption{}, opts...)
	if client != nil {
		providerOpts = append(providerOpts, WithConverseStreamClient(client))
	}
	if detector != nil {
		providerOpts = append(providerOpts, WithCredentialDetector(detector))
	}
	if err := registry.RegisterTextProvider(providerID, NewProvider(providerOpts...)); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func bedrockTestModel(providerID sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:                   "bedrock-test-model",
		Provider:             providerID,
		API:                  sigma.APIBedrockConverseStream,
		SupportedInputs:      []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:        true,
		SupportsThinking:     true,
		InputCostPerMillion:  1,
		OutputCostPerMillion: 2,
	}
}

type fakeCredentialDetector struct {
	info CredentialInfo
	err  error
}

func (d fakeCredentialDetector) Detect(context.Context, sigma.Model, sigma.Options, Config) (CredentialInfo, error) {
	if d.err != nil {
		return CredentialInfo{}, d.err
	}
	if d.info.Source == "" {
		return CredentialInfo{Source: CredentialSourceDefaultChain, AccessKeyID: "AKIAFAKE"}, nil
	}
	return d.info, nil
}

type fakeConverseClient struct {
	request ConverseRequest
	stream  ConverseStream
	err     error
}

func (c *fakeConverseClient) ConverseStream(_ context.Context, req ConverseRequest) (ConverseStream, error) {
	c.request = req
	if c.err != nil {
		return nil, c.err
	}
	if c.stream == nil {
		return fakeStream(ConverseEvent{Kind: ConverseEventMessageStop, StopReason: "end_turn"}), nil
	}
	return c.stream, nil
}

type fakeConverseStream struct {
	events chan ConverseEvent
	err    error
}

func fakeStream(events ...ConverseEvent) *fakeConverseStream {
	ch := make(chan ConverseEvent, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return &fakeConverseStream{events: ch}
}

func (s *fakeConverseStream) Events() <-chan ConverseEvent {
	return s.events
}

func (s *fakeConverseStream) Close() error {
	return nil
}

func (s *fakeConverseStream) Err() error {
	return s.err
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

	select {
	case event, ok := <-stream.Events():
		if !ok {
			t.Fatal("stream closed before event")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return sigma.Event{}
	}
}

func eventKinds(events []sigma.Event) []sigma.EventKind {
	kinds := make([]sigma.EventKind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	return kinds
}

func eventKindsFromConverse(events []ConverseEvent) []ConverseEventKind {
	kinds := make([]ConverseEventKind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	return kinds
}

func readConverseEvents(stream ConverseStream) []ConverseEvent {
	var events []ConverseEvent
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events
}

func durationPtrForTest(value time.Duration) *time.Duration {
	return &value
}

func bedrockEventStream(frames ...[]byte) []byte {
	return bytes.Join(frames, nil)
}

func bedrockEventStreamFrame(eventType string, payload []byte) []byte {
	headers := appendEventStreamHeader(nil, ":message-type", "event")
	headers = appendEventStreamHeader(headers, ":event-type", eventType)
	headers = appendEventStreamHeader(headers, ":content-type", "application/json")
	totalLen := 12 + len(headers) + len(payload) + 4
	frame := make([]byte, totalLen)
	binary.BigEndian.PutUint32(frame[0:4], uint32(totalLen))
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(headers)))
	binary.BigEndian.PutUint32(frame[8:12], crc32.ChecksumIEEE(frame[0:8]))
	copy(frame[12:], headers)
	copy(frame[12+len(headers):], payload)
	binary.BigEndian.PutUint32(frame[totalLen-4:], crc32.ChecksumIEEE(frame[:totalLen-4]))
	return frame
}

func appendEventStreamHeader(dst []byte, name string, value string) []byte {
	dst = append(dst, byte(len(name)))
	dst = append(dst, name...)
	dst = append(dst, 7)
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(value)))
	dst = append(dst, value...)
	return dst
}
