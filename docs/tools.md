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
the local tool loop below. OpenAI Responses, Anthropic Messages, and Google
Generative AI support provider-defined tool declarations. OpenAI Chat
Completions, Mistral Conversations, and Bedrock Converse return a `*sigma.Error`
with the `sigma.ErrorUnsupported` code if a provider-defined tool is supplied.

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
