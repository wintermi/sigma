# Release notes: sigma v0.3.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.3.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

v0.3.0 extends Sigma's generated image metadata with an OpenRouter-routed Grok
Imagine image model, tightens the OpenAI-compatible preview adapters around
prompt caching, replay, stream parsing, and Codex OAuth, and adds typed
provider error classification for safer caller retry and recovery decisions.
It also broadens the OpenAI image preview surface with edit, variation,
streaming partial-image, and Responses image-generation tool coverage backed by
deterministic fixtures. Direct xAI/Grok support remains focused on the preview
Chat Completions adapter.

## Added

- Generated image model metadata for `x-ai/grok-imagine-image-quality` through
  the existing OpenRouter image-generation adapter, including OpenRouter
  credential discovery and xAI routed-provider metadata.
- OpenAI Chat Completions, Responses, and Codex Responses derive bounded
  `prompt_cache_key` values from `sigma.WithSessionID` when prompt caching is
  enabled.
- Long-lived OpenAI prompt-cache retention maps to `24h` retention unless a
  request supplies an explicit OpenAI provider option.
- Chat Completions replay normalizes prior Responses-style
  `call_id|item_id` tool-call identifiers and batches image tool results after
  consecutive tool-result messages for image-capable models.
- OpenAI Images supports generation, reference-image edits through
  `ImageRequest.Inputs`, edit masks through `ImageRequest.Mask`, and explicit
  `dall-e-2` variation requests.
- `Client.StreamImages` adds optional image-provider streaming, and OpenAI
  Images can surface partial image events while preserving `GenerateImages` for
  final-result workflows.
- OpenAI Responses image-generation tool output is mapped to assistant image
  content blocks, including partial image events during streaming.
- OpenAI-compatible stream parsing recognizes Chat Completions
  `reasoning_text` deltas plus Responses/Codex refusal and reasoning-text
  events.
- OpenAI Codex Responses includes stdlib-only device-code OAuth login, token
  refresh helpers, and an in-memory OAuth token provider for caller-managed
  credentials.
- OpenAI Codex Responses sends Codex backend headers for OAuth account routing,
  Responses SSE beta access, originator identity, and session-scoped request
  IDs, while normalizing request payloads for required instructions, disabled
  storage, unsupported output-token caps, and unsupported response replay IDs.
- `cmd/sigma-surface-probe` can run opt-in live OpenAI Responses probes and
  OpenAI Codex Responses probes, including device-code OAuth for Codex and a
  `gpt-5.3-codex` default for ChatGPT-backed Codex probing.
- `sigma.ClassifyError` exposes stable provider error classes for auth, quota,
  billing, context overflow, rate limits, transient failures, invalid requests,
  provider failures, and unknown errors, including provider retry-after hints
  where available.

## Compatibility

- No direct xAI image provider is added in this release. Grok image generation
  is represented as OpenRouter image metadata and uses the existing
  `openrouter-images` provider path.
- The direct xAI/Grok text provider remains a preview OpenAI-compatible Chat
  Completions adapter.
- Anthropic-style OpenAI-compatible cache markers continue to use
  endpoint-specific `cache_control` payload markers. Sigma does not mix those
  payloads with OpenAI-native prompt-cache fields.
- OpenAI Codex Responses remains SSE-only. OAuth credential persistence remains
  caller-owned; Sigma exposes device-code login, refresh, and token-provider
  helpers but does not write tokens to disk.
- OpenAI image variations are intentionally limited to explicit `dall-e-2`
  requests. Other OpenAI image models use generation or edit operations.

## Deferred work

- Direct xAI/Grok image-provider semantics remain deferred until the request
  and response shape is covered by deterministic fixtures.
- Live OpenAI image validation remains deferred to opt-in probes; deterministic
  fixtures are the release evidence for image generation, edits, variations,
  streaming, and Responses image-generation tool output.
- Codex WebSocket transport, WebSocket session caching/fallback, browser
  callback OAuth login, token persistence, Copilot dynamic headers, and
  Cloudflare OpenAI-compatible auth rewriting remain deferred.
- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
No live xAI or OpenRouter provider calls are required for release validation.
OpenAI provider changes, image generation/edit/variation/streaming behavior,
Codex OAuth flows, and typed provider error classification are covered by
deterministic request, response, OAuth, and SSE fixtures.
