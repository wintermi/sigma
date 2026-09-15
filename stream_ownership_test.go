// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wintermi/sigma"
)

func TestToolEventsDoNotAliasProviderOrAccumulator(t *testing.T) {
	t.Parallel()
	for _, aborted := range []bool{false, true} {
		name := "success"
		if aborted {
			name = "aborted"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, writer := sigma.NewStream(ctx)
			defer stream.Close()
			arguments := map[string]any{
				"nested": map[string]string{"city": "Melbourne"},
				"number": json.Number("9007199254740993"),
				"empty":  map[string]any{},
			}
			metadata := map[string]any{"nested": []map[string]string{{"source": "provider"}}}
			partial := &sigma.PartialToolCall{ID: "call_1", Name: "lookup", ProviderMetadata: map[string]any{
				"arguments": arguments, "nested": metadata["nested"],
			}}
			call := &sigma.ToolCall{ID: "call_1", Name: "lookup", Arguments: arguments, ProviderMetadata: metadata}
			advance := make(chan struct{})
			finished := make(chan error, 1)
			go func() {
				for _, event := range []sigma.Event{
					{Kind: sigma.EventKindToolCallDelta, PartialToolCall: partial},
					{Kind: sigma.EventKindToolCallDelta, PartialToolCall: partial},
					{Kind: sigma.EventKindToolCallEnd, ToolCall: call},
					{Kind: sigma.EventKindToolCallEnd, ToolCall: call},
				} {
					if err := writer.Emit(ctx, event); err != nil {
						finished <- err
						return
					}
					select {
					case <-advance:
					case <-ctx.Done():
						finished <- ctx.Err()
						return
					}
				}
				if aborted {
					cancel()
					finished <- nil
					return
				}
				block := sigma.ToolCallBlock(call.ID, call.Name, arguments)
				block.ProviderMetadata = metadata
				finished <- writer.Done(ctx, sigma.AssistantMessage{Content: []sigma.ContentBlock{block}, StopReason: sigma.StopReasonToolCalls})
			}()
			count := 0
			for event := range stream.Events() {
				var args any
				var meta map[string]any
				if event.PartialToolCall != nil {
					args = event.PartialToolCall.ProviderMetadata["arguments"]
					meta = event.PartialToolCall.ProviderMetadata
				} else if event.ToolCall != nil {
					args = event.ToolCall.Arguments
					meta = event.ToolCall.ProviderMetadata
				} else {
					continue
				}
				assertOriginalToolArguments(t, args)
				if got := meta["nested"].([]map[string]string)[0]["source"]; got != "provider" {
					t.Errorf("later event metadata = %q, want provider", got)
				}
				args.(map[string]any)["nested"].(map[string]string)["city"] = "corrupted"
				meta["nested"].([]map[string]string)[0]["source"] = "corrupted"
				count++
				advance <- struct{}{}
			}
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			if count != 4 {
				t.Fatalf("tool event count = %d, want 4", count)
			}
			if aborted != errors.Is(stream.Err(), context.Canceled) {
				t.Fatalf("stream error = %v, aborted=%v", stream.Err(), aborted)
			}
			final, ok := stream.Final()
			if !ok || len(final.Content) != 1 {
				t.Fatalf("final = %#v, exists=%v", final, ok)
			}
			assertOriginalToolArguments(t, final.Content[0].ToolArguments)
			if !aborted && final.Content[0].ProviderMetadata["nested"].([]map[string]string)[0]["source"] != "provider" {
				t.Error("final metadata was mutated by event consumer")
			}
		})
	}
}

func assertOriginalToolArguments(t *testing.T, args any) {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"empty":{},"nested":{"city":"Melbourne"},"number":9007199254740993}`; got != want {
		t.Errorf("arguments = %s, want %s", got, want)
	}
}
