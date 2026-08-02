# Release notes: sigma v0.7.0

This is the maintainer-facing development note for the next `sigma` tag. Add
the v0.7.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.7.0 hardens existing provider protocol compatibility and
caller-directed stream recovery, including reliable sessionless Codex WebSocket
request IDs and corrected Codex GPT-5.6 context limits. It refreshes the Kimi
Coding, Fireworks, and selected OpenCode Go catalogs, adds focused xAI OpenAI
Responses registration and caller-configured device-code OAuth surfaces, and
adds Kimi Coding subscription device-code OAuth alongside a dynamic Radius
gateway text provider with caller-configured browser and device-code OAuth plus
NVIDIA Nemotron 3 Ultra to the existing Fireworks text routes. It also adds
Kimi K3 Chat Completions compatibility for native reasoning effort, replay,
cache affinity, and deferred client tools on its existing Fireworks route, and
direct Qwen Token Plan registrations for international and China regional
endpoints with a focused Qwen3.7 Max and Qwen3.8 Max Preview catalog. OpenAI
Responses and Chat Completions also gain grammar-constrained custom tools with
reviewed capability metadata and explicit compatible-endpoint opt-in or
opt-out. It
also adds opt-in OpenRouter browser PKCE login with caller-owned permanent-key
storage and a manual remote-browser fallback for the existing text and image
routes. Radius gateway catalogs can also be stored through caller-owned
snapshots and restored explicitly without a gateway request. Anthropic Messages
environment credential discovery now supports bearer gateway tokens alongside
OAuth-token and API-key fallbacks. Mistral Conversations also adds
server-executed retrieval tools with normalized source and citation results,
preserves per-tool strict JSON Schema settings in function definitions, and
fails safely for explicit error or unknown terminal stop reasons.
Generated Claude Opus 5 metadata now covers direct Anthropic, the global Amazon
Bedrock inference-profile route, and GitHub Copilot's Anthropic Messages route
with adaptive thinking, current limits, and pricing metadata.
Cached Codex WebSocket continuations that are no longer recognised now retry
once with the full request context before existing fallback behavior applies.
Cached Codex WebSocket connections and continuation state are also isolated by
authenticated account when callers reuse a session ID.
OpenAI Responses routes now also retain non-empty raw terminal response status
in assistant provider metadata for diagnostics.
OpenAI-compatible Chat Completions routes likewise retain non-empty raw
terminal `finish_reason` values in assistant provider metadata.
Anthropic Messages, Google Gemini and Vertex AI, and Amazon Bedrock Converse
streams now also retain their non-empty raw terminal reasons in assistant
provider metadata for diagnostics.
Explicit `CacheRetentionNone` requests on reviewed direct OpenAI GPT-5.6
Responses models now disable implicit prompt-cache writes through capability-
gated explicit cache mode.
The existing OpenRouter image adapter now also exposes Krea 2 Large, Medium,
and Medium Turbo, MAI-Image 2.5 Pro, and Auto Router Beta through generated
model metadata. Text callers can now also select an HTTP client for an
individual HTTP/SSE request. Package-level dispatch and routing now avoid
full-catalog clones, registry reads no longer take initialization write locks,
and embedding batches use bounded parallel workers for both configured limits
and retry splits. Google Generative AI, Mistral Conversations, and Bedrock
Converse Stream also reject missing terminal markers with retryable transient
errors and partial finals. Codex session cleanup now clears all session state,
while abandoned debug and fallback state expires after five idle minutes.
The surface-probe command now also has an opt-in native Vertex Gemini text
route that reuses built-in model capabilities, accepts caller-supplied OAuth
access tokens or API keys, and keeps live provider calls outside CI. Gemini 2.5
reasoning requests now use supported token budgets across direct Google and
native Vertex routes, while probe cases honor models that do not support
disabled thinking. The native Vertex text catalog now adds Gemini 3.6 Flash and
Gemini 3.5 Flash-Lite, removes retired or superseded Gemini 1.5, Gemini 2.0,
Gemini 2.5 Flash-Lite Preview 09-2025, and Gemini 3 Pro Preview rows, and
supports medium thinking on both Gemini 3.1 Pro Preview endpoints. The Flash
and Flash-Lite latest aliases remain available for text, image, and function
tools without advertising named thinking levels rejected by those endpoints.
When a surface-probe repair confirms minimal-text availability after another
case fails, the original failure classification is now retained and the
availability result is reported separately.

## Changed

- Package-level model lookup, routing, generation, image, and embedding helpers
  now use the live shared default registry without cloning the generated catalog
  on every call. Public registry clones and explicitly constructed clients
  remain isolated, and registry read methods no longer acquire initialization
  write locks.
- `Client.EmbedBatch` now runs both configured-limit groups and retry-generated
  splits through workers bounded by `MaxParallelBatches`. Fatal branch errors
  cancel siblings without masking the original failure, and returned vectors
  remain ordered by the original inputs.
- Reviewed direct OpenAI GPT-5.6 Responses models now send explicit
  prompt-cache mode for explicit `CacheRetentionNone` requests, preventing
  implicit prompt-cache writes without changing unset retention or unmarked
  model payloads.
- OpenAI Responses routes now retain non-empty `completed`, `incomplete`, or
  `failed` terminal status values in assistant provider metadata. Transient
  response statuses are not retained when the terminal event omits status.
- OpenAI-compatible Chat Completions routes now retain each non-empty terminal
  `finish_reason` in assistant provider metadata, including values that
  normalize to provider errors or unknown stop reasons.
- Anthropic Messages streams now retain terminal reasons as `stop_reason`,
  Google Gemini API and Vertex AI streams retain them as `finishReason`, and
  Amazon Bedrock Converse streams retain them as `stopReason` in assistant
  provider metadata. Recognized and provider-specific reasons use the same
  normalized stop and error behavior as before.
- Text `Stream` and `Complete` calls now accept `WithRequestHTTPClient` for a
  caller-selected HTTP/SSE transport client.
- Anthropic Messages now resolves `ANTHROPIC_AUTH_TOKEN` as bearer
  authentication, followed by `ANTHROPIC_OAUTH_TOKEN` and `ANTHROPIC_API_KEY`.
  Gateway tokens use `Authorization` without an `X-Api-Key` or Claude Code
  identity headers.
- Generated Claude Opus 5 metadata now covers direct Anthropic, the global
  Amazon Bedrock inference-profile route, and GitHub Copilot's Anthropic
  Messages route with text/image input, tools, adaptive thinking, 1M context,
  128K output, and reviewed cache pricing.
- Mistral Conversations now supports typed named function selection with native
  tool-choice objects.
- Mistral Conversations now preserves existing boolean per-tool strict JSON
  Schema settings when serializing function definitions.
- Mistral Conversations retains each non-empty terminal `stop_reason` in
  assistant provider metadata; explicit error and unrecognized terminal values
  now return typed provider failures with sanitized diagnostics.
- Mistral Conversations now supports opt-in server-executed web search, premium
  web search, and document-library tools. Returned tool execution diagnostics,
  source references, and citations remain available through existing Sigma
  provider metadata and result accessors.
- Codex request-affinity headers now limit session IDs to 64 characters while
  preserving local session resource management. Sessionless WebSocket
  handshakes now use monotonic UUIDv7 request IDs, and GPT-5.6 Codex models use
  their 272K context limit so unavailable long-context budgets and price tiers
  are not selected. OpenRouter uses its native cache-affinity header, and
  Bedrock terminal responses with unrecognised stop reasons now surface typed
  provider errors.
- Cached Codex WebSocket continuations rejected before output now retry once
  with the full request context. Repeated rejections preserve the existing SSE
  fallback behavior.
- Cached Codex WebSocket connections and their continuation state are now
  scoped by authenticated account as well as caller session ID, preventing an
  account change from reusing another account's live connection.
- Grok 4.5 now uses the xAI OpenAI Responses route with low, medium, and high
  reasoning levels. Long-lived prompt-cache retention is omitted for that route
  while cache keys and session affinity remain available.
- xAI now supports caller-configured device-code OAuth login, token refresh,
  and opt-in provider-auth registration for its existing text routes. Token
  persistence remains owned by the application.
- Kimi Coding now includes K3 and Kimi For Coding HighSpeed with current
  context, output, image-input, tool, reasoning, and estimated cost metadata.
  K3 supports `low`, `high`, and `max` reasoning levels, while K3 and Kimi For
  Coding preserve empty thinking signatures during replay. The stale `k2p7`
  catalog row is no longer included.
- Kimi Coding subscriptions can now use opt-in device-code OAuth login, token
  refresh, in-memory credential resolution, and provider-auth registration.
  Tokens use the existing Messages bearer-auth path while applications retain
  persistence ownership.
- OpenRouter now supports opt-in browser PKCE login that returns a permanent
  API key. Applications can place that key in a caller-supplied CredentialStore
  for existing text and image requests; Sigma does not add a persistence
  backend or refresh lifecycle. Applications can also provide a manual fallback
  that accepts a pasted final redirect URL or authorization code when the
  browser cannot reach Sigma's loopback callback.
- Generated OpenRouter image metadata now includes Krea 2 Large, Medium, and
  Medium Turbo, MAI-Image 2.5 Pro, and Auto Router Beta through the existing
  image adapter.
- OpenCode Go routes Grok 4.5 through OpenAI Responses and Kimi K3 through
  Chat Completions, with reviewed text/image, tool, reasoning, context,
  output, and pricing metadata.
- Curated Fireworks Chat Completions and Messages models now include verified
  standard-serverless input, cached-input, and output pricing. Deterministic
  Messages coverage also protects cache-affinity headers and omitted unsupported
  tool fields.
- Fireworks Kimi K3 now uses native Chat Completions reasoning effort including
  `max`, preserves required empty reasoning replay, sends cache-affinity
  headers while omitting unsupported long-lived cache retention, and loads
  client tools from existing `AddedToolNames` markers when needed.
- NVIDIA Nemotron 3 Ultra NVFP4 is now available on the existing Fireworks Chat
  Completions and Anthropic-compatible Messages routes with text-only input,
  tool and reasoning support, current limits, and standard serverless pricing.
- Premature OpenAI Responses and Anthropic Messages terminal-event gaps now
  surface as transient, retryable failures while preserving partial finals.
  Sigma does not re-dispatch a stream after its body begins; applications own
  retry and fallback decisions.
- Google Generative AI, Mistral Conversations, and Bedrock Converse Stream now
  likewise require their finish-reason, response-done, and message-stop markers
  before accepting a clean transport ending. Premature endings retain partial
  finals and are classified as transient and retryable without automatic
  post-body redispatch.
- Codex WebSocket cleanup now removes every account-scoped connection plus
  continuation, SSE fallback, debug-stat, and timer state for the requested
  session. State without a cached connection expires after the existing
  five-minute idle window, and activity refreshes that expiry.
- Radius gateway models now refresh explicitly from the gateway at runtime and
  use its native text streaming protocol with image, thinking, function-tool,
  usage, and response-ID handling. There is no static Radius catalog.
- Radius gateway can now write validated model snapshots to caller-owned
  storage and restore them explicitly without contacting the gateway. Normal
  catalog refreshes remain network-backed.
- Radius gateway now supports opt-in browser PKCE and device-code OAuth login,
  refresh, in-memory or stored credential resolution, and OAuth-authenticated
  catalog refresh. Applications provide the OAuth client and retain token
  persistence ownership.
- Qwen Token Plan now has international and China regional Chat Completions
  registrations with the matching environment API-key fallbacks. The focused
  catalog includes text-only Qwen3.7 Max plus text/image Qwen3.8 Max Preview,
  both with tool and Qwen thinking compatibility metadata.
- OpenAI Responses and Chat Completions grammar-constrained custom tools now
  accept typed Lark or regex definitions when their JSON Schema has one
  required string input. Reviewed compatibility metadata enables the native
  custom-tool payload; callers may explicitly enable or disable it for
  compatible endpoints. Assistant and tool-result replay plus streamed custom
  input continue to use Sigma's normal tool-call surface.
- `cmd/sigma-surface-probe` now supports opt-in native Vertex Gemini text
  diagnostics. Omitted model selection walks the sorted built-in
  `google-vertex` text catalog, explicit model IDs are validated locally, and
  image, function-tool, disabled-thinking, and reasoning-level cases follow
  generated model capabilities. Project, location, and caller-supplied OAuth
  access tokens or API keys are passed through existing provider options and
  authentication surfaces without a Vertex model-discovery request.
- Generated Google and native Vertex Gemini 2.5 metadata now maps Sigma's
  minimal, low, medium, and high reasoning levels to provider token budgets.
  Gemini 2.5 Pro marks disabled thinking unsupported, and the native Vertex
  probe omits disabled-thinking cases when `off` is not supported.
- Generated native Vertex Gemini text metadata now includes
  `gemini-3.6-flash` and `gemini-3.5-flash-lite` with reviewed text/image,
  function-tool, thinking, limit, and standard global pricing metadata. Both
  Gemini 3.1 Pro Preview endpoints now accept low, medium, and high thinking.
  Retired Gemini 1.5 and Gemini 2.0 rows, the dated Gemini 2.5 Flash-Lite
  preview, and Gemini 3 Pro Preview are no longer included. The surface-probe
  documentation uses the global location so a complete catalog run includes
  global-only models.
- Native Vertex `gemini-flash-latest` and `gemini-flash-lite-latest` metadata
  no longer schedules or accepts named thinking levels that the endpoints
  reject. Their existing text, image, function-tool, limits, pricing, and SSE
  metadata remains available.
- Surface-probe minimal-text repair checks now set additive
  `availabilityOKAfterFailure` evidence while preserving the original
  `sigma_request_shape`, capability, or other failure outcome and error.

## Compatibility

- `OpenAIResponsesCompat.SupportsExplicitPromptCacheMode` is additive and
  opt-in. Only an explicit `CacheRetentionNone` request on a marked model gains
  `prompt_cache_options`; unset retention, older or unmarked models, Azure
  OpenAI, Codex, and existing provider-specific cache overrides retain their
  prior behavior. Provider registration, authentication, transports,
  serialized messages, and normalized responses are unchanged.
- OpenAI Responses terminal-status metadata is additive and uses the existing
  opaque provider metadata map. Public APIs, request payloads, serialized
  message shapes, normalized stop reasons, and provider error behavior are
  unchanged.
- OpenAI-compatible Chat Completions finish-reason metadata is additive and
  uses the existing opaque provider metadata map. Public APIs, request
  payloads, serialized message shapes, normalized stop reasons, and provider
  error behavior are unchanged.
- Anthropic, Google, Vertex AI, and Bedrock terminal-reason metadata is
  additive and uses the existing opaque provider metadata map with the
  wire-native keys `stop_reason`, `finishReason`, and `stopReason`. Public APIs,
  request payloads, normalized stop reasons, provider errors, and transports
  are unchanged.
- `WithRequestHTTPClient` is additive. A call-scoped client overrides client
  and provider fallback clients for HTTP/SSE dispatch without changing default
  clients, provider request shapes, image or embedding calls, or WebSocket
  dialing.
- Anthropic environment credential discovery is additive: explicit request and
  client credentials keep precedence, and provider IDs, routes, and serialized
  message shapes are unchanged.
- Claude Opus 5 is a metadata-only catalog addition. Existing Anthropic,
  Bedrock, and GitHub Copilot registration APIs and request shapes are
  unchanged; the Bedrock entry is global inference-profile-only, so Sigma does
  not register `anthropic.claude-opus-5` as an on-demand model ID.
- Mistral Conversations now accepts the existing `MistralToolChoiceTool` with a
  non-empty name. Provider IDs, routes, and serialized message shapes are
  unchanged.
- Mistral Conversations now serializes existing boolean
  `Tool.ProviderMetadata["strict"]` values on function definitions. Provider
  IDs, routes, and public types remain unchanged.
- Mistral request shapes, APIs, and recognized terminal reasons are unchanged.
  Explicit error and unknown terminal `stop_reason` values now end with a typed
  provider failure, with their raw values retained in existing provider metadata
  and diagnostics.
- Mistral retrieval tools are opt-in through `provider/mistral.Tools`; their
  execution remains server-side. Sigma does not add a client tool loop,
  connector registration, conversation persistence, or lifecycle APIs.
- `provider/xai` adds Responses registration helpers. Built-in `xai/grok-4.5`
  now dispatches through OpenAI Responses rather than Chat Completions; no
  provider ID or serialized-message shape changes.
- xAI OAuth requires an application-supplied approved client ID and scopes. It
  does not change API-key authentication, provider IDs, request routes, or
  serialized-message shapes.
- `ProviderKimiCoding` retains its existing registration API. K3 now accepts
  `low`, `high`, and `max` reasoning levels, while `kimi-coding/k2p7` no longer
  resolves from the built-in catalog; supported-model message shapes are
  unchanged.
- Kimi Coding subscription OAuth is opt-in through `provider/kimi` auth
  registration or an in-memory token provider. It does not change the existing
  API-key fallback, provider ID, request route, or serialized-message shape;
  applications continue to own token persistence.
- OpenRouter browser OAuth is opt-in through `provider/openrouter` and returns
  a permanent API key. Its optional manual fallback accepts a pasted redirect
  URL or authorization code for remote browsers. It does not change the
  existing API-key fallback, provider ID, request routes, or serialized-message
  shapes; applications continue to own credential persistence.
- Cached Codex WebSocket continuation recovery is internal. It does not change
  provider IDs, request APIs, serialized message shapes, or normal session
  behavior.
- Codex WebSocket account isolation is internal. Existing cleanup helpers close
  every account-scoped connection and reset all state for the requested caller
  session ID. Reusing an explicitly cleaned session starts fresh debug counters;
  inactive diagnostic and SSE fallback state expires after five minutes.
- Shared default dispatch, read-only registry locking, and bounded embedding
  workers are internal execution changes. Provider and model IDs, request and
  serialized-message shapes, public registry clone isolation, and embedding
  configuration/result types are unchanged.
- Additional provider terminal-marker checks change only truncated-stream
  handling. Normal request payloads and successful response normalization are
  unchanged, and applications still own stream retry and fallback execution.
- `ProviderOpenCodeGo` retains its existing registration API. Its Grok 4.5
  catalog row now uses the existing Responses dispatch path, while Kimi K3
  remains on Chat Completions.
- The existing Fireworks Kimi K3 model ID and registration are unchanged. Its
  Chat Completions compatibility now recognizes `max` reasoning, required
  replay, cache-affinity headers, and deferred client-tool definitions through
  existing `AddedToolNames`; no provider, route, or new request option is
  added.
- `ProviderRadius` is a new opt-in registration. Its models are empty until an
  explicit refresh succeeds; requests use standard API-key resolver precedence
  with `RADIUS_API_KEY` as the environment fallback. OAuth uses a caller-owned
  client configuration and can authenticate catalog refreshes through
  `WithCatalogAuthResolver`. `radius.WithCatalogStore` adds caller-owned model
  snapshots, and `Client.RestoreTextModels` restores one without a gateway
  request; registrations without a catalog store retain the existing behavior.
- `provider/qwen` adds additive opt-in registrations for
  `ProviderQwenTokenPlan` and `ProviderQwenTokenPlanCN`. Requests retain the
  existing shared Chat Completions payload shape; API-key discovery uses
  `QWEN_TOKEN_PLAN_API_KEY` and `QWEN_TOKEN_PLAN_CN_API_KEY` respectively.
- Grammar custom tools are opt-in by OpenAI Responses metadata,
  `OpenAICompletionsCompat` metadata, or `OpenAIOptions`; setting
  `EnableGrammarTools` to `false` preserves function-tool serialization, and
  explicit enablement is rejected on unrelated APIs. This adds no new provider
  or provider-neutral constrained-sampling surface.
- The `google-vertex` surface-probe route is an additive CLI diagnostic. It
  resolves project and location from explicit environment variables, prefers
  `GOOGLE_CLOUD_ACCESS_TOKEN` over the existing API-key fallbacks, and neither
  persists nor refreshes the token. Gemini 2.5 reasoning payloads now use
  `thinkingBudget` instead of the unsupported `thinkingLevel`, and unsupported
  disabled-thinking requests are rejected or omitted according to generated
  metadata. Public APIs, provider registration, authentication, transports,
  serialized messages, normalized responses, default probe routes, and CI
  behavior are unchanged.
- The native `google-vertex` text refresh is metadata-only. Existing provider
  registration, authentication, request payload contracts, transports,
  normalized responses, and public APIs are unchanged. The newly added models
  resolve through the existing adapter, while `gemini-1.5-flash`,
  `gemini-1.5-flash-8b`, `gemini-1.5-pro`, `gemini-2.0-flash`,
  `gemini-2.0-flash-lite`, `gemini-2.5-flash-lite-preview-09-2025`, and
  `gemini-3-pro-preview` no longer resolve from the built-in Vertex registry.
  Catalog costs use standard global PayGo rates; region-specific and
  long-context pricing remain outside this refresh.
- The Vertex latest-alias correction changes only capability validation:
  ordinary requests keep their existing payloads, while explicit named
  thinking requests fail locally instead of reaching Vertex with an
  unsupported field. Provider registration, authentication, transports,
  serialized messages, normalized responses, and public APIs are unchanged.
- `availabilityOKAfterFailure` is additive probe JSONL and summary metadata.
  Minimal-text success no longer replaces the original outcome, so consumers
  should treat availability as independent supporting evidence.

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).
- Vertex partner MaaS probes, Vertex image and embedding probes, broader
  catalog expansion, location-aware probe filtering, regional pricing, and
  automatic ambient credential loading remain outside this native Gemini text
  slice.

## Validation status

Validate this release with the process in [RELEASING.md](../RELEASING.md),
including the local CI-equivalent `mise run ci` gate before tagging.
