# Request persistence

Sigma request persistence uses the public `sigma.Request` JSON shape. Store a
request with `sigma.MarshalRequest`, load it with `sigma.UnmarshalRequest`, and
pass the decoded value to `Client.Complete` or `Client.Stream`.

```go
data, err := sigma.MarshalRequest(req)
if err != nil {
	return err
}

req, err = sigma.UnmarshalRequest(data)
if err != nil {
	return err
}

final, err := client.Complete(ctx, model, req)
```

No envelope or schema version is added today. The JSON field names on
`Request`, `Message`, `ContentBlock`, and `Tool` are part of sigma's public API
stability policy. If a future format change needs an envelope, it should be added
as an opt-in format rather than changing what `MarshalRequest` emits.

`UnmarshalRequest` rejects unknown struct fields and then validates replay
invariants:

- every message has a supported role
- content blocks have a supported type for their role
- image blocks use supported `base64` or `url` sources
- tool results refer to an earlier assistant tool call
- assistant tool-call IDs are unique
- assistant provenance metadata is attached only to assistant messages
- assistant stop reasons are known

Open-ended JSON maps are intentionally preserved. `ProviderMetadata`,
`ToolArguments`, and tool schemas may contain provider-specific fields that
sigma does not interpret but that may be required to continue a conversation.
Opaque provider signatures on thinking and tool-call blocks are also preserved.

## Appending assistant turns

`AssistantMessage` is output metadata. Persist conversation history by converting
the final assistant response back into a `Message`:

```go
history = append(history, sigma.Message{
	Role:       sigma.RoleAssistant,
	Content:    final.Content,
	Provider:   final.Provider,
	API:        model.API,
	Model:      final.Model,
	StopReason: final.StopReason,
	Usage:      final.Usage,
})
```

For tool loops, append the assistant message first, then append one
`sigma.ToolResult` or `sigma.ToolError` for each tool call you executed. See
[Tools](tools.md).

When a caller-owned tool execution has its own token usage, attach it to the
tool-result message before persisting it:

```go
result := sigma.ToolResult(call.ID, output)
result.Usage = &sigma.Usage{
	InputTokens:  toolInputTokens,
	OutputTokens: toolOutputTokens,
	Provider:     toolProvider,
	Model:        toolModel,
}
history = append(history, result)
```

Tool-result usage is execution-local metadata. Sigma preserves it through
persistence and model handoff but does not send it to providers or include it
in request-token estimates, model-turn cost accounting, or evaluation usage
totals. It is distinct from `Usage.ToolUseInputTokens`, which records
provider-reported tool or connector input tokens on an assistant turn.

For canceled streams, only persist the aborted assistant message if the partial
content was visible to the user or needed for a later continue request. See
[Cancellation](cancellation.md).

Persisting assistant usage is optional. When present on the latest successful
assistant message, `sigma.EstimateRequestTokens` uses it as an anchor and only
estimates messages that follow it. Tool-result usage is preserved but never
used as an anchor. Usage on user and developer messages is rejected by
`ValidateRequest`.

## Storage concerns

Persisted requests can become large. Base64 image blocks expand binary data and
should usually be stored out-of-band with only a stable reference in your
application data, unless the provider requires inline image bytes for replay.

Do not store credentials in persisted requests. API keys, OAuth access tokens,
session cookies, and Authorization headers belong in an `AuthResolver`, request
options, or your application's secret store. `MarshalRequest` only covers
conversation context and tool definitions; it does not include client options or
credential callbacks.

Apply your own redaction policy before storing user messages, tool results, and
provider metadata. Tool outputs often contain secrets from external systems, and
provider metadata may include opaque continuation handles or signatures. Those
values can be necessary for provider continuity, but they should be treated as
sensitive application data.

This package does not implement a database layer and does not encrypt persisted
data. Choose storage, retention, redaction, access control, and encryption at
the application boundary.
