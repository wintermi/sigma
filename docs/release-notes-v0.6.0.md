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
cost estimate. Persisted assistant messages can now also carry optional usage
metadata, and deterministic request-estimate helpers let callers approximate
context size before dispatch, anchoring on the latest successful
provider-reported usage when available. Mistral Conversations now also maps
cache-enabled session IDs to
prompt-cache keys, reports provider cached prompt tokens as cache reads, and
accepts URL-backed image references for image-capable chat inputs and
stringified image-bearing tool results. Context-aware max-output-token helpers now let
callers opt in to output budgeting from model metadata and deterministic
request estimates without changing provider dispatch defaults. Reasoning
budget planning helpers now also let callers opt in to planning visible output
caps and hidden thinking budgets together from model/context metadata and
deterministic request estimates without changing provider dispatch defaults.
It also adds
caller-owned GitHub Copilot OAuth
helpers for
device-code login, token refresh, request-time credential resolution, and
explicit model-policy enablement. Codex Responses WebSocket transport now also
honors standard HTTP(S) proxy environment variables with `NO_PROXY` exclusions
while keeping the existing SSE fallback, and now exposes a Codex-specific
connect timeout plus session-cache debug stats for connection reuse, cached
context deltas, and fallback diagnostics. Root session-resource cleanup helpers
now also let callers release cached provider resources without knowing the
provider-specific cleanup function, with Codex WebSocket sessions registered
automatically. Text request transport choices now also fail locally before
provider dispatch when callers pass unknown transports or request HTTP/WebSocket
transport for built-in streaming APIs that do not support them. Core text and
image stream cancellation now preserves aborted partial finals through
`Collect`, `Complete`, and `CollectImages`, and canceled streams close promptly
even when callers stop reading events. Runtime stream/replay hardening now also
covers tolerant SSE parsing, deterministic fake-provider lifecycles,
partial-message snapshots, best-effort decoded partial tool-call argument
metadata, safer JSON/tool-argument persistence, stricter coercion, image
request validation, explicit handoff coordinates, OpenAI-compatible
usage/finish-reason normalization, request-scoped provider auth precedence, and
cache-cost catalog validation. Kimi and Kimi
Coding are promoted as
focused Anthropic-compatible provider slices with generated metadata,
credential discovery, request headers, adaptive thinking metadata, and
session-affinity support. Xiaomi is promoted as a focused OpenAI-compatible
provider slice with API-billing and regional token-plan registration helpers,
generated MiMo metadata, and regional API-key discovery. The environment
credential resolver also exposes non-secret discovery helpers so applications
can inspect candidate and configured API-key variable names before making a
request, and focused provider helpers now let callers pass Cloudflare AI
Gateway placeholder values and Bedrock region/static credential values without
mutating process environment.
Generated Bedrock metadata now also includes focused EU Anthropic Claude
regional rows that reuse the existing `eu.` inference-profile endpoint fallback
for the EU runtime route.
Bedrock SigV4 signing now also preserves escaped inference-profile ARN paths
for AWS-credential Converse Stream and embeddings requests.
Credential stores and provider auth descriptors now give applications an
opt-in way to resolve stored API keys, serialize OAuth refreshes, and preserve
rotated credentials while leaving Sigma's default environment-based credential
resolution unchanged. Stored auth descriptors can now also apply
provider-scoped request configuration, such as routing placeholder values,
before provider URLs and headers are built.
Request-scoped header suppression now also lets callers remove final outgoing
compatibility/default headers across text, image, and embedding requests while
preserving credential resolution and avoiding a generic environment override
surface.
Cloudflare Workers AI is also promoted as a direct
OpenAI-compatible Chat Completions wrapper with account placeholder resolution
and normal bearer-token auth for the direct Workers AI endpoint. Vercel AI
Gateway is promoted as a focused Anthropic-compatible Messages wrapper over
the generated gateway model metadata with normal API-key auth. Built-in
Anthropic-compatible route metadata and wrapper defaults now use versioned
Messages base URLs for Anthropic, Vercel AI Gateway, GitHub Copilot, and
Cloudflare AI Gateway so Sigma targets `/v1/messages`-shaped endpoints. NVIDIA
NIM is also promoted as a focused OpenAI-compatible text and embedding wrapper with
generated metadata, direct NIM base URL defaults, request header metadata, and
NVIDIA-specific embedding input-type mapping; its text metadata now also
requests streamed usage when supported, includes focused GPT-OSS 120B and
Nemotron 3 Ultra direct NIM rows, and can be exercised through the opt-in
surface probe. Moonshot AI and Moonshot AI CN
are promoted as focused OpenAI-compatible Chat Completions wrappers with
generated K2.7 Code CN and HighSpeed metadata plus
metadata-driven handling for K2.7 routes that reject explicit disabled thinking
payloads. Z.ai and Z.ai Coding CN are also promoted as focused
OpenAI-compatible Chat Completions
wrappers with generated GLM metadata, GLM-5.2 reasoning-effort mapping, and
deterministic registration and request coverage. Ant Ling is promoted as a
focused OpenAI-compatible Chat Completions wrapper with generated Ling/Ring
metadata reuse, Ant Ling reasoning-object compatibility, and deterministic
registration and request coverage. Azure OpenAI Responses is promoted as a
focused provider wrapper with provider-scoped registration and request option
helpers over the existing Responses adapter. DeepSeek, Groq, Cerebras, and Together are
promoted as focused OpenAI-compatible Chat Completions wrappers that reuse the
existing generated metadata and shared adapter with deterministic registration,
request, error, and cancellation coverage. Hugging Face Router is also
promoted as a focused OpenAI-compatible Chat Completions wrapper with `HF_TOKEN`
credential discovery, focused generated metadata, and the shared adapter's
deterministic registration, request, error, and cancellation coverage.
OpenRouter is also promoted as a focused OpenAI-compatible Chat Completions
wrapper with generated text metadata reuse, request-scoped routing
compatibility, and deterministic registration, request, error, and cancellation
coverage. The
surface probe command adds a
credential-gated cross-provider handoff diagnostic for replaying small
tool-call contexts across selected live routes without moving live provider
calls into CI. It also adds a focused structured-output probe mode for
OpenAI-compatible routes, reporting JSON object and strict JSON Schema support
or fallback behavior without updating generated metadata. Provider-neutral
structured-output and top-logprob request controls now map onto existing
OpenAI-compatible, Anthropic Messages, and Bedrock Converse request paths with
local validation for unsupported APIs. Public handoff helpers now also let
callers adapt persisted or incremental conversation
context for a target model with explicit capability-loss reporting, while
keeping orchestration and provider execution caller-owned. Assistant results
now also expose provider-neutral source and
citation accessors for the source metadata Sigma already captures from grounded
and citation-bearing responses, plus text response ID and routed response model
accessors over captured provider response metadata. Request content now also
supports provider-neutral document/PDF blocks backed by base64 data, URLs, or
provider file IDs, with initial OpenAI Responses, OpenAI Chat Completions, and
Anthropic Messages payload support. Local tool-call validation
also now evaluates composed JSON Schema branches, string patterns, and `not`
schemas so callers can reject invalid model-emitted arguments before running
tools, with an opt-in validation mode for conservative primitive argument
coercion on decoded argument copies. OpenAI-compatible Chat
Completions streams now
also preserve provider reasoning-detail metadata on streamed tool calls so it
can be replayed with assistant tool-call history. The deterministic provider
test suite now also locks Google stream `thoughtSignature` attachment,
OpenAI-compatible Chat Completions thinking-block replay behavior,
OpenAI-compatible stream error finish handling, OpenAI Responses terminal
stream handling, duplicate reasoning-alias suppression, and the new promoted
thin provider rows, plus request-conversion guardrails for replay IDs, Chat
Completions payload shape, routed model metadata, and Google legacy
tool-schema sanitization. The model metadata generator can now also compare
the checked-in catalog with a validated candidate catalog and print a
deterministic added/removed/changed report without writing generated files. It
can also write a validated review-only candidate catalog from an explicit
`models.dev` snapshot path or opt-in network source, leaving the checked-in
catalog and generated files untouched until the diff is reviewed.
Registries can now also refresh app-owned dynamic text, image, and embedding
model sources at runtime, so local servers and routers with live catalogs can
update client model listings without changing Sigma's curated built-in
catalogs. Registry model copies now also protect nested provider metadata, so
callers can mutate returned metadata without corrupting registry state.
Deterministic routing-decision helpers now classify requests into route tiers
with weighted rule-based signals, select tier candidates from caller-defined
policies, and turn classified upstream errors into retry, fallback, or abort
advice without adding any execution loop or configuration format to Sigma.

## Added

- Anthropic Messages usage now populates
  `sigma.Usage.LongCacheWriteInputTokens` from long prompt-cache write usage
  and `sigma.CostForUsage` prices those writes at the long-cache input
  multiplier while preserving total cache-write token accounting.
- Anthropic Messages prompt-cache markers are now bounded to API-valid
  breakpoints for cache-enabled agent loops, avoiding over-marked user turns,
  tool results, and tool definitions in long conversations.
- Text-generation usage now populates provider/model identity on
  `sigma.Usage`, preserves a JSON-like `Usage.Raw` copy of provider usage
  payloads when providers report usage, normalizes provider tool/connector
  token counts into `Usage.ToolUseInputTokens`, and exposes provider-reported
  cost separately from Sigma's estimated `Cost.TotalCost`.
- Persisted assistant messages now accept optional `Usage` metadata, and
  `sigma.EstimateRequestTokens`, `EstimateMessageTokens`,
  `EstimateContentTokens`, and `EstimateTextTokens` provide deterministic
  approximate token estimates using provider-reported usage as the latest
  successful assistant-turn anchor when available.
- `sigma.MaxTokensForContext` and `sigma.WithMaxTokensForContext` now combine
  model context/output metadata with `EstimateRequestTokens` to produce an
  opt-in max-output-token cap for callers that want context-aware budgeting
  before dispatch.
- `sigma.ReasoningBudgetForContext` and
  `sigma.WithReasoningBudgetForContext` now combine visible output caps,
  hidden thinking budgets, model context/output metadata, and
  `EstimateRequestTokens` into an opt-in reasoning budget plan before
  dispatch.
- Mistral Conversations now maps cache-enabled `sigma.WithSessionID` requests
  to `prompt_cache_key` and `x-affinity`, and streamed provider cached prompt
  token fields now populate `Usage.CacheReadInputTokens` while preserving raw
  usage payloads for diagnostics.
- Mistral Conversations now accepts URL-backed `sigma.ImageURL` blocks in user
  messages and tool-result messages when the target model declares image input
  support. User image chunks use the provider's `image_url` content shape, and
  tool-result images replay as string image references so `function.result`
  remains schema-valid.
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
- Text request transport options now validate locally before provider dispatch.
  Unknown transport strings, HTTP transport for built-in streaming text APIs,
  and WebSocket transport outside the Codex Responses route fail as invalid
  options before a provider call is made.
- `sigma.CleanupSessionResources` and `sigma.RegisterSessionResourceCleanup`
  now provide provider-neutral session resource cleanup. Codex Responses
  WebSocket sessions register automatically, so callers can release one session
  or all cached provider sessions from the root package while existing
  provider-specific cleanup helpers continue to work.
- Kimi can now be registered with `kimi.Register` or
  `kimi.RegisterDefault`, and Kimi Coding can be registered with
  `kimi.RegisterCoding` or `kimi.RegisterCodingDefault`, using the shared
  Anthropic-compatible Messages adapter with Kimi endpoint defaults, Kimi CLI
  request headers, `KIMI_API_KEY` credential discovery, and generated metadata
  for `kimi-for-coding`, `k2p7`, and `kimi-k2-thinking`.
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
- `sigma.CredentialStore`, `sigma.InMemoryCredentialStore`,
  `sigma.ProviderAuth`, and `sigma.WithStoredProviderAuth` now provide an
  opt-in stored credential layer for API-key and OAuth credentials. Store
  updates use serialized modify callbacks, registries can carry provider auth
  descriptors independently from provider implementations, auth-derived
  provider configuration is applied before provider URL/header construction,
  and Anthropic, GitHub Copilot, OpenAI Codex, and Cloudflare helpers expose
  focused descriptors over their existing auth behavior.
- `sigma.WithSuppressedHeader` and `sigma.WithSuppressedHeaders` now remove
  final outgoing request headers after provider defaults, model metadata
  headers, dynamic compatibility headers, and caller headers are merged. Image
  and embedding requests expose matching `WithImageSuppressed*` and
  `WithEmbeddingSuppressed*` helpers.
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
- Vercel AI Gateway can now be registered with `vercel.Register` or
  `vercel.RegisterDefault`, using the shared Anthropic-compatible Messages
  adapter with Vercel AI Gateway base URL defaults, generated gateway model
  metadata, and `AI_GATEWAY_API_KEY` credential discovery.
- NVIDIA NIM can now be registered with `nvidia.Register` for
  OpenAI-compatible Chat Completions and `nvidia.RegisterEmbeddings` for
  OpenAI-compatible embeddings, using direct NIM base URL defaults,
  `NVIDIA_API_KEY` credential discovery, generated text and embedding
  metadata, and deterministic request coverage.
- NVIDIA NIM embeddings now map `sigma.EmbeddingInputTypeQuery` to
  `input_type: "query"` and `sigma.EmbeddingInputTypeDocument` to
  `input_type: "passage"` unless callers set
  `EmbeddingRequest.ProviderMetadata["input_type"]` explicitly.
- NVIDIA NIM text metadata now marks streaming usage as supported for the
  direct NIM OpenAI-compatible Chat Completions rows and includes focused
  generated rows for `openai/gpt-oss-120b` and
  `nvidia/nemotron-3-ultra-550b-a55b`.
- `cmd/sigma-generate-models -validate-nvidia-live` now fetches NVIDIA NIM
  `/models` on demand and reports direct text catalog rows that need review
  without changing the default offline generation path.
- `cmd/sigma-generate-models -diff-catalog` now compares the checked-in
  catalog with a validated candidate catalog and prints deterministic added,
  removed, changed, and unchanged counts for text, image, and embedding rows
  without writing generated files.
- `cmd/sigma-generate-models -refresh-catalog` now writes a validated
  review-only candidate catalog from an explicit `models.dev` snapshot path or
  opt-in network source. Refresh mode requires an explicit
  `-refresh-snapshot-date`, preserves existing image and embedding rows, and
  exits before writing generated Go files.
- `sigma.TextModelSource` and `sigma.TextModelSourceFunc` now let applications
  attach provider-scoped runtime text model sources to a registry, and
  `Registry.RefreshTextModels` / `Client.RefreshTextModels` refresh those
  app-owned listings atomically after local validation.
- `sigma.ImageModelSource` and `sigma.ImageModelSourceFunc` now let
  applications attach provider-scoped runtime image model sources to a
  registry, and `Registry.RefreshImageModels` / `Client.RefreshImageModels`
  refresh those app-owned listings atomically after local validation.
- `sigma.EmbeddingModelSource` and `sigma.EmbeddingModelSourceFunc` now let
  applications attach provider-scoped runtime embedding model sources to a
  registry, and `Registry.RefreshEmbeddingModels` /
  `Client.RefreshEmbeddingModels` refresh those app-owned listings atomically
  after local validation.
- `cmd/sigma-surface-probe` now includes an opt-in `nvidia` route that uses
  `NVIDIA_API_KEY`, the direct NIM base URL, the NVIDIA provider wrapper, and
  `nvidia/nemotron-3-super-120b-a12b` as its default probe model when callers
  do not pass `-models`.
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
- Ant Ling can now be registered with `antling.Register` or
  `antling.RegisterDefault`, using the shared OpenAI-compatible Chat
  Completions adapter with direct base URL defaults, `ANT_LING_API_KEY`
  credential discovery, generated Ling/Ring metadata, and deterministic
  registration/request coverage.
- Ant Ling OpenAI-compatible requests now use the generated compatibility
  metadata or detected Ant Ling host shape to send `max_tokens`, omit
  unsupported prompt-cache fields, and map supported Ring reasoning levels to
  `reasoning: {"effort": ...}` without sending `reasoning_effort`.
- Azure OpenAI Responses can now be registered with `azure.Register` or
  `azure.RegisterDefault`, using the existing Azure Responses adapter with the
  built-in provider ID, provider-scoped endpoint/deployment/API-version
  helpers, API-key auth, and caller-supplied Microsoft Entra token
  credentials.
- DeepSeek, Groq, Cerebras, and Together can now be registered with their
  provider-local `Register` or `RegisterDefault` helpers, using the shared
  OpenAI-compatible Chat Completions adapter with direct base URL defaults,
  request-time bearer auth, existing generated metadata, and deterministic
  registration, request, redaction, context-overflow, and cancellation
  coverage.
- Hugging Face Router can now be registered with `huggingface.Register` or
  `huggingface.RegisterDefault`, using the shared OpenAI-compatible Chat
  Completions adapter with the router base URL, `HF_TOKEN` credential
  discovery, and deterministic registration, request, redaction,
  context-overflow, and cancellation coverage. Built-in metadata includes
  focused router rows for Qwen Coder, Kimi K2.6, and GLM 5.1.
- OpenRouter can now be registered for text generation with
  `openrouter.Register` or `openrouter.RegisterDefault`, using the shared
  OpenAI-compatible Chat Completions adapter with the OpenRouter base URL,
  `OPENROUTER_API_KEY` credential discovery, generated text metadata reuse,
  OpenRouter reasoning/routing compatibility, and deterministic registration,
  request, redaction, context-overflow, and cancellation coverage.
- `bedrock.WithRequestRegion` and `bedrock.WithRequestStaticCredentials` now
  provide request-scoped Bedrock runtime region and static AWS credential values
  before the existing AWS region and static credential environment fallbacks.
- Generated Amazon Bedrock metadata now includes focused EU Anthropic Claude
  regional rows for Fable 5, Haiku 4.5, Opus 4.5/4.6/4.7/4.8, and Sonnet 4.6,
  with deterministic registry assertions and the existing `eu.` runtime
  endpoint fallback.
- `cmd/sigma-surface-probe -handoff` now builds a small tool-call context for
  each selected live route/model and replays it pairwise into the other selected
  routes, emitting JSONL diagnostics with `sourceRoute` and `sourceModel` so
  replay failures can be attributed without making handoff checks part of CI.
- `cmd/sigma-surface-probe -structured-output` now runs only the
  OpenAI-compatible JSON object and strict JSON Schema cases, emitting JSONL
  results and summary recommendations that identify schema support, JSON-object
  fallback, and prompt-only JSON fallback without making live probes part of CI
  or changing generated metadata.
- `sigma.WithStructuredOutput`, `sigma.WithJSONOutput`,
  `sigma.WithJSONSchemaOutput`, and `sigma.WithTopLogprobs` now provide
  provider-neutral request controls. Structured output maps to existing
  OpenAI-compatible response formats, Anthropic `output_format`, or Bedrock's
  synthetic schema-tool path, while unsupported API families fail locally
  before provider dispatch.
- `sigma.TransformRequestForModel` and `sigma.TransformMessagesForModel` now
  adapt conversation context for a target text model without invoking a
  provider. The helpers preserve provider-native thinking only for exact-model
  replay, convert foreign or unsupported thinking to tagged text, reject
  unsupported image content by default, optionally replace unsupported image
  blocks with caller-supplied text, repair tool-result names where target
  metadata requires them, synthesize explicit error tool results for missing
  tool outputs, bridge only tool-result-to-user transitions for targets that
  require it, normalize replay IDs for stricter targets, and return a
  `HandoffReport` describing every lossy or compatibility-driven change.
- `sigma.AssistantMessage.Sources`, `sigma.ContentBlock.Citations`, and
  `sigma.AssistantMessage.Citations` now expose normalized source and citation
  entries from provider metadata, including URLs, URIs, titles, offsets, cited
  text, and copied provider metadata for provider-specific details.
- `sigma.AssistantMessage.ResponseID` now exposes captured text-generation
  provider response IDs from existing assistant metadata without requiring
  callers to inspect provider metadata maps directly.
- `sigma.AssistantMessage.ResponseModel` now exposes captured routed
  provider model IDs from existing assistant metadata without requiring callers
  to inspect provider metadata maps directly.
- `sigma.ValidateToolCall` now evaluates `anyOf`, `oneOf`, `allOf`, `pattern`,
  and `not` in tool input schemas, including nested property, array item, and
  additional property schemas, while preserving decoded-copy results and
  redacted validation errors.
- `sigma.ValidateToolCallWithOptions` now accepts
  `sigma.ToolValidationOptions{CoercePrimitives: true}` so callers can opt
  into conservative primitive argument coercion before strict tool validation,
  without changing the default `ValidateToolCall` behavior, and without
  rewriting already-valid `anyOf` or `oneOf` values. Primitive coercion no
  longer converts JSON `null` into `0`, `false`, or `""`; null only validates
  against schemas that permit null values.
- Persisted content/tool JSON now preserves unknown content-block fields during
  round trips and decodes tool-call arguments with `json.Number`, avoiding
  64-bit integer precision loss during conversation replay while keeping
  replay validation strict for known content blocks.
- Deterministic provider tests now cover Google stream `thoughtSignature`-only
  chunks, empty signature deltas, signature updates on existing blocks, and
  OpenAI-compatible Chat Completions omission of prior private thinking blocks
  when `reasoning_content` is not required.
- Core streaming now accepts colonless SSE field lines, ignores unknown fields,
  supports LF, CRLF, and CR-only event boundaries, and populates
  `Event.PartialMessage` on non-terminal content events from the accumulated
  stream state.
- Streaming tool-call deltas now expose best-effort decoded partial argument
  metadata when object or array fragments can be completed safely, while
  retaining raw `argumentsText` metadata and leaving final persisted tool-call
  arguments unchanged.
- `sigmatest.FauxProvider` now fails loudly when scripts are exhausted and
  synthesizes realistic start/delta/end event lifecycles from final text,
  thinking, and tool-call content when explicit script events are omitted.
- OpenAI-compatible Chat Completions streams now preserve provider
  `reasoning_details` metadata on tool-call blocks and replay it with
  assistant tool-call history.
- Provider protocol hardening now covers Anthropic budget-thinking fallbacks,
  max-token-safe thinking budgets, metadata filtering, long-cache degradation,
  split thinking signatures, Google malformed function-call stop classification,
  Google empty replay-block filtering, Gemini image count validation, Bedrock
  redacted reasoning and exception classification, OpenAI-compatible
  index-less tool-call deltas and private-thinking replay, Azure Responses
  endpoint normalization, and GitHub Copilot enterprise OAuth refresh.
- OpenAI-compatible Chat Completions streams now surface provider
  `finish_reason` values of `network_error` and `model_context_window_exceeded`
  as errors instead of successful unknown stops, including context-overflow
  classification for `model_context_window_exceeded`.
- OpenAI-compatible Chat Completions streams now map top-level
  `prompt_cache_hit_tokens` to cache-read usage when nested cached-token
  details are absent, request streamed usage by default for local/custom
  endpoints unless metadata marks it unsupported, and map
  `finish_reason: "end"` to `StopReasonEndTurn`.
- OpenAI-compatible Chat Completions streams now use only the first non-empty
  reasoning alias from each delta, avoiding duplicated thinking text when a
  provider emits multiple equivalent reasoning fields in one chunk.
- Shared diagnostic redaction now treats Google API-key headers and Cloudflare
  AI Gateway auth headers as credential-bearing headers, so debug hooks redact
  those values even when they do not match known token patterns.
- OpenAI-compatible Chat Completions streams now require a terminal
  `finish_reason` before EOF is treated as a successful completion, preserving
  partial content and usage on the error final message when a stream ends
  early.
- Core text and image stream cancellation now records aborted final results
  before closing, preserving partial text or image outputs through the collector
  helpers and closing canceled streams even when callers abandon unread events.
- Image generation requests now receive local provider-neutral validation for
  known operations, counts, prompts/inputs, structurally valid text/image
  inputs, base64 data, URLs, and image-only masks before provider dispatch.
- Handoff reports now keep `HandoffChange.MessageIndex` as the source request
  coordinate and add `OutputMessageIndex` for inserted or synthesized output
  messages, avoiding mixed coordinate systems in repair reports.
- Request-scoped provider auth callbacks now run before client/default
  environment auth, matching the request-over-ambient precedence used by
  request API keys.
- Model catalog validation now rejects negative cache read/write input cost
  fields before generated metadata can be refreshed.
- OpenAI Responses streams now require `response.completed`,
  `response.incomplete`, or `response.failed` before EOF is treated as a
  terminal provider response. Premature EOF now returns an error with partial
  content preserved, and terminal incomplete responses finalize as max-token
  stops while preserving provider usage.
- OpenAI and Azure OpenAI Responses request payloads now keep
  `previous_response_id` limited to explicit provider options, so
  cache-affinity `sigma.WithSessionID` values are not sent as provider
  continuation IDs.
- Deterministic request-conversion tests now cover distinct OpenAI Responses
  replay IDs around reasoning items, OpenAI-compatible Chat Completions
  tool/max-token payload guardrails, provider-reported routed stream model
  metadata, and Google legacy tool-schema sanitization without adding live
  provider calls.
- `sigma.ClassifyRequest`, `sigma.RoutePolicy.Select`, and
  `sigma.RoutePolicy.Fallback` now provide deterministic routing decisions:
  weighted rule-based request classification into route tiers, tiered
  candidate selection with escalation and caller-supplied exclusions, and
  classified-error fallback advice including larger-context candidate
  selection on context overflow. Sigma only decides; callers execute requests
  and own health/cooldown state.

## Compatibility

- `sigma.Usage.LongCacheWriteInputTokens` is additive metadata for cost
  accounting. Existing `CacheWriteInputTokens` values remain the total cache
  write count, so callers that ignore the long-cache split keep the same token
  totals.
- Usage remains optional: `AssistantMessage.Usage == nil` and terminal
  `Event.Usage == nil` still mean no usage was supplied, while a non-nil
  zero-valued usage means the provider explicitly reported zero values.
- Request token estimates are approximate and caller-facing. They use
  deterministic character and image heuristics plus persisted assistant usage
  anchors; they do not call provider tokenizers, affect provider dispatch, or
  change cost accounting.
- Context-aware max-token helpers are also caller-facing and opt-in. They use a
  fixed safety margin plus the same deterministic request estimate, leave
  `MaxTokens` unset when no usable output cap exists, and do not change
  `Client.Complete`, `Client.Stream`, provider payload builders, or
  tokenizer behavior unless callers apply the returned option.
- OpenAI-compatible Chat Completions streams now treat EOF before a terminal
  `finish_reason` as an error for every compatible route. This avoids silently
  accepting truncated streams while preserving partial content and usage for
  diagnostics.
- For OpenAI and Azure OpenAI Responses, `sigma.WithSessionID` remains a cache
  and session-affinity hint. Responses continuation must use a real `resp_*`
  value supplied through `previous_response_id` provider options.
- Existing `sigma.Cost` component fields and `TotalCost` remain Sigma's
  estimated cost from model metadata. Provider-reported cost is additive and is
  only populated when an upstream payload contains a clear numeric cost field.
- Mistral prompt-cache behavior is additive and uses existing
  `sigma.WithSessionID` and `sigma.WithCacheRetention` options. Empty/default,
  short, long, and persistent retention enable the Mistral cache key; explicit
  `CacheRetentionNone` suppresses automatic `prompt_cache_key` and `x-affinity`
  values.
- Mistral URL image support is additive and uses the existing
  `sigma.ImageURL` content block. Base64 image blocks keep their existing data
  URL encoding for user image chunks, image-bearing tool results are flattened
  to string references, and unsupported image sources continue to return
  explicit request-conversion errors.
- Mistral Conversations request-shape validation now matches the
  `/v1/conversations` schema. Function results omit Chat Completions-only
  `name` and `is_error` fields, native Magistral reasoning sends top-level
  `prompt_mode`, and typed named-tool choices are rejected locally because
  Conversations accepts only `auto`, `none`, `any`, or `required`.
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
- Kimi and Kimi Coding registration is additive: the existing metadata rows
  remain available, and broader router or regional endpoint catalog expansion
  stays deferred.
- Xiaomi token-plan support is additive and uses distinct provider IDs for the
  CN, AMS, and SGP regional OpenAI-compatible routes. The API-billing
  `ProviderXiaomi` rows remain available, and `mimo-v2-flash` remains scoped to
  the API-billing provider rather than the token-plan providers.
- Environment credential discovery is additive and non-secret. `Resolve`
  remains the API that returns credential values, and the new helper methods do
  not probe ambient cloud credentials or OAuth token stores.
- Header suppression is additive and request-scoped. Header names are matched
  case-insensitively after final merge; credential-bearing auth headers are
  retained so suppression does not create keyless request semantics.
- Cloudflare and Bedrock request configuration helpers are provider-specific
  request options. They do not add a root-level environment override map, and
  existing process environment fallback behavior remains available.
- Cloudflare Workers AI direct routing is additive and Chat Completions-only in
  this release. It uses normal bearer-token auth, while Cloudflare AI Gateway
  routes continue to use `cf-aig-authorization`.
- Built-in Anthropic-compatible route base URLs are normalized for Sigma's
  Messages adapter. Custom caller-supplied base URLs are still used as provided,
  with Sigma appending `/messages`.
- NVIDIA NIM direct routing is additive and uses Sigma's existing
  OpenAI-compatible text and embedding adapters through a provider-specific
  wrapper. Endpoint overrides remain explicit through provider options or model
  metadata; Sigma does not read `NVIDIA_BASE_URL`. NVIDIA-specific
  OpenAI-compatible defaults now request streamed usage and omit unsupported
  `store`, developer-role, reasoning-effort, and strict-tool request fields for
  generated rows and custom NVIDIA endpoints detected by provider ID or NIM
  host.
- Moonshot direct routing is additive and Chat Completions-only in this
  release. The wrappers reuse the shared OpenAI-compatible adapter; broader
  live-provider coverage remains deferred until route-specific behavior needs
  independent fixtures.
- Z.ai direct routing is additive and Chat Completions-only in this release. The
  Z.ai and Z.ai Coding CN wrappers reuse the shared OpenAI-compatible adapter;
  broader live-provider coverage remains deferred until route-specific behavior
  needs independent fixtures.
- Ant Ling direct routing is additive and Chat Completions-only in this
  release. The wrapper reuses the shared OpenAI-compatible adapter and existing
  generated metadata; broader live-provider coverage remains deferred until
  route-specific behavior needs independent fixtures.
- DeepSeek, Groq, Cerebras, and Together direct routing is additive and Chat
  Completions-only in this release. The wrappers reuse the shared
  OpenAI-compatible adapter and existing generated metadata; Sigma does not add
  provider-specific base URL environment probing or hosted-tool APIs for these
  rows.
- Hugging Face Router direct routing is additive and Chat Completions-only in
  this release. The wrapper reuses the shared OpenAI-compatible adapter with a
  focused generated metadata subset; Sigma does not add live router discovery,
  provider-specific base URL environment probing, or hosted-tool APIs for this
  row.
- OpenRouter direct text routing is additive and Chat Completions-only in this
  release. The wrapper reuses the shared OpenAI-compatible adapter, existing
  focused text metadata, and OpenRouter compatibility handling; broad
  OpenRouter catalog expansion remains deferred to the reviewed catalog refresh
  workflow.
- OpenRouter image-generation helpers now use explicit image names:
  `openrouter.RegisterImages`, `openrouter.RegisterImagesDefault`, and
  `openrouter.NewImagesProvider`. The generic `openrouter.Register` and
  `openrouter.NewProvider` names now refer to text Chat Completions.
- Bedrock credential precedence remains explicit: typed bearer-token options
  and auth resolvers run before request static credentials, and request static
  credentials run before the existing static environment credential path.
- Bedrock SigV4 path canonicalization now matches the escaped model-ID path
  sent on the wire for inference-profile ARNs. This is a bug fix with no public
  API change, and bearer-token Bedrock requests keep their existing unsigned
  authorization behavior.
- Source and citation accessors are additive views over existing provider
  metadata. They do not change persisted request shape, replay behavior,
  provider dispatch, or the raw `ProviderMetadata` maps.
- `AssistantMessage.ResponseID` is an additive accessor over existing provider
  metadata. It does not add a serialized text result field or change provider
  request, stream, replay, or persistence behavior.
- `AssistantMessage.ResponseModel` is an additive accessor over existing
  provider metadata. It does not add a serialized text result field or change
  provider request, stream, replay, routing, or persistence behavior.
- Public handoff helpers are additive and opt-in. They transform copied
  requests or message slices before callers submit them; they do not change
  `Client.Complete`, `Client.Stream`, provider dispatch, persisted message
  shape, live probes, credential handling, or model registry contents.
- Tool schema composition validation and primitive argument coercion are
  additive. `ValidateToolCall` remains strict by default; callers must use
  `ValidateToolCallWithOptions` to request coercion, and already-valid union
  values are preserved.
- Provider-neutral structured output is additive and request-scoped. Explicit
  provider-specific response-format options still win when callers need a
  native shape, and top-logprob requests remain limited to
  OpenAI-compatible Chat Completions routes.
- The new replay and stream regression tests do not change public APIs,
  provider request shapes, or persisted replay semantics. Core cancellation
  tests now lock the stable `Final`/`Err`/`Done`/collector contract rather than
  requiring terminal event delivery after a caller stops reading.
- The request-conversion regression tests are coverage-only. They preserve
  existing public APIs and keep request-shape behavior unchanged for callers.
- Runtime text model refresh is additive and app-owned. Sources are registered
  per provider, fetched outside the registry write lock, validated before
  applying changes, and only replace models previously owned by that source.
  Caller-registered models, built-in metadata, image models, and embedding
  models are not refreshed by this text-only surface.
- Stored credential auth is additive and opt-in. Existing clients keep the
  same request API-key, client resolver, environment, and provider-callback
  behavior unless they configure both a credential store and
  `WithStoredProviderAuth`. Sigma provides an in-memory store and provider auth
  descriptors, and auth-derived provider configuration has lower precedence
  than caller-supplied request options. Sigma does not add built-in file,
  keychain, or encrypted persistence.

## Deferred work

During v0.6.0 development a review of Sigma's core surfaces (client and registry
lookup, Request/Message/ContentBlock/AssistantMessage shapes, Stream event
protocol with granular block start/delta/end and partial tool calls, Usage/Cost
accounting including long-cache and thinking tokens, auth resolvers and
OAuthTokenProvider, internal request/message transforms, provider adapters,
persistence, embeddings+retrieval, images, sigmatest, and generated metadata)
identified additional user-visible capability gaps that align with existing
deferred items. Public handoff support now ships as a narrow helper surface,
runtime text model refresh now supports app-owned sources, and opt-in
CredentialStore-based auth now covers stored API-key and OAuth resolution plus
provider-scoped request configuration; file-backed or encrypted persistence
plus built-in live model discovery remain deferred.
See the expanded bullets below and [TODO.md](../TODO.md) for the current list.
All candidate work remains subject to the deterministic evidence, fixture, and
cancellation bar described in [RELEASING.md](../RELEASING.md).

- File-backed, encrypted, OS keychain, and UI-driven credential persistence
  remain deferred and caller-owned. Sigma now defines the store interface and
  process-local in-memory store, but applications still own where durable
  credentials are persisted.
- OAuth token stores, AWS SSO, and full SDK-equivalent cloud credential-chain
  behavior remain deferred. Bedrock includes stdlib profile, ECS, web identity,
  and IMDS credential loading for the built-in adapter; applications with more
  advanced credential requirements should continue to resolve credentials before
  calling Sigma.
- Billing reconciliation, subscription analytics, and UI presentation of usage
  totals remain caller-owned. Sigma normalizes and preserves provider data but
  does not claim invoice-grade billing accuracy.
- Tokenizer-exact context budgeting and automatic dispatch-time output-token
  clamping remain deferred until Sigma has explicit tokenizer, precedence,
  observability, and override semantics.
- Cross-provider handoff is now available as public request/message adaptation
  helpers plus the existing opt-in surface probe diagnostic, but full agent
  orchestration remains deferred. Sigma does not automatically run transformed
  requests, select fallback models, persist handoff state, or hide reported
  capability loss from callers.
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
- Broader NVIDIA NIM behavioral validation beyond the focused surface-probe
  route, embedding catalog expansion, and any route behavior beyond the shared
  OpenAI-compatible adapters remain deferred until each surface has
  deterministic request, response, error, and metadata evidence.
- Broader Bedrock regional catalog expansion beyond the focused EU Anthropic
  rows remains deferred until regional routing, availability, and compatibility
  evidence are reviewed for each promoted row.
- Moonshot live-provider expansion beyond the focused direct Chat Completions
  wrapper and reviewed K2.7 metadata remains deferred until route-specific
  behavior needs deterministic evidence.
- Z.ai Anthropic-compatible, image, embedding, and broader live validation
  routes remain deferred until each surface has deterministic request, stream,
  error, and metadata evidence.
- Provider-hosted tools for the newly promoted OpenAI-compatible rows remain
  deferred until Sigma has an explicit provider-defined tool contract.
- Broad Hugging Face Router catalog expansion remains deferred until it can
  flow through the reviewed catalog refresh workflow with deterministic diffs.
- Broad OpenRouter text catalog expansion remains deferred until it can flow
  through the reviewed catalog refresh workflow with deterministic routing,
  pricing, and provider/API diffs.
- Provider-neutral routing policy, routed-model fallback selection, and
  automatic model substitution remain deferred; Sigma now exposes captured
  routed model metadata but does not act on it automatically.
- Runtime image model refresh, embedding model refresh, built-in live provider
  catalog refresh, and credential-backed discovery remain deferred until each
  surface has explicit ownership and validation semantics.
- Source ranking, citation rendering, provider-specific citation UI policy, and
  broader document/PDF processing remain deferred and caller-owned.
- Mistral file image references, built-in connector tools, append/restart
  lifecycle operations, and broad catalog expansion remain deferred.
- Full JSON Schema runtime support, including `$ref`, formats, conditionals,
  and default argument coercion, remains deferred.
- Broader provider-neutral structured-output mappings remain deferred until
  each provider family has explicit request, response, and fallback semantics.

## Validation status

Current v0.6.0 development state validated on 2026-07-03 with:

- `mise run mise:validate`.
- `mise run go:fmt`.
- `mise run go:fmt:check`.
- `mise run go:test`.
- `mise run go:vet`.
- `mise run ci`.
- `git diff --check`.
