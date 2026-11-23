// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"context"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestGoogleStreamDropsUnsignedEmptyText(t *testing.T) {
	t.Parallel()

	call := func(id string) string {
		return `{"functionCall":{"name":"list_customers","args":{},"id":"` + id + `"},"thoughtSignature":"Zmlyc3Q="}`
	}
	chunk := func(parts string, finish string) string {
		finishReason := ""
		if finish != "" {
			finishReason = `,"finishReason":"` + finish + `"`
		}
		return `data: {"candidates":[{"content":{"role":"model","parts":[` + parts + `]}` + finishReason + `}]}` + "\n\n"
	}

	tests := []struct {
		name  string
		body  string
		stop  sigma.StopReason
		kinds []sigma.ContentBlockType
		text  string
	}{
		{
			// gemini-4-neon on Vertex ends a function call turn with an unsigned empty text part.
			name:  "trailing empty text after tool call",
			body:  chunk(call("call_1"), "") + chunk(`{"text":""}`, "STOP"),
			stop:  sigma.StopReasonToolCalls,
			kinds: []sigma.ContentBlockType{sigma.ContentBlockToolCall},
		},
		{
			name:  "empty text before later text",
			body:  chunk(`{"text":""}`, "") + chunk(`{"text":"hello"}`, "STOP"),
			stop:  sigma.StopReasonEndTurn,
			kinds: []sigma.ContentBlockType{sigma.ContentBlockText},
			text:  "hello",
		},
		{
			name:  "empty text between tool calls",
			body:  chunk(call("call_1"), "") + chunk(`{"text":""}`, "") + chunk(call("call_2"), "STOP"),
			stop:  sigma.StopReasonToolCalls,
			kinds: []sigma.ContentBlockType{sigma.ContentBlockToolCall, sigma.ContentBlockToolCall},
		},
	}

	for _, api := range []sigma.API{sigma.APIGoogleGenerativeAI, sigma.APIGoogleVertex} {
		for _, tt := range tests {
			t.Run(string(api)+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				model := sigma.Model{ID: "test", Provider: sigma.ProviderGoogle, API: api}
				if api == sigma.APIGoogleVertex {
					model.Provider = sigma.ProviderGoogleVertex
				}
				ctx := context.Background()
				stream, writer := sigma.NewStream(ctx)
				var events []sigma.Event
				done := make(chan struct{})
				go func() {
					defer close(done)
					for event := range stream.Events() {
						events = append(events, event)
					}
				}()
				final, err := parseGenerativeStream(ctx, strings.NewReader(tt.body), writer, model)
				writer.Close()
				<-done
				if err != nil {
					t.Fatal(err)
				}

				if final.StopReason != tt.stop {
					t.Fatalf("stop reason = %q, want %q", final.StopReason, tt.stop)
				}
				if len(final.Content) != len(tt.kinds) {
					t.Fatalf("content = %+v, want kinds %v", final.Content, tt.kinds)
				}
				for index, kind := range tt.kinds {
					if final.Content[index].Type != kind {
						t.Fatalf("content[%d] type = %q, want %q", index, final.Content[index].Type, kind)
					}
				}
				if tt.text != "" && final.Content[0].Text != tt.text {
					t.Fatalf("text = %q, want %q", final.Content[0].Text, tt.text)
				}

				textStarts := 0
				for _, event := range events {
					if event.ContentIndex != nil && *event.ContentIndex >= len(final.Content) {
						t.Fatalf("event %q has content index %d beyond %d blocks", event.Kind, *event.ContentIndex, len(final.Content))
					}
					if event.Kind == sigma.EventKindTextStart {
						textStarts++
					}
				}
				wantTextStarts := 0
				if tt.text != "" {
					wantTextStarts = 1
				}
				if textStarts != wantTextStarts {
					t.Fatalf("text_start events = %d, want %d", textStarts, wantTextStarts)
				}
			})
		}
	}
}
