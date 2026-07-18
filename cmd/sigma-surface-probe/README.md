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
-models                 comma-separated model IDs; omitting this discovers models
-repair                 try targeted repair variants after a failing case
-include-unavailable    probe known unavailable advertised models instead of skipping
-codex-oauth            run OpenAI Codex device-code OAuth for openai-codex
-handoff                run cross-provider replay handoff diagnostics
-structured-output      run focused OpenAI-compatible structured-output probes
-images                 run focused OpenAI image-generation probes
-timeout                overall probe timeout, default 10m
```

Default routes are `zen,go`. Image mode defaults to the `openai` image route.
All other routes must be requested explicitly.

## Routes

| Route | API shape | Credential | Default model behavior |
| --- | --- | --- | --- |
| `openai` | OpenAI Responses | `OPENAI_API_KEY` | Discovers OpenAI models |
| `openai` with `-images` | OpenAI Images and OpenAI Responses image-generation tool | `OPENAI_API_KEY` | Uses `gpt-image-1`, `dall-e-2` for variations, and `gpt-5.5` for the Responses tool case |
| `openai-codex` | OpenAI Codex Responses | `OPENAI_CODEX_ACCESS_TOKEN`, `OPENAI_CODEX_REFRESH_TOKEN`, or `-codex-oauth` | Uses `gpt-5.5` unless `-models` is set |
| `zen` | OpenCode routed surfaces | `OPENCODE_API_KEY` | Discovers Zen models |
| `go` | OpenCode Go routed surfaces | `OPENCODE_API_KEY` | Discovers Go models |
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

Anthropic-compatible routes currently run:

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

## Output

Each completed case is written immediately:

```json
{"route":"xai","model":"grok-4.3","case":"basic_text","attempt":"basic_text","outcome":"ok"}
```

When `-repair` is enabled, a failed original case may be followed by a working
repair variant:

```json
{"route":"fireworks-openai","model":"accounts/fireworks/routers/kimi-k2p6-turbo","case":"image_input","attempt":"image_url_fallback","outcome":"fixed_by_repair_variant","originalError":"provider rejected base64 image input","failedAttempts":[{"attempt":"image_input","error":"provider rejected base64 image input"}],"hint":"base64_image_rejected_url_image_ok"}
```

Handoff replay results include the target route/model in `route` and `model`,
and the source context in `sourceRoute` and `sourceModel`:

```json
{"route":"xai","model":"grok-4.3","case":"handoff_replay","attempt":"target_replay","sourceRoute":"openai","sourceModel":"gpt-5.5","outcome":"ok"}
```

The final line is a summary report:

```json
{"summary":{"total":18,"ok":17,"skipped":0,"sigmaRequestShape":0,"providerCapabilityLimit":0,"upstreamAvailability":0,"noWorkingAttempt":0,"fixedByRepairVariant":1,"availabilityOKAfterFailure":0},"recommendations":[{"route":"fireworks-openai","model":"accounts/fireworks/routers/kimi-k2p6-turbo","case":"image_input","hint":"base64_image_rejected_url_image_ok","evidence":"image_input repaired by image_url_fallback"}]}
```

Outcome meanings:

| Outcome | Meaning |
| --- | --- |
| `ok` | The original probe case worked. |
| `skipped` | The route/model is known unavailable and was skipped. |
| `sigma_request_shape` | The provider rejected the request shape. |
| `provider_capability_limit` | The provider does not appear to support the tested capability. |
| `upstream_availability` | The upstream route or model is currently unavailable. |
| `fixed_by_repair_variant` | The original case failed, but a targeted variant worked. |
| `availability_ok_after_failure` | The original case failed, but minimal text still worked. |
| `no_working_attempt` | The original case and repair variants did not produce a working request. |
