// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/mistral"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/provider/radius"
)

const (
	replayNumbers        = `{"decimal":0.123456789012345678901,"exponent":1.234567890123456789e+42,"integer":9007199254740993}`
	replayAnthropicDone  = "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n"
	replayAnthropicStart = "data: {\"type\":\"message_start\",\"message\":{\"role\":\"assistant\",\"content\":[]}}\n\n"
)

type replayTransport struct {
	response string
	body     []byte
	calls    int
}

func (r *replayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var err error
	r.body, err = io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	r.calls++
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(r.response)), Request: req}, nil
}

func replayClient(t *testing.T, route string, response string) (*sigma.Client, sigma.Model, *replayTransport) {
	t.Helper()
	wire := &replayTransport{response: response}
	model := sigma.Model{Provider: sigma.ProviderID("replay-" + route), ID: "replay-model", SupportsTools: true, SupportsThinking: true}
	var provider sigma.TextProvider
	switch route {
	case "anthropic":
		model.API = sigma.APIAnthropicMessages
		provider = anthropic.NewProvider()
	case "vertex-anthropic":
		model.Provider = sigma.ProviderGoogleVertexAnthropic
		model.API = sigma.APIAnthropicMessages
		provider = anthropic.NewVertexProvider(anthropic.WithVertexConfig(anthropic.VertexConfig{ProjectID: "test", Location: "us-central1"}))
	case "google":
		model.API = sigma.APIGoogleGenerativeAI
		provider = google.NewProvider()
	case "vertex-google":
		model.Provider = sigma.ProviderGoogleVertex
		model.API = sigma.APIGoogleVertex
		provider = google.NewVertexProvider(google.WithVertexConfig(google.VertexConfig{ProjectID: "test", Location: "us-central1"}))
	case "openai":
		model.API = sigma.APIOpenAICompletions
		provider = openai.NewProvider()
	case "responses":
		model.API = sigma.APIOpenAIResponses
		provider = openai.NewResponsesProvider()
	case "mistral":
		model.API = sigma.APIMistralConversations
		provider = mistral.NewProvider()
	case "radius":
		model.API = sigma.APIRadiusMessages
		provider = radius.NewProvider(radius.WithGatewayURL("https://radius.test"))
	default:
		t.Fatalf("unknown route %q", route)
	}
	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(model.Provider, provider); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatal(err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry), sigma.WithHTTPClient(&http.Client{Transport: wire}), sigma.WithAuthResolver(sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "synthetic-key"}, nil
	}))), model, wire
}

func replayHistory(final sigma.AssistantMessage, model sigma.Model) sigma.Message {
	return sigma.Message{Role: sigma.RoleAssistant, Provider: final.Provider, API: model.API, Model: final.Model, StopReason: final.StopReason, ProviderThinkingLevel: final.ProviderThinkingLevel, Content: final.Content, Usage: final.Usage}
}

func persistReplay(t *testing.T, req sigma.Request) sigma.Request {
	t.Helper()
	data, err := sigma.MarshalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := sigma.UnmarshalRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	return restored
}

func replayBlock(index int, block string) string {
	return fmt.Sprintf("data: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":%s}\n\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", index, block, index)
}

func TestProviderNumericPersistenceReplay(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"anthropic", "vertex-anthropic", "google", "vertex-google", "openai"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			var response string
			switch route {
			case "anthropic", "vertex-anthropic":
				response = replayAnthropicStart + replayBlock(0, `{"type":"tool_use","id":"precise_call","name":"lookup","input":`+replayNumbers+`}`) + replayAnthropicDone
			case "google", "vertex-google":
				response = `data: {"candidates":[{"content":{"parts":[{"functionCall":{"id":"precise_call","name":"lookup","args":` + replayNumbers + `}}]},"finishReason":"STOP"}]}` + "\n\n"
			case "openai":
				encoded, _ := json.Marshal(replayNumbers)
				response = `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"precise_call","type":"function","function":{"name":"lookup","arguments":` + string(encoded) + `}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"
			}
			client, model, wire := replayClient(t, route, response)
			req := sigma.Request{Messages: []sigma.Message{sigma.UserText("lookup")}}
			final, err := client.Complete(context.Background(), model, req)
			if err != nil {
				t.Fatal(err)
			}
			if len(final.Content) != 1 {
				t.Fatalf("final = %#v", final)
			}
			req.Messages = append(req.Messages, replayHistory(final, model), sigma.ToolResult("precise_call", "ok"), sigma.UserText("continue"))
			restored := persistReplay(t, req)
			if _, err := client.Complete(context.Background(), model, restored); err != nil {
				t.Fatal(err)
			}
			var encoded string
			if route == "openai" {
				value, _ := json.Marshal(replayNumbers)
				encoded = string(value)
			} else {
				encoded = replayNumbers
			}
			if !strings.Contains(string(wire.body), encoded) {
				t.Fatalf("outgoing arguments lost precision: %s", wire.body)
			}
		})
	}
}

func TestAnthropicHostedPersistenceReplay(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"anthropic", "vertex-anthropic"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			blocks := []string{
				`{"type":"server_tool_use","id":"server:one","name":"web_search","input":` + replayNumbers + `,"caller":{"type":"direct"},"opaque":"keep","empty_object":{}}`,
				`{"type":"text","text":""}`,
				`{"type":"web_search_tool_result","tool_use_id":"server:one","content":[{"encrypted_content":"opaque-search","future":` + replayNumbers + `}]}`,
				`{"type":"web_fetch_tool_result","tool_use_id":"server:one","content":[],"future":true,"empty_object":{}}`,
				`{"type":"text","text":" Between results \n"}`,
				`{"type":"server_tool_use","id":"server:two","name":"code_execution","input":{}}`,
				`{"type":"code_execution_tool_result","tool_use_id":"server:two","content":{"type":"code_execution_tool_result_error","error_code":"unavailable"}}`,
				`{"type":"bash_code_execution_tool_result","tool_use_id":"server:two","content":{"encrypted_content":"opaque-code","stdout":""}}`,
				`{"type":"text_editor_code_execution_tool_result","tool_use_id":"server:two","content":[],"unknown":{"version":9007199254740993}}`,
				`{"type":"text","text":"Finished"}`,
			}
			response := replayAnthropicStart
			for i, block := range blocks {
				if i == 0 {
					initial := strings.Replace(block, replayNumbers, "{}", 1)
					delta, err := json.Marshal(replayNumbers)
					if err != nil {
						t.Fatal(err)
					}
					response += fmt.Sprintf("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":%s}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n", initial, delta)
					continue
				}
				response += replayBlock(i, block)
			}
			response += replayAnthropicDone
			client, model, wire := replayClient(t, route, response)
			req := sigma.Request{Messages: []sigma.Message{sigma.UserText("search")}}
			final, err := client.Complete(context.Background(), model, req)
			if err != nil {
				t.Fatal(err)
			}
			if len(final.Content) != 5 {
				t.Fatalf("represented content = %#v", final.Content)
			}
			original, _ := json.Marshal(final)
			cloned := final.Content[0].Clone()
			cloned.ProviderMetadata["anthropic_hosted_replay"].(map[string]any)["server_tool_use"].(map[string]any)["opaque"] = "changed"
			unchanged, _ := json.Marshal(final)
			if string(original) != string(unchanged) {
				t.Fatal("cloning final aliased hosted metadata")
			}
			req.Messages = append(req.Messages, replayHistory(final, model), sigma.UserText("continue"))
			restored := persistReplay(t, req)
			before, _ := json.Marshal(restored)
			clone := restored.Messages[1].Content[1].Clone()
			clone.ProviderMetadata["anthropic_hosted_replay"].(map[string]any)["results_after"].([]any)[0].(map[string]any)["content"] = "mutated"
			handed, err := sigma.TransformRequestForModel(model, restored)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Complete(context.Background(), model, handed.Request); err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(restored)
			if string(before) != string(after) {
				t.Fatal("replay mutated caller history")
			}
			var payload struct {
				Messages []struct {
					Role    string
					Content []json.RawMessage
				}
			}
			if err := json.Unmarshal(wire.body, &payload); err != nil {
				t.Fatal(err)
			}
			var got []json.RawMessage
			for _, msg := range payload.Messages {
				if msg.Role == "assistant" {
					got = msg.Content
				}
			}
			expected := append([]string{blocks[0]}, blocks[2:]...)
			if len(got) != len(expected) {
				t.Fatalf("replay = %s", wire.body)
			}
			for i := range got {
				var a, b any
				da := json.NewDecoder(strings.NewReader(string(got[i])))
				da.UseNumber()
				if err := da.Decode(&a); err != nil {
					t.Fatal(err)
				}
				db := json.NewDecoder(strings.NewReader(expected[i]))
				db.UseNumber()
				if err := db.Decode(&b); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(a, b) {
					t.Fatalf("block %d = %s, want %s", i, got[i], expected[i])
				}
			}
			for _, mismatch := range []string{"provider", "api", "model", "missing", "envelope"} {
				t.Run(mismatch, func(t *testing.T) {
					incompatible := persistReplay(t, req)
					msg := &incompatible.Messages[1]
					switch mismatch {
					case "provider":
						msg.Provider = "other"
					case "api":
						msg.API = sigma.APIOpenAICompletions
					case "model":
						msg.Model = "other"
					case "missing":
						msg.Provider = ""
						msg.API = ""
						msg.Model = ""
					case "envelope":
						msg.Content[1].ProviderMetadata["anthropic_hosted_replay"].(map[string]any)["model"] = "other"
					}
					if _, err := client.Complete(context.Background(), model, incompatible); err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(wire.body), "server_tool_use") || strings.Contains(string(wire.body), "opaque-search") {
						t.Fatalf("incompatible replay = %s", wire.body)
					}
					if !strings.Contains(string(wire.body), "Finished") {
						t.Fatal("visible text lost")
					}
				})
			}
			malformed := persistReplay(t, req)
			malformed.Messages[1].Content[1].ProviderMetadata["anthropic_hosted_replay"].(map[string]any)["results_after"].([]any)[0].(map[string]any)["tool_use_id"] = "missing"
			calls := wire.calls
			_, err = client.Complete(context.Background(), model, malformed)
			var local *sigma.Error
			if !errors.As(err, &local) || local.Code != sigma.ErrorInvalidRequest || wire.calls != calls {
				t.Fatalf("invalid replay = %v, calls %d -> %d", err, calls, wire.calls)
			}
		})
	}
}

func TestAnthropicOrphanHostedResultIsProviderError(t *testing.T) {
	t.Parallel()
	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprint(initial), func(t *testing.T) {
			t.Parallel()
			result := `{"type":"web_search_tool_result","tool_use_id":"absent","content":[]}`
			response := replayAnthropicStart + replayBlock(0, result) + replayAnthropicDone
			if initial {
				response = `data: {"type":"message_start","message":{"content":[` + result + `]}}` + "\n\n" + replayAnthropicDone
			}
			client, model, _ := replayClient(t, "anthropic", response)
			_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("search")}})
			var provider *sigma.ProviderError
			if !errors.As(err, &provider) || !errors.Is(err, sigma.ErrProviderResponse) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestInterruptedHistoryProviderDispatch(t *testing.T) {
	t.Parallel()
	response := replayAnthropicStart + replayBlock(0, `{"type":"text","text":" visible partial \n"}`) + replayBlock(1, `{"type":"thinking","thinking":"opaque-plan","signature":"signed"}`) + `data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"failed_call","name":"lookup","input":{}}}` + "\n\n" + `data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}` + "\n\n"
	source, sourceModel, _ := replayClient(t, "anthropic", response)
	final, err := source.Complete(context.Background(), sourceModel, sigma.Request{Messages: []sigma.Message{sigma.UserText("lookup")}})
	if err == nil || final.StopReason != sigma.StopReasonError || len(final.Content) != 3 {
		t.Fatalf("truncated final = %#v, error %v", final, err)
	}
	for _, route := range []string{"anthropic", "vertex-anthropic", "google", "vertex-google", "openai", "responses", "mistral", "radius"} {
		for _, handoff := range []bool{false, true} {
			for _, stop := range []sigma.StopReason{sigma.StopReasonError, sigma.StopReasonAborted} {
				t.Run(fmt.Sprintf("%s/%v/%s", route, handoff, stop), func(t *testing.T) {
					t.Parallel()
					client, model, wire := replayClient(t, route, replayAnthropicStart+replayAnthropicDone)
					failed := replayHistory(final, sourceModel)
					failed.StopReason = stop
					result := sigma.ToolResult("failed_call", "discarded-result")
					result.AddedToolNames = []string{"late-tool"}
					req := persistReplay(t, sigma.Request{Messages: []sigma.Message{sigma.UserText("lookup"), failed, result, sigma.UserText("continue")}})
					before, _ := json.Marshal(req)
					dispatch := req
					if handoff {
						transformed, err := sigma.TransformRequestForModel(model, req)
						if err != nil {
							t.Fatal(err)
						}
						dispatch = transformed.Request
					}
					// Response parsing is irrelevant here; every route must prepare the request
					// before sending it to the synthetic transport.
					_, _ = client.Complete(context.Background(), model, dispatch)
					if wire.calls != 1 {
						t.Fatalf("dispatch calls = %d", wire.calls)
					}
					for _, invalid := range []string{"failed_call", "discarded-result", "opaque-plan", "No result provided", "late-tool"} {
						if strings.Contains(string(wire.body), invalid) {
							t.Fatalf("replayed %q: %s", invalid, wire.body)
						}
					}
					if strings.Contains(string(wire.body), "visible partial") != (route != "responses") {
						t.Fatalf("visible partial policy: %s", wire.body)
					}
					after, _ := json.Marshal(req)
					if string(before) != string(after) {
						t.Fatal("caller history changed")
					}
				})
			}
		}
	}
}

func TestGoogleTerminalContracts(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"google", "vertex-google"} {
		for _, tc := range []struct {
			name, response     string
			stop               sigma.StopReason
			failure, transient bool
		}{
			{name: "prompt block", response: `"promptFeedback":{"blockReason":"SAFETY","extra":"retained"}`, stop: sigma.StopReasonContentFilter},
			{name: "blank feedback", response: `"promptFeedback":{"blockReason":" "}`, stop: sigma.StopReasonError, failure: true, transient: true},
			{name: "unspecified", response: `"promptFeedback":{"blockReason":"BLOCK_REASON_UNSPECIFIED"}`, stop: sigma.StopReasonError, failure: true, transient: true},
			{name: "candidate filter", response: `"candidates":[{"finishReason":"SAFETY"}]`, stop: sigma.StopReasonContentFilter},
			{name: "malformed", response: `"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"MALFORMED_FUNCTION_CALL"}]`, stop: sigma.StopReasonError, failure: true},
			{name: "unexpected", response: `"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"UNEXPECTED_TOOL_CALL"}]`, stop: sigma.StopReasonError, failure: true},
			{name: "premature EOF", response: `"candidates":[{"content":{"parts":[{"text":"partial"}]}}]`, stop: sigma.StopReasonError, failure: true, transient: true},
			{name: "max tokens", response: `"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"MAX_TOKENS"}]`, stop: sigma.StopReasonMaxTokens},
			{name: "unknown", response: `"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"FUTURE"}]`, stop: sigma.StopReasonUnknown},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				response := "data: {" + tc.response + `,"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}` + "\n\n"
				client, model, wire := replayClient(t, route, response)
				req := sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}
				check := func(final sigma.AssistantMessage, err error) {
					t.Helper()
					if (err != nil) != tc.failure || final.StopReason != tc.stop {
						t.Fatalf("stop %s, error %v", final.StopReason, err)
					}
					if final.Usage == nil || final.Usage.TotalTokens != 7 {
						t.Fatalf("usage = %#v", final.Usage)
					}
					if strings.Contains(tc.response, "partial") && (len(final.Content) != 1 || final.Content[0].Text != "partial") {
						t.Fatalf("partial = %#v", final.Content)
					}
					if tc.name == "prompt block" && final.ProviderMetadata["promptFeedback"] == nil {
						t.Fatal("lost feedback")
					}
					if tc.failure {
						class := sigma.ClassifyError(err)
						if class.RetryHint.Retryable != tc.transient {
							t.Fatalf("classification = %#v", class)
						}
						if !tc.transient {
							var provider *sigma.ProviderError
							if !errors.As(err, &provider) || !errors.Is(err, sigma.ErrProviderResponse) || class.Class != sigma.ErrorClassProvider {
								t.Fatalf("error = %v, classification %#v", err, class)
							}
							if final.ProviderMetadata["finishReason"] == nil {
								t.Fatal("lost raw finish reason")
							}
						}
					}
				}
				final, err := client.Complete(context.Background(), model, req)
				check(final, err)
				_, err = client.CompleteText(context.Background(), model, "test")
				if (err != nil) != tc.failure {
					t.Fatalf("CompleteText error = %v", err)
				}
				stream := client.Stream(context.Background(), model, req)
				var terminal sigma.Event
				for event := range stream.Events() {
					terminal = event
				}
				final, ok := stream.Final()
				if !ok {
					t.Fatal("missing final")
				}
				check(final, stream.Err())
				kind := sigma.EventKindDone
				if tc.failure {
					kind = sigma.EventKindError
				}
				if terminal.Kind != kind || terminal.FinalMessage == nil || terminal.StopReason != tc.stop {
					t.Fatalf("terminal = %#v", terminal)
				}
				if wire.calls != 3 {
					t.Fatalf("unexpected retry: calls = %d", wire.calls)
				}
			})
		}
	}
}

func TestAnthropicBlankHistoryAndHandoff(t *testing.T) {
	t.Parallel()
	client, model, wire := replayClient(t, "anthropic", replayAnthropicStart+replayAnthropicDone)
	source, sourceModel, _ := replayClient(t, "google", `data: {"candidates":[{"content":{"parts":[{"text":"","thoughtSignature":"opaque"}]},"finishReason":"STOP"}]}`+"\n\n")
	final, err := source.Complete(context.Background(), sourceModel, sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Content) != 1 || final.Content[0].Text != "" || final.Content[0].ProviderSignature == "" {
		t.Fatalf("signature-only final = %#v", final)
	}
	signatureOnly := replayHistory(final, sourceModel)
	signed := sigma.Thinking("", "signature")
	redacted := sigma.Thinking("", "")
	redacted.Redacted = true
	redacted.ProviderSignature = "encrypted"
	req := persistReplay(t, sigma.Request{Messages: []sigma.Message{
		sigma.UserText(" \n\t"), signatureOnly,
		{Role: sigma.RoleUser, Content: []sigma.ContentBlock{sigma.Text(" "), sigma.Text(" untrimmed \n"), sigma.Text("\t")}},
		{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, Content: []sigma.ContentBlock{sigma.Text(" "), signed, redacted, sigma.ToolCallBlock("empty", "lookup", map[string]any{})}},
		{Role: sigma.RoleTool, ToolCallID: "empty"},
	}})
	handed, err := sigma.TransformRequestForModel(model, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), model, handed.Request); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Messages []struct{ Content []map[string]any }
	}
	if err := json.Unmarshal(wire.body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Messages) != 3 {
		t.Fatalf("messages = %s", wire.body)
	}
	first := payload.Messages[0].Content
	if len(first) != 1 || first[0]["text"] != " untrimmed \n" {
		t.Fatalf("text = %#v", first)
	}
	assistant := payload.Messages[1].Content
	if len(assistant) != 3 || assistant[0]["type"] != "thinking" || assistant[0]["thinking"] != "" || assistant[0]["signature"] != "signature" || assistant[1]["type"] != "redacted_thinking" {
		t.Fatalf("thinking = %#v", assistant)
	}
	if content, ok := payload.Messages[2].Content[0]["content"].([]any); !ok || len(content) != 0 {
		t.Fatal("lost empty tool result")
	}
	calls := wire.calls
	_, err = client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText(" \t"), signatureOnly}})
	var invalid *sigma.Error
	if !errors.As(err, &invalid) || invalid.Code != sigma.ErrorInvalidRequest || wire.calls != calls {
		t.Fatalf("all-empty error %v, calls %d", err, wire.calls)
	}
}
