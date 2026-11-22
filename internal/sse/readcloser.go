// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

type contextReadCloser struct {
	body io.ReadCloser
	done chan struct{}
	once sync.Once
}

// CloseOnContextDone closes body when ctx is canceled so a blocked SSE read can
// unblock promptly. Callers still own the returned ReadCloser and should close it.
func CloseOnContextDone(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	if ctx == nil {
		ctx = context.Background()
	}
	wrapped := &contextReadCloser{body: body, done: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			_ = wrapped.Close()
		case <-wrapped.done:
		}
	}()
	return wrapped
}

func (r *contextReadCloser) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if err == nil {
		return n, nil
	}
	// io.EOF is returned as is: readers such as io.ReadAll compare it with ==.
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	return n, fmt.Errorf("sse: read: %w", err)
}

func (r *contextReadCloser) Close() error {
	var err error
	r.once.Do(func() {
		close(r.done)
		if closeErr := r.body.Close(); closeErr != nil {
			err = fmt.Errorf("sse: close reader: %w", closeErr)
		}
	})
	return err
}
