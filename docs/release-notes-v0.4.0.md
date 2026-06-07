# Release notes: sigma v0.4.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.4.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.4.0 is open for development. The first compatibility fixes tighten
structured-output request shaping, OpenAI Responses reasoning replay defaults,
OpenAI-compatible Chat Completions history replay for stricter routes, and
Vertex AI routing for both Gemini and focused non-Gemini MaaS routes, and a
focused provider-surface expansion for Google/Vertex image generation plus
Google/Vertex/Bedrock embeddings.

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
- Anthropic Messages requests now expose typed Sigma controls for tool choice,
  thinking display, and explicit interleaved-thinking beta opt-in, so callers
  can use `sigma.WithAnthropicOptions` for common provider controls without raw
  provider option maps.
- Direct Mistral Pixtral models now advertise image input support and the
  Mistral Conversations adapter can send base64 image inputs plus image-bearing
  tool results through deterministic request shapes.
- Bedrock Converse Stream now derives the `eu-central-1` runtime endpoint for
  built-in EU regional inference-profile rows when callers have not configured
  a region, endpoint, or AWS region environment variable.
- Google Gemini API and Vertex AI now expose preview image generation adapters
  through Sigma's provider-neutral `ImageProvider` surface, covering Gemini API
  Imagen `predict`, Gemini image `generateContent` image outputs, and Vertex
  Imagen `predict` with explicit project/location routing.
- Google Gemini API, Google Vertex AI, and Amazon Bedrock now expose preview
  embedding adapters through Sigma's provider-neutral `EmbeddingProvider`
  surface, with representative generated metadata for the new built-in
  embedding models.
- Bedrock embeddings use `InvokeModel` through Sigma's existing stdlib Bedrock
  region, endpoint, credential, retry, debug, and SigV4 paths for Titan,
  Cohere, Titan image text-only, and Nova text embedding request shapes.

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
- Typed Anthropic options take precedence over matching raw provider options.
  Raw provider options remain available for advanced or newly introduced
  Anthropic fields.
- Mistral image support is limited to base64 image data on direct Pixtral
  Conversations models. URL/file image references and broader Mistral catalog
  changes remain future work.
- Explicit Bedrock provider config, provider options, custom endpoints,
  `AWS_REGION`, and `AWS_DEFAULT_REGION` keep precedence over generated
  metadata fallback.
- Google and Vertex image adapters are generation-only in this release. Edits,
  variations, live probes, and broad image catalog expansion remain outside the
  deterministic release gate.
- Google and Vertex embeddings map query/document intent to provider task
  types while preserving explicit provider options for advanced request fields.
- Bedrock embeddings intentionally reuse the existing stdlib credential path;
  AWS profiles, SSO, web identity, IMDS, and shared-config loading are not
  introduced by this provider-surface slice.

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).
- Ambient Vertex project/location fallback and built-in ADC token discovery
  remain deferred pending a broader provider credential-loading policy.
- Mistral-on-Vertex remains deferred until its `rawPredict` and
  `streamRawPredict` Chat Completions-shaped payloads have deterministic
  fixtures.
- Broad Vertex MaaS catalog expansion and live Vertex MaaS probes remain
  opt-in future work and are not part of `mise run ci`.
- Anthropic OAuth, Claude Code identity headers, Claude Code tool-name
  canonicalization, and live Anthropic-compatible probes remain deferred.
- Mistral built-in connectors, append/restart lifecycle operations, URL/file
  image references, and broad direct Mistral catalog expansion remain deferred.
- AWS profile, SSO, web identity, IMDS, shared-config loading, and live Bedrock
  validation remain deferred.
- Hosted-tool factory expansion, live probes, broad catalog refresh,
  tokenizer-based embedding estimates, provider-selection fallback, external
  vector stores, and AWS SDK credential-chain integration remain deferred.

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
