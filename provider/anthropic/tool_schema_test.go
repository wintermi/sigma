// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

func TestAnthropicToolInputSchemaIsRootObject(t *testing.T) {
	t.Parallel()

	properties := map[string]any{"mode": map[string]any{"$ref": "#/$defs/mode"}}
	defs := map[string]any{"mode": map[string]any{"type": "string"}}
	for _, tt := range []struct {
		name   string
		schema any
		want   map[string]any
	}{
		{
			name: "root oneOf and description are dropped",
			schema: sigma.Schema{
				"description": "Pick one",
				"properties":  properties,
				"required":    []any{"mode"},
				"oneOf":       []any{map[string]any{"required": []any{"mode"}}},
				"$defs":       defs,
			},
			want: map[string]any{"type": "object", "properties": properties, "required": []any{"mode"}, "$defs": defs},
		},
		{
			name:   "missing root type",
			schema: sigma.Schema{"properties": properties, "additionalProperties": false},
			want:   map[string]any{"type": "object", "properties": properties, "additionalProperties": false},
		},
		{
			name: "nil schema",
			want: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := anthropicToolInputSchema(sigma.Tool{Name: "pick", InputSchema: tt.schema}, false)
			if err != nil {
				t.Fatalf("anthropicToolInputSchema returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("input_schema = %#v, want %#v", got, tt.want)
			}
		})
	}
}
