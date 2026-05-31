// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

// EventKind identifies the kind of provider-neutral streaming event.
type EventKind string

const (
	// EventKindStart marks the beginning of a stream.
	EventKindStart EventKind = "start"
	// EventKindTextStart marks the beginning of a text content block.
	EventKindTextStart EventKind = "text_start"
	// EventKindTextDelta carries text appended to a text content block.
	EventKindTextDelta EventKind = "text_delta"
	// EventKindTextEnd marks the end of a text content block.
	EventKindTextEnd EventKind = "text_end"
	// EventKindThinkingStart marks the beginning of a thinking content block.
	EventKindThinkingStart EventKind = "thinking_start"
	// EventKindThinkingDelta carries text appended to a thinking content block.
	EventKindThinkingDelta EventKind = "thinking_delta"
	// EventKindThinkingEnd marks the end of a thinking content block.
	EventKindThinkingEnd EventKind = "thinking_end"
	// EventKindToolCallStart marks the beginning of a tool-call content block.
	EventKindToolCallStart EventKind = "toolcall_start"
	// EventKindToolCallDelta carries partial tool-call data.
	EventKindToolCallDelta EventKind = "toolcall_delta"
	// EventKindToolCallEnd marks the end of a tool-call content block.
	EventKindToolCallEnd EventKind = "toolcall_end"
	// EventKindImageStart marks the beginning of an image content block.
	EventKindImageStart EventKind = "image_start"
	// EventKindImageDelta carries partial image content.
	EventKindImageDelta EventKind = "image_delta"
	// EventKindImageEnd marks the end of an image content block.
	EventKindImageEnd EventKind = "image_end"
	// EventKindDone marks the successful end of a stream.
	EventKindDone EventKind = "done"
	// EventKindError marks a stream error.
	EventKindError EventKind = "error"
)

// PartialToolCall describes an in-progress tool-call update.
type PartialToolCall struct {
	ID                string         `json:"id,omitempty"`
	Name              string         `json:"name,omitempty"`
	ArgumentsDelta    string         `json:"argumentsDelta,omitempty"`
	ProviderSignature string         `json:"providerSignature,omitempty"`
	ProviderMetadata  map[string]any `json:"providerMetadata,omitempty"`
}

// Event is a provider-neutral text-generation stream event.
//
// Content block events may be interleaved. Consumers must route text,
// thinking, and tool-call updates by ContentIndex instead of assuming events
// arrive as a single sequential output buffer.
//
// Typical consumers switch on Kind:
//
//	switch event.Kind {
//	case sigma.EventKindTextDelta:
//		handleTextDelta(event.ContentIndex, event.DeltaText)
//	case sigma.EventKindToolCallEnd:
//		handleToolCall(event.ContentIndex, event.ToolCall)
//	case sigma.EventKindDone, sigma.EventKindError:
//		finish(event)
//	}
type Event struct {
	Kind            EventKind         `json:"kind"`
	ContentIndex    *int              `json:"contentIndex,omitempty"`
	DeltaText       string            `json:"deltaText,omitempty"`
	Text            string            `json:"text,omitempty"`
	Thinking        string            `json:"thinking,omitempty"`
	Image           *ContentBlock     `json:"image,omitempty"`
	PartialImage    *ContentBlock     `json:"partialImage,omitempty"`
	ToolCall        *ToolCall         `json:"toolCall,omitempty"`
	PartialToolCall *PartialToolCall  `json:"partialToolCall,omitempty"`
	PartialMessage  *AssistantMessage `json:"partialMessage,omitempty"`
	FinalMessage    *AssistantMessage `json:"finalMessage,omitempty"`
	Usage           *Usage            `json:"usage,omitempty"`
	StopReason      StopReason        `json:"stopReason,omitempty"`
	Error           string            `json:"error,omitempty"`
}

// IsTerminal reports whether kind ends a stream.
func (kind EventKind) IsTerminal() bool {
	return kind == EventKindDone || kind == EventKindError
}

// IsDelta reports whether kind carries an incremental content update.
func (kind EventKind) IsDelta() bool {
	switch kind {
	case EventKindTextDelta, EventKindThinkingDelta, EventKindToolCallDelta, EventKindImageDelta:
		return true
	default:
		return false
	}
}

// IsStart reports whether kind starts a stream or content block.
func (kind EventKind) IsStart() bool {
	switch kind {
	case EventKindStart, EventKindTextStart, EventKindThinkingStart, EventKindToolCallStart, EventKindImageStart:
		return true
	default:
		return false
	}
}

// String returns the event kind string.
func (kind EventKind) String() string {
	return string(kind)
}

// IsTerminal reports whether event ends a stream.
func (event Event) IsTerminal() bool {
	return event.Kind.IsTerminal()
}

// IsDelta reports whether event carries an incremental content update.
func (event Event) IsDelta() bool {
	return event.Kind.IsDelta()
}

// IsStart reports whether event starts a stream or content block.
func (event Event) IsStart() bool {
	return event.Kind.IsStart()
}
