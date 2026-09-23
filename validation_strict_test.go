// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/toolschema"
)

func TestStrictNullabilityDerivationAndValidationAgree(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, property string
		preserve       bool
	}{
		{"unconstrained", `{}`, true},
		{"null type", `{"type":"null"}`, true},
		{"nullable type", `{"type":["string","null"]}`, true},
		{"enum excludes null", `{"type":["string","null"],"enum":["x"]}`, false},
		{"type excludes null", `{"type":"string","enum":["x",null]}`, false},
		{"const excludes null", `{"type":["string","null"],"const":"x"}`, false},
		{"const null", `{"const":null}`, true},
		{"enum and const", `{"enum":[null,"x"],"const":"x"}`, false},
		{"nullable union", `{"anyOf":[{"type":"string"},{"type":"null"}]}`, true},
		{"constrained union", `{"enum":["x"],"anyOf":[{"type":"string"},{"type":"null"}]}`, false},
		{"unconstrained union", `{"anyOf":[{"type":"string"},{}]}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			schema := json.RawMessage(`{"type":"object","properties":{"value":` + tt.property + `}}`)
			before := append([]byte(nil), schema...)
			original := map[string]any{"value": nil}
			tool := sigma.Tool{Name: "tool", InputSchema: schema, ProviderMetadata: map[string]any{"strict": true}}
			normalized, err := sigma.ValidateToolCall([]sigma.Tool{tool}, sigma.ToolCall{Name: "tool", Arguments: original})
			if err != nil {
				t.Fatal(err)
			}
			if _, present := normalized["value"]; present != tt.preserve {
				t.Fatalf("presence=%v, want %v", present, tt.preserve)
			}
			strict, err := toolschema.MakeStrict(schema)
			if err != nil {
				t.Fatal(err)
			}
			// The emitted strict schema must accept the provider's null placeholder.
			if _, err := sigma.ValidateToolCall([]sigma.Tool{{Name: "tool", InputSchema: strict}}, sigma.ToolCall{Name: "tool", Arguments: original}); err != nil {
				t.Fatalf("derived schema rejects placeholder: %v", err)
			}
			properties := strict["properties"].(map[string]any)
			var property map[string]any
			if err := json.Unmarshal([]byte(tt.property), &property); err != nil {
				t.Fatal(err)
			}
			if tt.preserve && !reflect.DeepEqual(properties["value"], property) {
				t.Fatal("nullable property was unnecessarily wrapped")
			}
			if !reflect.DeepEqual([]byte(schema), before) || !reflect.DeepEqual(original, map[string]any{"value": nil}) {
				t.Fatal("mutated caller input")
			}
		})
	}
}

func TestStrictNullNormalizationPreservesSchemaErrors(t *testing.T) {
	t.Parallel()
	for _, property := range []string{`{"type":12}`, `{"enum":"invalid"}`, `{"anyOf":12}`, `{"anyOf":[{},12]}`} {
		tool := sigma.Tool{Name: "tool", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":` + property + `}}`), ProviderMetadata: map[string]any{"strict": true}}
		_, err := sigma.ValidateToolCall([]sigma.Tool{tool}, sigma.ToolCall{Name: "tool", Arguments: map[string]any{"value": nil}})
		var validation *sigma.ToolValidationError
		if !errors.As(err, &validation) || validation.Reason != "schema is malformed" {
			t.Fatalf("%s: %v", property, err)
		}
	}
}
