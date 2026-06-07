# Changelog

All notable changes to this project will be documented in this file.

The project follows standard Major.Minor.Patch versioning and Go module
semantic import versioning. The initial release is `v0.1.0`; public APIs may
still change before `v1.0.0`, with breaking changes called out in release notes.

## [0.4.0] - TBC

See [release notes](docs/release-notes-v0.4.0.md).

### Added

- OpenCode Go DeepSeek V4 Flash Chat Completions requests now downgrade
  strict JSON Schema response formats to JSON object mode, avoiding provider
  rejection while preserving JSON-mode generation.
- OpenAI Responses requests now default to `store: false`, include encrypted
  reasoning replay metadata when reasoning is enabled, and default reasoning
  summaries to `auto` while preserving explicit caller overrides.
- OpenAI-compatible Chat Completions replay now omits empty assistant history
  turns and can opt specific compatibility routes into empty `tools: []`
  payloads when prior tool-call history requires the tools field.
- Google Vertex AI routing remains an explicit provider contract: callers pass
  project/location through `VertexConfig` or provider options and supply
  ADC/OAuth tokens with `WithVertexTokenProvider`, while ambient routing and
  built-in ADC discovery remain deferred.
- Vertex AI now has first-class non-Gemini provider registrations for
  OpenAI-compatible MaaS routes and Anthropic Claude `streamRawPredict` routes,
  including shared Vertex project/location routing, API-key or OAuth token auth,
  placeholder credential fallback, and representative generated metadata.
- Anthropic Messages now has typed Sigma options for tool choice, thinking
  display, and explicit interleaved-thinking beta opt-in while preserving raw
  provider options for advanced fields.
- Mistral Conversations now supports base64 image input and image-bearing tool
  results for direct Pixtral models, with generated Pixtral metadata advertising
  text and image input support.
- Bedrock Converse Stream now derives the runtime endpoint and
  `eu-central-1` region for built-in EU regional inference-profile rows when no
  explicit region, endpoint, or AWS region environment variable is configured.
- Google Gemini API and Vertex AI now have preview image generation adapters
  for Imagen and Gemini image output using Sigma's provider-neutral
  `ImageProvider` surface.
- Google Gemini API, Google Vertex AI, and Amazon Bedrock now have preview
  embedding adapters using Sigma's provider-neutral `EmbeddingProvider`
  surface, with representative generated model metadata.
- Bedrock embeddings use `InvokeModel` through the existing stdlib Bedrock
  region, endpoint, credential, retry, debug, and SigV4 paths for Titan,
  Cohere, and Nova text embedding request shapes.

## [0.3.0] - 2026-06-05

See [release notes](docs/release-notes-v0.3.0.md).

### Added

- Generated image metadata now includes the OpenRouter-routed Grok Imagine
  image-quality model, marked with xAI routing metadata and `OPENROUTER_API_KEY`
  credential discovery.
- Generated text and image metadata has been refreshed with curated current
  rows for the supported Sigma provider IDs, including broader OpenAI,
  Anthropic, Google, Vertex AI, Mistral, Bedrock, OpenCode, and metadata-only
  OpenAI-compatible model coverage.
- Generated text metadata now includes focused metadata-only rows for Azure
  OpenAI Responses, OpenAI Codex Responses, Cloudflare AI Gateway,
  Cloudflare Workers AI, NVIDIA NIM, Z.ai, Ant Ling, Moonshot AI, MiniMax,
  Vercel AI Gateway, and expanded GitHub Copilot routes where existing Sigma
  adapters can carry the API shape.
- OpenAI-compatible Chat Completions reasoning metadata now supports Together,
  Qwen, Z.ai, and Ant Ling request formats, including Z.ai `tool_stream`
  payloads for tool-enabled requests.
- OpenCode Zen and OpenCode Go generated metadata now includes the promoted
  DeepSeek V4 Flash and MiniMax M3 routed rows, stricter unsupported thinking
  levels for known OpenCode reasoning models, adaptive Anthropic thinking
  metadata for selected Claude routes, and temperature suppression for
  OpenCode Claude models that reject temperature.
- Generated OpenRouter image metadata now includes the stable Gemini image route
  and additional current image-generation routes while keeping broad OpenRouter
  text expansion deferred to the catalog refresh workflow.
- OpenAI Chat Completions, Responses, and Codex Responses now derive bounded
  `prompt_cache_key` values from `sigma.WithSessionID` when prompt caching is
  enabled, and map long-lived cache retention to OpenAI's `24h` retention where
  supported.
- OpenAI-compatible Chat Completions and direct OpenAI Responses now emit
  session-affinity headers from `sigma.WithSessionID` when prompt caching is
  enabled, while preserving explicit caller header overrides.
- OpenAI-compatible Chat Completions replay now normalizes prior Responses-style
  `call_id|item_id` tool-call identifiers before sending Chat Completions
  history.
- OpenAI Responses replay now omits stale function-call item IDs when carrying
  same-provider history across different OpenAI Responses models.
- OpenAI-compatible Chat Completions can carry image tool results forward as a
  single batched follow-up user image message after consecutive tool results
  for image-capable models, while preserving the ordinary text or placeholder
  tool-result messages.
- OpenAI Responses now emits explicit automatic image detail on user image
  inputs and image-capable `function_call_output` image parts.
- OpenAI Images now supports reference-image edits through
  `ImageRequest.Inputs`, explicit `ImageOperationVariation` requests for
  `dall-e-2`, and `ImageRequest.Mask` for edit masks.
- OpenAI Images edits can send URL and file-ID image references through JSON
  request bodies when no binary image upload is required.
- Image providers can expose streaming through `Client.StreamImages`, and the
  OpenAI Images adapter can request partial image events with `stream` and
  `partial_images` while still supporting `GenerateImages`.
- OpenAI Responses image-generation tool output is parsed into assistant image
  content blocks, including partial image events during streaming.
- OpenAI-compatible stream parsing now recognizes Chat Completions
  `reasoning_text` deltas and Responses/Codex refusal and reasoning-text
  events.
- OpenAI-compatible Chat Completions and Responses now normalize invalid UTF-8
  text at request, replay, and stream boundaries before provider dispatch or
  final message persistence.
- OpenAI-compatible GitHub Copilot routes now add dynamic initiator, intent,
  and vision request headers while preserving explicit caller header overrides.
- OpenAI-compatible Cloudflare AI Gateway routes now resolve environment-backed
  base URL placeholders and send API keys through Cloudflare's gateway auth
  header without broad catalog promotion.
- OpenAI Codex Responses now has stdlib-only browser callback and device-code
  OAuth login, token refresh helpers, and an in-memory OAuth token provider that
  callers can wrap with their own credential persistence.
- OpenAI Codex Responses now sends Codex backend request headers for OAuth
  account routing, Responses SSE beta access, originator identity, and
  session-scoped request IDs, and aligns Codex payloads with ChatGPT backend
  requirements for required instructions, disabled storage, and unsupported
  output-token caps and response replay IDs.
- OpenAI Codex Responses now supports stdlib-only direct WebSocket transport
  with session caching, delta replay, cleanup helpers, and SSE fallback before
  stream output starts.
- OpenAI Responses and Codex Responses usage accounting now reports cached
  input tokens as cache reads instead of ordinary input tokens.
- OpenAI Responses and Codex Responses cost reporting now accounts for
  request/response service-tier pricing multipliers for `flex` and `priority`
  tiers.
- `cmd/sigma-surface-probe` can run opt-in live OpenAI Responses probes with
  `OPENAI_API_KEY` and OpenAI Codex Responses probes with browser callback
  OAuth, device-code OAuth, or caller-supplied Codex OAuth tokens, defaulting
  Codex live probes to the latest ChatGPT-supported Codex fallback.
- Provider execution errors now expose typed `sigma.ClassifyError` results with
  stable auth, quota, billing, context-overflow, rate-limit, transient,
  invalid-request, provider, and unknown classes plus retry-after hints.
- Google Generative AI and Vertex AI now honor concrete model-scoped `baseURL`
  and `headers` metadata with request/provider options retaining higher
  precedence, while Vertex ignores generated `{location}` base URL templates.
- Google Vertex AI auto credential mode now treats placeholder API-key values
  as unavailable so configured OAuth/ADC token providers can be used instead.
- Google replay now normalizes tool-call IDs for Google-hosted model families
  that require explicit function IDs, and omits empty function-response IDs for
  native Gemini requests.
- Sigma now exposes a provider-neutral vector embeddings API with embedding
  model discovery, request-scoped embedding options, redacted embedding debug
  hooks, OpenAI `/v1/embeddings` support, and generated metadata for
  `text-embedding-3-small` and `text-embedding-3-large`.
- Embedding results now include typed SDK-level attempt metadata for provider,
  API, model, retry attempt, status code, request ID, and per-attempt latency.
- `sigma.OpenAICompatibleEmbeddingModel` now constructs metadata for caller-
  registered OpenAI-compatible embedding endpoints without hand-written model
  metadata maps.
- `openai.RegisterLocalEmbeddings` now registers an explicit local
  OpenAI-compatible embeddings provider/model pair with Ollama-friendly
  defaults and normalized `/v1` base URLs.
- Embedding model metadata now exposes supported dimension ranges alongside
  default dimensions, max input tokens, and input-token pricing.
- Embedding batches can now use `Client.EmbedBatch` for duplicate input reuse,
  retry-aware batch splitting, optional oversized-input splitting, progress
  callbacks, and aggregate status/request/usage/cost summaries while preserving
  the existing provider-neutral embedding contracts.
- `Client.EmbedBatch` now honors model and request-level embedding batch limits,
  supports cross-call embedding caches keyed by provider/model/dimensions and
  SHA-256 input hashes, uses safer UTF-8-aware split boundaries for oversized
  inputs, and records structured batch trace events for caller aggregation.
- Embedding error classification now marks context-overflow, request-too-large,
  and local tokenizer EOF failures as split-recoverable without treating them as
  same-request retries.
- Embedding requests now support provider-neutral query/document intent via
  `EmbeddingInputType`, `EmbeddingQuery`, and `EmbeddingDocuments`, with
  explicit newline normalization through `NormalizeEmbeddingNewlines`.
- `sigma.NewEmbeddingEmbedder` now wraps a client and embedding model with
  small query/document embedding helpers while preserving Sigma's explicit
  newline-normalization policy.
- Embedding vector utilities now provide deterministic dot product, cosine
  similarity, normalization, weighted vector combination, and cosine-ranking
  helpers with typed errors for numeric edge cases.
- Embedding retrieval primitives now include `RetrievalDocument`,
  `RetrievalChunk`, deterministic character-based splitting, metadata-copying
  document splitting, and `RetrievalResult` values that do not expose stored
  vectors.
- `InMemoryRetrievalIndex` now provides a compact in-memory retrieval helper
  that embeds documents with `EmbeddingInputTypeDocument`, embeds searches with
  `EmbeddingInputTypeQuery`, routes provider work through `Client.EmbedBatch`,
  stores normalized vectors internally, and returns stable cosine-ranked
  results.

### Compatibility

- The direct xAI/Grok provider remains a preview Chat Completions adapter.
  Grok image generation is represented through OpenRouter image metadata rather
  than a direct xAI image provider.
- Anthropic-style OpenAI-compatible cache markers continue to use their
  endpoint-specific `cache_control` format rather than OpenAI-native prompt
  cache fields.

### Known limitations

- Default registry entries are metadata-only; applications must import provider
  packages and call their `Register` functions before runtime dispatch.
- OpenAI image generation remains preview. Live image validation is
  credential-gated and outside deterministic CI.
- Preview providers are not part of the first release gate and may change before
  `v1.0.0`.
- OAuth token persistence is caller-owned. OpenAI Codex Responses includes
  browser callback login, device-code login, and refresh helpers, but does not
  write credentials to disk.
- Anthropic Claude Code OAuth identity headers and Claude Code tool-name
  canonicalization are deferred with the broader OAuth/provider-specific
  compatibility work.
- WebSocket transport is currently implemented only for OpenAI Codex Responses;
  unsupported transport choices for other routes should fail locally before
  network calls.
- Proxy-aware Codex WebSocket dialing remains deferred; proxy-constrained
  environments should use SSE fallback.
- Token usage and cost reporting come from provider usage data and model
  metadata; tokenizer-based token estimates are deferred.
- Built-in embeddings now include representative OpenAI, Google Gemini API,
  Google Vertex AI, and Amazon Bedrock text embedding models. External vector
  stores, tokenizer-aware chunking and estimates, provider-selection fallback,
  broad provider promotion, and live embedding probes remain deferred.
- Built-in model metadata is still refreshed through the curated checked-in
  catalog; automated `models.dev`/provider-catalog ingestion is deferred until
  it can preserve deterministic review and fixtures.
- Mistral Conversations built-in connectors, append/restart, URL/file image
  references, and broad catalog expansion remain deferred until their request
  shapes are covered by deterministic fixtures.
- Bedrock credential-chain integration, profiles, SSO, web identity, IMDS,
  shared AWS config loading, broader regional alias expansion beyond the built-in
  EU inference-profile fallback, and live Bedrock CI coverage remain deferred.
- Anthropic-compatible Fireworks model routing remains deferred; the built-in
  Fireworks row continues to target the OpenAI-compatible Fire Pass route.
- Live Google Gemini API and Vertex AI validation remains deferred; deterministic
  fixtures are the release evidence for the Google preview adapters.
- The Go package targets server/CLI use; browser-specific behavior is out of
  scope for this release.
- Agent runtime orchestration and cross-provider context handoff with
  capability-loss reporting are deferred to later integration work; this release
  exposes only provider-neutral primitives.
- DeepSeek, Groq, Cerebras, Together, GitHub Copilot, Cloudflare, NVIDIA,
  Z.ai, Ant Ling, Moonshot AI, MiniMax, Kimi, and Xiaomi are not yet
  first-class provider rows; generated metadata and routing may exist, but
  independent provider-quality claims still need fixtures.
- Future xAI/Grok catalog refreshes and provider-specific Grok
  request semantics beyond the preview Chat Completions adapter remain
  deferred until they have deterministic coverage.
- No live provider calls are required or expected for release validation.
  Live OpenCode, Fireworks, and xAI/Grok probing is available through
  `cmd/sigma-surface-probe`, but it is credential-gated and outside the
  deterministic release gate.

## [0.2.0] - 2026-05-31

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
- OpenAI-specific typed request options for structured output and Chat
  Completions token logprobs, with local validation for unsupported API
  families.
- OpenAI Responses replay now preserves or synthesizes bounded provider item
  IDs for prior assistant text, reasoning, and function-call items.
- OpenAI Responses tool-result replay can keep image blocks inside
  `function_call_output` for image-capable models.
- OpenAI-compatible Chat Completions compatibility metadata now supports
  Anthropic-style cache markers, opt-in `tool_stream` payloads, and
  model-specific suppression of explicit `reasoning_effort`.
- OpenAI-compatible Chat Completions now maps OpenRouter reasoning requests to
  nested `reasoning.effort`, supports request-scoped OpenRouter routing
  overrides, and exposes expanded OpenRouter routing metadata.
- OpenAI-compatible Chat Completions and OpenRouter Images now account for
  provider-reported prompt cache writes separately from cache reads.
- xAI/Grok now has a first-class preview provider package in `provider/xai`,
  reusing the OpenAI-compatible Chat Completions adapter with xAI defaults,
  `XAI_API_KEY` credential fallback, and deterministic streaming, tools, error,
  redaction, cancellation, and context-overflow coverage.
- Generated xAI/Grok text metadata now includes curated Grok 3, Grok 4.20,
  Grok 4.3, Grok Build, and Grok Code routes with xAI compatibility metadata.
- Anthropic Messages compatibility metadata for Anthropic-compatible endpoints,
  including eager tool input streaming, cache/session-affinity support, empty
  thinking-signature replay, and budget/adaptive thinking formats.
- Anthropic Messages now sends explicit disabled thinking for reasoning-capable
  models, supports adaptive thinking `output_config.effort`, omits temperature
  while thinking is enabled, groups consecutive tool results, emits block-end
  events at `content_block_stop`, repairs malformed stream JSON and streamed
  tool-call arguments when possible, stops cleanly at `message_stop`, reports
  truncated streams, and preserves stream-start usage when later deltas are
  partial.
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
- `cmd/sigma-surface-probe` can also run opt-in live Fireworks probes for both
  the OpenAI-compatible Fire Pass route and the Anthropic-compatible Messages
  route, using `FIREWORKS_API_KEY`.
- `cmd/sigma-surface-probe` can run opt-in live xAI/Grok surface probes over
  the OpenAI-compatible Chat Completions route, using `XAI_API_KEY`.
- OpenAI Responses now normalizes Chat Completions-style function
  `tool_choice` objects to the Responses function-choice shape.
- OpenAI-compatible Chat Completions stream metadata now accumulates streamed
  `logprobs` chunks instead of keeping only the latest chunk.
- Generated text metadata now includes representative metadata-only entries for
  every exposed non-custom provider ID, aligned with current compatibility
  metadata and generated base URL/header handling.
- Google Generative AI and Vertex AI now expose typed Google request controls
  for tool choice and explicit disabled thinking, with deterministic validation
  for unsupported tool-choice values.
- Google payload conversion now replays thought signatures only when they come
  from the same provider/API/model and are valid Google base64 signatures,
  sends JSON Schema tools through `parametersJsonSchema` by default, and keeps
  a legacy sanitized `parameters` escape hatch for compatible endpoints.
- Google tool-result replay now groups consecutive function responses and can
  carry image tool results for image-capable models, nesting images for Gemini
  3+ and using a sidecar image turn for older Gemini routes.
- Google stream parsing now synthesizes stable tool-call IDs when responses omit
  or duplicate IDs, maps additional Google safety finish reasons, and separates
  cached prompt tokens from ordinary input tokens while counting thinking tokens
  as billable output.
- Native Anthropic metadata now includes current Claude Haiku, Sonnet, and Opus
  Messages rows, including adaptive-thinking metadata for supported models.
- Mistral Conversations now supports provider-neutral reasoning controls,
  streamed thinking chunks, `x-affinity` session reuse through
  `sigma.WithSessionID`, and stable replay of cross-provider tool-call IDs.
- Generated Mistral metadata now includes representative adjustable-reasoning
  and native Magistral Conversations rows.
- Amazon Bedrock Converse Stream now has typed `sigma.BedrockOptions` for tool
  choice, thinking display, interleaved thinking, stop sequences, top-p,
  request metadata, additional model request fields, and response field paths.
- Amazon Bedrock Converse Stream now maps provider-neutral reasoning levels to
  Claude adaptive or fixed-budget thinking payloads, supports cache-point TTLs,
  groups consecutive tool results, preserves image tool-result content, applies
  request headers before SigV4 signing, reads region fallback from AWS region
  environment variables, and uses Sigma's shared HTTP retry and response-debug
  hooks.
- Release docs now record the deferred model-registry generation plan, including
  future `models.dev` ingestion, source precedence, refresh reports, and the
  deterministic catalog review gate.

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
- OpenAI-compatible Chat Completions first-release coverage, including
  custom/local endpoints, compatibility metadata, streaming text, image input,
  tools, usage, errors, redaction, and cancellation fixtures.
- Anthropic Messages first-release coverage, including Anthropic-compatible
  routing, streaming text, image input, thinking, tools, cache markers, usage,
  errors, and deterministic fixtures.
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
- Documentation for release scope, providers, streaming, tools, images, reasoning,
  errors, custom models, testing, persistence, design inspiration, provider
  parity, security, and generated metadata.
