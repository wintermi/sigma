# Release notes: sigma v0.8.0

This is the maintainer-facing development note for the next `sigma` tag. Add
the v0.8.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.8.0 begins by tightening native Gemini 3 replay compatibility across
Google Generative AI and Vertex AI so function calls and matching tool results
retain stable normalized IDs. Amazon Bedrock Converse Stream service exceptions
also retain their requested model and AWS request ID for diagnostic correlation.
Google and Vertex replay now also retain blank signature-only text and thinking
parts only when the signature is valid for the same provider, API, and model.
Replayed Bedrock tool inputs now remove provider-rejected empty object-member
names without altering stored or streamed tool arguments.
Qwen Token Plan now exposes Qwen3.8 Max under its generally available model ID
across both regional routes while preserving supported reasoning levels and
keeping Qwen3.7 Max toggle-only. A distinct Individual subscription route adds
eight curated models, including DeepSeek V4 Pro 0813, through the shared
international endpoint and credential, with each model's thinking controls
preserved. Z.ai and Z.ai Coding CN now add GLM-5.2 Highspeed and GLM-5.3 with
million-token limits, model-specific reasoning aliases, and evidence-backed
estimated pricing. Baseten is now available through a first-class
OpenAI-compatible route with focused GLM 5.2 and Kimi K2.6 metadata and native
chat-template thinking controls. GLM 5.2 now advertises both text and image
input so image-bearing tool results remain available on that route. Xiaomi's
direct and regional Token Plan catalogs now expose only the supported MiMo V2.5
text-provider lineup instead of retired V2 model names.
Fireworks GLM 5.2 routes now use session affinity for automatic
prompt caching without unsupported long-cache retention. Anthropic-routed
OpenRouter agent loops now advance their final conversation cache breakpoint
through the latest non-empty tool result.
Direct DeepSeek V4 Flash plus its OpenCode Zen, Zen Free, and Go routes now
support low reasoning effort while retaining their existing high and
maximum-effort mappings.
Direct xAI now includes Grok 4.6 through OpenAI Responses with text and image
input, function tools, 500k-token context and output limits, tiered
long-context pricing, and reasoning controls through `xhigh`.
Anthropic Messages streams now surface text and thinking delivered with
content-block start events immediately through incremental output. Refusal
stops now also retain non-empty structured provider details for callers that
need the refusal category or explanation. Direct Claude Fable 5 and Opus 5
requests can additionally opt into catalog-declared server-side refusal
fallbacks without changing default requests; known fallback responses retain
the requested model identity while reporting usage and estimated cost against
the returned model.
In-progress text streams now identify every partial assistant snapshot as
pending, beginning with an empty snapshot on the initial start event.
OpenAI-compatible Chat Completions models can also opt into successful
`[DONE]` termination when their endpoint does not emit `finish_reason`.
OpenAI-compatible Chat Completions, Responses, and Azure Responses requests can
now carry request-scoped arbitrary sampling parameters with explicit override
precedence. OpenAI Responses-compatible and Azure OpenAI Responses requests now
also clamp typed output-token limits below 16 to the accepted request minimum.
OpenAI-compatible Chat Completions usage now also recognizes top-level
`cached_tokens` from compatible Kimi and Moonshot responses as cache reads
instead of ordinary input.
Custom OpenAI-compatible Chat Completions models can also opt into safely
clamped top-level thinking-token budgets for compatible inference servers.
Text, image, and embedding requests can now ask stored credentials and built-in
caller-owned OAuth token providers to refresh before dispatch when too little
token lifetime remains, without changing existing refresh defaults.
Provider OAuth descriptors and registry summaries now also identify known
subscription-backed flows so applications can distinguish them from generic
OAuth sign-in without inferring from provider names or credential types.
GitHub Copilot callers can also discover the authenticated account's available
model IDs and filter Sigma's curated catalog without enabling policies or
mutating registry state.
Overlapping runtime text, image, and embedding model-source operations now
publish per-provider registry state in latest-started order, so a slower older
refresh cannot overwrite a newer refresh or cached text restore.
Kimi and Kimi Coding requests now use a Sigma-owned coding-endpoint identity
from their shared provider wrapper instead of duplicated model-catalog headers.
The opt-in evaluation runner now isolates every case/model/repetition run with
an independent deadline so a stalled provider call does not cancel later
evaluations. Repository-internal Sigma evaluation harnesses can now execute
bounded caller-owned text tool loops, and the live suite adds a deterministic
tool-call round trip that verifies the call, local result, and final answer.
Reviewed OpenAI Responses and Codex Responses models now use native,
message-anchored additional-tool input, and streamed tool-call namespaces are
retained only when their loading context can be replayed safely. Codex Responses
SSE and WebSocket streams also recognize `response.done` completion and retain
explicit `end_turn` values as opaque diagnostics. Provider failures that report
an exhausted upstream request buffer are now classified as retryable for
caller-owned recovery. Responses incomplete terminals now distinguish
max-output and content-filter stops from missing or unknown reasons, with a
provider-neutral helper for bounded caller-owned max-token recovery. Existing
strict function-tool opt-ins now derive provider-compatible closed schemas for
supported OpenAI-compatible Chat Completions, Responses, Mistral, and
capability-gated Anthropic Messages routes, while local validation maps
optional non-nullable `null` placeholders back to omission without mutating
caller-owned data.
Text requests can now select automatic or disabled tool use through one
provider-neutral option across every built-in text API, while advanced tool
selection remains available through existing provider-specific controls.

## Added

- The international and China Z.ai Coding Plan routes now expose GLM-5.2,
  GLM-5.2 Highspeed, and GLM-5.3 as a consistent million-token cohort. GLM-5.2
  variants map Sigma's `minimal` through `high` levels to provider `high` and
  `xhigh`/`max` to provider `max`; GLM-5.3 maps `minimal`/`low` to `low`,
  `medium`/`high` to `high`, and `xhigh`/`max` to `max`. Omitted reasoning
  remains disabled, explicit GLM-5.3 `off` fails locally, GLM-5.2 uses
  API-equivalent estimated pricing, and unevidenced prices remain zero.
- `AnthropicOptions.EnableRefusalFallbacks` enables direct Anthropic
  server-side refusal fallback only for models with generated allowed-target
  metadata. Claude Fable 5 uses the ordered Opus 4.8 and Opus 5 targets, while
  Claude Opus 5 uses Opus 4.8. Disabled requests omit both the fallback payload
  and beta header; unsupported opt-ins fail before dispatch. Provider-reported
  declared fallback models drive usage identity and estimated pricing, while
  unknown returned model IDs remain diagnostic and retain requested-model
  accounting.
- `WithToolChoice` accepts `ToolChoiceAuto` or `ToolChoiceNone` across OpenAI
  Chat Completions, Responses, Azure Responses, Codex Responses, Anthropic
  Messages, Google Gemini and Vertex, Mistral Conversations, and Bedrock
  Converse. Required, any, named-tool, and custom configurations remain on the
  existing provider-specific option surfaces.
- Google Generative AI and Vertex assistant replay now preserve blank text and
  thinking parts when their thought signature passes the existing
  provider/API/model and base64 checks. Unsigned whitespace and blank parts with
  invalid or foreign signatures are omitted, while nonblank content and tool
  calls retain their original order.
- `StopReasonPending` now identifies in-progress assistant output. Every
  non-terminal text event carries a `PartialMessage`; the initial start event
  receives an empty pending snapshot, and accumulated content snapshots remain
  pending until a provider supplies another explicit reason or terminates the
  stream.
- Strict function tools now receive derived provider schemas with closed
  objects, all properties required, and originally optional non-nullable
  properties represented as nullable. The existing boolean
  `Tool.ProviderMetadata["strict"]` opt-in drives this behavior on supported
  OpenAI-compatible Chat Completions, Responses, Mistral Conversations, and
  capability-gated Anthropic Messages routes. Built-in direct Anthropic models
  advertise support through `AnthropicMessagesCompat.SupportsStrictTools`, and
  `anthropic.MessagesCompat.StrictTools` lets custom endpoints override the
  detected capability. `ValidateToolCall` removes matching provider-emitted
  `null` placeholders from its decoded argument copy.
- `OAuthAuth.IsSubscription` and `ProviderAuthInfo.OAuthSubscription` expose
  advisory subscription metadata through detailed provider auth descriptors,
  registry listings, and snapshots. Anthropic, GitHub Copilot, Kimi Coding,
  OpenAI Codex, and xAI mark their OAuth flows as subscription-backed; Radius
  and unmarked custom OAuth flows retain the zero-value generic classification.
- `githubcopilot.DiscoverGitHubCopilotModels` returns the authenticated
  account's available model IDs, and `GitHubCopilotModelAvailability.ModelFilter`
  creates a snapshot filter for `Client.Models`. Discovery excludes explicitly
  tool-incompatible models and recovers Individual account availability from
  enabled policies only when the picker catalog is empty.
- Runtime text, image, and embedding model-source publication now uses
  independent per-provider latest-started generations. Text refresh and cached
  restore operations share the same ordering rule, while operations for other
  providers continue independently.
- `IsRecoverableMaxTokens` reports when a max-token completion used fewer
  output tokens than the caller's original requested or model limit. It is
  advisory only; callers retain control of compaction, retry budgets, and
  request replay.
- The reviewed GPT-5.4, GPT-5.4 Mini/Pro, GPT-5.5, and GPT-5.6 OpenAI Responses
  rows plus GPT-5.6 Codex Responses rows now encode deferred client tools as
  native developer-role `additional_tools` items immediately after the matching
  tool result. Older tool-search-capable models retain the existing paired
  client search replay.
- `WithOAuthMinimumValidity`, `WithImageOAuthMinimumValidity`, and
  `WithEmbeddingOAuthMinimumValidity` now let callers require additional OAuth
  lifetime before dispatch. Stored credentials refresh serially and persist
  rotations, while built-in caller-owned token providers update their in-memory
  credentials through the existing refresh callbacks.
- `OpenAIOptions.SamplingParameters` now carries arbitrary request-scoped
  sampling fields for OpenAI-compatible Chat Completions, Responses, and Azure
  Responses. These fields override typed request values, while raw provider
  `extra_body` values retain final precedence; unsupported APIs reject non-empty
  sampling maps before dispatch.
- `OpenAICompletionsCompat.SupportsThinkingTokenBudget` now enables top-level
  `thinking_token_budget` payloads for custom compatible models when callers
  select reasoning and provide an explicit positive budget. Sigma clamps the
  budget against the request or model output ceiling to preserve 1,024 tokens
  for visible output; sampling parameters and raw `extra_body` values retain
  their existing override precedence.
- `cmd/sigma-evals-runner` now defaults each case/model/repetition run to an
  independent one-minute timeout bounded by the overall command deadline.
  Timed-out runs remain operational failures with partial artifacts and
  comparison diagnostics, while later runs continue when the overall deadline
  remains active; callers can configure the duration or disable it.
- Repository-internal Sigma evaluation harnesses can now execute caller-owned
  text tools across bounded completion rounds. Successful and intentional-error
  tool results retain names in replay, normalized traces, accumulated usage,
  and private transcripts; executor failures remain operational errors. The
  opt-in live runner now includes a sixth case whose hidden local result passes
  only with a matching successful tool call/result trace and exact final answer.
- `OpenAICompletionsCompat` now supports an opt-in setting for endpoints that
  end streams with `[DONE]` but do not emit `finish_reason`.
- Qwen Token Plan Individual now provides a distinct registration route for
  DeepSeek V4 Flash 0731, DeepSeek V4 Pro, DeepSeek V4 Pro 0813, GLM-5.2,
  Qwen3.6 Flash, Qwen3.7 Max, Qwen3.7 Plus, and Qwen3.8 Max. It reuses the
  international endpoint, `QWEN_TOKEN_PLAN_API_KEY`, and the shared
  OpenAI-compatible Chat Completions adapter.
- Baseten now provides a first-class registration route backed by the shared
  OpenAI-compatible Chat Completions adapter. The focused built-in catalog
  covers vision-capable GLM 5.2 and Kimi K2.6 with `BASETEN_API_KEY` discovery,
  reviewed inputs, limits, and token pricing.
- Xiaomi's direct API-billing and regional Token Plan catalogs provide the
  `mimo-v2.5`, `mimo-v2.5-pro`, and `mimo-v2.5-pro-ultraspeed` model lineup
  through the existing OpenAI-compatible Chat Completions routes.
- Direct xAI metadata now includes Grok 4.6 through the existing OpenAI
  Responses registration path. It accepts text and image input, function
  tools, and low, medium, high, or `xhigh` reasoning within a 500k-token
  context and output limit. Standard rates are $2 input, $6 output, and $0.50
  cached input per million tokens; requests above 200k input tokens use the
  $4, $12, and $1 rates.

## Compatibility

- Xiaomi no longer advertises `mimo-v2-flash`, `mimo-v2-omni`, or
  `mimo-v2-pro` through its generated direct or regional Token Plan catalogs.
  Callers using those retired IDs must select a V2.5 model; Sigma does not
  silently alias or migrate model IDs. Xiaomi endpoints, credentials, request
  routing, and retained V2.5 metadata are unchanged.
- Omitting `WithToolChoice` preserves existing request payloads and Codex's
  automatic default. Existing typed and raw provider-specific tool controls
  override the provider-neutral fallback. `ToolChoiceNone` retains declared
  tool definitions on providers that support an explicit disabled choice;
  Bedrock instead omits active and replay-synthesized tool configuration because
  Converse has no disabled tool-choice variant.
- Direct xAI Grok 4.6 uses `/responses`, disables provider-side response
  storage, and requests encrypted reasoning whenever a supported effort is
  selected. Explicit `xhigh` maps directly to the provider effort; off and
  minimal remain unsupported. Long cache retention is omitted while cache keys
  and session affinity remain available. Grok 4.5 and the existing legacy Chat
  Completions routes are unchanged.
- OpenAI-compatible Chat Completions usage now falls back to top-level
  `cached_tokens` when nested cache details and `prompt_cache_hit_tokens` do not
  report a cache read. Cache reads remain included in provider prompt totals,
  so Sigma removes them from ordinary input tokens and prices them separately;
  raw usage, existing field precedence, and zero or omitted values are
  unchanged.
- Typed max-token options below 16 now serialize as 16 for
  OpenAI Responses-compatible and Azure OpenAI Responses requests. Unset values
  remain omitted, sampling parameters and raw `extra_body` values retain their
  existing precedence, and Codex Responses continues omitting
  `max_output_tokens`.
- Anthropic Messages refusal stops now retain non-empty `stop_details` in
  opaque assistant provider metadata alongside the raw `stop_reason`. Refusal
  and sensitive stops remain normalized as content filters, and null or empty
  details remain omitted.
- Blank or whitespace-only Google and Vertex assistant text or thinking with a
  missing, invalid, or cross-provider/API/model thought signature is no longer
  serialized as an empty part. Valid same-model signature-only parts remain
  replayable, and nonblank content remains present without an unusable
  signature. Caller-owned history and tool-call replay are unchanged.
- Non-terminal text events that previously omitted `PartialMessage` or left its
  stop reason empty now expose an empty or accumulated snapshot with
  `StopReasonPending`. Provider-supplied non-empty partial reasons and all
  successful, failed, or aborted terminal reasons remain unchanged. Consumers
  should persist only terminal messages; image streams and provider request
  behavior are unaffected.
- Anthropic-style OpenRouter Chat Completions cache markers now treat non-empty
  tool-result messages as eligible final conversation breakpoints. Empty tool
  results fall back to the preceding eligible message; disabled caching, other
  cache-control formats, tool-result fields, and the bounded system and final
  tool-definition markers remain unchanged.
- `kimi.DefaultUserAgent` now identifies requests as `sigma/kimi-coding` for
  both Kimi provider IDs. Provider, model, and request header overrides retain
  their existing precedence, and API-key and OAuth authentication are unchanged.
- Strict schema derivation does not add public tool types. Anthropic-compatible
  routes remain unchanged unless model metadata or the provider compatibility
  override enables strict tools; Bedrock and Google routes remain unchanged.
  Omitted or false strict metadata and unsupported routes preserve their
  existing payloads. Explicit strict schemas that use references, composed
  object or array unions, tuples, conditionals, pattern properties, or schema-
  valued additional properties fail before dispatch rather than relying on
  provider rejection; caller-owned schemas and tool-call arguments remain
  unchanged.
- Subscription metadata is informational only. It does not change OAuth login,
  credential selection, refresh timing, persistence, or provider dispatch, and
  custom OAuth descriptors remain generic unless callers opt in explicitly.
- GitHub Copilot model discovery is caller-invoked and advisory. It retries one
  rate-limited catalog request with a bounded provider delay, but does not run
  during login or refresh, enable model policies, persist availability, mutate
  registries, or replace generated catalog metadata. A valid empty account
  catalog produces a filter that matches no models.
- A superseded runtime model-source operation returns Sigma's existing conflict
  error and cannot replace the winning registry catalog, even when the newer
  operation fails. Source interfaces, source-owned network and cache side
  effects, automatic refresh behavior, persistence policy, and generated
  catalogs remain unchanged.
- OpenAI, Azure, and Codex Responses map incomplete `max_output_tokens` and
  `content_filter` reasons to successful normalized stops. Missing and unknown
  reasons return typed provider errors while preserving partial content, usage,
  cost, terminal status, Codex `end_turn`, and raw incomplete diagnostics.
- Upstream request-buffer exhaustion now produces a transient classification
  and same-model retry advice even when accompanied by a bad-request status.
  Sigma preserves partial finals and does not automatically replay post-body
  failures.
- Codex Responses accepts `response.done` as a successful terminal alias across
  SSE and WebSocket transports and retains an explicitly supplied `end_turn`
  boolean in assistant provider metadata. The value remains diagnostic and does
  not alter normalized stop reasons or agent control flow.
- OpenAI Responses and Codex Responses now retain function and custom-tool
  namespaces in opaque provider metadata through streaming and same-model
  replay. Cross-model replay keeps a namespace only when the matching deferred
  tool is loaded in the replayed request; incompatible providers and APIs omit
  it.
- Omitted or zero OAuth minimum-validity options retain existing provider
  refresh timing. Request requirements can lengthen but cannot shorten a
  provider's configured refresh window, and credentials without a known expiry
  remain usable without an early refresh.
- Google Generative AI and native Vertex Gemini 3 requests now preserve
  normalized tool-call IDs on replayed function calls and matching tool
  results. Older Vertex Gemini requests continue omitting unsupported IDs.
- Amazon Bedrock Converse Stream service exceptions now retain the requested
  model and AWS request ID in typed provider errors and assistant diagnostics
  while preserving existing stop reasons and retry classification.
- Amazon Bedrock Converse Stream removes empty object-member names recursively
  only from outbound replayed tool inputs. Provider-emitted tool arguments,
  caller-owned messages, arrays, scalar values, `null`, and non-empty keys
  remain unchanged.
- Qwen Token Plan now replaces the retired Qwen3.8 Max Preview ID with
  Qwen3.8 Max while preserving supported reasoning levels through native
  `reasoning_effort` controls on the international and China routes. Qwen3.7
  Max remains toggle-only. The Individual route preserves mapped reasoning
  efforts for DeepSeek V4, including Pro 0813, GLM-5.2, and Qwen3.8 Max while
  keeping Qwen3.6 Flash and both Qwen3.7 models toggle-only.
- Baseten GLM 5.2 accepts text and image input, including image-bearing tool
  results, while retaining its mapped off, high, and max reasoning efforts.
  Kimi K2.6 retains its existing image support and explicit thinking toggle
  without sending unsupported reasoning-effort values.
- Fireworks GLM 5.2 and GLM 5.2 Fast requests now send session affinity when
  prompt caching is enabled and omit unsupported explicit long-cache retention.
- Direct DeepSeek V4 Flash plus its OpenCode Zen, Zen Free, and Go routes now
  map `ThinkingLevelLow` to the provider's `low` reasoning effort. DeepSeek V4
  Pro and models exposed through other routes retain their existing
  independently reviewed level mappings.
- Anthropic Messages streams now emit non-empty text and thinking delivered by
  content-block start events as ordered initial deltas while retaining
  signatures, citations, and complete final blocks.
- OpenAI-compatible Chat Completions streams configured without finish-reason
  support now infer normal or tool-call completion from assembled output after
  an explicit `[DONE]` marker. Default compatibility remains strict, and raw
  EOF without a terminal signal remains an error.

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

Validate this release with the process in [RELEASING.md](../RELEASING.md),
including the local CI-equivalent `mise run ci` gate before tagging.
