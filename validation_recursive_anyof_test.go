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

func recursiveFilterTools() []sigma.Tool {
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
	return []sigma.Tool{{Name: "search", InputSchema: schema}}
}

func nestedOrFilter(leaf string, depth int) string {
	filter := leaf
	for range depth {
		filter = `{"op":"or","args":[` + filter + `]}`
	}
	return filter
}

// A recursive discriminated union must validate in time linear in the
// model-controlled nesting depth. Before the fix, depth 20 took seconds and
// each extra level doubled the time.
func TestValidateToolCallRecursiveAnyOfIsNotExponential(t *testing.T) {
	t.Parallel()

	tools := recursiveFilterTools()
	filter := nestedOrFilter(`{"op":"eq","field":"x"}`, 24)
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

// Coercion runs only when the arguments fail validation, so it must also stay
// polynomial for a recursive union whose deepest leaf needs coercing or cannot
// be coerced at all. Before the fix every branch was coerced into before its
// discriminator was checked, doubling the time per nesting level.
func TestCoerceToolCallRecursiveAnyOfIsNotExponential(t *testing.T) {
	t.Parallel()

	tools := recursiveFilterTools()
	options := sigma.ToolValidationOptions{CoercePrimitives: true}
	tests := []struct {
		name      string
		leaf      string
		wantError bool
	}{
		{name: "coercible leaf", leaf: `{"op":"eq","field":1}`},
		{name: "uncoercible leaf", leaf: `{"op":"eq","field":{}}`, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			call := sigma.ToolCall{Name: "search", Arguments: `{"filter":` + nestedOrFilter(tt.leaf, 24) + `}`}
			start := time.Now()
			args, err := sigma.ValidateToolCallWithOptions(tools, call, options)
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("ValidateToolCallWithOptions took %s for nesting depth 24", elapsed)
			}
			if tt.wantError {
				if err == nil {
					t.Fatal("ValidateToolCallWithOptions accepted an object field")
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateToolCallWithOptions returned error: %v", err)
			}
			node := args["filter"]
			for range 24 {
				node = node.(map[string]any)["args"].([]any)[0]
			}
			if field := node.(map[string]any)["field"]; field != "1" {
				t.Fatalf("leaf field = %#v, want coerced string %q", field, "1")
			}
		})
	}
}
