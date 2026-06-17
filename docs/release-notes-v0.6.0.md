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
cost estimate. It also adds caller-owned GitHub Copilot OAuth helpers for
device-code login, token refresh, request-time credential resolution, and
explicit model-policy enablement. Kimi Coding is promoted as a focused
Anthropic-compatible provider slice with generated metadata, credential
discovery, request headers, adaptive thinking metadata, and session-affinity
support. Xiaomi is promoted as a focused OpenAI-compatible provider slice with
API-billing and regional token-plan registration helpers, generated MiMo
metadata, and regional API-key discovery. The environment credential resolver
also exposes non-secret discovery helpers so applications can inspect candidate
and configured API-key variable names before making a request, and focused
provider helpers now let callers pass Cloudflare AI Gateway placeholder values
and Bedrock region/static credential values without mutating process
environment. The surface probe command adds a credential-gated cross-provider
handoff diagnostic for replaying small tool-call contexts across selected live
routes without moving live provider calls into CI. Assistant results now also
expose provider-neutral source and citation accessors for the source metadata
Sigma already captures from grounded and citation-bearing responses. Local
tool-call validation also now evaluates composed JSON Schema branches so
callers can reject invalid model-emitted arguments before running tools.

## Added

- Anthropic Messages usage now populates
  `sigma.Usage.LongCacheWriteInputTokens` from long prompt-cache write usage
  and `sigma.CostForUsage` prices those writes at the long-cache input
  multiplier while preserving total cache-write token accounting.
- Text-generation usage now populates provider/model identity on
  `sigma.Usage`, preserves a JSON-like `Usage.Raw` copy of provider usage
  payloads when providers report usage, normalizes provider tool/connector
  token counts into `Usage.ToolUseInputTokens`, and exposes provider-reported
  cost separately from Sigma's estimated `Cost.TotalCost`.
- GitHub Copilot now has stdlib-only device-code OAuth login through
  `githubcopilot.LoginGitHubCopilotDeviceCode`, Copilot token refresh through
  `githubcopilot.RefreshGitHubCopilotToken`, and an in-memory token provider
  from `githubcopilot.NewGitHubCopilotOAuthTokenProvider` that can be used as a
  Sigma auth resolver.
- GitHub Copilot model policies can now be enabled explicitly with
  `githubcopilot.EnableGitHubCopilotModel` and
  `githubcopilot.EnableGitHubCopilotModels`, which report per-model success or
  failure without making model enablement an automatic login side effect.
- Kimi Coding can now be registered with `kimi.RegisterCoding` or
  `kimi.RegisterCodingDefault`, using the shared Anthropic-compatible Messages
  adapter with Kimi Coding base URL defaults, Kimi CLI request headers,
  `KIMI_API_KEY` credential discovery, and generated metadata for `k2p7`,
  `kimi-for-coding`, and `kimi-k2-thinking`.
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
- `cloudflare.WithAIGatewayAccountID` and `cloudflare.WithAIGatewayID` now
  provide request-scoped Cloudflare AI Gateway placeholder values before the
  existing `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_GATEWAY_ID` environment
  fallback.
- `bedrock.WithRequestRegion` and `bedrock.WithRequestStaticCredentials` now
  provide request-scoped Bedrock runtime region and static AWS credential values
  before the existing AWS region and static credential environment fallbacks.
- `cmd/sigma-surface-probe -handoff` now builds a small tool-call context for
  each selected live route/model and replays it pairwise into the other selected
  routes, emitting JSONL diagnostics with `sourceRoute` and `sourceModel` so
  replay failures can be attributed without making handoff checks part of CI.
- `sigma.AssistantMessage.Sources`, `sigma.ContentBlock.Citations`, and
  `sigma.AssistantMessage.Citations` now expose normalized source and citation
  entries from provider metadata, including URLs, URIs, titles, offsets, cited
  text, and copied provider metadata for provider-specific details.
- `sigma.ValidateToolCall` now evaluates `anyOf`, `oneOf`, and `allOf` in tool
  input schemas, including nested property, array item, and additional property
  schemas, while preserving decoded-copy results and redacted validation errors.

## Compatibility

- `sigma.Usage.LongCacheWriteInputTokens` is additive metadata for cost
  accounting. Existing `CacheWriteInputTokens` values remain the total cache
  write count, so callers that ignore the long-cache split keep the same token
  totals.
- Usage remains optional: `AssistantMessage.Usage == nil` and terminal
  `Event.Usage == nil` still mean no usage was supplied, while a non-nil
  zero-valued usage means the provider explicitly reported zero values.
- Existing `sigma.Cost` component fields and `TotalCost` remain Sigma's
  estimated cost from model metadata. Provider-reported cost is additive and is
  only populated when an upstream payload contains a clear numeric cost field.
- GitHub Copilot OAuth credentials remain caller-owned. Sigma does not persist
  tokens, does not automatically enable model policies after login, and does
  not change the existing GitHub Copilot request dispatch path.
- Kimi Coding is additive: the existing `kimi` metadata row remains available,
  and broader router or regional endpoint catalog expansion stays deferred.
- Xiaomi token-plan support is additive and uses distinct provider IDs for the
  CN, AMS, and SGP regional OpenAI-compatible routes. The API-billing
  `ProviderXiaomi` rows remain available, and `mimo-v2-flash` remains scoped to
  the API-billing provider rather than the token-plan providers.
- Environment credential discovery is additive and non-secret. `Resolve`
  remains the API that returns credential values, and the new helper methods do
  not probe ambient cloud credentials or OAuth token stores.
- Cloudflare and Bedrock request configuration helpers are provider-specific
  request options. They do not add a root-level environment override map, and
  existing process environment fallback behavior remains available.
- Bedrock credential precedence remains explicit: typed bearer-token options
  and auth resolvers run before request static credentials, and request static
  credentials run before the existing static environment credential path.
- Source and citation accessors are additive views over existing provider
  metadata. They do not change persisted request shape, replay behavior,
  provider dispatch, or the raw `ProviderMetadata` maps.
- Tool schema composition validation is additive and stricter for previously
  unchecked composed branches. It does not add primitive coercion or change the
  `ValidateToolCall` API.

## Deferred work

- OAuth token persistence remains deferred and caller-owned. Deferred work
  continues to be tracked in [TODO.md](../TODO.md).
- Ambient cloud credential probing, OAuth token stores, AWS profiles, SSO, web
  identity, IMDS, and shared-config loading remain deferred. Applications that
  need those flows should continue to resolve credentials before calling Sigma.
- Billing reconciliation, subscription analytics, and UI presentation of usage
  totals remain caller-owned. Sigma normalizes and preserves provider data but
  does not claim invoice-grade billing accuracy.
- Cross-provider handoff remains a diagnostic probe, not a public orchestration
  runtime. Full context handoff APIs and capability-loss reporting remain
  deferred.
- Xiaomi Anthropic-compatible token-plan routes remain deferred until they have
  separate provider IDs, compatibility metadata, and deterministic replay
  fixtures.
- Provider-neutral document/PDF content blocks, source ranking, citation
  rendering, and provider-specific citation UI policy remain deferred and
  caller-owned.
- Full JSON Schema runtime support, including `$ref`, `pattern`, formats,
  `not`, conditionals, and implicit argument coercion, remains deferred.

## Validation status

Current v0.6.0 development state validated on 2026-06-18 with:

- `mise run mise:validate`.
- `mise run clean`.
- `mise run go:generate`.
- `mise run go:fmt`.
- `mise run go:build`.
- `mise run go:test`.
- `mise run go:race`.
- `mise run go:vet`.
- `mise run go:fmt:check`.
- `mise run go:lint`.
- `mise run ci`.
- `git diff --check`.
