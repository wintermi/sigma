# Release notes: sigma v0.4.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.4.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.4.0 is open for development. The first compatibility fixes tighten
structured-output request shaping, OpenAI Responses reasoning replay defaults,
OpenAI-compatible Chat Completions history replay for stricter routes, and
Vertex AI routing for both Gemini and focused non-Gemini MaaS routes.

## Added

- OpenCode Go DeepSeek V4 Flash Chat Completions requests now preserve JSON
  generation by downgrading unsupported strict JSON Schema response formats to
  JSON object mode.
- OpenAI Responses requests now default to disabled provider-side storage,
  include encrypted reasoning replay metadata when reasoning is enabled, and
  default reasoning summaries to `auto` while preserving explicit caller
  overrides.
- OpenAI-compatible Chat Completions replay now skips empty assistant history
  turns and supports an opt-in compatibility flag for routes that require an
  empty tools array when replaying prior tool-call history.
- Google Vertex AI keeps project/location routing explicit through
  `VertexConfig` or provider options and continues to use caller-supplied
  `WithVertexTokenProvider` sources for ADC/OAuth tokens.
- Vertex AI now has focused non-Gemini text support through
  `google-vertex-openai` for OpenAI-compatible MaaS Chat Completions and
  `google-vertex-anthropic` for Anthropic Claude `streamRawPredict`, both using
  shared Vertex endpoint construction and Google auth handling.
- Generated model metadata now includes representative Vertex MaaS rows for
  Llama and Claude routes while keeping broad MaaS catalog expansion out of the
  default registry refresh.

## Compatibility

- `opencode-go/deepseek-v4-flash` no longer sends
  `response_format.type = "json_schema"` through Chat Completions. Sigma
  downgrades that request shape to `response_format.type = "json_object"` for
  this model because the route rejects strict JSON Schema response formats
  before generation.
- OpenAI Responses keeps explicit `include` and `store` provider options at
  caller precedence while adding safer defaults for ordinary reasoning replay.
- OpenAI-compatible Chat Completions keeps empty `tools` arrays out of default
  payloads and enables them only for compatibility routes that opt in.
- Google Vertex AI does not read ambient project/location routing or load ADC
  tokens itself; applications should resolve those inputs before registering or
  calling the provider.
- Vertex OpenAI-compatible MaaS models use the Chat Completions API surface but
  route through Vertex's `endpoints/openapi/chat/completions` endpoint and
  Google `X-Goog-Api-Key` or OAuth Bearer auth.
- Vertex Anthropic models use the Anthropic Messages API surface but send
  `anthropic_version` in the request body and route through Vertex
  `streamRawPredict`, not the direct Anthropic `/messages` endpoint.

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).
- Ambient Vertex project/location fallback and built-in ADC token discovery
  remain deferred pending a broader provider credential-loading policy.
- Mistral-on-Vertex remains deferred until its `rawPredict` and
  `streamRawPredict` Chat Completions-shaped payloads have deterministic
  fixtures.
- Broad Vertex MaaS catalog expansion and live Vertex MaaS probes remain
  opt-in future work and are not part of `mise run ci`.

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
