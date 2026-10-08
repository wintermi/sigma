// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidationReportsFirstPropertyErrorDeterministically(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
		want   string
	}{
		{
			name:   "invalid values",
			schema: `{"type":"object","properties":{"d":{"type":"string"},"c":{"type":"string"},"b":{"type":"string"},"a":{"type":"string"}}}`,
			want:   "$.a",
		},
		{
			name:   "malformed property schemas",
			schema: `{"type":"object","properties":{"d":1,"c":1,"b":1,"a":1}}`,
			want:   `"a"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tools := []Tool{{Name: "test", InputSchema: json.RawMessage(tt.schema)}}
			call := ToolCall{Name: "test", Arguments: json.RawMessage(`{"a":1,"b":1,"c":1,"d":1}`)}
			for range 50 {
				_, err := ValidateToolCall(tools, call)
				var validationErr *ToolValidationError
				if !errors.As(err, &validationErr) {
					t.Fatalf("error = %v, want ToolValidationError", err)
				}
				got := validationErr.Path
				if validationErr.Reason == "schema is malformed" {
					got = validationErr.Err.Error()
				}
				if got != tt.want && !strings.Contains(got, tt.want) {
					t.Fatalf("reported %q, want first property %s", got, tt.want)
				}
			}
		})
	}
}
