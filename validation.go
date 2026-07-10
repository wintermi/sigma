// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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

	if err := validateValueWithRoot(schema, args, "$", call.Name); err != nil {
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
	if err := validateAnyOf(schema, value, path, toolName); err == nil {
		return value, false, nil
	} else if isMalformedSchemaError(err) {
		return nil, false, err
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
	if err := validateOneOf(schema, value, path, toolName); err == nil {
		return value, false, nil
	} else if isMalformedSchemaError(err) {
		return nil, false, err
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
	// null is never coerced into another type; enforcing this here covers
	// every current and future primitive coercer.
	if value == nil {
		return value, false
	}
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
		data = v
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		var err error
		data, err = json.Marshal(v)
		if err != nil {
			return nil, err
		}
	}
	var value any
	if err := decodeUseNumber(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

type validationContext struct {
	root   map[string]any
	active map[string]struct{}
}

func validateValueWithRoot(schema map[string]any, value any, path string, toolName string) error {
	context := validationContext{root: schema, active: make(map[string]struct{})}
	return context.validateValue(schema, value, path, toolName)
}

func validateValue(schema map[string]any, value any, path string, toolName string) error {
	return validateValueWithRoot(schema, value, path, toolName)
}

func (context *validationContext) validateValue(schema map[string]any, value any, path string, toolName string) error {
	remaining, handled, err := context.validateReference(schema, value, path, toolName)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	schema = remaining

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
	if err := validateConst(schema, value, path, toolName); err != nil {
		return err
	}

	for _, typ := range types {
		switch typ {
		case "object":
			if object, ok := value.(map[string]any); ok {
				if err := context.validateObject(schema, object, path, toolName); err != nil {
					return err
				}
			}
		case "array":
			if array, ok := value.([]any); ok {
				if err := context.validateArray(schema, array, path, toolName); err != nil {
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
	return context.validateComposedSchemas(schema, value, path, toolName)
}

func (context *validationContext) validateReference(schema map[string]any, value any, path string, toolName string) (map[string]any, bool, error) {
	reference, ok := schema["$ref"]
	if !ok {
		return schema, false, nil
	}
	ref, ok := reference.(string)
	if !ok {
		return nil, false, toolValidationError(toolName, path, "local JSON Pointer reference", reference, "schema is malformed", nil)
	}
	target, err := context.resolveReference(ref)
	if err != nil {
		return nil, false, toolValidationError(toolName, path, "resolvable local JSON Pointer reference", ref, "schema is malformed", err)
	}
	key := ref + "\x00" + path
	if _, active := context.active[key]; active {
		return nil, false, toolValidationError(toolName, path, "non-cyclic local JSON Pointer reference", ref, "schema is malformed", nil)
	}
	context.active[key] = struct{}{}
	err = context.validateValue(target, value, path, toolName)
	delete(context.active, key)
	if err != nil {
		return nil, false, err
	}
	if len(schema) == 1 {
		return nil, true, nil
	}
	return schemaWithoutReference(schema), false, nil
}

func (context *validationContext) validateComposedSchemas(schema map[string]any, value any, path string, toolName string) error {
	if err := context.validateAllOf(schema, value, path, toolName); err != nil {
		return err
	}
	if err := context.validateAnyOf(schema, value, path, toolName); err != nil {
		return err
	}
	if err := context.validateOneOf(schema, value, path, toolName); err != nil {
		return err
	}
	if err := context.validateNot(schema, value, path, toolName); err != nil {
		return err
	}
	return context.validateConditional(schema, value, path, toolName)
}

func (context *validationContext) validateAllOf(schema map[string]any, value any, path string, toolName string) error {
	branches, ok, err := schemaBranches(schema, "allOf")
	if err != nil {
		return toolValidationError(toolName, path, "allOf schema array", schema["allOf"], "schema is malformed", err)
	}
	if !ok {
		return nil
	}
	for _, branch := range branches {
		if err := context.validateValue(branch, value, path, toolName); err != nil {
			return err
		}
	}
	return nil
}

func validateAnyOf(schema map[string]any, value any, path string, toolName string) error {
	context := validationContext{root: schema, active: make(map[string]struct{})}
	return context.validateAnyOf(schema, value, path, toolName)
}

func (context *validationContext) validateAnyOf(schema map[string]any, value any, path string, toolName string) error {
	branches, ok, err := schemaBranches(schema, "anyOf")
	if err != nil {
		return toolValidationError(toolName, path, "anyOf schema array", schema["anyOf"], "schema is malformed", err)
	}
	if !ok {
		return nil
	}
	matches := 0
	for _, branch := range branches {
		err := context.validateValue(branch, value, path, toolName)
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
	context := validationContext{root: schema, active: make(map[string]struct{})}
	return context.validateOneOf(schema, value, path, toolName)
}

func (context *validationContext) validateOneOf(schema map[string]any, value any, path string, toolName string) error {
	branches, ok, err := schemaBranches(schema, "oneOf")
	if err != nil {
		return toolValidationError(toolName, path, "oneOf schema array", schema["oneOf"], "schema is malformed", err)
	}
	if !ok {
		return nil
	}
	matches := 0
	for _, branch := range branches {
		err := context.validateValue(branch, value, path, toolName)
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

func (context *validationContext) validateNot(schema map[string]any, value any, path string, toolName string) error {
	raw, ok := schema["not"]
	if !ok {
		return nil
	}
	branch, ok := raw.(map[string]any)
	if !ok {
		return toolValidationError(toolName, path, "not schema object", raw, "schema is malformed", nil)
	}
	err := context.validateValue(branch, value, path, toolName)
	if err == nil {
		return toolValidationError(toolName, path, "value not matching schema", value, "not violation", nil)
	}
	if isMalformedSchemaError(err) {
		return err
	}
	return nil
}

func (context *validationContext) validateConditional(schema map[string]any, value any, path string, toolName string) error {
	rawIf, ok := schema["if"]
	if !ok {
		return nil
	}
	ifSchema, ok := rawIf.(map[string]any)
	if !ok {
		return toolValidationError(toolName, path, "if schema object", rawIf, "schema is malformed", nil)
	}

	branch := "else"
	if err := context.validateValue(ifSchema, value, path, toolName); err == nil {
		branch = "then"
	} else if isMalformedSchemaError(err) {
		return err
	}
	rawSelected, ok := schema[branch]
	if !ok {
		return nil
	}
	selected, ok := rawSelected.(map[string]any)
	if !ok {
		return toolValidationError(toolName, path, branch+" schema object", rawSelected, "schema is malformed", nil)
	}
	return context.validateValue(selected, value, path, toolName)
}

func (context *validationContext) resolveReference(ref string) (map[string]any, error) {
	if ref == "#" {
		return context.root, nil
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("only local JSON Pointer references are supported")
	}

	var current any = context.root
	for _, token := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		decoded, err := unescapeJSONPointerToken(token)
		if err != nil {
			return nil, err
		}
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[decoded]
			if !ok {
				return nil, fmt.Errorf("reference does not resolve")
			}
		case []any:
			index, err := jsonPointerIndex(decoded, len(value))
			if err != nil {
				return nil, err
			}
			current = value[index]
		default:
			return nil, fmt.Errorf("reference does not resolve")
		}
	}

	target, ok := current.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("reference target must be a schema object")
	}
	return target, nil
}

func unescapeJSONPointerToken(token string) (string, error) {
	var builder strings.Builder
	for index := 0; index < len(token); index++ {
		if token[index] != '~' {
			builder.WriteByte(token[index])
			continue
		}
		if index+1 >= len(token) {
			return "", fmt.Errorf("invalid JSON Pointer escape")
		}
		index++
		switch token[index] {
		case '0':
			builder.WriteByte('~')
		case '1':
			builder.WriteByte('/')
		default:
			return "", fmt.Errorf("invalid JSON Pointer escape")
		}
	}
	return builder.String(), nil
}

func jsonPointerIndex(token string, length int) (int, error) {
	if token == "" || (len(token) > 1 && token[0] == '0') {
		return 0, fmt.Errorf("invalid JSON Pointer array index")
	}
	index, err := strconv.Atoi(token)
	if err != nil || index < 0 || index >= length {
		return 0, fmt.Errorf("reference does not resolve")
	}
	return index, nil
}

func schemaWithoutReference(schema map[string]any) map[string]any {
	copy := make(map[string]any, len(schema)-1)
	for key, value := range schema {
		if key != "$ref" {
			copy[key] = value
		}
	}
	return copy
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

func validateConst(schema map[string]any, value any, path string, toolName string) error {
	constant, ok := schema["const"]
	if !ok || jsonEqual(constant, value) {
		return nil
	}
	return toolValidationError(toolName, path, "constant "+summarizeValue(constant), value, "const violation", nil)
}

func (context *validationContext) validateObject(schema map[string]any, object map[string]any, path string, toolName string) error {
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
		if err := context.validateValue(propertySchema, value, joinPath(path, name), toolName); err != nil {
			return err
		}
	}

	return context.validateAdditionalProperties(schema, properties, object, path, toolName)
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

func (context *validationContext) validateAdditionalProperties(schema map[string]any, properties map[string]map[string]any, object map[string]any, path string, toolName string) error {
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
			if err := context.validateValue(v, object[name], joinPath(path, name), toolName); err != nil {
				return err
			}
		}
		return nil
	default:
		return toolValidationError(toolName, path, "boolean or schema object", raw, "schema is malformed", nil)
	}
}

func (context *validationContext) validateArray(schema map[string]any, array []any, path string, toolName string) error {
	raw, ok := schema["items"]
	if !ok {
		return nil
	}
	itemSchema, ok := raw.(map[string]any)
	if !ok {
		return toolValidationError(toolName, path, "items schema object", raw, "schema is malformed", nil)
	}
	for i, item := range array {
		if err := context.validateValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, i), toolName); err != nil {
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
	if err := validateStringPattern(schema, text, path, toolName); err != nil {
		return err
	}
	return validateStringFormat(schema, text, path, toolName)
}

func validateStringFormat(schema map[string]any, text string, path string, toolName string) error {
	raw, ok := schema["format"]
	if !ok {
		return nil
	}
	format, ok := raw.(string)
	if !ok {
		return toolValidationError(toolName, path, "format string", raw, "schema is malformed", nil)
	}
	if format == "" {
		return nil
	}
	if err := validateFormat(format, text); err != nil {
		return toolValidationError(toolName, path, "valid "+format, text, "format violation", nil)
	}
	return nil
}

func validateFormat(format string, text string) error {
	switch format {
	case "date":
		_, err := time.Parse("2006-01-02", text)
		return formatParseError("date", err)
	case "time":
		_, err := time.Parse("15:04:05Z07:00", text)
		return formatParseError("time", err)
	case "date-time":
		_, err := time.Parse(time.RFC3339, text)
		return formatParseError("date-time", err)
	case "email":
		address, err := mail.ParseAddress(text)
		if err != nil || address.Address != text {
			return fmt.Errorf("invalid email")
		}
		return nil
	case "uri":
		parsed, err := url.ParseRequestURI(text)
		if err != nil || parsed.Scheme == "" {
			return fmt.Errorf("invalid URI")
		}
		return nil
	case "uuid":
		if !uuidPattern.MatchString(text) {
			return fmt.Errorf("invalid UUID")
		}
		return nil
	case "hostname":
		if !validHostname(text) {
			return fmt.Errorf("invalid hostname")
		}
		return nil
	case "ipv4", "ipv6":
		address, err := netip.ParseAddr(text)
		if err != nil || (format == "ipv4" && !address.Is4()) || (format == "ipv6" && !address.Is6()) {
			return fmt.Errorf("invalid IP address")
		}
		return nil
	default:
		return nil
	}
}

func formatParseError(format string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("parse %s: %w", format, err)
}

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validHostname(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	host = strings.TrimSuffix(host, ".")
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
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
