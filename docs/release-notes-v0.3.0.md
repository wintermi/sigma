# Release notes: sigma v0.3.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.3.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

v0.3.0 extends Sigma's generated image metadata with an OpenRouter-routed Grok
Imagine image model and tightens the OpenAI-compatible preview adapters around
prompt caching, replay, and stream parsing. Direct xAI/Grok support remains
focused on the preview Chat Completions adapter.

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
  `call_id|item_id` tool-call identifiers and preserves image tool results for
  image-capable models.
- OpenAI-compatible stream parsing recognizes Chat Completions
  `reasoning_text` deltas plus Responses/Codex refusal and reasoning-text
  events.

## Compatibility

- No direct xAI image provider is added in this release. Grok image generation
  is represented as OpenRouter image metadata and uses the existing
  `openrouter-images` provider path.
- The direct xAI/Grok text provider remains a preview OpenAI-compatible Chat
  Completions adapter.
- Anthropic-style OpenAI-compatible cache markers continue to use
  endpoint-specific `cache_control` payload markers. Sigma does not mix those
  payloads with OpenAI-native prompt-cache fields.
- OpenAI Codex Responses remains SSE-only and still requires a caller-supplied
  OAuth token provider.

## Deferred work

- Direct xAI/Grok image-provider semantics remain deferred until the request
  and response shape is covered by deterministic fixtures.
- Codex WebSocket transport, WebSocket session caching/fallback, interactive
  OAuth login, token persistence, Copilot dynamic headers, and Cloudflare
  OpenAI-compatible auth rewriting remain deferred.
- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
No live xAI or OpenRouter provider calls are required for release validation.
OpenAI provider changes are covered by deterministic request and SSE fixtures.
