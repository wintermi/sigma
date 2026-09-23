// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestRegressionStrictNullPreservation(t *testing.T) {
	t.Parallel()
	tool := sigma.Tool{Name: "audit", InputSchema: sigma.Schema{"type": "object", "properties": map[string]any{"value": map[string]any{}}}, ProviderMetadata: map[string]any{"strict": true}}
	args, err := sigma.ValidateToolCall([]sigma.Tool{tool}, sigma.ToolCall{Name: "audit", Arguments: map[string]any{"value": nil}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := args["value"]; !ok {
		t.Fatal("strict normalization deleted a valid explicit null allowed by an unconstrained property schema")
	}
}

func TestRegressionStrictNullOmissionWithEnum(t *testing.T) {
	t.Parallel()
	tool := sigma.Tool{Name: "audit", InputSchema: sigma.Schema{"type": "object", "properties": map[string]any{"value": map[string]any{"type": []string{"string", "null"}, "enum": []string{"x"}}}}, ProviderMetadata: map[string]any{"strict": true}}
	args, err := sigma.ValidateToolCall([]sigma.Tool{tool}, sigma.ToolCall{Name: "audit", Arguments: map[string]any{"value": nil}})
	if err != nil {
		t.Fatalf("optional non-nullable value should become omission: %v", err)
	}
	if _, ok := args["value"]; ok {
		t.Fatal("placeholder null was retained")
	}
}
