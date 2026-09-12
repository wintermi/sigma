# Tools

Tools are provider-neutral JSON Schema-compatible definitions passed on
`sigma.Request.Tools`. Models request tools by returning assistant
`ContentBlockToolCall` blocks.

```go
tools := []sigma.Tool{{
	Name:        "weather",
	Description: "Look up current weather for a city.",
	InputSchema: sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"city": map[string]any{"type": "string"},
			"units": map[string]any{
				"type": "string",
				"enum": []any{"celsius", "fahrenheit"},
			},
		},
		"required":             []any{"city"},
		"additionalProperties": false,
	},
}}
```


## Numeric Arguments

Provider-authored JSON numbers in `ToolCall.Arguments`, `ContentBlock.ToolArguments`,
and decoded partial argument metadata use `json.Number`. This preserves large
integers, decimals, and exponent spellings through streaming, cancellation,
persistence, validation, and replay. Migrate assertions against `float64` to
`json.Number`; choose a conversion explicitly when executing a tool:

```go
number, ok := args["record_id"].(json.Number)
if !ok {
	return fmt.Errorf("record_id must be a JSON number")
}
recordID, err := number.Int64()
if err != nil {
	return fmt.Errorf("record_id must fit int64: %w", err)
}
```

Use `number.String()` or a decimal/big-number representation when the destination
requires more precision. `Float64()` is available when rounding is acceptable.
Typed usage counters and cost fields are unchanged. Malformed or incomplete
arguments retain the existing raw-text or conservative partial-object fallback.

## Provider-Defined Tools

Some providers expose server-side tools that the provider executes itself.
Declare those with the provider helper packages:

```go
tools := []sigma.Tool{
	openai.Tools.WebSearch(openai.WithSearchContextSize("low")),
	anthropic.Tools.CodeExecution(),
	google.Tools.GoogleSearch(google.WithWebSearch()),
}
```

Provider-defined tools are declaration-only in Sigma. Sigma serializes them to
native provider payloads, but it does not auto-execute or replay them through
the local tool loop below. OpenAI Responses, Anthropic Messages, Google
Generative AI, and Mistral Conversations support provider-defined tool
declarations. Mistral supports server-executed web search, premium web search,
and document-library tools. OpenAI Chat Completions and Bedrock Converse return
a `*sigma.Error` with the `sigma.ErrorUnsupported` code if a provider-defined
tool is supplied.

Anthropic captures hosted web-search, web-fetch, and code-execution result blocks
in `ContentBlock.ProviderMetadata["anthropic_hosted_replay"]`. This includes bash
and text-editor execution variants, encrypted content, errors, empty results,
and unknown fields within these supported blocks. Save final content intact and
record the source provider, API, and model on the containing assistant message.
Replay places results immediately after their original anchor, even if a blank
text anchor is omitted, and preserves server-call IDs and passthrough fields.
Mismatched or missing provenance omits hosted calls and results; malformed
compatible associations fail locally with `ErrorInvalidRequest`. Results already
discarded by older versions cannot be recovered.

Failed or aborted assistant calls are removed during request preparation along
with their tool results and deferred-tool markers. Nonblank visible text survives
on non-Responses routes; Responses omits the entire failed turn. Successful
unanswered calls still receive synthetic results. These rules also apply to
requests prepared by the handoff helpers and do not alter stored history.

## Tool Loop

```text
Request{Messages, Tools}
  -> model returns AssistantMessage with StopReasonToolCalls
  -> app validates each ToolCall
  -> app runs local tool
  -> app appends ToolResult or ToolError
  -> next Client.Complete or Client.Stream call
```

Sigma validates and serializes the provider-neutral shapes. Your application
owns tool execution, authorization, side effects, retries, and result redaction.

## Validating Calls

Use `sigma.ValidateToolCall` before running a model-emitted call:

```go
for _, call := range calls {
	args, err := sigma.ValidateToolCall(tools, call)
	if err != nil {
		messages = append(messages, sigma.ToolError(call.ID, sigma.ToolErrorMessage(call, err)))
		continue
	}
	result, err := runTool(call.Name, args)
	if err != nil {
		messages = append(messages, sigma.ToolError(call.ID, err.Error()))
		continue
	}
	messages = append(messages, sigma.ToolResult(call.ID, result))
}
```

`ValidateToolCall` supports the common subset providers emit for tool schemas:
`type`, `properties`, `required`, `enum`, `items`, `additionalProperties`,
`minimum`, `maximum`, `minLength`, `maxLength`, `pattern`, `not`, `anyOf`,
`oneOf`, `allOf`, `const`, and `if`/`then`/`else`. It resolves local JSON
Pointer `$ref` values, including `$defs`/`definitions` and recursive schemas.
It strictly evaluates `date`, `time`, `date-time`, `email`, `uri`, `uuid`,
`hostname`, `ipv4`, and `ipv6` formats; unknown formats remain annotations.
External, file, and network references are rejected locally. Other unsupported
JSON Schema keywords remain outside Sigma's validation contract.

## Strict Tool Schemas

For routes that already support strict function tools, opt in with boolean
provider metadata:

```go
tool := sigma.Tool{
	Name: "weather",
	InputSchema: sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"city":  map[string]any{"type": "string"},
			"units": map[string]any{"type": "string"},
		},
		"required": []any{"city"},
	},
	ProviderMetadata: map[string]any{"strict": true},
}
```

Sigma sends a derived schema copy with closed objects and every property
required. Originally optional non-nullable properties become nullable on the
wire, and `ValidateToolCall` maps provider-emitted `null` placeholders for
those properties back to omission. The original schema and arguments are not
mutated.

Strict derivation is available on strict-capable OpenAI-compatible Chat
Completions models, Responses routes, Mistral Conversations, and capability-
gated Anthropic Messages models. Built-in direct Anthropic models advertise
support; custom models and compatible endpoints can use
`AnthropicMessagesCompat.SupportsStrictTools` or
`anthropic.MessagesCompat.StrictTools`. Strict derivation rejects schemas that
cannot be converted safely, including references, composed object or array
unions, tuples, conditionals, pattern properties, and schema-valued additional
properties, before provider dispatch. Omitted or false strict metadata
preserves the original schema, and other provider routes remain unchanged.

## Streaming Tool Calls

Tool calls can stream as `toolcall_delta` events. The arguments are not
guaranteed to be valid JSON until the final `toolcall_end` event or final
assistant message.

```go
case sigma.EventKindToolCallDelta:
	if event.PartialToolCall != nil {
		arguments[eventIndex(event)] += event.PartialToolCall.ArgumentsDelta
	}
case sigma.EventKindToolCallEnd:
	call := *event.ToolCall
```

Use `ContentIndex` to track interleaved tool calls.

## Persisting Tool Results

Tool-result messages use role `tool` and must refer to an earlier assistant
tool-call ID. `sigma.MarshalRequest` and `sigma.UnmarshalRequest` validate that
relationship before replay. See [Request persistence](persistence.md).

## Provider Coverage

Tool support varies. OpenAI-compatible, OpenAI Responses, Anthropic, Mistral,
Google, and Bedrock paths have fixture coverage, but not every provider supports
strict schemas, partial JSON, or provider signatures. Check
[provider parity](provider-parity.md) before relying on provider-specific tool
behavior.

The runnable [tools example](../examples/tools/main.go) demonstrates validation,
tool errors, and retry through the next model turn.
