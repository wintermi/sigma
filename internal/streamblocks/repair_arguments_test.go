// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package streamblocks

import (
	"fmt"
	"testing"
)

// Models sometimes emit invalid escapes or raw control characters inside
// argument strings; the final call must still carry structured arguments.
func TestToolCallFinalArgumentsRepairInvalidEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments string
		want      string
	}{
		{name: "invalid escape and raw newline", arguments: "{\"pattern\":\"\\d+\",\"code\":\"a\nb\"}", want: "map[code:a\nb pattern:\\d+]"},
		{name: "valid arguments unchanged", arguments: `{"path":"a.txt"}`, want: "map[path:a.txt]"},
		{name: "truncated arguments stay raw", arguments: `{"path":"a.t`, want: `{"path":"a.t`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var call ToolCall
			call.AppendArguments(tt.arguments)
			if got := fmt.Sprint(call.ToolCall().Arguments); got != tt.want {
				t.Fatalf("arguments = %q, want %q", got, tt.want)
			}
		})
	}
}
