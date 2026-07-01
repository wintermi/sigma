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
