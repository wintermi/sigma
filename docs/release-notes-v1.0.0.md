# Release notes: sigma v1.0.0

This is the maintainer-facing release note for `sigma` v1.0.0. For the
itemized change list see [CHANGELOG.md](../CHANGELOG.md); for the validation
commands and pre-tag checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v1.0.0 is the first stable release. From this tag, the exported API of
the root `sigma` package, the `provider/...` packages, and `sigmatest`, together
with the request JSON read and written by `MarshalRequest` and
`UnmarshalRequest`, follow semantic versioning: a breaking change requires a new
major version and module path. [Versioning and
compatibility](../README.md#versioning-and-compatibility) defines the boundary.
The built-in model catalog, wire behavior that follows upstream API changes,
error text, and the `internal/...`, `cmd/...`, and `examples/` trees can still
change in any release.

The release brings the catalog in line with what providers serve today, hardens
credential handling and login flows, makes reasoning controls and history
replay behave the same way across providers, tightens tool validation and
streaming, and corrects failure classification, caching, and cost estimates.
Read the [upgrade notes](#upgrade-notes) before moving from v0.7.0.

### Catalog

- The built-in catalog now holds 631 text, 58 image, and 8 embedding models.
- Models providers have shut down are removed: 129 rows from the November
  retirement review, which checked provider deprecation pages and live model
  lists, and 167 rows from the September registry review. Ant Ling and Xiaomi
  lost every model to retirement and now list the successors their providers
  name: Ling-3.1-flash, MiMo-V2.6-Flash, and MiMo-V2.6-Pro.
- Rows that answered with a different model or not at all are removed: the
  Gemini API `gemini-3.5-flash` and `gemini-3.7-flash` IDs, which Google
  silently serves with newer models; the Vertex `gemini-flash-latest` and
  `gemini-flash-lite-latest` aliases, which Vertex no longer serves; and the
  Codex `gpt-5.5` and `gpt-5.3-codex-spark` rows, which Codex rejects for
  ChatGPT accounts.
- The November catalog review adds 75 text models, including Claude Sonnet 5.5,
  Haiku 5.5, and Fable 5.2 with their Bedrock and Vertex routes, GPT-6.1 and
  GPT-6.2, Gemini 4 Argon, Carbon, Hydrogen, and Neon, Mistral Large 4, and
  several NVIDIA NIM rows. OpenCode Zen and Go and Fireworks gain 52 current
  models, and GitHub Copilot, OpenAI Codex, and Azure OpenAI Responses list the
  GPT-6.1 and GPT-6.2 models.
- Direct OpenAI and Codex add GPT-6 Astra, direct xAI adds Grok 4.6, MiniMax
  and Kimi Coding add MiniMax-M3.1-Flash and the Kimi K3.1 family, and Z.ai
  adds GLM-5.3 and GLM-5.3 Highspeed.
- Prices and limits are corrected: long-context price tiers on 27 direct,
  Azure, OpenCode, OpenRouter, Vercel, and Copilot rows, Vertex Gemini Pro
  pricing above 200,000 input tokens, the 10% regional premium on geographic
  Bedrock Claude profiles, Mistral cached-input rates, MiniMax-M3 limits, and
  GPT-5.6 on Azure and Bedrock.
- Model capabilities are corrected: image input on 16 Mistral and Ministral
  rows, temperature restrictions on 13 Claude rows, and the thinking levels
  each model actually accepts. `Model.SupportsThinkingLevel` now requires an
  explicit map entry for `xhigh` and `max` on every thinking model.
- The Mistral Medium 3.5 row uses Mistral's documented ID,
  `mistral-medium-3-5`.

### Providers and routes

- Baseten is a first-class OpenAI-compatible provider, and Qwen Token Plan
  covers every plan, including the new Individual China provider
  (`ProviderQwenTokenPlanIndividualCN`, `qwen.RegisterIndividualCN`). Each Qwen
  plan reads its own API-key variable before the shared regional one.
- OpenCode requests use OpenCode's documented per-protocol endpoints under
  `/inference`, and OpenCode keeps same-model replay state across turns.
- GitHub Copilot business, enterprise, and data-residency accounts reach their
  own endpoint, and Copilot reasoning usage, reasoning effort, and strict tools
  match the direct providers.
- OpenAI Codex uses the ChatGPT Codex Responses endpoint, honors `MaxTokens`,
  and sends the system prompt as a developer message so it stays in the prompt
  cache.
- Vertex express mode works for native Gemini text, and generated Azure OpenAI
  Responses models send `api-version=v1`.
- Radius uses the gateway's current context shape, forwards tool choice,
  reports the physical model through `AssistantMessage.ResponseModel()`, and
  estimates cost from the gateway catalog.
- Mistral requests send `store: false` by default, so Mistral no longer keeps
  every transcript.
- Anthropic message threads are available as an opt-in through
  `AnthropicMessagesCompat.SupportsMessageThreads`.

### Authentication and credential safety

- HTTP redirects no longer carry provider credentials to another host, and
  refuse HTTPS downgrades and cross-origin body resends.
- GitHub Copilot sends its token only to trusted `proxy-ep` hosts, including
  stored base URLs, and Radius gateway catalogs cannot point model requests at
  another host.
- Browser logins listen on both loopback addresses, validate the callback state
  first, fall back to a free port when the default is taken, show their success
  page reliably, and cancel the paste-the-code prompt once the browser
  finishes.
- Bedrock honors `AWS_PROFILE` ahead of environment keys, applies the AWS SDK
  host rule to container credential URIs, and fetches metadata credentials
  without going through `HTTP_PROXY`.
- Credential structs, `Options`, and `AuthResolution` redact secrets when
  formatted, and synchronous provider and Codex WebSocket errors redact
  recognized credentials.
- A stored OAuth credential owns its provider: a failed refresh ends the
  request instead of switching to an environment API key.
- OAuth tokens refresh five minutes before expiry, a started refresh completes
  and persists even when the caller is cancelled, and a failed save of
  refreshed credentials is retried. `WithOAuthMinimumValidity` and its image and
  embedding variants let callers require more token lifetime before dispatch.
- Device-code logins poll at the server's interval with a one-second floor, and
  GitHub Copilot login enables the catalog models the account needs.
  `githubcopilot.DiscoverGitHubCopilotModels` reports which models an account
  can use.
- The Claude Code identity is sent only with Anthropic subscription tokens, and
  `OAuthAuth.IsSubscription` marks subscription-backed OAuth flows.

### Reasoning and thinking

- Turning thinking off reaches the provider on OpenAI Responses, Azure, Chat
  Completions, Codex, OpenRouter, Mistral, and Claude Opus 5, instead of
  falling back to the default effort.
- Each provider receives only the levels a model accepts: `xhigh` goes only to
  Claude models that support it, Gemini models that cannot disable thinking get
  their lowest level, custom Gemini 2.x models get thinking budgets, and
  gpt-oss receives its effort on Groq, Cerebras, Cloudflare, Together, and
  Bedrock.
- Changing the thinking level no longer invalidates the GPT-6 prompt cache:
  GPT-6 sends a fixed effort and carries the requested level in
  `configuration_update` items. Other Responses models can opt in with
  `OpenAIResponsesCompat.SupportsMidConversationEffort`.
- Claude Opus 5 records its effort in `AssistantMessage.ProviderThinkingLevel`
  and replays it per turn, and Claude adaptive thinking on Anthropic, Bedrock,
  and OpenCode drops stale signed thinking instead of failing.
- Responses reasoning models request encrypted reasoning whenever reasoning may
  run, so the model keeps its reasoning across tool calls, and final messages
  report reasoning OpenAI removed in `ProviderMetadata["input_transformations"]`.
- Direct Anthropic budget thinking uses the same default budgets as Bedrock.

### Replay and handoff

- Thinking recorded by another provider, API, or model is dropped rather than
  replayed as text, and reasoning replay requires exact provider, API, and model
  provenance.
- Cross-model handoff drops the previous model's thinking and reports each drop
  in `HandoffReport.DroppedThinkingBlocks`.
- Responses replay generates its own item IDs when the paired reasoning item is
  dropped and keeps parallel tool calls distinct, and failed or aborted
  assistant turns are left out of later requests.
- Google, Bedrock, and Chat Completions replay keep signatures, encrypted
  reasoning, and reasoning details only where the provider can use them.

### Tools and structured output

- `WithToolChoice` selects automatic or disabled tool use across every built-in
  text API.
- Strict function tools get derived provider schemas, and strict schemas that a
  provider would reject, such as untyped nodes or keywords Claude's strict mode
  does not allow, fail locally with the tool and keyword named.
- `ValidateToolCall` enforces more of JSON Schema, accepts schema shapes common
  in zod, Pydantic, and MCP definitions, treats `null` in an optional property
  as omission, and with `CoercePrimitives` wraps a single value sent where an
  array is expected. Recursive unions no longer take exponential time.
- Tool arguments keep exact JSON numbers as `json.Number` through streaming,
  persistence, and replay, and empty argument objects survive on the wire.
- Anthropic accepts tool schemas with a root `anyOf`, `oneOf`, or `allOf`,
  Gemini respects `const` values, and Claude conversations survive a failed
  tool result that contains an image.
- Requests with images to a text-only model replace each run of images with a
  short note instead of failing, and whitespace-only tool results no longer
  fail requests.
- Tool call IDs need to be unique only within one assistant message, so
  transcripts from providers that reuse IDs across turns can be persisted.

### Streaming, errors, and retries

- Turns that end with an unrecognized finish reason now fail with a typed
  provider error on Chat Completions, Gemini, Vertex, Anthropic, and Codex,
  instead of looking complete. Gemini `CONTINUATION` is a max-token stop.
- Context overflow is detected on Bedrock, mid-stream, and from common
  provider wording, and throttling is no longer mistaken for overflow.
- OpenCode and Z.ai plan limits classify as quota, and more mid-stream gateway
  failures, numeric error codes, and capacity errors classify as transient.
- Opt-in HTTP retries default to a 60-second delay cap with jittered
  exponential backoff, follow `x-should-retry`, and include 408 and 409.
- Every non-terminal text event carries a `PartialMessage` with
  `StopReasonPending`.
- Streams that open with a byte order mark keep their first event, and the
  stream body follows the `io.Reader` end-of-stream contract.
- Long streamed tool arguments and delta-only providers stream in linear time.
- Codex WebSocket connections retire after 45 minutes, and requests fall back
  to SSE only when the connection fails before any output.

### Caching, usage, and cost

- Responses session affinity uses the documented `session-id` header, and Codex
  and GPT-6 keep their prompt cache across turns and level changes.
- Bedrock one-hour caching uses the Converse `1h` value and prices one-hour
  writes correctly, and Azure receives `prompt_cache_retention` only when the
  model supports it.
- DeepSeek, Together, Cerebras, Z.ai, and Cloudflare Workers AI streams report
  usage and cost again, OpenRouter bring-your-own-key costs include the
  upstream inference, and GPT-6 Fast mode is priced as priority processing.
- Context-aware max-token helpers reserve 15% of the estimated input, and token
  estimates use 3.5 characters per token.

### Embeddings, images, and retrieval

- Embedding responses are checked for vector count, dimensions, finite values,
  and size, and external embedding caches require a `CacheNamespace`.
- `EmbedBatch` averages oversized inputs only when `SplitOversized` is set.
- The retrieval splitter handles invalid UTF-8, short headings, and large
  documents in linear time.
- Gemini image requests map portrait and landscape sizes, OpenAI multi-image
  edits use `image[]`, OpenRouter image models request only the modalities they
  produce, and `gemini-3.1-flash-image` replaces the retired Imagen 4 rows.

### Developer tooling

- `cmd/sigma-surface-probe` probes only catalog models, using each row's
  metadata, and tests OpenCode models through the API OpenCode routes them to.
  It validates `json_schema` replies against the schema, skips cases a model's
  provider rejects, stops at the overall deadline and reports the rest as
  skipped, defaults to a 30-minute timeout, and adds Vertex Anthropic and Google
  image routes.
- `cmd/sigma-evals-runner` bounds each run with its own timeout and runs
  caller-owned tool loops.
- `mise run go:build` is a CGO-disabled compilation check of every package.

## Added

- Stability policy: [Versioning and
  compatibility](../README.md#versioning-and-compatibility) defines the stable
  API and the deprecation rule.
- `SubmitDeferred`, `FetchDeferred`, and `CancelDeferred` run durable text
  responses in the background, initially on direct OpenAI Responses, with a
  serializable `DeferredResponseHandle`.
- `WithToolChoice` with `ToolChoiceAuto` and `ToolChoiceNone` across every
  built-in text API.
- `StopReasonPending` for in-progress assistant snapshots.
- `IsRecoverableMaxTokens` for advisory max-token recovery.
- `WithOAuthMinimumValidity`, `WithImageOAuthMinimumValidity`, and
  `WithEmbeddingOAuthMinimumValidity`.
- `OAuthAuth.IsSubscription` and `ProviderAuthInfo.OAuthSubscription`.
- `githubcopilot.DiscoverGitHubCopilotModels` and
  `GitHubCopilotModelAvailability.ModelFilter`.
- `AnthropicOptions.EnableRefusalFallbacks` for catalog-declared server-side
  refusal fallbacks.
- `AnthropicMessagesCompat.SupportsMidConversationEffort` and
  `SupportsMessageThreads`, and
  `OpenAIResponsesCompat.SupportsMidConversationEffort`.
- `OpenAIOptions.SamplingParameters`,
  `OpenAICompatibleModelConfig.SamplingParameters`, and
  `MetadataOpenAISamplingParameters` for arbitrary sampling fields.
- `OpenAICompletionsCompat.ThinkingTokenBudgetField`,
  `RequiresAssistantContent`, `SupportsPromptCacheKey`, and the opt-in
  `[DONE]` completion setting for endpoints without `finish_reason`.
- `OpenAIResponsesCompat.SupportsMaxOutputTokens` and `SupportsPromptCacheKey`.
- `AssistantMessage.ResponseModel()`.
- `ErrEmbeddingVectorNonFinite`, and `EmbeddingBatchConfig.SplitOversized` and
  `CacheNamespace`.
- `bedrock.ConverseUsage.LongCacheWriteInputTokens`.
- Optional `Usage` on persisted tool-result messages.
- `ProviderBaseten`, `ProviderQwenTokenPlanIndividual`, and
  `ProviderQwenTokenPlanIndividualCN`.

## Compatibility

### Upgrade notes

- Removed catalog models no longer resolve. Callers that name one must register
  it themselves or move to a current model; sigma does not alias removed IDs.
- Callers that used `mistral-medium-3.5` must switch to `mistral-medium-3-5`.
- Mistral requests send `store: false`. Set the `store` provider option to
  `true` to keep the previous behavior.
- Tool arguments decode numbers as `json.Number`. Code that asserts `float64`
  must convert with `Float64`, `Int64`, or a lossless representation.
- External embedding caches must set a non-secret
  `EmbeddingBatchConfig.CacheNamespace`. Cache keys move to version 3, so
  older entries are bypassed and may be regenerated at provider cost.
- `EmbedBatch` fails with `ErrInvalidOptions` for an input over the batch byte
  limit unless `SplitOversized` is set.
- `bedrock.ConverseUsage` gains a field, so unkeyed struct literals must be
  updated.
- A stored OAuth credential that cannot refresh now fails the request instead
  of falling back to an environment API key.
- Turns that end with an unrecognized finish reason, Gemini
  `MALFORMED_FUNCTION_CALL` or `UNEXPECTED_TOOL_CALL`, or a failed Codex
  response now return errors. Callers that expected a nil error must handle
  them.
- Qwen Token Plan reads `QWEN_TOKEN_PLAN_TEAM_API_KEY`,
  `QWEN_TOKEN_PLAN_INDIVIDUAL_API_KEY`, or `QWEN_TOKEN_PLAN_TEAM_CN_API_KEY`
  before the shared regional variable, which still works.
- AWS credential and config file overrides replace their default paths instead
  of falling back to the home-directory files.
- Client-default `ToolChoiceNone` now suppresses tool selection, and an
  explicit tool choice is no longer dropped when no tools are sent.

### Deprecations

- `WithHandoffThinkingDelimiters`, `HandoffChangeThinkingConverted`, and
  `HandoffReport.ConvertedThinkingBlocks` still compile but have no effect,
  since handoff now drops foreign thinking. They remain until the next major
  version.

### Serialization boundary

- Histories written by earlier versions remain loadable without migration and
  keep their opaque metadata. Replay omits reasoning metadata whose provider,
  API, or model provenance does not match the target, while text, tool calls,
  and tool results are kept.
- Shared copies preserve nil lists as `null` and empty lists as `[]`.
- Tool-result messages may carry an optional `Usage`, which is never sent to
  providers.
- Only terminal assistant messages should be persisted; partial snapshots carry
  `StopReasonPending`.
- Anthropic hosted-tool results discarded by earlier versions cannot be
  reconstructed.

### Provider behavior

- OpenCode keeps one Zen and one Go provider. Models that need China hosting
  can return a `RegionError` until the workspace opts in.
- Fireworks Spot models are not in the catalog, since Spot IDs fail for accounts
  without Spot access. Callers with access can register the
  `accounts/fireworks/models/<model>-spot` models themselves.
- Provider-specific tool choices, sampling parameters, and raw `extra_body`
  values keep their precedence over the new provider-neutral options.
- Pricing remains provider-specific and estimated, and catalog inclusion does
  not guarantee account access.

## Deferred work

- Recheck the dated upcoming model retirements before their deadlines.
- Add Fireworks Spot models once a Spot-enabled account confirms them, and add
  Fireworks `ember-1` once its surface probes pass.
- Identify the OpenCode Go models that need workspace China-hosting opt-in, and
  add explicit availability metadata and diagnostics for them.
- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

Validated on 2026-11-22 with:

- `mise run go:generate` with no generated metadata drift.
- `mise run ci`.
- `git diff --check`.

Maintainer-run live surface probes passed every case on the xAI, Fireworks Chat
Completions, Fireworks Anthropic-compatible, and Google Vertex routes. The last
full OpenCode Go run left eight cases inconclusive because of provider limits,
which the probe now skips.
