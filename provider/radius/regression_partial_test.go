// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package radius

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestPrematureEOFPreservesPartialContent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		frames []string
		check  func(sigma.ContentBlock) bool
	}{
		{"text", []string{`{"type":"text_start"}`, `{"type":"text_delta","delta":"partial"}`}, func(b sigma.ContentBlock) bool { return b.Text == "partial" }},
		{"thinking", []string{`{"type":"thinking_start"}`, `{"type":"thinking_delta","delta":"plan"}`}, func(b sigma.ContentBlock) bool { return b.ThinkingText == "plan" }},
		{"partial tool", []string{`{"type":"toolcall_start","id":"call1","toolName":"lookup"}`, `{"type":"toolcall_delta","delta":"{\"id\":9007199254740993}"}`}, func(b sigma.ContentBlock) bool {
			m, ok := b.ToolArguments.(map[string]any)
			return ok && m["id"] == json.Number("9007199254740993")
		}},
		{"structured tool", []string{`{"type":"toolcall_start","id":"call1","toolName":"lookup"}`, `{"type":"toolcall_end","toolCall":{"id":"call1","name":"lookup","arguments":{"id":9007199254740993}}}`}, func(b sigma.ContentBlock) bool {
			m, ok := b.ToolArguments.(map[string]any)
			return ok && m["id"] == json.Number("9007199254740993")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			for _, frame := range tc.frames {
				wire.WriteString("data: " + frame + "\n\n")
			}
			final, err := parseStream(ctx, strings.NewReader(wire.String()), writer, sigma.Model{Provider: sigma.ProviderRadius, ID: "test"})
			writer.Close()
			<-done
			if err == nil {
				t.Fatal("expected premature EOF")
			}
			classification := sigma.ClassifyError(&sigma.Error{Code: sigma.ErrorStream, Message: err.Error()})
			if !classification.RetryHint.Retryable || classification.Class != sigma.ErrorClassTransient {
				t.Fatalf("classification = %#v", classification)
			}
			if len(final.Content) != 1 || !tc.check(final.Content[0]) {
				t.Fatalf("lost partial: %#v", final.Content)
			}
		})
	}
}
