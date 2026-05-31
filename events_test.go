// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"encoding/json"
	"testing"

	"github.com/wintermi/sigma"
)

func TestEventKindStringValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind sigma.EventKind
		want string
	}{
		{kind: sigma.EventKindStart, want: "start"},
		{kind: sigma.EventKindTextStart, want: "text_start"},
		{kind: sigma.EventKindTextDelta, want: "text_delta"},
		{kind: sigma.EventKindTextEnd, want: "text_end"},
		{kind: sigma.EventKindThinkingStart, want: "thinking_start"},
		{kind: sigma.EventKindThinkingDelta, want: "thinking_delta"},
		{kind: sigma.EventKindThinkingEnd, want: "thinking_end"},
		{kind: sigma.EventKindToolCallStart, want: "toolcall_start"},
		{kind: sigma.EventKindToolCallDelta, want: "toolcall_delta"},
		{kind: sigma.EventKindToolCallEnd, want: "toolcall_end"},
		{kind: sigma.EventKindImageStart, want: "image_start"},
		{kind: sigma.EventKindImageDelta, want: "image_delta"},
		{kind: sigma.EventKindImageEnd, want: "image_end"},
		{kind: sigma.EventKindDone, want: "done"},
		{kind: sigma.EventKindError, want: "error"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := tt.kind.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}

			encoded, err := json.Marshal(tt.kind)
			if err != nil {
				t.Fatalf("marshal kind: %v", err)
			}
			if got, want := string(encoded), `"`+tt.want+`"`; got != want {
				t.Fatalf("marshal kind = %s, want %s", got, want)
			}
		})
	}
}

func TestEventTerminalDetection(t *testing.T) {
	t.Parallel()

	terminalKinds := map[sigma.EventKind]bool{
		sigma.EventKindDone:  true,
		sigma.EventKindError: true,
	}

	for _, kind := range []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindTextStart,
		sigma.EventKindTextDelta,
		sigma.EventKindTextEnd,
		sigma.EventKindThinkingStart,
		sigma.EventKindThinkingDelta,
		sigma.EventKindThinkingEnd,
		sigma.EventKindToolCallStart,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallEnd,
		sigma.EventKindImageStart,
		sigma.EventKindImageDelta,
		sigma.EventKindImageEnd,
		sigma.EventKindDone,
		sigma.EventKindError,
	} {
		kind := kind
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()

			want := terminalKinds[kind]
			if got := kind.IsTerminal(); got != want {
				t.Fatalf("kind IsTerminal() = %v, want %v", got, want)
			}
			if got := (sigma.Event{Kind: kind}).IsTerminal(); got != want {
				t.Fatalf("event IsTerminal() = %v, want %v", got, want)
			}
		})
	}
}

func TestEventKindHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		event     sigma.Event
		wantStart bool
		wantDelta bool
	}{
		{name: "stream start", event: sigma.Event{Kind: sigma.EventKindStart}, wantStart: true},
		{name: "text start", event: sigma.Event{Kind: sigma.EventKindTextStart}, wantStart: true},
		{name: "thinking start", event: sigma.Event{Kind: sigma.EventKindThinkingStart}, wantStart: true},
		{name: "tool call start", event: sigma.Event{Kind: sigma.EventKindToolCallStart}, wantStart: true},
		{name: "image start", event: sigma.Event{Kind: sigma.EventKindImageStart}, wantStart: true},
		{name: "text delta", event: sigma.Event{Kind: sigma.EventKindTextDelta}, wantDelta: true},
		{name: "thinking delta", event: sigma.Event{Kind: sigma.EventKindThinkingDelta}, wantDelta: true},
		{name: "tool call delta", event: sigma.Event{Kind: sigma.EventKindToolCallDelta}, wantDelta: true},
		{name: "image delta", event: sigma.Event{Kind: sigma.EventKindImageDelta}, wantDelta: true},
		{name: "done", event: sigma.Event{Kind: sigma.EventKindDone}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.event.IsStart(); got != tt.wantStart {
				t.Fatalf("IsStart() = %v, want %v", got, tt.wantStart)
			}
			if got := tt.event.IsDelta(); got != tt.wantDelta {
				t.Fatalf("IsDelta() = %v, want %v", got, tt.wantDelta)
			}
		})
	}
}

func TestEventJSONSerialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event sigma.Event
		want  string
	}{
		{
			name: "text delta",
			event: sigma.Event{
				Kind:         sigma.EventKindTextDelta,
				ContentIndex: intPtr(0),
				DeltaText:    "Hel",
				PartialMessage: &sigma.AssistantMessage{
					Content: []sigma.ContentBlock{sigma.Text("Hel")},
				},
			},
			want: `{
				"kind": "text_delta",
				"contentIndex": 0,
				"deltaText": "Hel",
				"partialMessage": {
					"content": [{"type": "text", "text": "Hel"}]
				}
			}`,
		},
		{
			name: "thinking end",
			event: sigma.Event{
				Kind:         sigma.EventKindThinkingEnd,
				ContentIndex: intPtr(1),
				Thinking:     "Checked the constraints.",
			},
			want: `{
				"kind": "thinking_end",
				"contentIndex": 1,
				"thinking": "Checked the constraints."
			}`,
		},
		{
			name: "tool call delta",
			event: sigma.Event{
				Kind:         sigma.EventKindToolCallDelta,
				ContentIndex: intPtr(2),
				PartialToolCall: &sigma.PartialToolCall{
					ID:             "call_1",
					Name:           "lookup",
					ArgumentsDelta: "{\"city\"",
				},
			},
			want: `{
				"kind": "toolcall_delta",
				"contentIndex": 2,
				"partialToolCall": {
					"id": "call_1",
					"name": "lookup",
					"argumentsDelta": "{\"city\""
				}
			}`,
		},
		{
			name: "tool call end",
			event: sigma.Event{
				Kind:         sigma.EventKindToolCallEnd,
				ContentIndex: intPtr(2),
				ToolCall: &sigma.ToolCall{
					ID:   "call_1",
					Name: "lookup",
					Arguments: map[string]any{
						"city": "Melbourne",
					},
				},
			},
			want: `{
				"kind": "toolcall_end",
				"contentIndex": 2,
				"toolCall": {
					"id": "call_1",
					"name": "lookup",
					"arguments": {"city": "Melbourne"}
				}
			}`,
		},
		{
			name: "done",
			event: sigma.Event{
				Kind:       sigma.EventKindDone,
				StopReason: sigma.StopReasonEndTurn,
				Usage:      &sigma.Usage{InputTokens: 5, OutputTokens: 7, TotalTokens: 12},
				FinalMessage: &sigma.AssistantMessage{
					Content:    []sigma.ContentBlock{sigma.Text("Hello")},
					StopReason: sigma.StopReasonEndTurn,
				},
			},
			want: `{
				"kind": "done",
				"finalMessage": {
					"content": [{"type": "text", "text": "Hello"}],
					"stopReason": "end-turn"
				},
				"usage": {
					"inputTokens": 5,
					"outputTokens": 7,
					"totalTokens": 12
				},
				"stopReason": "end-turn"
			}`,
		},
		{
			name: "error",
			event: sigma.Event{
				Kind:  sigma.EventKindError,
				Error: "provider disconnected",
			},
			want: `{
				"kind": "error",
				"error": "provider disconnected"
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}

			assertSameJSON(t, got, []byte(tt.want))
		})
	}
}

func intPtr(value int) *int {
	return &value
}
