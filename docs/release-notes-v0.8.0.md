# Release notes: sigma v0.8.0

This is the maintainer-facing development note for the next `sigma` tag. Add
the v0.8.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

HTTP retries now follow a provider's `x-should-retry` header, retrying a
response the provider marks retryable and stopping on one it marks final, and
accept fractional `Retry-After` and `Retry-After-Ms` delays. The retry
documentation now lists 408 and 409 and describes a zero `WithMaxRetryDelay`.

Cancelled streams now always deliver their synthesized terminal event. When a
slow consumer had left an earlier event unread, the terminal event was dropped
and the channel closed without one; the stale event is now replaced instead.

When sigma's own `WithTimeout` elapses during a text stream, the stream now ends
with `StopReasonError` and a transient error that still matches
`context.DeadlineExceeded`, so retry and route-fallback advice apply. Caller
cancellation and caller-owned deadlines remain aborts.

Cached Codex WebSocket connections are now retired after 55 minutes, before the
backend's 60-minute connection limit, so long sessions open a fresh connection
instead of failing on a connection the server is about to close.

Codex requests with `CacheRetentionNone` now drop session affinity entirely,
omitting session headers and the prompt cache key and skipping cached WebSocket
continuation, matching the direct Responses adapter and the reference
implementation.

Codex requests with `WithReasoningLevel(ThinkingLevelOff)` now send the model's
off effort (`none` for GPT-6 Luna and Sol) rather than omitting reasoning, which
ran the request at Codex's default effort.

Anthropic subscription requests now identify as Claude Code 2.1.280 rather than
the outdated 2.1.75.

Token estimates now use 3.5 characters per token instead of 4, matching the
reference implementation. Context-based output limits are slightly more
conservative, reducing context-limit request failures.

Generated Azure OpenAI Responses models now send `api-version=v1`, the default
of the `/openai/v1` API they call. The previous `2025-04-01-preview` value is a
legacy-route version outside the v1 API's accepted values (`v1`, `preview`).
`azure.WithAPIVersion` still overrides the value per request.

`sigma-surface-probe` now redacts the request credential and recognized secret
shapes from provider error bodies it reports during model discovery and
Fireworks capability lookups, matching the redaction applied elsewhere.

`EmbedBatch` splits and averages an input larger than the batch byte limit only
when `SplitOversized` is set. Without it, the call now fails with
`ErrInvalidOptions` before any provider request instead of returning a lossy
averaged vector the caller did not ask for.

Bedrock Nova, Titan image, and variable-size Cohere embeddings now honor
`EmbeddingRequest.Dimensions`, and Nova query embeddings use the
`GENERIC_RETRIEVAL` purpose. Explicit provider options still take precedence,
and fixed-size Cohere v3 requests are unchanged.

`SplitRetrievalText` no longer crawls forward one rune at a time after a chunk
shorter than the overlap, which produced suffix fragments such as `" Heading"`
and `"eading"`, and it omits whitespace-only chunks that embedding rejects.
`AddDocuments` therefore succeeds for documents that open with a short heading.

Radius final messages now carry a cost estimate from the gateway catalog's
pricing, as other adapters do. Radius also keeps text and tool-call thought
signatures from the stream and replays them, so tool loops on Gemini-backed
gateway models no longer lose the signatures those models require.

Responses that report `service_tier: "fast"`, as GPT-6 Fast mode does, are now
priced like priority processing instead of at the standard rate.

Fourteen Mistral models and the two Bedrock Ministral 3 models now advertise
image input, so image requests reach the provider instead of failing local
capability checks.

Catalog pricing corrections: direct MiniMax-M3 now charges its base rate below
512K input tokens and accepts its full 1M context and 512K output; 27 Mistral
rows price cached input at a tenth of the input rate rather than zero; and 28
geographic and in-region Bedrock Claude profiles include AWS's 10% regional
premium. All values match the reference catalog.

The models.dev catalog refresh now imports context-length price tiers, filling
rates a tier omits from the base price, so refreshed and newly added rows carry
current long-context pricing. Rows whose source lists no tiers keep their
curated tiers.

Twenty text rows gain the long-context price tiers their providers charge,
matching the reference catalog: direct Gemini 2.5 Pro and 3.1 Pro, xAI Grok 4.3
and 4.5, OpenCode and OpenCode Go Grok, GPT, Gemini, MiniMax M3, and Qwen rows,
OpenRouter GPT-5.6, Vercel AI Gateway and GitHub Copilot GPT-5.4 and 5.5. Cost
estimates for prompts above each threshold previously used the base rate.

An explicit OAuth minimum validity is now a requirement on the refreshed token.
When a provider's tokens are shorter-lived than the requested minimum, the
rotation is persisted and the request fails with an error wrapping
`ErrCredentialUnavailable` instead of silently dispatching a token that expires
too soon and refreshing again on every request.

Anthropic, Codex, and Radius browser logins validate the callback state before
anything else. Requests without the login's state, non-GET requests, and
callbacks without a code are answered with an error page while the login keeps
waiting, so a stale tab or cross-site request can no longer abort it. A
state-matching provider `error` still ends the login, and Codex now reports it
as an authorization failure.

Anthropic browser login falls back to a free loopback port when port 53692 is
reserved or in use, for example by Hyper-V/WSL port exclusions, and then to
pasted input alone. Codex browser login completes with `OnManualCode` input when
port 1455 is held by the Codex CLI. Both previously failed with `address already
in use`.

`ValidateToolCall` accepts schema shapes common in zod, Pydantic, and MCP tool
definitions. ECMAScript `\uXXXX` pattern escapes are translated for Go's RE2
engine, lookaround and backreference patterns are treated as annotations,
array-form tuple `items` with `additionalItems` are validated by position, and
`patternProperties` keys are validated and allowed under
`additionalProperties: false`. These shapes previously rejected every call as a
malformed schema; invalid regexes are still rejected.

Final streamed tool-call arguments containing invalid escapes or raw control
characters inside strings are now repaired for every adapter, matching the
existing Anthropic behavior. They previously reached callers as raw strings and
failed tool validation. Truncated arguments remain unrepaired text.

Mistral tool results keep their string `function.result` shape, but images now
follow the consecutive results as `image_url` chunks in a user entry rather than
inlined base64 text that the model could not see and that inflated the prompt.
Failed tool results are prefixed with `[tool error]`, image-only results read
`(see attached image)`, and empty results send `(no tool output)`.

Anthropic streams that end with an unrecognised stop reason now return a typed
provider error, as the Bedrock, Responses, and Chat Completions adapters do.
`model_context_window_exceeded` is reported as a context overflow, so overflow
handling and route fallback apply where it previously looked like a normal
completion.

Anthropic streams now fail with a typed provider error when a server-side model
fallback starts after output has been produced; previously the refusing model's
partial text and the fallback model's answer were returned as one successful
turn. A fallback before any output remains transparent.

Chat Completions thinking now records the field it streamed in, and same-model
replay sends it back in that field when no reasoning details are preserved.
Thinking tool loops on Moonshot Kimi and similar models previously omitted the
`reasoning_content` those providers require. Older saved blocks without the
recorded field replay as before.

OpenRouter routing options now apply only to requests sent to OpenRouter,
including custom provider IDs that use an OpenRouter base URL. A client-wide
OpenRouter routing default previously added an OpenRouter `provider` field to
every Chat Completions request, which providers such as OpenAI reject.

gpt-oss requests on Groq, Cerebras, Cloudflare Workers AI, and Together now send
`reasoning_effort` for the requested level instead of silently dropping it.
Catalog validation rejects unknown Chat Completions reasoning formats, such as
the `openai` value that disabled reasoning on the Together row.

Bedrock Converse now sends reasoning effort to OpenAI models: gpt-oss receives
`reasoning_effort` clamped to low through high, other GPT models nested
`reasoning.effort`, and `minimal` is sent as low. Levels were previously
dropped, and thinking budgets no longer send an Anthropic `thinking` block to GPT
models. The generated Bedrock gpt-oss and GPT-5.x rows now advertise reasoning,
and the GPT-5.x rows accept `xhigh`, so built-in models can use these levels.

Bedrock Claude adaptive thinking for Opus 4.7 and later, Sonnet 5, Haiku 5, and
Fable 5 now sends `thinking.block_binding` with `drop_block` and the
`thinking-binding-controls-2026-08-01` beta outside GovCloud, matching the direct
Anthropic adapter. Replaying signed thinking after the system prompt or tools
change drops stale blocks instead of failing with an invalid signature.

Claude adaptive thinking on Anthropic and Bedrock sends `xhigh` effort only to
Opus 4.7 and later, Sonnet 5, Haiku 5, and Fable 5, or where model metadata maps
`xhigh` explicitly. Sonnet 4.6 and similar models now receive `high` instead of
an unsupported `xhigh`. Bedrock also uses adaptive thinking for Haiku 5 models.

Turning thinking off on Google and Vertex Gemini models that cannot disable it
now requests the lowest level their metadata supports: `LOW` for Gemini 3.7 and
3.8 Flash, and the minimal budget for Gemini 2.5 Pro. These requests previously
sent `MINIMAL` or a zero budget, which the provider rejects. Models that can
disable thinking are unchanged.

Codex WebSocket requests fall back to SSE only when the connection fails before
any output, or when a connection-limit or missing-continuation error persists
after its retry. Provider error frames, failed responses, and authentication or
payload errors are returned with diagnostics instead of resending the request
over SSE and switching the session to SSE.

Responses stream `error` events with OpenAI's documented top-level `code` and
`message` fields now return typed provider errors over SSE and Codex WebSocket.
Context-length errors are recognized as overflow and server errors as transient,
where they previously surfaced as a generic stream error.

Mistral conversations that end with `stop_reason: "error"`, which Mistral uses
for transient server failures, now return a transient `server_error` with retry
advice. Unknown stop reasons remain non-retryable errors without the event type
as their provider code.

Stream failures without a typed network cause are now classified from their
message. HTTP/2 stream resets and GOAWAY closures while reading a response body
are transient with retry advice instead of non-retryable provider errors.

Context-overflow classification recognizes z.ai CN "Prompt exceeds max length",
DashScope/Qwen "Range of input length should be", DS4 "configured context size",
and generic "too many tokens" errors. Rate-limit wording such as Bedrock
throttling remains excluded.

Temporary provider capacity errors now classify as transient with retry advice:
`server_busy` codes, "servers are currently busy", "Selected model is at
capacity", and Azure "currently experiencing high demand" peak-load rejections,
including the Azure form reported under a generic invalid-request code.

OpenRouter image generation now requests text output only from models that
produce it. Generated metadata records `outputModalities` for all 50 OpenRouter
image models; the 39 image-only models, including FLUX, Recraft, and Seedream,
receive `modalities: ["image"]` instead of a combination OpenRouter rejects.
Caller-registered models without that metadata and explicit `modalities`
options keep their existing behavior.

GPT-5.6 Luna, Terra, and Sol estimates on Azure now match direct OpenAI pricing,
and the Bedrock rows use the 1.1x in-region rate with a 1.05M context window and
272K long-context tier. Bedrock `au.anthropic.claude-opus-4-6-v1` now carries
Opus 4.6 pricing. These rows previously overestimated cost by up to 5x; no
adapters or live-validation claims change.

Responses replay sends a thinking block as a reasoning item only when OpenAI can
resolve it: with same-model encrypted content, or by its provider item ID when
the request sets the `store` provider option. Reasoning models used without a
reasoning level, foreign thinking, and blocks without IDs previously produced
reasoning items that `store: false` requests reject. Saved histories are
unchanged.

Responses streams that omit `output_index`, such as llama.cpp, keep each output
item separate. Previously every event mapped to the first slot, so parallel tool
calls could return with another call's ID, name, or arguments. A tool call that
reuses another call's output index now ends the stream with a typed provider
error instead of overwriting it.

Anthropic message-anchored tool loading now places `tool_reference` blocks
inside the content of the `tool_result` that loads them, as the Messages API
requires. The tool's original output follows every tool result as sibling
blocks, so the conversation cache marker lands on that output rather than on a
reference.

Chat Completions streams no longer start a text block for an empty `content`
delta. Servers such as vLLM and OpenRouter open with `{"content":""}`, which
previously left an empty text block that made tool-only turns fail persistence
validation and placed reasoning after the answer.

Context-overflow messages reported under generic invalid-request codes, such as
Anthropic `prompt is too long` and Bedrock `Input is too long for requested
model`, now classify as context overflow with split-recoverable advice.
`IsContextOverflow` applies the same precedence to final-message diagnostics and
recognizes overflow already detected by provider adapters. Other invalid-request
errors keep their existing classification.

A started OAuth refresh now completes and persists its result even when the
requesting caller is canceled. Stored-credential refresh and the Codex,
Anthropic, GitHub Copilot, Kimi, xAI, and Radius token providers bound the
refresh request and persistence callback by a 15-second timeout instead of the
caller's context, so providers that rotate refresh tokens no longer leave the
only valid token unsaved. Cancellation before or during the ownership wait is
unchanged.

OpenAI Codex requests now use the ChatGPT Codex Responses endpoint over SSE and
WebSocket. Generated Codex models previously posted to `/backend-api/responses`;
base URLs ending in the backend root, `/codex`, or `/codex/responses` now all
resolve to `/codex/responses`. Explicit `endpoint` provider options are unchanged.

Google streams preserve explicitly empty signed text after thinking or tool
calls, so Gemini and Vertex replay signatures on their original blocks. Exact
provenance rules and caller-owned histories are unchanged. Generic stream error
wrappers retain credential failures as authentication errors and retryable
network failures as transient errors, restoring routing advice without automatic
request replay. Synthesized cancellation finals retain provider/model identity
before any event, after partial output, and during independent collector
cancellation. Initial pending snapshots and accepted terminals are unchanged.

Embedding adapters validate consistent nonempty dimensions against the final
payload sent by the responding HTTP attempt, including existing overrides.
Malformed controls fail locally; malformed successes retain typed provider
errors and attempt metadata with no vectors, retries, splitting, or cache writes
from that response. Shared batch checks also cover custom providers, split
responses, and cache-hit combinations. Cache keys advance to version 3: old
entries remain stored but are bypassed, so subsequent requests may regenerate
embeddings and incur provider costs. The image comparison example now uses a
registered model ID. Catalog contents and live-validation claims are unchanged.

Bedrock reasoning replay now omits signatures for non-Claude models and converts
unsigned Claude thinking to ordinary text. Signed and redacted Claude blocks and
persisted histories retain their existing behavior. Manual Claude thinking
budgets respect the effective output cap with 1,024 tokens reserved for output;
thinking is disabled when a minimum 1,024-token budget cannot fit. Enabled manual
thinking resolves an absent output cap from model metadata, falling back to
1,024. Interleaved thinking with tools in the final request retains the exception
allowing a larger budget. Adaptive and non-Claude controls remain unchanged.

Anthropic server-tool invocation counts now remain in raw usage without being
added to token totals. Raw usage accumulates supplied fields across sparse stream
updates, preserves unknown metadata, and applies explicit zero/null replacements
without mutating earlier snapshots. Token totals may therefore decrease for
responses using server tools, while invocation counts remain available in
`Usage.Raw.server_tool_use`. The stored-auth example now correctly wraps its
registry in `WithRegistry`. These corrections add no public APIs or persistence
migrations and are covered by deterministic tests, without live-provider claims.

Responses replay now generates distinct item IDs for parallel tool calls and
reasoning blocks, reserving native IDs before allocating synthetic ones. A shared
request mapping keeps normalized function/custom calls and results associated,
including punctuated, long, and compound IDs. Wire IDs can change when repair is
needed; persisted histories retain their original identities and opaque fields.
Signatures and encrypted content are sent only for matching nonempty
provider/API/model provenance across OpenAI, Azure, and Codex Responses routes.

OpenAI, Gemini, Vertex, and Bedrock embeddings accept successful response bodies
up to 256 MiB instead of silently truncating at 16 MiB. Oversized successes return
explicit typed provider errors with status, request identity, and attempts;
they are not retried, partially returned, or cached. Existing batch-splitting and
vector-validation rules continue to apply.

Native Vertex text, images, and embeddings and OpenAI/Anthropic MaaS routes now
apply resolved routing, headers, and provider options before constructing each
HTTP attempt. Caller overrides, credential modes, token-provider precedence,
cancellation, and final header suppression remain in effect. Retries obtain fresh
auth defaults. These corrections add no public API or persistence migration.

Bedrock requests signed with AWS access keys now double-encode the canonical
request path as SigV4 requires. Model IDs containing `:`, such as `…-v1:0`,
and inference-profile ARNs previously produced signatures that AWS rejected.
Bearer-token requests are unchanged.

On models that advertise thinking support, `ThinkingLevelMap` now overrides
provider values rather than listing every supported level. Unmapped levels
through `high` stay supported and are sent as the level text; `xhigh` and `max`
still require an entry, and `UnsupportedThinkingLevels` rejects a level. This
restores low through high reasoning on generated GPT-5.x rows. Models without
`SupportsThinking` keep the previous behavior, where the map lists every
supported level.

The Claude Code identity is now limited to Anthropic subscription tokens. GitHub
Copilot and Kimi Coding OAuth credentials on Anthropic Messages routes no longer
receive the Claude Code system prompt, betas, or client headers.

The September model registry review adds 50 text models across existing routes,
GPT Image 1.5 and 2, and text input for Gemini Embedding 2. It updates 18 existing
text rows and removes 167 text entries, three OpenRouter image previews, and
direct Gemini `text-embedding-004`. That review produced 548 text,
56 image, and 8 embedding models. Removals include IDs absent from the comparison
inventory across matching providers and confirmed direct-provider shutdowns.
Removed IDs no longer resolve through default discovery; applications must
explicitly choose replacements. Histories and stored embeddings are unchanged.

The October Vertex review brings its catalog to 39 text models (14 native
Gemini, 17 Claude, and eight OpenAI-compatible MaaS), three Gemini image models,
and one embedding model. The complete offline catalog now contains 562 text,
58 image, and 8 embedding models. Additions use existing adapters; model and
region access still depend on the caller's account. No runtime route or release
classification changes, and live validation remains opt-in.

Vertex Gemini Pro estimates now apply documented pricing above 200,000 input
tokens, including cached input. Newer Claude models retain flat global rates
across that boundary. Claude Sonnet 4.6's output limit, Llama 3.3's pricing and
output limit, and Gemini image aspect ratios are corrected. Deprecated models
with future retirement dates remain discoverable. Existing restrictions on
named thinking levels for Vertex latest aliases remain in place; Grok and GLM
MaaS entries preserve provider-default reasoning without advertising named
controls that the adapter cannot express.

Vertex embeddings add `gemini-embedding-001`, with default batching limited to
one input per request. Default Vertex discovery excludes
`gemini-3.1-flash-lite-image`, `text-embedding-004`, `text-embedding-005`, and
`text-multilingual-embedding-002` because they are absent from the reviewed
inventory. These are catalog-policy exclusions, not provider-retirement claims.
Text models represented by equivalent versioned Claude IDs remain discoverable.
Model selection and vector-store migration remain explicit; stored vectors,
histories, caller-registered metadata, and other providers are unchanged.
Image cost metadata records baseline output prices; larger resolutions can cost
more. Deterministic tests cover routing, reasoning payloads, batch splitting,
pricing boundaries, and removal boundaries.

Pricing remains provider-specific and estimated. New entries preserve existing
route, authentication, context-default, and transport policies. Google Flash
introductory pricing and upcoming model retirements require follow-up before
their published deadlines.

Synchronous provider errors now redact recognized credentials in their displayed
messages while retaining the original causes for `errors.Is` and `errors.As`.
Responses results preserve all output-text and refusal parts in content-index
order, including final-only and deferred responses, with one public text block
per output item. Terminal snapshots no longer replace the message with its last
part or duplicate accumulated text.

All built-in embedding decoders reject absent, null, empty, non-array, or
malformed vectors and numbers that overflow float32. Valid zero-valued vectors
remain supported. Malformed HTTP successes return typed provider errors with
status, request ID, model identity, and attempts, without partial vectors or
cache writes. Existing dimension policies and cache identities are unchanged.

OAuth callers canceled while waiting for another caller's refresh now return
promptly. Credential inspection, rotation, and persistence callbacks remain
serialized. Strict tool validation preserves valid explicit nulls, including
unconstrained optional properties, and removes optional null placeholders only
when the original schema rejects null. Combined type, enum, const, and supported
union constraints determine strict-schema nullability; malformed-schema errors
remain errors. These changes require no persistence migration.

Shared copies now preserve nil lists as `null` and non-nil empty lists as `[]`
across tool content, provider options, metadata, and credentials. This corrects
serialization that previously depended on the Go slice type; existing byte and
raw-JSON conventions remain unchanged. Tool validation compares nested numbers
mathematically, so `1`, `1.0`, and `1e0` are equivalent within object and array
`const`/`enum` constraints, including exclusions through `not`.

Codex WebSocket handshake, proxy, and retained session errors redact recognized
credential material and bound diagnostic previews, including when SSE fallback
succeeds. OpenAI tool-schema conversion, WebSocket requests, and continuation
caches preserve exact schema numbers, and changes to large integer constraints
invalidate continuation reuse. OpenAI embedding successes now require exactly
one explicit, unique, in-range index per input. Malformed responses return typed provider errors with
the original HTTP status and attempt metadata, without partial vectors; valid
out-of-order responses still return in input order. These corrections add no
public APIs, dependencies, catalog changes, or history migrations.

Tool replay now preserves empty argument objects throughout content cloning and
handoff, including objects nested in arrays. Previously persisted explicit nulls
remain unchanged; no history migration is attempted. Chat Completions uses one
deterministic mapping for function calls, custom calls, and results, preserving
safe IDs while distinguishing composite IDs and long or punctuated IDs. Transformed
wire IDs may change. Developer instructions no longer prematurely close tool
exchanges: repair matches actual results, synthesizes only missing results, then
emits held instructions before role conversion and compatibility bridge insertion.
Handoff reports retain original source indices and final inserted-message indices.

Tool validation now applies supported constraints without a repeated type in
reference siblings and composed schemas. Previously accepted out-of-range
arguments may fail validation. Opt-in coercion resolves nested and recursive local
references against the original root while preserving valid union values and
caller-owned inputs. The comparison guide now reflects supported references,
formats, and conditionals. Shared SSE parsing ignores data-free control frames;
explicit empty data and malformed JSON retain their existing error behavior.
Public APIs, dependencies, persistence formats, catalog data, and provider
capabilities are unchanged by these corrections.

Client-default tool choices now reach provider requests, and incremental tool
events isolate nested arguments and metadata from provider state and final
results. Exact numbers and empty argument objects survive these copies,
including aborted finals. Bedrock now accounts for one-hour cache writes and
rejects missing completion reasons while retaining partial output and trailing
usage. Codex WebSocket proxy exclusions now handle parent domains, wildcard
lists, ports, and IPv6 literals. Documentation also reconciles the existing
credential chain, image adapters, and redacted debug hooks.

Internal evaluations now pair Go subtests by stable case ID and validate
operational requirements before judging. Missing measurements from any model
turn keep the corresponding token or cost aggregate unavailable rather than
reporting misleading subtotals. Artifact schema v2 records immutable inputs and judged outputs,
named judgments, evaluated prompts/tools, and allowlisted controls before
provider request adjustments. Old artifacts are not migrated, iteration
metadata remains v1, and public Sigma APIs are unchanged. Auth timeout tests
now synchronize resolver completion before inspecting counters, eliminating
an intermittent race-detector failure.

Evaluation comparisons now exclude independently failed or skipped tests while
retaining threshold-only scores. Threshold failures are reported during cleanup
and remain separate from CLI operational failures. Submitted table runs that
fail before dispatch retain their comparison identities and diagnostics;
never-submitted runs remain outside this accounting. Numeric tool-event arguments
reach judges as exact `json.Number` values. Artifact and iteration schema
versions remain unchanged. The provider parity guide also now documents the
existing direct OpenAI background submit, single-fetch, and cancel lifecycle
while retaining the deferred boundaries for other routes and orchestration.

Tool arguments now retain exact JSON numbers through persistence and replay on
Anthropic, Google, and Bedrock, including shared Vertex routes. Anthropic hosted
search, fetch, and code-execution results survive in ordered block metadata and
replay only with matching provider/API/model provenance. Old histories remain
readable, but results discarded by earlier versions cannot be reconstructed.

Request preparation removes tool exchanges and opaque reasoning from failed or
aborted assistant turns while retaining nonblank visible text. Responses keeps
its existing whole-turn omission policy. Stored history and partial finals are
unchanged, and handoff preserves the information needed to apply this policy
at dispatch. Anthropic also omits blank text and empty messages, preserves valid
signed thinking and empty tool results, and rejects all-empty requests locally.

Google and Vertex text prompt blocking now terminates as content filtering.
`MALFORMED_FUNCTION_CALL` and `UNEXPECTED_TOOL_CALL` now return typed,
non-retryable provider errors and emit `error` events, with partial content,
usage, and raw finish reasons retained. Callers that previously expected a nil
error for these stop reasons must handle the returned failure.

Google Vertex file-read probes now explicitly request only a `read_file` call
for `README.md`, without asking for the file contents. This removes missing
argument and response-sequencing ambiguity from both automatic and forced tool
cases; provider-generated malformed calls still surface as failures.

Failed xAI JSON-object probes now compare an explicit formatting instruction,
a matching JSON schema, and plain-text mode using the same user prompt, plus
three JSON-object variants with string, number, and alternate boolean values.
All retain the output budget. Successful controls and per-attempt errors provide
evidence for persistent failures while the original safety rejection remains
inconclusive.
An explicit `-json-text` mode runs the same JSON-object prompt in plain-text mode
and validates the completed answer locally. It reports a separate `json_text`
case and does not claim provider-enforced JSON support or downgrade application
requests automatically.

Bedrock stream closure now cancels outstanding work and releases idle transports.
AWS credential/config file overrides replace their respective default paths;
applications relying on fallback to an overridden home-directory file must
configure their intended credential source explicitly. The provider guide now
covers the existing default chain, caching, and resolver-only configuration.
Tool validation now rejects numeric arguments that previously passed because of
floating-point rounding, and decimal-string coercion retains exact values.
Typed JSON-compatible containers are isolated across content, registry, option,
and credential copies; opaque Go objects remain caller-owned. Tool streaming
avoids redundant argument-prefix copies, and weighted embedding combination
avoids overflow from finite coordinates and integer weight totals.

Surface probes now preserve recognized safety rejections as inconclusive
failures. Later successful variants remain diagnostic controls and do not
produce token-budget repair claims or recommendations for those cases.
OpenCode Zen/Go now send caller session IDs as `x-opencode-session` across
routed APIs even when caching is disabled, preserving explicit header overrides
and suppression. Surface probes generate separate conversation IDs per case,
reuse them across turns, retries, and repairs, and report `MissingSessionID` as
a request-shape error.

`sigma` v0.8.0 begins by tightening native Gemini 3 replay compatibility across
Google Generative AI and Vertex AI so function calls and matching tool results
retain stable normalized IDs. Google and Vertex streams now also preserve
explicit max-token, provider-error, and unknown finish reasons when a response
contains function calls, so only a normal `STOP` is promoted to tool-call
completion.
Google and Vertex replay now also retain blank signature-only text and thinking
parts only when the signature is valid for the same provider, API, and model.
Amazon Bedrock Converse Stream service exceptions also retain their requested
model and AWS request ID for diagnostic correlation.
Replayed Bedrock tool inputs now remove provider-rejected empty object-member
names without altering stored or streamed tool arguments.
Bedrock Converse also preserves encrypted reasoning blobs across split stream
deltas and replays them through the provider's scalar base64 wire shape without
exposing opaque reasoning as visible text.
Qwen Token Plan now exposes Qwen3.8 Max under its generally available model ID
across both regional routes while preserving supported reasoning levels and
keeping Qwen3.7 Max toggle-only. A distinct Individual subscription route adds
eight curated models, including DeepSeek V4 Pro 0813, through the shared
international endpoint and credential, with each model's thinking controls
preserved. Z.ai and Z.ai Coding CN now add GLM-5.2 Highspeed and GLM-5.3 with
million-token limits, model-specific reasoning aliases, and evidence-backed
estimated pricing. Enabled Z.ai reasoning requests now also preserve exact
same-provider/API/model reasoning across turns for provider-side caching.
Baseten is now available through a first-class
OpenAI-compatible route with focused GLM 5.2 and Kimi K2.6 metadata and native
chat-template thinking controls. GLM 5.2 now advertises both text and image
input so image-bearing tool results remain available on that route. Xiaomi's
direct and regional Token Plan catalogs now expose only the supported MiMo V2.5
text-provider lineup instead of retired V2 model names.
Curated OpenRouter text routes now carry reviewed optional or mandatory
reasoning behavior and exact effort availability. Optional routes explicitly
send `reasoning.effort: "none"` when reasoning is omitted; mandatory routes
retain provider defaults and reject explicit off or unsupported efforts before
dispatch.
OpenCode Zen and Go now expose the complete current 63- and 27-model
catalogues. The generated refresh adds 26 models, removes ten no longer
advertised, and reconciles routed APIs, inputs, thinking levels, pricing,
cache pricing, and token limits. Known live-probe models now resolve their
route from generated metadata, and region opt-in failures are reported as
upstream availability rather than an inconclusive request-shape failure.
The curated OpenRouter image catalog now also includes Seedream 5.0 Lite and
Pro, Qwen Image 3 and 3 Pro, Meta Muse Image, Grok Imagine Image 2.0, and four
Recraft V4 Styles variants through the existing image adapter. Their estimated
image costs remain zero until independently verified pricing is available.
Fireworks GLM 5.2 routes now use session affinity for automatic
prompt caching without unsupported long-cache retention. Anthropic-routed
OpenRouter agent loops now advance their final conversation cache breakpoint
through the latest non-empty tool result.
OpenCode Zen and Go DeepSeek V4 Flash routes support low reasoning effort while
retaining their high and maximum-effort mappings. The September registry
review removes direct V4 Flash and its experimental vision variant from default
discovery. Direct V4 Pro retains documented peak-rate cost estimates.
Direct xAI now includes Grok 4.6 through OpenAI Responses with text and image
input, function tools, 500k-token context and output limits, tiered
long-context pricing, and reasoning controls through `xhigh`.
Anthropic Messages streams now surface text and thinking delivered with
content-block start events immediately through incremental output. Refusal
stops now also retain non-empty structured provider details for callers that
need the refusal category or explanation. Direct Claude Fable 5 requests can
additionally opt into catalog-declared server-side refusal fallbacks without
changing default requests; known fallback responses retain the requested model
identity while reporting usage and estimated cost against the returned model.
Direct Claude Opus 5 now preserves the provider-native thinking effort on
partial and terminal assistant messages and replays it for exact matching prior
turns, preventing signed-thinking mismatches when effort changes between turns.
In-progress text streams now identify every partial assistant snapshot as
pending, beginning with an empty snapshot on the initial start event.
OpenAI-compatible Chat Completions models can also opt into successful
`[DONE]` termination when their endpoint does not emit `finish_reason`.
Indexed OpenAI-compatible Chat Completions tool-call continuations now retain
the first provider-issued ID and name across mixed text, reasoning, and tool
deltas, preventing identity drift during execution, persistence, and replay.
OpenAI-compatible Chat Completions streams now also retain validated encrypted,
signed-text, and summary reasoning details in their original order across
assistant-content persistence and exact same-provider/API/model replay,
including responses without tool calls.
OpenAI-compatible Chat Completions, Responses, and Azure Responses requests can
now carry request-scoped arbitrary sampling parameters with explicit override
precedence. Caller-registered models can also declare default arbitrary
sampling fields that remain below Sigma's core and typed request values,
request-scoped sampling overrides, and raw provider body overrides. OpenAI
Responses-compatible and Azure OpenAI Responses requests now also clamp typed
output-token limits below 16 to the accepted request minimum.
Models can now opt out of that automatic output-token field through
`OpenAIResponsesCompat.SupportsMaxOutputTokens`. Cache-options-capable
Responses models now also map automatic long retention to
`prompt_cache_options.ttl: "30m"`, including the existing direct GPT-5.6 rows.
Direct OpenAI Responses requests can now run in the background through an
explicit provider-neutral lifecycle. Callers receive a JSON-serializable,
provenance-bearing handle, can perform one status fetch at a time, and can
request cancellation without changing ordinary streaming or completion
behavior. Completed and incomplete background responses use the same content,
tool, usage, cost, metadata, and error conversion as streamed Responses.
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
mutating registry state. Explicit model-policy enablement now retries throttled
requests within a five-second bound while remaining caller-invoked.
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
The opt-in surface probe now also exercises Anthropic Claude through Vertex
`streamRawPredict`, using catalog-backed model selection and the existing
explicit Vertex routing and credential contract.
Image probe mode now also exercises the Google Gemini API and Vertex AI image
adapters with generated Gemini 2.5 and 3.1 image metadata. Explicit model
selections are validated locally, every image case has an independent deadline,
and success requires non-empty base64 or URL image data.
Reviewed OpenAI Responses and Codex Responses models now use native,
message-anchored additional-tool input, and streamed tool-call namespaces are
retained only when their loading context can be replayed safely. Codex Responses
SSE and WebSocket streams also recognize `response.done` completion and retain
explicit `end_turn` values as opaque diagnostics. OpenAI, Azure, and Codex
Responses streams now also retain non-empty assistant message phases through
persistence, with recognized commentary and final-answer boundaries replayed
only to the exact provider, API, and model. Later Responses requests now also
omit failed or aborted assistant turns and their associated tool results from
wire history, avoiding incomplete reasoning and call pairings while retaining
the partial finals locally for callers. Provider failures that report an
exhausted upstream request buffer are now classified as retryable for
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

- Direct OpenAI and OpenAI Codex now include `gpt-6-astra` through their
  existing Responses adapters, with text/image input, function tools,
  low/medium/high/xhigh/max reasoning, and message-anchored deferred tools.
  Explicit off and minimal reasoning are rejected locally. Direct OpenAI
  supports explicit prompt-cache mode and 30-minute long retention; Codex
  keeps its existing cache behavior and output-token field omission.
  Both catalog rows use Sigma's conservative 272,000-token context default
  and a 128,000-token output limit, although the documented API context
  maximum is 1,050,000 tokens. USD estimates per million tokens are
  10 input, 50 output, 1 cache read, and 12.5 cache write; requests above
  272,000 input tokens use 20/75/2/25 for the full request. Codex costs are
  API-equivalent estimates, not subscription charges. Catalog inclusion does
  not guarantee account access. See the
  [model documentation](https://developers.openai.com/api/docs/models/gpt-6-astra).
  Provider maturity, existing models, and dispatch defaults are unchanged;
  this addition does not introduce async tools or mid-turn steering.

- OpenRouter image generation now exposes Seedream 5.0 Lite and Pro, Qwen
  Image 3 and 3 Pro, Meta Muse Image, Grok Imagine Image 2.0, and Recraft V4
  Styles, Styles Pro, Styles Vector, and Styles Pro Vector through the existing
  image route. Each row retains the shared OpenRouter endpoint, authentication,
  1024x1024 PNG capability contract, and a zero estimated image cost pending
  independently verified pricing.
- `SubmitDeferred`, `FetchDeferred`, and `CancelDeferred` provide an explicit
  provider-neutral lifecycle for durable text responses, initially backed by
  direct OpenAI Responses. `DeferredResponseHandle` is safe to serialize for
  later polling and pins the provider, API, model, and response ID so a handle
  cannot be dispatched to a different registered route. Queued and in-progress
  observations carry no assistant message; terminal output reuses the existing
  Responses parser, including reasoning, function and grammar tools, usage,
  estimated cost, routed-model metadata, and partial output on failures.
  Fetching performs exactly one request and cancellation returns the resulting
  provider status, including an already-completed response. Sigma does not
  automatically poll, resume background streams, or extend upstream retention;
  callers using the default non-stored request policy must retrieve results
  within the provider's temporary polling window.
- The international and China Z.ai Coding Plan routes now expose GLM-5.2,
  GLM-5.2 Highspeed, and GLM-5.3 as a consistent million-token cohort. GLM-5.2
  variants map Sigma's `minimal` through `high` levels to provider `high` and
  `xhigh`/`max` to provider `max`; GLM-5.3 maps `minimal`/`low` to `low`,
  `medium`/`high` to `high`, and `xhigh`/`max` to `max`. Omitted reasoning
  remains disabled, and explicit GLM-5.3 `off` fails locally. The China route
  additionally exposes GLM-4.6V with text and image input, Z.ai reasoning,
  streamed tools, a 128,000-token context window, a 32,768-token output limit,
  and `max_tokens` request compatibility. GLM-5.1, GLM-5.2, and GLM-5V-Turbo
  use API-equivalent estimated pricing across both routes, while unevidenced
  prices remain zero. The typed Z.ai reasoning format now defaults
  same-provider/API/model assistant replay to `reasoning_content` and sends
  `clear_thinking: false` whenever reasoning is enabled so provider-side
  reasoning state remains cacheable. An explicit
  `RequiresReasoningContentOnAssistantMessages` setting overrides the replay
  default; mismatched provenance is never replayed, and disabled or omitted
  reasoning retains the existing disabled payload without `clear_thinking`.
- `AnthropicOptions.EnableRefusalFallbacks` enables direct Anthropic
  server-side refusal fallback only for models with generated allowed-target
  metadata. Claude Fable 5 uses the ordered Opus 4.8 and Opus 5 targets.
  Disabled requests omit both the fallback payload and beta header; unsupported
  opt-ins fail before dispatch. Provider-reported declared fallback models
  drive usage identity and estimated pricing, while unknown returned model IDs
  remain diagnostic and retain requested-model accounting.
- `AnthropicMessagesCompat.SupportsMidConversationEffort` capability-gates
  provider-native thinking-effort persistence and replay. Direct Claude Opus 5
  records the resolved effort in `AssistantMessage.ProviderThinkingLevel` and
  persisted `Message` values, emits it on partial and terminal results, and
  inserts exact-provenance replay markers before matching assistant turns. An
  omitted reasoning level defaults to `high`; explicit off is unsupported.
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
- `OpenAICompatibleModelConfig.SamplingParameters` and
  `MetadataOpenAISamplingParameters` now let caller-registered models provide
  default arbitrary sampling fields for OpenAI-compatible Chat Completions,
  Responses, and Azure Responses. Required payload fields and typed request
  options override model defaults, request-scoped sampling maps override
  matching keys while retaining other defaults, and raw provider `extra_body`
  values retain final precedence.
- `OpenAICompletionsCompat.ThinkingTokenBudgetField` now lets custom compatible
  models select top-level `thinking_token_budget`, `thinking_budget`, or
  `thinking_budget_tokens` payloads when callers select reasoning and provide
  an explicit positive budget. `SupportsThinkingTokenBudget` remains a
  backward-compatible alias for `thinking_token_budget`, while an explicit
  field selector takes precedence. Sigma clamps the budget against the request
  or model output ceiling to preserve 1,024 tokens for visible output;
  sampling parameters and raw `extra_body` values retain their existing
  override precedence.
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
- `cmd/sigma-surface-probe` now exposes `google-vertex-anthropic` as an opt-in
  live route over the existing Vertex Anthropic provider. The route defaults to
  the built-in `claude-sonnet-4-6` row, validates explicit Claude IDs against
  Sigma's catalog without model discovery, and capability-gates image,
  reasoning, and tool cases from the selected model metadata. Anthropic tool
  cases use provider-neutral automatic choice or typed required choice instead
  of OpenAI-specific request options.
- `cmd/sigma-surface-probe -images` now exposes opt-in `google` and
  `google-vertex` routes. The direct route probes `gemini-2.5-flash-image` and
  `gemini-3.1-flash-image` through `generateContent`; the Vertex route probes
  the generated Gemini 3.1 row through Vertex `generateContent`. Direct
  credentials prefer `GOOGLE_API_KEY` over `GOOGLE_CLOUD_API_KEY`, while Vertex
  reuses the existing explicit project, location, OAuth-token, and API-key
  contract. Each case has an independent timeout bounded by the overall probe
  deadline, and successful responses must contain a non-empty base64 or URL
  image.
- Persisted tool-result messages can now retain optional `Usage` from
  caller-owned tool execution through replay and model handoff. This metadata
  is not serialized into provider requests and remains separate from
  assistant-usage context anchors, model-turn cost accounting, and evaluation
  usage totals.
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
- Direct DeepSeek retains V4 Pro with peak estimates of $1.32 input, $3.96
  output, and $0.044 cached input per million tokens. The September review
  removes direct V4 Flash and V4 Flash Vision Exp from default discovery.
  Caller-defined vision metadata still uses the existing Chat Completions adapter.
- Xiaomi's direct catalog provides `mimo-v2.5`, `mimo-v2.5-pro`, and
  `mimo-v2.5-pro-ultraspeed`. The regional Token Plan catalogs retain the first
  two IDs through their existing OpenAI-compatible Chat Completions routes.
- Direct xAI metadata now includes Grok 4.6 through the existing OpenAI
  Responses registration path. It accepts text and image input, function
  tools, and low, medium, high, or `xhigh` reasoning within a 500k-token
  context and output limit. Standard rates are $2 input, $6 output, and $0.50
  cached input per million tokens; requests above 200k input tokens use the
  $4, $12, and $1 rates.

## Reliability corrections

- Preserve supplied Responses cache-write usage in normalized and raw accounting,
  deduct cache reads and writes from ordinary input, and apply existing pricing
  and tier adjustments across streaming and deferred results, Azure, and Codex.
- Keep distinct adjacent reasoning blocks separate when IDs, formats, signatures,
  or supplied indexes conflict. Compatible fragments still merge, missing identity
  fields may be filled, and encrypted entries remain discrete and ordered.
- Accept OpenRouter image response bodies up to 64 MiB, with typed, safe overflow
  errors retaining HTTP status and request ID and no retry after body consumption.
- Apply schema container keywords to the actual value without inferring a type;
  preserve explicit types, nested opt-in coercion, full-schema validation, exact
  numbers, strict nullable semantics, and caller-owned data.
- Require exact nonempty provider/API/model provenance for modern and legacy
  reasoning replay. Stored histories remain readable and unchanged; incompatible
  reasoning is omitted while ordinary text, tool calls, and results are retained.
- Reconcile Azure, Codex, and Xiaomi provider documentation with registration
  helpers, generated metadata, auth paths, and Xiaomi regional token-plan routes.

Synthetic tool-call IDs for Google and OpenAI Chat Completions are now random,
opaque identifiers that remain distinct across turns and concurrent streams.
This includes custom Chat Completions tools and Vertex text through the shared
Google parser. Provider-authored IDs retain their existing behavior; already-invalid
persisted histories with duplicate IDs are not automatically migrated.

Google text, images, and embeddings and OpenRouter images now resolve rich auth
before building each request's route, payload, and headers. Each retry starts
from caller options and refreshes auth defaults once. Caller overrides, protected
credential headers, final header suppression, and request timeouts retain their
existing contracts. HTTP-client selection and embedding-cache namespaces remain
caller-owned.

OpenAI and OpenRouter inline image errors redact messages, codes, and error-type
metadata in structured results, including serialized results. Direct Google and
Vertex Gemini image results retain candidate finish reasons and prompt feedback
in metadata. Safety and token-limit stops preserve partial output without automatic
retry; explicit error reasons produce typed provider errors, and empty envelopes
without terminal evidence are rejected. Imagen's separate protocol is unchanged.

Oversized-input embedding batching now computes weighted averages in float64
before returning float32 coordinates, avoiding overflow from large finite inputs.
The result remains unnormalized. These corrections add no exported APIs,
dependencies, persistence schema changes, catalog changes, or provider promotions.

Tool-argument decoding preserves provider-authored JSON numbers as `json.Number`,
including structured initial inputs, partial deltas, cancellation snapshots,
persistence, and replay. Callers asserting `float64` must migrate to `json.Number`
and explicitly choose `Int64`, `Float64`, or a lossless representation. Token
counters and cost fields retain their existing types. Signed empty assistant text
can be persisted; provider-specific replay still checks format and provenance.

Deferred Responses resolve auth once per HTTP attempt before building the route,
payload, and headers. Retries rebuild submissions and preserve the successful
attempt's conversion metadata and service tier. Submit, fetch, and cancel timeouts
cover auth, retry waits, headers, and response decoding. Handles contain no
credentials or auth-derived endpoints.

Chat Completions `n` and Google/Vertex `candidateCount` must be omitted or numeric
one, including after overrides and payload hooks. Nonzero alternatives cannot
alter the accumulated result. Radius premature EOF retains partial content and
returns transient retry advice; post-body retries remain caller-owned.

External embedding caches now require a non-secret `CacheNamespace`. Version 3
keys retain namespace and a deterministic configuration digest; implementations
must honor every field. Older entries, including version 2 entries, are bypassed
without scanning, rewriting, or deleting them. Callers must change
namespaces when opaque tenant, endpoint, custom transport, or provider identities
change. Warm cache paths perform the same preparation and validation as cold
paths and honor cancellation without authenticating. Batch option functions run
once. Retrieval insertions validate complete batches before publication and
preserve existing results and inferred dimensions on failure or cancellation.

`mise run go:build` is now a CGO-disabled compilation check of `./...`, including
examples and command packages. It emits no binary or root archive, uses no
`main.version` linker flags, and is included in CI. `mise run clean` remains
available explicitly. The embedding guide covers OpenAI-compatible, Gemini,
Vertex, and Bedrock adapters without changing release classifications.

## Compatibility

- Existing histories remain loadable and retain opaque metadata. Outbound
  reasoning replay now omits both modern and legacy metadata when any source
  provider/API/model component is absent or mismatched; no history migration is
  required, and ordinary text, calls, and results are preserved.
- Responses with cache writes can report higher costs because writes now use
  their checked-in cache-write rates. Provider token totals are unchanged.
- Valid composed-schema arguments previously rejected by implicit container
  typing may now succeed. Explicit types and object-only tool arguments remain
  enforced.

- `bedrock.ConverseUsage` adds `LongCacheWriteInputTokens`, the one-hour subset
  of total cache writes. Callers using unkeyed literals must update them; keyed
  literals can omit the field. Root usage/persistence schemas and catalog rates
  are unchanged, but one-hour cache cost estimates now apply the correct rate.
- Client-default `ToolChoiceNone` now suppresses model tool selection unless
  overridden. Incremental tool-event mutations no longer affect later events
  or terminal arguments; terminal `FinalMessage` and `Stream.Final` retain
  their existing shared-result contract.
- Bedrock completion without a supplied stop reason now returns an error,
  classified as transient with retry advice. Received output and usage are
  retained; no automatic replay after streamed output is introduced.
- Codex WebSocket connections bypass configured proxies for matching parent
  domains and exact IPv4/IPv6 exclusions, respecting optional ports. Leading-dot
  and `*.` forms continue matching roots as well as descendants. `*` also works
  inside exclusion lists; malformed entries and CIDR remain nonmatching.

- Explicit `base_url` or `baseURL` request configuration blocks auth-derived
  defaults under either spelling. Provider constructor and model-metadata header
  maps now follow the same deterministic case-insensitive merging rules as
  client/request options, with existing precedence and suppression retained.
- OpenAI image streams require `image_generation.completed` or
  `image_edit.completed`; EOF and `[DONE]` alone no longer indicate success.
  Explicit empty completions remain valid. Available output and usage survive
  incomplete-stream failures, and partial previews remain progress events.
  Missing image completion and Chat Completions `finish_reason` classify as
  transient/retryable without automatically replaying streamed requests.
- Canceled `InMemoryCredentialStore` modifications and deletions stop waiting for
  provider ownership promptly. Active callbacks retain serialized ownership and
  their existing commit behavior; they remain responsible for cooperative
  cancellation. Different providers can still progress independently.
- Truncated JSON credential redaction also accepts LF/CRLF and mixed JSON
  whitespace around field colons, preserving neighboring safe content and
  bounded UTF-8 previews.

- Codex WebSocket sessions reuse idle connections only when provider identity,
  effective URL, and final handshake headers match. Changed credentials or
  routing start fresh continuation state; overlapping requests and concurrent
  connection creation retain independent ownership and cleanup.
- Codex WebSocket, OpenAI images (including streaming, edits, and variations),
  and embeddings apply auth-derived routes, headers, and provider options before
  request construction. Explicit request configuration retains precedence and
  credentials resolve once per attempt. Header merges ignore capitalization,
  preserve the winning spelling, and resolve duplicate variants within one map
  in lexical order, with the last variant winning.
- An accepted text or image terminal result survives subsequent parent or
  collector cancellation and explicit closure. Blocked terminal delivery can be
  abandoned to close promptly; queued events remain readable and `Final`/`Err`
  retain the accepted outcome. Before acceptance, cancellation retains aborted
  partials. Closing OpenAI image streams or the root generation fallback cancels
  provider work, including stalled response reads, without synthesizing aborts.
- Anthropic Messages completion requires start and stop markers and a nonempty
  provider stop reason, even for empty output. Incomplete streams retain partials
  and classify as transient; automatic retry behavior is unchanged.
- Credential-field redaction covers unterminated JSON strings through the end of
  diagnostics, including escape boundaries and multiline values. Preview bounds
  and UTF-8 safety remain unchanged. Public interfaces, serialized formats,
  provider scope, and generated catalog metadata are unchanged by this hardening.

- `OpenAIResponsesCompat.SupportsMaxOutputTokens` uses the existing tri-state
  `OpenAICompatSupport` values. Unspecified or supported retains typed output
  limits and the minimum of 16; unsupported omits their automatic serialization.
  Explicit model sampling defaults, request sampling parameters, and raw body
  overrides remain available, with their existing precedence. Codex still
  removes the field after all overrides. Local validation and context budgeting
  are unchanged, but opting out means the provider does not enforce the typed
  output cap. No catalog row enables this opt-out by default.
- `SupportsExplicitPromptCacheMode` now selects the automatic long-cache wire
  format as well as explicit no-cache mode. Flagged models use
  `prompt_cache_options: {"ttl":"30m"}`; unflagged models retain
  `prompt_cache_retention: "24h"`, and unsupported long retention remains
  omitted. Typed `PromptCacheRetention` stays literal under its existing
  capability gate. Either cache field in request sampling or `extra_body`
  suppresses automatic long-cache directives, including explicit null values.
  Raw body values retain final precedence without nested-object merging or
  repair of caller-authored conflicts. Unset, short, none, affinity keys, and
  existing Azure/Codex catalog capabilities remain unchanged. Direct background
  submission uses the same builder and compatibility behavior.
- OpenCode retains one Zen provider and one Go provider because regional model
  availability is controlled by workspace opt-in rather than a distinct API
  endpoint or credential. Models requiring China hosting may return a
  `RegionError`; live probes now classify that result as upstream availability.
- Per-turn Anthropic thinking-effort replay is enabled only for direct Claude
  Opus 5. Fable 5, Sonnet 5, routed aliases, and caller-registered models remain
  unchanged unless their exact model configuration explicitly opts in.
- GitHub Copilot Claude Fable 5 now uses its Anthropic-compatible Messages
  route instead of Chat Completions. Selected reasoning levels are sent through
  adaptive thinking controls while authentication, Copilot headers, image and
  tool support, pricing, context limits, and other provider routes remain
  unchanged.
- The curated OpenRouter Claude Sonnet 5, DeepSeek V4 Pro, Gemini 3.5 Flash,
  GPT-5.2 Codex, and GPT-5.6 routes now expose only their reviewed reasoning
  efforts. Optional routes map omitted or explicitly disabled reasoning to
  nested effort `none`; mandatory routes omit a disabled-reasoning payload by
  default and reject explicit off locally. Pricing, limits, routing,
  authentication, provider registration, and the curated model set are
  unchanged.
- The Vertex Anthropic surface-probe route is diagnostic-only and remains
  outside `mise run ci`. It requires explicit project and location values plus
  a caller-supplied API key or OAuth access token; Sigma does not add ambient
  credential loading, persistence, model discovery, provider IDs, catalog
  rows, or runtime request changes. Existing default probe routes remain
  `zen,go`.
- The Google image surface-probe routes are diagnostic-only and remain outside
  `mise run ci`. Image mode still defaults to `openai`; the new routes add no
  ambient credential loading or persistence. Image generation remains a
  preview surface.
- Generated direct and Vertex Google image metadata replaces the retired
  `imagen-4.0-generate-001` rows with `gemini-3.1-flash-image`. The Vertex image
  adapter routes Gemini image models through `generateContent` and retains the
  existing Imagen `predict` behavior for caller-registered legacy model IDs;
  Sigma does not silently alias retired IDs. The catalog snapshot and generated
  artifacts now reflect the supported model set.
- Responses assistant phases remain opaque provider metadata rather than new
  provider-neutral content or end-turn controls. Unknown phases are retained
  for diagnostics but omitted from replay, as are recognized phases from a
  different provider, API, or model; partial and terminal stop-reason behavior
  is unchanged.
- OpenAI, Azure, and Codex Responses requests omit assistant turns whose
  persisted stop reason is `error` or `aborted`, together with tool results for
  calls from those turns. Successful, max-token, content-filter, and other
  non-failed history remains replayable, caller-owned messages and partial
  finals are unchanged, and Sigma does not automatically retry or replay the
  failed request.
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
  Converse has no disabled tool-choice variant. OpenAI-compatible Chat
  Completions preserve explicitly supplied provider-neutral and typed
  provider-specific choices even when no tool definitions are emitted,
  including fully deferred tool sets. Provider-specific typed choices override
  provider-neutral choices, and low-level payload overrides retain final
  precedence. Callers targeting endpoints that reject a choice without tools
  must omit the explicit option; Sigma no longer silently discards it.
- Direct xAI Grok 4.6 uses `/responses`, disables provider-side response
  storage, and requests encrypted reasoning whenever a supported effort is
  selected. Explicit `xhigh` maps directly to the provider effort; off and
  minimal remain unsupported. Long cache retention is omitted while cache keys
  and session affinity remain available. Grok 4.5 and the existing legacy Chat
  Completions routes are unchanged.
- Direct DeepSeek discovery removals leave caller-defined Chat Completions
  metadata supported. No Responses or Anthropic-compatible route, Files API
  image reference, live probe, or automatic price-window selection is added.
- OpenAI-compatible Chat Completions usage now falls back to top-level
  `cached_tokens` when nested cache details and `prompt_cache_hit_tokens` do not
  report a cache read. Cache reads remain included in provider prompt totals,
  so Sigma removes them from ordinary input tokens and prices them separately;
  raw usage, existing field precedence, and zero or omitted values are
  unchanged.
- Indexed OpenAI-compatible Chat Completions function and grammar tool calls
  now treat the first non-empty provider ID and name as authoritative. A
  provider ID arriving after Sigma generated a temporary ID replaces it once;
  later conflicting identity values are ignored while arguments, partial
  events, usage, raw finish reasons, and normalized stops remain unchanged.
  Indexless correlation behavior and request payloads are unchanged.
- OpenAI-compatible Chat Completions replays complete `reasoning_details` only
  when persisted provider, API, and model provenance are nonempty and exactly
  match the target, for both modern and legacy representations.
  Consecutive compatible text and summary fragments are coalesced into complete
  logical entries, with later fragments filling missing identity, format,
  index, and signature metadata without replacing prior values. Encrypted
  entries remain discrete and ordered. Conflicting nonempty IDs, formats, or
  signatures and differing supplied indexes (including zero) start new entries.
  Invalid or unknown entries are omitted
  individually, older tool-call metadata remains a same-provenance replay fallback, and
  requests without persisted details retain their previous payloads and
  defaults.
- Typed max-token options below 16 now serialize as 16 for
  OpenAI Responses-compatible and Azure OpenAI Responses requests. Unset values
  remain omitted, sampling parameters and raw `extra_body` values retain their
  existing precedence, and Codex Responses continues omitting
  `max_output_tokens`.
- Model-scoped arbitrary sampling defaults apply only to OpenAI-compatible Chat
  Completions, Responses, and Azure Responses. Omitted defaults leave existing
  payloads unchanged; Codex Responses and non-OpenAI APIs ignore the metadata,
  and broader provider-neutral sampling semantics remain deferred.
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
  rate-limited catalog request with a bounded provider delay and does not enable
  model policies. Explicit policy enablement retries up to two throttled POSTs
  within a five-second wait budget, honoring `Retry-After-Ms`, `Retry-After`,
  and context cancellation; other failures remain single-attempt. Neither
  helper runs during login or refresh, persists availability, mutates Sigma
  registries, or replaces generated catalog metadata. A valid empty account
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
- Provider-wrapped DNS lookup failures, connection and socket/WebSocket
  closures, reset-before-headers and HTTP/2 no-response failures, explicit
  provider retry guidance, `ResourceExhausted` capacity failures, and known
  premature-stream diagnostics now produce transient classification and
  same-model retry advice when structured status or type evidence is
  unavailable. Auth, billing, quota, rate-limit, context-overflow, and
  cancellation precedence is unchanged; partial finals remain intact and
  post-body request replay remains caller-owned.
- Opt-in pre-body HTTP retries now include `408 Request Timeout` and
  `409 Conflict` responses alongside `429` and `5xx`. These statuses produce
  transient same-request retry advice when retries are disabled or exhausted,
  while structured provider codes and messages retain precedence. The default
  remains zero retries, and post-body replay remains caller-owned.
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
- Google Generative AI and Vertex responses containing function calls now map
  only an explicit `STOP` to `StopReasonToolCalls`. Max-token, provider-error,
  and unknown finish reasons retain their normalized stop reason, while the
  function call, usage, cost, terminal event, and raw `finishReason` remain
  available. Request payloads and defaults are unchanged.
- Amazon Bedrock Converse Stream service exceptions now retain the requested
  model and AWS request ID in typed provider errors and assistant diagnostics
  while preserving existing stop reasons and retry classification.
- Amazon Bedrock Converse Stream removes empty object-member names recursively
  only from outbound replayed tool inputs. Provider-emitted tool arguments,
  caller-owned messages, arrays, scalar values, `null`, and non-empty keys
  remain unchanged.
- Amazon Bedrock Converse Stream now decodes scalar base64 `redactedContent`
  deltas, joins their underlying bytes into one opaque redacted thinking block,
  and replays that blob before associated text or tool use. Invalid persisted
  blobs are omitted with their valid sibling content preserved, while malformed
  provider deltas remain typed provider failures with partial output intact.
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
- Direct DeepSeek V4 Flash plus its OpenCode Zen and Go routes now
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

- Codex WebSocket `NO_PROXY` suffix/IPv6 hardening, broader catalog additions and
  retirements, and additional routed Claude effort support remain deferred.
  Mistral lifecycle operations, broader cloud credential loading, and agent
  orchestration retain their existing boundaries. vLLM priority already fits
  the existing sampling-parameter surface.
- OpenCode Go may require workspace-level China-hosting opt-in for individual
  models. A later evidence-backed pass should identify the affected set, add
  explicit availability metadata and actionable diagnostics, and introduce a
  separate regional provider only if the endpoint or credential boundary
  actually differs.
- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

The GPT-6 Astra catalog addition passed catalog-backed local fixtures for both
Responses routes, including reasoning rejection before dispatch, image/function
tools, deferred-tool replay, cache controls, and cost boundaries with service
tiers. The catalog diff adds exactly two text rows without changing existing
rows. `mise run go:generate` refreshed the model outputs and snapshot headers;
`mise run go:fmt`, `mise run go:build`, `mise run ci` (including race tests),
and `git diff --check` passed. Validation made no live provider calls.

The Responses cache and output-token compatibility changes passed deterministic
payload and local-server coverage for direct, Azure, Codex, and background
requests, plus metadata persistence, registry copying, and generator tests.
`mise run go:generate` left the catalog and generated model outputs unchanged;
`mise run go:fmt`, `mise run ci` (including race tests), and `git diff --check`
passed. No live provider calls were made for this validation.

Validate this release with the process in [RELEASING.md](../RELEASING.md),
including the local CI-equivalent `mise run ci` gate before tagging.
