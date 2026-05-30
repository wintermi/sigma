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
controls including structured output and logprob requests, and
Anthropic-compatible endpoints can declare Messages compatibility without
relying on raw `extra_body` maps.

The release keeps the image-generation surface narrow: the adapter implements
non-streaming generation through OpenAI's dedicated Images API and deliberately
leaves edits, variations, streaming partial images, and Responses image-tool
generation for later work.

OpenCode has a routed preview provider for selected Zen/Go model families in
this release. Models that use Google Generative AI, Anthropic Messages, OpenAI
Responses, or OpenAI-compatible Chat Completions routes can share the
`opencode` and `opencode-go` provider IDs while still dispatching to the
correct Sigma adapter at runtime. The release keeps this coverage curated and
fixture-backed; it does not promote every advertised OpenCode model.

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

Mistral Conversations also gets a targeted preview lift: provider-neutral
reasoning requests now map to Mistral's Conversations completion arguments,
streamed thinking chunks are preserved as Sigma thinking blocks, session IDs can
reuse Mistral prefix cache affinity, and replayed cross-provider tool-call IDs are
normalized to Mistral-compatible IDs.

OpenRouter-compatible behavior is tighter in this release without broadening
the default catalog: text requests use OpenRouter's nested reasoning payload,
request-scoped routing can override generated routing metadata, expanded
routing fields are represented in typed metadata, and cache-write token usage
is reported separately from cache reads.

Google preview adapters also get targeted compatibility fixes. Gemini API and
Vertex requests now have typed tool-choice and disabled-thinking controls,
safer thought-signature replay, JSON Schema function declarations by default,
image-capable tool-result replay, stable fallback tool-call IDs, and corrected
cached/thinking-token accounting.

Amazon Bedrock Converse Stream gets a focused preview lift without changing its
stdlib transport boundary. Applications can now use typed Bedrock request
options for tool choice, thinking display, interleaved thinking, stop sequences,
top-p, request metadata, additional model fields, and response field paths.
The adapter also has better Claude thinking payloads, cache-point placement,
tool-result replay, request headers, region fallback, retries, and response
debug hooks while keeping AWS SDK credential-chain integration deferred.

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
- `sigma.OpenAIOptions` also covers structured output across OpenAI-compatible
  Chat Completions and Responses-family payloads, plus Chat Completions token
  logprob requests.
- OpenAI Responses replay now preserves existing provider item metadata or
  synthesizes bounded IDs for prior assistant text, reasoning, and function-call
  items.
- OpenAI Responses tool-result replay keeps image blocks inside
  `function_call_output` for image-capable models.
- OpenAI-compatible Chat Completions can opt into Anthropic-style cache markers
  and z.ai-style `tool_stream` payloads through compatibility metadata, and can
  suppress explicit `reasoning_effort` for models that reject it.
- OpenAI-compatible Chat Completions now detects OpenRouter reasoning as nested
  `reasoning.effort`, supports request-scoped OpenRouter routing overrides,
  and includes expanded OpenRouter routing metadata for price, quantization,
  throughput, latency, and object-form sorting preferences.
- OpenAI-compatible Chat Completions and OpenRouter Images now map
  `cache_write_tokens` into `Usage.CacheWriteInputTokens` while keeping cache
  reads and normalized input tokens separate.
- OpenCode Zen and OpenCode Go metadata now cover Kimi K2.6 DeepSeek-style
  thinking payloads without `reasoning_effort`, plus OpenCode Zen Grok Build
  0.1 reasoning-effort suppression.
- `provider/opencode` now routes selected OpenCode Zen and OpenCode Go model
  families to Google Generative AI, Anthropic Messages, OpenAI Responses, or
  OpenAI-compatible Chat Completions using generated model metadata hints.
- OpenCode metadata now includes representative routed entries for Gemini,
  Claude, Qwen, GPT/Codex, and Go MiniMax/Qwen route families, while known
  unavailable advertised models remain outside default promoted metadata.
- `cmd/sigma-surface-probe` provides an opt-in live OpenCode probe and repair
  workflow. It reports Sigma request-shape failures separately from provider
  capability limits and upstream model availability failures.
- `cmd/sigma-surface-probe` also provides opt-in live Fireworks probes for the
  OpenAI-compatible Fire Pass route and the Anthropic-compatible Messages
  route, with the same JSONL result and repair workflow.
- OpenAI Responses now normalizes Chat Completions-style function
  `tool_choice` payloads before sending Responses requests.
- OpenAI-compatible Chat Completions streams now accumulate streamed `logprobs`
  metadata across chunks.
- Generated metadata now seeds representative entries for the remaining exposed
  provider IDs, including current OpenAI-compatible, Anthropic-compatible, and
  Vertex compatibility metadata.
- Google Generative AI and Vertex AI now support typed Google request controls
  for tool choice and explicit disabled thinking.
- Google payloads now use `parametersJsonSchema` for function tools by default,
  retain only same-provider/API/model base64 thought signatures, group
  consecutive function responses, and replay image tool results for
  image-capable Gemini routes.
- Google streaming now synthesizes stable tool-call IDs when Google omits or
  duplicates IDs, maps additional safety finish reasons, and separates cached
  prompt tokens from ordinary input tokens while including thinking tokens in
  output token accounting.
- Native Anthropic generated metadata now includes current Claude Haiku, Sonnet,
  and Opus Messages rows, with adaptive-thinking metadata on supported models.
- Mistral Conversations now maps adjustable-reasoning models to
  `completion_args.reasoning_effort`, native Magistral models to
  `completion_args.prompt_mode`, parses streamed thinking chunks, sends
  `x-affinity` from `sigma.WithSessionID`, and normalizes replayed tool-call IDs.
- Generated Mistral metadata now includes representative adjustable-reasoning
  and native Magistral Conversations rows.
- `sigma.BedrockOptions` and `sigma.WithBedrockOptions` now expose typed
  Bedrock request controls for tool choice, thinking display, interleaved
  thinking, stop sequences, top-p, request metadata, additional model request
  fields, and additional model response field paths.
- Bedrock Converse Stream now maps provider-neutral reasoning levels to Claude
  adaptive or fixed-budget thinking payloads, omits thinking display for
  GovCloud targets, supports cache-point TTLs, groups consecutive tool results,
  preserves image tool-result content, applies request headers before SigV4
  signing, reads region fallback from `AWS_REGION`/`AWS_DEFAULT_REGION`, and
  uses the shared HTTP retry and response-debug-hook path.
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
- Anthropic Messages stream parsing now repairs malformed stream JSON and
  streamed tool-call arguments when possible, stops cleanly at `message_stop`
  before proxy trailers, and reports truncated streams while preserving partial
  content.
- Runtime behavior follows existing Sigma provider conventions: request-scoped
  auth, retries, timeouts, redacted debug hooks, typed provider errors, and
  cancellation mapping.
- Docs and provider parity now classify `openai-images` as a generation-only
  preview adapter instead of metadata-only.

## Compatibility

No persisted request JSON shapes changed. Public API additions are limited to
new registration helpers in `provider/openai`, new `OpenAIOptions` fields, new
`GoogleOptions` fields for Google tool choice and disabled thinking, new
`BedrockOptions` fields for Bedrock Converse controls, new OpenAI-compatible
compatibility metadata values including
`OpenAICompletionsCompat.SupportsReasoningEffort`, and new Anthropic Messages
compatibility metadata on `sigma.Model`.

Typed OpenAI structured-output and logprob controls fail locally with
`ErrorInvalidOptions` when requested for unsupported API families.
Typed Google tool choice fails locally with `ErrorInvalidOptions` when the
choice is not `auto`, `none`, or `any`.

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
- Full OpenCode catalog parity, including advertised-but-unavailable models and
  provider-specific feature quirks beyond the curated routed preview metadata.
- Live OpenCode or Fireworks surface validation in CI.
  `cmd/sigma-surface-probe` is credential-gated and remains outside
  deterministic release validation.
- Automated model catalog refresh from `models.dev` and provider catalog APIs;
  generated metadata still enters the release through the checked-in catalog,
  checksum test, and generated Go review flow.
- Broad OpenRouter text and image catalog expansion; only fixture-backed
  behavior and curated representative metadata are in scope for this tag.
- Mistral Conversations image input, built-in connectors, append/restart, and
  broad Mistral catalog expansion.
- Broad Bedrock catalog expansion, AWS SDK credential-chain integration,
  profiles, SSO, web identity, IMDS, shared AWS config loading, proxy-specific
  SDK behavior, and live Bedrock CI coverage.
- Live Google Gemini API and Vertex AI validation; deterministic fixtures remain
  the release evidence for Google preview adapter behaviour.
- Anthropic Claude Code OAuth identity headers and Claude Code tool-name
  canonicalization.
- GitHub Copilot and Cloudflare AI Gateway Anthropic Messages routing.
- Codex WebSocket session caching/fallback.
- Live OpenAI image tests; standard validation remains deterministic and
  credential-free.

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
The OpenAI Images adapter and OpenAI/Anthropic text-provider additions are
covered by deterministic `httptest` fixtures and golden request payloads; no
live provider network calls are required.
The OpenCode routed preview provider and surface probe helpers are covered by
deterministic route, metadata, and classification tests; live OpenCode probing
is optional and requires `OPENCODE_API_KEY`.
The Fireworks surface probe helpers are covered by deterministic route,
metadata, and classification tests; live Fireworks probing is optional and
requires `FIREWORKS_API_KEY`.
Mistral Conversations reasoning, thinking-stream, session-affinity, and
tool-call replay compatibility are covered by deterministic `httptest` fixtures
and golden request payloads.
Google Gemini API and Vertex AI request controls, thought-signature replay,
image tool-result replay, fallback tool-call IDs, and usage accounting are
covered by deterministic `httptest` fixtures and golden request payloads.
Bedrock typed options, Claude thinking payloads, cache points, grouped
tool-result replay, image tool-result replay, custom headers, environment
region fallback, retries, and response debug hooks are covered by deterministic
payload, fake-client, and `httptest` fixtures.
