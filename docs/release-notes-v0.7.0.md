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
The existing OpenRouter image adapter now also exposes Krea 2 Large, Medium,
and Medium Turbo, MAI-Image 2.5 Pro, and Auto Router Beta through generated
model metadata. Text callers can now also select an HTTP client for an
individual HTTP/SSE request.

## Changed

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

## Compatibility

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
  every account-scoped connection for the requested session, while debug stats
  and SSE fallback state remain aggregated by caller session ID.
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

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

Validate this release with the process in [RELEASING.md](../RELEASING.md),
including the local CI-equivalent `mise run ci` gate before tagging.
