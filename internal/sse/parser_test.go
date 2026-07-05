// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sse_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wintermi/sigma/internal/sse"
)

func TestParseOpenAIStyleFramesStopsAtDone(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}",
		"",
		"data: [DONE]",
		"",
		"data: ignored",
		"",
	}, "\n")

	events, err := collect(input, func(event sse.Event) error {
		if event.Done {
			return sse.ErrStop
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, want := len(events), 2; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if got, want := events[0].Data, `{"choices":[{"delta":{"content":"hello"}}]}`; got != want {
		t.Fatalf("first data = %q, want %q", got, want)
	}
	if !events[1].Done {
		t.Fatal("DONE frame did not set Done")
	}
}

func TestParseAnthropicStyleEventDataIDAndCRLF(t *testing.T) {
	t.Parallel()

	input := "event: content_block_delta\r\nid: evt_1\r\ndata: {\"delta\":{\"text\":\"hi\"}}\r\n\r\n"

	events, err := collect(input, nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	event := events[0]
	if got, want := event.Event, "content_block_delta"; got != want {
		t.Fatalf("event name = %q, want %q", got, want)
	}
	if got, want := event.ID, "evt_1"; got != want {
		t.Fatalf("event id = %q, want %q", got, want)
	}
	if got, want := event.Data, `{"delta":{"text":"hi"}}`; got != want {
		t.Fatalf("event data = %q, want %q", got, want)
	}
	if got, want := len(event.Lines), 3; got != want {
		t.Fatalf("line metadata count = %d, want %d", got, want)
	}
}

func TestParseSupportsCROnlyLineEndings(t *testing.T) {
	t.Parallel()

	events, err := collect("event: message\rdata: first\rdata: second\r\r", nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if got, want := events[0].Event, "message"; got != want {
		t.Fatalf("event name = %q, want %q", got, want)
	}
	if got, want := events[0].Data, "first\nsecond"; got != want {
		t.Fatalf("event data = %q, want %q", got, want)
	}
}

func TestParseMultilineEmptyDataAndComments(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		": keepalive",
		"data: first",
		"data: second",
		"",
		"data:",
		"",
		": ignored-only comment",
		"",
	}, "\n")

	events, err := collect(input, nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, want := len(events), 2; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if got, want := events[0].Data, "first\nsecond"; got != want {
		t.Fatalf("multiline data = %q, want %q", got, want)
	}
	if got, want := len(events[0].Lines), 3; got != want {
		t.Fatalf("first frame line count = %d, want %d", got, want)
	}
	if !events[0].Lines[0].Comment {
		t.Fatal("comment line was not recorded as comment metadata")
	}
	if got, want := events[1].Data, ""; got != want {
		t.Fatalf("empty data = %q, want %q", got, want)
	}
}

func TestParseDispatchesFinalFrameWithoutTrailingBlankLine(t *testing.T) {
	t.Parallel()

	events, err := collect("event: message\ndata: final", nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if got, want := events[0].Data, "final"; got != want {
		t.Fatalf("final data = %q, want %q", got, want)
	}
}

func TestParseAcceptsColonlessFieldLines(t *testing.T) {
	t.Parallel()

	events, err := collect("data\nignored\ndata: value\n\n", nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if got, want := events[0].Data, "\nvalue"; got != want {
		t.Fatalf("event data = %q, want %q", got, want)
	}
	if got, want := events[0].Lines[0].Field, "data"; got != want {
		t.Fatalf("colonless field = %q, want %q", got, want)
	}
	if got, want := events[0].Lines[1].Field, "ignored"; got != want {
		t.Fatalf("unknown colonless field = %q, want %q", got, want)
	}
}

func TestParseAcceptsLargeFrameAboveScannerLimit(t *testing.T) {
	t.Parallel()

	large := strings.Repeat("x", 2*1024*1024)
	events, err := collect("data: "+large+"\n\n", nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, want := events[0].Data, large; got != want {
		t.Fatalf("large data length = %d, want %d", len(got), len(want))
	}
}

func TestParseEnforcesSizeLimits(t *testing.T) {
	t.Parallel()

	err := sse.Parse(context.Background(), strings.NewReader("data: too-large\n\n"), func(sse.Event) error {
		return nil
	}, sse.WithMaxEventBytes(4))
	if err == nil {
		t.Fatal("Parse returned nil error")
	}
	if !errors.Is(err, sse.ErrEventTooLarge) {
		t.Fatalf("Parse error = %v, want ErrEventTooLarge", err)
	}
}

func TestParseEnforcesLineSizeLimit(t *testing.T) {
	t.Parallel()

	err := sse.Parse(context.Background(), strings.NewReader("data: too-large\n\n"), func(sse.Event) error {
		return nil
	}, sse.WithMaxLineBytes(4))
	if err == nil {
		t.Fatal("Parse returned nil error")
	}
	if !errors.Is(err, sse.ErrLineTooLarge) {
		t.Fatalf("Parse error = %v, want ErrLineTooLarge", err)
	}
}

func TestCloseOnContextDoneUnblocksReader(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	reader := newBlockingReadCloser()
	done := make(chan error, 1)
	go func() {
		done <- sse.Parse(ctx, sse.CloseOnContextDone(ctx, reader), func(sse.Event) error {
			return errors.New("handler should not be called")
		})
	}()

	receiveTestSignal(t, reader.started)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, context.Canceled) {
			t.Fatalf("Parse error = %v, want reader close or context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Parse did not return after context cancellation")
	}
	if !reader.isClosed() {
		t.Fatal("reader was not closed after context cancellation")
	}
}

func TestParseReturnsReaderCancellationError(t *testing.T) {
	t.Parallel()

	err := sse.Parse(context.Background(), cancelReader{}, func(sse.Event) error {
		t.Fatal("handler should not be called")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Parse error = %v, want context.Canceled", err)
	}
}

func collect(input string, onEvent func(sse.Event) error) ([]sse.Event, error) {
	var events []sse.Event
	err := sse.Parse(context.Background(), strings.NewReader(input), func(event sse.Event) error {
		events = append(events, event)
		if onEvent != nil {
			return onEvent(event)
		}
		return nil
	})
	return events, err
}

type cancelReader struct{}

func (cancelReader) Read([]byte) (int, error) {
	return 0, context.Canceled
}

var _ io.Reader = cancelReader{}

type blockingReadCloser struct {
	started chan struct{}
	closed  chan struct{}
	start   sync.Once
	close   sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	r.start.Do(func() { close(r.started) })
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *blockingReadCloser) Close() error {
	r.close.Do(func() { close(r.closed) })
	return nil
}

func (r *blockingReadCloser) isClosed() bool {
	select {
	case <-r.closed:
		return true
	default:
		return false
	}
}

func receiveTestSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func TestParseDispatchesCRTerminatedEventWhileStreamIdle(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	t.Cleanup(func() {
		_ = writer.Close()
	})
	events := make(chan sse.Event, 1)
	go func() {
		_ = sse.Parse(context.Background(), reader, func(event sse.Event) error {
			events <- event
			return sse.ErrStop
		})
	}()
	go func() {
		// CR-only terminators with the connection left open and idle: the
		// event must dispatch without waiting for the next byte.
		_, _ = writer.Write([]byte("data: hi\r\r"))
	}()

	select {
	case event := <-events:
		if got, want := event.Data, "hi"; got != want {
			t.Fatalf("event data = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CR-terminated event was not dispatched while the stream idled")
	}
}
