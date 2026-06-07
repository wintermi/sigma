// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package streamblocks

import (
	"encoding/json"
	"strings"

	"github.com/wintermi/sigma"
)

// ToolPartialMode controls how partial tool-call metadata is exposed.
type ToolPartialMode int

const (
	// ToolPartialArgumentsText preserves raw argument text and adds decoded
	// arguments only when the accumulated value is valid JSON.
	ToolPartialArgumentsText ToolPartialMode = iota
	// ToolPartialArguments exposes the provider-neutral arguments value,
	// returning the raw text while JSON is still incomplete.
	ToolPartialArguments
)

// Text accumulates streamed text for one content block.
type Text struct {
	ContentIndex     int
	Started          bool
	Closed           bool
	ProviderMetadata map[string]any

	text strings.Builder
}

// Append adds delta and returns the full accumulated text.
func (b *Text) Append(delta string) string {
	b.text.WriteString(delta)
	return b.String()
}

// Set replaces the accumulated text.
func (b *Text) Set(text string) {
	b.text.Reset()
	b.text.WriteString(text)
}

// String returns the accumulated text.
func (b *Text) String() string {
	return b.text.String()
}

// Thinking accumulates streamed thinking for one content block.
type Thinking struct {
	ContentIndex      int
	Signature         string
	ProviderSignature string
	Redacted          bool
	Started           bool
	Closed            bool

	text strings.Builder
}

// Append adds delta and returns the full accumulated thinking text.
func (b *Thinking) Append(delta string) string {
	b.text.WriteString(delta)
	return b.String()
}

// Set replaces the accumulated thinking text.
func (b *Thinking) Set(text string) {
	b.text.Reset()
	b.text.WriteString(text)
}

// String returns the accumulated thinking text.
func (b *Thinking) String() string {
	return b.text.String()
}

// ToolCall accumulates streamed tool-call identity and argument fragments.
type ToolCall struct {
	ContentIndex      int
	ProviderSignature string
	ProviderMetadata  map[string]any
	Started           bool
	Closed            bool

	id        string
	name      string
	arguments strings.Builder

	decodedText string
	decoded     any
	decodedOK   bool
}

// SetID replaces the tool-call id when value is non-empty.
func (c *ToolCall) SetID(value string) {
	if value != "" {
		c.id = value
	}
}

// ID returns the accumulated tool-call id.
func (c *ToolCall) ID() string {
	return c.id
}

// SetName replaces the tool-call name when value is non-empty.
func (c *ToolCall) SetName(value string) {
	if value != "" {
		c.name = value
	}
}

// Name returns the accumulated tool-call name.
func (c *ToolCall) Name() string {
	return c.name
}

// AppendArguments adds an argument fragment.
func (c *ToolCall) AppendArguments(delta string) {
	if delta == "" {
		return
	}
	c.arguments.WriteString(delta)
	c.decodedText = ""
	c.decoded = nil
	c.decodedOK = false
}

// SetArguments replaces the accumulated argument text.
func (c *ToolCall) SetArguments(arguments string) {
	c.arguments.Reset()
	c.arguments.WriteString(arguments)
	c.decodedText = ""
	c.decoded = nil
	c.decodedOK = false
}

// ArgumentsText returns the accumulated raw argument text.
func (c *ToolCall) ArgumentsText() string {
	return c.arguments.String()
}

// Partial builds the public partial tool-call payload for the accumulated state.
func (c *ToolCall) Partial(argumentsDelta string, mode ToolPartialMode) *sigma.PartialToolCall {
	partial := &sigma.PartialToolCall{
		ID:                c.id,
		Name:              c.name,
		ArgumentsDelta:    argumentsDelta,
		ProviderSignature: c.ProviderSignature,
	}
	switch mode {
	case ToolPartialArgumentsText:
		if arguments := c.ArgumentsText(); arguments != "" {
			metadata := map[string]any{"argumentsText": arguments}
			if decoded, ok := c.DecodeArguments(); ok {
				metadata["arguments"] = decoded
			}
			for key, value := range c.ProviderMetadata {
				metadata[key] = value
			}
			partial.ProviderMetadata = metadata
		} else if len(c.ProviderMetadata) > 0 {
			partial.ProviderMetadata = copyAnyMap(c.ProviderMetadata)
		}
	case ToolPartialArguments:
		partial.ProviderMetadata = map[string]any{"arguments": c.ArgumentsValue()}
		for key, value := range c.ProviderMetadata {
			partial.ProviderMetadata[key] = value
		}
	}
	return partial
}

// ToolCall returns the final public tool-call value.
func (c *ToolCall) ToolCall() sigma.ToolCall {
	return sigma.ToolCall{
		ID:                c.id,
		Name:              c.name,
		Arguments:         c.ArgumentsValue(),
		ProviderSignature: c.ProviderSignature,
		ProviderMetadata:  copyAnyMap(c.ProviderMetadata),
	}
}

// ArgumentsValue returns decoded arguments, an empty object for empty arguments,
// or the raw text while arguments are invalid/incomplete JSON.
func (c *ToolCall) ArgumentsValue() any {
	if decoded, ok := c.DecodeArguments(); ok {
		return decoded
	}
	if arguments := c.ArgumentsText(); arguments != "" {
		return arguments
	}
	return map[string]any{}
}

// DecodeArguments decodes accumulated arguments only when callers need the
// structured value, caching the result until the argument text changes.
func (c *ToolCall) DecodeArguments() (any, bool) {
	arguments := c.ArgumentsText()
	if arguments == "" {
		return map[string]any{}, true
	}
	if c.decodedText == arguments {
		return c.decoded, c.decodedOK
	}
	var decoded any
	err := json.Unmarshal([]byte(arguments), &decoded)
	c.decodedText = arguments
	c.decoded = decoded
	c.decodedOK = err == nil
	return decoded, err == nil
}

func copyAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
