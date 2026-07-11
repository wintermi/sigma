# Cancellation

Sigma uses `context.Context` for text-generation cancellation. Pass a cancelable
context to `Client.Stream`, `Client.Complete`, or the package-level helpers, then
call the cancel function when the user aborts the turn.

Canceled text streams end with `StopReasonAborted`. `Collect` returns the final
assistant message available at cancellation time plus an inspectable error:

```go
ctx, cancel := context.WithCancel(context.Background())
stream := client.Stream(ctx, model, req)

// Cancel from UI, signal handling, or another goroutine.
cancel()

final, err := sigma.Collect(context.Background(), stream)
if errors.Is(err, sigma.ErrAborted) || errors.Is(err, context.Canceled) {
	// final.StopReason == sigma.StopReasonAborted
}
```

The context passed to `Collect` is also an active cancellation boundary. If it
is canceled while the context used to create the stream remains live, Sigma
aborts the stream with the same contract: partial output is retained, the final
stop reason is `StopReasonAborted`, and the returned error matches both
`ErrAborted` and the collector context error. `CollectImages` behaves the same
way for image streams.

Calling `Stream.Close` or `ImageStream.Close` intentionally still closes the
stream without synthesizing an aborted result. Use context cancellation when
the operation should be recorded as aborted.

When a provider has already emitted text, thinking, or tool-call deltas, Sigma
preserves those deltas in the aborted final assistant message. This lets callers
decide whether the partial response is useful conversation history:

```go
if errors.Is(err, sigma.ErrAborted) && len(final.Content) > 0 {
	history = append(history, sigma.Message{
		Role:       sigma.RoleAssistant,
		Provider:   final.Provider,
		Model:      final.Model,
		StopReason: sigma.StopReasonAborted,
		Content:    final.Content,
	})
}
```

Append an aborted assistant message when the partial content was visible to the
user or is needed for a later "continue" request. Drop it when the abort happened
before meaningful content was shown, or when the next request should ignore the
interrupted attempt.

Continuation after abort is a new provider call with the saved conversation
history. Sigma does not guarantee that a provider can resume the same server-side
generation after a network abort, and it does not automatically retry canceled
requests.

Provider implementations should preserve any partial content they emit before
reporting cancellation. Providers that do not expose text-streaming assistant
messages, such as image-only providers, may only return an aborted stop reason
without partial assistant content.
