// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

// A consumer ranging over Events must see a terminal event after cancellation
// even when an undelivered event still fills the buffer.
func TestStreamCancellationDeliversTerminalEventWhenBufferIsFull(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	stream, writer := sigma.NewStream(ctx)
	defer stream.Close()

	if err := writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindStart}); err != nil {
		t.Fatal(err)
	}
	index := 0
	go func() { _ = writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindTextStart, ContentIndex: &index}) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	// Let the producer finish aborting before a slow consumer starts reading.
	<-stream.Done()

	var kinds []sigma.EventKind
	for event := range stream.Events() {
		kinds = append(kinds, event.Kind)
	}
	if len(kinds) == 0 || kinds[len(kinds)-1] != sigma.EventKindError {
		t.Fatalf("events = %v, want a terminal error event last", kinds)
	}
}
