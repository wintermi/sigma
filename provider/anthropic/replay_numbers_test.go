// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"encoding/json"
	"testing"
)

func TestReplayJSONNumbers(t *testing.T) {
	t.Parallel()
	const raw = `{"decimal":0.123456789012345678901,"exponent":1.234567890123456789e+42,"integer":9007199254740993}`
	for _, tc := range []struct {
		name    string
		value   any
		want    string
		invalid bool
	}{
		{name: "raw", value: json.RawMessage(raw), want: raw},
		{name: "bytes", value: []byte(raw), want: raw},
		{name: "numbers", value: map[string]any{"decimal": json.Number("0.123456789012345678901"), "exponent": json.Number("1.234567890123456789e+42"), "integer": json.Number("9007199254740993")}, want: raw},
		{name: "string", value: raw, want: `"{\"decimal\":0.123456789012345678901,\"exponent\":1.234567890123456789e+42,\"integer\":9007199254740993}"`},
		{name: "nil", want: "null"},
		{name: "invalid raw", value: json.RawMessage(`{"x":`), invalid: true},
		{name: "trailing bytes", value: []byte(`{} {}`), invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			value, err := jsonValue(tc.value)
			if (err != nil) != tc.invalid {
				t.Fatalf("error = %v", err)
			}
			if tc.invalid {
				return
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.want {
				t.Fatalf("JSON = %s, want %s", data, tc.want)
			}
		})
	}
}
