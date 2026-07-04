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

	reader := bufio.NewReader(r)
	builder := eventBuilder{}
	lineNumber := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		raw, err := readLine(reader, cfg.maxLineBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				_, err := dispatchFrame(ctx, &builder, handle)
				return err
			}
			return err
		}
		lineNumber++

		line := trimLineEnding(raw)
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

func readLine(reader *bufio.Reader, maxLineBytes int) ([]byte, error) {
	var line []byte
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return line, nil
			}
			return nil, fmt.Errorf("sse: read line: %w", err)
		}
		line = append(line, b)
		if maxLineBytes > 0 && len(line) > maxLineBytes {
			return nil, ErrLineTooLarge
		}
		switch b {
		case '\n':
			return line, nil
		case '\r':
			next, err := reader.Peek(1)
			if err == nil && next[0] == '\n' {
				_, _ = reader.ReadByte()
				line = append(line, '\n')
				if maxLineBytes > 0 && len(line) > maxLineBytes {
					return nil, ErrLineTooLarge
				}
			}
			return line, nil
		}
	}
}

func trimLineEnding(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte{'\n'})
	return bytes.TrimSuffix(line, []byte{'\r'})
}
