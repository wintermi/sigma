// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

// Schema is a JSON Schema-compatible tool parameter definition.
//
// ValidateToolCall supports the subset commonly emitted for model tools:
// type, properties, required, enum, items, additionalProperties, minimum,
// maximum, minLength, maxLength, pattern, not, and oneOf/anyOf/allOf.
// ValidateToolCallWithOptions can opt into primitive argument coercion before
// strict validation.
// Unsupported JSON Schema keywords are ignored; $ref, formats, and conditional
// schemas are not evaluated, and coercion remains off by default.
type Schema map[string]any

// Tool describes a callable model tool.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// InputSchema accepts Schema, map[string]any, json.RawMessage, []byte, or
	// another JSON-marshable value containing a JSON Schema-compatible object.
	InputSchema any `json:"inputSchema,omitempty"`
	// ProviderDefinedType identifies a server-side provider tool such as web
	// search or code execution. When set, supported providers serialize the tool
	// using their native tool shape instead of a JSON Schema function tool.
	ProviderDefinedType    string         `json:"providerDefinedType,omitempty"`
	ProviderDefinedOptions map[string]any `json:"providerDefinedOptions,omitempty"`
	ProviderMetadata       map[string]any `json:"providerMetadata,omitempty"`
}

// ToolCall describes a model request to invoke a tool.
type ToolCall struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Arguments         any            `json:"arguments,omitempty"`
	ProviderSignature string         `json:"providerSignature,omitempty"`
	ProviderMetadata  map[string]any `json:"providerMetadata,omitempty"`
}
