// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wintermi/sigma/internal/redact"
)

// ValidateModelRef validates the minimum fields needed to identify a model.
func ValidateModelRef(ref ModelRef) error {
	if ref.Provider == "" {
		return &Error{Code: ErrorUnsupported, Message: "provider is required"}
	}
	if ref.ID == "" {
		return &Error{Code: ErrorUnsupported, Message: "model id is required"}
	}
	return nil
}

// ToolValidationError reports a tool-call argument or tool schema validation
// failure. Actual is a short, redacted summary safe for logs and tool-result
// retry messages.
type ToolValidationError struct {
	ToolName string
	Path     string
	Expected string
	Actual   string
	Reason   string
	Err      error
}

// ToolValidationOptions configures local tool-call validation.
type ToolValidationOptions struct {
	// CoercePrimitives converts common model-emitted primitive mismatches on the
	// decoded argument copy before strict schema validation.
	CoercePrimitives bool
}

func (e *ToolValidationError) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{"tool validation failed"}
	if e.ToolName != "" {
		parts = append(parts, "tool="+e.ToolName)
	}
	if e.Path != "" {
		parts = append(parts, "path="+e.Path)
	}
	if e.Expected != "" {
		parts = append(parts, "expected="+e.Expected)
	}
	if e.Actual != "" {
		parts = append(parts, "actual="+e.Actual)
	}
	if e.Reason != "" {
		parts = append(parts, "reason="+e.Reason)
	}
	if e.Err != nil && !errors.Is(e.Err, ErrToolValidation) {
		parts = append(parts, "cause="+e.Err.Error())
	}
	return redact.String(strings.Join(parts, " "))
}

// Unwrap returns the underlying validation cause.
func (e *ToolValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is supports errors.Is(err, ErrToolValidation).
func (e *ToolValidationError) Is(target error) bool {
	return target == ErrToolValidation
}

// ValidateToolCall validates a model-emitted tool call against the matching
// tool's JSON Schema-compatible InputSchema. It returns a decoded copy of the
// arguments on success and never mutates the supplied tool schema or call
// arguments.
func ValidateToolCall(tools []Tool, call ToolCall) (map[string]any, error) {
	return ValidateToolCallWithOptions(tools, call, ToolValidationOptions{})
}

// ValidateToolCallWithOptions validates a model-emitted tool call against the
// matching tool's JSON Schema-compatible InputSchema. It returns a decoded copy
// of the arguments on success and never mutates the supplied tool schema or
// call arguments.
func ValidateToolCallWithOptions(tools []Tool, call ToolCall, options ToolValidationOptions) (map[string]any, error) {
	tool, ok := findTool(tools, call.Name)
	if !ok {
		return nil, toolValidationError(call.Name, "$", "registered tool name", call.Name, "tool is not registered", nil)
	}

	schema, err := decodeSchema(tool.InputSchema)
	if err != nil {
		return nil, toolValidationError(call.Name, "$", "valid JSON Schema object", tool.InputSchema, "schema is malformed", err)
	}

	args, err := decodeToolArguments(call.Arguments)
	if err != nil {
		return nil, toolValidationError(call.Name, "$", "JSON object arguments", call.Arguments, "arguments are malformed", err)
	}

	if options.CoercePrimitives {
		coerced, err := coerceValue(schema, args, "$", call.Name)
		if err != nil {
			return nil, err
		}
		if coercedArgs, ok := coerced.(map[string]any); ok {
			args = coercedArgs
		}
	}

	if err := validateValue(schema, args, "$", call.Name); err != nil {
		return nil, err
	}
	return args, nil
}

func coerceValue(schema map[string]any, value any, path string, toolName string) (any, error) {
	if coerced, changed, err := coerceAllOf(schema, value, path, toolName); err != nil {
		return nil, err
	} else if changed {
		value = coerced
	}
	if coerced, changed, err := coerceAnyOf(schema, value, path, toolName); err != nil {
		return nil, err
	} else if changed {
		value = coerced
	}
	if coerced, changed, err := coerceOneOf(schema, value, path, toolName); err != nil {
		return nil, err
	} else if changed {
		value = coerced
	}

	types, err := schemaTypes(schema)
	if err != nil {
		return nil, toolValidationError(toolName, path, "valid schema type", schema["type"], "schema is malformed", err)
	}
	if len(types) == 0 {
		types = inferredTypes(schema)
	}
	if len(types) > 1 && valueMatchesAnyType(value, types) {
		return coerceNestedValue(schema, value, types, path, toolName)
	}
	if len(types) > 0 && !valueMatchesAnyType(value, types) {
		for _, typ := range types {
			coerced, changed := coercePrimitiveByType(value, typ)
			if changed {
				value = coerced
				break
			}
		}
	}
	return coerceNestedValue(schema, value, types, path, toolName)
}

func coerceAllOf(schema map[string]any, value any, path string, toolName string) (any, bool, error) {
	branches, ok, err := schemaBranches(schema, "allOf")
	if err != nil {
		return nil, false, toolValidationError(toolName, path, "allOf schema array", schema["allOf"], "schema is malformed", err)
	}
	if !ok {
		return value, false, nil
	}
	coerced := value
	changed := false
	for _, branch := range branches {
		next, err := coerceValue(branch, coerced, path, toolName)
		if err != nil {
			return nil, false, err
		}
		if !reflect.DeepEqual(next, coerced) {
			changed = true
		}
		coerced = next
	}
	return coerced, changed, nil
}

func coerceAnyOf(schema map[string]any, value any, path string, toolName string) (any, bool, error) {
	branches, ok, err := schemaBranches(schema, "anyOf")
	if err != nil {
		return nil, false, toolValidationError(toolName, path, "anyOf schema array", schema["anyOf"], "schema is malformed", err)
	}
	if !ok {
		return value, false, nil
	}
	for _, branch := range branches {
		candidate := cloneAnyValue(value)
		coerced, err := coerceValue(branch, candidate, path, toolName)
		if err != nil {
			return nil, false, err
		}
		if err := validateValue(branch, coerced, path, toolName); err == nil {
			return coerced, !reflect.DeepEqual(coerced, value), nil
		} else if isMalformedSchemaError(err) {
			return nil, false, err
		}
	}
	return value, false, nil
}

func coerceOneOf(schema map[string]any, value any, path string, toolName string) (any, bool, error) {
	branches, ok, err := schemaBranches(schema, "oneOf")
	if err != nil {
		return nil, false, toolValidationError(toolName, path, "oneOf schema array", schema["oneOf"], "schema is malformed", err)
	}
	if !ok {
		return value, false, nil
	}
	for _, branch := range branches {
		candidate := cloneAnyValue(value)
		coerced, err := coerceValue(branch, candidate, path, toolName)
		if err != nil {
			return nil, false, err
		}
		if err := validateValue(branch, coerced, path, toolName); err == nil {
			return coerced, !reflect.DeepEqual(coerced, value), nil
		} else if isMalformedSchemaError(err) {
			return nil, false, err
		}
	}
	return value, false, nil
}

func coerceNestedValue(schema map[string]any, value any, types []string, path string, toolName string) (any, error) {
	for _, typ := range types {
		switch typ {
		case "object":
			object, ok := value.(map[string]any)
			if !ok {
				continue
			}
			if err := coerceObject(schema, object, path, toolName); err != nil {
				return nil, err
			}
		case "array":
			array, ok := value.([]any)
			if !ok {
				continue
			}
			if err := coerceArray(schema, array, path, toolName); err != nil {
				return nil, err
			}
		}
	}
	return value, nil
}

func coerceObject(schema map[string]any, object map[string]any, path string, toolName string) error {
	properties, err := schemaProperties(schema)
	if err != nil {
		return toolValidationError(toolName, path, "properties object", schema["properties"], "schema is malformed", err)
	}
	for name, propertySchema := range properties {
		value, ok := object[name]
		if !ok {
			continue
		}
		coerced, err := coerceValue(propertySchema, value, joinPath(path, name), toolName)
		if err != nil {
			return err
		}
		object[name] = coerced
	}

	raw, declared := schema["additionalProperties"]
	if !declared {
		return nil
	}
	additionalSchema, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	for _, name := range sortedKeys(object) {
		if _, ok := properties[name]; ok {
			continue
		}
		coerced, err := coerceValue(additionalSchema, object[name], joinPath(path, name), toolName)
		if err != nil {
			return err
		}
		object[name] = coerced
	}
	return nil
}

func coerceArray(schema map[string]any, array []any, path string, toolName string) error {
	raw, ok := schema["items"]
	if !ok {
		return nil
	}
	itemSchema, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	for i, item := range array {
		coerced, err := coerceValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, i), toolName)
		if err != nil {
			return err
		}
		array[i] = coerced
	}
	return nil
}

func coercePrimitiveByType(value any, typ string) (any, bool) {
	switch typ {
	case "number":
		return coerceNumber(value, false)
	case "integer":
		return coerceNumber(value, true)
	case "boolean":
		return coerceBoolean(value)
	case "string":
		return coerceString(value)
	case "null":
		return coerceNull(value)
	default:
		return value, false
	}
}

func coerceNumber(value any, integer bool) (any, bool) {
	if value == nil {
		return json.Number("0"), true
	}
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return value, false
		}
		number, err := strconv.ParseFloat(trimmed, 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
			return value, false
		}
		if integer && math.Trunc(number) != number {
			return value, false
		}
		return json.Number(formatNumber(number)), true
	case bool:
		if v {
			return json.Number("1"), true
		}
		return json.Number("0"), true
	default:
		return value, false
	}
}

func coerceBoolean(value any) (any, bool) {
	if value == nil {
		return false, true
	}
	switch v := value.(type) {
	case string:
		switch v {
		case "true":
			return true, true
		case "false":
			return false, true
		default:
			return value, false
		}
	default:
		number, ok := numberFloat(value)
		if !ok {
			return value, false
		}
		switch number {
		case 1:
			return true, true
		case 0:
			return false, true
		default:
			return value, false
		}
	}
}

func coerceString(value any) (any, bool) {
	if value == nil {
		return "", true
	}
	switch v := value.(type) {
	case string:
		return value, false
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	default:
		if number, ok := numberFloat(value); ok {
			return formatNumber(number), true
		}
		return value, false
	}
}

func coerceNull(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil, true
		}
	case bool:
		if !v {
			return nil, true
		}
	default:
		if number, ok := numberFloat(value); ok && number == 0 {
			return nil, true
		}
	}
	return value, false
}

// ToolErrorMessage converts a tool validation failure into text suitable for a
// ToolError result, so the model can retry with corrected arguments.
func ToolErrorMessage(call ToolCall, err error) string {
	if err == nil {
		return ""
	}
	toolName := call.Name
	if toolName == "" {
		toolName = "unknown"
	}
	return redact.String("Tool call " + toolName + " has invalid arguments: " + err.Error())
}

func findTool(tools []Tool, name string) (Tool, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

func decodeSchema(input any) (map[string]any, error) {
	if input == nil {
		return map[string]any{"type": "object"}, nil
	}

	value, err := decodeJSONValue(input)
	if err != nil {
		return nil, err
	}
	schema, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema must be a JSON object")
	}
	return schema, nil
}

func decodeToolArguments(input any) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}

	value, err := decodeJSONValue(input)
	if err != nil {
		return nil, err
	}
	args, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("arguments must be a JSON object")
	}
	return args, nil
}

func decodeJSONValue(input any) (any, error) {
	var data []byte
	switch v := input.(type) {
	case json.RawMessage:
		data = append([]byte(nil), v...)
	case []byte:
		data = append([]byte(nil), v...)
	case string:
		data = []byte(v)
	default:
		var err error
		data, err = json.Marshal(v)
		if err != nil {
			return nil, err
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("JSON value must not contain trailing data")
	}
	return value, nil
}

func validateValue(schema map[string]any, value any, path string, toolName string) error {
	types, err := schemaTypes(schema)
	if err != nil {
		return toolValidationError(toolName, path, "valid schema type", schema["type"], "schema is malformed", err)
	}
	if len(types) == 0 {
		types = inferredTypes(schema)
	}
	if len(types) > 0 && !valueMatchesAnyType(value, types) {
		return toolValidationError(toolName, path, strings.Join(types, " or "), value, "wrong primitive type", nil)
	}

	if err := validateEnum(schema, value, path, toolName); err != nil {
		return err
	}

	for _, typ := range types {
		switch typ {
		case "object":
			if object, ok := value.(map[string]any); ok {
				if err := validateObject(schema, object, path, toolName); err != nil {
					return err
				}
			}
		case "array":
			if array, ok := value.([]any); ok {
				if err := validateArray(schema, array, path, toolName); err != nil {
					return err
				}
			}
		case "string":
			if text, ok := value.(string); ok {
				if err := validateString(schema, text, path, toolName); err != nil {
					return err
				}
			}
		case "number", "integer":
			if isJSONNumber(value) {
				if err := validateNumber(schema, value, path, toolName); err != nil {
					return err
				}
			}
		}
	}
	return validateComposedSchemas(schema, value, path, toolName)
}

func validateComposedSchemas(schema map[string]any, value any, path string, toolName string) error {
	if err := validateAllOf(schema, value, path, toolName); err != nil {
		return err
	}
	if err := validateAnyOf(schema, value, path, toolName); err != nil {
		return err
	}
	if err := validateOneOf(schema, value, path, toolName); err != nil {
		return err
	}
	return validateNot(schema, value, path, toolName)
}

func validateAllOf(schema map[string]any, value any, path string, toolName string) error {
	branches, ok, err := schemaBranches(schema, "allOf")
	if err != nil {
		return toolValidationError(toolName, path, "allOf schema array", schema["allOf"], "schema is malformed", err)
	}
	if !ok {
		return nil
	}
	for _, branch := range branches {
		if err := validateValue(branch, value, path, toolName); err != nil {
			return err
		}
	}
	return nil
}

func validateAnyOf(schema map[string]any, value any, path string, toolName string) error {
	branches, ok, err := schemaBranches(schema, "anyOf")
	if err != nil {
		return toolValidationError(toolName, path, "anyOf schema array", schema["anyOf"], "schema is malformed", err)
	}
	if !ok {
		return nil
	}
	matches := 0
	for _, branch := range branches {
		err := validateValue(branch, value, path, toolName)
		if err == nil {
			matches++
			continue
		}
		if isMalformedSchemaError(err) {
			return err
		}
	}
	if matches > 0 {
		return nil
	}
	return toolValidationError(toolName, path, "at least one matching schema", value, "anyOf violation", nil)
}

func validateOneOf(schema map[string]any, value any, path string, toolName string) error {
	branches, ok, err := schemaBranches(schema, "oneOf")
	if err != nil {
		return toolValidationError(toolName, path, "oneOf schema array", schema["oneOf"], "schema is malformed", err)
	}
	if !ok {
		return nil
	}
	matches := 0
	for _, branch := range branches {
		err := validateValue(branch, value, path, toolName)
		if err == nil {
			matches++
			continue
		}
		if isMalformedSchemaError(err) {
			return err
		}
	}
	if matches != 1 {
		return toolValidationError(toolName, path, "exactly one matching schema", value, "oneOf violation", nil)
	}
	return nil
}

func schemaBranches(schema map[string]any, keyword string) ([]map[string]any, bool, error) {
	raw, ok := schema[keyword]
	if !ok {
		return nil, false, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, true, fmt.Errorf("%s must be an array", keyword)
	}
	if len(values) == 0 {
		return nil, true, fmt.Errorf("%s must contain at least one schema", keyword)
	}
	branches := make([]map[string]any, 0, len(values))
	for index, value := range values {
		branch, ok := value.(map[string]any)
		if !ok {
			return nil, true, fmt.Errorf("%s[%d] must be a schema object", keyword, index)
		}
		branches = append(branches, branch)
	}
	return branches, true, nil
}

func validateNot(schema map[string]any, value any, path string, toolName string) error {
	raw, ok := schema["not"]
	if !ok {
		return nil
	}
	branch, ok := raw.(map[string]any)
	if !ok {
		return toolValidationError(toolName, path, "not schema object", raw, "schema is malformed", nil)
	}
	err := validateValue(branch, value, path, toolName)
	if err == nil {
		return toolValidationError(toolName, path, "value not matching schema", value, "not violation", nil)
	}
	if isMalformedSchemaError(err) {
		return err
	}
	return nil
}

func isMalformedSchemaError(err error) bool {
	var validationErr *ToolValidationError
	return errors.As(err, &validationErr) && validationErr.Reason == "schema is malformed"
}

func schemaTypes(schema map[string]any) ([]string, error) {
	raw, ok := schema["type"]
	if !ok {
		return nil, nil
	}
	switch v := raw.(type) {
	case string:
		if !supportedType(v) {
			return nil, fmt.Errorf("unsupported type %q", v)
		}
		return []string{v}, nil
	case []any:
		types := make([]string, 0, len(v))
		for _, item := range v {
			typ, ok := item.(string)
			if !ok || !supportedType(typ) {
				return nil, fmt.Errorf("type array must contain supported strings")
			}
			types = append(types, typ)
		}
		return types, nil
	default:
		return nil, fmt.Errorf("type must be a string or string array")
	}
}

func inferredTypes(schema map[string]any) []string {
	if _, ok := schema["properties"]; ok {
		return []string{"object"}
	}
	if _, ok := schema["required"]; ok {
		return []string{"object"}
	}
	if _, ok := schema["additionalProperties"]; ok {
		return []string{"object"}
	}
	if _, ok := schema["items"]; ok {
		return []string{"array"}
	}
	return nil
}

func supportedType(typ string) bool {
	switch typ {
	case "object", "array", "string", "number", "integer", "boolean", "null":
		return true
	default:
		return false
	}
}

func valueMatchesAnyType(value any, types []string) bool {
	for _, typ := range types {
		if valueMatchesType(value, typ) {
			return true
		}
	}
	return false
}

func valueMatchesType(value any, typ string) bool {
	switch typ {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		return isJSONNumber(value)
	case "integer":
		return isJSONInteger(value)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}

func validateEnum(schema map[string]any, value any, path string, toolName string) error {
	raw, ok := schema["enum"]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if !ok {
		return toolValidationError(toolName, path, "enum array", raw, "schema is malformed", nil)
	}
	for _, allowed := range values {
		if jsonEqual(allowed, value) {
			return nil
		}
	}
	return toolValidationError(toolName, path, "one of "+summaryList(values), value, "enum violation", nil)
}

func validateObject(schema map[string]any, object map[string]any, path string, toolName string) error {
	properties, err := schemaProperties(schema)
	if err != nil {
		return toolValidationError(toolName, path, "properties object", schema["properties"], "schema is malformed", err)
	}

	required, err := schemaRequired(schema)
	if err != nil {
		return toolValidationError(toolName, path, "required string array", schema["required"], "schema is malformed", err)
	}
	for _, name := range required {
		if _, ok := object[name]; !ok {
			return toolValidationError(toolName, joinPath(path, name), "required property", "missing", "missing required field", nil)
		}
	}

	for name, propertySchema := range properties {
		value, ok := object[name]
		if !ok {
			continue
		}
		if err := validateValue(propertySchema, value, joinPath(path, name), toolName); err != nil {
			return err
		}
	}

	return validateAdditionalProperties(schema, properties, object, path, toolName)
}

func schemaProperties(schema map[string]any) (map[string]map[string]any, error) {
	raw, ok := schema["properties"]
	if !ok {
		return nil, nil
	}
	rawProperties, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("properties must be an object")
	}
	properties := make(map[string]map[string]any, len(rawProperties))
	for name, rawSchema := range rawProperties {
		propertySchema, ok := rawSchema.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("property %q schema must be an object", name)
		}
		properties[name] = propertySchema
	}
	return properties, nil
}

func schemaRequired(schema map[string]any) ([]string, error) {
	raw, ok := schema["required"]
	if !ok {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("required must be an array")
	}
	required := make([]string, 0, len(values))
	for _, value := range values {
		name, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("required entries must be strings")
		}
		required = append(required, name)
	}
	return required, nil
}

func validateAdditionalProperties(schema map[string]any, properties map[string]map[string]any, object map[string]any, path string, toolName string) error {
	raw, declared := schema["additionalProperties"]
	if !declared {
		return nil
	}

	switch v := raw.(type) {
	case bool:
		if v {
			return nil
		}
		for _, name := range sortedKeys(object) {
			if _, ok := properties[name]; !ok {
				return toolValidationError(toolName, joinPath(path, name), "declared property", object[name], "additional property is not allowed", nil)
			}
		}
		return nil
	case map[string]any:
		for _, name := range sortedKeys(object) {
			if _, ok := properties[name]; ok {
				continue
			}
			if err := validateValue(v, object[name], joinPath(path, name), toolName); err != nil {
				return err
			}
		}
		return nil
	default:
		return toolValidationError(toolName, path, "boolean or schema object", raw, "schema is malformed", nil)
	}
}

func validateArray(schema map[string]any, array []any, path string, toolName string) error {
	raw, ok := schema["items"]
	if !ok {
		return nil
	}
	itemSchema, ok := raw.(map[string]any)
	if !ok {
		return toolValidationError(toolName, path, "items schema object", raw, "schema is malformed", nil)
	}
	for i, item := range array {
		if err := validateValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, i), toolName); err != nil {
			return err
		}
	}
	return nil
}

func validateString(schema map[string]any, text string, path string, toolName string) error {
	if err := validateStringBound(schema, "minLength", text, path, toolName); err != nil {
		return err
	}
	if err := validateStringBound(schema, "maxLength", text, path, toolName); err != nil {
		return err
	}
	return validateStringPattern(schema, text, path, toolName)
}

func validateStringBound(schema map[string]any, keyword string, text string, path string, toolName string) error {
	raw, ok := schema[keyword]
	if !ok {
		return nil
	}
	limit, err := nonNegativeInt(raw)
	if err != nil {
		return toolValidationError(toolName, path, keyword+" integer", raw, "schema is malformed", err)
	}
	length := utf8.RuneCountInString(text)
	if keyword == "minLength" && length < limit {
		return toolValidationError(toolName, path, "length >= "+strconv.Itoa(limit), text, "string is too short", nil)
	}
	if keyword == "maxLength" && length > limit {
		return toolValidationError(toolName, path, "length <= "+strconv.Itoa(limit), text, "string is too long", nil)
	}
	return nil
}

func validateStringPattern(schema map[string]any, text string, path string, toolName string) error {
	raw, ok := schema["pattern"]
	if !ok {
		return nil
	}
	pattern, ok := raw.(string)
	if !ok {
		return toolValidationError(toolName, path, "pattern string", raw, "schema is malformed", nil)
	}
	matched, err := regexp.MatchString(pattern, text)
	if err != nil {
		return toolValidationError(toolName, path, "valid regex pattern", raw, "schema is malformed", err)
	}
	if !matched {
		return toolValidationError(toolName, path, "match pattern "+pattern, text, "pattern violation", nil)
	}
	return nil
}

func validateNumber(schema map[string]any, value any, path string, toolName string) error {
	number, ok := numberFloat(value)
	if !ok {
		return nil
	}
	if raw, exists := schema["minimum"]; exists {
		minimum, err := schemaNumber(raw)
		if err != nil {
			return toolValidationError(toolName, path, "numeric minimum", raw, "schema is malformed", err)
		}
		if number < minimum {
			return toolValidationError(toolName, path, ">= "+formatNumber(minimum), value, "number is below minimum", nil)
		}
	}
	if raw, exists := schema["maximum"]; exists {
		maximum, err := schemaNumber(raw)
		if err != nil {
			return toolValidationError(toolName, path, "numeric maximum", raw, "schema is malformed", err)
		}
		if number > maximum {
			return toolValidationError(toolName, path, "<= "+formatNumber(maximum), value, "number is above maximum", nil)
		}
	}
	return nil
}

func isJSONNumber(value any) bool {
	_, ok := numberFloat(value)
	return ok
}

func isJSONInteger(value any) bool {
	number, ok := numberFloat(value)
	if !ok {
		return false
	}
	return math.Trunc(number) == number
}

func numberFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case json.Number:
		number, err := v.Float64()
		return number, err == nil
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}

func schemaNumber(value any) (float64, error) {
	number, ok := numberFloat(value)
	if !ok {
		return 0, fmt.Errorf("value must be numeric")
	}
	return number, nil
}

func nonNegativeInt(value any) (int, error) {
	number, ok := numberFloat(value)
	if !ok || math.Trunc(number) != number || number < 0 {
		return 0, fmt.Errorf("value must be a non-negative integer")
	}
	return int(number), nil
}

func jsonEqual(left any, right any) bool {
	leftNumber, leftIsNumber := numberFloat(left)
	rightNumber, rightIsNumber := numberFloat(right)
	if leftIsNumber || rightIsNumber {
		return leftIsNumber && rightIsNumber && leftNumber == rightNumber
	}
	return reflect.DeepEqual(left, right)
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func joinPath(base string, field string) string {
	if base == "" || base == "$" {
		return "$." + field
	}
	return base + "." + field
}

func toolValidationError(toolName string, path string, expected string, actual any, reason string, err error) *ToolValidationError {
	if err == nil {
		err = ErrToolValidation
	}
	return &ToolValidationError{
		ToolName: toolName,
		Path:     path,
		Expected: expected,
		Actual:   summarizeValue(actual),
		Reason:   reason,
		Err:      err,
	}
}

func summarizeValue(value any) string {
	if text, ok := value.(string); ok && text == "missing" {
		return "missing"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return redact.String(fmt.Sprintf("%T", value))
	}
	return redact.Preview(string(data), 160)
}

func summaryList(values []any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, summarizeValue(value))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
