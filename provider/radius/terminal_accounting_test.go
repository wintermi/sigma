// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package radius

import (
	"context"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func parseRadiusFrames(t *testing.T, model sigma.Model, frames ...string) sigma.AssistantMessage {
	t.Helper()
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
	final, err := parseStream(ctx, strings.NewReader(wire.String()), writer, model)
	writer.Close()
	<-done
	if err != nil {
		t.Fatalf("parseStream returned error: %v", err)
	}
	return final
}

func TestRadiusFinalCarriesCatalogCost(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderRadius, ID: "priced", InputCostPerMillion: 1, OutputCostPerMillion: 2}
	final := parseRadiusFrames(t, model,
		`{"type":"start"}`,
		`{"type":"done","reason":"stop","usage":{"input":1000000,"output":1000000,"totalTokens":2000000}}`,
	)
	if final.Cost == nil || final.Cost.TotalCost != 3 {
		t.Fatalf("cost = %#v, want total 3", final.Cost)
	}
}

// Gateway signatures belong to the backing model's replay contract, such as
// Gemini thought signatures on tool calls, and must survive a round trip.
func TestRadiusReplaysTextAndToolCallSignatures(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderRadius, ID: "test"}
	final := parseRadiusFrames(t, model,
		`{"type":"start"}`,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_end","contentIndex":0,"content":"hi","contentSignature":"msg_sig"}`,
		`{"type":"toolcall_start","contentIndex":1,"id":"c1","toolName":"t"}`,
		`{"type":"toolcall_end","contentIndex":1,"toolCall":{"id":"c1","name":"t","arguments":{},"thoughtSignature":"gemini_sig"}}`,
		`{"type":"done","reason":"toolUse"}`,
	)
	replayed, err := messagePayload(sigma.Message{Role: sigma.RoleAssistant, Content: final.Content, Provider: final.Provider, Model: final.Model})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := replayed.Content.([]map[string]any)
	if len(content) != 2 {
		t.Fatalf("replayed content = %#v, want text and tool call", replayed.Content)
	}
	if got := content[0]["textSignature"]; got != "msg_sig" {
		t.Fatalf("text replay = %#v, want textSignature msg_sig", content[0])
	}
	if got := content[1]["thoughtSignature"]; got != "gemini_sig" {
		t.Fatalf("tool call replay = %#v, want thoughtSignature gemini_sig", content[1])
	}
}
