# Release notes: sigma v0.5.0

This is the maintainer-facing release note for `sigma` v0.5.0. For the
itemized change list see [CHANGELOG.md](../CHANGELOG.md); for the validation
commands and pre-tag checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.5.0 focuses on provider hardening for Bedrock application inference
profile routing, request-scoped Bedrock bearer tokens, typed Mistral tool
selection, and advanced Anthropic and Bedrock request-shape controls. It also
tightens the generated model-metadata workflow with a deterministic local
catalog summary while keeping broad catalog refresh automation outside the
release scope.

This release additionally adds first-class Anthropic (Claude Pro/Max) OAuth
login with Claude Code identity support, fixes provider thinking payload
shapes for Claude Fable 5, Z.ai, and Moonshot routes, hardens Bedrock replay
against blank text blocks, and corrects Azure GPT-5.4/5.5, GPT-5 Pro,
Moonshot, and OpenCode request metadata. It also preserves long prompt-cache
write usage for accurate cost accounting, broadens provider context-overflow
detection, adds Fireworks Kimi K2.7 Code coverage on both
Fireworks text surfaces plus an Anthropic-compatible Kimi K2.6 route, and adds
a final-message helper for callers that need to distinguish oversized-context
failures from ordinary provider errors.
GitHub Copilot and Cloudflare AI Gateway also move from metadata-only routes to
first-class compatible provider wrappers for the text API families Sigma can
exercise deterministically.

## Added

- Bedrock Converse Stream now derives the runtime region from application
  inference profile ARNs supplied as the model ID or `inference_profile_arn`
  provider option before AWS region environment fallbacks.
- Bedrock Converse Stream now accepts request-scoped bearer-token auth through
  `sigma.BedrockOptions.BearerToken`, before auth resolver and environment
  credential fallback.
- Mistral Conversations now accepts typed `sigma.MistralOptions.ToolChoice`
  values for automatic, required, disabled, any-tool, and named-tool selection.
- The model metadata generator can emit a deterministic catalog summary covering
  source count, text/image/embedding totals, text tool/reasoning counts, and
  provider/API buckets. Generator tests now also cover deterministic embedding
  model rendering.
- Generated OpenRouter image metadata now includes routed MAI Image 2.5 and
  Riverflow 2.5 rows through the existing OpenRouter Images adapter.
- Generated Anthropic metadata now includes Claude Fable 5 with adaptive
  thinking metadata, xhigh thinking-level mapping, image input support, current
  limits, and pricing.
- Fireworks now has a first-class Anthropic-compatible provider registration
  path, generated Kimi K2.7 Code metadata for both the OpenAI-compatible
  `ProviderFireworks` route and the Anthropic-compatible
  `ProviderFireworksAnthropic` route, plus Kimi K2.6 metadata on
  `ProviderFireworksAnthropic`.
- Anthropic Messages now accepts typed `sigma.AnthropicOptions.OutputFormat`
  values and sends them as native `output_format` payloads.
- Anthropic Messages can disable parallel tool use with
  `sigma.AnthropicOptions.DisableParallelToolUse`, adding the provider field to
  typed or map-shaped tool choices and synthesizing an `auto` choice when tools
  are present.
- Anthropic Messages usage now populates
  `sigma.Usage.LongCacheWriteInputTokens` from long prompt-cache write usage
  and `sigma.CostForUsage` prices those writes at the long-cache input
  multiplier while preserving total cache-write token accounting.
- Bedrock Converse Stream now accepts `sigma.BedrockOptions.ResponseFormat`,
  injects a synthetic schema tool, and surfaces the generated JSON arguments as
  assistant text while preserving any real tool calls emitted by the model.
- Anthropic Messages now omits the disabled-thinking payload when
  `sigma.AnthropicMessagesCompat.SupportsDisabledThinking` is unsupported, and
  generated Claude Fable 5 metadata sets that flag because the model rejects
  explicit `thinking: disabled` requests.
- OpenAI-compatible Z.ai reasoning requests now send `thinking` objects with
  enabled or disabled types instead of the legacy `enable_thinking` toggle, and
  generated Moonshot AI metadata now uses the DeepSeek-style thinking format so
  thinking-off requests explicitly disable reasoning. Moonshot AI and
  Moonshot AI CN metadata now also marks streaming usage as supported.
- OpenAI-compatible Moonshot routes are now detected from provider IDs or
  `api.moonshot.*` hosts, applying the Moonshot `max_tokens`,
  developer-role, store, strict-tool, and DeepSeek-style thinking request
  shape for caller-registered models.
- Generated Moonshot AI metadata now includes Kimi K2.7 Code with text/image
  input, reasoning, tool support, current limits, pricing, and
  `MOONSHOT_API_KEY` discovery.
- MiniMax and MiniMax CN now have first-class Anthropic-compatible provider
  registration helpers, and generated direct MiniMax metadata now uses the
  `/anthropic/v1` base URL expected by Sigma's Messages adapter.
- GitHub Copilot now has first-class compatible provider registration helpers
  for Chat Completions, Responses, and Anthropic Messages routes. The wrappers
  apply Copilot defaults, dynamic request headers, bearer auth, and
  `COPILOT_GITHUB_TOKEN` environment credential discovery.
- Cloudflare AI Gateway now has first-class compatible provider registration
  helpers for OpenAI-compatible and Anthropic-compatible text routes. The
  wrappers resolve account/gateway base URL placeholders and send
  `CLOUDFLARE_API_KEY` credentials through `cf-aig-authorization`.
- OpenCode Zen and OpenCode Go Chat Completions now send explicit `max_tokens`
  instead of `max_completion_tokens`.
- Generated OpenCode Go metadata now uses `reasoning_effort` requests for Kimi
  K2.6 and Kimi K2.7 Code, avoiding rejected disabled `thinking` objects for
  thinking-off/default requests.
- Generated Azure GPT-5.4 and GPT-5.5 context windows now match the
  1,050,000-token Azure Foundry deployments, and OpenAI/Azure GPT-5 Pro max
  output tokens are corrected to 128,000.
- Bedrock Converse Stream now replaces blank required user and tool-result
  text with an `<empty>` placeholder and drops blank replayed assistant text
  blocks before dispatch.
- Bedrock provider errors now append a link to the AWS data-retention
  documentation when a model rejects the configured data retention mode.
- Provider error classification now recognizes additional context-overflow
  wording from OpenAI-compatible routes, OpenRouter, Together, Copilot, Kimi,
  MiniMax, and local OpenAI-compatible endpoints. `sigma.IsContextOverflow`
  can inspect final assistant messages for overflow diagnostics and
  caller-supplied context-window usage signals.
- Anthropic Messages now has stdlib-only Claude Pro/Max OAuth support:
  `anthropic.LoginAnthropicBrowser` browser callback login with a manual
  code-paste fallback, `anthropic.RefreshAnthropicToken`, and
  `anthropic.NewAnthropicOAuthTokenProvider` with expiry-aware refresh and an
  `OnRefresh` callback for caller-owned persistence.
- Anthropic Messages now sends the Claude Code identity required by Anthropic
  OAuth tokens: `claude-code-20250219`/`oauth-2025-04-20` beta headers, the
  Claude Code user agent, a leading Claude Code identity system block, and
  canonical Claude Code tool-name casing with streamed tool names restored to
  the caller's original casing.

## Compatibility

- Explicit Bedrock region configuration continues to win over ARN-derived
  regions. Existing AWS environment fallback, EU regional inference-profile
  endpoint fallback, and caller-supplied endpoint behavior are unchanged.
- Request-scoped Bedrock bearer tokens are explicit caller-owned credentials and
  do not add AWS profile, SSO, web identity, IMDS, or shared-config loading.
- Typed Mistral tool choice takes precedence over raw `tool_choice` provider
  options. Raw provider options remain available when typed Mistral options are
  unset.
- The model-generation summary is reporting-only. It does not fetch provider
  catalogs, change catalog source precedence, or add/remove built-in model rows.
- The OpenRouter image catalog refresh is limited to image-generation rows that
  use Sigma's existing OpenRouter Images adapter. It does not promote broad
  OpenRouter text routing or new provider families.
- The Anthropic catalog refresh is limited to the direct Anthropic Messages row.
  Anthropic-routed aliases on other provider families remain separate catalog
  decisions because their route shape, auth, compatibility metadata, pricing,
  and regional availability can differ.
- Anthropic `OutputFormat` is explicit caller-owned behavior; Sigma does not
  infer native structured-output support from model names in this release.
- Anthropic parallel-tool suppression fails locally when combined with a raw
  non-map `tool_choice`, because the provider field must be merged into a
  map-shaped tool-choice payload.
- `sigma.Usage.LongCacheWriteInputTokens` is additive metadata for cost
  accounting. Existing `CacheWriteInputTokens` values remain the total cache
  write count, so callers that ignore the long-cache split keep the same token
  totals.
- Bedrock structured-output mode requires tool-capable models and reserves the
  `__sigma_json_response` synthetic tool name for the generated schema tool.
- Disabled-thinking omission applies only to models whose compatibility
  metadata marks disabled thinking as unsupported; other reasoning-capable
  models keep sending the explicit disabled payload when thinking is off.
- The Z.ai, Moonshot, and OpenCode payload changes follow the providers'
  current request shapes. Raw provider options and other reasoning formats are
  unchanged. The Moonshot detection also applies to caller-registered
  Moonshot-compatible models.
- The Azure GPT-5.4/5.5, GPT-5 Pro, and Moonshot catalog changes are metadata
  corrections driven by generated fields. Moonshot AI CN keeps the existing
  curated model set; only the direct Moonshot AI catalog gains the Kimi K2.7
  Code row.
- The Bedrock blank-text placeholder applies only where Converse requires
  non-empty content; non-blank text, images, tool calls, and redacted thinking
  replay are unchanged. The data-retention hint appends documentation to the
  provider message without changing error classification.
- Context-overflow helper usage-based detection is caller-owned: Sigma does
  not estimate tokens or call provider tokenizers, and `sigma.IsContextOverflow`
  only checks silent or length-stop overflow signals when the caller supplies a
  positive context window.
- Claude Code identity mode activates only when the resolved credential is
  OAuth-typed or carries an Anthropic OAuth access token; API-key requests are
  byte-for-byte unchanged. Anthropic browser login binds the
  provider-registered `http://localhost:53692/callback` redirect, and OAuth
  token persistence remains caller-owned.

## Deferred work

- Provider-neutral document/PDF content blocks, normalized source/citation
  result APIs, broad provider-neutral sampling controls, and model-inferred
  Anthropic output-format routing remain deferred and are tracked in
  [TODO.md](../TODO.md).
- AWS SDK credential loading, live provider probes, Mistral connectors,
  candidate catalog ingestion, source-precedence automation, refresh diff
  reports, broad OpenRouter/Vercel text expansion, Cloudflare Workers AI
  promotion, broader Anthropic-routed alias expansion, Bedrock regional-profile
  catalog expansion, and new provider family catalog promotion remain deferred.

## Validation status

Validated on 2026-06-15 with:

- `mise run go:test`.
- `mise run go:fmt:check`.
- `mise run go:vet`.
- `mise run ci`.
- `git diff --check`.
