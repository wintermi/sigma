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
	"testing"

	"github.com/wintermi/sigma"
)

// OpenAI-compatible servers such as vLLM and OpenRouter open a stream with
// {"role":"assistant","content":""}; that empty chunk must not start a block.
func TestCompletionsEmptyRoleContentDoesNotStartTextBlock(t *testing.T) {
	t.Parallel()

	const roleChunk = `{"id":"c","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`
	tests := []struct {
		name   string
		chunks []string
		want   []sigma.ContentBlockType
	}{
		{
			name: "tool-only turn",
			chunks: []string{
				roleChunk,
				`{"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":null}]}`,
				`{"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			},
			want: []sigma.ContentBlockType{sigma.ContentBlockToolCall},
		},
		{
			name: "reasoning before answer",
			chunks: []string{
				roleChunk,
				`{"id":"c","choices":[{"index":0,"delta":{"reasoning_content":"think"},"finish_reason":null}]}`,
				`{"id":"c","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":null}]}`,
				`{"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			},
			want: []sigma.ContentBlockType{sigma.ContentBlockThinking, sigma.ContentBlockText},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, chunk := range tt.chunks {
					_, _ = io.WriteString(w, "data: "+chunk+"\n\n")
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("completions-empty-content-test")
			model := openAITestModel(providerID)
			client := openAITestClient(t, providerID, model, server.URL)
			req := sigma.Request{
				Messages: []sigma.Message{sigma.UserText("hi")},
				Tools:    []sigma.Tool{{Name: "read", InputSchema: sigma.Schema{"type": "object"}}},
			}
			final, err := client.Complete(context.Background(), model, req)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			got := make([]sigma.ContentBlockType, 0, len(final.Content))
			for _, block := range final.Content {
				got = append(got, block.Type)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("content types = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("content types = %v, want %v", got, tt.want)
				}
			}

			history := append(req.Messages, sigma.Message{
				Role:       sigma.RoleAssistant,
				Content:    final.Content,
				Provider:   final.Provider,
				API:        model.API,
				Model:      final.Model,
				StopReason: final.StopReason,
			})
			if _, err := sigma.MarshalRequest(sigma.Request{Messages: history}); err != nil {
				t.Fatalf("persisting the turn failed: %v", err)
			}
		})
	}
}
