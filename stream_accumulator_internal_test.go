// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"strings"
	"testing"
	"time"
)

// Providers may send only DeltaText. Accumulating those deltas must not copy
// the whole block on every event.
func TestPartialAccumulatorAppendsDeltaOnlyEventsLinearly(t *testing.T) {
	t.Parallel()

	const size, step = 2 << 20, 16
	delta := strings.Repeat("x", step)
	accumulator := newPartialAccumulator()
	start := time.Now()
	for range size / step {
		accumulator.apply(Event{Kind: EventKindTextDelta, ContentIndex: intPtr(0), DeltaText: delta})
		accumulator.apply(Event{Kind: EventKindThinkingDelta, ContentIndex: intPtr(1), DeltaText: delta})
		accumulator.apply(Event{Kind: EventKindToolCallDelta, ContentIndex: intPtr(2), PartialToolCall: &PartialToolCall{ArgumentsDelta: delta}})
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("accumulating %d bytes per block took %s", size, elapsed)
	}

	text := accumulator.blocks[0].contentBlock(false).Text
	thinking := accumulator.blocks[1].contentBlock(false).ThinkingText
	arguments, _ := accumulator.blocks[2].contentBlock(false).ToolArguments.(string)
	if len(text) != size || len(thinking) != size || len(arguments) != size {
		t.Fatalf("accumulated lengths = %d, %d, %d; want %d", len(text), len(thinking), len(arguments), size)
	}

	accumulator.apply(Event{Kind: EventKindTextDelta, ContentIndex: intPtr(0), Text: "replaced"})
	if got := accumulator.blocks[0].contentBlock(false).Text; got != "replaced" {
		t.Fatalf("cumulative Text did not replace the block: %q", got[:min(len(got), 20)])
	}
}
