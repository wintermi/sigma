// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package streamstate

import (
	"context"
	"errors"
	"sync"
)

var (
	// ErrClosed reports that a stream has already closed.
	ErrClosed = errors.New("stream closed")
	// ErrTerminated reports that a terminal event has already been accepted.
	ErrTerminated = errors.New("stream already terminated")
)

// Terminal is the terminal event and recorded outcome for a stream.
type Terminal[T, F any] struct {
	Event    T
	Final    F
	HasFinal bool
	Err      error
}

// CancelTerminal builds the terminal event used when the stream context ends.
type CancelTerminal[T, F any] func(error) Terminal[T, F]

// Producer serializes event delivery and terminal state for a stream.
type Producer[T, F any] struct {
	ctx    context.Context
	cancel CancelTerminal[T, F]

	events chan T
	in     chan request[T, F]
	close  chan chan struct{}
	done   chan struct{}
	closed chan struct{}
	once   sync.Once

	mu       sync.RWMutex
	final    F
	hasFinal bool
	err      error
}

type request[T, F any] struct {
	event    T
	final    F
	hasFinal bool
	err      error
	terminal bool
	reply    chan error
}

// NewProducer constructs a producer with the provided event buffer size.
func NewProducer[T, F any](ctx context.Context, buffer int, cancel CancelTerminal[T, F]) *Producer[T, F] {
	if ctx == nil {
		ctx = context.Background()
	}
	producer := &Producer[T, F]{
		ctx:    ctx,
		cancel: cancel,
		events: make(chan T, buffer),
		in:     make(chan request[T, F]),
		close:  make(chan chan struct{}),
		done:   make(chan struct{}),
		closed: make(chan struct{}),
	}
	go producer.run()
	return producer
}

// Events returns the event channel.
func (p *Producer[T, F]) Events() <-chan T {
	return p.events
}

// Done closes after the event channel closes.
func (p *Producer[T, F]) Done() <-chan struct{} {
	return p.done
}

// Err returns the terminal error, if any.
func (p *Producer[T, F]) Err() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.err
}

// Final returns the terminal final value, if one was recorded.
func (p *Producer[T, F]) Final() (F, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.final, p.hasFinal
}

// Emit sends a non-terminal event.
func (p *Producer[T, F]) Emit(ctx context.Context, event T) error {
	return p.send(ctx, request[T, F]{event: event})
}

// Finish sends a terminal event.
func (p *Producer[T, F]) Finish(ctx context.Context, terminal Terminal[T, F]) error {
	return p.send(ctx, request[T, F]{
		event:    terminal.Event,
		final:    terminal.Final,
		hasFinal: terminal.HasFinal,
		err:      terminal.Err,
		terminal: true,
	})
}

// Close closes the stream without emitting another event.
func (p *Producer[T, F]) Close() {
	ack := make(chan struct{})
	select {
	case p.close <- ack:
		<-ack
	case <-p.done:
	}
}

func (p *Producer[T, F]) send(ctx context.Context, req request[T, F]) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req.reply = make(chan error, 1)

	select {
	case p.in <- req:
	case <-ctx.Done():
		return ctx.Err()
	case <-p.closed:
		return ErrClosed
	case <-p.done:
		return ErrClosed
	}

	select {
	case err := <-req.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Producer[T, F]) run() {
	defer close(p.events)
	defer close(p.done)

	var active *request[T, F]
	terminated := false

	for {
		if active == nil && terminated {
			return
		}

		var out chan T
		var event T
		if active != nil {
			out = p.events
			event = active.event
		}

		var in chan request[T, F]
		if active == nil && !terminated {
			in = p.in
		}

		var ctxDone <-chan struct{}
		if !terminated {
			ctxDone = p.ctx.Done()
		}

		select {
		case req := <-in:
			if req.terminal {
				p.record(req.final, req.hasFinal, req.err)
				p.closeWrites()
				terminated = true
			}
			active = &req

		case out <- event:
			active.reply <- nil
			if active.terminal {
				return
			}
			active = nil

		case <-ctxDone:
			p.abort(p.ctx.Err(), active)
			return

		case ack := <-p.close:
			if err := p.ctx.Err(); err != nil {
				p.abort(err, active)
				close(ack)
				return
			}
			p.closeWrites()
			if active != nil {
				active.reply <- ErrClosed
			}
			close(ack)
			return
		}
	}
}

func (p *Producer[T, F]) abort(err error, active *request[T, F]) {
	terminal := p.cancel(err)
	p.record(terminal.Final, terminal.HasFinal, terminal.Err)
	p.closeWrites()
	if active != nil {
		active.reply <- ErrClosed
	}
	select {
	case p.events <- terminal.Event:
	default:
	}
}

func (p *Producer[T, F]) record(final F, hasFinal bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.final = final
	p.hasFinal = hasFinal
	p.err = err
}

func (p *Producer[T, F]) closeWrites() {
	p.once.Do(func() {
		close(p.closed)
	})
}
