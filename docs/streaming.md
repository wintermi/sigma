# Streaming

Text generation is stream-first. `Client.Stream` dispatches to a registered
`TextProvider`, and `Client.Complete` collects that stream into an
`AssistantMessage`.

```go
stream := client.Stream(ctx, model, sigma.Request{
	Messages: []sigma.Message{sigma.UserText("Explain sigma in one sentence.")},
})
defer stream.Close()

for event := range stream.Events() {
	switch event.Kind {
	case sigma.EventKindTextDelta:
		fmt.Print(event.DeltaText)
	case sigma.EventKindThinkingDelta:
		// Usually hidden unless your UI exposes reasoning.
	case sigma.EventKindToolCallDelta:
		// Accumulate by ContentIndex; JSON arguments may be partial.
	case sigma.EventKindDone:
		// Successful terminal event.
	case sigma.EventKindError:
		// Terminal event with Event.Error and a final message.
	}
}
if err := stream.Err(); err != nil {
	return err
}
final, ok := stream.Final()
_ = final
_ = ok
```

`Stream.Events` is single-consumer. If multiple components need events, have one
goroutine read the stream and fan out copies in your application.

## Event Order

Providers emit provider-neutral events:

- `start`
- `text_start`, `text_delta`, `text_end`
- `thinking_start`, `thinking_delta`, `thinking_end`
- `toolcall_start`, `toolcall_delta`, `toolcall_end`
- `done`
- `error`

Content events may be interleaved. Track state by `Event.ContentIndex` instead
of assuming all text arrives before all tool calls or thinking blocks.

Treat tool-call IDs as opaque. Google (including Vertex text) and OpenAI Chat
Completions generate random fallback IDs when a provider omits an ID, keeping
calls distinct across turns and concurrent streams. Each call retains its generated
ID through its events and final result. A delayed first provider-authored Chat
Completions ID can replace the fallback; track live events by content index and
use the final ID for tool results and persistence. Custom tools follow the same rule.

```go
textByIndex := map[int]string{}

for event := range stream.Events() {
	if event.Kind != sigma.EventKindTextDelta || event.ContentIndex == nil {
		continue
	}
	textByIndex[*event.ContentIndex] += event.DeltaText
}
```

Terminal events carry `FinalMessage`. `Stream.Final` returns the same final
assistant message when the provider recorded one.

Every non-terminal event carries `PartialMessage`. Its stop reason defaults to
`StopReasonPending` while generation remains in progress; the initial `start`
event carries an empty pending snapshot before any content blocks arrive. A
provider-supplied non-empty partial stop reason is preserved.

Use partial messages only for live display or transient state. Persist the
terminal `FinalMessage` or `Stream.Final` result, whose stop reason records how
generation actually ended.

## Collecting

Use `sigma.Collect` when the caller does not need incremental events:

```go
final, err := sigma.Collect(ctx, client.Stream(ctx, model, req))
```

`Client.Complete` is the same pattern behind the client API:

```go
final, err := client.Complete(ctx, model, req)
```

`Client.CompleteText` is intentionally stricter. It returns only final text and
errors if the assistant produced thinking blocks, image blocks, or tool calls,
so non-text output is not silently discarded.

## Sources And Citations

Grounded or citation-bearing provider responses may include source metadata on
the final assistant message or citations on individual content blocks. Use the
typed accessors instead of reading provider metadata maps directly:

```go
final, err := client.Complete(ctx, model, req)
if err != nil {
	return err
}

for _, source := range final.Sources() {
	fmt.Println(source.Title, source.URL, source.URI)
}
for _, citation := range final.Citations() {
	fmt.Println(citation.Title, citation.URL, citation.CitedText)
}
```

The returned entries are copied views over provider metadata. Rendering,
ranking, de-duplication, and provider-specific citation policy remain
application-owned.

## Backpressure And Closing

Streams use a small event buffer. Providers can emit one unread event, but a
slow consumer applies backpressure after that. Call `stream.Close()` when a UI
or caller stops reading early.

Terminal acceptance commits the result before event delivery. If parent or
collector cancellation, or explicit closure, interrupts blocked terminal
delivery, `Final` and `Err` keep that accepted outcome. The undeliverable terminal
event is abandoned, already queued events remain readable, and `Events` closes
before `Done`. Collectors return the accepted result even when their context is
canceled. This contract also applies to image streams.

Anthropic Messages requires `message_start`, `message_stop`, and a nonempty
provider stop reason for successful completion, including empty output.
Incomplete streams preserve partial content and usage and classify as transient
stream failures. Sigma does not automatically retry after streamed output.

Cancellation is controlled by `context.Context`; see [Errors](errors.md) and
[Cancellation](cancellation.md).

Google and Vertex text streams blocked at the prompt level finish with `done`
and `StopReasonContentFilter`, retaining prompt feedback and usage. Candidate
finish reasons `MALFORMED_FUNCTION_CALL` and `UNEXPECTED_TOOL_CALL` finish with
`error` and `StopReasonError`; `Complete` and `CompleteText` return a
`ProviderError` wrapping `ErrProviderResponse`. These failures are non-retryable
and retain partial output, usage, and the raw finish reason. A stream with no
prompt-block or candidate-terminal evidence still reports premature EOF.

Anthropic hosted-result metadata is available on final content blocks. Incremental
event kinds are unchanged; persist final content to preserve the complete replay
sequence. Anthropic filters blank text when preparing subsequent requests.

## Persistence

If you want to save a completed assistant turn, append an assistant `Message`
containing `final.Content`, `final.Provider`, `model.API`, `final.Model`,
`final.ProviderThinkingLevel`, `final.Usage`, and `final.StopReason` to your conversation history. Persist the next request with
`sigma.MarshalRequest`; see [Request persistence](persistence.md).

## Alternatives and interrupted results

Sigma returns one assistant result. Chat Completions `n` and Google/Vertex
`generationConfig.candidateCount` may be omitted or set to numeric one. Other
supplied values return `ErrInvalidOptions`, including values introduced by raw
overrides or Google payload hooks. Parsers isolate alternative index zero even
if another index arrives first; response-level usage and provider errors remain
visible.

Radius streams that end without a terminal event return the accumulated partial
message and a transient, retryable error. Retrying after response-body delivery
remains the caller's responsibility.

Empty assistant text blocks carrying a signature can be persisted. Persistence
retains opaque signatures; each provider validates signature format and exact
provider/API/model provenance before replay. Empty Anthropic text anchors carrying
hosted-result metadata are also persistable; other unsigned empty text remains invalid.

Direct OpenAI deferred submit, fetch, and cancel operations apply `Options.Timeout`
through authentication, HTTP retries, and body decoding. Each HTTP attempt
resolves authentication once before constructing its route, payload, and headers.
Submission handles retain conversion and service-tier metadata from the successful
attempt, without credentials or auth-derived endpoints.
