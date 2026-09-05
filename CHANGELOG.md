# Changelog

All notable changes to this project will be documented in this file.

The project follows standard Major.Minor.Patch versioning and Go module
semantic import versioning. The initial release is `v0.1.0`; public APIs may
still change before `v1.0.0`, with breaking changes called out in release notes.

## [0.8.0] - Unreleased

See [release notes](docs/release-notes-v0.8.0.md).

### Added

- Responses models can now opt out of automatic `max_output_tokens` through
  `OpenAIResponsesCompat.SupportsMaxOutputTokens`. Unspecified capability keeps
  existing behavior; explicit sampling and body overrides remain available,
  while Codex continues omitting the field unconditionally.
- Generated OpenRouter image metadata now includes Seedream 5.0 Lite and Pro,
  Qwen Image 3 and 3 Pro, Meta Muse Image, Grok Imagine Image 2.0, and four
  Recraft V4 Styles variants through the existing image adapter. Models without
  independently verified pricing retain zero-cost estimates.
- Z.ai and Z.ai Coding CN now include GLM-5.2 Highspeed and GLM-5.3 with
  million-token context limits and model-specific reasoning aliases. GLM-5.2
  receives API-equivalent estimated pricing, and both Coding Plan routes now
  apply the same evidence-backed estimates to GLM-5.1 and GLM-5V-Turbo. Z.ai
  Coding CN also adds the text-and-image GLM-4.6V model with Z.ai reasoning,
  streamed tools, and its required `max_tokens` field, while models without
  matching price evidence retain zero-cost estimates and default requests
  remain unchanged.
- Direct DeepSeek now includes the experimental DeepSeek V4 Flash Vision model
  through the existing OpenAI-compatible Chat Completions route, with text and
  image input, tools, low through maximum reasoning, million-token limits, and
  conservative peak-rate cost estimates. Existing direct V4 Flash and V4 Pro
  estimates now use the same documented peak-rate basis.
- Direct Anthropic Claude Fable 5 requests can now opt into
  catalog-declared server-side refusal fallbacks through
  `AnthropicOptions.EnableRefusalFallbacks`. Defaults remain unchanged, while
  declared fallback responses report usage and estimated cost against the
  returned model's generated pricing.
- Direct Anthropic Claude Opus 5 now preserves the exact provider-native
  thinking effort on partial and terminal assistant messages and replays it
  for matching prior turns. Omitted reasoning defaults to `high`; explicit
  thinking off is rejected locally, and other Anthropic-compatible routes
  remain unchanged.
- `WithToolChoice` now provides provider-neutral automatic or disabled tool
  selection across built-in text providers while retaining provider-specific
  controls for required and named-tool selection.
- Text stream events now expose `StopReasonPending` on in-progress
  `PartialMessage` snapshots, including an empty snapshot on the initial start
  event, while terminal messages retain their existing stop reasons.
- Existing strict function-tool opt-ins now derive provider-compatible closed
  schemas for supported OpenAI-compatible Chat Completions, Responses, and
  Mistral Conversations routes, plus capability-gated Anthropic Messages
  models. Local validation also treats provider-emitted `null` placeholders
  for optional non-nullable arguments as omitted without mutating caller-owned
  schemas or arguments.
- Provider OAuth descriptors and registry auth summaries now identify known
  subscription-backed flows, allowing applications to distinguish them from
  generic OAuth sign-in without changing credential resolution or dispatch.
- GitHub Copilot OAuth callers can now discover the authenticated account's
  available model IDs and use the returned snapshot as a `Client.Models`
  filter without changing Sigma's curated catalog or enabling model policies.
- `IsRecoverableMaxTokens` now lets callers detect max-token completions that
  ended below the original requested or model output limit before any
  context-based clamping, without automatically replaying the request.
- Reviewed OpenAI Responses and Codex Responses models now load deferred client
  tools through native, message-anchored `additional_tools` items, while older
  capable models retain the existing client tool-search replay fallback.
- Text, image, and embedding requests can now require a minimum remaining OAuth
  lifetime before dispatch, triggering an early serialized refresh for stored
  credentials and Sigma's built-in caller-owned token providers without
  shortening provider refresh windows or changing omitted-option behavior.
- OpenAI-compatible Chat Completions, Responses, and Azure Responses requests
  now accept arbitrary request-scoped sampling parameters through
  `OpenAIOptions.SamplingParameters`; sampling values override typed request
  fields, while provider `extra_body` values retain final precedence.
- Caller-registered OpenAI-compatible models can now declare default arbitrary
  sampling parameters through `OpenAICompatibleModelConfig.SamplingParameters`
  or `MetadataOpenAISamplingParameters`. Sigma's core and typed request fields
  override model defaults, request-scoped sampling values override matching
  keys, and provider `extra_body` values retain final precedence.
- Direct OpenAI Responses requests can now be submitted for background
  execution through explicit provider-neutral submit, fetch, and cancel
  methods. JSON-serializable handles retain exact provider, API, model, and
  response ID provenance; each fetch performs one status check, while terminal
  responses reuse Sigma's existing content, tool, usage, cost, and incomplete
  response conversion. Existing streaming and completion requests are
  unchanged.
- Custom OpenAI-compatible Chat Completions models can now select
  `thinking_token_budget`, `thinking_budget`, or `thinking_budget_tokens`
  through `OpenAICompletionsCompat.ThinkingTokenBudgetField`, reusing Sigma's
  existing explicit reasoning-budget options while reserving 1,024 tokens for
  visible output. `SupportsThinkingTokenBudget` remains a backward-compatible
  alias for `thinking_token_budget`, and the explicit selector takes precedence.
- `cmd/sigma-evals-runner` now applies an independent, configurable timeout to
  each case/model/repetition run so one stalled provider call is recorded as an
  operational failure without cancelling later evaluations; the existing
  overall command timeout remains the hard invocation limit.
- Repository-internal Sigma evaluation harnesses can now execute bounded,
  caller-owned text tool loops while retaining complete usage and trace
  artifacts. The opt-in live runner adds a deterministic tool-call round-trip
  case that requires a matching successful call, result, and final answer.
- `cmd/sigma-surface-probe` now has an opt-in Vertex Anthropic Claude route
  over the existing `streamRawPredict` adapter. It defaults to the built-in
  Claude Sonnet 4.6 model, validates explicit model IDs locally, and reuses
  explicit Vertex project, location, API-key, or OAuth credential inputs
  without adding live provider calls to CI.
- `cmd/sigma-surface-probe -images` now has opt-in Google Gemini API and Vertex
  AI routes for Gemini image generation through `generateContent`. The probes
  reuse generated image models, validate explicit selections locally, isolate
  each case with its own timeout, and require actual image data for success
  while remaining outside deterministic CI. Generated Google image metadata
  now replaces retired Imagen 4 rows with Gemini 3.1 Flash Image for direct and
  Vertex routes, and the Vertex image adapter accepts Gemini inline-image
  responses while retaining Imagen `predict` compatibility for caller-defined
  models.
- Persisted tool-result messages can now carry optional `Usage` describing
  caller-owned tool execution. The metadata survives replay and handoff while
  remaining excluded from provider payloads, request-token estimates, model
  turn cost accounting, and evaluation usage totals.
- OpenAI-compatible Chat Completions models can now opt out of terminal
  `finish_reason` support through `OpenAICompletionsCompat`, accepting an
  explicit `[DONE]` marker while continuing to reject unmarked stream EOF.
- Qwen Token Plan Individual is now available as a distinct subscription route
  with eight curated models, including DeepSeek V4 Pro 0813, the shared
  international credential and endpoint, and model-specific Qwen thinking
  controls.
- Baseten now has a first-class OpenAI-compatible Chat Completions route with
  focused GLM 5.2 and Kimi K2.6 metadata, `BASETEN_API_KEY` discovery, and
  model-specific chat-template thinking controls.
- Generated direct xAI metadata now includes Grok 4.6 through OpenAI Responses
  with text and image input, function tools, 500k-token limits, tiered pricing,
  and low through `xhigh` reasoning levels.

### Changed

- Regenerated the OpenCode Zen and Go catalogues to the current 63- and
  27-model sets. The refresh adds newer Claude, Gemini, GPT, Grok, DeepSeek,
  GLM, Kimi, Qwen, Muse, and related models, reconciles route, capability,
  thinking-level, pricing, cache-pricing, and token-limit metadata, and removes
  ten models no longer present in the advertised catalogues.
- OpenCode live surface probes now use generated route metadata for known
  models and classify provider `RegionError` responses as upstream availability
  failures, including workspace-level China-hosting opt-in requirements.
- Kimi and Kimi Coding requests now use the Sigma-owned `sigma/kimi-coding`
  default user agent from the shared provider wrapper instead of duplicated
  model-catalog headers. Explicit provider, model, and request header overrides
  retain their existing precedence.

### Fixed

- Automatic long-cache requests for Responses models marked
  `SupportsExplicitPromptCacheMode` now use `prompt_cache_options.ttl: "30m"`
  instead of legacy `prompt_cache_retention: "24h"`. Explicit request sampling
  or body cache fields suppress automatic long-cache directives, including
  explicit nulls, while legacy models and existing no-cache behavior retain
  their current contracts.
- GitHub Copilot Claude Fable 5 now uses its Anthropic-compatible Messages
  route so selected reasoning levels are transmitted through adaptive thinking
  controls while preserving existing authentication, headers, capabilities,
  pricing, and limits.
- Generated OpenRouter text metadata now distinguishes optional and mandatory
  reasoning and exposes only the supported effort levels for the curated Claude
  Sonnet 5, DeepSeek V4 Pro, Gemini 3.5 Flash, GPT-5.2 Codex, and GPT-5.6
  routes. Optional routes explicitly disable omitted reasoning with
  `reasoning.effort: "none"`; mandatory routes preserve provider defaults and
  reject explicit off or unsupported efforts locally.
- OpenAI, Azure, and Codex Responses history replay now omits failed or
  aborted assistant turns and their associated tool results, preventing
  incomplete reasoning or tool-call items from reaching later requests while
  preserving caller-owned partial finals and valid history.
- Explicit GitHub Copilot model-policy enablement now retries HTTP 429
  responses up to twice within a five-second budget, honoring provider retry
  delays and context cancellation while preserving caller-invoked policy
  changes and independent per-model results.
- Z.ai-format OpenAI-compatible Chat Completions requests now preserve
  same-provider/API/model assistant reasoning as `reasoning_content` and send
  `clear_thinking: false` whenever reasoning is enabled. Explicit replay
  compatibility overrides remain authoritative, while disabled reasoning and
  other formats retain their existing payloads.
- OpenAI-compatible Chat Completions requests now preserve explicitly supplied
  typed `tool_choice` values even when no tool definitions are emitted,
  including requests whose tools are fully deferred. Omitted choices remain
  absent, declared-tool behavior is unchanged, provider-specific typed choices
  override provider-neutral choices, and low-level payload overrides keep final
  precedence.
- OpenAI-compatible Chat Completions streams now preserve validated encrypted,
  signed-text, and summary `reasoning_details` in provider order, coalescing
  consecutive streamed text and summary fragments into complete logical
  entries before assistant-content persistence and exact same-provider/API/
  model replay. Later fragments fill missing identity and format metadata
  without replacing prior values; invalid persisted entries are omitted
  without losing valid siblings, legacy tool-call metadata remains replayable,
  and requests without stored details are unchanged.
- OpenAI, Azure, and Codex Responses streams now preserve non-empty assistant
  message phases in opaque content metadata. Recognized `commentary` and
  `final_answer` phases retain their original item boundaries on exact
  provider/API/model replay, while unknown or incompatible phases remain
  diagnostic-only and normalized stop reasons are unchanged.
- OpenAI-compatible Chat Completions streams now preserve the first
  provider-issued ID and name for indexed function and grammar tool calls when
  later continuation deltas conflict, while still accepting a late first
  provider ID after a synthetic stream start.
- Google Generative AI and Vertex streams no longer convert explicit
  max-token, provider-error, or unknown finish reasons into successful tool-call
  stops merely because the response also contains a function call. Tool calls,
  usage, cost, and raw finish-reason diagnostics remain available.
- Amazon Bedrock Converse Stream now preserves scalar base64
  `redactedContent` reasoning across split stream deltas and subsequent replay,
  while omitting invalid persisted blobs instead of sending malformed history.
- Xiaomi's direct and regional Token Plan catalogs no longer advertise the
  retired `mimo-v2-flash`, `mimo-v2-omni`, or `mimo-v2-pro` model IDs, while
  retaining the supported V2.5 lineup.
- Baseten GLM 5.2 metadata now advertises text and image inputs, allowing
  image-bearing tool results to remain available through the shared
  OpenAI-compatible Chat Completions request path.
- OpenAI-compatible Chat Completions usage now recognizes top-level
  `cached_tokens` as a cache-read fallback, keeping compatible Kimi and
  Moonshot cache hits out of ordinary input-token and cost accounting while
  preserving existing nested and legacy-field precedence.
- OpenAI Responses-compatible and Azure OpenAI Responses requests now clamp
  typed `max_output_tokens` values below 16 to the accepted request minimum,
  while low-level request overrides retain precedence and Codex continues
  omitting the unsupported field.
- Anthropic Messages refusal stops now retain non-empty structured
  `stop_details` in opaque assistant provider metadata while preserving
  content-filter normalization and the raw provider stop reason.
- Google Generative AI and Vertex replay now retain blank text and thinking
  parts only when they carry a valid same-provider/API/model thought signature,
  omitting whitespace-only or unusably signed blanks without changing nonblank
  content or caller-owned history.
- Runtime text, image, and embedding model sources now publish overlapping
  refresh results in latest-started order, preventing slower superseded
  refreshes or text catalog restores from overwriting newer registry state.
- OpenRouter Anthropic-routed Chat Completions now place the final conversation
  prompt-cache marker on the latest non-empty tool result instead of the
  preceding assistant or user message, while retaining the bounded system and
  tool-definition markers.
- Direct DeepSeek V4 Flash and its OpenCode Zen and Go routes now
  expose their supported low reasoning effort while preserving the existing
  high and `xhigh`-to-`max` mappings; other routed DeepSeek models retain their
  independently reviewed level support.
- OpenAI, Azure, and Codex Responses now normalize incomplete terminals as
  max-token or content-filter stops only for recognized reasons. Missing and
  unknown reasons return typed provider errors while preserving partial output,
  usage, status, and raw incomplete diagnostics.
- Provider failures reporting an exhausted upstream request buffer now classify
  as transient and retryable, allowing caller-owned policies to retry the same
  model without automatic post-body request replay.
- Provider-wrapped DNS lookup failures, connection and socket/WebSocket
  closures, reset-before-headers and HTTP/2 no-response failures, explicit
  provider retry guidance, `ResourceExhausted` capacity failures, and known
  premature-stream diagnostics now classify as transient and retryable when
  structured status or type evidence is unavailable. Existing auth, billing,
  quota, rate-limit, context-overflow, and cancellation precedence is
  unchanged, and Sigma does not automatically replay post-body failures.
- Opt-in pre-body HTTP retries now treat `408 Request Timeout` and `409 Conflict`
  as transient alongside `429` and `5xx` responses. Exhausted or disabled
  retries retain same-request retry advice, structured provider code and message
  precedence is unchanged, and Sigma does not automatically replay post-body
  failures.
- Codex Responses SSE and WebSocket streams now accept `response.done` as a
  successful terminal signal and retain explicit `end_turn` values in opaque
  assistant provider metadata without changing normalized stop reasons.
- OpenAI Responses and Codex Responses tool-call namespaces now survive
  streaming and compatible replay through provider metadata without leaking
  them into incompatible provider, API, or model histories.
- Google Generative AI and native Vertex Gemini 3 requests now preserve
  normalized tool-call IDs on replayed function calls and matching tool
  results, while older Vertex Gemini requests continue omitting unsupported
  IDs.
- Amazon Bedrock Converse Stream replay now removes empty object-member names
  from outbound tool inputs, avoiding provider rejection while preserving
  caller-owned and provider-emitted tool arguments.
- Amazon Bedrock Converse Stream service exceptions now retain the requested
  model and AWS request ID in typed provider errors and assistant diagnostics
  without changing retry classification.
- Qwen Token Plan now exposes the generally available Qwen3.8 Max model instead
  of its retired preview ID while preserving native `reasoning_effort` controls
  across the international and China regional routes; Qwen3.7 Max remains
  toggle-only.
- Fireworks GLM 5.2 and GLM 5.2 Fast requests now use session affinity for
  automatic prompt caching without sending unsupported long-cache retention.
- Anthropic Messages streams now emit non-empty text and thinking carried by
  content-block start events as ordered initial deltas, preserving complete
  incremental output alongside signatures and citations.

## [0.7.0] - 2026-08-02

See [release notes](docs/release-notes-v0.7.0.md).

### Added

- A repository-internal Go evaluation framework now provides generic harnesses
  and judges, a sequential Sigma text harness, paired baseline/candidate
  summaries, private JSONL artifacts, and an opt-in smoke runner under
  `cmd/sigma-evals-runner` for direct OpenAI Responses, OpenCode Go, Fireworks,
  and native Vertex Gemini models. The sequential suite covers factual recall,
  arithmetic, exact formatting, JSON extraction, and multi-turn recall without
  adding a public API or live calls to deterministic CI, and prints compact
  per-run score, usage, latency, cost, and output results through a direct
  `go run` task. The runner can filter cases and compare a baseline with one or
  more candidate models across repeated paired runs, reporting pass-rate lift
  and candidate-minus-baseline token, latency, and cost deltas.
- Text streams and completions now accept `WithRequestHTTPClient` for an
  opt-in HTTP/SSE client override on an individual call.
- Anthropic Messages now resolves `ANTHROPIC_AUTH_TOKEN` bearer credentials,
  then `ANTHROPIC_OAUTH_TOKEN`, then `ANTHROPIC_API_KEY` through the existing
  environment credential resolver.
- Generated Claude Opus 5 metadata now covers direct Anthropic, the global
  Amazon Bedrock inference-profile route, and GitHub Copilot's Anthropic
  Messages route with reviewed adaptive-thinking, limit, and pricing metadata.
- Mistral Conversations now supports typed named-function tool selection.
- Mistral Conversations now supports server-executed web search, premium web
  search, and document-library tools, preserving returned source references
  and citations.
- Mistral Conversations now preserves existing boolean per-tool strict JSON
  Schema settings when serializing function tools.
- xAI now exposes first-class OpenAI Responses provider registration helpers.
- xAI now supports caller-configured device-code OAuth login, token refresh,
  in-memory credential resolution, and opt-in provider-auth registration while
  applications retain OAuth client registration and token persistence ownership.
- Generated Kimi Coding metadata now includes K3 and Kimi For Coding HighSpeed
  with reviewed context, output, image-input, tool, reasoning, and estimated
  cost metadata.
- Kimi Coding now supports opt-in subscription device-code OAuth login, token
  refresh, in-memory credential resolution, and provider-auth registration
  while applications retain token persistence ownership.
- OpenRouter now supports opt-in browser PKCE login that returns a permanent
  API key and can store it through a caller-supplied CredentialStore for the
  existing text and image routes.
- OpenRouter browser PKCE login now supports a caller-managed pasted redirect
  URL or authorization-code fallback for browsers running on another machine.
- Generated OpenRouter image metadata now includes Krea 2 Large, Medium, and
  Medium Turbo, MAI-Image 2.5 Pro, and Auto Router Beta through the existing
  image adapter.
- Generated OpenCode Go metadata now routes Grok 4.5 through OpenAI Responses
  and Kimi K3 through Chat Completions, with reviewed text/image, tool,
  reasoning, limit, and pricing metadata.
- Radius gateway now has a first-class API-key-authenticated text provider that
  refreshes gateway-owned model metadata at runtime and supports native
  streaming, replay, usage, and response IDs without adding a static catalog.
- Radius gateway now supports opt-in caller-configured browser and device-code
  OAuth login, token refresh, stored-provider auth, and OAuth-authenticated
  runtime catalog refresh while applications retain client registration and
  token persistence ownership.
- Radius gateway now supports opt-in caller-owned runtime catalog snapshots and
  explicit offline model restoration while normal catalog refreshes remain
  network-backed.
- Qwen Token Plan now has first-class OpenAI-compatible Chat Completions
  provider registration helpers for international and China regions, with
  focused Qwen3.7 Max and Qwen3.8 Max Preview metadata.
- OpenAI Responses and Chat Completions now support grammar-constrained custom
  tools with typed Lark or regex definitions. Reviewed capability metadata and
  explicit caller opt-in or opt-out control native custom-tool serialization;
  replay, tool results, and streamed custom input retain Sigma's existing
  tool-call surface.
- `cmd/sigma-surface-probe` now has an opt-in native Vertex Gemini text route
  with catalog-backed model selection, explicit project/location routing,
  externally supplied OAuth access tokens or API keys, and capability-gated
  image, function-tool, and thinking diagnostics.
- Gemini 2.5 reasoning levels now serialize as model-family token budgets for
  direct Google and native Vertex requests. Generated metadata marks disabled
  thinking unsupported on Gemini 2.5 Pro and keeps the Vertex probe from
  sending unsupported disabled-thinking cases.

### Changed

- Package-level model lookup, routing, generation, image, and embedding helpers
  now share the live default registry without cloning the full catalog per call;
  public `DefaultRegistry` and `NewClient` isolation remain unchanged. Registry
  reads also avoid write-locking lazy initialization.
- `Client.EmbedBatch` now applies `MaxParallelBatches` to configured limit
  groups and retry-generated splits through bounded workers, canceling sibling
  work on fatal errors while preserving input-ordered vectors.
- Reviewed direct OpenAI GPT-5.6 Responses models now translate explicit
  `CacheRetentionNone` requests into explicit prompt-cache mode, preventing
  implicit cache writes while leaving default and unsupported-model payloads
  unchanged.
- OpenAI Responses routes now retain non-empty raw terminal response status in
  assistant provider metadata without changing normalized stop reasons or
  provider error behavior.
- OpenAI-compatible Chat Completions routes now retain non-empty raw terminal
  `finish_reason` values in assistant provider metadata without changing
  normalized stop reasons or provider error behavior.
- Anthropic Messages, Google Gemini and Vertex AI, and Amazon Bedrock Converse
  streams now retain non-empty raw terminal reasons under their wire-native
  assistant provider metadata keys without changing normalized stop reasons or
  provider error behavior.
- The native Vertex Gemini text catalog now includes Gemini 3.6 Flash and
  Gemini 3.5 Flash-Lite, removes retired or superseded Gemini 1.5, 2.0, dated
  2.5 Flash-Lite preview, and 3 Pro Preview rows, and enables medium thinking
  for both Gemini 3.1 Pro Preview endpoints.
- Native Vertex Flash and Flash-Lite latest aliases no longer advertise named
  thinking levels rejected by those endpoints. Surface-probe repairs now keep
  the original failure classification when minimal text succeeds and report
  that availability evidence separately.
- `cmd/sigma-surface-probe` now gives each primary case and repair attempt an
  independent timeout within the overall run deadline, classifies deadline and
  rate-limit failures as upstream availability, and separates logprob support
  from output-budget repair evidence. Fireworks Kimi K3 discovery reuses its
  generated registry metadata.
- Codex request-affinity headers now clamp session IDs to 64 characters,
  OpenRouter cache affinity uses its `x-session-id` header, and unrecognised
  Bedrock terminal stop reasons now return typed provider errors.
- Generated xAI Grok 4.5 metadata now routes through OpenAI Responses with
  low, medium, and high reasoning levels. Long-lived prompt-cache retention is
  omitted for this route while cache keys and session affinity remain available.
- Kimi Coding K3 now supports low, high, and max adaptive-thinking efforts;
  the stale `k2p7` catalog row is no longer included. Current estimated rates
  and empty-signature replay compatibility remain available where supported.
- Generated Fireworks metadata now includes verified standard-serverless input,
  cached-input, and output rates for its curated Chat Completions and Messages
  routes. Deterministic coverage also locks the Messages route's cache-affinity
  header and unsupported tool-field behavior.
- Generated Fireworks metadata now includes NVIDIA Nemotron 3 Ultra NVFP4 on
  the existing Chat Completions and Anthropic-compatible Messages routes with
  its serverless limits, tool and reasoning support, and standard pricing.
- Fireworks Kimi K3 now uses its native Chat Completions reasoning effort,
  required reasoning replay, cache-affinity headers, long-cache suppression,
  and metadata-gated deferred client-tool loading.
- Premature OpenAI Responses and Anthropic Messages stream endings now classify
  as transient, retryable failures while preserving partial final messages;
  applications continue to own post-body request retries.

### Fixed

- Google Generative AI, Mistral Conversations, and Amazon Bedrock Converse
  Stream now reject clean transport endings that omit their required terminal
  marker, preserving partial finals and classifying the failure as transient
  and retryable.
- Codex WebSocket session cleanup now removes connections, continuation state,
  SSE fallback markers, debug stats, and expiry timers. Orphaned diagnostic and
  fallback state expires after the existing five-minute idle window.
- Mistral Conversations now fails safely when an explicit error or unrecognized
  terminal stop reason is returned, while retaining the raw terminal value in
  assistant provider metadata and sanitized diagnostics.
- Sessionless Codex WebSocket handshakes now use monotonic UUIDv7 request IDs,
  and built-in GPT-5.6 Codex metadata reports its 272K context limit so
  impossible long-context budgets and price tiers are not selected.
- Cached Codex WebSocket connections and continuation state are now isolated
  by authenticated account when callers reuse a session ID.
- Cached Codex WebSocket continuations rejected before output now retry once
  with the full request context. Repeated rejections retain the existing SSE
  fallback behavior.

## [0.6.0] - 2026-07-15

See [release notes](docs/release-notes-v0.6.0.md).

### Added

- OpenAI Responses-style streams now preserve final reasoning content and
  multi-part summary boundaries, emit block-end events as output items complete,
  and use a non-empty placeholder when replaying empty tool results.
- Deterministic regression coverage now protects Responses partial tool
  arguments, Google tool-call signature replay, and Bedrock replay content
  sanitization and rejection boundaries.
- Metadata-marked Anthropic Messages and OpenAI/Codex Responses models can now
  defer client-defined function schemas until an annotated tool result, using
  native tool references or client tool-search records while unmarked routes
  keep eager tool payloads.
- Deterministic routing decisions now ship as pure helpers: `ClassifyRequest`
  scores requests into route tiers with weighted rule-based signals,
  `RoutePolicy.Select` picks the first usable tier candidate with escalation
  and exclusions, and `RoutePolicy.Fallback` turns classified upstream errors
  into retry, fallback, or abort advice, including larger-context candidate
  selection on context overflow. Sigma only decides; callers execute requests
  and own health tracking.
- Provider-neutral document/PDF request content blocks now support base64,
  URL, and provider file-ID sources, with initial OpenAI Responses, OpenAI Chat
  Completions, and Anthropic Messages payload support.
- Azure OpenAI Responses now has a first-class provider wrapper with
  provider-scoped registration and request option helpers for endpoint,
  deployment, API version, credential source, and caller-supplied Microsoft
  Entra token credentials while preserving the lower-level OpenAI adapter APIs.
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
- Generated Amazon Bedrock metadata now includes current Claude regional
  inference profiles across AU, EU, Global, Japan, and US routes, plus
  GPT-OSS, DeepSeek R1, and Llama 4 direct models with reviewed capabilities,
  limits, prices, cache rates, and credential defaults.
- Generated Amazon Bedrock metadata now includes direct GPT-5.6 Luna, Sol, and
  Terra Converse Stream rows with text/image and tool capabilities, reviewed
  limits, and input, output, cache-read, and cache-write pricing.
- Generated Cloudflare Workers AI metadata now includes Kimi K2.7 Code and
  GLM 5.2 with reviewed reasoning, tool, image, pricing, and session-affinity
  compatibility.
- Xiaomi now has a first-class OpenAI-compatible provider wrapper for the
  API-billing route and regional token-plan routes, including generated
  metadata, regional API-key discovery, and deterministic registration/request
  coverage for `mimo-v2.5-pro-ultraspeed` and the token-plan MiMo rows.
- Kimi and Kimi Coding now have first-class Anthropic-compatible provider
  wrappers and generated metadata for `kimi-for-coding`, `k2p7`, and
  `kimi-k2-thinking`, including `KIMI_API_KEY` discovery, Kimi CLI request
  headers, adaptive thinking metadata, and session-affinity support.
- GitHub Copilot now has stdlib-only device-code OAuth login, Copilot token
  refresh helpers, an in-memory OAuth token provider that also implements
  Sigma's auth resolver interface, and explicit opt-in helpers for enabling
  Copilot model policies while keeping credential persistence caller-owned.
- Generated GitHub Copilot metadata now includes `claude-fable-5` on the Chat
  Completions route with text/image, tool, reasoning, pricing, and context
  metadata plus conservative request compatibility flags.
- Generated GitHub Copilot metadata now includes `kimi-k2.7-code` on Chat
  Completions and `mai-code-1-flash-picker` on Responses, with reviewed input,
  tool, reasoning, context, pricing, and compatibility metadata.
- Generated OpenCode Go metadata now includes `glm-5.2` and `qwen3.7-plus`,
  with their Chat Completions and Messages routes, capability and pricing
  metadata, and GLM `max_tokens`/reasoning-level compatibility.
- Generated OpenCode Zen metadata now includes GPT-5.6 Luna, Sol, and Terra
  on Responses plus DeepSeek V4 Pro, GLM-5.2, Grok 4.5, Hy3 Free, Kimi K2.7
  Code, MiniMax-M3, Nemotron 3 Ultra Free, and North Mini Code Free on Chat
  Completions, with reviewed routing, capabilities, limits, pricing, and
  compatibility metadata. Cached Zen Responses requests preserve the client
  request ID while omitting `session_id` unless callers explicitly provide
  session headers.
- OpenAI Codex OAuth login results can now be written into caller-supplied
  `sigma.CredentialStore` implementations with
  `openai.StoreCodexOAuthCredentials`, giving store-backed Codex Responses auth
  the same serialized refresh path as other stored OAuth credentials while
  keeping concrete disk or keychain storage caller-owned.
- OpenAI Codex Responses WebSocket transport now honors standard HTTP(S) proxy
  environment variables with `NO_PROXY` exclusions by tunneling through
  HTTP/HTTPS `CONNECT` proxies while preserving the existing SSE fallback.
- OpenAI Codex Responses WebSocket transport now has a Codex-specific connect
  timeout plus session-cache debug stats for created/reused connections,
  full/delta context requests, previous response IDs, WebSocket failures, and
  SSE fallback activation.
- Text request transport options now fail locally before provider dispatch when
  callers pass an unknown transport or request HTTP/WebSocket transport for a
  built-in streaming API that does not support it.
- `sigma.CleanupSessionResources` and `sigma.RegisterSessionResourceCleanup`
  now provide a provider-neutral way to release cached session resources, with
  OpenAI Codex Responses WebSocket sessions registered automatically while the
  provider-specific cleanup helpers remain available.
- Anthropic Messages usage now preserves long prompt-cache write tokens
  separately and prices those writes at the provider's long-cache input
  multiplier while keeping total cache-write tokens unchanged.
- Text-generation usage now carries provider/model identity, provider raw usage
  payloads, normalized tool-use input tokens, and provider-reported cost when
  available, while keeping Sigma's model-metadata cost estimate separate.
- `CostForUsage` now applies validated request-wide model pricing tiers when
  combined input and prompt-cache usage crosses a model-specific threshold.
  Generated high-context GPT metadata now carries the reviewed tiered rates.
- Runtime hardening now covers SSE parsing tolerance for colonless fields and
  CR-only line endings, realistic deterministic fake-provider stream lifecycles
  with partial-message snapshots, best-effort decoded partial tool-call
  argument metadata, 64-bit-safe persisted tool arguments, stricter null
  coercion, image request validation, explicit handoff output coordinates,
  OpenAI-compatible cache-hit usage fallbacks, local/custom streaming-usage
  defaults, `finish_reason: "end"` mapping, request-scoped provider auth
  precedence, and non-negative cache-cost catalog validation.
- Persisted assistant messages can now carry optional usage metadata, and
  `EstimateRequestTokens` plus related helpers provide deterministic
  approximate request token estimates anchored on the latest successful
  provider-reported usage when available.
- `MaxTokensForContext` and `WithMaxTokensForContext` now provide opt-in
  context-aware max-output-token budgeting from model metadata and
  deterministic request estimates without changing provider dispatch defaults.
- `WithAutomaticMaxTokensForContext` now lets callers opt in to dispatch-time
  max-output-token clamping from client defaults or per-request options, with
  request options able to disable a client default and existing validation
  still catching invalid explicit token caps.
- `ReasoningBudgetForContext` and `WithReasoningBudgetForContext` now provide
  opt-in planning for visible output caps and hidden thinking budgets using
  model/context metadata and deterministic request estimates without changing
  provider dispatch defaults.
- Mistral Conversations now maps cache-enabled `sigma.WithSessionID` requests
  to both `prompt_cache_key` and `x-affinity`, and streamed Mistral cached
  prompt tokens now populate `Usage.CacheReadInputTokens` instead of ordinary
  input tokens.
- Mistral Conversations now accepts URL-backed `sigma.ImageURL` chat inputs for
  image-capable models and replays image-bearing tool results as schema-valid
  string references, while preserving existing base64 image input behavior.
- `EnvironmentAuthResolver` now exposes non-secret environment credential
  discovery helpers for ordered candidate variable names and configured
  variable names, with broader built-in API-key defaults for OpenAI-compatible
  provider IDs that previously relied only on generated model metadata.
- Sigma now exposes opt-in credential stores and provider auth descriptors for
  stored API-key and OAuth flows. `CredentialStore`,
  `InMemoryCredentialStore`, registered `ProviderAuth` descriptors, and
  `WithStoredProviderAuth` let applications resolve stored credentials,
  serialize OAuth refreshes, and apply descriptor-provided provider
  configuration for request routing without changing default environment-based
  credential behavior.
- Request options now support final outgoing header suppression across text,
  image, and embedding calls, letting callers remove provider/default
  compatibility headers without adding a generic environment override surface
  or changing credential resolution.
- Cloudflare AI Gateway and Amazon Bedrock now expose provider-specific
  request configuration helpers for AI Gateway account/gateway placeholder
  resolution, Bedrock request regions, and Bedrock request-scoped static AWS
  credentials while preserving existing environment fallbacks.
- Generated Amazon Bedrock metadata now includes focused EU Anthropic Claude
  regional rows for Fable 5, Haiku 4.5, Opus 4.5/4.6/4.7/4.8, and Sonnet 4.6,
  reusing the existing EU runtime endpoint fallback for `eu.` inference-profile
  model IDs.
- Generated Amazon Bedrock metadata now also includes curated non-regional
  Gemma 3, Llama 3.1/3.3/4, Nemotron 3, GPT-5.4/5.5, Palmyra X4/X5, and Grok
  4.3 Converse Stream rows with reviewed input, tool, limit, and pricing data.
  Nova 2 Lite now maps supported provider-neutral reasoning levels to its
  Bedrock reasoning configuration and rejects incompatible local options before
  dispatch.
- Generated Claude metadata now includes focused current Sonnet 5 and Fable 5
  rows across existing Anthropic-compatible routes, including direct
  Anthropic, direct Amazon Bedrock, Cloudflare AI Gateway, Vercel AI Gateway,
  OpenCode Zen, and GitHub Copilot Sonnet 5 coverage while leaving broader
  catalog expansion to the reviewed refresh workflow.
- Generated direct OpenAI Responses metadata now includes GPT-5.6 Luna, Sol,
  and Terra with text/image, tool, and reasoning capabilities; cache-write
  pricing; and 272,000-token high-context price tiers.
- Generated Azure OpenAI Responses and OpenAI Codex Responses metadata now
  includes GPT-5.6 Luna, Sol, and Terra with their route-specific context
  limits, reasoning mappings, pricing, and Codex high-context price tiers.
- Generated direct-provider metadata now includes Cerebras Gemma 4 31B, xAI
  Grok 4.5, and NVIDIA NIM MiniMax M3 and GLM 5.2 with their reviewed
  capabilities, limits, pricing, and existing provider compatibility defaults.
- Generated Google Vertex AI metadata now includes Gemini 3.1 Flash Lite,
  Gemini 3.5 Flash, and the Flash/Flash-Lite latest aliases with reviewed
  capabilities, limits, pricing, and existing native routing defaults.
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
- Hugging Face Router now has a first-class OpenAI-compatible Chat Completions
  provider wrapper, including base URL defaults, bearer auth, `HF_TOKEN`
  discovery, focused generated metadata, and deterministic registration,
  request, error, and cancellation coverage.
- OpenRouter now has a first-class OpenAI-compatible Chat Completions provider
  wrapper, including base URL defaults, bearer auth, `OPENROUTER_API_KEY`
  discovery, generated text metadata reuse, OpenRouter reasoning/routing
  compatibility, and deterministic registration, request, error, and
  cancellation coverage.
- Generated OpenRouter image metadata now includes focused current Gemini image
  and GPT Image routed rows while keeping broad OpenRouter text expansion
  deferred to the catalog refresh workflow.
- Generated OpenRouter text metadata now includes curated Claude Sonnet 5,
  GPT-5.2 Codex, GPT-5.6 Luna/Sol/Terra, Gemini 3.5 Flash, and DeepSeek V4 Pro
  routes with reviewed compatibility, reasoning, pricing, and capability
  metadata. Broader catalog expansion remains deferred to the catalog refresh
  workflow.
- Generated Fireworks metadata now includes focused GLM 5.2
  OpenAI-compatible rows plus additional Anthropic-compatible Messages rows for
  DeepSeek V4, GLM 5.1, GPT OSS, MiniMax, Qwen, and Kimi router variants while
  keeping live Fireworks validation outside deterministic CI.
- NVIDIA NIM now has first-class OpenAI-compatible Chat Completions and
  Embeddings provider wrappers, including base URL defaults, bearer auth,
  generated text and embedding metadata, embedding input-type mapping,
  streaming-usage request defaults, an opt-in live surface-probe route, and
  deterministic registration/request coverage. The generated text catalog now
  also includes direct NIM rows for `openai/gpt-oss-120b` and
  `nvidia/nemotron-3-ultra-550b-a55b`, plus opt-in live `/models` validation
  for reviewing direct NIM catalog availability while normal generation remains
  offline.
- `cmd/sigma-generate-models -diff-catalog` now compares the checked-in
  catalog with a validated candidate catalog and reports added, removed,
  changed, and unchanged text, image, and embedding rows without writing
  generated files.
- `cmd/sigma-generate-models -refresh-catalog` now writes a validated
  review-only candidate catalog from an explicit `models.dev` snapshot path or
  opt-in network source, preserving the checked-in catalog and generated Go
  files until maintainers review the deterministic diff.
- Registries can now accept provider-scoped runtime text model sources through
  `RegisterTextModelSource`, and `Registry.RefreshTextModels` /
  `Client.RefreshTextModels` refresh app-owned dynamic model listings while
  preserving Sigma's curated built-in catalog as the offline default.
- Registries can now accept provider-scoped runtime image model sources through
  `RegisterImageModelSource`, and `Registry.RefreshImageModels` /
  `Client.RefreshImageModels` refresh app-owned dynamic image model listings
  while preserving Sigma's curated built-in image catalog as the offline
  default.
- Registries can now accept provider-scoped runtime embedding model sources
  through `RegisterEmbeddingModelSource`, and
  `Registry.RefreshEmbeddingModels` / `Client.RefreshEmbeddingModels` refresh
  app-owned dynamic embedding model listings while preserving Sigma's curated
  built-in embedding catalog as the offline default.
- Moonshot AI and Moonshot AI CN now have first-class OpenAI-compatible Chat
  Completions provider wrappers, generated Kimi K2.7 Code CN and HighSpeed
  metadata, and metadata-driven omission of disabled-thinking payloads for
  K2.7 Code routes that reject explicit thinking-off requests.
- `cmd/sigma-surface-probe` now has an opt-in cross-provider handoff diagnostic
  that builds small tool-call contexts and replays them pairwise across selected
  live routes without adding live provider calls to CI.
- `cmd/sigma-surface-probe -structured-output` now runs focused
  OpenAI-compatible JSON object and strict JSON Schema capability probes,
  emitting reviewable hints for supported schema output, JSON-object fallback,
  and prompt-only JSON fallback without updating generated metadata.
- `cmd/sigma-surface-probe -images` now runs focused OpenAI image diagnostics
  for generation, multipart edits, reference-only JSON edits, variations,
  streaming partial images, and Responses image-generation tool output without
  adding live provider calls to CI.
- `sigma.WithStructuredOutput`, `sigma.WithJSONOutput`,
  `sigma.WithJSONSchemaOutput`, and `sigma.WithTopLogprobs` now provide
  provider-neutral request controls that map onto existing OpenAI-compatible,
  Anthropic Messages, and Bedrock Converse structured-output paths with local
  validation for unsupported APIs.
- `sigma.TransformRequestForModel` and `sigma.TransformMessagesForModel` now
  expose opt-in cross-provider handoff helpers that adapt replayed messages for
  a target model, including thinking-to-text conversion, unsupported-image
  replacement options, tool-history repair, unanswered tool-call cleanup, and
  explicit capability-loss reports.
- Handoff and replay normalization now preserve ordinary tool-result-to-assistant
  loops, bridge only tool-result-to-user transitions for OpenAI-compatible
  targets that require it, synthesize explicit error tool results for missing
  tool outputs, preserve provider-native thinking only for exact-model replay,
  and normalize replayed tool-call IDs for stricter Anthropic and Bedrock
  targets.
- Assistant messages and content blocks now expose provider-neutral source and
  citation accessors, letting callers read normalized URLs, URIs, titles,
  offsets, cited text, and copied provider metadata without scraping opaque
  provider metadata maps directly.
- Assistant messages now expose a provider-neutral `ResponseID` accessor over
  existing text-generation response metadata, letting callers read provider
  response IDs without scraping opaque provider metadata maps directly.
- Assistant messages now expose a provider-neutral `ResponseModel` accessor
  over existing text-generation response metadata, letting callers read routed
  provider model IDs without scraping opaque provider metadata maps directly.
- `sigma.ValidateToolCall` now strictly evaluates `anyOf`, `oneOf`, `allOf`,
  `pattern`, and `not` in tool input schemas, including nested property, array
  item, and additional property schemas, so invalid composed or constrained
  tool arguments are rejected before tool execution.
- `sigma.ValidateToolCall` now also resolves local JSON Pointer `$ref` values,
  including recursive definitions, evaluates `if`/`then`/`else`, and strictly
  validates common date, time, email, URI, UUID, hostname, and IP formats
  without adding dependencies or fetching external schemas.
- `sigma.ValidateToolCallWithOptions` now lets callers opt into primitive tool
  argument coercion on decoded argument copies before strict validation, while
  leaving `ValidateToolCall` strict by default.
- Deterministic provider tests now cover Google stream `thoughtSignature`
  replay on signature-only chunks and OpenAI-compatible Chat Completions replay
  omission of prior private thinking blocks when `reasoning_content` is not
  required.
- OpenAI-compatible Chat Completions streams now preserve provider
  `reasoning_details` metadata on tool-call blocks and replay it with
  assistant tool-call history.
- Deterministic request-conversion tests now lock OpenAI Responses replay IDs,
  OpenAI-compatible Chat Completions request-shape guardrails, routed stream
  model metadata, and Google legacy tool-schema sanitization without changing
  provider APIs.

### Changed

- OpenRouter image-generation registration helpers now use explicit image names:
  `openrouter.RegisterImages`, `openrouter.RegisterImagesDefault`, and
  `openrouter.NewImagesProvider`, leaving `openrouter.Register` and
  `openrouter.NewProvider` for the text Chat Completions provider.

### Fixed

- GitHub Copilot `mai-code-1-flash-picker` now routes through the Responses
  endpoint instead of Chat Completions.
- Stored Cloudflare API-key credentials now fill missing account and gateway
  routing values from the matching environment variables while preserving
  stored and request-scoped values.

- OpenAI Codex Responses WebSocket transport now retries once when the backend
  reports a connection limit before output begins; a repeated limit response or
  any other pre-output failure retains the existing SSE fallback.
- OpenAI Codex Responses WebSocket reads now reject frames and fragmented
  messages larger than 16 MiB before oversized payload allocation, and canceled
  writes now close promptly without making `Close` wait behind blocked I/O.
- Canceling the context passed to `Collect` or `CollectImages` now aborts a live
  stream even when its creation context remains active, preserving partial text
  or images in the aborted final result while leaving intentional `Close`
  behavior unchanged.
- Shared HTTP retries now close retryable response bodies without reading them,
  and oversized numeric retry delays saturate so configured maximum-delay checks
  reject them. Amazon Bedrock Converse errors now read at most 4 KiB before
  closing the response body.
- Amazon Bedrock Converse now treats bare API-key credentials as bearer tokens,
  so request-scoped and stored API keys no longer fail as incomplete SigV4
  credentials. API-key credentials with AWS access-key metadata remain signed.
- Oversized-input embedding reconstruction now rejects split or cached vectors
  whose dimensions differ, rather than returning a partial weighted average.
- Dynamic text, image, and embedding model refreshes now detect source
  replacement during an in-flight fetch and return a conflict without applying
  stale models; cloned registries preserve the source revision sequence.
- Registry model copies now deep-copy nested provider metadata containers, and
  opt-in primitive tool-argument coercion now preserves already-valid `anyOf`
  and `oneOf` values instead of coercing them toward the first matching branch.
- Amazon Bedrock SigV4 signing now canonicalizes the escaped wire path for
  model IDs with encoded slash segments, so inference-profile ARNs sign
  consistently across Converse Stream and Bedrock embeddings.
- Anthropic Messages prompt-cache markers are now bounded to the system prompt,
  final cacheable user-side block, and final tool definition, avoiding
  API-rejected payloads when cache-enabled agent loops include multiple user
  turns, tool results, or tools.
- Anthropic Messages now clamps thinking budgets inside `max_tokens`, falls
  back from reasoning levels to budget thinking for non-adaptive Claude routes,
  appends split thinking signatures, forwards only supported `metadata.user_id`,
  and degrades unsupported long-cache retention to normal ephemeral caching.
- Google Gemini and Vertex streams now classify malformed or unexpected
  function-call finish reasons as provider errors, Google replay omits empty
  assistant/model blocks, and Gemini image generation rejects unsupported
  multi-image requests locally instead of sending `numberOfImages`.
- Bedrock Converse now appends split reasoning signatures, replays redacted
  reasoning with `redactedContent`, classifies event-stream exception types
  such as throttling, resolves stdlib default-chain credentials from profiles,
  ECS, web identity, and IMDS, and recognizes Claude 5-family thinking/cache
  compatibility.
- Built-in Anthropic Messages routes now use versioned base URLs for Anthropic,
  Vercel AI Gateway, GitHub Copilot, and Cloudflare AI Gateway metadata and
  wrapper defaults, so Sigma dispatches to `/v1/messages`-shaped endpoints
  instead of provider-root `/messages` paths.
- Mistral Conversations no longer emits Chat Completions-only request fields on
  `/v1/conversations`: image chunks use `image_url`, function results omit
  `name` and `is_error`, native Magistral `prompt_mode` is top-level, and typed
  named-tool choices are rejected locally because Conversations only accepts
  `auto`, `none`, `any`, or `required`.
- OpenAI Responses streams now require a terminal provider response event before
  treating EOF as success, preserve partial content on premature EOF errors, and
  finalize terminal incomplete responses as max-token stops with usage intact.
- OpenAI and Azure OpenAI Responses no longer send cache-affinity
  `sigma.WithSessionID` values as `previous_response_id`; callers can still pass
  real `resp_*` continuation IDs through explicit provider options.
- OpenAI-compatible Chat Completions streams now surface provider
  `finish_reason` values of `network_error` and `model_context_window_exceeded`
  as errors instead of successful unknown stops, preserving context-overflow
  classification for the latter.
- OpenAI-compatible Chat Completions streams now use the first non-empty
  reasoning alias from each delta and require a terminal `finish_reason` before
  treating stream EOF as a successful completion.
- OpenAI-compatible Chat Completions streams now keep index-less tool-call
  deltas separated by provider ID, buffer encrypted `reasoning_details` until
  finalization, stop replaying provider-private thinking as visible assistant
  text by default, and avoid direct OpenAI message-level `cache_control`.
- Azure OpenAI Responses endpoint normalization now preserves conventional
  `/openai/v1` endpoint values without appending duplicate path segments.
- GitHub Copilot OAuth refresh now honors the stored credential enterprise
  domain before falling back to provider options.
- Shared diagnostic redaction now treats Google API-key headers and Cloudflare
  AI Gateway auth headers as credential-bearing headers, so debug hooks redact
  those values even when they do not match known token patterns.
- Core text and image stream cancellation now records aborted final results
  before closing, so `Collect`, `Complete`, and `CollectImages` preserve
  partial outputs with typed aborted errors and canceled abandoned streams close
  without waiting for callers to drain events.

### Reviewed

- Reviewed Sigma's primary surfaces (client/registry dispatch, Request shapes,
  streaming events with block lifecycle and partial tool calls, Usage/Cost with
  provider identity and long-cache splits, auth and OAuth helpers, persistence,
  internal message transforms, embeddings/retrieval, image generation, model
  metadata, and provider adapters) for matching capabilities. Identified
  user-visible gaps (durable credential storage with atomic modify semantics,
  runtime/dynamic model list refresh for custom sources, and public
  cross-provider handoff with message adaptation plus explicit capability-loss
  reporting) that align with items already tracked in TODO.md. The public
  handoff slice now ships as opt-in helpers, and runtime text model refresh now
  supports app-owned sources; durable credential storage and non-text/live
  model discovery remain deferred.

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
  required, disabled, and any-tool selection while preserving raw provider
  options for advanced request fields.
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
- OAuth disk and keychain persistence is caller-owned. OpenAI Codex Responses
  includes browser callback login, device-code login, refresh helpers, and a
  helper for writing login results to caller-supplied credential stores.
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
- Full AWS SDK-equivalent Bedrock credential-chain behavior, including SSO,
  broader regional alias expansion beyond focused EU Anthropic inference-profile
  metadata, and live Bedrock CI coverage remain deferred.
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
