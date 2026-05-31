# Errors And Cancellation

Sigma errors are ordinary Go errors with sentinel support through `errors.Is`
and typed detail through `errors.As`.

## Common Sentinels

- `sigma.ErrNoProvider`
- `sigma.ErrModelNotFound`
- `sigma.ErrCredentialUnavailable`
- `sigma.ErrAborted`
- `sigma.ErrContextOverflow`
- `sigma.ErrToolValidation`
- `sigma.ErrProviderResponse`
- `sigma.ErrInvalidOptions`
- `sigma.ErrRetryAfterExceedsMaxDelay`

The package error type, `*sigma.Error`, carries an `ErrorCode`, provider, model,
and wrapped cause where available.

```go
final, err := client.Complete(ctx, model, req)
if errors.Is(err, sigma.ErrModelNotFound) {
	return err
}
_ = final
```

## Provider Errors

HTTP and provider failures should surface as `*sigma.ProviderError` and match
`sigma.ErrProviderResponse`:

```go
var providerErr *sigma.ProviderError
if errors.As(err, &providerErr) {
	log.Println(providerErr.Provider, providerErr.StatusCode, providerErr.RequestID)
}
```

`ProviderError` redacts body previews, request IDs, headers, and known secret
shapes before formatting. `ProviderError.Diagnostic` returns safe-to-log context
for an assistant message that ended with `StopReasonError`.

## Classified Provider Errors

Use `sigma.ClassifyError` when an application needs stable retry or recovery
decisions without parsing provider-specific error strings:

```go
classification := sigma.ClassifyError(err)
switch classification.Class {
case sigma.ErrorClassContextOverflow:
	// Compact context and retry once.
case sigma.ErrorClassRateLimited, sigma.ErrorClassTransient:
	if classification.RetryHint.Retryable {
		time.Sleep(classification.RetryHint.After)
	}
case sigma.ErrorClassAuth, sigma.ErrorClassQuota, sigma.ErrorClassBilling:
	// Surface an account or credential action to the caller.
}
```

The classifier unwraps `*sigma.GenerationError`, `*sigma.Error`, and
`*sigma.ProviderError`. It preserves the ordinary Go inspection path, so callers
can combine it with sentinel and typed-error checks:

```go
if errors.Is(err, sigma.ErrContextOverflow) {
	// Same signal as ErrorClassContextOverflow.
}

var providerErr *sigma.ProviderError
if errors.As(err, &providerErr) {
	log.Println(providerErr.Provider, providerErr.StatusCode, providerErr.ProviderCode)
}
```

## Stream Errors

Stream terminal errors are wrapped in `*sigma.GenerationError`, which preserves
the final assistant message:

```go
final, err := sigma.Collect(ctx, stream)
if err != nil {
	var generationErr *sigma.GenerationError
	if errors.As(err, &generationErr) {
		final, _ = generationErr.FinalMessage()
	}
}
```

For non-streaming callers, `Client.Complete` and `Client.CompleteText` return
the same errors that `Collect` returns.

## Cancellation

Use `context.Context` for cancellation and deadlines:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

final, err := client.Complete(ctx, model, req)
if errors.Is(err, sigma.ErrAborted) || errors.Is(err, context.DeadlineExceeded) {
	_ = final
}
```

Text providers should preserve any partial content emitted before cancellation.
Canceled streams end with `StopReasonAborted` when the provider records a final
assistant message. Image providers may return an aborted stop reason without
partial content.

See [Cancellation](cancellation.md) for persistence guidance around aborted
assistant messages.

## Retries And Timeouts

HTTP providers share sigma's retry policy:

- no retries by default (`DefaultMaxRetries` is `0`)
- optional request timeout with `sigma.WithTimeout`
- retries only for transient network errors, HTTP `429`, and `5xx`
- provider `Retry-After` is honored when it does not exceed
  `WithMaxRetryDelay`
- streaming responses are not retried after the response body has been handed to
  the stream parser

```go
final, err := client.Complete(
	ctx,
	model,
	req,
	sigma.WithTimeout(30*time.Second),
	sigma.WithMaxRetries(2),
	sigma.WithMaxRetryDelay(5*time.Second),
)
```

## Tool Validation

`sigma.ValidateToolCall` returns `*sigma.ToolValidationError` for schema or
argument failures. It matches `sigma.ErrToolValidation`, and
`sigma.ToolErrorMessage` converts it to a safe tool-result string for retrying a
model turn.

## Credential Errors

Credential lookup failures return `*sigma.CredentialUnavailableError` and match
`sigma.ErrCredentialUnavailable`. The error lists redacted sources checked by
the credential chain.
