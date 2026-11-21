// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

func TestGoogleToolSchemaRewritesConstAsEnum(t *testing.T) {
	t.Parallel()

	schema := sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"units": map[string]any{"type": "string", "const": "celsius"},
			"const": map[string]any{"type": "integer", "const": 2},
			"shape": map[string]any{"anyOf": []any{
				map[string]any{"properties": map[string]any{"kind": map[string]any{"const": "circle"}}},
				map[string]any{"properties": map[string]any{"kind": map[string]any{"const": true}}},
			}},
			"note": map[string]any{"type": "string", "default": map[string]any{"const": "literal"}},
			"pick": map[string]any{"const": "a", "enum": []any{"a", "b"}},
		},
	}
	tools, err := googleTools(sigma.ProviderGoogle, []sigma.Tool{{Name: "lookup", InputSchema: schema}}, sigma.Options{})
	if err != nil {
		t.Fatalf("googleTools returned error: %v", err)
	}
	parameters := tools[0]["functionDeclarations"].([]map[string]any)[0]["parametersJsonSchema"].(map[string]any)
	properties := parameters["properties"].(map[string]any)

	for name, want := range map[string]map[string]any{
		"units": {"type": "string", "enum": []any{"celsius"}},
		"const": {"type": "integer", "enum": []any{json.Number("2")}},
		"note":  {"type": "string", "default": map[string]any{"const": "literal"}},
		"pick":  {"const": "a", "enum": []any{"a", "b"}},
	} {
		if got := properties[name]; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %#v, want %#v", name, got, want)
		}
	}
	variants := properties["shape"].(map[string]any)["anyOf"].([]any)
	circle := variants[0].(map[string]any)["properties"].(map[string]any)["kind"]
	if want := map[string]any{"enum": []any{"circle"}}; !reflect.DeepEqual(circle, want) {
		t.Fatalf("circle kind = %#v, want %#v", circle, want)
	}
	boolean := variants[1].(map[string]any)["properties"].(map[string]any)["kind"]
	if want := map[string]any{"const": true}; !reflect.DeepEqual(boolean, want) {
		t.Fatalf("boolean kind = %#v, want const kept", boolean)
	}
	if got := schema["properties"].(map[string]any)["units"].(map[string]any)["const"]; got != "celsius" {
		t.Fatalf("caller schema mutated: units const = %#v", got)
	}
}
