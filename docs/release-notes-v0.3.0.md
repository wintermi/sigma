# Release notes: sigma v0.3.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.3.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

v0.3.0 refreshes Sigma's generated model catalog with curated current metadata
for supported provider IDs, including broader OpenAI, Anthropic, Google, Vertex
AI, Mistral, Bedrock, OpenCode, and metadata-only OpenAI-compatible rows. It
also extends generated image metadata with OpenRouter-routed Grok and Gemini
image routes, tightens the OpenAI-compatible preview adapters around prompt
caching, replay, stream parsing, Codex OAuth, and Codex WebSocket session
reuse, and adds typed provider error classification for safer caller retry and
recovery decisions. The Google preview adapters now include the scoped provider
hardening for Vertex credential fallback, model-scoped routing metadata, and
replayed tool-call IDs. Direct xAI/Grok support remains focused on the preview
Chat Completions adapter.

## Added

- Generated image model metadata for `x-ai/grok-imagine-image-quality` through
  the existing OpenRouter image-generation adapter, including OpenRouter
  credential discovery and xAI routed-provider metadata.
- Curated generated text metadata for current OpenAI, Anthropic, Google,
  Vertex AI, Mistral, Bedrock, OpenCode, and OpenAI-compatible model rows while
  keeping default registry entries metadata-only until providers are registered.
- Generated OpenRouter image metadata for the stable Gemini image route and
  additional current image-generation routes through the existing
  `openrouter-images` provider path.
- OpenAI Chat Completions, Responses, and Codex Responses derive bounded
  `prompt_cache_key` values from `sigma.WithSessionID` when prompt caching is
  enabled.
- Long-lived OpenAI prompt-cache retention maps to `24h` retention unless a
  request supplies an explicit OpenAI provider option.
- OpenAI-compatible Chat Completions and direct OpenAI Responses emit
  session-affinity headers from `sigma.WithSessionID` when prompt caching is
  enabled, while explicit request headers retain precedence.
- Chat Completions replay normalizes prior Responses-style
  `call_id|item_id` tool-call identifiers and batches image tool results after
  consecutive tool-result messages for image-capable models.
- OpenAI Responses replay avoids stale function-call item IDs when
  same-provider conversation history crosses OpenAI Responses model IDs.
- OpenAI Responses emits explicit automatic image detail on user image inputs
  and image-capable `function_call_output` image parts.
- OpenAI Images supports generation, reference-image edits through
  `ImageRequest.Inputs`, edit masks through `ImageRequest.Mask`, and explicit
  `dall-e-2` variation requests.
- OpenAI Images edits can send URL and file-ID image references through JSON
  request bodies when no binary image upload is required.
- `Client.StreamImages` adds optional image-provider streaming, and OpenAI
  Images can surface partial image events while preserving `GenerateImages` for
  final-result workflows.
- OpenAI Responses image-generation tool output is mapped to assistant image
  content blocks, including partial image events during streaming.
- OpenAI-compatible stream parsing recognizes Chat Completions
  `reasoning_text` deltas plus Responses/Codex refusal and reasoning-text
  events.
- OpenAI-compatible Chat Completions and Responses normalize invalid UTF-8 text
  at request, replay, and stream boundaries before provider dispatch or final
  message persistence.
- OpenAI-compatible GitHub Copilot routes add dynamic initiator, intent, and
  vision request headers while preserving explicit caller header overrides.
- OpenAI-compatible Cloudflare AI Gateway routes resolve environment-backed
  base URL placeholders and send API keys through Cloudflare's gateway auth
  header without broad catalog promotion.
- OpenAI Codex Responses includes stdlib-only browser callback and device-code
  OAuth login, token refresh helpers, and an in-memory OAuth token provider for
  caller-managed credentials.
- OpenAI Codex Responses sends Codex backend headers for OAuth account routing,
  Responses SSE beta access, originator identity, and session-scoped request
  IDs, while normalizing request payloads for required instructions, disabled
  storage, unsupported output-token caps, and unsupported response replay IDs.
- OpenAI Codex Responses supports stdlib-only direct WebSocket transport with
  session caching, delta replay, explicit cleanup helpers, and SSE fallback
  before stream output starts.
- OpenAI Responses and Codex Responses usage accounting separates cached input
  tokens from ordinary input tokens so cache reads flow into `sigma.Usage`.
- OpenAI Responses and Codex Responses cost reporting applies `flex` and
  `priority` service-tier pricing multipliers when the provider reports or the
  Codex request selects those tiers.
- `cmd/sigma-surface-probe` can run opt-in live OpenAI Responses probes and
  OpenAI Codex Responses probes, including browser callback and device-code
  OAuth for Codex plus a latest-supported ChatGPT Codex fallback for probing.
- `sigma.ClassifyError` exposes stable provider error classes for auth, quota,
  billing, context overflow, rate limits, transient failures, invalid requests,
  provider failures, and unknown errors, including provider retry-after hints
  where available.
- Google Generative AI and Vertex AI consume concrete model-scoped `baseURL`
  and `headers` metadata, preserve request/provider option precedence, and keep
  generated Vertex `{location}` templates out of request hosts.
- Google Vertex AI can fall back from placeholder API-key values to configured
  OAuth/ADC token providers in auto credential mode.
- Google replay normalizes tool-call IDs for Google-hosted model families that
  require explicit function IDs and omits empty Gemini function-response IDs.

## Compatibility

- No direct xAI image provider is added in this release. Grok image generation
  is represented as OpenRouter image metadata and uses the existing
  `openrouter-images` provider path.
- The direct xAI/Grok text provider remains a preview OpenAI-compatible Chat
  Completions adapter.
- Anthropic-style OpenAI-compatible cache markers continue to use
  endpoint-specific `cache_control` payload markers. Sigma does not mix those
  payloads with OpenAI-native prompt-cache fields.
- OpenAI Codex Responses supports SSE and direct WebSocket transport. OAuth
  credential persistence remains caller-owned; Sigma exposes browser callback
  login, device-code login, refresh, and token-provider helpers but does not
  write tokens to disk.
- OpenAI image variations are intentionally limited to explicit `dall-e-2`
  requests. Other OpenAI image models use generation or edit operations.

## Deferred work

- Broader Google Gemini API and Vertex AI catalog coverage remains deferred to
  the catalog refresh workflow with deterministic modeldata, payload, error,
  and compatibility coverage.
- Broad OpenRouter text expansion, Bedrock regional aliases, and automated
  catalog ingestion remain deferred to the catalog refresh workflow.
- Anthropic-compatible Fireworks model routing remains deferred; the built-in
  Fireworks row stays on the OpenAI-compatible Fire Pass route.
- Mistral image input remains deferred until the Conversations request shape is
  covered by deterministic payload fixtures.
- Direct xAI/Grok image-provider semantics remain deferred until the request
  and response shape is covered by deterministic fixtures.
- Live OpenAI image validation remains deferred to opt-in probes; deterministic
  fixtures are the release evidence for image generation, multipart edits,
  reference-only JSON edits, variations, streaming, and Responses
  image-generation tool output.
- Proxy-aware Codex WebSocket dialing, token persistence, and first-class
  Copilot or Cloudflare provider-row promotion remain deferred.
- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
No live xAI or OpenRouter provider calls are required for release validation.
OpenAI provider changes, image generation/edit/variation/streaming behavior,
Codex OAuth and WebSocket flows, typed provider error classification, and
generated catalog metadata are covered by deterministic request, response,
OAuth, SSE/WebSocket, checksum, and registry fixtures.
