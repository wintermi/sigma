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
Qwen Token Plan now exposes Qwen3.8 Max under its generally available model ID
across both regional routes while preserving supported reasoning levels and
keeping Qwen3.7 Max toggle-only. A distinct Individual subscription route adds
seven curated models through the shared international endpoint and credential,
with each model's thinking controls preserved. Baseten is now available through
a first-class OpenAI-compatible route with focused GLM 5.2 and Kimi K2.6
metadata and native chat-template thinking controls. Fireworks GLM 5.2 routes
now use session affinity for automatic prompt caching without unsupported
long-cache retention.
Anthropic Messages streams now surface text and thinking delivered with
content-block start events immediately through incremental output.
OpenAI-compatible Chat Completions models can also opt into successful
`[DONE]` termination when their endpoint does not emit `finish_reason`.
OpenAI-compatible Chat Completions, Responses, and Azure Responses requests can
now carry request-scoped arbitrary sampling parameters with explicit override
precedence.
Custom OpenAI-compatible Chat Completions models can also opt into safely
clamped top-level thinking-token budgets for compatible inference servers.
Text, image, and embedding requests can now ask stored credentials and built-in
caller-owned OAuth token providers to refresh before dispatch when too little
token lifetime remains, without changing existing refresh defaults.
Provider OAuth descriptors and registry summaries now also identify known
subscription-backed flows so applications can distinguish them from generic
OAuth sign-in without inferring from provider names or credential types.
The opt-in evaluation runner now isolates every case/model/repetition run with
an independent deadline so a stalled provider call does not cancel later
evaluations.
Reviewed OpenAI Responses and Codex Responses models now use native,
message-anchored additional-tool input, and streamed tool-call namespaces are
retained only when their loading context can be replayed safely. Codex Responses
SSE and WebSocket streams also recognize `response.done` completion and retain
explicit `end_turn` values as opaque diagnostics. Provider failures that report
an exhausted upstream request buffer are now classified as retryable for
caller-owned recovery. Responses incomplete terminals now distinguish
max-output and content-filter stops from missing or unknown reasons, with a
provider-neutral helper for bounded caller-owned max-token recovery.

## Added

- `OAuthAuth.IsSubscription` and `ProviderAuthInfo.OAuthSubscription` expose
  advisory subscription metadata through detailed provider auth descriptors,
  registry listings, and snapshots. Anthropic, GitHub Copilot, Kimi Coding,
  OpenAI Codex, and xAI mark their OAuth flows as subscription-backed; Radius
  and unmarked custom OAuth flows retain the zero-value generic classification.
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
- `OpenAICompletionsCompat` now supports an opt-in setting for endpoints that
  end streams with `[DONE]` but do not emit `finish_reason`.
- Qwen Token Plan Individual now provides a distinct registration route for
  DeepSeek V4 Flash 0731, DeepSeek V4 Pro, GLM-5.2, Qwen3.6 Flash, Qwen3.7 Max,
  Qwen3.7 Plus, and Qwen3.8 Max. It reuses the international endpoint,
  `QWEN_TOKEN_PLAN_API_KEY`, and the shared OpenAI-compatible Chat Completions
  adapter.
- Baseten now provides a first-class registration route backed by the shared
  OpenAI-compatible Chat Completions adapter. The focused built-in catalog
  covers GLM 5.2 and Kimi K2.6 with `BASETEN_API_KEY` discovery, reviewed
  inputs, limits, and token pricing.

## Compatibility

- Subscription metadata is informational only. It does not change OAuth login,
  credential selection, refresh timing, persistence, or provider dispatch, and
  custom OAuth descriptors remain generic unless callers opt in explicitly.
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
- Qwen Token Plan now replaces the retired Qwen3.8 Max Preview ID with
  Qwen3.8 Max while preserving supported reasoning levels through native
  `reasoning_effort` controls on the international and China routes. Qwen3.7
  Max remains toggle-only. The Individual route preserves mapped reasoning
  efforts for DeepSeek V4, GLM-5.2, and Qwen3.8 Max while keeping Qwen3.6 Flash
  and both Qwen3.7 models toggle-only.
- Baseten GLM 5.2 requests now send `chat_template_args.enable_thinking` with
  mapped off, high, and max reasoning efforts. Kimi K2.6 uses the same explicit
  thinking toggle without sending unsupported reasoning-effort values.
- Fireworks GLM 5.2 and GLM 5.2 Fast requests now send session affinity when
  prompt caching is enabled and omit unsupported explicit long-cache retention.
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
