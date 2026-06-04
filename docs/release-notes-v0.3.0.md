# Release notes: sigma v0.3.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.3.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

v0.3.0 refreshes Sigma's generated model catalog with curated current metadata
for supported provider IDs, including broader OpenAI, Anthropic, Google, Vertex
AI, Mistral, Bedrock, OpenCode, and metadata-only OpenAI-compatible rows. It
also adds focused metadata-only rows for adapter-backed provider families such
as Azure OpenAI Responses, OpenAI Codex Responses, Cloudflare AI Gateway,
Cloudflare Workers AI, NVIDIA NIM, Z.ai, Ant Ling, Moonshot AI, MiniMax, Vercel
AI Gateway, and expanded GitHub Copilot routes. The release extends generated
image metadata with OpenRouter-routed Grok and Gemini image routes, tightens
the OpenAI-compatible preview adapters around prompt caching, replay, stream
  parsing, provider-specific reasoning formats, Codex OAuth, and Codex WebSocket
  session reuse, adds a provider-neutral embeddings surface for OpenAI text
  embedding models plus typed embedding telemetry and custom OpenAI-compatible
  embedding metadata, hardens resilient embedding batches with limits, cache
  hooks, safer splitting, and trace metadata, adds query/document embedding
  intent plus deterministic vector scoring helpers, and adds typed provider
  error classification for safer caller retry and recovery decisions. The Google
  preview adapters now include the scoped provider hardening for Vertex
  credential fallback, model-scoped routing metadata, and replayed tool-call IDs.
  Direct xAI/Grok support remains focused on the preview Chat Completions adapter.

## Added

- Generated image model metadata for `x-ai/grok-imagine-image-quality` through
  the existing OpenRouter image-generation adapter, including OpenRouter
  credential discovery and xAI routed-provider metadata.
- Curated generated text metadata for current OpenAI, Anthropic, Google,
  Vertex AI, Mistral, Bedrock, OpenCode, and OpenAI-compatible model rows while
  keeping default registry entries metadata-only until providers are registered.
- Focused metadata-only text rows for Azure OpenAI Responses, OpenAI Codex
  Responses, Cloudflare AI Gateway, Cloudflare Workers AI, NVIDIA NIM, Z.ai,
  Ant Ling, Moonshot AI, MiniMax, Vercel AI Gateway, and expanded GitHub
  Copilot routes where existing Sigma adapters can express the API surface.
- OpenAI-compatible Chat Completions reasoning metadata supports Together,
  Qwen, Z.ai, and Ant Ling payload formats, including Z.ai `tool_stream` for
  tool-enabled requests.
- OpenCode Zen and OpenCode Go metadata now includes the promoted DeepSeek V4
  Flash and MiniMax M3 routed rows, stricter unsupported thinking-level
  metadata for known reasoning models, adaptive Anthropic thinking metadata for
  selected Claude routes, and temperature suppression for OpenCode Claude
  models that reject temperature.
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
- Vector embeddings now have a provider-neutral API with embedding model
  discovery, request-scoped embedding options, redacted embedding debug hooks,
  OpenAI `/v1/embeddings` support, and generated metadata for
  `text-embedding-3-small` and `text-embedding-3-large`.
- Embedding responses now include typed SDK-level attempt metadata for provider,
  API, model, retry attempt, status code, request ID, and per-attempt latency.
- `sigma.OpenAICompatibleEmbeddingModel` now constructs metadata for
  caller-registered OpenAI-compatible embedding endpoints, including base URL,
  headers, dimensions, token limits, and input-token pricing.
- Embedding model metadata now records supported dimension ranges alongside
  default dimensions, max input tokens, and cost metadata for routing.
- `Client.EmbedBatch` adds resilient embedding batch execution with duplicate
  input reuse, retry-aware batch splitting, optional oversized-input splitting,
  progress callbacks, and aggregate status/request/usage/cost summaries.
- Embedding model metadata now records known batch input and UTF-8 byte limits,
  and built-in OpenAI embedding rows include the documented input-array cap.
- `Client.EmbedBatch` now applies model/request batch limits before provider
  dispatch, supports cross-call embedding caches keyed by SHA-256 input hashes,
  and keeps token-budget estimation caller-owned.
- Oversized embedding recovery now uses safer newline, whitespace, and
  UTF-8-safe rune split boundaries, and local request-too-large/tokenizer EOF
  failures classify as split-recoverable without becoming same-request retries.
- `EmbeddingBatchSummary` now includes structured trace events for cache
  lookups/stores, planned limit splits, provider attempts, errors, and
  oversized-input splits without raw input text.
- Embedding requests now support provider-neutral query/document intent through
  `EmbeddingInputType`, `EmbeddingQuery`, and `EmbeddingDocuments`, while
  keeping OpenAI `/v1/embeddings` payloads unchanged.
- `NormalizeEmbeddingNewlines` gives callers explicit newline normalization
  without silently changing embedding inputs in `Client.Embed` or
  `Client.EmbedBatch`.
- Deterministic embedding vector utilities now cover dot product, cosine
  similarity, vector normalization, weighted vector combination, and
  cosine-based ranking with typed numeric error sentinels.

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
- Newly added provider-family rows are metadata-only until callers register the
  matching existing adapter or a custom provider for that `ProviderID`.

## Deferred work

- Broader Google Gemini API and Vertex AI catalog coverage remains deferred to
  the catalog refresh workflow with deterministic modeldata, payload, error,
  and compatibility coverage.
- Broader OpenCode catalog expansion and live OpenCode surface validation
  remain deferred to the catalog refresh workflow and opt-in credential-gated
  probes.
- Broad OpenRouter text expansion, Bedrock regional aliases, and automated
  catalog ingestion remain deferred to the catalog refresh workflow.
- Anthropic-compatible Fireworks model routing remains deferred; the built-in
  Fireworks row stays on the OpenAI-compatible Fire Pass route.
- Mistral image input remains deferred until the Conversations request shape is
  covered by deterministic payload fixtures.
- Embedding vector stores, general text chunking, tokenizer-based input
  estimates, provider-selection fallback, non-OpenAI embedding adapters, and
  live embedding probes remain deferred.
- Direct xAI/Grok image-provider semantics remain deferred until the request
  and response shape is covered by deterministic fixtures.
- Live OpenAI image validation remains deferred to opt-in probes; deterministic
  fixtures are the release evidence for image generation, multipart edits,
  reference-only JSON edits, variations, streaming, and Responses
  image-generation tool output.
- Proxy-aware Codex WebSocket dialing, token persistence, broad OpenRouter text
  expansion, automated catalog ingestion, and first-class promotion for the
  new metadata-only provider families remain deferred until they have
  deterministic fixtures.
- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
No live xAI or OpenRouter provider calls are required for release validation.
OpenAI provider changes, image generation/edit/variation/streaming behavior,
Codex OAuth and WebSocket flows, typed provider error classification, and
  generated catalog metadata, including strict OpenCode thinking and routed model
  metadata plus the focused provider-family registry refresh and OpenAI
  embedding support, typed embedding telemetry, custom OpenAI-compatible
  embedding metadata, embedding capability metadata, and resilient embedding
  batch hardening plus query/document intent, newline normalization, and vector
  scoring helpers are covered by deterministic request, response, OAuth,
  SSE/WebSocket, checksum, payload, registry, cache, split, trace, and numeric
  fixtures.
