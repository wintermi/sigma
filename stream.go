// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"sort"
	"sync"

	"github.com/wintermi/sigma/internal/streamstate"
)

const (
	// StopReasonAborted indicates generation stopped because the stream context was canceled.
	StopReasonAborted StopReason = "aborted"
)

const (
	// ErrorStream indicates a provider stream ended with an error event.
	ErrorStream ErrorCode = "stream"
	// ErrorAborted indicates a stream ended because its context was canceled.
	ErrorAborted ErrorCode = "aborted"
	// ErrorStreamClosed indicates a write was attempted after stream closure.
	ErrorStreamClosed ErrorCode = "stream-closed"
	// ErrorInvalidStreamEvent indicates a writer was asked to emit an invalid event.
	ErrorInvalidStreamEvent ErrorCode = "invalid-stream-event"
)

// streamEventBuffer is intentionally one event. Providers can finish before a
// consumer starts reading, but any second unread event applies backpressure.
const streamEventBuffer = 1

// Stream is a single-consumer stream of ordered provider-neutral events.
//
// Events is single-consumer: callers should have exactly one goroutine receive
// from it, or coordinate their own fan-out. Close lets a consumer stop early.
type Stream struct {
	producer *streamstate.Producer[Event, AssistantMessage]
}

// StreamWriter is the provider side of a Stream.
type StreamWriter interface {
	// Emit sends a non-terminal event.
	Emit(context.Context, Event) error
	// Done sends the single successful terminal event.
	Done(context.Context, AssistantMessage) error
	// Error sends the single error terminal event.
	Error(context.Context, error, AssistantMessage) error
	// Close closes the stream without emitting another event.
	Close()
}

type streamWriter struct {
	producer *streamstate.Producer[Event, AssistantMessage]
	partial  *partialAccumulator
}

// NewStream constructs a stream and its provider-side writer.
func NewStream(ctx context.Context) (*Stream, StreamWriter) {
	partial := newPartialAccumulator()
	producer := streamstate.NewProducer[Event, AssistantMessage](ctx, streamEventBuffer, partial.cancelTerminal)
	return &Stream{producer: producer}, &streamWriter{producer: producer, partial: partial}
}

func errorStream(ctx context.Context, err error, final AssistantMessage) *Stream {
	stream, writer := NewStream(ctx)
	_ = writer.Error(ctx, err, final)
	return stream
}

// Events returns the ordered stream events.
func (s *Stream) Events() <-chan Event {
	if s == nil || s.producer == nil {
		ch := make(chan Event)
		close(ch)
		return ch
	}
	return s.producer.Events()
}

// Done closes after Events closes.
func (s *Stream) Done() <-chan struct{} {
	if s == nil || s.producer == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.producer.Done()
}

// Err returns the terminal stream error, if any.
func (s *Stream) Err() error {
	if s == nil || s.producer == nil {
		return nil
	}
	return s.producer.Err()
}

// Final returns the terminal assistant message, if the stream recorded one.
func (s *Stream) Final() (AssistantMessage, bool) {
	if s == nil || s.producer == nil {
		return AssistantMessage{}, false
	}
	return s.producer.Final()
}

// Close stops the stream without waiting for a provider terminal event.
func (s *Stream) Close() {
	if s == nil || s.producer == nil {
		return
	}
	s.producer.Close()
}

func (w *streamWriter) Emit(ctx context.Context, event Event) error {
	if event.IsTerminal() {
		return &Error{
			Code:    ErrorInvalidStreamEvent,
			Message: "terminal events must be sent with Done or Error",
		}
	}
	w.partial.apply(event)
	if event.PartialMessage == nil {
		if partial, ok := w.partial.snapshot(true, false); ok {
			event.PartialMessage = &partial
		}
	}
	if err := mapStreamStateError(w.producer.Emit(ctx, event)); err != nil {
		return err
	}
	return nil
}

func (w *streamWriter) Done(ctx context.Context, final AssistantMessage) error {
	event := finalEvent(EventKindDone, final, "")
	return mapStreamStateError(w.producer.Finish(ctx, streamstate.Terminal[Event, AssistantMessage]{
		Event:    event,
		Final:    final,
		HasFinal: true,
	}))
}

func (w *streamWriter) Error(ctx context.Context, err error, final AssistantMessage) error {
	if final.StopReason == "" {
		final.StopReason = StopReasonError
	}
	code := ErrorStream
	if final.StopReason == StopReasonAborted {
		code = ErrorAborted
	}
	terminalErr := terminalError(code, err, "stream error")
	event := finalEvent(EventKindError, final, terminalErr.Error())
	generationErr := &GenerationError{Final: final, Err: terminalErr}
	return mapStreamStateError(w.producer.Finish(ctx, streamstate.Terminal[Event, AssistantMessage]{
		Event:    event,
		Final:    final,
		HasFinal: true,
		Err:      generationErr,
	}))
}

func (w *streamWriter) Close() {
	w.producer.Close()
}

// Collect consumes stream until it receives a terminal event or ctx is canceled.
func Collect(ctx context.Context, stream *Stream) (AssistantMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	events := stream.Events()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return closedStreamResult(stream)
			}
			switch event.Kind { //nolint:exhaustive
			case EventKindDone:
				return finalFromEvent(stream, event), nil
			case EventKindError:
				final := finalFromEvent(stream, event)
				return final, errorFromEvent(event, stream.Err())
			}
		case <-ctx.Done():
			stream.Close()
			return closedStreamResult(stream)
		}
	}
}

type partialAccumulator struct {
	mu     sync.Mutex
	blocks map[int]*partialBlock
}

type partialBlock struct {
	kind       ContentBlockType
	text       string
	thinking   string
	image      *ContentBlock
	toolID     string
	toolName   string
	arguments  string
	argument   any
	hasArg     bool
	hasContent bool
	started    bool
}

func newPartialAccumulator() *partialAccumulator {
	return &partialAccumulator{blocks: make(map[int]*partialBlock)}
}

func (a *partialAccumulator) apply(event Event) {
	a.mu.Lock()
	defer a.mu.Unlock()

	index := eventContentIndex(event)
	switch event.Kind { //nolint:exhaustive
	case EventKindTextStart:
		a.block(index, ContentBlockText)
	case EventKindTextDelta:
		block := a.block(index, ContentBlockText)
		if event.Text != "" {
			block.text = event.Text
		} else {
			block.text += event.DeltaText
		}
		block.hasContent = block.hasContent || block.text != ""
	case EventKindTextEnd:
		block := a.block(index, ContentBlockText)
		if event.Text != "" {
			block.text = event.Text
			block.hasContent = true
		}
	case EventKindThinkingStart:
		a.block(index, ContentBlockThinking)
	case EventKindThinkingDelta:
		block := a.block(index, ContentBlockThinking)
		if event.Thinking != "" {
			block.thinking = event.Thinking
		} else {
			block.thinking += event.DeltaText
		}
		block.hasContent = block.hasContent || block.thinking != ""
	case EventKindThinkingEnd:
		block := a.block(index, ContentBlockThinking)
		if event.Thinking != "" {
			block.thinking = event.Thinking
			block.hasContent = true
		}
	case EventKindToolCallStart, EventKindToolCallDelta:
		block := a.block(index, ContentBlockToolCall)
		block.applyToolPartial(event.PartialToolCall)
	case EventKindToolCallEnd:
		block := a.block(index, ContentBlockToolCall)
		if event.ToolCall != nil {
			block.toolID = event.ToolCall.ID
			block.toolName = event.ToolCall.Name
			block.argument = event.ToolCall.Arguments
			block.hasArg = true
			block.hasContent = true
		}
	case EventKindImageStart:
		a.block(index, ContentBlockImage)
	case EventKindImageDelta:
		block := a.block(index, ContentBlockImage)
		if event.PartialImage != nil {
			image := *event.PartialImage
			block.image = &image
			block.hasContent = true
		}
	case EventKindImageEnd:
		block := a.block(index, ContentBlockImage)
		if event.Image != nil {
			image := *event.Image
			block.image = &image
			block.hasContent = true
		}
	}
}

func (a *partialAccumulator) block(index int, kind ContentBlockType) *partialBlock {
	block := a.blocks[index]
	if block == nil {
		block = &partialBlock{kind: kind}
		a.blocks[index] = block
	}
	if block.kind == "" {
		block.kind = kind
	}
	block.started = true
	return block
}

func (a *partialAccumulator) cancelTerminal(err error) streamstate.Terminal[Event, AssistantMessage] {
	terminalErr := terminalError(ErrorAborted, err, "stream aborted")
	final := a.final()
	final.StopReason = StopReasonAborted
	return streamstate.Terminal[Event, AssistantMessage]{
		Event:    finalEvent(EventKindError, final, terminalErr.Error()),
		Final:    final,
		HasFinal: true,
		Err:      &GenerationError{Final: final, Err: terminalErr},
	}
}

func (a *partialAccumulator) final() AssistantMessage {
	message, _ := a.snapshot(false, true)
	return message
}

func (a *partialAccumulator) snapshot(includeStarted bool, decodeArguments bool) (AssistantMessage, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.blocks) == 0 {
		return AssistantMessage{}, false
	}
	indexes := make([]int, 0, len(a.blocks))
	for index, block := range a.blocks {
		if block.include(includeStarted) {
			indexes = append(indexes, index)
		}
	}
	if len(indexes) == 0 {
		return AssistantMessage{}, false
	}
	sort.Ints(indexes)
	content := make([]ContentBlock, 0, len(indexes))
	for _, index := range indexes {
		content = append(content, a.blocks[index].contentBlock(decodeArguments))
	}
	return AssistantMessage{Content: content}, true
}

func eventContentIndex(event Event) int {
	if event.ContentIndex != nil {
		return *event.ContentIndex
	}
	return 0
}

func (b *partialBlock) applyToolPartial(partial *PartialToolCall) {
	if partial == nil {
		return
	}
	if partial.ID != "" {
		b.toolID = partial.ID
	}
	if partial.Name != "" {
		b.toolName = partial.Name
	}
	if partial.ArgumentsDelta != "" {
		b.arguments += partial.ArgumentsDelta
	}
	if arguments, ok := partial.ProviderMetadata["arguments"]; ok {
		b.argument = arguments
		b.hasArg = true
	}
	if argumentsText, ok := partial.ProviderMetadata["argumentsText"].(string); ok {
		b.arguments = argumentsText
	}
	b.hasContent = b.hasContent || b.toolID != "" || b.toolName != "" || b.arguments != "" || b.hasArg
}

func (b *partialBlock) include(includeStarted bool) bool {
	return b != nil && (b.hasContent || includeStarted && b.started)
}

func (b *partialBlock) contentBlock(decodeArguments bool) ContentBlock {
	switch b.kind {
	case ContentBlockThinking:
		return Thinking(b.thinking, "")
	case ContentBlockImage:
		if b.image != nil {
			// Deep-clone so consumers mutating the snapshot cannot corrupt
			// the accumulator's copy shared with later snapshots.
			return cloneHandoffContentBlock(*b.image)
		}
		return ContentBlock{Type: ContentBlockImage}
	case ContentBlockToolCall:
		return ToolCallBlock(b.toolID, b.toolName, b.toolArguments(decodeArguments))
	default:
		return Text(b.text)
	}
}

func (b *partialBlock) toolArguments(decode bool) any {
	if b.hasArg {
		return cloneHandoffAny(b.argument)
	}
	if b.arguments == "" {
		return map[string]any{}
	}
	// Per-event snapshots return the accumulated arguments text as-is;
	// decoding it on every delta would re-parse the whole buffer each time.
	// The terminal snapshot decodes once so an aborted stream's final
	// message still carries structured arguments.
	if decode {
		var decoded any
		if err := json.Unmarshal([]byte(b.arguments), &decoded); err == nil {
			return decoded
		}
	}
	return b.arguments
}

func finalEvent(kind EventKind, final AssistantMessage, message string) Event {
	finalCopy := final
	return Event{
		Kind:         kind,
		FinalMessage: &finalCopy,
		Usage:        finalCopy.Usage,
		StopReason:   finalCopy.StopReason,
		Error:        message,
	}
}

func finalFromEvent(stream *Stream, event Event) AssistantMessage {
	if event.FinalMessage != nil {
		return *event.FinalMessage
	}
	if final, ok := stream.Final(); ok {
		return final
	}
	return AssistantMessage{}
}

func closedStreamResult(stream *Stream) (AssistantMessage, error) {
	final, hasFinal := stream.Final()
	if err := stream.Err(); err != nil {
		return final, ensureStreamError(err)
	}
	if hasFinal {
		return final, nil
	}
	return AssistantMessage{}, &Error{
		Code:    ErrorStreamClosed,
		Message: "stream closed before terminal event",
	}
}

func errorFromEvent(event Event, err error) error {
	if err != nil {
		return ensureStreamError(err)
	}
	message := event.Error
	if message == "" {
		message = "stream error"
	}
	code := ErrorStream
	if event.StopReason == StopReasonAborted {
		code = ErrorAborted
	}
	final := AssistantMessage{StopReason: event.StopReason}
	if event.FinalMessage != nil {
		final = *event.FinalMessage
	}
	return &GenerationError{
		Final: final,
		Err:   &Error{Code: code, Message: message},
	}
}

func terminalError(code ErrorCode, err error, fallback string) *Error {
	if err == nil {
		return &Error{Code: code, Message: fallback}
	}
	var streamErr *Error
	if stderrors.As(err, &streamErr) {
		return streamErr
	}
	return &Error{Code: code, Message: err.Error(), Err: err}
}

func ensureStreamError(err error) error {
	var generationErr *GenerationError
	if stderrors.As(err, &generationErr) {
		return generationErr
	}
	var streamErr *Error
	if stderrors.As(err, &streamErr) {
		return streamErr
	}
	return &Error{Code: ErrorStream, Message: err.Error(), Err: err}
}

func mapStreamStateError(err error) error {
	if err == nil {
		return nil
	}
	if stderrors.Is(err, streamstate.ErrClosed) || stderrors.Is(err, streamstate.ErrTerminated) {
		return &Error{Code: ErrorStreamClosed, Message: err.Error()}
	}
	return err
}
