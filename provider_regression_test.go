// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/provider/openrouter"
)

type regressionTransport func(*http.Request) (*http.Response, error)

func (f regressionTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func regressionResponse(req *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func regressionOptions(body string) sigma.Options {
	return sigma.Options{
		AuthResolver: sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "synthetic-key"}, nil
		}),
		HTTPClient: &http.Client{Transport: regressionTransport(func(req *http.Request) (*http.Response, error) {
			return regressionResponse(req, body), nil
		})},
	}
}

func TestSyntheticToolIDsPersistAcrossTurnsAndStreams(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{"google", "vertex", "openai", "custom"} {
		t.Run(surface, func(t *testing.T) {
			t.Parallel()
			var provider sigma.TextProvider = openai.NewProvider()
			body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"
			prefix := "call_"
			tool := sigma.Tool{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}
			if surface == "google" || surface == "vertex" {
				provider = google.NewProvider()
				if surface == "vertex" {
					provider = google.NewVertexProvider(google.WithVertexConfig(google.VertexConfig{ProjectID: "project", Location: "us-central1", CredentialMode: google.VertexCredentialAPIKey}))
				}
				prefix = "google_tool_call_"
				body = "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"lookup\",\"args\":{}}}]},\"finishReason\":\"STOP\"}]}\n\n"
			}
			if surface == "custom" {
				tool.InputSchema = sigma.Schema{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}, "required": []any{"command"}}
				tool.OpenAIGrammar = &sigma.OpenAIGrammar{Syntax: sigma.OpenAIGrammarRegex, Definition: ".*"}
				body = "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"custom\",\"custom\":{\"name\":\"lookup\",\"input\":\"go\"}}]}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"custom\",\"custom\":{\"input\":\" test\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"
			}
			model := sigma.Model{ID: "test", Provider: "test", API: provider.API()}
			opts := regressionOptions(body)
			enabled := true
			opts.OpenAIOptions = &sigma.OpenAIOptions{EnableGrammarTools: &enabled}
			request := sigma.Request{Messages: []sigma.Message{sigma.UserText("lookup")}, Tools: []sigma.Tool{tool}}
			validID := regexp.MustCompile("^" + prefix + "[A-Za-z0-9_-]+$")
			collect := func(request sigma.Request) sigma.AssistantMessage {
				stream := provider.Stream(context.Background(), model, request, opts)
				var eventIDs []string
				for event := range stream.Events() {
					if event.PartialToolCall != nil {
						eventIDs = append(eventIDs, event.PartialToolCall.ID)
					}
				}
				if err := stream.Err(); err != nil {
					t.Fatal(err)
				}
				final, ok := stream.Final()
				if !ok || len(final.Content) != 1 {
					t.Fatalf("missing tool result: %#v", final)
				}
				id := final.Content[0].ToolCallID
				if !validID.MatchString(id) {
					t.Fatalf("unsafe synthetic ID %q", id)
				}
				if len(eventIDs) < 2 {
					t.Fatal("missing tool events")
				}
				for _, eventID := range eventIDs {
					if eventID != id {
						t.Fatalf("event ID %q differs from final %q", eventID, id)
					}
				}
				return final
			}
			for range 2 {
				final := collect(request)
				request.Messages = append(request.Messages, sigma.Message{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, Content: final.Content, StopReason: final.StopReason}, sigma.ToolResult(final.Content[0].ToolCallID, "found"))
			}
			encoded, err := sigma.MarshalRequest(request)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := sigma.UnmarshalRequest(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored.Messages, request.Messages) {
				t.Fatal("tool history changed during persistence")
			}
			// Start independent streams together; their parsers cannot share counters.
			streams := make([]*sigma.Stream, 8)
			for i := range streams {
				streams[i] = provider.Stream(context.Background(), model, request, opts)
			}
			ids := map[string]bool{request.Messages[1].Content[0].ToolCallID: true, request.Messages[3].Content[0].ToolCallID: true}
			for _, stream := range streams {
				final, err := sigma.Collect(context.Background(), stream)
				if err != nil {
					t.Fatal(err)
				}
				id := final.Content[0].ToolCallID
				if ids[id] {
					t.Fatalf("independent streams reused %q", id)
				}
				ids[id] = true
			}
		})
	}
}

func TestInlineImageErrorsRedactStructuredResults(t *testing.T) {
	t.Parallel()
	for _, provider := range []sigma.ImageProvider{openai.NewImagesProvider(), openrouter.NewImagesProvider()} {
		t.Run(string(provider.API()), func(t *testing.T) {
			t.Parallel()
			model := sigma.ImageModel{ID: "test", Provider: "test", API: provider.API()}
			result, err := provider.Generate(context.Background(), model, sigma.ImageRequest{Prompt: "test"}, regressionOptions(`{"error":{"message":"invalid key sk-message123; please retry","code":"sk-code123456","type":"sk-type123456"}}`))
			var providerErr *sigma.ProviderError
			if !errors.As(err, &providerErr) || providerErr.StatusCode != http.StatusOK || result.StopReason != sigma.StopReasonError {
				t.Fatalf("lost typed failure: %#v %v", result, err)
			}
			if len(result.Errors) != 1 || !strings.Contains(result.Errors[0].Message, "please retry") {
				t.Fatalf("lost useful error details: %#v", result.Errors)
			}
			encoded, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			for _, secret := range []string{"sk-message123", "sk-code123456", "sk-type123456"} {
				if strings.Contains(string(encoded), secret) || strings.Contains(err.Error(), secret) {
					t.Errorf("secret %q leaked: %s / %v", secret, encoded, err)
				}
			}
		})
	}
}

func TestGeminiImageTerminalEvidence(t *testing.T) {
	t.Parallel()
	for _, provider := range []sigma.ImageProvider{google.NewImagesProvider(), google.NewVertexImagesProvider(google.WithVertexConfig(google.VertexConfig{ProjectID: "project", Location: "us-central1", CredentialMode: google.VertexCredentialAPIKey}))} {
		for _, tc := range []struct {
			name, body string
			reason     sigma.StopReason
			outputs    int
			failed     bool
		}{
			{"success", `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"caption"},{"inlineData":{"data":"aW1hZ2U="}}]}}]}`, sigma.StopReasonEndTurn, 2, false},
			{"omitted", `{"candidates":[{"content":{"parts":[{"text":"caption"}]}}]}`, sigma.StopReasonEndTurn, 1, false},
			{"safety", `{"candidates":[{"finishReason":"SAFETY"}]}`, sigma.StopReasonContentFilter, 0, false},
			{"partial", `{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"partial"}]}}]}`, sigma.StopReasonMaxTokens, 1, false},
			{"prompt block", `{"promptFeedback":{"blockReason":"SAFETY","blockReasonMessage":"blocked"}}`, sigma.StopReasonContentFilter, 0, false},
			{"unknown", `{"candidates":[{"finishReason":"NEW_REASON"}]}`, sigma.StopReasonUnknown, 0, false},
			{"error", `{"candidates":[{"finishReason":"MALFORMED_FUNCTION_CALL","content":{"parts":[{"text":"partial"}]}}]}`, sigma.StopReasonError, 1, true},
			{"mixed safety", `{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"first"}]}},{"finishReason":"SAFETY","content":{"parts":[{"text":"second"}]}}]}`, sigma.StopReasonContentFilter, 2, false},
			{"mixed error", `{"candidates":[{"finishReason":"SAFETY"},{"finishReason":"UNEXPECTED_TOOL_CALL"}]}`, sigma.StopReasonError, 0, true},
			{"mixed limit", `{"candidates":[{"finishReason":"NEW_REASON"},{"finishReason":"MAX_TOKENS"}]}`, sigma.StopReasonMaxTokens, 0, false},
			{"mixed unknown", `{"candidates":[{"finishReason":"STOP"},{"finishReason":"NEW_REASON"}]}`, sigma.StopReasonUnknown, 0, false},
			{"empty", `{}`, sigma.StopReasonError, 0, true},
			{"empty candidates", `{"candidates":[{"content":{"parts":[]}}]}`, sigma.StopReasonError, 0, true},
			{"unspecified prompt", `{"promptFeedback":{"blockReason":"BLOCK_REASON_UNSPECIFIED"}}`, sigma.StopReasonError, 0, true},
		} {
			t.Run(string(provider.API())+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				model := sigma.ImageModel{ID: "gemini-image-test", Provider: "test", API: provider.API()}
				opts := regressionOptions(tc.body)
				attempts := 0
				opts.HTTPClient = &http.Client{Transport: regressionTransport(func(req *http.Request) (*http.Response, error) {
					attempts++
					return regressionResponse(req, tc.body), nil
				})}
				retries := 2
				opts.MaxRetries = &retries
				result, err := provider.Generate(context.Background(), model, sigma.ImageRequest{Prompt: "test"}, opts)
				if result.StopReason != tc.reason || len(result.Images) != tc.outputs || (err != nil) != tc.failed {
					t.Fatalf("got reason=%s outputs=%d err=%v", result.StopReason, len(result.Images), err)
				}
				if attempts != 1 {
					t.Fatalf("terminal response retried %d times", attempts)
				}
				if tc.failed {
					var typed *sigma.ProviderError
					if !errors.As(err, &typed) || typed.API != sigma.API(model.API) {
						t.Fatalf("missing correctly attributed provider error: %v", err)
					}
				}
				var decoded struct {
					Candidates     []struct{ FinishReason string }
					PromptFeedback map[string]any
				}
				if err := json.Unmarshal([]byte(tc.body), &decoded); err != nil {
					t.Fatal(err)
				}
				if len(decoded.Candidates) > 0 && decoded.Candidates[0].FinishReason != "" {
					reasons := make([]string, len(decoded.Candidates))
					for i, c := range decoded.Candidates {
						reasons[i] = c.FinishReason
					}
					if !reflect.DeepEqual(result.ProviderMetadata["finishReasons"], reasons) {
						t.Fatalf("lost candidate reasons: %#v", result.ProviderMetadata)
					}
				}
				if decoded.PromptFeedback != nil && !reflect.DeepEqual(result.ProviderMetadata["promptFeedback"], decoded.PromptFeedback) {
					t.Fatal("lost prompt feedback")
				}
				if tc.name == "mixed safety" && (result.Images[0].Text != "first" || result.Images[1].Text != "second") {
					t.Fatal("candidate content reordered")
				}
			})
		}
	}
}
