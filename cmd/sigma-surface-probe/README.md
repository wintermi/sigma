# Sigma Surface Probe

`sigma-surface-probe` is an opt-in live diagnostic command for checking provider
request surfaces against real upstream APIs. It emits JSONL as each case
finishes, then writes one final summary object.

Live probes require provider credentials and are intentionally outside
deterministic CI.

## Run It

Run from the repository root:

```bash
mise run go:run -- ./cmd/sigma-surface-probe [flags]
```

Common flags:

```text
-routes                 comma-separated routes to probe
-models                 comma-separated model IDs; omitting this uses route defaults
-repair                 try targeted repair variants after a failing case
-include-unavailable    probe known unavailable advertised models instead of skipping
-codex-oauth            run OpenAI Codex device-code OAuth for openai-codex
-handoff                run cross-provider replay handoff diagnostics
-structured-output      run focused OpenAI-compatible structured-output probes
-images                 run focused image-generation probes
-timeout                overall probe timeout, default 10m
-case-timeout           maximum duration per case or repair attempt, default 1m
```

Each primary case and repair attempt receives its own `-case-timeout`, bounded
by the overall `-timeout`. Set `-case-timeout=0` to use only the overall
deadline. Retryable HTTP 5xx and connection-reset failures are retried twice
with the identical request before they are reported as upstream availability.

Default routes are `zen,go`. Image mode defaults to the `openai` image route.
All other routes must be requested explicitly.

## Routes

| Route | API shape | Credential | Default model behavior |
| --- | --- | --- | --- |
| `openai` | OpenAI Responses | `OPENAI_API_KEY` | Discovers OpenAI models |
| `openai` with `-images` | OpenAI Images and OpenAI Responses image-generation tool | `OPENAI_API_KEY` | Uses `gpt-image-1`, `dall-e-2` for variations, and `gpt-5.5` for the Responses tool case |
| `google` with `-images` | Gemini `generateContent` | `GOOGLE_API_KEY`, then `GOOGLE_CLOUD_API_KEY` | Uses the generated `gemini-2.5-flash-image` and `gemini-3.1-flash-image` rows |
| `google-vertex` with `-images` | Vertex AI Gemini `generateContent` | `GOOGLE_CLOUD_ACCESS_TOKEN`, `GOOGLE_CLOUD_API_KEY`, or `GOOGLE_API_KEY`; also requires project and location | Uses the generated `gemini-3.1-flash-image` row |
| `openai-codex` | OpenAI Codex Responses | `OPENAI_CODEX_ACCESS_TOKEN`, `OPENAI_CODEX_REFRESH_TOKEN`, or `-codex-oauth` | Uses `gpt-5.5` unless `-models` is set |
| `zen` | OpenCode routed surfaces | `OPENCODE_API_KEY` | Discovers Zen models |
| `go` | OpenCode Go routed surfaces | `OPENCODE_API_KEY` | Discovers Go models |
| `google-vertex` | Native Vertex Gemini `streamGenerateContent` | `GOOGLE_CLOUD_ACCESS_TOKEN`, `GOOGLE_CLOUD_API_KEY`, or `GOOGLE_API_KEY`; also requires project and location | Uses every built-in `google-vertex` Gemini text model in sorted order unless `-models` is set |
| `google-vertex-anthropic` | Vertex Anthropic Claude `streamRawPredict` | `GOOGLE_CLOUD_ACCESS_TOKEN`, `GOOGLE_CLOUD_API_KEY`, or `GOOGLE_API_KEY`; also requires project and location | Uses `claude-sonnet-4-6` unless `-models` selects other built-in Vertex Claude models |
| `fireworks-openai` | Fireworks OpenAI-compatible Chat Completions | `FIREWORKS_API_KEY` | Discovers Fireworks models |
| `fireworks-anthropic` | Fireworks Anthropic-compatible Messages | `FIREWORKS_API_KEY` | Discovers Fireworks models |
| `moonshot` | Moonshot AI OpenAI-compatible Chat Completions | `MOONSHOT_API_KEY` | Discovers Moonshot AI models |
| `moonshot-cn` | Moonshot AI CN OpenAI-compatible Chat Completions | `MOONSHOT_API_KEY` | Discovers Moonshot AI CN models |
| `nvidia` | NVIDIA NIM OpenAI-compatible Chat Completions | `NVIDIA_API_KEY` | Uses `nvidia/nemotron-3-super-120b-a12b` unless `-models` is set |
| `xai` | xAI/Grok OpenAI-compatible Chat Completions | `XAI_API_KEY` | Discovers xAI models |

## Examples

Probe the default OpenCode routes:

```bash
OPENCODE_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe
```

Probe only OpenCode Zen with a known model:

```bash
OPENCODE_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -routes zen \
  -models kimi-k3 \
  -repair
```

Probe only OpenCode Go with a known model:

```bash
OPENCODE_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -routes go \
  -models kimi-k3 \
  -repair
```

The `go` and `zen` routes create a fresh conversation ID for each probe case.
The same ID is reused for that case's turns, transport retries, and repair
variants and sent as `x-opencode-session` across the routed APIs. OpenCode Go
requires this header for routing. A provider `MissingSessionID` error is
classified as `sigma_request_shape`.

Probe the Fireworks OpenAI-compatible route:

```bash
FIREWORKS_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -routes fireworks-openai \
  -models accounts/fireworks/routers/kimi-k2p6-turbo \
  -repair
```

Probe the Fireworks Anthropic-compatible route:

```bash
FIREWORKS_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -routes fireworks-anthropic \
  -models accounts/fireworks/models/kimi-k2p6 \
  -repair
```

Use `accounts/fireworks/models/...` IDs with `fireworks-anthropic`; model IDs
that Fireworks also serves through Chat Completions can be probed with
`fireworks-openai`. `accounts/fireworks/routers/...` IDs are for
`fireworks-openai`.

For `accounts/.../models/...` IDs, the Fireworks OpenAI route reads the model's
current state, `supportsImageInput`, `supportsTools`, and context-length
metadata before choosing optional cases. Non-ready models are skipped. If that
lookup fails and no generated registry entry is available, the probe uses
text-only conservative defaults and emits an `optional_capabilities` skip
instead of assuming support.

Probe direct Moonshot K2.7 routes:

```bash
MOONSHOT_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -routes moonshot,moonshot-cn \
  -models kimi-k2.7-code,kimi-k2.7-code-highspeed \
  -repair
```

Probe xAI/Grok with a known model:

```bash
XAI_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -routes xai \
  -models grok-4.5 \
  -repair
```

Probe NVIDIA NIM with the default model:

```bash
NVIDIA_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -routes nvidia \
  -repair
```

Probe one native Vertex Gemini model with an externally supplied OAuth access
token:

```bash
GOOGLE_CLOUD_ACCESS_TOKEN="$(gcloud auth application-default print-access-token)" \
GOOGLE_CLOUD_PROJECT=my-project \
GOOGLE_CLOUD_LOCATION=global \
mise run go:run -- ./cmd/sigma-surface-probe \
  -routes google-vertex \
  -models gemini-3.6-flash \
  -repair
```

The project resolves from `GOOGLE_CLOUD_PROJECT`, then `GCLOUD_PROJECT`. The
location resolves from `GOOGLE_CLOUD_LOCATION`, then `GOOGLE_CLOUD_REGION`.
Authentication resolves `GOOGLE_CLOUD_ACCESS_TOKEN` first, followed by
`GOOGLE_CLOUD_API_KEY` and `GOOGLE_API_KEY`. The probe passes an access token as
a typed OAuth bearer credential and does not print, persist, inspect, or refresh
it.

Omit `-models` to probe every built-in native Vertex Gemini text model
sequentially. Explicit IDs must be built-in `google-vertex` text models; model
selection is catalog-backed and never calls a Vertex model-discovery endpoint.
Vertex-hosted partner MaaS models, images, and embeddings are not included in
the native Gemini text route; use `-images -routes google-vertex` for the
focused Vertex Gemini image probe.
Use the `global` location when probing the complete catalog because some current
models are global-only. A regional or multi-region location remains valid when
every explicitly selected model is available there.

Probe the default Vertex Anthropic Claude model with the same explicit routing
and credential contract:

```bash
GOOGLE_CLOUD_ACCESS_TOKEN="$(gcloud auth application-default print-access-token)" \
GOOGLE_CLOUD_PROJECT=my-project \
GOOGLE_CLOUD_LOCATION=global \
mise run go:run -- ./cmd/sigma-surface-probe \
  -routes google-vertex-anthropic \
  -repair
```

The route defaults to `claude-sonnet-4-6`. Use `-models` to select one or more
other built-in `google-vertex-anthropic` Claude IDs. Model selection remains
catalog-backed and does not call a Vertex model-discovery endpoint. The probe
does not load ambient credentials or persist access tokens.

Probe OpenAI Responses with a known model:

```bash
OPENAI_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -routes openai \
  -models gpt-5.5 \
  -repair
```

Probe OpenAI image generation surfaces:

```bash
OPENAI_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -images
```

Probe current Gemini image models through the direct Google image route:

```bash
GOOGLE_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -images \
  -routes google
```

The direct route uses `GOOGLE_API_KEY` first and falls back to
`GOOGLE_CLOUD_API_KEY`. It sends `gemini-2.5-flash-image` and
`gemini-3.1-flash-image` through `generateContent`. Use `-models` to select one
of those generated model rows; unrelated IDs are reported as skipped before
network access.

Probe Gemini image generation through Vertex AI with an externally supplied
OAuth access token:

```bash
GOOGLE_CLOUD_ACCESS_TOKEN="$(gcloud auth application-default print-access-token)" \
GOOGLE_CLOUD_PROJECT=my-project \
GOOGLE_CLOUD_LOCATION=us-central1 \
mise run go:run -- ./cmd/sigma-surface-probe \
  -images \
  -routes google-vertex
```

The Vertex image route reuses the text route's explicit project, location, and
credential precedence: OAuth access token, Cloud API key, then Google API key.
It does not load ambient credentials or persist tokens. Both Google image
routes remain opt-in live diagnostics outside deterministic CI.

Probe OpenAI Codex Responses with device-code OAuth:

```bash
mise run go:run -- ./cmd/sigma-surface-probe \
  -routes openai-codex \
  -models gpt-5.5 \
  -codex-oauth \
  -repair
```

For non-interactive Codex runs, set `OPENAI_CODEX_ACCESS_TOKEN`, or set
`OPENAI_CODEX_REFRESH_TOKEN` and let the probe refresh it in memory before the
run. The probe does not persist refreshed Codex credentials.

Probe pairwise cross-provider replay handoff between selected routes:

```bash
OPENAI_API_KEY=... XAI_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -handoff \
  -routes openai,xai
```

Handoff mode asks each selected route/model to produce a small tool-call
context, appends a deterministic tool result, then replays each source context
into every other selected target route/model. Missing credentials and source
models that do not emit a tool call are reported as skipped diagnostics.

Discover and probe every model returned by one provider:

```bash
XAI_API_KEY=... mise run go:run -- ./cmd/sigma-surface-probe \
  -routes xai \
  -repair
```

When combining `-routes` with `-models`, each listed model is considered for
each listed route. Prefer separate commands when the route model IDs differ.

## Probe Cases

OpenAI-compatible routes currently run:

```text
basic_text
developer_instruction
json_object
json_schema
logprobs
cache_ephemeral
image_input
thinking_string_none
thinking_object_disabled
thinking_bool_false
enable_thinking_false
reasoning_effort_low
reasoning_effort_medium
reasoning_effort_high
tool_auto_file_read
tool_required_file_read
strict_tool_required_write
three_turn_file_update
```

OpenAI Responses and OpenAI Codex Responses routes run a Responses-shaped subset
covering text, developer instructions, structured output, cache keys, image
input, typed reasoning controls, and tool calls. Codex also checks text
verbosity. The Codex image case uses an HTTPS image URL because the ChatGPT
Codex backend rejects base64 image payloads. Fireworks OpenAI-compatible probes
omit unsupported raw thinking-disable variants and keep the object-disabled case
because Fireworks expects `thinking` to be an object.

OpenAI image mode currently runs:

```text
generate
edit_multipart
edit_reference_json
variation
stream_partial
responses_image_tool
```

The `variation` case uses `dall-e-2`. The other image API cases use
`gpt-image-1`, and `responses_image_tool` uses OpenAI Responses with `gpt-5.5`
and the image-generation tool. These probes require `OPENAI_API_KEY` and stay
outside deterministic CI.

The direct Google image route runs `generate_gemini` and
`generate_gemini_3_1`; the Vertex image route runs `generate_gemini_3_1`. A
successful image case requires a non-empty base64 or URL image, so a text-only
Gemini response is not treated as success. Every image case receives an
independent `-case-timeout`, bounded by the overall `-timeout`, so one stalled
request does not prevent later cases.

Anthropic-compatible routes run the cases supported by the selected model's
generated or probe metadata:

```text
basic_text
developer_instruction
cache_ephemeral
image_input
reasoning_level_low
reasoning_level_medium
reasoning_level_high
tool_auto_file_read
tool_required_file_read
```

The `google-vertex` route always checks basic text and system instructions. It
adds image input, automatic and forced-any function tools, typed disabled
thinking, and low, medium, or high reasoning cases only when the selected
built-in model metadata advertises the corresponding capability or thinking
level. Gemini 2.5 reasoning levels use the generated token-budget mappings;
Gemini 3 models use named thinking levels. Disabled-thinking cases are omitted
when the model metadata marks `off` as unsupported. This keeps restricted
Gemini models from receiving unsupported reasoning configurations.

## Output

Each completed case is written immediately:

```json
{"route":"xai","model":"grok-4.3","case":"basic_text","attempt":"basic_text","outcome":"ok"}
```

When `-repair` is enabled, a failed original case may be followed by a working
repair variant. A variant counts as a repair only when it preserves the
capability under test and the original failure is not a recognized safety
rejection:

```json
{"route":"fireworks-openai","model":"accounts/fireworks/routers/kimi-k2p6-turbo","case":"image_input","attempt":"image_url_fallback","outcome":"fixed_by_repair_variant","originalError":"provider rejected base64 image input","failedAttempts":[{"attempt":"image_input","error":"provider rejected base64 image input"}],"hint":"base64_image_failed_url_image_ok"}
```

Handoff replay results include the target route/model in `route` and `model`,
and the source context in `sourceRoute` and `sourceModel`:

```json
{"route":"xai","model":"grok-4.3","case":"handoff_replay","attempt":"target_replay","sourceRoute":"openai","sourceModel":"gpt-5.5","outcome":"ok"}
```

The final line is a summary report:

```json
{"summary":{"total":18,"ok":17,"skipped":0,"sigmaRequestShape":0,"providerCapabilityLimit":0,"upstreamAvailability":0,"inconclusive":0,"noWorkingAttempt":0,"fixedByRepairVariant":1,"availabilityOKAfterFailure":0},"recommendations":[{"route":"fireworks-openai","model":"accounts/fireworks/routers/kimi-k2p6-turbo","case":"image_input","hint":"base64_image_failed_url_image_ok","evidence":"image_input repaired by image_url_fallback"}]}
```

Outcome meanings:

| Outcome | Meaning |
| --- | --- |
| `ok` | The original probe case worked. |
| `skipped` | The route/model is known unavailable and was skipped. |
| `sigma_request_shape` | The provider rejected the request shape. |
| `provider_capability_limit` | The provider does not appear to support the tested capability. |
| `upstream_availability` | The upstream route or model is currently unavailable. |
| `inconclusive` | The failure does not contain enough evidence for a request-shape, capability, or availability conclusion. |
| `fixed_by_repair_variant` | The original case failed, but a targeted variant worked. |
| `no_working_attempt` | The original case and repair variants did not produce a working request. |

When a failed case still passes the minimal-text availability check, the
result keeps its original outcome and includes
`"availabilityOKAfterFailure":true`. The summary counts that evidence
separately from the failure classification. Successful diagnostic controls
that remove the tested capability are listed in `successfulControls`; they do
not change the original outcome or produce a repair recommendation.

Recognized safety rejections (`Content violates usage guidelines` or
`SAFETY_CHECK_TYPE_*`) remain `inconclusive`, with the original error retained.
A later successful variant is recorded in `successfulControls`, and a successful
minimal-text check still sets `availabilityOKAfterFailure`. Neither produces a
recommendation for that case: success with a larger output cap does not establish
that the cap caused the safety rejection. A generic HTTP 403 alone is not treated
as a safety rejection.

### xAI JSON-object safety rejections

The `json_object` case sends one user message, `Return JSON exactly {"ok":true}.`,
with `response_format: {"type":"json_object"}` and no tools or previous history.
A provider HTTP 403 containing `SAFETY_CHECK_TYPE_BIO` is a safety rejection;
it does not establish that JSON-object mode is unsupported or that the output
token budget is too small. xAI documents both JSON-object and JSON-schema modes
in its [structured-output documentation](https://docs.x.ai/developers/model-capabilities/text/structured-outputs).

If this case is `inconclusive` while `json_schema` is `ok` and
`availabilityOKAfterFailure` is true, the model remains reachable and the
separate schema case succeeded. The JSON-object case is still unresolved.
The separate schema case uses a different prompt, so its success does not prove
that switching the rejected prompt to JSON-schema mode will work. If JSON-object
mode is required, retain the provider request ID, request timestamp, and redacted
request/response payloads for an xAI support investigation. A false positive on
the benign probe is possible, but the response alone does not establish the cause.
`-repair` cannot override provider moderation, and a successful availability
control must not be reported as a fix for that rejection.

For persistent xAI failures, `-repair` (or `-structured-output`) adds six
diagnostic requests after the availability check. All retain the original
output-token budget; the first three also retain the original user prompt:

| Attempt | Difference from the original request | What success establishes |
| --- | --- | --- |
| `json_object_explicit_instruction` | Adds a system instruction to return only a JSON object, without Markdown | JSON-object mode accepted this instruction variant. |
| `json_object_schema_control` | Uses a strict schema with the requested boolean `ok` field | The same user prompt was accepted with schema-constrained output. |
| `json_object_text_control` | Uses plain-text output mode | The same user prompt was accepted without JSON-object mode. |
| `json_object_string_value` | Requests `{"ok":"ready"}` in JSON-object mode | This string-value prompt was accepted. |
| `json_object_number_value` | Requests `{"ok":1}` in JSON-object mode | This number-value prompt was accepted. |
| `json_object_false_value` | Requests `{"ok":false}` in JSON-object mode | This alternate boolean-value prompt was accepted. |

The value comparisons keep the `ok` key, prompt wording, JSON-object mode, and
request options unchanged. The three values are fixed so runs can be compared.
Acceptance can vary between runs, so a successful
variant does not establish the cause of the original rejection.

Run these focused checks from a terminal with `XAI_API_KEY` configured:

```bash
mise run go:run -- ./cmd/sigma-surface-probe -routes xai -models grok-4.6 -structured-output -repair
```

Successful comparisons appear in `successfulControls`; failures retain their
individual errors and request IDs in `failedAttempts`. These are comparisons,
not automatic repair claims, and the original safety rejection remains
`inconclusive`. If the instruction variant succeeds, it provides a concrete
request shape to verify in the application. If only the schema/text comparisons
succeed, the evidence points toward an interaction with JSON-object mode, but
does not identify the provider's internal cause. These additional requests run
only after a failed xAI JSON-object case; a passing primary case is unchanged.

### Locally validated JSON text fallback

When the same prompt succeeds in plain-text mode, run the explicit fallback:

```bash
mise run go:run -- ./cmd/sigma-surface-probe -routes xai -models grok-4.6 -json-text
```

`-json-text` runs only the `json_text` case on Chat Completions routes. It sends
the original `Return JSON exactly {"ok":true}.` prompt and 256-token budget with
`response_format: {"type":"text"}`. Sigma validates the completed response
locally against the exact requested object, allowing whitespace. Prose,
Markdown fences, duplicate keys, extra fields, wrong values, truncated output,
and trailing JSON fail validation. Provider errors are returned unchanged.

This is an opt-in workaround for obtaining JSON text, not a fix for an upstream
safety rejection or evidence of provider-enforced JSON support. No provider
adapter silently downgrades requests. Without the flag, the original capability
checks and their error reporting remain unchanged. The flag takes precedence
over `-structured-output`; `-repair` does not add variants to this fallback.

Applications can use the same existing provider option,
`sigma.WithProviderOption(sigma.ProviderXAI, "extra_body", map[string]any{"response_format": map[string]any{"type": "text"}})`,
and validate returned JSON against their own expected structure before using it.
Plain-text mode does not guarantee valid JSON. A successful diagnostic text
control previously established only request acceptance; `json_text` also checks
the returned value. Live success must still be verified in the calling environment.
