# Security

Sigma provider adapters handle credentials and user payloads at the application
boundary. The package aims to avoid accidental credential disclosure in local
diagnostics, persisted requests, and examples, but it does not implement a
secrets manager and cannot guarantee provider-side data handling policies.

## Credential Handling

Provide API keys, OAuth tokens, and cloud credentials through `AuthResolver`,
provider-specific token providers, or per-request options. Do not place
credentials in prompts, tool arguments, provider metadata, or persisted request
JSON.

Diagnostic paths redact common credential shapes before exposing them:

- `Authorization`, `Proxy-Authorization`, API-key, `Cookie`, and `Set-Cookie`
  headers
- bearer tokens and API-key-shaped strings
- signed URL and query parameters such as `api_key`, `access_token`,
  `refresh_token`, `signature`, `X-Amz-Signature`, and `X-Goog-Signature`
- OAuth device fields such as `device_code` and `user_code`
- JSON credential fields such as `api_key`, `access_token`, `refresh_token`,
  `client_secret`, `secret_access_key`, and `session_token`
- provider error body previews and underlying error messages

Recognized JSON credential fields are also redacted in incomplete diagnostics.
An unterminated string value is hidden through the end of the diagnostic,
including escaped quotes, backslashes, and multiline content. Complete values
preserve neighboring non-sensitive content. Previews remain bounded and safe at
UTF-8 boundaries; this does not extend redaction to arbitrary unknown secrets.

Debug hooks receive redacted copies of request payloads, request headers, and
response headers. Mutating a debug value does not mutate the provider request or
later hooks. There is currently no unsafe opt-in for raw debug payloads; callers
that need raw traffic should instrument their own HTTP transport and accept the
credential handling risk there.

`Credential.String`, `ProviderError.Error`, `ProviderError.String`, diagnostics,
and related error paths are intended to be safe for logs. Treat all raw
`Request`, `Options`, and provider transport objects as sensitive application
data.

## Persistence

`MarshalRequest` serializes only the public `Request` shape. It does not store
client defaults, request options, API keys, auth callbacks, HTTP headers, or
provider token providers.

Persisted conversation content can still contain sensitive user text, tool
outputs, images, opaque provider signatures, and continuation metadata supplied
by your application. Apply application-level retention, access control,
encryption, and redaction before storing that data.

## Provider Data

Provider calls send the selected request content, tools, images, model options,
headers, and credentials needed by that provider. Review provider terms and data
retention policies before sending user data. Sigma does not make provider-side
privacy guarantees.

## Dependencies And Generated Data

The root `sigma` package must not expose provider SDK types. Provider-specific
SDKs stay behind provider packages so callers can use the root API without
importing cloud SDK shapes.

Direct dependencies should stay minimal and reviewable. The root module does not
currently require provider SDKs; provider catalog metadata is generated data.
Review generated changes for unexpected endpoint, header, credential-source, and
capability changes before merging.

Live provider tests must be opt-in and gated by explicit environment variables.
The default verification command, `mise run go:test`, must remain deterministic
and must not call live LLM APIs.
