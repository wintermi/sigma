# Release notes: sigma v0.2.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.2.0 summary and scope. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.2.0 adds a runtime adapter for OpenAI Images generation and tightens
the existing OpenAI text adapters. The existing generated `gpt-image-1`
metadata is now runnable when applications register the OpenAI image provider,
and OpenAI text requests expose more of the stable provider payload controls
without requiring raw `extra_body` maps.

The release keeps the image-generation surface narrow: the adapter implements
non-streaming generation through OpenAI's dedicated Images API and deliberately
leaves edits, variations, streaming partial images, and Responses image-tool
generation for later work.

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
  and z.ai-style `tool_stream` payloads through compatibility metadata.
- Runtime behavior follows existing Sigma provider conventions: request-scoped
  auth, retries, timeouts, redacted debug hooks, typed provider errors, and
  cancellation mapping.
- Docs and provider parity now classify `openai-images` as a generation-only
  preview adapter instead of metadata-only.

## Compatibility

No persisted root package JSON shapes changed. Public API additions are limited
to new registration helpers in `provider/openai`, new `OpenAIOptions` fields,
and new OpenAI-compatible compatibility metadata values.

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
- Codex WebSocket session caching/fallback.
- Live OpenAI image tests; standard validation remains deterministic and
  credential-free.

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
The OpenAI Images adapter and OpenAI text-provider additions are covered by
deterministic `httptest` fixtures and golden request payloads; no live OpenAI
network calls are required.
