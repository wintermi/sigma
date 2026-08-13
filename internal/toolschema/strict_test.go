// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package toolschema

import (
	"reflect"
	"strings"
	"testing"
)

func TestMakeStrictDerivesClosedSchemaWithoutMutation(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"type":  "object",
		"title": "LookupInput",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string"},
			"offset": map[string]any{"type": "number"},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"enabled": map[string]any{"type": "boolean"},
				},
			},
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": map[string]any{"label": map[string]any{"type": "string"}},
					"required":   []any{"label"},
				},
			},
			"nullable": map[string]any{
				"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}},
			},
		},
		"required": []any{"path", "metadata"},
	}
	original := cloneMap(t, schema)

	strict, err := MakeStrict(schema)
	if err != nil {
		t.Fatalf("MakeStrict returned error: %v", err)
	}
	if !reflect.DeepEqual(schema, original) {
		t.Fatalf("MakeStrict mutated input:\n got: %#v\nwant: %#v", schema, original)
	}
	if got, want := strict["required"], []string{"items", "metadata", "nullable", "offset", "path"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root required = %#v, want %#v", got, want)
	}
	if got := strict["additionalProperties"]; got != false {
		t.Fatalf("root additionalProperties = %#v, want false", got)
	}

	properties := strict["properties"].(map[string]any)
	offset := properties["offset"].(map[string]any)["anyOf"].([]any)
	if got, want := offset[0], map[string]any{"type": "number"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("offset value schema = %#v, want %#v", got, want)
	}
	if got, want := offset[1], map[string]any{"type": "null"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("offset null schema = %#v, want %#v", got, want)
	}

	metadata := properties["metadata"].(map[string]any)
	if got, want := metadata["required"], []string{"enabled"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nested required = %#v, want %#v", got, want)
	}
	if metadata["additionalProperties"] != false {
		t.Fatalf("nested additionalProperties = %#v, want false", metadata["additionalProperties"])
	}
	itemsUnion := properties["items"].(map[string]any)["anyOf"].([]any)
	itemObject := itemsUnion[0].(map[string]any)["items"].(map[string]any)
	if itemObject["additionalProperties"] != false {
		t.Fatalf("array item additionalProperties = %#v, want false", itemObject["additionalProperties"])
	}
	if got := properties["nullable"].(map[string]any)["anyOf"].([]any); len(got) != 2 {
		t.Fatalf("nullable variants = %#v, want existing two variants", got)
	}
}

func TestMakeStrictRejectsUnsafeSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema any
		want   string
	}{
		{name: "boolean root", schema: true, want: "root schema must be an object"},
		{name: "non object root", schema: map[string]any{"type": "string"}, want: "root schema must have type object"},
		{name: "reference", schema: objectWithProperty(map[string]any{"$ref": "#/$defs/value"}), want: "$ref schemas are unsupported"},
		{name: "definitions", schema: map[string]any{"type": "object", "$defs": map[string]any{}}, want: "$defs schemas are unsupported"},
		{name: "all of", schema: objectWithProperty(map[string]any{"allOf": []any{map[string]any{"type": "string"}}}), want: "allOf schemas are unsupported"},
		{name: "one of", schema: objectWithProperty(map[string]any{"oneOf": []any{map[string]any{"type": "string"}}}), want: "oneOf schemas are unsupported"},
		{name: "structured any of", schema: objectWithProperty(map[string]any{"anyOf": []any{map[string]any{"type": "object"}, map[string]any{"type": "null"}}}), want: "object and array unions are unsupported"},
		{name: "tuple", schema: objectWithProperty(map[string]any{"type": "array", "items": []any{map[string]any{"type": "string"}}}), want: "tuple schemas are unsupported"},
		{name: "conditional", schema: objectWithProperty(map[string]any{"type": "string", "if": map[string]any{"const": "x"}}), want: "if schemas are unsupported"},
		{name: "pattern properties", schema: map[string]any{"type": "object", "patternProperties": map[string]any{}}, want: "patternProperties schemas are unsupported"},
		{name: "true additional properties", schema: map[string]any{"type": "object", "additionalProperties": true}, want: "additionalProperties is unsupported"},
		{name: "schema additional properties", schema: map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}, want: "additionalProperties is unsupported"},
		{name: "properties without object", schema: map[string]any{"type": "string", "properties": map[string]any{}}, want: "properties require type object"},
		{name: "malformed properties", schema: map[string]any{"type": "object", "properties": []any{}}, want: "properties must be a schema map"},
		{name: "malformed required", schema: map[string]any{"type": "object", "required": "value"}, want: "required must be a string array"},
		{name: "null required", schema: map[string]any{"type": "object", "required": nil}, want: "required must be a string array"},
		{name: "unknown required", schema: map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{"missing"}}, want: "required contains unknown property"},
		{name: "multiple JSON values", schema: []byte(`{"type":"object"} {"type":"object"}`), want: "multiple JSON values"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := MakeStrict(tt.schema)
			if err == nil {
				t.Fatal("MakeStrict returned nil error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MakeStrict error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestMakeStrictDefaultsNilSchemaToClosedObject(t *testing.T) {
	t.Parallel()

	strict, err := MakeStrict(nil)
	if err != nil {
		t.Fatalf("MakeStrict returned error: %v", err)
	}
	want := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"required":             []string{},
		"additionalProperties": false,
	}
	if !reflect.DeepEqual(strict, want) {
		t.Fatalf("strict schema = %#v, want %#v", strict, want)
	}
}

func objectWithProperty(property map[string]any) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"value": property},
	}
}

func cloneMap(t *testing.T, input map[string]any) map[string]any {
	t.Helper()

	decoded, err := decode(input)
	if err != nil {
		t.Fatalf("decode returned error: %v", err)
	}
	return decoded.(map[string]any)
}
