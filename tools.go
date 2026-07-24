// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

// Schema is a JSON Schema-compatible tool parameter definition.
//
// ValidateToolCall supports the subset commonly emitted for model tools:
// type, properties, required, enum, items, additionalProperties, minimum,
// maximum, minLength, maxLength, pattern, format, not, if/then/else, and
// oneOf/anyOf/allOf. It also resolves local JSON Pointer references through
// $ref, including recursive definitions.
// ValidateToolCallWithOptions can opt into primitive argument coercion before
// strict validation.
// External references and unsupported JSON Schema keywords are rejected or
// ignored respectively; coercion remains off by default.
type Schema map[string]any

// OpenAIGrammarSyntax identifies the grammar format accepted by OpenAI custom
// tools.
type OpenAIGrammarSyntax string

const (
	// OpenAIGrammarLark identifies a Lark grammar definition.
	OpenAIGrammarLark OpenAIGrammarSyntax = "lark"
	// OpenAIGrammarRegex identifies a regular-expression grammar definition.
	OpenAIGrammarRegex OpenAIGrammarSyntax = "regex"
)

// OpenAIGrammar configures an OpenAI custom tool grammar.
type OpenAIGrammar struct {
	Syntax     OpenAIGrammarSyntax `json:"syntax"`
	Definition string              `json:"definition"`
}

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
	// OpenAIGrammar configures this tool as an OpenAI Responses custom tool
	// when grammar tools are enabled for the request.
	OpenAIGrammar *OpenAIGrammar `json:"openAIGrammar,omitempty"`
}

// ToolCall describes a model request to invoke a tool.
type ToolCall struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Arguments         any            `json:"arguments,omitempty"`
	ProviderSignature string         `json:"providerSignature,omitempty"`
	ProviderMetadata  map[string]any `json:"providerMetadata,omitempty"`
}
