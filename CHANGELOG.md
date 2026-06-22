# Changelog

All notable changes to this project will be documented in this file.

The project follows standard Major.Minor.Patch versioning and Go module
semantic import versioning. The initial release is `v0.1.0`; public APIs may
still change before `v1.0.0`, with breaking changes called out in release notes.

## [0.6.0] - Unreleased

See [release notes](docs/release-notes-v0.6.0.md).

### Added

- Ant Ling now has a first-class OpenAI-compatible Chat Completions provider
  wrapper, including base URL defaults, bearer auth, generated Ling/Ring
  metadata reuse, Ant Ling reasoning-object compatibility, and deterministic
  registration/request coverage.
- Z.ai and Z.ai Coding CN now have first-class OpenAI-compatible Chat
  Completions provider wrappers, including base URL defaults, bearer auth, API-key
  discovery, and deterministic registration/request coverage.
- Generated Z.ai and Z.ai Coding CN metadata now includes `glm-5.2` with
  provider-specific reasoning-effort mapping, `tool_stream` support, and
  GLM-family model metadata.
- Xiaomi now has a first-class OpenAI-compatible provider wrapper for the
  API-billing route and regional token-plan routes, including generated
  metadata, regional API-key discovery, and deterministic registration/request
  coverage for `mimo-v2.5-pro-ultraspeed` and the token-plan MiMo rows.
- Kimi Coding now has a first-class Anthropic-compatible provider wrapper and
  generated metadata for `k2p7`, `kimi-for-coding`, and `kimi-k2-thinking`,
  including `KIMI_API_KEY` discovery, Kimi CLI request headers, adaptive
  thinking metadata, and session-affinity support.
- GitHub Copilot now has stdlib-only device-code OAuth login, Copilot token
  refresh helpers, an in-memory OAuth token provider that also implements
  Sigma's auth resolver interface, and explicit opt-in helpers for enabling
  Copilot model policies while keeping credential persistence caller-owned.
- OpenAI Codex Responses WebSocket transport now honors standard HTTP(S) proxy
  environment variables with `NO_PROXY` exclusions by tunneling through
  HTTP/HTTPS `CONNECT` proxies while preserving the existing SSE fallback.
- OpenAI Codex Responses WebSocket transport now has a Codex-specific connect
  timeout plus session-cache debug stats for created/reused connections,
  full/delta context requests, previous response IDs, WebSocket failures, and
  SSE fallback activation.
- Anthropic Messages usage now preserves long prompt-cache write tokens
  separately and prices those writes at the provider's long-cache input
  multiplier while keeping total cache-write tokens unchanged.
- Text-generation usage now carries provider/model identity, provider raw usage
  payloads, normalized tool-use input tokens, and provider-reported cost when
  available, while keeping Sigma's model-metadata cost estimate separate.
- Mistral Conversations now maps cache-enabled `sigma.WithSessionID` requests
  to both `prompt_cache_key` and `x-affinity`, and streamed Mistral cached
  prompt tokens now populate `Usage.CacheReadInputTokens` instead of ordinary
  input tokens.
- `EnvironmentAuthResolver` now exposes non-secret environment credential
  discovery helpers for ordered candidate variable names and configured
  variable names, with broader built-in API-key defaults for OpenAI-compatible
  provider IDs that previously relied only on generated model metadata.
- Cloudflare AI Gateway and Amazon Bedrock now expose provider-specific
  request configuration helpers for AI Gateway account/gateway placeholder
  resolution, Bedrock request regions, and Bedrock request-scoped static AWS
  credentials while preserving existing environment fallbacks.
- Cloudflare Workers AI now has a first-class OpenAI-compatible Chat
  Completions wrapper for direct Workers AI routes, including request-scoped
  account placeholder resolution, normal bearer-token auth, generated metadata,
  and deterministic registration/request coverage.
- Vercel AI Gateway now has a first-class Anthropic-compatible Messages
  provider wrapper, including base URL defaults, API-key discovery through
  existing metadata, generated gateway model metadata reuse, and deterministic
  registration/request coverage.
- DeepSeek, Groq, Cerebras, and Together now have first-class
  OpenAI-compatible Chat Completions provider wrappers, including base URL
  defaults, bearer auth, generated metadata reuse, and deterministic
  registration, request, error, and cancellation coverage.
- NVIDIA NIM now has first-class OpenAI-compatible Chat Completions and
  Embeddings provider wrappers, including base URL defaults, bearer auth,
  generated text and embedding metadata, embedding input-type mapping,
  streaming-usage request defaults, an opt-in live surface-probe route, and
  deterministic registration/request coverage. The generated text catalog now
  also includes direct NIM rows for `openai/gpt-oss-120b` and
  `nvidia/nemotron-3-ultra-550b-a55b`, plus opt-in live `/models` validation
  for reviewing direct NIM catalog availability while normal generation remains
  offline.
- Moonshot AI and Moonshot AI CN now have first-class OpenAI-compatible Chat
  Completions provider wrappers, generated Kimi K2.7 Code CN and HighSpeed
  metadata, and metadata-driven omission of disabled-thinking payloads for
  K2.7 Code routes that reject explicit thinking-off requests.
- `cmd/sigma-surface-probe` now has an opt-in cross-provider handoff diagnostic
  that builds small tool-call contexts and replays them pairwise across selected
  live routes without adding live provider calls to CI.
- Assistant messages and content blocks now expose provider-neutral source and
  citation accessors, letting callers read normalized URLs, URIs, titles,
  offsets, cited text, and copied provider metadata without scraping opaque
  provider metadata maps directly.
- `sigma.ValidateToolCall` now strictly evaluates `anyOf`, `oneOf`, and `allOf`
  in tool input schemas, including nested property, array item, and additional
  property schemas, so invalid composed tool arguments are rejected before tool
  execution.
- Deterministic provider tests now cover Google stream `thoughtSignature`
  replay on signature-only chunks and OpenAI-compatible Chat Completions replay
  of prior thinking blocks as assistant text when `reasoning_content` is not
  required.
- Deterministic request-conversion tests now lock OpenAI Responses replay IDs,
  OpenAI-compatible Chat Completions request-shape guardrails, routed stream
  model metadata, and Google legacy tool-schema sanitization without changing
  provider APIs.

### Fixed

- OpenAI-compatible Chat Completions streams now surface provider
  `finish_reason` values of `network_error` and `model_context_window_exceeded`
  as errors instead of successful unknown stops, preserving context-overflow
  classification for the latter.

## [0.5.0] - 2026-06-13

See [release notes](docs/release-notes-v0.5.0.md).

### Added

- Bedrock Converse Stream now derives the runtime region from application
  inference profile ARNs on the model or request/provider options before AWS
  region environment fallbacks, while preserving explicit region overrides.
- Bedrock Converse Stream now accepts a request-scoped bearer token through
  typed Bedrock options, taking precedence over auth resolvers and environment
  credential fallbacks.
- Mistral Conversations now has typed tool-choice controls for automatic,
  required, disabled, any-tool, and named-tool selection while preserving raw
  provider options for advanced request fields.
- The model metadata generator now has an opt-in deterministic catalog summary
  report covering source count, text/image/embedding totals, text
  tool/reasoning counts, and provider/API buckets, with embedding generation
  included in deterministic-render coverage.
- Generated OpenRouter image metadata now includes the MAI Image 2.5 and
  Riverflow 2.5 routed rows, keeping broad OpenRouter text expansion deferred
  to the catalog refresh workflow.
- Generated Anthropic metadata now includes Claude Fable 5 with adaptive
  thinking metadata, xhigh thinking-level mapping, image input support, current
  limits, and pricing.
- Fireworks now exposes a separate Anthropic-compatible provider registration
  path and generated metadata for `accounts/fireworks/models/kimi-k2p6` under
  the `fireworks-anthropic` provider ID. Kimi K2.7 Code is also available on
  both the OpenAI-compatible `fireworks` route and the Anthropic-compatible
  `fireworks-anthropic` route as `accounts/fireworks/models/kimi-k2p7-code`.
- Anthropic Messages now has typed options for native `output_format` payloads
  and `disable_parallel_tool_use` tool-choice controls.
- Bedrock Converse Stream now supports typed structured-output requests by
  synthesizing a schema tool and returning the structured tool arguments as
  assistant text while preserving real tool calls.
- Anthropic Messages now omits the disabled-thinking payload for models whose
  compatibility metadata marks disabled thinking as unsupported, and generated
  Claude Fable 5 metadata now sets that flag because the model rejects explicit
  `thinking: disabled` requests.
- OpenAI-compatible Z.ai reasoning requests now send `thinking` objects with
  enabled or disabled types instead of the legacy `enable_thinking` toggle.
- Generated Moonshot AI and Moonshot AI CN metadata now uses the DeepSeek-style
  thinking format and streaming-usage support so thinking-off requests
  explicitly disable reasoning and streamed usage can be requested.
- OpenAI-compatible Moonshot routes are now detected from the provider ID or
  `api.moonshot.*` host, applying the Moonshot `max_tokens`,
  developer-role, store, strict-tool, and DeepSeek-style thinking request
  shape even for caller-registered models.
- Generated Moonshot AI metadata now includes the direct Kimi K2.7 Code row
  with text/image input, reasoning, tool support, current limits, pricing, and
  `MOONSHOT_API_KEY` discovery.
- MiniMax and MiniMax CN now have a first-class Anthropic-compatible provider
  wrapper, and generated direct MiniMax metadata now targets the
  `/anthropic/v1` base URL used by Sigma's Messages adapter.
- GitHub Copilot now has a first-class compatible provider wrapper for Chat
  Completions, Responses, and Anthropic Messages routes, including Copilot base
  URL defaults, dynamic request headers, bearer auth, and
  `COPILOT_GITHUB_TOKEN` environment credential discovery.
- Cloudflare AI Gateway now has first-class compatible provider wrappers for
  OpenAI-compatible and Anthropic-compatible text routes, including
  environment-backed account/gateway base URL placeholders and
  `cf-aig-authorization` gateway auth.
- OpenCode Zen and OpenCode Go Chat Completions now send explicit `max_tokens`
  instead of `max_completion_tokens`, matching the OpenCode request shape.
- Generated OpenCode Go metadata now uses `reasoning_effort` requests for Kimi
  K2.6 and Kimi K2.7 Code, avoiding rejected disabled `thinking` objects for
  thinking-off/default requests.
- Generated Azure GPT-5.4 and GPT-5.5 context windows now match the
  1,050,000-token Azure Foundry deployments, and OpenAI/Azure GPT-5 Pro max
  output tokens are corrected to 128,000.
- Bedrock Converse Stream now replaces blank required user and tool-result text
  with an `<empty>` placeholder and drops blank replayed assistant text blocks,
  which Bedrock would otherwise reject.
- Bedrock provider errors now link the AWS data-retention documentation when a
  model rejects the configured data retention mode.
- Provider error classification now recognizes additional context-overflow
  messages from OpenAI-compatible routes, OpenRouter, Together, Copilot, Kimi,
  MiniMax, and local OpenAI-compatible endpoints. `sigma.IsContextOverflow`
  can also identify final assistant messages that report provider diagnostics
  or caller-supplied context-window usage consistent with overflow.
- Anthropic Messages now has stdlib-only browser callback OAuth login for
  Claude Pro/Max subscriptions, token refresh helpers, and an in-memory OAuth
  token provider, with credential persistence remaining caller-owned.
- Anthropic Messages now sends the Claude Code identity required by Anthropic
  OAuth tokens: identity beta headers, a leading Claude Code system block, and
  canonical Claude Code tool-name casing with streamed tool names restored to
  the caller's original casing.

## [0.4.0] - 2026-06-08

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
- Provider replay now drops abandoned local assistant tool-call blocks when a
  new user or developer turn arrives before the corresponding tool result,
  while preserving answered tool-call history and hosted provider tool
  metadata.
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
- Anthropic Messages streaming now preserves hosted server-tool metadata,
  citation deltas, context-management metadata, container metadata, and
  thinking-token usage details for provider-neutral replay and diagnostics.
- Google Gemini API and Vertex AI streaming now preserve grounding metadata and
  normalized source entries from grounded responses.
- Bedrock Converse Stream now synthesizes placeholder tool specs from replayed
  assistant/tool history when the current request has no active tools, avoiding
  provider rejection of otherwise valid tool-use history.
- Anthropic Messages, Google Gemini API/Vertex AI, Mistral Conversations, and
  Bedrock Converse request builders now strip invalid UTF-8 from replayed text
  before provider JSON encoding, matching the existing OpenAI-compatible text
  cleanup behavior.

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
- Broader Fireworks catalog expansion remains deferred beyond the built-in Fire
  Pass route and the verified Kimi K2.6 and Kimi K2.7 Code rows.
- Live Google Gemini API and Vertex AI validation remains deferred; deterministic
  fixtures are the release evidence for the Google preview adapters.
- The Go package targets server/CLI use; browser-specific behavior is out of
  scope for this release.
- Agent runtime orchestration and cross-provider context handoff with
  capability-loss reporting are deferred to later integration work; this release
  exposes only provider-neutral primitives.
- DeepSeek, Groq, Cerebras, Together, GitHub Copilot, Cloudflare, NVIDIA, Ant
  Ling, Moonshot AI, MiniMax, and Kimi are not yet first-class provider rows;
  generated metadata and routing may exist, but independent provider-quality
  claims still need fixtures.
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
