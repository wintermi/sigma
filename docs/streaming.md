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

Cancellation is controlled by `context.Context`; see [Errors](errors.md) and
[Cancellation](cancellation.md).

## Persistence

If you want to save a completed assistant turn, append an assistant `Message`
containing `final.Content`, `final.Provider`, `final.Model`, and
`final.StopReason` to your conversation history. Persist the next request with
`sigma.MarshalRequest`; see [Request persistence](persistence.md).
