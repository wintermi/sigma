// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/transform"
	"github.com/wintermi/sigma/sigmatest"
)

func TestRequestPersistenceRoundTripCoversReplayContent(t *testing.T) {
	t.Parallel()

	req := persistedReplayRequest()

	data, err := sigma.MarshalRequest(req)
	if err != nil {
		t.Fatalf("MarshalRequest returned error: %v", err)
	}

	roundTripped, err := sigma.UnmarshalRequest(data)
	if err != nil {
		t.Fatalf("UnmarshalRequest returned error: %v", err)
	}

	after, err := sigma.MarshalRequest(roundTripped)
	if err != nil {
		t.Fatalf("MarshalRequest after round trip returned error: %v", err)
	}
	assertSameJSON(t, after, data)

	thinking := roundTripped.Messages[1].Content[0]
	if got, want := thinking.ProviderSignature, "opaque-thinking-signature"; got != want {
		t.Fatalf("thinking provider signature = %q, want %q", got, want)
	}
	toolCall := roundTripped.Messages[1].Content[2]
	if got, want := toolCall.ProviderSignature, "opaque-tool-signature"; got != want {
		t.Fatalf("tool-call provider signature = %q, want %q", got, want)
	}
	if roundTripped.Messages[1].Usage == nil || roundTripped.Messages[1].Usage.Total() != 123 {
		t.Fatalf("assistant usage after round trip = %#v, want total usage 123", roundTripped.Messages[1].Usage)
	}
}

func TestRequestPersistenceRejectsInvalidReplayState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       sigma.Request
		errorText string
	}{
		{
			name:      "missing role",
			req:       sigma.Request{Messages: []sigma.Message{{Content: []sigma.ContentBlock{sigma.Text("hello")}}}},
			errorText: "role is required",
		},
		{
			name: "invalid content block",
			req: sigma.Request{Messages: []sigma.Message{{
				Role:    sigma.RoleUser,
				Content: []sigma.ContentBlock{{Type: sigma.ContentBlockText}},
			}}},
			errorText: "text block is empty",
		},
		{
			name:      "orphaned tool result",
			req:       sigma.Request{Messages: []sigma.Message{sigma.ToolResult("call_missing", "weather")}},
			errorText: "no preceding assistant tool call",
		},
		{
			name: "duplicate tool call id",
			req: sigma.Request{Messages: []sigma.Message{
				{
					Role:    sigma.RoleAssistant,
					Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_1", "weather", map[string]any{"city": "Melbourne"})},
				},
				{
					Role:    sigma.RoleAssistant,
					Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_1", "lookup", map[string]any{"query": "Melbourne"})},
				},
			}},
			errorText: "duplicate tool call id",
		},
		{
			name: "unsupported image data",
			req: sigma.Request{Messages: []sigma.Message{{
				Role:    sigma.RoleUser,
				Content: []sigma.ContentBlock{{Type: sigma.ContentBlockImage, MIMEType: "image/png", ImageSource: "file", Data: "/tmp/image.png"}},
			}}},
			errorText: "unsupported image source",
		},
		{
			name: "missing document MIME type",
			req: sigma.Request{Messages: []sigma.Message{{
				Role:    sigma.RoleUser,
				Content: []sigma.ContentBlock{sigma.DocumentBase64("", "input.pdf", "JVBERi0xLjQ=")},
			}}},
			errorText: "document MIME type is required",
		},
		{
			name: "missing document filename",
			req: sigma.Request{Messages: []sigma.Message{{
				Role:    sigma.RoleUser,
				Content: []sigma.ContentBlock{sigma.DocumentBase64("application/pdf", "", "JVBERi0xLjQ=")},
			}}},
			errorText: "document filename is required",
		},
		{
			name: "invalid document source",
			req: sigma.Request{Messages: []sigma.Message{{
				Role:    sigma.RoleUser,
				Content: []sigma.ContentBlock{{Type: sigma.ContentBlockDocument, MIMEType: "application/pdf", DocumentSource: "file", Filename: "input.pdf", Data: "/tmp/input.pdf"}},
			}}},
			errorText: "unsupported document source",
		},
		{
			name: "invalid base64 document data",
			req: sigma.Request{Messages: []sigma.Message{{
				Role:    sigma.RoleUser,
				Content: []sigma.ContentBlock{sigma.DocumentBase64("application/pdf", "input.pdf", "not base64")},
			}}},
			errorText: "base64 document data is invalid",
		},
		{
			name: "assistant document block",
			req: sigma.Request{Messages: []sigma.Message{{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.DocumentBase64("application/pdf", "input.pdf", "JVBERi0xLjQ=")},
			}}},
			errorText: `role "assistant" cannot contain "document" blocks`,
		},
		{
			name: "inconsistent assistant metadata",
			req: sigma.Request{Messages: []sigma.Message{{
				Role:    sigma.RoleAssistant,
				API:     sigma.APIAnthropicMessages,
				Content: []sigma.ContentBlock{sigma.Text("hello")},
			}}},
			errorText: "requires provider metadata",
		},
		{
			name: "usage on non-assistant message",
			req: sigma.Request{Messages: []sigma.Message{{
				Role:    sigma.RoleUser,
				Content: []sigma.ContentBlock{sigma.Text("hello")},
				Usage:   &sigma.Usage{InputTokens: 1},
			}}},
			errorText: "assistant usage requires role",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := sigma.ValidateRequest(tt.req)
			if err == nil {
				t.Fatal("ValidateRequest returned nil error")
			}
			assertInvalidRequestError(t, err, tt.errorText)
		})
	}
}

func TestUnmarshalRequestRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := sigma.UnmarshalRequest([]byte(`{"messages":[],"schemaVersion":1}`))
	if err == nil {
		t.Fatal("UnmarshalRequest returned nil error")
	}
	assertInvalidRequestError(t, err, "unknown field")
}

func TestUnmarshalRequestRoundTripsContentBlockExtraFields(t *testing.T) {
	t.Parallel()

	input := `{"messages":[{"role":"user","content":[{"type":"text","text":"hello","unexpected":true}]}]}`
	req, err := sigma.UnmarshalRequest([]byte(input))
	if err != nil {
		t.Fatalf("UnmarshalRequest returned error: %v", err)
	}
	block := req.Messages[0].Content[0]
	if got, want := block.ExtraFields["unexpected"], true; got != want {
		t.Fatalf("extra field = %v, want %v", got, want)
	}

	data, err := sigma.MarshalRequest(req)
	if err != nil {
		t.Fatalf("MarshalRequest returned error: %v", err)
	}
	if !strings.Contains(string(data), `"unexpected":true`) {
		t.Fatalf("marshaled request lost extra field: %s", data)
	}
}

func TestPersistedRequestCanBeUsedForCompletion(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Final: sigma.AssistantMessage{
			Content:    []sigma.ContentBlock{sigma.Text("It is 18 C.")},
			StopReason: sigma.StopReasonEndTurn,
		},
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("sigmatest.Registry returned error: %v", err)
	}

	req := sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("What is the weather?"),
			{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_weather", "weather", map[string]any{"city": "Melbourne"})},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call_weather",
				Content: []sigma.ContentBlock{
					sigma.Text("18 C"),
					sigma.DocumentFileID("application/pdf", "forecast.pdf", "file_forecast"),
				},
			},
			sigma.UserText("Summarize it."),
		},
		Tools: []sigma.Tool{{
			Name:        "weather",
			Description: "Looks up weather.",
			InputSchema: sigma.Schema{"type": "object"},
		}},
	}
	data, err := sigma.MarshalRequest(req)
	if err != nil {
		t.Fatalf("MarshalRequest returned error: %v", err)
	}
	replayed, err := sigma.UnmarshalRequest(data)
	if err != nil {
		t.Fatalf("UnmarshalRequest returned error: %v", err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	final, err := client.Complete(context.Background(), sigmatest.TextModel(), replayed)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "It is 18 C."; got != want {
		t.Fatalf("completion text = %q, want %q", got, want)
	}

	capture, ok := provider.LastRequest()
	if !ok {
		t.Fatal("faux provider did not capture request")
	}
	if got, want := len(capture.Request.Messages), len(req.Messages); got != want {
		t.Fatalf("captured message count = %d, want %d", got, want)
	}
}

func TestCrossProviderReplayAfterJSONRoundTrip(t *testing.T) {
	t.Parallel()

	source := sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("Check the forecast."),
			{
				Role:     sigma.RoleAssistant,
				Provider: sigma.ProviderAnthropic,
				API:      sigma.APIAnthropicMessages,
				Model:    "claude-sonnet",
				Content: []sigma.ContentBlock{
					{
						Type:              sigma.ContentBlockThinking,
						ThinkingText:      "Need the weather tool.",
						Signature:         "anthropic-signature",
						ProviderSignature: "opaque-provider-signature",
					},
					sigma.Text("I will check."),
					sigma.ToolCallBlock("call_weather", "weather", map[string]any{"city": "Melbourne"}),
				},
				StopReason: sigma.StopReasonToolCalls,
			},
			sigma.ToolResult("call_weather", "18 C"),
			sigma.UserText("Now answer."),
		},
		Tools: []sigma.Tool{{
			Name:        "weather",
			Description: "Looks up weather.",
			InputSchema: sigma.Schema{"type": "object"},
		}},
	}

	data, err := sigma.MarshalRequest(source)
	if err != nil {
		t.Fatalf("MarshalRequest returned error: %v", err)
	}
	replayed, err := sigma.UnmarshalRequest(data)
	if err != nil {
		t.Fatalf("UnmarshalRequest returned error: %v", err)
	}
	if got, want := replayed.Messages[1].Content[0].ProviderSignature, "opaque-provider-signature"; got != want {
		t.Fatalf("provider signature after JSON round trip = %q, want %q", got, want)
	}

	target := sigma.Model{
		ID:            "gpt-fixture",
		Provider:      sigma.ProviderOpenAI,
		API:           sigma.APIOpenAIResponses,
		SupportsTools: true,
	}
	transformed, err := transform.Transform(transform.Input{
		TargetModel: target,
		Request:     replayed,
		Compatibility: transform.Compatibility{
			RequireToolResultName: true,
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}
	if got, want := transformed.Messages[1].Content[0].Type, sigma.ContentBlockText; got != want {
		t.Fatalf("foreign thinking block type = %q, want %q", got, want)
	}

	provider := &apiFauxProvider{
		FauxProvider: sigmatest.NewFauxProvider(sigmatest.Script{
			Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("It is 18 C.")}},
		}),
		api: sigma.APIOpenAIResponses,
	}
	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(sigma.ProviderOpenAI, provider); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(target); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, err := client.Complete(context.Background(), target, transformed); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	capture, ok := provider.LastRequest()
	if !ok {
		t.Fatal("faux provider did not capture transformed request")
	}
	if got, want := capture.Request.Messages[2].ToolName, "weather"; got != want {
		t.Fatalf("transformed tool result name = %q, want %q", got, want)
	}
}

func persistedReplayRequest() sigma.Request {
	toolCall := sigma.ToolCallBlock("call_weather", "weather", map[string]any{
		"city": "Melbourne",
		"days": json.Number("1"),
	})
	toolCall.ProviderSignature = "opaque-tool-signature"

	return sigma.Request{
		SystemPrompt: "Be concise.",
		Messages: []sigma.Message{
			sigma.UserContent(
				sigma.Text("What is the weather?"),
				sigma.ImageBase64("image/png", "aGVsbG8="),
				sigma.ImageURL("image/jpeg", "https://example.test/input.jpg"),
				sigma.DocumentBase64("application/pdf", "forecast.pdf", "JVBERi0xLjQ="),
			),
			{
				Role:     sigma.RoleAssistant,
				Provider: sigma.ProviderAnthropic,
				API:      sigma.APIAnthropicMessages,
				Model:    "claude-sonnet",
				Content: []sigma.ContentBlock{
					{
						Type:              sigma.ContentBlockThinking,
						ThinkingText:      "Need current weather.",
						Signature:         "thinking-signature",
						ProviderSignature: "opaque-thinking-signature",
					},
					sigma.Text("I will check."),
					toolCall,
				},
				StopReason: sigma.StopReasonToolCalls,
				Usage:      &sigma.Usage{InputTokens: 100, OutputTokens: 23},
			},
			sigma.ToolResult("call_weather", "18 C"),
			{
				Role:       sigma.RoleAssistant,
				Content:    []sigma.ContentBlock{sigma.Text("Partial answer")},
				StopReason: sigma.StopReasonAborted,
			},
			{
				Role:       sigma.RoleAssistant,
				Content:    []sigma.ContentBlock{sigma.Text("Provider failed after partial output.")},
				StopReason: sigma.StopReasonError,
			},
		},
		Tools: []sigma.Tool{{
			Name:        "weather",
			Description: "Looks up current weather.",
			InputSchema: sigma.Schema{"type": "object"},
		}},
	}
}

func assertInvalidRequestError(t *testing.T, err error, wantText string) {
	t.Helper()

	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if got, want := sigmaErr.Code, sigma.ErrorInvalidRequest; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if !strings.Contains(err.Error(), wantText) {
		t.Fatalf("error = %q, want substring %q", err.Error(), wantText)
	}
}

type apiFauxProvider struct {
	*sigmatest.FauxProvider
	api sigma.API
}

func (p *apiFauxProvider) API() sigma.API {
	return p.api
}
