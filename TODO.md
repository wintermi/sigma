# TODO

Deferred work that is outside the current release scope. None of these are
current release gates. Each item should land with the same evidence bar as MVP
features — deterministic fixtures, golden payloads, or fake clients, plus
cancellation/error coverage (see the coverage standards in
[RELEASING.md](RELEASING.md)) — before it can be promoted out of "future" status.

## Credential ergonomics

- [x] Add model-aware environment credential discovery helpers that expose
      candidate and configured environment variable names without returning
      secret values.
- [x] Add provider-specific request configuration helpers for Cloudflare AI
      Gateway account/gateway placeholders and Bedrock request region/static
      credential values without adding a generic environment override surface.
- [x] Let stored Cloudflare API-key credentials fill missing account and
      gateway routing values from the matching environment variables while
      preserving stored and request-scoped precedence.
- [x] Add request-scoped final header suppression across text, image, and
      embedding calls without adding generic environment overrides or changing
      credential resolution.
- [ ] Keep ambient cloud credential probing and OAuth token persistence deferred
      unless they get separate public API contracts.

## Request budgeting

- [x] Add opt-in context-aware max-output-token budgeting helpers that combine
      model context/output metadata with deterministic request estimates without
      changing dispatch defaults.
- [x] Add opt-in reasoning budget planning helpers that combine visible output
      caps, model/context limits, deterministic request estimates, and hidden
      thinking budgets without changing dispatch defaults.
- [ ] Keep tokenizer-exact context budgeting deferred unless Sigma adopts a
      provider tokenizer dependency or caller-supplied tokenizer contract.
- [x] Add opt-in dispatch-time output-token clamping with explicit client
      default/request override precedence, using existing deterministic request
      estimates and model context/output metadata without changing dispatch
      defaults.

## Core streaming

- [x] Harden core text and image stream cancellation so collectors preserve
      aborted partial finals and abandoned canceled streams close promptly.
- [x] Harden runtime stream/replay edges with tolerant SSE line parsing,
      deterministic fake-provider lifecycle synthesis, live partial-message
      snapshots, best-effort decoded partial tool-call argument metadata, safe
      JSON/tool-argument round trips, strict null coercion, image request
      validation, explicit handoff output coordinates, cache-hit usage
      fallbacks, local/custom streaming-usage defaults, request-scoped provider
      auth precedence, and cache-cost catalog checks.
- [x] Classify premature OpenAI Responses and Anthropic Messages terminal-event
      gaps as transient and retryable while preserving partial finals and
      keeping post-body retry execution caller-owned.

## Image generation

- [ ] Decide whether image generation graduates from preview into the MVP
      boundary and update release docs accordingly.
- [x] Add focused Google Gemini API and Vertex AI image generation adapters for
      Imagen/Gemini image output through the provider-neutral image surface.
- [x] Add provider-scoped runtime image model sources and refresh helpers so
      applications can update app-owned dynamic image model listings without
      replacing Sigma's curated offline catalog.
- [x] Add live OpenAI image validation probes for generation, edits,
      reference-only JSON edits, variations, streaming partial images, and
      Responses image-generation tool output without making live provider calls
      part of `mise run ci`.
- [ ] Add opt-in live Google Gemini API and Vertex AI image probes without
      making live provider calls part of `mise run ci`.

## Embeddings

Sigma now has a first-class provider-neutral embeddings surface with OpenAI,
Google Gemini API, Google Vertex AI, and Amazon Bedrock adapter coverage plus
representative generated metadata. Resilient batch hardening is available
through `Client.EmbedBatch`; small in-memory retrieval primitives are available
for local document/chunk search, while external stores and tokenizer-aware
workflows remain future work.

- [x] Add resilient embedding batch helpers for duplicate input reuse,
      retry-aware batch splitting, optional oversized-input splitting, progress
      callbacks, and aggregate usage/cost summaries.
- [x] Add typed embedding attempt telemetry with request IDs, status codes,
      provider/API/model identity, retry attempts, and per-attempt latency.
- [x] Add generic batch telemetry for total request attempts, status buckets,
      request IDs, attempts, usage, and cost.
- [x] Add model/request-level embedding batch limits for input counts and UTF-8
      byte budgets while keeping token-budget estimation caller-owned.
- [x] Add cross-call embedding cache hooks with SHA-256 input keys and
      deterministic fake-cache coverage.
- [x] Add split-recoverable classification for embedding request-too-large and
      local tokenizer EOF failures.
- [x] Add safer oversized embedding split boundaries and structured batch trace
      events for caller aggregation.
- [x] Add OpenAI-compatible embedding model construction for caller-registered
      local or private embedding endpoints.
- [x] Add an explicit local OpenAI-compatible embedding registration helper
      with Ollama-friendly defaults and normalized `/v1` base URLs.
- [x] Add explicit embedding dimension-range metadata for discovery and routing.
- [x] Add provider-scoped runtime embedding model sources and refresh helpers so
      applications can update app-owned dynamic embedding model listings
      without replacing Sigma's curated offline catalog.
- [x] Add retrieval document/chunk types, deterministic character-based
      splitting, and in-memory cosine search routed through `Client.EmbedBatch`.
- [x] Add a compact query/document embedder helper around the provider-neutral
      embedding API without adding another provider adapter.
- [ ] Add external vector-store integration only as an explicit new surface,
      not as part of provider dispatch.
- [ ] Add tokenizer-aware or format-specific text chunking without making
      provider tokenizers a hidden runtime dependency.
- [x] Add similarity/ranking helpers with deterministic numeric fixtures before
      documenting them as reusable retrieval primitives.
- [ ] Add tokenizer-based embedding input estimates without making provider
      tokenizers a hidden runtime dependency.
- [ ] Add provider-selection fallback only after settling provider/model
      precedence semantics for embeddings as a separate public API decision.
- [x] Add focused non-OpenAI embedding adapters for Google Gemini API, Google
      Vertex AI, and Amazon Bedrock with deterministic request/response, usage,
      error, and cancellation-path coverage.
- [ ] Add opt-in live embedding probes without making live provider calls part
      of `mise run ci`.

## OpenAI-compatible parity

OpenAI Chat Completions, Responses, and Codex Responses now cover the focused
v0.3 prompt-cache, replay, stream-shape, cached-token accounting, and Codex
WebSocket gaps with deterministic fixtures. Broader provider-specific
integrations remain future work until they have the same local evidence bar.

- [x] Add cache-affinity headers for OpenAI-compatible Chat Completions and
      direct OpenAI Responses when prompt caching and `sigma.WithSessionID` are
      enabled.
- [x] Clamp Codex request-affinity headers to the provider's 64-character
      session-ID limit while preserving Sigma's local session-cache keys.
- [x] Use monotonic UUIDv7 handshake IDs for sessionless Codex WebSocket
      requests and keep the built-in GPT-5.6 Codex context limits at 272K.
- [x] Send OpenRouter cache affinity through `x-session-id`, preserving caller
      header overrides and avoiding generic OpenAI session-affinity headers.
- [x] Keep OpenAI and Azure OpenAI Responses `previous_response_id` payloads
      explicit so cache-affinity `sigma.WithSessionID` values are not sent as
      provider continuation IDs.
- [x] Add OpenAI Responses/Codex service-tier cost accounting for `flex` and
      `priority` request tiers.
- [x] Harden OpenAI Responses replay when same-provider history crosses model
      IDs with prior function-call item IDs.
- [x] Harden OpenAI Responses stream terminal handling so premature EOF is an
      error and terminal incomplete responses preserve usage as max-token stops.
- [x] Harden OpenAI Responses completion and replay so final reasoning content
      and multi-part summaries are preserved, started blocks end when their
      output item completes, and empty tool results remain non-empty.
- [x] Harden OpenAI Responses reasoning defaults and OpenAI-compatible Chat
      Completions replay for empty assistant turns and opt-in tool-history
      payload requirements.
- [x] Add deterministic OpenAI-compatible Chat Completions replay coverage for
      prior private thinking blocks on routes that do not require native
      `reasoning_content`.
- [x] Add Codex WebSocket transport, session caching, cleanup helpers, and SSE
      fallback while keeping the implementation stdlib-only.
- [x] Add proxy-aware Codex WebSocket dialing for standard HTTP(S) proxy
      environment variables and `NO_PROXY` exclusions while preserving SSE
      fallback.
- [x] Add Codex WebSocket-specific connect timeout and session-cache debug
      stats while keeping the preview transport on request contexts, explicit
      session cleanup helpers, and SSE fallback.
- [x] Retry a pre-output Codex WebSocket connection-limit response exactly once
      before retaining the existing SSE fallback for repeated or other failures.
- [x] Add provider-neutral session resource cleanup so callers can release
      cached Codex WebSocket sessions through root `sigma` helpers while keeping
      provider-specific cleanup helpers available.
- [x] Add OpenAI Codex device-code OAuth login and refresh helpers while keeping
      token persistence caller-owned.
- [x] Add OpenAI Codex browser callback OAuth login while keeping token
      persistence caller-owned.
- [x] Add an OpenAI Codex credential-store bridge so browser and device-code
      login results can be written to caller-supplied `CredentialStore`
      implementations without choosing a concrete storage backend.
- [ ] Keep file-backed, encrypted, OS keychain, or UI-driven OpenAI Codex token
      persistence deferred to caller-owned integrations.
- [x] Evaluate GitHub Copilot dynamic headers before making Copilot a
      first-class OpenAI-compatible row.
- [x] Evaluate Cloudflare AI Gateway auth header rewriting before adding a
      first-class Cloudflare OpenAI-compatible row.
- [x] Promote GitHub Copilot to first-class compatible provider wrappers for
      Chat Completions, Responses, and Anthropic Messages.
- [x] Route GitHub Copilot `mai-code-1-flash-picker` through its supported
      Responses endpoint in generated model metadata.
- [x] Promote Cloudflare AI Gateway to first-class compatible provider wrappers
      for OpenAI-compatible and Anthropic-compatible text routes.
- [x] Send Z.ai reasoning requests as `thinking` objects with enabled or
      disabled types instead of the legacy `enable_thinking` toggle.
- [x] Add GLM-5.2 Z.ai reasoning-effort metadata and surface
      OpenAI-compatible provider finish reasons that indicate upstream errors.
- [x] Harden OpenAI-compatible Chat Completions streams to avoid duplicate
      reasoning alias deltas and reject successful EOF before a terminal
      `finish_reason`.
- [x] Send explicit `max_tokens` for OpenCode Zen and OpenCode Go Chat
      Completions through compatibility detection.
- [x] Add opt-in structured-output capability probes for OpenAI-compatible
      routes so JSON object and strict JSON Schema support can be refreshed
      from live evidence without making provider calls part of `mise run ci`.

## Vertex MaaS

Sigma now has focused non-Gemini Vertex support for OpenAI-compatible MaaS
routes and Anthropic Claude `streamRawPredict` routes, using explicit
project/location routing and caller-supplied API-key or OAuth token credentials.
Broader MaaS provider coverage remains future work until each route has
deterministic request, stream, error, and metadata evidence.

- [x] Add first-class Vertex OpenAI-compatible MaaS and Vertex Anthropic
      provider registrations with shared project/location, API-version,
      endpoint override, API-key, OAuth token, and placeholder credential
      behavior.
- [x] Add representative generated metadata for Vertex OpenAI-compatible Llama
      MaaS and Anthropic Claude routes without broad catalog expansion.
- [ ] Add Mistral-on-Vertex `rawPredict`/`streamRawPredict` support only after
      settling its Chat Completions-shaped request and response fixtures.
- [ ] Add broader Vertex MaaS catalog refresh support through the existing
      reviewable catalog workflow rather than ad hoc metadata imports.
- [ ] Add opt-in live Vertex MaaS probes for OpenAI-compatible, Anthropic, and
      future Mistral routes without making live provider calls part of
      `mise run ci`.

## Provider replay and metadata hardening

Sigma preserves provider-native replay and diagnostic metadata only when it can
do so through existing provider-neutral shapes or opaque provider metadata.
Public content-type additions and first-class provider promotions remain future
work until their API boundaries are explicit.

- [x] Preserve Anthropic hosted server-tool metadata, citations,
      context-management metadata, container metadata, and thinking-token usage
      details through deterministic stream parsing.
- [x] Preserve Google grounding metadata and normalized source entries from
      Gemini API and Vertex AI grounded responses.
- [x] Synthesize Bedrock Converse tool specs from replayed assistant/tool
      history when the current request has no active tools.
- [x] Add typed Bedrock structured-output requests through a synthetic schema
      tool that is hidden from callers while preserving real tool calls.
- [x] Drop abandoned local assistant tool-call blocks during provider replay
      when a new user/developer turn arrives before the matching tool result,
      while preserving answered calls and hosted provider tool metadata.
- [x] Normalize invalid UTF-8 text before non-OpenAI provider JSON encoding for
      Anthropic, Google/Vertex, Mistral, and Bedrock request builders.
- [x] Broaden provider context-overflow classification and add a final-message
      helper for diagnostic and caller-supplied context-window usage signals.
- [x] Preserve long prompt-cache write usage separately for cost accounting
      while keeping total cache-write tokens unchanged.
- [x] Bound Anthropic Messages prompt-cache breakpoints to API-valid placement
      so cache-enabled agent loops do not mark every user turn, tool result, or
      tool definition.
- [x] Normalize text-generation usage/accounting metadata with provider/model
      identity, raw provider usage payloads, tool-use input tokens, terminal
      stream usage, and separate provider-reported versus Sigma-estimated cost.
- [x] Add strict local tool-call validation for composed JSON Schema branches
      using `anyOf`, `oneOf`, and `allOf` without adding implicit argument
      coercion.
- [x] Add strict local tool-call validation for string `pattern` constraints
      and `not` schemas without adding implicit argument coercion.
- [x] Add opt-in primitive argument coercion for local tool-call validation
      while keeping strict `ValidateToolCall` behavior as the default.
- [x] Harden registry model metadata copies and opt-in union coercion so nested
      provider metadata remains caller-owned and already-valid `anyOf` /
      `oneOf` values are not rewritten.
- [x] Add provider-neutral document/PDF request content blocks with base64,
      URL, and provider file-ID sources plus initial OpenAI and Anthropic
      payload compatibility.
- [x] Add typed Anthropic native `output_format` and parallel-tool suppression
      controls without adding provider-neutral structured-output semantics.
- [x] Add provider-neutral source/citation result APIs over existing
      OpenAI-compatible, Anthropic, and Google metadata while keeping
      provider-specific rendering and advanced document/PDF citation policy
      deferred.
- [x] Add a provider-neutral text response ID accessor over existing
      provider response metadata without changing serialized assistant message
      shape.
- [x] Add a provider-neutral routed response model accessor over existing
      provider response metadata without changing serialized assistant message
      shape.
- [x] Preserve OpenAI-compatible Chat Completions `reasoning_details` metadata
      on tool-call blocks for replay while keeping broader provider-neutral
      reasoning-detail rendering deferred.
- [x] Harden provider replay and protocol edge cases across Anthropic, Google,
      Bedrock, OpenAI-compatible, Azure Responses, and GitHub Copilot OAuth
      with deterministic fixtures for signatures, private reasoning metadata,
      empty replay blocks, endpoint normalization, and credential precedence.
- [x] Add deterministic replay regression coverage for Responses partial tool
      arguments, Google tool-call signature filtering, and Bedrock malformed or
      unsupported replay content.
- [x] Add metadata-gated deferred client-tool loading for Anthropic Messages
      tool references and OpenAI/Codex Responses client tool-search replay.
- [ ] Add broader provider-neutral sampling controls such as top-p, top-k,
      seed, and penalty fields only after settling cross-provider semantics.
- [x] Add opt-in live provider metadata/replay and pairwise handoff probes
      without making live provider calls part of `mise run ci`.
- [x] Add deterministic request-conversion regression coverage for replay IDs,
      OpenAI-compatible request-shape guardrails, routed stream model metadata,
      and Google legacy tool-schema sanitization.
- [x] Add strict local JSON Pointer `$ref`, selected standard format, and
      `if`/`then`/`else` validation without adding dependencies or default
      primitive coercion.
- [ ] Keep external schema references, unsupported JSON Schema vocabulary, and
      default primitive coercion deferred unless Sigma adds a broader validation
      contract.

## First-class provider rows

Provider IDs and compatibility routing may already exist, but each needs
independent provider-quality claims backed by fixtures before becoming a
first-class row in [provider parity](docs/provider-parity.md). Representative
generated metadata can exist before first-class promotion when it is
metadata-only and backed by compatibility checks.

- [ ] Keep provider packages that do not yet have Sigma-native adapters behind
      first-class row promotion, with deterministic request, stream, error,
      redaction, and cancellation coverage before registration.
- [x] DeepSeek — promote to a first-class provider row with fixtures.
- [x] Groq — promote to a first-class provider row with fixtures.
- [x] Cerebras — promote to a first-class provider row with fixtures.
- [x] Together — promote to a first-class provider row with fixtures.
- [x] Hugging Face Router — promote to a first-class provider row with
      fixtures and focused generated metadata.
- [ ] Hugging Face Router — add broader live-provider and hosted-tool coverage
      only if route-specific behavior needs evidence beyond the shared
      OpenAI-compatible adapter.
- [x] OpenRouter — promote to a first-class OpenAI-compatible text provider row
      with fixtures over the existing focused generated metadata and routing
      compatibility.
- [ ] OpenRouter — add broader live-provider and hosted-tool coverage only if
      route-specific behavior needs evidence beyond the shared
      OpenAI-compatible adapter.
- [ ] DeepSeek, Groq, Cerebras, and Together — add broader live-provider and
      hosted-tool coverage only if route-specific behavior needs evidence
      beyond the shared OpenAI-compatible adapter.
- [x] GitHub Copilot — promote to a first-class provider row with fixtures.
- [x] Cloudflare AI Gateway — promote to a first-class provider row with
      fixtures.
- [x] Cloudflare Workers AI — promote to a first-class provider row with
      fixtures.
- [x] Cloudflare Workers AI — add focused Kimi K2.7 Code and GLM 5.2 generated
      metadata with deterministic registry coverage, preserving session
      affinity through the existing adapter.
- [x] Azure OpenAI Responses — promote to a first-class provider row with
      provider-scoped registration and request-option helpers.
- [x] NVIDIA NIM — promote to a first-class provider row with fixtures.
- [x] NVIDIA NIM — add an opt-in live surface-probe route for streaming, tools,
      usage, and request-shape diagnostics without making live calls part of
      CI.
- [x] NVIDIA NIM — add focused generated text metadata for `openai/gpt-oss-120b`
      and `nvidia/nemotron-3-ultra-550b-a55b`.
- [x] NVIDIA NIM — add opt-in live `/models` validation for reviewing direct NIM
      catalog rows without making generation or CI network-dependent.
- [ ] NVIDIA NIM — expand broader live-provider fixture coverage for redaction,
      cancellation, embedding models, and route behavior only if the provider
      needs behavior beyond the shared OpenAI-compatible adapters.
- [x] Z.ai and Z.ai Coding CN — promote to first-class provider rows with
      fixtures.
- [ ] Z.ai and Z.ai Coding CN — add broader live-provider fixture coverage for
      streaming, tools, usage, redaction, and cancellation if the providers need
      behavior beyond the shared OpenAI-compatible adapter.
- [x] Ant Ling — promote to a first-class provider row with fixtures.
- [x] Moonshot AI and Moonshot AI CN — promote to first-class provider rows
      with fixtures.
- [ ] Moonshot AI and Moonshot AI CN — add broader live-provider fixture
      coverage for streaming, tools, usage, redaction, and cancellation if the
      providers need behavior beyond the shared OpenAI-compatible adapter.
- [x] MiniMax and MiniMax CN — promote to first-class Anthropic-compatible
      provider rows with deterministic registration and endpoint-path coverage.
- [ ] MiniMax and MiniMax CN — add broader live-provider fixture coverage for
      streaming, tools, usage, redaction, and cancellation if the providers need
      behavior beyond the shared Anthropic-compatible adapter.
- [x] Vercel AI Gateway — promote to a first-class provider row with fixtures.
- [x] Kimi — promote to a first-class provider row with fixtures.
- [x] Xiaomi — promote to a first-class provider row with fixtures.
- [x] Radius gateway — add an API-key-authenticated dynamic text provider with
      gateway catalog refresh, native streaming, replay, typed errors, and
      cancellation fixtures while keeping the default registry catalog-free.
- [ ] Radius gateway — keep OAuth and durable model-catalog persistence
      deferred until they have separate public contracts.
- [ ] For each promoted provider, add streaming, tools, usage, typed error
      classification, redaction, and cancellation coverage.

## xAI/Grok parity

Sigma now has a first-class preview xAI/Grok provider over the shared
OpenAI-compatible Chat Completions adapter, plus Grok 4.5 over the shared
OpenAI Responses adapter. Curated generated metadata covers the direct xAI
routes. Future additions should still be promoted only with deterministic
request-shape evidence.

- [x] Route Grok 4.5 through OpenAI Responses with bounded reasoning-level and
      prompt-cache-retention compatibility metadata.
- [ ] Keep future xAI/Grok generated metadata refreshes tied to deterministic
      modeldata, payload, error, and compatibility coverage.
- [x] Add OpenRouter-routed Grok Imagine image metadata without introducing a
      direct xAI image provider.
- [x] Add opt-in live xAI/Grok surface probes only as diagnostics, keeping live
      provider calls out of `mise run ci`.
- [ ] Evaluate direct xAI/Grok image-provider semantics only after the API
      shape is backed by deterministic request and response fixtures.
- [x] Add caller-configured xAI device-code OAuth login, token refresh,
      in-memory credential resolution, and provider-auth descriptors while
      leaving OAuth client registration and durable token persistence
      caller-owned.

## Model registry generation

Sigma's built-in model registry is generated from a curated checked-in catalog.
That keeps release output reviewable, but refreshes are manual and the default
catalog is intentionally smaller than the provider/source metadata available
upstream.

- [x] Refresh the curated v0.3 generated catalog with current rows for
      supported provider IDs while preserving Sigma runtime contracts and
      metadata-only default registration.
- [x] Add focused metadata-only provider-family rows for existing adapter
      paths, including Azure OpenAI Responses, OpenAI Codex Responses,
      Cloudflare AI Gateway, Cloudflare Workers AI, NVIDIA NIM, Z.ai, Ant Ling,
      Moonshot AI, MiniMax, Vercel AI Gateway, and expanded GitHub Copilot
      routes.
- [x] Add focused Chat Completions reasoning-format support for generated
      Together, Qwen, Z.ai, and Ant Ling metadata, including Z.ai `tool_stream`
      payloads.
- [x] Refresh the focused OpenRouter image catalog with the missing routed MAI
      Image 2.5 and Riverflow 2.5 rows while keeping broad OpenRouter text
      expansion deferred.
- [x] Add focused OpenRouter image metadata for current Gemini and GPT Image
      routes while keeping broad OpenRouter text expansion deferred.
- [x] Add a curated OpenRouter text-model cohort for Claude Sonnet 5, GPT-5.2
      Codex, Gemini 3.5 Flash, and DeepSeek V4 Pro with route-specific
      compatibility, reasoning, pricing, and capability metadata while keeping
      broad text expansion deferred.
- [x] Add curated OpenRouter GPT-5.6 Luna, Sol, and Terra route metadata with
      reviewed reasoning, text/image, tool, limit, and cache-pricing coverage
      while keeping broad text expansion deferred.
- [x] Add the missing direct Anthropic Claude Fable 5 row with adaptive
      thinking metadata, xhigh thinking-level mapping, image input support,
      current limits, and pricing.
- [x] Add a deterministic local catalog summary report for the generator,
      covering source count, text/image/embedding totals, text tool/reasoning
      counts, and provider/API buckets without changing the checked-in catalog.
- [x] Add validated request-wide model cost tiers for high-context pricing,
      including generated metadata and deterministic input/cache accounting.
- [x] Add focused direct OpenAI GPT-5.6 Luna, Sol, and Terra metadata with
      reasoning, cache-write pricing, and validated high-context cost tiers.
- [x] Add focused GPT-5.6 Luna, Sol, and Terra metadata to existing Azure OpenAI
      Responses and OpenAI Codex Responses routes, including route-specific
      limits, reasoning mappings, pricing, and deterministic metadata coverage.
- [x] Add focused direct-provider metadata for Cerebras Gemma 4 31B, xAI Grok
      4.5, and NVIDIA NIM MiniMax M3 and GLM 5.2 while keeping broad router
      catalog expansion deferred to the reviewed refresh workflow.
- [x] Add curated non-regional Bedrock Converse Stream metadata for Gemma 3,
      Llama 3.1/3.3/4, Nemotron 3, GPT-5.4/5.5, Palmyra X4/X5, and Grok 4.3,
      with deterministic registry coverage.
- [x] Correct the Azure GPT-5.4/GPT-5.5 context windows to the 1,050,000-token
      Azure Foundry deployments and the OpenAI/Azure GPT-5 Pro max output
      tokens to 128,000.
- [x] Add the DeepSeek-style thinking format to direct Moonshot AI and
      Moonshot AI CN generated rows, streaming-usage metadata, and provider or
      host compatibility detection so thinking-off requests explicitly disable
      reasoning and streamed usage can be requested.
- [x] Add the direct Moonshot AI Kimi K2.7 Code row with text/image input,
      reasoning, tools, current limits, pricing, and `MOONSHOT_API_KEY`
      discovery.
- [x] Add direct Moonshot AI CN Kimi K2.7 Code plus direct Moonshot AI and
      Moonshot AI CN Kimi K2.7 Code HighSpeed rows, including
      disabled-thinking compatibility metadata.
- [x] Add OpenCode Go Kimi K2.6 and Kimi K2.7 Code metadata with
      `reasoning_effort` requests so thinking-off/default requests omit
      rejected disabled `thinking` objects.
- [x] Add focused OpenCode Go GLM-5.2 and Qwen3.7 Plus metadata with
      deterministic Chat Completions and Messages route coverage.
- [x] Add disabled-thinking compatibility metadata to the generated Claude
      Fable 5 row so thinking-off requests omit the rejected disabled payload.
- [x] Add an offline-friendly refresh command that ingests explicit
      `models.dev` snapshots into a validated candidate catalog without
      replacing the checked-in review step or generated files.
- [ ] Keep explicit source precedence and override rules near the generator:
      `models.dev` for broad text model discovery, provider APIs for surfaces
      not covered there, and hand-curated overrides for known endpoint behavior
      mismatches.
- [ ] Add provider-catalog overlay inputs only after each source has explicit
      precedence, reviewable diffs, and deterministic fixture coverage.
- [x] Extend the local summary into a candidate catalog diff report that lists
      added, removed, changed, and unchanged text, image, and embedding rows so
      catalog review is tractable before generated files are written.
- [ ] Expand OpenRouter text metadata beyond the curated route cohort only
      through the catalog refresh workflow, with deterministic diffs and
      reviewable routing/cost changes instead of ad hoc catalog imports.
- [x] Add focused current Bedrock Claude regional inference-profile and direct
      GPT-OSS, DeepSeek R1, and Llama 4 metadata with deterministic registry
      assertions, while keeping broad catalog refresh work deferred.
- [ ] Keep Bedrock model families without a documented provider-neutral
      Converse reasoning-control mapping conservative, even when they emit
      reasoning content.
- [ ] Evaluate Anthropic-routed aliases through Cloudflare AI Gateway,
      OpenRouter, Vercel AI Gateway, OpenCode, and Bedrock only after route
      shape, auth, compatibility metadata, pricing, and regional availability
      are reviewed per provider family.
- [x] Add focused current Claude Sonnet 5 and Fable 5 metadata across existing
      Anthropic-compatible routes with deterministic modeldata assertions,
      while keeping broad catalog parity deferred to the reviewed refresh
      workflow.
- [x] Add GitHub Copilot Claude Fable 5 Chat Completions metadata with
      deterministic registry and request-shape coverage.
- [x] Add GitHub Copilot Kimi K2.7 Code and MAI-Code-1-Flash Chat Completions
      metadata with deterministic registry coverage while keeping the existing
      adapter and request shape unchanged.
- [x] Promote a focused Kimi Coding Anthropic-compatible provider slice with a
      distinct provider ID, credential env var, compatibility metadata, and
      deterministic wrapper coverage.
- [x] Refresh Kimi Coding metadata with K3 and HighSpeed routes, reviewed
      limits and estimated costs, adaptive-thinking controls, and empty-signature
      replay compatibility while preserving the existing adapter.
- [x] Add focused Hugging Face Router metadata after settling provider ID,
      credential env var, compatibility metadata, and first-class provider-row
      requirements.
- [ ] Expand broad Hugging Face Router metadata only through the catalog refresh
      workflow, with deterministic diffs and reviewable routing/cost changes.
- [ ] Expand broad Vercel AI Gateway and OpenRouter text catalogs only after
      source-aware provider/API mapping, tool-capable filtering, reasoning
      metadata, and cost/routing diff reports are in place.
- [ ] Preserve Sigma's current generated-output contract: deterministic order,
      validated `internal/modeldata/catalog.json`, generated Go code, checksum
      tests, and metadata-only default registry entries.
- [ ] Add source-aware tests for provider/API mapping, tool-capable filtering,
      reasoning metadata, thinking-level maps, provider headers, auth env vars,
      and compatibility metadata.
- [ ] Expand generated metadata only after matching provider adapters or
      compatibility routes have deterministic payload/error coverage.

## OpenCode parity

Sigma now has a routed OpenCode Zen/Go preview provider for selected model
families that need Google Generative AI, Anthropic Messages, OpenAI Responses,
or OpenAI-compatible Chat Completions routes. Broader catalog coverage should
still be promoted only with deterministic evidence.

- [x] Complete the reviewed OpenCode Zen catalog cohort for GPT-5.6 Luna/Sol/
      Terra, DeepSeek V4 Pro, GLM-5.2, Grok 4.5, Hy3 Free, Kimi K2.7 Code,
      MiniMax-M3, Nemotron 3 Ultra Free, and North Mini Code Free, including
      cached Responses request-ID affinity compatibility.
- [x] Add OpenCode Go Grok 4.5 OpenAI Responses and Kimi K3 Chat Completions
      metadata with deterministic generated-registry and request-shape coverage.
- [ ] Continue reviewing later OpenCode Zen and OpenCode Go catalog changes,
      prioritizing high-value routed families and avoiding advertised-but-
      unavailable models.
- [x] Evaluate and fixture selected OpenCode-routed OpenAI Responses,
      Anthropic Messages, and Google API models before promoting them to
      built-in metadata.
- [x] Promote strict OpenCode Zen/Go metadata for the covered DeepSeek V4,
      MiniMax M3, Grok Build, Kimi K2.6/K2.7 Code, and Claude adaptive-thinking rows,
      including unsupported thinking-level and temperature compatibility
      metadata.
- [x] Add deterministic generated-metadata, Responses route/header, and
      DeepSeek V4 Pro payload coverage for the completed Zen cohort; no live
      OpenCode calls are required.
- [ ] Keep `cmd/sigma-surface-probe` as an opt-in live diagnostic tool for
      route-shape regressions, capability limits, and upstream availability
      changes; do not add live OpenCode calls to `mise run ci`.

## Fireworks parity

Sigma now has opt-in live Fireworks surface probes for the current
OpenAI-compatible Fire Pass route and an Anthropic-compatible Messages route.
These probes are diagnostics only; deterministic fixtures remain the release
evidence bar.

- [x] Add credential-gated Fireworks surface probes to
      `cmd/sigma-surface-probe`.
- [ ] Keep live Fireworks probing out of `mise run ci`; use it only for manual
      compatibility investigation with `FIREWORKS_API_KEY`.
- [x] Add Fireworks Anthropic-compatible routing before promoting regular
      Fireworks model rows that do not use the OpenAI-compatible Fire Pass
      route.
- [x] Promote focused Fireworks OpenAI-compatible and Anthropic-compatible
      metadata after matching deterministic modeldata and compatibility
      coverage exists.
- [x] Record verified standard-serverless input, cached-input, and output
      pricing for the curated Fireworks routes, with deterministic request
      coverage for Messages cache affinity and tool compatibility.
- [x] Add current serverless NVIDIA Nemotron 3 Ultra metadata to the existing
      Fireworks Chat Completions and Anthropic-compatible Messages routes.
- [ ] Keep broader Fireworks catalog discovery, unrequested payload/error
      behavior, and live-provider coverage deferred until specific routes need
      evidence beyond the shared adapters.

## Google parity

The Google adapters already cover the v0.2 request and stream slice with
deterministic fixtures. The next useful hardening pass around Vertex
credentials, model-scoped routing metadata, and tool-call replay for
Google-hosted non-Gemini routes is complete. Vertex project/location routing
stays explicit through `VertexConfig` or provider options, and ADC/OAuth tokens
stay caller-supplied through `WithVertexTokenProvider`. Broader model coverage
should still come through the catalog refresh workflow.

- [x] Treat Vertex API-key placeholder values such as angle-bracket markers and
      local credential sentinels as unavailable in auto credential mode, so a
      configured OAuth/ADC token provider can be used instead of sending the
      placeholder as `X-Goog-Api-Key`.
- [x] Let Google Generative AI and Vertex adapters consume concrete
      model-scoped `baseURL` and `headers` metadata where present, while keeping
      request/provider options higher precedence and ignoring generated Vertex
      base URL templates that still contain `{location}` placeholders.
- [x] Normalize Google replayed tool-call IDs for model families that require
      explicit function-call IDs, and omit empty function-response IDs for
      native Gemini requests.
- [x] Add deterministic Google stream coverage for `thoughtSignature`-only
      chunks, empty signature deltas, and signature updates on existing blocks.
- [x] Harden Google replay and image-generation request shapes by omitting
      empty assistant/model blocks, mapping malformed function-call finish
      reasons to provider errors, and rejecting unsupported Gemini multi-image
      requests before dispatch.
- [x] Add focused Google Gemini API embeddings and image generation adapters
      with deterministic payload and response fixtures.
- [x] Add focused Vertex AI embeddings and Imagen generation adapters with
      explicit project/location routing and deterministic fixtures.
- [x] Add curated native Vertex Gemini 3.1 Flash Lite, Gemini 3.5 Flash, and
      Flash/Flash-Lite latest metadata with deterministic registry coverage.
- [ ] Keep live Google Gemini API and Vertex AI validation out of `mise run ci`;
      use credential-gated probes only for manual compatibility investigation.
- [ ] Keep ambient Vertex project/location environment fallback and built-in ADC
      token discovery deferred unless Sigma adds a broader provider credential
      loading policy.
- [ ] Add broader Vertex-specific fixtures only when Vertex behavior diverges
      from the shared Google payload or stream parser.
- [ ] Expand Google and Vertex catalog coverage only through the catalog refresh
      workflow, with deterministic modeldata, payload, error, and compatibility
      coverage for promoted rows.

## Mistral parity

- [x] Add typed Mistral Conversations tool-choice controls for automatic,
      required, disabled, and any-tool selection while preserving raw provider
      options for advanced request fields.
- [x] Add Mistral Conversations base64 image input and image-bearing tool
      results for direct Pixtral models with deterministic payload fixtures.
- [x] Add Mistral Conversations prompt-cache keys and cached-token usage
      accounting for cache-enabled session requests.
- [x] Add Mistral URL image references for image-capable Conversations models
      with deterministic user-input and tool-result payload fixtures.
- [x] Harden Mistral Conversations payloads against Chat Completions-only field
      shapes: image chunks use `image_url`, tool results stay string-valued,
      native Magistral `prompt_mode` is top-level, and named tool choice is
      rejected locally.
- [ ] Add Mistral file image references only after Sigma defines an explicit
      chat-level file-loading policy.
- [ ] Add Mistral built-in connector tools such as web search, code interpreter,
      image generation, and document libraries after deciding how they map to
      Sigma provider-defined tools.
- [ ] Add Mistral Conversations append and restart support only if Sigma exposes
      provider conversation lifecycle operations beyond single-turn streaming.
- [ ] Expand broad Mistral generated metadata only through the catalog refresh
      workflow, with deterministic modeldata, payload, error, and compatibility
      coverage for promoted rows.

## Bedrock parity

- [x] Derive the `eu-central-1` runtime endpoint for built-in EU regional
      inference-profile rows when callers have not configured an endpoint,
      region, or AWS region environment variable.
- [x] Add focused EU Anthropic Bedrock regional metadata rows for high-value
      Claude routes that already match the built-in EU runtime endpoint
      fallback.
- [x] Derive the runtime region from Bedrock application inference profile ARNs
      supplied as the model ID or `inference_profile_arn` provider option before
      AWS region environment fallbacks, while preserving explicit region
      overrides.
- [x] Add focused Bedrock `InvokeModel` embedding support for Titan, Cohere,
      and Nova text embedding request shapes through the existing stdlib
      credential and signing path.
- [x] Add request-scoped Bedrock bearer-token auth through typed Bedrock options
      before resolver and environment credential fallback.
- [x] Treat bare request-scoped and stored API-key credentials as Bedrock bearer
      tokens while preserving SigV4 signing for credentials with AWS key metadata.
- [x] Add request-scoped Bedrock region and static AWS credential helpers before
      environment fallback while keeping AWS SDK integration and SSO deferred.
- [x] Add stdlib Bedrock default-chain credential loading for shared profiles,
      ECS credentials, web identity, and IMDS behind the existing fakeable
      credential detector.
- [x] Keep Bedrock SigV4 canonical request paths aligned with escaped model-ID
      wire paths for inference-profile ARNs across Converse Stream and
      embeddings.
- [x] Harden Bedrock Claude replay compatibility for split reasoning
      signatures, redacted reasoning content, event-stream exception
      classification, and Claude 5-family thinking/cache predicates.
- [x] Treat unrecognised Bedrock Converse terminal stop reasons as typed
      provider errors instead of successful unknown completions.
- [x] Replace blank required user/tool-result text with a placeholder and drop
      blank replayed assistant text blocks that Bedrock Converse rejects.
- [x] Append the AWS data-retention documentation link to Bedrock provider
      errors that reject the configured data retention mode.
- [x] Add Nova 2 Lite `reasoningConfig` support for low, medium, and high
      provider-neutral reasoning levels, with local validation for unsupported
      budgets, levels, and high-effort inference combinations.
- [x] Add focused direct Bedrock GPT-5.6 Luna, Sol, and Terra metadata with
      reviewed text/image, tool, limit, and cache-pricing coverage while
      preserving conservative reasoning-control metadata.
- [ ] Keep live Bedrock validation out of `mise run ci`; use credential-gated
      checks only for manual compatibility investigation.
- [ ] Expand broad Bedrock generated metadata only through the catalog refresh
      workflow, with deterministic modeldata, payload, error, regional routing,
      and compatibility coverage for promoted rows.
- [ ] Keep AWS SSO and full SDK-equivalent credential-chain behavior outside
      the built-in stdlib Bedrock adapter unless Sigma adopts an AWS SDK
      credential integration.

## Authentication and credentials

- [x] Implement OpenAI Codex device-code OAuth login with caller-owned
      credential persistence.
- [x] Implement OpenAI Codex browser callback OAuth login with caller-owned
      credential persistence.
- [x] Implement Anthropic (Claude Pro/Max) browser callback OAuth login,
      refresh helpers, and an in-memory OAuth token provider with caller-owned
      credential persistence.
- [x] Implement GitHub Copilot device-code OAuth login, Copilot token refresh,
      and an in-memory OAuth token provider with caller-owned credential
      persistence.
- [x] Add explicit GitHub Copilot model-policy enablement helpers with
      per-model result reporting, without making model enablement an automatic
      login side effect.
- [x] Add a focused OpenAI Codex credential-store bridge for storing OAuth login
      results through caller-supplied `CredentialStore` implementations.
- [ ] Keep file-backed, encrypted, OS keychain, UI-driven, and broader
      OAuth-provider token persistence deferred to caller-owned integrations.
- [x] Add Anthropic Claude Code OAuth identity headers and Claude Code
      tool-name canonicalization, applied automatically when the resolved
      Anthropic credential is an OAuth token.
- [x] Redact Google API-key headers and Cloudflare AI Gateway auth headers in
      shared diagnostic/debug-hook header copies.
- [x] Wire OpenAI Codex browser/device-code login and refresh into Codex
      Responses through `openai.NewCodexOAuthTokenProvider`.
- [x] Add deterministic coverage for Codex and Anthropic login/refresh flows
      plus GitHub Copilot login, refresh, token-provider, and model-policy
      helpers without live network calls; persistence flows stay deferred with
      caller-owned token persistence.

## Anthropic-compatible routing

- [x] Add typed Anthropic Messages controls for tool choice, thinking display,
      and explicit interleaved-thinking beta opt-in while preserving raw
      provider options for advanced fields.
- [x] Add GitHub Copilot and Cloudflare AI Gateway Anthropic Messages routing
      coverage before making either a first-class Anthropic-compatible row.
- [x] Normalize built-in Anthropic Messages base URLs for Anthropic, Vercel AI
      Gateway, GitHub Copilot, and Cloudflare AI Gateway so Sigma's adapter
      appends `/messages` to versioned route bases.
- [ ] Add opt-in live Anthropic-compatible provider probes for compatibility
      metadata, separate from the deterministic release gate.

## Transports

- [x] Add Codex Responses WebSocket transport support.
- [x] Add Codex WebSocket session caching, cleanup, and SSE fallback behavior.
- [ ] Add WebSocket transport support for other provider routes only when their
      route-specific wire protocols are covered by deterministic fixtures.
- [x] Ensure unsupported transport choices continue to fail locally before any
      network call, with tests asserting the early failure.

## Usage and cost reporting

- [x] Separate OpenAI Responses cached input tokens from ordinary input tokens
      when provider usage includes cache-read details.
- [x] Add deterministic approximate request token estimates that can anchor on
      persisted assistant usage without adding provider tokenizer dependencies.
- [ ] Add tokenizer-based token estimates as an alternative to provider-reported
      usage.
- [ ] Reconcile tokenizer estimates against provider usage data and model
      metadata so reported cost stays consistent.

## Request controls

- [x] Add provider-neutral structured-output and top-logprob request controls
      that map onto existing OpenAI-compatible, Anthropic Messages, and Bedrock
      Converse structured-output paths with local validation for unsupported
      APIs.
- [ ] Keep broader provider-neutral structured-output mappings deferred until
      each provider family has explicit request, response, and fallback
      semantics.

## Platform reach

- [ ] Add browser-specific behavior support (the MVP package stays server/CLI
      friendly).
- [ ] Confirm build constraints / packaging keep server/CLI builds unaffected.

## Agent runtime and orchestration

- [ ] Add agent runtime integration on top of the provider-neutral primitives
      `sigma` exposes (orchestration is deferred to later integration cards).
- [x] Implement cross-provider context handoff beyond diagnostic surface probes.
      Expose public helpers to adapt conversation messages (assistant provenance,
      thinking, tool calls/results including images) for a target model. Reuse and
      promote internal transform logic for thinking-to-tagged-text conversion
      (when target lacks reasoning support or API families differ), image
      downgrade or explicit rejection for non-vision targets, developer role
      normalization, tool name repair, unanswered call repair with synthetic
      error tool results, exact-model thinking preservation, provider-safe
      replay ID normalization, and tool-result-to-user assistant bridging only
      for targets that require it.
- [x] Produce explicit capability-loss reports from handoff transforms (counts
      or details of converted thinking blocks, elided/rejected images, other
      degradations) so callers can surface changes rather than experiencing
      silent behavior shift. Support both whole-request and incremental message
      list adaptation.
- [x] Cover handoff surfaces with deterministic behavioural
      tests (text+thinking+tools+image cases to non-supporting targets, error
      paths, provenance preservation) meeting the evidence bar in RELEASING.md.
- [x] Implement durable credential storage for OAuth and stored API-key flows.
      Provide a CredentialStore interface (read, modify-with-fn for serialized
      atomic updates during refresh, delete) plus in-memory default. Integrate so
      caller-supplied stores participate in EnvironmentAuthResolver paths and
      provider OAuth login/refresh (Anthropic, GitHub Copilot, OpenAI Codex)
      without changing existing caller-owned default behaviour. Stored auth
      descriptors can also apply provider-scoped request configuration such as
      routing placeholders before provider URLs are built.
- [ ] Add file-backed, encrypted, OS keychain, or UI-driven credential
      persistence only as separate caller-owned integrations on top of
      CredentialStore; keep Sigma's built-in store process-local.
- [x] Support runtime/dynamic text model discovery and refresh for custom or
      provider-registered sources (local inference servers, routers with live
      catalogs) so Client.Models and registry contents are not limited to the
      static generated catalog while preserving curated metadata as the reliable
      offline baseline and reviewable default.
- [ ] Extend runtime/dynamic refresh beyond text models only after image,
      embedding, built-in live provider catalog refresh, and credential-backed
      discovery semantics are settled separately.
