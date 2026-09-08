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

func TestStreamAlternativesRemainSeparate(t *testing.T) {
	t.Parallel()
	bad := `{"index":1,"content":{"parts":[{"text":"NO","thoughtSignature":"wrong"},{"functionCall":{"name":"wrong","args":{}}}]},"finishReason":"MAX_TOKENS"}`
	good := `{"content":{"parts":[{"text":"YES"}]},"finishReason":"STOP"}`
	for _, frames := range [][]string{{`{"candidates":[` + bad + `,` + good + `],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":2,"totalTokenCount":9}}`}, {`{"candidates":[` + bad + `]}`, `{"candidates":[` + good + `],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":2,"totalTokenCount":9}}`}} {
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
		final, err := parseGenerativeStream(ctx, strings.NewReader(wire.String()), writer, sigma.Model{Provider: sigma.ProviderGoogle, ID: "test"})
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
