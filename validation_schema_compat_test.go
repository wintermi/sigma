// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"testing"
)

// Generated tool schemas (zod, Pydantic, MCP servers) use ECMAScript patterns,
// tuple items, and patternProperties; valid calls must not be rejected as
// malformed schemas, and the constraints sigma can evaluate still apply.
func TestValidateToolCallAcceptsCommonGeneratedSchemaShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		schema    string
		arguments string
		wantPath  string
	}{
		{name: "lookahead pattern is an annotation", schema: `{"type":"object","properties":{"pw":{"type":"string","pattern":"^(?=.*[A-Z]).{8,}$"}}}`, arguments: `{"pw":"abcdefgh"}`},
		{name: "unicode escape pattern matches", schema: `{"type":"object","properties":{"s":{"type":"string","pattern":"^[\\u0041-\\u005A]+$"}}}`, arguments: `{"s":"ABC"}`},
		{name: "unicode escape pattern enforced", schema: `{"type":"object","properties":{"s":{"type":"string","pattern":"^[\\u0041-\\u005A]+$"}}}`, arguments: `{"s":"abc"}`, wantPath: "$.s"},
		{name: "tuple items", schema: `{"type":"object","properties":{"pt":{"type":"array","items":[{"type":"number"},{"type":"number"}]}}}`, arguments: `{"pt":[1,2]}`},
		{name: "tuple item enforced", schema: `{"type":"object","properties":{"pt":{"type":"array","items":[{"type":"number"},{"type":"number"}]}}}`, arguments: `{"pt":[1,"x"]}`, wantPath: "$.pt[1]"},
		{name: "tuple additional items rejected", schema: `{"type":"object","properties":{"pt":{"type":"array","items":[{"type":"number"}],"additionalItems":false}}}`, arguments: `{"pt":[1,2]}`, wantPath: "$.pt[1]"},
		{name: "pattern properties allowed", schema: `{"type":"object","patternProperties":{"^x-":{"type":"string"}},"additionalProperties":false}`, arguments: `{"x-foo":"bar"}`},
		{name: "pattern properties enforced", schema: `{"type":"object","patternProperties":{"^x-":{"type":"string"}},"additionalProperties":false}`, arguments: `{"x-foo":1}`, wantPath: "$.x-foo"},
		{name: "unmatched key still additional", schema: `{"type":"object","patternProperties":{"^x-":{"type":"string"}},"additionalProperties":false}`, arguments: `{"other":"bar"}`, wantPath: "$.other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tools := []Tool{{Name: "t", InputSchema: json.RawMessage(tt.schema)}}
			_, err := ValidateToolCall(tools, ToolCall{Name: "t", Arguments: json.RawMessage(tt.arguments)})
			if tt.wantPath == "" {
				if err != nil {
					t.Fatalf("valid call rejected: %v", err)
				}
				return
			}
			validationErr, ok := err.(*ToolValidationError)
			if !ok || validationErr.Path != tt.wantPath || validationErr.Reason == "schema is malformed" {
				t.Fatalf("error = %v, want a value violation at %s", err, tt.wantPath)
			}
		})
	}
}
