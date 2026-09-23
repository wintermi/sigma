// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import "fmt"

// normalizeOptionalNulls operates only on decoded copies owned by validation.
func (context *validationContext) normalizeOptionalNulls(value any, schema map[string]any, path string, toolName string) error {
	if schema == nil {
		return nil
	}
	if _, referencesAnotherSchema := schema["$ref"]; referencesAnotherSchema {
		return nil
	}
	if array, ok := value.([]any); ok {
		itemSchema, _ := schema["items"].(map[string]any)
		for i, item := range array {
			if err := context.normalizeOptionalNulls(item, itemSchema, fmt.Sprintf("%s[%d]", path, i), toolName); err != nil {
				return err
			}
		}
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	properties, err := schemaProperties(schema)
	if err != nil {
		return toolValidationError(toolName, path, "properties object", schema["properties"], "schema is malformed", err)
	}
	requiredNames, err := schemaRequired(schema)
	if err != nil {
		return toolValidationError(toolName, path, "required string array", schema["required"], "schema is malformed", err)
	}
	required := make(map[string]bool, len(requiredNames))
	for _, name := range requiredNames {
		required[name] = true
	}
	for name, property := range properties {
		propertyValue, exists := object[name]
		if !exists {
			continue
		}
		propertyPath := joinPath(path, name)
		if propertyValue == nil && !required[name] && !containsReference(property) {
			err := context.validateValue(property, nil, propertyPath, toolName)
			if isMalformedSchemaError(err) {
				return err
			}
			if err != nil {
				delete(object, name)
				continue
			}
		}
		if err := context.normalizeOptionalNulls(propertyValue, property, propertyPath, toolName); err != nil {
			return err
		}
	}
	return nil
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
