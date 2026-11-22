// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

// Size, uniqueness, exclusive-bound, and multiple constraints are enforced, so
// a model's out-of-range arguments are sent back for repair instead of being
// passed to the tool.
func TestValidateToolCallEnforcesSizeAndNumericKeywords(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, schema, value string
		valid               bool
	}{
		{"minItems met", `{"type":"array","minItems":1}`, `["a"]`, true},
		{"minItems missed", `{"type":"array","minItems":1}`, `[]`, false},
		{"maxItems exceeded", `{"type":"array","maxItems":1}`, `["a","b"]`, false},
		{"uniqueItems distinct", `{"type":"array","uniqueItems":true}`, `[1,"1",{"a":1}]`, true},
		{"uniqueItems duplicate numbers", `{"type":"array","uniqueItems":true}`, `[1,1.0]`, false},
		{"uniqueItems duplicate objects", `{"type":"array","uniqueItems":true}`, `[{"a":1},{"a":1}]`, false},
		{"prefixItems positions", `{"type":"array","prefixItems":[{"type":"string"},{"type":"integer"}]}`, `["a",1,true]`, true},
		{"prefixItems wrong type", `{"type":"array","prefixItems":[{"type":"string"},{"type":"integer"}]}`, `["a","b"]`, false},
		{"prefixItems closed by items", `{"type":"array","prefixItems":[{"type":"string"}],"items":false}`, `["a","b"]`, false},
		{"minProperties missed", `{"type":"object","minProperties":1}`, `{}`, false},
		{"maxProperties exceeded", `{"type":"object","maxProperties":1}`, `{"a":1,"b":2}`, false},
		{"exclusiveMinimum equal", `{"type":"number","exclusiveMinimum":0}`, `0`, false},
		{"exclusiveMinimum above", `{"type":"number","exclusiveMinimum":0}`, `0.0001`, true},
		{"exclusiveMaximum equal", `{"type":"number","exclusiveMaximum":10}`, `10`, false},
		{"draft-04 exclusive minimum", `{"type":"number","minimum":0,"exclusiveMinimum":true}`, `0`, false},
		{"multipleOf decimal", `{"type":"number","multipleOf":0.1}`, `0.3`, true},
		{"multipleOf missed", `{"type":"integer","multipleOf":5}`, `12`, false},
		{"multipleOf large integer", `{"type":"integer","multipleOf":3}`, `123456789012345678901234567890`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var value any
			decoder := json.NewDecoder(strings.NewReader(tt.value))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				t.Fatal(err)
			}
			tool := sigma.Tool{Name: "tool", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":` + tt.schema + `},"required":["value"]}`)}
			_, err := sigma.ValidateToolCall([]sigma.Tool{tool}, sigma.ToolCall{Name: "tool", Arguments: map[string]any{"value": value}})
			if (err == nil) != tt.valid {
				t.Fatalf("ValidateToolCall error = %v, want valid %v", err, tt.valid)
			}
		})
	}
}
