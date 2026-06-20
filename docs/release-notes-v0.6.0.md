# Release notes: sigma v0.6.0

This is the maintainer-facing development note for the next `sigma` tag. Add
the v0.6.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.6.0 opens with stronger provider usage and cost accounting for text
generation, including long prompt-cache write splits, raw provider usage
payloads for diagnostics, standalone provider/model identity on usage records,
and a clear split between provider-reported cost and Sigma's model-metadata
cost estimate. Mistral Conversations now also maps cache-enabled session IDs to
prompt-cache keys and reports provider cached prompt tokens as cache reads. It
also adds caller-owned GitHub Copilot OAuth helpers for
device-code login, token refresh, request-time credential resolution, and
explicit model-policy enablement. Codex Responses WebSocket transport now also
honors standard HTTP(S) proxy environment variables with `NO_PROXY` exclusions
while keeping the existing SSE fallback, and now exposes a Codex-specific
connect timeout plus session-cache debug stats for connection reuse, cached
context deltas, and fallback diagnostics. Kimi Coding is promoted as a focused
Anthropic-compatible provider slice with generated metadata, credential
discovery, request headers, adaptive thinking metadata, and session-affinity
support. Xiaomi is promoted as a focused OpenAI-compatible provider slice with
API-billing and regional token-plan registration helpers, generated MiMo
metadata, and regional API-key discovery. The environment credential resolver
also exposes non-secret discovery helpers so applications can inspect candidate
and configured API-key variable names before making a request, and focused
provider helpers now let callers pass Cloudflare AI Gateway placeholder values
and Bedrock region/static credential values without mutating process
environment. Cloudflare Workers AI is also promoted as a direct
OpenAI-compatible Chat Completions wrapper with account placeholder resolution
and normal bearer-token auth for the direct Workers AI endpoint. NVIDIA NIM is
also promoted as a focused OpenAI-compatible text and embedding wrapper with
generated metadata, direct NIM base URL defaults, request header metadata, and
NVIDIA-specific embedding input-type mapping. Moonshot AI and Moonshot AI CN
are promoted as focused OpenAI-compatible Chat Completions wrappers with
generated K2.7 Code CN and HighSpeed metadata plus
metadata-driven handling for K2.7 routes that reject explicit disabled thinking
payloads. Z.ai and Z.ai Coding CN are also promoted as focused
OpenAI-compatible Chat Completions
wrappers with generated GLM metadata, GLM-5.2 reasoning-effort mapping, and
deterministic registration and request coverage. The surface probe command adds
a credential-gated cross-provider handoff diagnostic for replaying small
tool-call contexts across selected live routes without moving live provider
calls into CI. Assistant results now also expose provider-neutral source and
citation accessors for the source metadata Sigma already captures from grounded
and citation-bearing responses. Local tool-call validation also now evaluates
composed JSON Schema branches so callers can reject invalid model-emitted
arguments before running tools. The deterministic provider test suite now also
locks Google stream `thoughtSignature` attachment, OpenAI-compatible Chat
Completions thinking-block replay behavior, and OpenAI-compatible stream error
finish handling, plus request-conversion guardrails for replay IDs, Chat
Completions payload shape, routed model metadata, and Google legacy tool-schema
sanitization.

## Added

- Anthropic Messages usage now populates
  `sigma.Usage.LongCacheWriteInputTokens` from long prompt-cache write usage
  and `sigma.CostForUsage` prices those writes at the long-cache input
  multiplier while preserving total cache-write token accounting.
- Text-generation usage now populates provider/model identity on
  `sigma.Usage`, preserves a JSON-like `Usage.Raw` copy of provider usage
  payloads when providers report usage, normalizes provider tool/connector
  token counts into `Usage.ToolUseInputTokens`, and exposes provider-reported
  cost separately from Sigma's estimated `Cost.TotalCost`.
- Mistral Conversations now maps cache-enabled `sigma.WithSessionID` requests
  to `prompt_cache_key` and `x-affinity`, and streamed provider cached prompt
  token fields now populate `Usage.CacheReadInputTokens` while preserving raw
  usage payloads for diagnostics.
- GitHub Copilot now has stdlib-only device-code OAuth login through
  `githubcopilot.LoginGitHubCopilotDeviceCode`, Copilot token refresh through
  `githubcopilot.RefreshGitHubCopilotToken`, and an in-memory token provider
  from `githubcopilot.NewGitHubCopilotOAuthTokenProvider` that can be used as a
  Sigma auth resolver.
- GitHub Copilot model policies can now be enabled explicitly with
  `githubcopilot.EnableGitHubCopilotModel` and
  `githubcopilot.EnableGitHubCopilotModels`, which report per-model success or
  failure without making model enablement an automatic login side effect.
- OpenAI Codex Responses WebSocket transport now resolves `HTTP_PROXY`,
  `HTTPS_PROXY`, lowercase aliases, and `ALL_PROXY` for `ws://` and `wss://`
  endpoints, respects `NO_PROXY`, and tunnels through HTTP/HTTPS `CONNECT`
  proxies before running the existing WebSocket handshake.
- OpenAI Codex Responses WebSocket transport now honors
  `OpenAIOptions.CodexWebSocketConnectTimeout` for the connection and handshake
  phase, defaults that cap to 15 seconds, treats zero as disabled, and records
  per-session debug stats for created/reused WebSocket connections, full and
  delta context requests, previous response IDs, WebSocket failures, and SSE
  fallback activation.
- Kimi Coding can now be registered with `kimi.RegisterCoding` or
  `kimi.RegisterCodingDefault`, using the shared Anthropic-compatible Messages
  adapter with Kimi Coding base URL defaults, Kimi CLI request headers,
  `KIMI_API_KEY` credential discovery, and generated metadata for `k2p7`,
  `kimi-for-coding`, and `kimi-k2-thinking`.
- Xiaomi can now be registered with `xiaomi.Register`,
  `xiaomi.RegisterTokenPlanCN`, `xiaomi.RegisterTokenPlanAMS`, or
  `xiaomi.RegisterTokenPlanSGP`, using the shared OpenAI-compatible Chat
  Completions adapter with API-billing and regional token-plan base URL
  defaults, regional API-key discovery, generated MiMo metadata, and
  DeepSeek-style reasoning replay compatibility.
- `sigma.EnvironmentAuthResolver` now has `EnvVars` and `ConfiguredEnvVars`
  helpers for model-aware environment credential discovery. They return ordered
  variable names only, respect model metadata before provider defaults, and add
  built-in fallback names for additional OpenAI-compatible provider IDs.
- `cloudflare.WithAIGatewayAccountID` and `cloudflare.WithAIGatewayID` now
  provide request-scoped Cloudflare AI Gateway placeholder values before the
  existing `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_GATEWAY_ID` environment
  fallback.
- Cloudflare Workers AI can now be registered with
  `cloudflare.RegisterWorkersAI` or `cloudflare.RegisterDefaultWorkersAI`,
  using the shared OpenAI-compatible Chat Completions adapter with the direct
  Workers AI base URL, `CLOUDFLARE_API_KEY` credential discovery,
  request-scoped `cloudflare.WithWorkersAIAccountID`, and
  `CLOUDFLARE_ACCOUNT_ID` environment fallback.
- NVIDIA NIM can now be registered with `nvidia.Register` for
  OpenAI-compatible Chat Completions and `nvidia.RegisterEmbeddings` for
  OpenAI-compatible embeddings, using direct NIM base URL defaults,
  `NVIDIA_API_KEY` credential discovery, generated text and embedding
  metadata, and deterministic request coverage.
- NVIDIA NIM embeddings now map `sigma.EmbeddingInputTypeQuery` to
  `input_type: "query"` and `sigma.EmbeddingInputTypeDocument` to
  `input_type: "passage"` unless callers set
  `EmbeddingRequest.ProviderMetadata["input_type"]` explicitly.
- Moonshot AI can now be registered with `moonshot.Register` or
  `moonshot.RegisterDefault`, and Moonshot AI CN can be registered with
  `moonshot.RegisterCN` or `moonshot.RegisterDefaultCN`, using the shared
  OpenAI-compatible Chat Completions adapter with direct base URL defaults and
  `MOONSHOT_API_KEY` credential discovery.
- Generated Moonshot metadata now includes Moonshot AI CN Kimi K2.7 Code and
  Kimi K2.7 Code HighSpeed rows for both direct Moonshot routes. K2.7 Code
  metadata marks explicit disabled thinking as unsupported, so default
  requests omit the disabled-thinking payload while explicit reasoning levels
  still enable thinking.
- Z.ai can now be registered with `zai.Register` or `zai.RegisterDefault`, and
  Z.ai Coding CN can be registered with `zai.RegisterCodingCN` or
  `zai.RegisterDefaultCodingCN`, using the shared OpenAI-compatible Chat
  Completions adapter with direct base URL defaults and `ZAI_API_KEY` /
  `ZAI_CODING_CN_API_KEY` credential discovery.
- Generated Z.ai and Z.ai Coding CN metadata now includes `glm-5.2` with
  GLM-family metadata, `tool_stream` support, and provider-specific reasoning
  effort mapping that can enable thinking while omitting `reasoning_effort` for
  minimal reasoning.
- `bedrock.WithRequestRegion` and `bedrock.WithRequestStaticCredentials` now
  provide request-scoped Bedrock runtime region and static AWS credential values
  before the existing AWS region and static credential environment fallbacks.
- `cmd/sigma-surface-probe -handoff` now builds a small tool-call context for
  each selected live route/model and replays it pairwise into the other selected
  routes, emitting JSONL diagnostics with `sourceRoute` and `sourceModel` so
  replay failures can be attributed without making handoff checks part of CI.
- `sigma.AssistantMessage.Sources`, `sigma.ContentBlock.Citations`, and
  `sigma.AssistantMessage.Citations` now expose normalized source and citation
  entries from provider metadata, including URLs, URIs, titles, offsets, cited
  text, and copied provider metadata for provider-specific details.
- `sigma.ValidateToolCall` now evaluates `anyOf`, `oneOf`, and `allOf` in tool
  input schemas, including nested property, array item, and additional property
  schemas, while preserving decoded-copy results and redacted validation errors.
- Deterministic provider tests now cover Google stream `thoughtSignature`-only
  chunks, empty signature deltas, signature updates on existing blocks, and
  OpenAI-compatible Chat Completions replay of prior thinking blocks as
  assistant text when `reasoning_content` is not required.
- OpenAI-compatible Chat Completions streams now surface provider
  `finish_reason` values of `network_error` and `model_context_window_exceeded`
  as errors instead of successful unknown stops, including context-overflow
  classification for `model_context_window_exceeded`.
- Deterministic request-conversion tests now cover distinct OpenAI Responses
  replay IDs around reasoning items, OpenAI-compatible Chat Completions
  tool/max-token payload guardrails, provider-reported routed stream model
  metadata, and Google legacy tool-schema sanitization without adding live
  provider calls.

## Compatibility

- `sigma.Usage.LongCacheWriteInputTokens` is additive metadata for cost
  accounting. Existing `CacheWriteInputTokens` values remain the total cache
  write count, so callers that ignore the long-cache split keep the same token
  totals.
- Usage remains optional: `AssistantMessage.Usage == nil` and terminal
  `Event.Usage == nil` still mean no usage was supplied, while a non-nil
  zero-valued usage means the provider explicitly reported zero values.
- Existing `sigma.Cost` component fields and `TotalCost` remain Sigma's
  estimated cost from model metadata. Provider-reported cost is additive and is
  only populated when an upstream payload contains a clear numeric cost field.
- Mistral prompt-cache behavior is additive and uses existing
  `sigma.WithSessionID` and `sigma.WithCacheRetention` options. Empty/default,
  short, long, and persistent retention enable the Mistral cache key; explicit
  `CacheRetentionNone` suppresses automatic `prompt_cache_key` and `x-affinity`
  values.
- GitHub Copilot OAuth credentials remain caller-owned. Sigma does not persist
  tokens, does not automatically enable model policies after login, and does
  not change the existing GitHub Copilot request dispatch path.
- Codex WebSocket proxy support is additive and environment-driven. It applies
  only to the Codex Responses WebSocket transport; SSE/HTTP requests continue
  to use their existing HTTP client paths, and unsupported proxy protocols
  still fall back before streaming starts.
- Codex WebSocket connect timeout and stats are additive and Codex-specific.
  `sigma.WithTimeout` still controls the overall request context; the new
  connect timeout only bounds WebSocket dial, proxy, TLS, and handshake work
  before any stream event starts. Stats reset helpers clear counters without
  closing cached sessions.
- Kimi Coding is additive: the existing `kimi` metadata row remains available,
  and broader router or regional endpoint catalog expansion stays deferred.
- Xiaomi token-plan support is additive and uses distinct provider IDs for the
  CN, AMS, and SGP regional OpenAI-compatible routes. The API-billing
  `ProviderXiaomi` rows remain available, and `mimo-v2-flash` remains scoped to
  the API-billing provider rather than the token-plan providers.
- Environment credential discovery is additive and non-secret. `Resolve`
  remains the API that returns credential values, and the new helper methods do
  not probe ambient cloud credentials or OAuth token stores.
- Cloudflare and Bedrock request configuration helpers are provider-specific
  request options. They do not add a root-level environment override map, and
  existing process environment fallback behavior remains available.
- Cloudflare Workers AI direct routing is additive and Chat Completions-only in
  this release. It uses normal bearer-token auth, while Cloudflare AI Gateway
  routes continue to use `cf-aig-authorization`.
- NVIDIA NIM direct routing is additive and uses Sigma's existing
  OpenAI-compatible text and embedding adapters through a provider-specific
  wrapper. Endpoint overrides remain explicit through provider options or model
  metadata; Sigma does not read `NVIDIA_BASE_URL`.
- Moonshot direct routing is additive and Chat Completions-only in this
  release. The wrappers reuse the shared OpenAI-compatible adapter; broader
  live-provider coverage remains deferred until route-specific behavior needs
  independent fixtures.
- Z.ai direct routing is additive and Chat Completions-only in this release. The
  Z.ai and Z.ai Coding CN wrappers reuse the shared OpenAI-compatible adapter;
  broader live-provider coverage remains deferred until route-specific behavior
  needs independent fixtures.
- Bedrock credential precedence remains explicit: typed bearer-token options
  and auth resolvers run before request static credentials, and request static
  credentials run before the existing static environment credential path.
- Source and citation accessors are additive views over existing provider
  metadata. They do not change persisted request shape, replay behavior,
  provider dispatch, or the raw `ProviderMetadata` maps.
- Tool schema composition validation is additive and stricter for previously
  unchecked composed branches. It does not add primitive coercion or change the
  `ValidateToolCall` API.
- The new replay and stream regression tests do not change public APIs,
  provider request shapes, or persisted replay semantics.
- The request-conversion regression tests are coverage-only. They preserve
  existing public APIs and keep request-shape behavior unchanged for callers.

## Deferred work

- OAuth token persistence remains deferred and caller-owned. Deferred work
  continues to be tracked in [TODO.md](../TODO.md).
- Ambient cloud credential probing, OAuth token stores, AWS profiles, SSO, web
  identity, IMDS, and shared-config loading remain deferred. Applications that
  need those flows should continue to resolve credentials before calling Sigma.
- Billing reconciliation, subscription analytics, and UI presentation of usage
  totals remain caller-owned. Sigma normalizes and preserves provider data but
  does not claim invoice-grade billing accuracy.
- Cross-provider handoff remains a diagnostic probe, not a public orchestration
  runtime. Full context handoff APIs and capability-loss reporting remain
  deferred.
- Non-Codex WebSocket transport support remains deferred until route-specific
  wire protocols have deterministic fixtures. The Codex preview transport now
  has request contexts, explicit session cleanup helpers, standard HTTP(S)
  proxy environment variables, a connect timeout, session-cache debug stats,
  and SSE fallback.
- Xiaomi Anthropic-compatible token-plan routes remain deferred until they have
  separate provider IDs, compatibility metadata, and deterministic replay
  fixtures.
- Cloudflare Workers AI Responses, Anthropic-compatible, image, embedding, and
  live validation routes remain deferred until each surface has deterministic
  request, stream, error, and metadata evidence.
- Broader NVIDIA NIM live validation, embedding catalog expansion, and any
  route behavior beyond the shared OpenAI-compatible adapters remain deferred
  until each surface has deterministic request, response, error, and metadata
  evidence.
- Moonshot live-provider expansion beyond the focused direct Chat Completions
  wrapper and reviewed K2.7 metadata remains deferred until route-specific
  behavior needs deterministic evidence.
- Z.ai Anthropic-compatible, image, embedding, and broader live validation
  routes remain deferred until each surface has deterministic request, stream,
  error, and metadata evidence.
- Provider-neutral document/PDF content blocks, source ranking, citation
  rendering, and provider-specific citation UI policy remain deferred and
  caller-owned.
- Mistral URL/file image references, built-in connector tools, append/restart
  lifecycle operations, and broad catalog expansion remain deferred.
- Full JSON Schema runtime support, including `$ref`, `pattern`, formats,
  `not`, conditionals, and implicit argument coercion, remains deferred.

## Validation status

Current v0.6.0 development state validated on 2026-06-20 with:

- `mise run go:generate`.
- `mise run go:fmt`.
- `mise run go:fmt:check`.
- `mise run go:test`.
- `mise run go:vet`.
- `mise run go:lint`.
- `mise run go:race`.
- `mise run ci`.
- `git diff --check`.
