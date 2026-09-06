// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package streamlifecycle

import (
	"context"
	"sync"

	"github.com/wintermi/sigma"
)

// NewTextStream creates a sigma stream whose request context is canceled when
// the consumer closes the stream or the stream naturally finishes.
func NewTextStream(ctx context.Context, opts sigma.Options) (context.Context, *sigma.Stream, sigma.StreamWriter, func()) {
	ctx, stopTimeout := sigma.ContextWithRequestTimeout(ctx, opts)
	ctx, cancel := context.WithCancel(ctx)
	stream, writer := sigma.NewStream(ctx)

	done := make(chan struct{})
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			close(done)
			cancel()
			stopTimeout()
		})
	}

	go func() {
		select {
		case <-stream.Done():
			cancel()
		case <-done:
		}
	}()

	return ctx, stream, writer, cleanup
}

// NewImageStream creates a sigma image stream whose request context is canceled when
// the consumer closes the stream or the stream naturally finishes.
func NewImageStream(ctx context.Context, opts sigma.Options) (context.Context, *sigma.ImageStream, sigma.ImageStreamWriter, func()) {
	ctx, stopTimeout := sigma.ContextWithRequestTimeout(ctx, opts)
	ctx, cancel := context.WithCancel(ctx)
	stream, writer := sigma.NewImageStream(ctx)

	done := make(chan struct{})
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			close(done)
			cancel()
			stopTimeout()
		})
	}

	go func() {
		select {
		case <-stream.Done():
			cancel()
		case <-done:
		}
	}()

	return ctx, stream, writer, cleanup
}
