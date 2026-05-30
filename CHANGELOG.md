# Changelog

All notable changes to this project will be documented in this file.

The project follows standard Major.Minor.Patch versioning and Go module
semantic import versioning. The initial release is `v0.1.0`; public APIs may
still change before `v1.0.0`, with breaking changes called out in release notes.

## [0.1.0] - 2026-05-29

See [release notes](docs/release-notes-v0.1.0.md).

### Added

- The repository is licensed under the MIT License.
- Root `sigma` package API for provider-neutral model metadata, requests,
  messages, content blocks, tools, usage, cost, images, streams, diagnostics,
  persistence, retries, credentials, and typed errors.
- `Client`, package-level helpers, and `Registry` APIs for isolated model and
  provider registration.
- Deterministic `sigmatest` providers for text and image tests without live
  network calls.
- Text completion and streaming contracts with ordered events, final assistant
  messages, cancellation handling, provider errors, tool-call deltas, thinking
  blocks, usage, and cost accounting.
- Context-aware SSE reads and shared stream lifecycle helpers in `internal/sse`
  and `internal/streamlifecycle`.
- Provider-defined tools alongside JSON-schema function tools (for example
  Anthropic web search, web fetch, and code execution).
- JSON persistence helpers for request replay, with validation for unknown
  persisted request fields.
- OpenAI-compatible Chat Completions MVP coverage, including custom/local
  endpoints, compatibility metadata, streaming text, image input, tools, usage,
  errors, redaction, and cancellation fixtures.
- Anthropic Messages MVP coverage, including Anthropic-compatible routing,
  streaming text, image input, thinking, tools, cache markers, usage, errors,
  and deterministic fixtures.
- Preview adapters for OpenAI Responses, Azure OpenAI Responses, OpenAI Codex
  Responses, Fireworks AI Chat Completions, OpenCode Zen and OpenCode Go Chat
  Completions, Google Generative AI, Google Vertex AI, Mistral Conversations,
  Amazon Bedrock Converse Stream, and OpenRouter image generation.
- Fireworks reasoning effort and thinking-budget controls over the shared
  OpenAI-compatible Chat Completions path.
- Amazon Bedrock Converse Stream over stdlib HTTP with SigV4 signing and
  EventStream parsing, without an AWS SDK dependency.
- Generated model metadata from a curated checked-in catalog, plus local
  generation tooling.
- Security tests and redaction helpers for provider errors, request/response
  debug hooks, credential formatting, persistence boundaries, and synthetic
  secret fixtures.
- Documentation for MVP scope, providers, streaming, tools, images, reasoning,
  errors, custom models, testing, persistence, design inspiration, provider
  parity, security, and generated metadata.

## [0.2.0] - TBC

See [release notes](docs/release-notes-v0.2.0.md).

### Added

- OpenAI Images generation adapter in `provider/openai`, with
  `RegisterImages`, `RegisterImagesDefault`, request-scoped auth, custom
  headers, retry/timeout handling, debug hooks, typed provider errors, and
  deterministic `httptest` coverage.
- OpenAI Images request payload support for prompt, model override, count, size,
  quality, output MIME type, and `extra_body` provider options.
- OpenAI Images response mapping for base64 image data, URL outputs, token
  usage, revised prompts, and provider metadata.
- OpenAI-specific request options for Chat Completions `tool_choice`,
  Responses/Codex `prompt_cache_retention`, Responses/Codex
  `parallel_tool_calls`, and Responses/Codex text verbosity.
- OpenAI Responses replay now preserves or synthesizes bounded provider item
  IDs for prior assistant text, reasoning, and function-call items.
- OpenAI Responses tool-result replay can keep image blocks inside
  `function_call_output` for image-capable models.
- OpenAI-compatible Chat Completions compatibility metadata now supports
  Anthropic-style cache markers, opt-in `tool_stream` payloads, and
  model-specific suppression of explicit `reasoning_effort`.
- Anthropic Messages compatibility metadata for Anthropic-compatible endpoints,
  including eager tool input streaming, cache/session-affinity support, empty
  thinking-signature replay, and budget/adaptive thinking formats.
- Anthropic Messages now sends explicit disabled thinking for reasoning-capable
  models, supports adaptive thinking `output_config.effort`, omits temperature
  while thinking is enabled, groups consecutive tool results, emits block-end
  events at `content_block_stop`, and preserves stream-start usage when later
  deltas are partial.
- Provider parity and image-generation docs now mark `openai-images` as a
  generation-only preview adapter instead of metadata-only.
- OpenCode Zen and OpenCode Go metadata now cover the promoted
  OpenAI-compatible `kimi-k2.6` and `grok-build-0.1` gaps, with deterministic
  payload fixtures for Kimi thinking and Grok Build reasoning-effort
  suppression.
- OpenCode Zen and OpenCode Go now have a routed preview provider that
  dispatches selected model families to Google Generative AI, Anthropic
  Messages, OpenAI Responses, or OpenAI-compatible Chat Completions based on
  model metadata, with deterministic route tests and curated metadata hints.
- `cmd/sigma-surface-probe` can run opt-in live OpenCode Zen/Go surface probes,
  including repair variants that distinguish Sigma request-shape issues,
  provider capability limits, and upstream availability failures.
- OpenAI Responses now normalizes Chat Completions-style function
  `tool_choice` objects to the Responses function-choice shape.
- OpenAI-compatible Chat Completions stream metadata now accumulates streamed
  `logprobs` chunks instead of keeping only the latest chunk.
- Generated text metadata now includes representative metadata-only entries for
  every exposed non-custom provider ID, aligned with current compatibility
  metadata and generated base URL/header handling.
- Release docs now record the deferred model-registry generation plan, including
  future `models.dev` ingestion, source precedence, refresh reports, and the
  deterministic catalog review gate.

### Known limitations

- Default registry entries are metadata-only; applications must import provider
  packages and call their `Register` functions before runtime dispatch.
- OpenAI Images is generation-only. Reference-image edits, variations,
  streaming partial images, and Responses image-tool generation remain deferred.
- Preview providers are not part of the first release gate and may change before
  `v1.0.0`.
- Interactive OAuth login and token persistence are deferred; the MVP uses
  caller-supplied credentials or injected OAuth token providers only. OpenAI
  Codex Responses in particular requires a caller-supplied OAuth token provider.
- Anthropic Claude Code OAuth identity headers and Claude Code tool-name
  canonicalization are deferred with the broader OAuth/provider-specific
  compatibility work.
- WebSocket transports are deferred; unsupported transport choices should fail
  locally before network calls.
- Codex WebSocket session caching/fallback remains deferred; Codex Responses
  continues to use SSE with a caller-supplied OAuth token provider.
- Token usage and cost reporting come from provider usage data and model
  metadata; tokenizer-based token estimates are deferred.
- Built-in model metadata is still refreshed through the curated checked-in
  catalog; automated `models.dev`/provider-catalog ingestion is deferred until
  it can preserve deterministic review and fixtures.
- The Go package targets server/CLI use; browser-specific behavior is out of
  scope for the MVP.
- Agent runtime orchestration and cross-provider context handoff (with
  capability-loss reporting) are deferred to later integration work; the MVP
  exposes only provider-neutral primitives.
- DeepSeek, Groq, Cerebras, xAI, Together, GitHub Copilot, Kimi, and Xiaomi are
  not yet first-class provider rows; generated metadata and routing may exist,
  but independent provider-quality claims still need fixtures.
- No live provider calls are required or expected for release validation.
  Live OpenCode probing is available through `cmd/sigma-surface-probe`, but it
  is credential-gated and outside the deterministic release gate.
- The release should not be tagged until maintainers accept the verification
  results and the [release notes](docs/release-notes-v0.2.0.md).
