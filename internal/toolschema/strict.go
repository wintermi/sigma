// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package toolschema provides provider-facing JSON Schema transformations for
// model tools.
package toolschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

var unsupportedStrictSchemaKeys = [...]string{
	"$ref",
	"$defs",
	"definitions",
	"allOf",
	"oneOf",
	"patternProperties",
	"dependentSchemas",
	"dependencies",
	"unevaluatedProperties",
	"propertyNames",
	"contains",
	"prefixItems",
	"not",
	"if",
	"then",
	"else",
}

// Enabled reports whether provider metadata explicitly enables strict tools.
func Enabled(metadata map[string]any) bool {
	strict, _ := metadata["strict"].(bool)
	return strict
}

// MakeStrict returns a provider-compatible strict copy of a tool schema.
func MakeStrict(input any) (map[string]any, error) {
	if input == nil {
		input = map[string]any{"type": "object"}
	}
	decoded, err := decode(input)
	if err != nil {
		return nil, err
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("root schema must be an object")
	}
	if err := makeNodeStrict(root); err != nil {
		return nil, err
	}
	if root["type"] != "object" {
		return nil, fmt.Errorf("root schema must have type object")
	}
	return root, nil
}

// NormalizeOptionalNulls removes provider-emitted null placeholders for
// optional non-nullable properties. Both value and schema must be decoded
// copies owned by the caller.
func NormalizeOptionalNulls(value any, schema map[string]any) {
	if schema == nil {
		return
	}
	if _, referencesAnotherSchema := schema["$ref"]; referencesAnotherSchema {
		return
	}
	if array, ok := value.([]any); ok {
		itemSchema, _ := schema["items"].(map[string]any)
		for _, item := range array {
			NormalizeOptionalNulls(item, itemSchema)
		}
		return
	}
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	properties, _ := schema["properties"].(map[string]any)
	required := stringSet(schema["required"])
	for name, rawProperty := range properties {
		property, ok := rawProperty.(map[string]any)
		if !ok {
			continue
		}
		propertyValue, exists := object[name]
		if !exists {
			continue
		}
		if propertyValue == nil && !required[name] && !allowsNull(property) && !containsReference(property) {
			delete(object, name)
			continue
		}
		NormalizeOptionalNulls(propertyValue, property)
	}
}

func decode(input any) (any, error) {
	var data []byte
	switch value := input.(type) {
	case json.RawMessage:
		data = value
	case []byte:
		data = value
	default:
		var err error
		data, err = json.Marshal(value)
		if err != nil {
			return nil, err
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return decoded, nil
}

func makeNodeStrict(schema map[string]any) error {
	for _, key := range unsupportedStrictSchemaKeys {
		if _, exists := schema[key]; exists {
			return fmt.Errorf("%s schemas are unsupported", key)
		}
	}
	if err := makeAnyOfStrict(schema); err != nil {
		return err
	}
	if err := makeItemsStrict(schema); err != nil {
		return err
	}

	typeName, _ := schema["type"].(string)
	if hasStructuredTypeUnion(schema["type"]) {
		return fmt.Errorf("object and array type unions are unsupported")
	}
	if _, exists := schema["properties"]; exists && typeName != "object" {
		return fmt.Errorf("properties require type object")
	}
	if typeName != "object" {
		return nil
	}
	return makeObjectStrict(schema)
}

func makeAnyOfStrict(schema map[string]any) error {
	raw, exists := schema["anyOf"]
	if !exists {
		return nil
	}
	variants, ok := raw.([]any)
	if !ok || len(variants) == 0 {
		return fmt.Errorf("anyOf must contain at least one schema")
	}
	for _, rawVariant := range variants {
		variant, ok := rawVariant.(map[string]any)
		if !ok {
			return fmt.Errorf("boolean schemas are unsupported")
		}
		if isStructured(variant) {
			return fmt.Errorf("object and array unions are unsupported")
		}
		if err := makeNodeStrict(variant); err != nil {
			return err
		}
	}
	return nil
}

func makeItemsStrict(schema map[string]any) error {
	raw, exists := schema["items"]
	if !exists {
		return nil
	}
	if _, tuple := raw.([]any); tuple {
		return fmt.Errorf("tuple schemas are unsupported")
	}
	items, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("boolean schemas are unsupported")
	}
	return makeNodeStrict(items)
}

func makeObjectStrict(schema map[string]any) error {
	if additional, exists := schema["additionalProperties"]; exists && additional != false {
		return fmt.Errorf("schema-valued or true additionalProperties is unsupported")
	}
	properties := map[string]any{}
	if raw, exists := schema["properties"]; exists {
		var ok bool
		properties, ok = raw.(map[string]any)
		if !ok {
			return fmt.Errorf("object properties must be a schema map")
		}
	}
	rawRequired, hasRequired := schema["required"]
	required, err := requiredSet(rawRequired, hasRequired)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for name := range required {
		if _, exists := properties[name]; !exists {
			return fmt.Errorf("required contains unknown property %q", name)
		}
	}
	for _, name := range names {
		property, ok := properties[name].(map[string]any)
		if !ok {
			return fmt.Errorf("property %q must be a schema object", name)
		}
		if err := makeNodeStrict(property); err != nil {
			return fmt.Errorf("property %q: %w", name, err)
		}
		if !required[name] && !allowsNull(property) {
			properties[name] = map[string]any{
				"anyOf": []any{property, map[string]any{"type": "null"}},
			}
		}
	}
	schema["properties"] = properties
	schema["required"] = names
	schema["additionalProperties"] = false
	return nil
}

func requiredSet(raw any, exists bool) (map[string]bool, error) {
	if !exists {
		return map[string]bool{}, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("object required must be a string array")
	}
	result := make(map[string]bool, len(values))
	for _, value := range values {
		name, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("object required must be a string array")
		}
		result[name] = true
	}
	return result, nil
}

func stringSet(raw any) map[string]bool {
	values, _ := raw.([]any)
	result := make(map[string]bool, len(values))
	for _, value := range values {
		if name, ok := value.(string); ok {
			result[name] = true
		}
	}
	return result
}

func allowsNull(schema map[string]any) bool {
	if schema["type"] == "null" {
		return true
	}
	if types, ok := schema["type"].([]any); ok {
		for _, value := range types {
			if value == "null" {
				return true
			}
		}
	}
	if value, exists := schema["const"]; exists && value == nil {
		return true
	}
	if values, ok := schema["enum"].([]any); ok {
		for _, value := range values {
			if value == nil {
				return true
			}
		}
	}
	if variants, ok := schema["anyOf"].([]any); ok {
		for _, raw := range variants {
			if variant, ok := raw.(map[string]any); ok && allowsNull(variant) {
				return true
			}
		}
	}
	return false
}

func isStructured(schema map[string]any) bool {
	if _, exists := schema["properties"]; exists {
		return true
	}
	if _, exists := schema["items"]; exists {
		return true
	}
	if value, ok := schema["type"].(string); ok {
		return value == "object" || value == "array"
	}
	return hasStructuredTypeUnion(schema["type"])
}

func hasStructuredTypeUnion(raw any) bool {
	values, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, value := range values {
		if value == "object" || value == "array" {
			return true
		}
	}
	return false
}

func containsReference(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, exists := typed["$ref"]; exists {
			return true
		}
		for _, nested := range typed {
			if containsReference(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsReference(nested) {
				return true
			}
		}
	}
	return false
}
