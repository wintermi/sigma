// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package contextlock

import (
	"context"
	"errors"
	"testing"
	"time"
)

type observedContext struct {
	context.Context
	waiting chan struct{}
}

func (c observedContext) Done() <-chan struct{} { close(c.waiting); return c.Context.Done() }

func TestMutexCanceledWaiterPreservesOwnership(t *testing.T) {
	t.Parallel()
	var mu Mutex
	if err := mu.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan struct{})
	result := make(chan error, 1)
	go func() { result <- mu.Lock(observedContext{ctx, waiting}) }()
	<-waiting
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled waiter blocked")
	}
	// An independent waiter must still wait for the original owner.
	waiting = make(chan struct{})
	go func() { result <- mu.Lock(observedContext{context.Background(), waiting}) }()
	<-waiting
	select {
	case err := <-result:
		t.Fatalf("waiter released another owner: %v", err)
	default:
	}
	mu.Unlock()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ownership was not released")
	}
	mu.Unlock()
}

func TestMutexAlreadyCanceled(t *testing.T) {
	t.Parallel()
	var mu Mutex
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := mu.Lock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := mu.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Unlock()
}

// cancelOnAcquisition models cancellation after the initial check but before
// ownership is returned, without depending on goroutine scheduling.
type cancelOnAcquisition struct {
	context.Context
	checked bool
}

func (c *cancelOnAcquisition) Err() error {
	if c.checked {
		return context.Canceled
	}
	c.checked = true
	return nil
}

func TestMutexCancellationAfterAcquisitionReleasesOwnership(t *testing.T) {
	t.Parallel()
	var mu Mutex
	ctx := &cancelOnAcquisition{Context: context.Background()}
	if err := mu.Lock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ready := make(chan error, 1)
	go func() { ready <- mu.Lock(context.Background()) }()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation retained ownership")
	}
	mu.Unlock()
}
