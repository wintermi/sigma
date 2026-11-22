// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestStrictToolSchemaRejectsKeywordsClaudeRejects(t *testing.T) {
	t.Parallel()
	object := func(property map[string]any) sigma.Schema {
		return sigma.Schema{"type": "object", "properties": map[string]any{"value": property}, "required": []any{"value"}}
	}
	tests := []struct {
		name   string
		schema sigma.Schema
		reject string
	}{
		{name: "numeric bound", schema: object(map[string]any{"type": "integer", "minimum": 1}), reject: "minimum"},
		{name: "nested in items", schema: object(map[string]any{"type": "array", "items": map[string]any{"type": "number", "multipleOf": 5}}), reject: "multipleOf"},
		{name: "nested in anyOf", schema: object(map[string]any{"anyOf": []any{map[string]any{"type": "string", "maxLength": 3}, map[string]any{"type": "integer", "maximum": 9}}}), reject: "maximum"},
		{name: "min items above one", schema: object(map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 2}), reject: "minItems"},
		{name: "unsupported format", schema: object(map[string]any{"type": "string", "format": "regex"}), reject: "format"},
		{name: "min items of one", schema: object(map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1})},
		{name: "supported format", schema: object(map[string]any{"type": "string", "format": "email"})},
		{name: "property named like a keyword", schema: sigma.Schema{"type": "object", "properties": map[string]any{"minimum": map[string]any{"type": "integer"}}, "required": []any{"minimum"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := anthropicToolInputSchema(sigma.Tool{Name: "lookup", InputSchema: tt.schema}, true)
			if tt.reject == "" {
				if err != nil {
					t.Fatalf("strict schema error = %v, want none", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.reject+" keyword") {
				t.Fatalf("strict schema error = %v, want %s rejected", err, tt.reject)
			}
			if _, err := anthropicToolInputSchema(sigma.Tool{Name: "lookup", InputSchema: tt.schema}, false); err != nil {
				t.Fatalf("non-strict schema error = %v, want none", err)
			}
		})
	}
}
