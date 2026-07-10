// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestValidateToolCallValidatesNestedSchemaAndReturnsDecodedCopy(t *testing.T) {
	t.Parallel()

	schema := sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"city":  map[string]any{"type": "string", "minLength": 2, "maxLength": 64},
			"units": map[string]any{"type": "string", "enum": []any{"metric", "imperial"}},
			"days": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"date": map[string]any{"type": "string"},
						"high": map[string]any{"type": "number", "minimum": -90, "maximum": 80},
					},
					"required":             []any{"date", "high"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []any{"city", "units", "days"},
		"additionalProperties": false,
	}
	args := map[string]any{
		"city":  "Melbourne",
		"units": "metric",
		"days": []any{
			map[string]any{"date": "2026-05-25", "high": 18},
		},
	}
	schemaBefore := mustJSON(t, schema)
	argsBefore := mustJSON(t, args)

	decoded, err := sigma.ValidateToolCall(
		[]sigma.Tool{{Name: "weather", InputSchema: schema}},
		sigma.ToolCall{Name: "weather", Arguments: args},
	)
	if err != nil {
		t.Fatalf("ValidateToolCall returned error: %v", err)
	}
	if got, want := decoded["city"], "Melbourne"; got != want {
		t.Fatalf("decoded city = %v, want %v", got, want)
	}

	decoded["city"] = "Sydney"
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("input arguments were mutated: got %v want %v", got, want)
	}
	if got := mustJSON(t, schema); got != schemaBefore {
		t.Fatalf("schema mutated:\nbefore %s\nafter  %s", schemaBefore, got)
	}
	if got := mustJSON(t, args); got != argsBefore {
		t.Fatalf("arguments mutated:\nbefore %s\nafter  %s", argsBefore, got)
	}
}

func TestValidateToolCallReportsValidationFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tools      []sigma.Tool
		call       sigma.ToolCall
		path       string
		reason     string
		expected   string
		errorText  string
		actualText string
	}{
		{
			name:      "unknown tool",
			tools:     []sigma.Tool{{Name: "weather", InputSchema: sigma.Schema{"type": "object"}}},
			call:      sigma.ToolCall{Name: "search", Arguments: map[string]any{}},
			path:      "$",
			reason:    "tool is not registered",
			expected:  "registered tool name",
			errorText: "tool=search",
		},
		{
			name: "missing required field",
			tools: []sigma.Tool{{
				Name:        "weather",
				InputSchema: sigma.Schema{"type": "object", "required": []any{"city"}},
			}},
			call:       sigma.ToolCall{Name: "weather", Arguments: map[string]any{}},
			path:       "$.city",
			reason:     "missing required field",
			expected:   "required property",
			actualText: "missing",
		},
		{
			name: "wrong primitive type",
			tools: []sigma.Tool{{
				Name: "weather",
				InputSchema: sigma.Schema{
					"type":       "object",
					"properties": map[string]any{"city": map[string]any{"type": "string"}},
				},
			}},
			call:       sigma.ToolCall{Name: "weather", Arguments: map[string]any{"city": 42}},
			path:       "$.city",
			reason:     "wrong primitive type",
			expected:   "string",
			actualText: "42",
		},
		{
			name: "enum violation",
			tools: []sigma.Tool{{
				Name: "weather",
				InputSchema: sigma.Schema{
					"type":       "object",
					"properties": map[string]any{"units": map[string]any{"type": "string", "enum": []any{"metric", "imperial"}}},
				},
			}},
			call:       sigma.ToolCall{Name: "weather", Arguments: map[string]any{"units": "kelvin"}},
			path:       "$.units",
			reason:     "enum violation",
			expected:   `one of ["metric", "imperial"]`,
			actualText: `"kelvin"`,
		},
		{
			name: "nested object error",
			tools: []sigma.Tool{{
				Name: "plot",
				InputSchema: sigma.Schema{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":       "object",
							"properties": map[string]any{"lat": map[string]any{"type": "number"}},
						},
					},
				},
			}},
			call:     sigma.ToolCall{Name: "plot", Arguments: map[string]any{"location": map[string]any{"lat": "south"}}},
			path:     "$.location.lat",
			reason:   "wrong primitive type",
			expected: "number",
		},
		{
			name: "array item error",
			tools: []sigma.Tool{{
				Name: "tag",
				InputSchema: sigma.Schema{
					"type":       "object",
					"properties": map[string]any{"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
				},
			}},
			call:     sigma.ToolCall{Name: "tag", Arguments: map[string]any{"tags": []any{"ok", 9}}},
			path:     "$.tags[1]",
			reason:   "wrong primitive type",
			expected: "string",
		},
		{
			name: "numeric maximum",
			tools: []sigma.Tool{{
				Name: "scale",
				InputSchema: sigma.Schema{
					"type":       "object",
					"properties": map[string]any{"count": map[string]any{"type": "integer", "maximum": 5}},
				},
			}},
			call:     sigma.ToolCall{Name: "scale", Arguments: map[string]any{"count": 6}},
			path:     "$.count",
			reason:   "number is above maximum",
			expected: "<= 5",
		},
		{
			name: "string length",
			tools: []sigma.Tool{{
				Name: "label",
				InputSchema: sigma.Schema{
					"type":       "object",
					"properties": map[string]any{"name": map[string]any{"type": "string", "maxLength": 3}},
				},
			}},
			call:     sigma.ToolCall{Name: "label", Arguments: map[string]any{"name": "long"}},
			path:     "$.name",
			reason:   "string is too long",
			expected: "length <= 3",
		},
		{
			name: "string pattern",
			tools: []sigma.Tool{{
				Name: "label",
				InputSchema: sigma.Schema{
					"type":       "object",
					"properties": map[string]any{"code": map[string]any{"type": "string", "pattern": `^[A-Z]{3}-\d{3}$`}},
				},
			}},
			call:     sigma.ToolCall{Name: "label", Arguments: map[string]any{"code": "abc-123"}},
			path:     "$.code",
			reason:   "pattern violation",
			expected: `match pattern ^[A-Z]{3}-\d{3}$`,
		},
		{
			name: "not schema",
			tools: []sigma.Tool{{
				Name: "label",
				InputSchema: sigma.Schema{
					"type":       "object",
					"properties": map[string]any{"status": map[string]any{"type": "string", "not": map[string]any{"enum": []any{"disabled"}}}},
				},
			}},
			call:     sigma.ToolCall{Name: "label", Arguments: map[string]any{"status": "disabled"}},
			path:     "$.status",
			reason:   "not violation",
			expected: "value not matching schema",
		},
		{
			name: "additional property",
			tools: []sigma.Tool{{
				Name: "weather",
				InputSchema: sigma.Schema{
					"type":                 "object",
					"properties":           map[string]any{"city": map[string]any{"type": "string"}},
					"additionalProperties": false,
				},
			}},
			call:     sigma.ToolCall{Name: "weather", Arguments: map[string]any{"city": "Melbourne", "secret": "sk-live-secret"}},
			path:     "$.secret",
			reason:   "additional property is not allowed",
			expected: "declared property",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := sigma.ValidateToolCall(tt.tools, tt.call)
			if err == nil {
				t.Fatal("ValidateToolCall returned nil error")
			}
			if !errors.Is(err, sigma.ErrToolValidation) {
				t.Fatalf("error does not match ErrToolValidation: %v", err)
			}

			var validationErr *sigma.ToolValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error type = %T, want ToolValidationError", err)
			}
			if validationErr.Path != tt.path {
				t.Fatalf("path = %q, want %q", validationErr.Path, tt.path)
			}
			if validationErr.Reason != tt.reason {
				t.Fatalf("reason = %q, want %q", validationErr.Reason, tt.reason)
			}
			if validationErr.Expected != tt.expected {
				t.Fatalf("expected = %q, want %q", validationErr.Expected, tt.expected)
			}
			if tt.actualText != "" && validationErr.Actual != tt.actualText {
				t.Fatalf("actual = %q, want %q", validationErr.Actual, tt.actualText)
			}
			if tt.errorText != "" && !strings.Contains(err.Error(), tt.errorText) {
				t.Fatalf("error text = %q, want substring %q", err.Error(), tt.errorText)
			}
			if strings.Contains(err.Error(), "sk-live-secret") {
				t.Fatalf("error leaked secret: %q", err.Error())
			}
		})
	}
}

func TestValidateToolCallValidatesPatternAndNotSchemas(t *testing.T) {
	t.Parallel()

	schema := sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{
				"type":    "string",
				"pattern": `^[A-Z]{3}-\d{3}$`,
			},
			"tags": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":    "string",
					"pattern": `^[a-z]+$`,
				},
			},
			"labels": map[string]any{
				"type": "object",
				"additionalProperties": map[string]any{
					"allOf": []any{
						map[string]any{"type": "string", "pattern": `^[a-z]+$`},
						map[string]any{"not": map[string]any{"enum": []any{"secret"}}},
					},
				},
			},
			"mode": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string", "pattern": `^auto$`},
					map[string]any{"type": "string", "pattern": `^manual$`},
				},
			},
		},
		"required": []any{"code", "tags", "labels", "mode"},
	}
	args := map[string]any{
		"code":   "ABC-123",
		"tags":   []any{"alpha", "beta"},
		"labels": map[string]any{"tier": "public"},
		"mode":   "manual",
	}
	schemaBefore := mustJSON(t, schema)
	argsBefore := mustJSON(t, args)

	decoded, err := sigma.ValidateToolCall(
		[]sigma.Tool{{Name: "classify", InputSchema: schema}},
		sigma.ToolCall{Name: "classify", Arguments: args},
	)
	if err != nil {
		t.Fatalf("ValidateToolCall returned error: %v", err)
	}
	if got, want := decoded["code"], "ABC-123"; got != want {
		t.Fatalf("code = %v, want %v", got, want)
	}
	if got := mustJSON(t, schema); got != schemaBefore {
		t.Fatalf("schema mutated:\nbefore %s\nafter  %s", schemaBefore, got)
	}
	if got := mustJSON(t, args); got != argsBefore {
		t.Fatalf("arguments mutated:\nbefore %s\nafter  %s", argsBefore, got)
	}
}

func TestValidateToolCallAcceptsRawJSONArguments(t *testing.T) {
	t.Parallel()

	decoded, err := sigma.ValidateToolCall(
		[]sigma.Tool{{
			Name:        "weather",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
		}},
		sigma.ToolCall{Name: "weather", Arguments: json.RawMessage(`{"city":"Melbourne"}`)},
	)
	if err != nil {
		t.Fatalf("ValidateToolCall returned error: %v", err)
	}
	if got, want := decoded["city"], "Melbourne"; got != want {
		t.Fatalf("city = %v, want %v", got, want)
	}
}

func TestValidateToolCallKeepsPrimitiveValidationStrictByDefault(t *testing.T) {
	t.Parallel()

	_, err := sigma.ValidateToolCall(
		[]sigma.Tool{{
			Name: "scale",
			InputSchema: sigma.Schema{
				"type":       "object",
				"properties": map[string]any{"count": map[string]any{"type": "integer"}},
			},
		}},
		sigma.ToolCall{Name: "scale", Arguments: map[string]any{"count": "42"}},
	)
	if err == nil {
		t.Fatal("ValidateToolCall returned nil error")
	}
	if !errors.Is(err, sigma.ErrToolValidation) {
		t.Fatalf("error does not match ErrToolValidation: %v", err)
	}
	var validationErr *sigma.ToolValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error type = %T, want ToolValidationError", err)
	}
	if got, want := validationErr.Reason, "wrong primitive type"; got != want {
		t.Fatalf("reason = %q, want %q", got, want)
	}
}

func TestValidateToolCallWithOptionsCoercesPrimitiveArguments(t *testing.T) {
	t.Parallel()

	schema := sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{
				"type":    "integer",
				"enum":    []any{42},
				"minimum": 1,
			},
			"ratio":    map[string]any{"type": "number"},
			"enabled":  map[string]any{"type": "boolean"},
			"disabled": map[string]any{"type": "boolean"},
			"empty":    map[string]any{"type": "null"},
			"label":    map[string]any{"type": "string", "pattern": `^true$`},
			"items":    map[string]any{"type": "array", "items": map[string]any{"type": "boolean"}},
			"extra": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "number"},
			},
			"existingUnion": map[string]any{"type": []any{"number", "string"}},
			"coercedUnion":  map[string]any{"type": []any{"boolean", "number"}},
		},
		"required": []any{"count", "ratio", "enabled", "disabled", "empty", "label", "items", "extra", "existingUnion", "coercedUnion"},
	}
	args := map[string]any{
		"count":         "42",
		"ratio":         true,
		"enabled":       "true",
		"disabled":      0,
		"empty":         "",
		"label":         true,
		"items":         []any{"false", 1},
		"extra":         map[string]any{"score": "9.5"},
		"existingUnion": "1",
		"coercedUnion":  "1",
	}
	schemaBefore := mustJSON(t, schema)
	argsBefore := mustJSON(t, args)

	decoded, err := sigma.ValidateToolCallWithOptions(
		[]sigma.Tool{{Name: "coerce", InputSchema: schema}},
		sigma.ToolCall{Name: "coerce", Arguments: args},
		sigma.ToolValidationOptions{CoercePrimitives: true},
	)
	if err != nil {
		t.Fatalf("ValidateToolCallWithOptions returned error: %v", err)
	}

	want := map[string]any{
		"count":         42,
		"ratio":         1,
		"enabled":       true,
		"disabled":      false,
		"empty":         nil,
		"label":         "true",
		"items":         []any{false, true},
		"extra":         map[string]any{"score": 9.5},
		"existingUnion": "1",
		"coercedUnion":  1,
	}
	if gotJSON, wantJSON := mustJSON(t, decoded), mustJSON(t, want); gotJSON != wantJSON {
		t.Fatalf("decoded arguments = %s, want %s", gotJSON, wantJSON)
	}
	if got := mustJSON(t, schema); got != schemaBefore {
		t.Fatalf("schema mutated:\nbefore %s\nafter  %s", schemaBefore, got)
	}
	if got := mustJSON(t, args); got != argsBefore {
		t.Fatalf("arguments mutated:\nbefore %s\nafter  %s", argsBefore, got)
	}
}

func TestValidateToolCallWithOptionsCoercesComposedSchemas(t *testing.T) {
	t.Parallel()

	schema := sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"all": map[string]any{
				"allOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "string", "pattern": `^true$`},
				},
			},
			"any": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "integer", "minimum": 40},
					map[string]any{"type": "boolean"},
				},
			},
			"one": map[string]any{
				"oneOf": []any{
					map[string]any{"type": "boolean"},
					map[string]any{"type": "integer", "minimum": 1},
				},
			},
		},
		"required": []any{"all", "any", "one"},
	}

	decoded, err := sigma.ValidateToolCallWithOptions(
		[]sigma.Tool{{Name: "compose", InputSchema: schema}},
		sigma.ToolCall{Name: "compose", Arguments: map[string]any{
			"all": true,
			"any": "42",
			"one": "false",
		}},
		sigma.ToolValidationOptions{CoercePrimitives: true},
	)
	if err != nil {
		t.Fatalf("ValidateToolCallWithOptions returned error: %v", err)
	}

	want := map[string]any{"all": "true", "any": 42, "one": false}
	if gotJSON, wantJSON := mustJSON(t, decoded), mustJSON(t, want); gotJSON != wantJSON {
		t.Fatalf("decoded arguments = %s, want %s", gotJSON, wantJSON)
	}
}

func TestValidateToolCallWithOptionsPreservesValidUnionValues(t *testing.T) {
	t.Parallel()

	schema := sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"any": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "integer"},
					map[string]any{"type": "string"},
				},
			},
			"one": map[string]any{
				"oneOf": []any{
					map[string]any{"type": "integer"},
					map[string]any{"type": "string"},
				},
			},
			"coercedAny": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "integer"},
					map[string]any{"type": "boolean"},
				},
			},
			"coercedOne": map[string]any{
				"oneOf": []any{
					map[string]any{"type": "boolean"},
					map[string]any{"type": "integer"},
				},
			},
		},
		"required": []any{"any", "one", "coercedAny", "coercedOne"},
	}

	decoded, err := sigma.ValidateToolCallWithOptions(
		[]sigma.Tool{{Name: "compose", InputSchema: schema}},
		sigma.ToolCall{Name: "compose", Arguments: map[string]any{
			"any":        "007",
			"one":        "007",
			"coercedAny": "42",
			"coercedOne": "false",
		}},
		sigma.ToolValidationOptions{CoercePrimitives: true},
	)
	if err != nil {
		t.Fatalf("ValidateToolCallWithOptions returned error: %v", err)
	}

	want := map[string]any{
		"any":        "007",
		"one":        "007",
		"coercedAny": 42,
		"coercedOne": false,
	}
	if gotJSON, wantJSON := mustJSON(t, decoded), mustJSON(t, want); gotJSON != wantJSON {
		t.Fatalf("decoded arguments = %s, want %s", gotJSON, wantJSON)
	}
}

func TestValidateToolCallWithOptionsCoercionRejectsMalformedUnionBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyword string
	}{
		{name: "anyOf", keyword: "anyOf"},
		{name: "oneOf", keyword: "oneOf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := sigma.ValidateToolCallWithOptions(
				[]sigma.Tool{{
					Name: "compose",
					InputSchema: sigma.Schema{
						"type": "object",
						"properties": map[string]any{
							"value": map[string]any{
								tt.keyword: []any{
									map[string]any{"type": "string"},
									map[string]any{"type": []any{1}},
								},
							},
						},
					},
				}},
				sigma.ToolCall{Name: "compose", Arguments: map[string]any{"value": "007"}},
				sigma.ToolValidationOptions{CoercePrimitives: true},
			)
			if err == nil {
				t.Fatal("ValidateToolCallWithOptions returned nil error")
			}

			var validationErr *sigma.ToolValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error type = %T, want ToolValidationError", err)
			}
			if got, want := validationErr.Reason, "schema is malformed"; got != want {
				t.Fatalf("reason = %q, want %q", got, want)
			}
		})
	}
}

func TestValidateToolCallWithOptionsRejectsInvalidPrimitiveCoercions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema sigma.Schema
		value  any
	}{
		{
			name:   "fractional integer",
			schema: sigma.Schema{"type": "integer"},
			value:  "42.1",
		},
		{
			name:   "numeric boolean string",
			schema: sigma.Schema{"type": "boolean"},
			value:  "1",
		},
		{
			name:   "text null",
			schema: sigma.Schema{"type": "null"},
			value:  "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := sigma.ValidateToolCallWithOptions(
				[]sigma.Tool{{
					Name: "coerce",
					InputSchema: sigma.Schema{
						"type":       "object",
						"properties": map[string]any{"value": tt.schema},
					},
				}},
				sigma.ToolCall{Name: "coerce", Arguments: map[string]any{"value": tt.value}},
				sigma.ToolValidationOptions{CoercePrimitives: true},
			)
			if err == nil {
				t.Fatal("ValidateToolCallWithOptions returned nil error")
			}
			if !errors.Is(err, sigma.ErrToolValidation) {
				t.Fatalf("error does not match ErrToolValidation: %v", err)
			}
			var validationErr *sigma.ToolValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error type = %T, want ToolValidationError", err)
			}
			if got, want := validationErr.Reason, "wrong primitive type"; got != want {
				t.Fatalf("reason = %q, want %q", got, want)
			}
		})
	}
}

func TestValidateToolCallWithOptionsDoesNotCoerceNullIntoPrimitiveZeroValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema sigma.Schema
	}{
		{name: "string", schema: sigma.Schema{"type": "string"}},
		{name: "number", schema: sigma.Schema{"type": "number"}},
		{name: "integer", schema: sigma.Schema{"type": "integer"}},
		{name: "boolean", schema: sigma.Schema{"type": "boolean"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := sigma.ValidateToolCallWithOptions(
				[]sigma.Tool{{
					Name: "coerce",
					InputSchema: sigma.Schema{
						"type":       "object",
						"properties": map[string]any{"value": tt.schema},
					},
				}},
				sigma.ToolCall{Name: "coerce", Arguments: map[string]any{"value": nil}},
				sigma.ToolValidationOptions{CoercePrimitives: true},
			)
			if err == nil {
				t.Fatal("ValidateToolCallWithOptions returned nil error")
			}
			var validationErr *sigma.ToolValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error type = %T, want ToolValidationError", err)
			}
			if got, want := validationErr.Reason, "wrong primitive type"; got != want {
				t.Fatalf("reason = %q, want %q", got, want)
			}
		})
	}

	decoded, err := sigma.ValidateToolCallWithOptions(
		[]sigma.Tool{{
			Name: "coerce",
			InputSchema: sigma.Schema{
				"type":       "object",
				"properties": map[string]any{"value": map[string]any{"type": "null"}},
			},
		}},
		sigma.ToolCall{Name: "coerce", Arguments: map[string]any{"value": nil}},
		sigma.ToolValidationOptions{CoercePrimitives: true},
	)
	if err != nil {
		t.Fatalf("ValidateToolCallWithOptions returned error for null schema: %v", err)
	}
	if got := decoded["value"]; got != nil {
		t.Fatalf("decoded null = %#v, want nil", got)
	}
}

func TestValidateToolCallValidatesComposedSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]any
		want map[string]any
	}{
		{
			name: "valid composed arguments",
			args: map[string]any{
				"selector": "name",
				"code":     "ABC-123",
				"filters":  []any{"active", "archived"},
				"labels": map[string]any{
					"priority": "high",
				},
			},
			want: map[string]any{
				"selector": "name",
				"code":     "ABC-123",
				"filters":  []any{"active", "archived"},
				"labels": map[string]any{
					"priority": "high",
				},
			},
		},
		{
			name: "anyOf accepts alternate branch",
			args: map[string]any{
				"selector": 42,
				"code":     "ABC-123",
				"filters":  []any{"active"},
				"labels":   map[string]any{"priority": "high"},
			},
			want: map[string]any{
				"selector": float64(42),
				"code":     "ABC-123",
				"filters":  []any{"active"},
				"labels":   map[string]any{"priority": "high"},
			},
		},
	}

	tools := []sigma.Tool{{Name: "search", InputSchema: composedToolSchema()}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := sigma.ValidateToolCall(tools, sigma.ToolCall{Name: "search", Arguments: tt.args})
			if err != nil {
				t.Fatalf("ValidateToolCall returned error: %v", err)
			}
			if gotJSON, wantJSON := mustJSON(t, got), mustJSON(t, tt.want); gotJSON != wantJSON {
				t.Fatalf("decoded arguments = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestValidateToolCallRejectsComposedSchemaFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     map[string]any
		path     string
		reason   string
		expected string
	}{
		{
			name:     "anyOf matches no branches",
			args:     map[string]any{"selector": false, "code": "ABC-123", "filters": []any{"active"}, "labels": map[string]any{"priority": "high"}},
			path:     "$.selector",
			reason:   "anyOf violation",
			expected: "at least one matching schema",
		},
		{
			name:     "oneOf matches no branches",
			args:     map[string]any{"selector": "name", "code": false, "filters": []any{"active"}, "labels": map[string]any{"priority": "high"}},
			path:     "$.code",
			reason:   "oneOf violation",
			expected: "exactly one matching schema",
		},
		{
			name:     "oneOf matches multiple branches",
			args:     map[string]any{"selector": "name", "code": "AB", "filters": []any{"active"}, "labels": map[string]any{"priority": "high"}},
			path:     "$.code",
			reason:   "oneOf violation",
			expected: "exactly one matching schema",
		},
		{
			name:     "allOf reports nested failure",
			args:     map[string]any{"selector": "name", "code": "ABC-123", "filters": []any{"active"}, "labels": map[string]any{"priority": "lo"}},
			path:     "$.labels.priority",
			reason:   "string is too short",
			expected: "length >= 3",
		},
		{
			name:     "array item composition failure",
			args:     map[string]any{"selector": "name", "code": "ABC-123", "filters": []any{"active", 7}, "labels": map[string]any{"priority": "high"}},
			path:     "$.filters[1]",
			reason:   "anyOf violation",
			expected: "at least one matching schema",
		},
	}

	tools := []sigma.Tool{{Name: "search", InputSchema: composedToolSchema()}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := sigma.ValidateToolCall(tools, sigma.ToolCall{Name: "search", Arguments: tt.args})
			if err == nil {
				t.Fatal("ValidateToolCall returned nil error")
			}
			if !errors.Is(err, sigma.ErrToolValidation) {
				t.Fatalf("error does not match ErrToolValidation: %v", err)
			}

			var validationErr *sigma.ToolValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error type = %T, want ToolValidationError", err)
			}
			if validationErr.Path != tt.path {
				t.Fatalf("path = %q, want %q", validationErr.Path, tt.path)
			}
			if validationErr.Reason != tt.reason {
				t.Fatalf("reason = %q, want %q", validationErr.Reason, tt.reason)
			}
			if validationErr.Expected != tt.expected {
				t.Fatalf("expected = %q, want %q", validationErr.Expected, tt.expected)
			}
		})
	}
}

func TestValidateToolCallRejectsMalformedComposedSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema sigma.Schema
	}{
		{
			name: "anyOf is not array",
			schema: sigma.Schema{
				"type":  "object",
				"anyOf": "string",
			},
		},
		{
			name: "anyOf is empty",
			schema: sigma.Schema{
				"type":  "object",
				"anyOf": []any{},
			},
		},
		{
			name: "oneOf branch is not schema",
			schema: sigma.Schema{
				"type":  "object",
				"oneOf": []any{"string"},
			},
		},
		{
			name: "allOf branch is malformed",
			schema: sigma.Schema{
				"type": "object",
				"allOf": []any{
					map[string]any{"type": []any{"object", 7}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := sigma.ValidateToolCall(
				[]sigma.Tool{{Name: "search", InputSchema: tt.schema}},
				sigma.ToolCall{Name: "search", Arguments: map[string]any{}},
			)
			if err == nil {
				t.Fatal("ValidateToolCall returned nil error")
			}
			if !errors.Is(err, sigma.ErrToolValidation) {
				t.Fatalf("error does not match ErrToolValidation: %v", err)
			}

			var validationErr *sigma.ToolValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error type = %T, want ToolValidationError", err)
			}
			if validationErr.Reason != "schema is malformed" {
				t.Fatalf("reason = %q, want schema is malformed", validationErr.Reason)
			}
		})
	}
}

func TestValidateToolCallRejectsMalformedSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema any
	}{
		{name: "invalid json", schema: json.RawMessage(`{"type":`)},
		{name: "schema is not object", schema: []any{"object"}},
		{name: "required is not array", schema: sigma.Schema{"type": "object", "required": "city"}},
		{name: "items is not schema", schema: sigma.Schema{"type": "object", "properties": map[string]any{"tags": map[string]any{"type": "array", "items": []any{}}}}},
		{name: "pattern is not string", schema: sigma.Schema{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string", "pattern": 7}}}},
		{name: "pattern is invalid", schema: sigma.Schema{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string", "pattern": "["}}}},
		{name: "not is not schema", schema: sigma.Schema{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string", "not": []any{}}}}},
		{name: "not branch is malformed", schema: sigma.Schema{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string", "not": map[string]any{"type": 7}}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := sigma.ValidateToolCall(
				[]sigma.Tool{{Name: "weather", InputSchema: tt.schema}},
				sigma.ToolCall{Name: "weather", Arguments: map[string]any{"city": "Melbourne", "tags": []any{"x"}}},
			)
			if err == nil {
				t.Fatal("ValidateToolCall returned nil error")
			}
			if !errors.Is(err, sigma.ErrToolValidation) {
				t.Fatalf("error does not match ErrToolValidation: %v", err)
			}

			var validationErr *sigma.ToolValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error type = %T, want ToolValidationError", err)
			}
			if validationErr.Reason != "schema is malformed" {
				t.Fatalf("reason = %q, want schema is malformed", validationErr.Reason)
			}
		})
	}
}

func TestToolErrorMessageRedactsValidationError(t *testing.T) {
	t.Parallel()

	_, err := sigma.ValidateToolCall(
		[]sigma.Tool{{
			Name: "weather",
			InputSchema: sigma.Schema{
				"type":                 "object",
				"properties":           map[string]any{"city": map[string]any{"type": "string"}},
				"additionalProperties": false,
			},
		}},
		sigma.ToolCall{Name: "weather", Arguments: map[string]any{"city": "Melbourne", "api_key": "sk-live-secret"}},
	)
	if err == nil {
		t.Fatal("ValidateToolCall returned nil error")
	}

	message := sigma.ToolErrorMessage(sigma.ToolCall{Name: "weather"}, err)
	if !strings.Contains(message, "invalid arguments") {
		t.Fatalf("message = %q, want retry hint", message)
	}
	if strings.Contains(message, "sk-live-secret") {
		t.Fatalf("message leaked secret: %q", message)
	}
}

func TestValidateToolCallSupportsLocalReferencesAndConditionals(t *testing.T) {
	t.Parallel()

	schema := sigma.Schema{
		"type": "object",
		"$defs": map[string]any{
			"contact": map[string]any{
				"type":       "object",
				"properties": map[string]any{"email": map[string]any{"type": "string", "format": "email"}},
				"required":   []any{"email"},
			},
		},
		"properties": map[string]any{
			"contact": map[string]any{"$ref": "#/$defs/contact"},
			"mode":    map[string]any{"type": "string", "enum": []any{"scheduled", "immediate"}},
			"when":    map[string]any{"type": "string", "format": "date-time"},
		},
		"required": []any{"contact", "mode"},
		"if": map[string]any{
			"properties": map[string]any{"mode": map[string]any{"const": "scheduled"}},
		},
		"then": map[string]any{"required": []any{"when"}},
		"else": map[string]any{"not": map[string]any{"required": []any{"when"}}},
	}
	tools := []sigma.Tool{{Name: "notify", InputSchema: schema}}

	valid := []sigma.ToolCall{
		{Name: "notify", Arguments: map[string]any{"contact": map[string]any{"email": "person@example.com"}, "mode": "scheduled", "when": "2026-07-10T12:30:00Z"}},
		{Name: "notify", Arguments: map[string]any{"contact": map[string]any{"email": "person@example.com"}, "mode": "immediate"}},
	}
	for _, call := range valid {
		if _, err := sigma.ValidateToolCall(tools, call); err != nil {
			t.Fatalf("ValidateToolCall(%v) returned error: %v", call.Arguments, err)
		}
	}

	invalid := []struct {
		name string
		call sigma.ToolCall
	}{
		{name: "invalid referenced format", call: sigma.ToolCall{Name: "notify", Arguments: map[string]any{"contact": map[string]any{"email": "not-an-email"}, "mode": "immediate"}}},
		{name: "then requires schedule", call: sigma.ToolCall{Name: "notify", Arguments: map[string]any{"contact": map[string]any{"email": "person@example.com"}, "mode": "scheduled"}}},
		{name: "else rejects schedule", call: sigma.ToolCall{Name: "notify", Arguments: map[string]any{"contact": map[string]any{"email": "person@example.com"}, "mode": "immediate", "when": "2026-07-10T12:30:00Z"}}},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := sigma.ValidateToolCall(tools, tt.call); err == nil {
				t.Fatal("ValidateToolCall returned nil error")
			}
		})
	}
}

func TestValidateToolCallValidatesSupportedFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		format  string
		valid   string
		invalid string
	}{
		{name: "date", format: "date", valid: "2026-07-10", invalid: "2026-02-30"},
		{name: "time", format: "time", valid: "12:30:00Z", invalid: "25:30:00Z"},
		{name: "date time", format: "date-time", valid: "2026-07-10T12:30:00+10:00", invalid: "2026-07-10 12:30:00"},
		{name: "email", format: "email", valid: "person@example.com", invalid: "person@@example.com"},
		{name: "uri", format: "uri", valid: "https://example.com/path", invalid: "not a uri"},
		{name: "uuid", format: "uuid", valid: "019f4c37-8aff-71b3-b314-d291eabc0aa2", invalid: "not-a-uuid"},
		{name: "hostname", format: "hostname", valid: "api.example.com", invalid: "-api.example.com"},
		{name: "ipv4", format: "ipv4", valid: "192.0.2.1", invalid: "2001:db8::1"},
		{name: "ipv6", format: "ipv6", valid: "2001:db8::1", invalid: "192.0.2.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tools := []sigma.Tool{{Name: "check", InputSchema: sigma.Schema{
				"type":       "object",
				"properties": map[string]any{"value": map[string]any{"type": "string", "format": tt.format}},
				"required":   []any{"value"},
			}}}
			if _, err := sigma.ValidateToolCall(tools, sigma.ToolCall{Name: "check", Arguments: map[string]any{"value": tt.valid}}); err != nil {
				t.Fatalf("valid %s returned error: %v", tt.format, err)
			}
			if _, err := sigma.ValidateToolCall(tools, sigma.ToolCall{Name: "check", Arguments: map[string]any{"value": tt.invalid}}); err == nil {
				t.Fatalf("invalid %s returned nil error", tt.format)
			}
		})
	}
}

func TestValidateToolCallLeavesUnknownFormatsAsAnnotations(t *testing.T) {
	t.Parallel()

	_, err := sigma.ValidateToolCall(
		[]sigma.Tool{{Name: "check", InputSchema: sigma.Schema{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": "string", "format": "future-format"}},
			"required":   []any{"value"},
		}}},
		sigma.ToolCall{Name: "check", Arguments: map[string]any{"value": "not otherwise constrained"}},
	)
	if err != nil {
		t.Fatalf("ValidateToolCall returned error: %v", err)
	}
}

func TestValidateToolCallRejectsInvalidOrExternalReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  string
	}{
		{name: "external", ref: "https://example.com/schema.json"},
		{name: "missing pointer", ref: "#/$defs/missing"},
		{name: "pointer is not schema", ref: "#/$defs/name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := sigma.ValidateToolCall(
				[]sigma.Tool{{Name: "check", InputSchema: sigma.Schema{
					"type":       "object",
					"$defs":      map[string]any{"name": "not a schema"},
					"properties": map[string]any{"value": map[string]any{"$ref": tt.ref}},
				}}},
				sigma.ToolCall{Name: "check", Arguments: map[string]any{"value": "ok"}},
			)
			if err == nil {
				t.Fatal("ValidateToolCall returned nil error")
			}
			var validationErr *sigma.ToolValidationError
			if !errors.As(err, &validationErr) || validationErr.Reason != "schema is malformed" {
				t.Fatalf("error = %v, want malformed schema validation error", err)
			}
		})
	}
}

func TestValidateToolCallSupportsEscapedAndRecursiveReferences(t *testing.T) {
	t.Parallel()

	schema := sigma.Schema{
		"type": "object",
		"definitions": map[string]any{
			"entry/name": map[string]any{"type": "string", "minLength": 1},
			"node": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":  map[string]any{"$ref": "#/definitions/entry~1name"},
					"child": map[string]any{"$ref": "#/definitions/node"},
				},
				"required": []any{"name"},
			},
		},
		"properties": map[string]any{"root": map[string]any{"$ref": "#/definitions/node"}},
		"required":   []any{"root"},
	}

	_, err := sigma.ValidateToolCall(
		[]sigma.Tool{{Name: "tree", InputSchema: schema}},
		sigma.ToolCall{Name: "tree", Arguments: map[string]any{
			"root": map[string]any{"name": "parent", "child": map[string]any{"name": "child"}},
		}},
	)
	if err != nil {
		t.Fatalf("ValidateToolCall returned error: %v", err)
	}
}

func TestValidateToolCallRejectsCyclicReferenceAndMalformedConditional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema sigma.Schema
	}{
		{
			name: "cyclic reference",
			schema: sigma.Schema{
				"$ref": "#",
			},
		},
		{
			name: "malformed conditional",
			schema: sigma.Schema{
				"type": "object",
				"if":   []any{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := sigma.ValidateToolCall(
				[]sigma.Tool{{Name: "check", InputSchema: tt.schema}},
				sigma.ToolCall{Name: "check", Arguments: map[string]any{}},
			)
			if err == nil {
				t.Fatal("ValidateToolCall returned nil error")
			}
			var validationErr *sigma.ToolValidationError
			if !errors.As(err, &validationErr) || validationErr.Reason != "schema is malformed" {
				t.Fatalf("error = %v, want malformed schema validation error", err)
			}
		})
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(data)
}

func composedToolSchema() sigma.Schema {
	return sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string", "enum": []any{"name", "id"}},
					map[string]any{"type": "integer", "minimum": 1},
				},
			},
			"code": map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string", "minLength": 2, "maxLength": 5},
					map[string]any{"type": "string", "enum": []any{"AB", "ABC-123"}},
				},
			},
			"filters": map[string]any{
				"type": "array",
				"items": map[string]any{
					"anyOf": []any{
						map[string]any{"type": "string", "enum": []any{"active", "archived"}},
						map[string]any{"type": "boolean"},
					},
				},
			},
			"labels": map[string]any{
				"type": "object",
				"additionalProperties": map[string]any{
					"allOf": []any{
						map[string]any{"type": "string"},
						map[string]any{"type": "string", "minLength": 3},
					},
				},
			},
		},
		"required": []any{"selector", "code", "filters", "labels"},
	}
}
