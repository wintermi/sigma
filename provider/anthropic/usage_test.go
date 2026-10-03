// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestUsageUpdatesPreserveRawFieldsAndTokenUnits(t *testing.T) {
	t.Parallel()
	p := streamParser{model: sigma.Model{ID: "claude-test", Provider: sigma.ProviderAnthropic}}
	merge := func(data string) {
		t.Helper()
		var event streamEvent
		if err := decodeStreamEvent(`{"type":"message_delta","usage":`+data+`}`, &event); err != nil {
			t.Fatal(err)
		}
		p.mergeUsage(event.Usage)
	}
	merge(`{"input_tokens":100,"service_tier":"standard","server_tool_use":{"web_search_requests":2},"future":{"retained":"yes","changed":1},"nullable":"before"}`)
	first := p.usage
	merge(`{"output_tokens":20}`)
	if p.usage.Total() != 120 || p.usage.ToolUseInputTokens != 0 {
		t.Fatalf("request counts affected tokens: %#v", p.usage)
	}
	wantRaw := map[string]any{
		"input_tokens": float64(100), "output_tokens": float64(20), "service_tier": "standard",
		"server_tool_use": map[string]any{"web_search_requests": float64(2)},
		"future":          map[string]any{"retained": "yes", "changed": float64(1)}, "nullable": "before",
	}
	if !reflect.DeepEqual(p.usage.Raw, wantRaw) {
		t.Fatalf("raw usage = %#v, want %#v", p.usage.Raw, wantRaw)
	}
	second := p.usage
	merge(`{"output_tokens":0,"service_tier":"","server_tool_use":{"web_search_requests":0,"web_fetch_requests":1},"future":{"changed":2},"nullable":null}`)
	if p.usage.OutputTokens != 0 || p.usage.Total() != 100 {
		t.Fatalf("zero update ignored: %#v", p.usage)
	}
	wantRaw["output_tokens"], wantRaw["service_tier"], wantRaw["nullable"] = float64(0), "", nil
	wantRaw["future"].(map[string]any)["changed"] = float64(2)
	wantRaw["server_tool_use"] = map[string]any{"web_search_requests": float64(0), "web_fetch_requests": float64(1)}
	if !reflect.DeepEqual(p.usage.Raw, wantRaw) {
		t.Fatalf("updated raw usage = %#v, want %#v", p.usage.Raw, wantRaw)
	}
	if _, exists := first.Raw["output_tokens"]; exists {
		t.Fatal("first snapshot acquired later output tokens")
	}
	if second.OutputTokens != 20 || second.Raw["future"].(map[string]any)["changed"] != float64(1) || second.Raw["server_tool_use"].(map[string]any)["web_search_requests"] != float64(2) {
		t.Fatal("later update mutated earlier snapshot")
	}
	merge(`{"server_tool_use":null,"future":false}`)
	if p.usage.Raw["server_tool_use"] != nil || p.usage.Raw["future"] != false {
		t.Fatalf("explicit replacements lost: %#v", p.usage.Raw)
	}
	merge(`{"server_tool_use":{"web_search_requests":3},"future":{"new":"value"}}`)
	if p.usage.Raw["server_tool_use"].(map[string]any)["web_search_requests"] != float64(3) {
		t.Fatal("request count was not replaced")
	}
	if !reflect.DeepEqual(p.usage.Raw["future"], map[string]any{"new": "value"}) {
		t.Fatal("object did not replace scalar")
	}
}

func TestUsageTerminalPreservesSparseUpdates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stream, writer := sigma.NewStream(ctx)
	defer stream.Close()
	go func() {
		defer writer.Close()
		final, err := parseMessagesStream(ctx, strings.NewReader(`data: {"type":"message_start","message":{"id":"usage-test","role":"assistant","content":[],"usage":{"input_tokens":100,"service_tier":"standard","server_tool_use":{"web_search_requests":2}}}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}

data: {"type":"message_stop"}

`), writer, sigma.Model{ID: "claude-test", Provider: sigma.ProviderAnthropic}, messagesCompat{}, nil, "")
		if err != nil {
			_ = writer.Error(ctx, err, final)
			return
		}
		_ = writer.Done(ctx, final)
	}()
	var events []sigma.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	final, ok := stream.Final()
	if !ok || final.Usage == nil {
		t.Fatal("missing final usage")
	}
	terminal := events[len(events)-1].Usage
	if terminal == nil || !reflect.DeepEqual(terminal, final.Usage) {
		t.Fatal("terminal usage differs from final")
	}
	if terminal.Total() != 120 || terminal.ToolUseInputTokens != 0 || terminal.Raw["input_tokens"] != float64(100) || terminal.Raw["service_tier"] != "standard" {
		t.Fatalf("terminal usage lost fields or mixed units: %#v", terminal)
	}
	if terminal.Raw["server_tool_use"].(map[string]any)["web_search_requests"] != float64(2) {
		t.Fatal("terminal usage lost request count")
	}
}
