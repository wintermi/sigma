// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"reflect"
)

// cloneMode retains each existing copy boundary's nil/empty wire conventions.
type cloneMode uint8

const (
	cloneMetadata cloneMode = iota
	cloneContent
	cloneProviderOptions
)

// cloneJSONValue isolates JSON-compatible containers while preserving concrete
// types. Opaque objects (including pointers and callbacks) remain caller-owned.
func cloneJSONValue(value any, mode cloneMode) any {
	switch v := value.(type) {
	case map[string]any:
		if v == nil || len(v) == 0 && mode != cloneProviderOptions {
			return map[string]any(nil)
		}
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = cloneJSONValue(item, mode)
		}
		return result
	case Schema:
		result := make(Schema, len(v))
		for key, item := range v {
			result[key] = cloneJSONValue(item, mode)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = cloneJSONValue(item, mode)
		}
		return result
	case []string:
		return append([]string(nil), v...)
	case []byte:
		if mode == cloneMetadata {
			return append(v[:0:0], v...)
		}
		return append([]byte(nil), v...)
	case json.RawMessage:
		if mode == cloneMetadata {
			return append(v[:0:0], v...)
		}
		return append(json.RawMessage(nil), v...)
	case map[string]string:
		if mode == cloneMetadata {
			return copyStringStringMap(v)
		}
	}
	return cloneJSONContainer(reflect.ValueOf(value), mode).Interface()
}

func cloneJSONContainer(value reflect.Value, mode cloneMode) reflect.Value {
	if !value.IsValid() {
		return reflect.Zero(reflect.TypeFor[any]())
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return value
		}
		result := reflect.New(value.Type()).Elem()
		result.Set(reflect.ValueOf(cloneJSONValue(value.Elem().Interface(), mode)))
		return result
	case reflect.Map:
		if value.IsNil() {
			return value
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			result.SetMapIndex(iter.Key(), cloneJSONContainer(iter.Value(), mode))
		}
		return result
	case reflect.Slice, reflect.Array:
		var result reflect.Value
		if value.Kind() == reflect.Slice {
			if value.IsNil() {
				return value
			}
			result = reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		} else {
			result = reflect.New(value.Type()).Elem()
		}
		for i := range value.Len() {
			result.Index(i).Set(cloneJSONContainer(value.Index(i), mode))
		}
		return result
	default:
		return value
	}
}
