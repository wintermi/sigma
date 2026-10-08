// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/wintermi/sigma"
)

// Thinking models such as Moonshot Kimi require their own reasoning back in the
// field it streamed in when a tool loop continues.
func TestCompletionsReplaysSameModelReasoningInItsSourceField(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"reasoning_content", "reasoning", "reasoning_text"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 2 {
					captureRequest(t, requests, r)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, chunk := range []string{
					`{"id":"c","choices":[{"index":0,"delta":{"` + field + `":"plan the call"},"finish_reason":null}]}`,
					`{"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":null}]}`,
					`{"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
				} {
					_, _ = io.WriteString(w, "data: "+chunk+"\n\n")
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("completions-reasoning-replay-test")
			model := openAITestModel(providerID)
			client := openAITestClient(t, providerID, model, server.URL)
			req := sigma.Request{
				Messages: []sigma.Message{sigma.UserText("hi")},
				Tools:    []sigma.Tool{{Name: "read", InputSchema: sigma.Schema{"type": "object"}}},
			}
			first, err := client.Complete(context.Background(), model, req)
			if err != nil {
				t.Fatalf("first Complete returned error: %v", err)
			}

			req.Messages = append(req.Messages,
				sigma.Message{Role: sigma.RoleAssistant, Content: first.Content, Provider: model.Provider, API: model.API, Model: model.ID, StopReason: first.StopReason},
				sigma.ToolResult("call_a", "ok"),
			)
			_, _ = client.Complete(context.Background(), model, req)

			messages := decodeResponsesPayload(t, receiveRequest(t, requests).Body)["messages"].([]any)
			assistant := messages[1].(map[string]any)
			if got := assistant[field]; got != "plan the call" {
				t.Fatalf("replayed assistant = %#v, want %s with the original reasoning", assistant, field)
			}
		})
	}
}
