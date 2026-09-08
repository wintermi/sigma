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

func TestStreamAlternativesRemainSeparate(t *testing.T) {
	t.Parallel()
	bad := `{"index":1,"delta":{"content":"NO","tool_calls":[{"index":0,"id":"wrong","function":{"name":"wrong","arguments":"{}"}}]},"finish_reason":"length"}`
	good := `{"delta":{"content":"YES"},"finish_reason":"stop"}`
	for _, frames := range [][]string{{`{"choices":[` + bad + `,` + good + `],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}`}, {`{"choices":[` + bad + `]}`, `{"choices":[` + good + `],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}`}} {
		ctx := context.Background()
		stream, writer := sigma.NewStream(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range stream.Events() {
			}
		}()
		var wire strings.Builder
		for _, frame := range frames {
			wire.WriteString("data: " + frame + "\n\n")
		}
		final, err := parseCompletionsStream(ctx, strings.NewReader(wire.String()), writer, sigma.Model{Provider: sigma.ProviderOpenAI, ID: "test"}, nil, true)
		writer.Close()
		<-done
		if err != nil {
			t.Fatal(err)
		}
		if len(final.Content) != 1 || final.Content[0].Text != "YES" || final.Content[0].ProviderSignature != "" || final.StopReason != sigma.StopReasonEndTurn {
			t.Fatalf("alternatives merged: %#v", final)
		}
		if final.Usage == nil || final.Usage.InputTokens != 7 || final.Usage.OutputTokens != 2 {
			t.Fatalf("response usage lost: %#v", final.Usage)
		}
	}
}
