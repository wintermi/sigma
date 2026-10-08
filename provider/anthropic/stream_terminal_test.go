// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"context"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

// parseTestMessagesStream parses an SSE body while draining its events.
func parseTestMessagesStream(t *testing.T, body string) (sigma.AssistantMessage, error) {
	t.Helper()
	ctx := context.Background()
	stream, writer := sigma.NewStream(ctx)
	defer stream.Close()
	type result struct {
		final sigma.AssistantMessage
		err   error
	}
	done := make(chan result, 1)
	go func() {
		defer writer.Close()
		final, err := parseMessagesStream(ctx, strings.NewReader(body), writer, sigma.Model{ID: "claude-test", Provider: sigma.ProviderAnthropic}, messagesCompat{}, nil, "")
		done <- result{final, err}
	}()
	for range stream.Events() {
	}
	got := <-done
	return got.final, got.err
}

func anthropicTestEvents(events ...string) string {
	var body strings.Builder
	for _, event := range events {
		body.WriteString("data: " + event + "\n\n")
	}
	return body.String()
}

const anthropicTestMessageStart = `{"type":"message_start","message":{"id":"msg_1","role":"assistant","content":[],"usage":{"input_tokens":1}}}`

// A server-side refusal fallback after output would merge two models' answers
// into one turn, so it must fail rather than complete.
func TestMessagesStreamRejectsMidOutputModelFallback(t *testing.T) {
	t.Parallel()

	_, err := parseTestMessagesStream(t, anthropicTestEvents(
		anthropicTestMessageStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"I can't help with"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"fallback"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Sure, here it is"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		`{"type":"message_stop"}`,
	))
	if err == nil || !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("error = %v, want mid-output fallback rejected", err)
	}
}

func TestMessagesStreamSkipsFallbackBeforeOutput(t *testing.T) {
	t.Parallel()

	final, err := parseTestMessagesStream(t, anthropicTestEvents(
		anthropicTestMessageStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"fallback"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`{"type":"message_stop"}`,
	))
	if err != nil {
		t.Fatalf("parse returned error: %v", err)
	}
	if len(final.Content) != 1 || final.Content[0].Text != "Answer" {
		t.Fatalf("content = %#v, want the single fallback-model answer", final.Content)
	}
}
