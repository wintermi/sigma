// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sse

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	defaultMaxLineBytes  = 16 << 20
	defaultMaxEventBytes = 16 << 20
)

var (
	// ErrStop lets a handler stop parsing without treating the stop as failure.
	ErrStop = errors.New("sse: stop parsing")
	// ErrMalformedLine is retained for compatibility; colonless field lines are
	// accepted by Parse and ignored unless they name a supported SSE field.
	ErrMalformedLine = errors.New("sse: malformed line")
	// ErrLineTooLarge reports a line exceeding the configured line limit.
	ErrLineTooLarge = errors.New("sse: line too large")
	// ErrEventTooLarge reports an event frame exceeding the configured event limit.
	ErrEventTooLarge = errors.New("sse: event too large")
)

// Line records one parsed event-stream line without its line ending.
type Line struct {
	Number  int
	Raw     string
	Field   string
	Value   string
	Comment bool
}

// Event is a provider-neutral Server-Sent Event frame.
type Event struct {
	Event string
	Data  string
	ID    string
	Done  bool
	Lines []Line
}

// Handler receives parsed events. Return ErrStop to end parsing successfully.
type Handler func(Event) error

// Option configures Parse.
type Option func(*config)

type config struct {
	maxLineBytes  int
	maxEventBytes int
}

// WithMaxLineBytes sets the maximum line size accepted by Parse.
func WithMaxLineBytes(n int) Option {
	return func(cfg *config) {
		cfg.maxLineBytes = n
	}
}

// WithMaxEventBytes sets the maximum accumulated event frame size accepted by Parse.
func WithMaxEventBytes(n int) Option {
	return func(cfg *config) {
		cfg.maxEventBytes = n
	}
}

// Parse reads an SSE stream from r and calls handle for each completed event.
//
// Parse understands comments, data, event, and id fields. Repeated data fields
// are joined with "\n". Provider-specific JSON decoding belongs in callers.
// Parse enforces default line and event size limits; use WithMaxLineBytes and
// WithMaxEventBytes when a provider needs tighter bounds. Context cancellation
// is returned once ctx is canceled or the underlying reader returns that error.
func Parse(ctx context.Context, r io.Reader, handle Handler, opts ...Option) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if handle == nil {
		return errors.New("sse: nil handler")
	}

	cfg := config{
		maxLineBytes:  defaultMaxLineBytes,
		maxEventBytes: defaultMaxEventBytes,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	reader := &lineReader{reader: bufio.NewReader(r)}
	builder := eventBuilder{}
	lineNumber := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, err := reader.next(cfg.maxLineBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				_, err := dispatchFrame(ctx, &builder, handle)
				return err
			}
			return err
		}
		lineNumber++

		if len(line) == 0 {
			stop, err := dispatchFrame(ctx, &builder, handle)
			if err != nil {
				return err
			}
			if stop {
				return nil
			}
			continue
		}

		if err := builder.add(lineNumber, line, cfg.maxEventBytes); err != nil {
			return err
		}
	}
}

func dispatchFrame(ctx context.Context, builder *eventBuilder, handle Handler) (bool, error) {
	if err := dispatch(ctx, builder, handle); err != nil {
		if errors.Is(err, ErrStop) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

type eventBuilder struct {
	event    string
	id       string
	data     []string
	lines    []Line
	hasEvent bool
	hasID    bool
	hasData  bool
	size     int
}

func (b *eventBuilder) add(number int, raw []byte, maxEventBytes int) error {
	if maxEventBytes > 0 && b.size+len(raw) > maxEventBytes {
		return ErrEventTooLarge
	}
	b.size += len(raw)

	line := string(raw)
	if strings.HasPrefix(line, ":") {
		b.lines = append(b.lines, Line{
			Number:  number,
			Raw:     line,
			Value:   strings.TrimPrefix(line, ":"),
			Comment: true,
		})
		return nil
	}

	field, value, ok := strings.Cut(line, ":")
	if !ok {
		field = line
		value = ""
	} else {
		value = strings.TrimPrefix(value, " ")
	}

	b.lines = append(b.lines, Line{
		Number: number,
		Raw:    line,
		Field:  field,
		Value:  value,
	})

	switch field {
	case "data":
		b.data = append(b.data, value)
		b.hasData = true
	case "event":
		b.event = value
		b.hasEvent = true
	case "id":
		b.id = value
		b.hasID = true
	}

	return nil
}

func (b *eventBuilder) eventFrame() (Event, bool) {
	if !b.hasEvent && !b.hasID && !b.hasData {
		return Event{}, false
	}

	data := strings.Join(b.data, "\n")
	return Event{
		Event: b.event,
		Data:  data,
		ID:    b.id,
		Done:  strings.TrimSpace(data) == "[DONE]",
		Lines: append([]Line(nil), b.lines...),
	}, true
}

func (b *eventBuilder) reset() {
	*b = eventBuilder{}
}

// LineError describes a parse failure at a specific stream line.
type LineError struct {
	Line int
	Raw  string
	Err  error
}

func (e *LineError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("sse: line %d: %v", e.Line, e.Err)
}

func (e *LineError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func dispatch(ctx context.Context, builder *eventBuilder, handle Handler) error {
	event, ok := builder.eventFrame()
	builder.reset()
	if !ok {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := handle(event); err != nil {
		return err
	}
	return nil
}

// lineReader splits a stream into lines terminated by '\n', "\r\n", or a bare
// '\r', returning line content without the terminator.
type lineReader struct {
	reader *bufio.Reader
	skipLF bool
}

// next returns the next line, scanning the buffered window in bulk rather than
// byte-at-a-time. A bare '\r' completes the line immediately — the possible
// '\n' half of a CRLF split across reads is consumed lazily on the following
// call — so a CR-terminated event is dispatched even while the stream idles.
func (r *lineReader) next(maxLineBytes int) ([]byte, error) {
	var line []byte
	for {
		window, err := r.peekWindow()
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return line, nil
			}
			return nil, fmt.Errorf("sse: read line: %w", err)
		}
		if r.skipLF {
			r.skipLF = false
			if window[0] == '\n' {
				_, _ = r.reader.Discard(1)
				continue
			}
		}
		terminator := bytes.IndexAny(window, "\r\n")
		if terminator < 0 {
			line = append(line, window...)
			if maxLineBytes > 0 && len(line) > maxLineBytes {
				return nil, ErrLineTooLarge
			}
			_, _ = r.reader.Discard(len(window))
			continue
		}
		line = append(line, window[:terminator]...)
		if maxLineBytes > 0 && len(line) > maxLineBytes {
			return nil, ErrLineTooLarge
		}
		discard := terminator + 1
		if window[terminator] == '\r' {
			if terminator+1 < len(window) {
				if window[terminator+1] == '\n' {
					discard++
				}
			} else {
				r.skipLF = true
			}
		}
		_, _ = r.reader.Discard(discard)
		return line, nil
	}
}

// peekWindow returns the buffered bytes, blocking only when the buffer is
// empty and more input is needed.
func (r *lineReader) peekWindow() ([]byte, error) {
	buffered := r.reader.Buffered()
	if buffered == 0 {
		if _, err := r.reader.Peek(1); err != nil {
			return nil, fmt.Errorf("sse: peek line: %w", err)
		}
		buffered = r.reader.Buffered()
	}
	window, err := r.reader.Peek(buffered)
	if err != nil {
		return nil, fmt.Errorf("sse: peek buffered line: %w", err)
	}
	return window, nil
}
