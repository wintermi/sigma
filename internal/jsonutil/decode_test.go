// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package jsonutil

import (
	"encoding/json"
	"testing"
)

func TestDecodePreservesNumbersAndRejectsTrailingValues(t *testing.T) {
	t.Parallel()
	for _, number := range []string{"9007199254740991", "9007199254740993", "9223372036854775807", "-9223372036854775808", "0.1234567890123456789", "1.234567890123456789e+20"} {
		t.Run(number, func(t *testing.T) {
			t.Parallel()
			raw := `{"nested":[` + number + `]}`
			var value map[string]any
			if err := Decode([]byte(raw), &value); err != nil {
				t.Fatal(err)
			}
			got := value["nested"].([]any)[0]
			if got != json.Number(number) {
				t.Fatalf("number = %#v", got)
			}
			encoded, err := json.Marshal(value)
			if err != nil || string(encoded) != raw {
				t.Fatalf("round trip = %s, %v", encoded, err)
			}
		})
	}
	for _, raw := range []string{`{} {}`, `{} null`, `{} garbage`, `{"n":`} {
		var value any
		if Decode([]byte(raw), &value) == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestIsOne(t *testing.T) {
	t.Parallel()
	for _, value := range []any{1, int64(1), float32(1), 1.0, json.Number("1.00"), json.Number("1e0")} {
		if !IsOne(value) {
			t.Errorf("rejected %#v", value)
		}
	}
	for _, value := range []any{nil, "1", true, 0, 2, -1, json.Number("1.00000000000000000001"), json.Number("1e9999999")} {
		if IsOne(value) {
			t.Errorf("accepted %#v", value)
		}
	}
}
