// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

// A recursive discriminated union must validate in time linear in the
// model-controlled nesting depth. Before the fix, depth 20 took seconds and
// each extra level doubled the time.
func TestValidateToolCallRecursiveAnyOfIsNotExponential(t *testing.T) {
	t.Parallel()

	schema := sigma.Schema{
		"type":       "object",
		"properties": map[string]any{"filter": map[string]any{"$ref": "#/$defs/filter"}},
		"$defs": map[string]any{"filter": map[string]any{"anyOf": []any{
			map[string]any{"type": "object", "required": []any{"op", "args"}, "properties": map[string]any{
				"op":   map[string]any{"const": "and"},
				"args": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/filter"}},
			}},
			map[string]any{"type": "object", "required": []any{"op", "args"}, "properties": map[string]any{
				"op":   map[string]any{"const": "or"},
				"args": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/filter"}},
			}},
			map[string]any{"type": "object", "required": []any{"op", "field"}, "properties": map[string]any{
				"op":    map[string]any{"const": "eq"},
				"field": map[string]any{"type": "string"},
			}},
		}}},
	}
	filter := `{"op":"eq","field":"x"}`
	for range 24 {
		filter = `{"op":"or","args":[` + filter + `]}`
	}
	tools := []sigma.Tool{{Name: "search", InputSchema: schema}}
	call := sigma.ToolCall{Name: "search", Arguments: `{"filter":` + filter + `}`}

	for _, options := range []sigma.ToolValidationOptions{{}, {CoercePrimitives: true}} {
		start := time.Now()
		if _, err := sigma.ValidateToolCallWithOptions(tools, call, options); err != nil {
			t.Fatalf("ValidateToolCallWithOptions(%+v) returned error: %v", options, err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("ValidateToolCallWithOptions(%+v) took %s for nesting depth 24", options, elapsed)
		}
	}

	invalid := sigma.ToolCall{Name: "search", Arguments: `{"filter":` + strings.Replace(filter, `"field":"x"`, `"field":1`, 1) + `}`}
	start := time.Now()
	if _, err := sigma.ValidateToolCall(tools, invalid); err == nil {
		t.Fatal("ValidateToolCall accepted a non-string field")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("rejecting the invalid filter took %s", elapsed)
	}
}
