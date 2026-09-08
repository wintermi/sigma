// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package streamblocks

import (
	"encoding/json"
	"testing"
)

func TestToolArgumentNumberPrecisionAcrossDeltas(t *testing.T) {
	t.Parallel()
	for _, number := range []string{"9007199254740993", "9223372036854775807", "-9223372036854775808", "0.1234567890123456789", "1.234567890123456789e20"} {
		t.Run(number, func(t *testing.T) {
			t.Parallel()
			raw := `{"nested":[` + number + `]}`
			var call ToolCall
			for _, delta := range []string{`{"nested":[`, number, `]`} {
				call.AppendArguments(delta)
			}
			partial, ok := call.DecodePartialArguments()
			if !ok {
				t.Fatal("missing partial arguments")
			}
			if partial.(map[string]any)["nested"].([]any)[0] != json.Number(number) {
				t.Fatalf("partial = %#v", partial)
			}
			call.AppendArguments("}")
			got, err := json.Marshal(call.ToolCall().Arguments)
			if err != nil || string(got) != raw {
				t.Fatalf("arguments = %s, %v", got, err)
			}
			call.AppendArguments(` {}`)
			if _, ok := call.DecodeArguments(); ok {
				t.Fatal("accepted trailing value")
			}
		})
	}
}
