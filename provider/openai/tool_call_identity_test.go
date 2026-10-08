// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestStreamingToolCallIdentityAcrossDeltas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		frames []string
		want   []string // tool call IDs, or names when IDs are synthesized
		byName bool
	}{
		{
			name: "index-less named calls stay separate",
			frames: []string{
				`{"choices":[{"delta":{"tool_calls":[{"function":{"name":"read","arguments":"{\"path\":\"a\"}"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"function":{"name":"write","arguments":"{\"path\":\"b\"}"}}]}}]}`,
			},
			want:   []string{"read", "write"},
			byName: true,
		},
		{
			name: "id-only continuation joins its indexed call",
			frames: []string{
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"read","arguments":"{\"pa"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"id":"call_a","function":{"arguments":"th\":\"a\"}"}}]}}]}`,
			},
			want: []string{"call_a"},
		},
		{
			name: "index continuation joins its id-only call",
			frames: []string{
				`{"choices":[{"delta":{"tool_calls":[{"id":"call_a","function":{"name":"read","arguments":"{\"pa"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"arguments":"th\":\"a\"}"}}]}}]}`,
			},
			want: []string{"call_a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			stream, writer := sigma.NewStream(ctx)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for range stream.Events() {
				}
			}()
			var wire strings.Builder
			for _, frame := range tt.frames {
				wire.WriteString("data: " + frame + "\n\n")
			}
			wire.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
			wire.WriteString("data: [DONE]\n\n")
			final, err := parseCompletionsStream(ctx, strings.NewReader(wire.String()), writer, sigma.Model{Provider: sigma.ProviderOpenAI, ID: "test"}, nil, true)
			writer.Close()
			<-done
			if err != nil {
				t.Fatalf("parseCompletionsStream returned error: %v", err)
			}
			var got []string
			for _, block := range final.Content {
				if block.Type != sigma.ContentBlockToolCall {
					continue
				}
				if args, _ := block.ToolArguments.(map[string]any); args["path"] == nil {
					t.Fatalf("tool call %q arguments = %#v, want complete path", block.ToolName, block.ToolArguments)
				}
				if tt.byName {
					got = append(got, block.ToolName)
				} else {
					got = append(got, block.ToolCallID)
				}
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("tool calls = %v, want %v", got, tt.want)
			}
		})
	}
}
