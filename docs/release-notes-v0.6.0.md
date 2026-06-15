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
explicit model-policy enablement.

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

## Deferred work

- OAuth token persistence remains deferred and caller-owned. Deferred work
  continues to be tracked in [TODO.md](../TODO.md).
- Billing reconciliation, subscription analytics, and UI presentation of usage
  totals remain caller-owned. Sigma normalizes and preserves provider data but
  does not claim invoice-grade billing accuracy.

## Validation status

Current v0.6.0 development state validated on 2026-06-15 with:

- `mise run mise:validate`.
- `mise run clean`.
- `mise run go:build`.
- `mise run go:test`.
- `mise run go:race`.
- `mise run go:vet`.
- `mise run go:fmt:check`.
- `mise run go:lint`.
- `mise run ci`.
- `git diff --check`.
