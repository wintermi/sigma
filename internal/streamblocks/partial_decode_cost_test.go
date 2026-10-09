// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package streamblocks

import (
	"strings"
	"testing"
	"time"
)

// A long streamed argument, such as a file written through a tool call, must
// not re-parse the whole accumulated text on every delta.
func TestToolCallPartialDecodeStaysLinearForLongArguments(t *testing.T) {
	t.Parallel()

	raw := `{"path":"main.go","content":"` + strings.Repeat("x", 256<<10) + `"}`
	var call ToolCall
	start := time.Now()
	for offset := 0; offset < len(raw); offset += 32 {
		delta := raw[offset:min(offset+32, len(raw))]
		call.AppendArguments(delta)
		_ = call.Partial(delta, ToolPartialArgumentsText)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("streaming %d argument bytes took %s", len(raw), elapsed)
	}
	arguments, ok := call.ToolCall().Arguments.(map[string]any)
	if !ok || len(arguments["content"].(string)) != 256<<10 {
		t.Fatalf("final arguments were not fully decoded: %T", call.ToolCall().Arguments)
	}
}

func TestToolCallPartialDecodesShortArgumentsOnEveryDelta(t *testing.T) {
	t.Parallel()

	var call ToolCall
	for _, delta := range []string{`{"query":"we`, `ather"`, `,"days":3}`} {
		call.AppendArguments(delta)
	}
	decoded, ok := call.DecodePartialArguments()
	if !ok || decoded.(map[string]any)["query"] != "weather" {
		t.Fatalf("partial arguments = %v, %v", decoded, ok)
	}

	call.SetArguments(`{"q":1}`)
	decoded, ok = call.DecodePartialArguments()
	if !ok || decoded.(map[string]any)["q"] == nil {
		t.Fatalf("replaced arguments = %v, %v", decoded, ok)
	}
}
