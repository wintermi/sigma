// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestContainerKeywordsDoNotImplyTypes(t *testing.T) {
	t.Parallel()
	array := map[string]any{"items": map[string]any{"type": "number"}}
	object := map[string]any{"properties": map[string]any{"count": map[string]any{"type": "integer"}}, "required": []any{"count"}, "additionalProperties": false}
	union := map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "array"}}, "items": map[string]any{"type": "number"}}
	for _, tt := range []struct {
		name    string
		schema  map[string]any
		value   any
		invalid bool
	}{
		{"audit string", union, "all", false},
		{"audit array", union, []any{1, 2}, false},
		{"audit invalid items", union, []any{"bad"}, true},
		{"explicit union rejects number", union, 42, true},
		{"untyped array keyword accepts string", array, "all", false},
		{"untyped array rejects bad items", array, []any{"bad"}, true},
		{"untyped object accepts string", object, "all", false},
		{"untyped object accepts null", object, nil, false},
		{"untyped object validates properties", object, map[string]any{"count": 2}, false},
		{"untyped object requires properties", object, map[string]any{}, true},
		{"untyped object rejects extra properties", object, map[string]any{"count": 2, "extra": true}, true},
		{"untyped object rejects wrong property", object, map[string]any{"count": "bad"}, true},
		{"allOf", map[string]any{"allOf": []any{array, map[string]any{"type": "string"}}}, "all", false},
		{"oneOf", map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "array"}}, "items": map[string]any{"type": "number"}}, "all", false},
		{"reference siblings", map[string]any{"$ref": "#/$defs/union", "items": map[string]any{"type": "number"}}, "all", false},
		{"reference sibling rejects items", map[string]any{"$ref": "#/$defs/union", "items": map[string]any{"type": "number"}}, []any{"bad"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			schema := sigma.Schema{"type": "object", "properties": map[string]any{"value": tt.schema}, "$defs": map[string]any{"union": map[string]any{"type": []any{"string", "array"}}}}
			args := map[string]any{"value": tt.value}
			before, schemaBefore := mustJSON(t, args), mustJSON(t, schema)
			for _, coerce := range []bool{false, true} {
				_, err := sigma.ValidateToolCallWithOptions([]sigma.Tool{{Name: "check", InputSchema: schema}}, sigma.ToolCall{Name: "check", Arguments: args}, sigma.ToolValidationOptions{CoercePrimitives: coerce})
				// Explicit primitive coercion can turn a number into a string.
				invalid := tt.invalid && !(coerce && tt.name == "explicit union rejects number")
				if (err != nil) != invalid {
					t.Fatalf("coerce=%t err=%v, invalid=%t", coerce, err, invalid)
				}
			}
			if mustJSON(t, args) != before || mustJSON(t, schema) != schemaBefore {
				t.Fatal("caller input mutated")
			}
		})
	}
}

func TestUntypedContainerCoercionAndNullableProperties(t *testing.T) {
	t.Parallel()
	schema := sigma.Schema{"type": "object", "properties": map[string]any{
		"array":    map[string]any{"items": map[string]any{"type": "integer"}},
		"object":   map[string]any{"properties": map[string]any{"enabled": map[string]any{"type": "boolean"}}},
		"nullable": map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}},
	}}
	args := map[string]any{"array": []any{"42"}, "object": map[string]any{"enabled": "true"}, "nullable": nil}
	before := mustJSON(t, args)
	tools := []sigma.Tool{{Name: "check", InputSchema: schema}}
	call := sigma.ToolCall{Name: "check", Arguments: args}
	if _, err := sigma.ValidateToolCall(tools, call); err == nil {
		t.Fatal("coercion enabled by default")
	}
	result, err := sigma.ValidateToolCallWithOptions(tools, call, sigma.ToolValidationOptions{CoercePrimitives: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, result); got != `{"array":[42],"nullable":null,"object":{"enabled":true}}` {
		t.Fatalf("coercion = %s", got)
	}
	if mustJSON(t, args) != before {
		t.Fatal("caller arguments mutated")
	}
}

func TestStrictOptionalNullWithUntypedContainers(t *testing.T) {
	t.Parallel()
	schema := sigma.Schema{"type": "object", "properties": map[string]any{
		"object":    map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}},
		"array":     map[string]any{"items": map[string]any{"type": "number"}},
		"explicit":  map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
		"composed":  map[string]any{"anyOf": []any{map[string]any{"type": "null"}, map[string]any{"type": "array"}}, "items": map[string]any{"type": "number"}},
		"reference": map[string]any{"$ref": "#/$defs/value", "items": map[string]any{"type": "number"}},
	}, "$defs": map[string]any{"value": map[string]any{"type": []any{"null", "array"}}}}
	args := map[string]any{"object": nil, "array": nil, "explicit": nil, "composed": nil, "reference": nil}
	before := mustJSON(t, args)
	result, err := sigma.ValidateToolCall([]sigma.Tool{{Name: "check", InputSchema: schema, ProviderMetadata: map[string]any{"strict": true}}}, sigma.ToolCall{Name: "check", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, result); got != `{"array":null,"composed":null,"object":null,"reference":null}` {
		t.Fatalf("null normalization = %s", got)
	}
	if mustJSON(t, args) != before {
		t.Fatal("caller arguments mutated")
	}
}
