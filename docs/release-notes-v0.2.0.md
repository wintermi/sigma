# Release notes: sigma v0.2.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.2.0 summary and scope. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.2.0 adds a runtime adapter for OpenAI Images generation and tightens
the existing OpenAI and Anthropic text adapters. The existing generated
`gpt-image-1` metadata is now runnable when applications register the OpenAI
image provider, OpenAI text requests expose more stable provider payload
controls, and Anthropic-compatible endpoints can declare Messages compatibility
without relying on raw `extra_body` maps.

The release keeps the image-generation surface narrow: the adapter implements
non-streaming generation through OpenAI's dedicated Images API and deliberately
leaves edits, variations, streaming partial images, and Responses image-tool
generation for later work.

OpenCode remains a curated OpenAI-compatible preview surface in this release.
The v0.2.0 work promotes only the narrow Kimi/Grok Build compatibility gaps
with deterministic fixtures; it does not promote OpenCode to full
source-package parity.

Built-in model metadata remains generated from Sigma's curated checked-in
catalog for v0.2.0. A future refresh workflow should ingest `models.dev` and
provider catalog APIs into reviewable candidate metadata, but the release keeps
that automation outside the current tag so generated registry changes remain
deterministic and fixture-backed.

The curated catalog now includes representative metadata-only text models for
each exposed non-custom provider ID. These rows make provider discovery,
credential-source reporting, compatibility metadata, and model limits line up
with Sigma's current provider constants without promoting those providers to
first-class parity rows.

## Added

- `provider/openai` now exposes `NewImagesProvider`, `RegisterImages`, and
  `RegisterImagesDefault`.
- OpenAI Images requests support prompt, model override, count, size, quality,
  output MIME type, custom headers, provider `organization`/`project` headers,
  endpoint/base URL overrides, and `extra_body` provider options.
- OpenAI Images responses map base64 image data, URL outputs, token usage,
  revised prompts, and provider metadata into `sigma.AssistantImages`.
- `sigma.OpenAIOptions` now covers Chat Completions `tool_choice`,
  Responses/Codex `prompt_cache_retention`, Responses/Codex
  `parallel_tool_calls`, and Responses/Codex text verbosity.
- OpenAI Responses replay now preserves existing provider item metadata or
  synthesizes bounded IDs for prior assistant text, reasoning, and function-call
  items.
- OpenAI Responses tool-result replay keeps image blocks inside
  `function_call_output` for image-capable models.
- OpenAI-compatible Chat Completions can opt into Anthropic-style cache markers
  and z.ai-style `tool_stream` payloads through compatibility metadata, and can
  suppress explicit `reasoning_effort` for models that reject it.
- OpenCode Zen and OpenCode Go metadata now cover Kimi K2.6 DeepSeek-style
  thinking payloads without `reasoning_effort`, plus OpenCode Zen Grok Build
  0.1 reasoning-effort suppression.
- Generated metadata now seeds representative entries for the remaining exposed
  provider IDs, including current OpenAI-compatible, Anthropic-compatible, and
  Vertex compatibility metadata.
- `TODO.md` now records the model-registry generation plan for future
  `models.dev` ingestion, provider-catalog overlays, refresh reports, and
  deterministic source review.
- `sigma.AnthropicMessagesCompat` and `sigma.AnthropicThinkingFormat` describe
  Anthropic-compatible endpoint support for eager tool input streaming, cache
  retention, session affinity, tool cache markers, empty thinking signatures,
  and budget/adaptive thinking payloads.
- Anthropic Messages now sends explicit `thinking: {type:"disabled"}` for
  reasoning-capable models when reasoning is off, supports adaptive thinking
  with `output_config.effort`, omits temperature while thinking is enabled,
  groups consecutive tool results, adds compatible tool-streaming hints, and
  preserves initial stream usage when final usage deltas are partial.
- Runtime behavior follows existing Sigma provider conventions: request-scoped
  auth, retries, timeouts, redacted debug hooks, typed provider errors, and
  cancellation mapping.
- Docs and provider parity now classify `openai-images` as a generation-only
  preview adapter instead of metadata-only.

## Compatibility

No persisted request JSON shapes changed. Public API additions are limited to
new registration helpers in `provider/openai`, new `OpenAIOptions` fields, new
OpenAI-compatible compatibility metadata values including
`OpenAICompletionsCompat.SupportsReasoningEffort`, and new Anthropic Messages
compatibility metadata on `sigma.Model`.

Applications still need to register providers explicitly. Built-in image model
metadata remains metadata-only until a registry has a matching image provider:

```go
registry := sigma.NewRegistry()
_ = openai.RegisterImages(registry, sigma.ProviderOpenAI)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

## Deferred work

- Reference-image editing through `ImageRequest.Inputs`.
- OpenAI image variations.
- Streaming partial image events.
- Responses API image-tool generation.
- GitHub Copilot dynamic headers and Cloudflare AI Gateway auth rewriting.
- OpenCode catalog parity, including broader curated OpenCode/OpenCode Go model
  metadata beyond the promoted Kimi/Grok Build OpenAI-compatible gaps.
- OpenCode-routed OpenAI Responses, Anthropic Messages, and Google API models;
  each route needs separate deterministic coverage before promotion.
- Automated model catalog refresh from `models.dev` and provider catalog APIs;
  generated metadata still enters the release through the checked-in catalog,
  checksum test, and generated Go review flow.
- Anthropic Claude Code OAuth identity headers and Claude Code tool-name
  canonicalization.
- GitHub Copilot and Cloudflare AI Gateway Anthropic Messages routing.
- Codex WebSocket session caching/fallback.
- Malformed Anthropic SSE JSON repair.
- Live OpenAI image tests; standard validation remains deterministic and
  credential-free.

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
The OpenAI Images adapter and OpenAI/Anthropic text-provider additions are
covered by deterministic `httptest` fixtures and golden request payloads; no
live provider network calls are required.
