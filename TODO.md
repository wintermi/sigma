# TODO

Deferred work that is outside the current release scope. None of these are
current release gates. Each item should land with the same evidence bar as MVP
features — deterministic fixtures, golden payloads, or fake clients, plus
cancellation/error coverage (see the coverage standards in
[RELEASING.md](RELEASING.md)) — before it can be promoted out of "future" status.

## Image generation

- [ ] Implement OpenAI Images reference-image editing through
      `ImageRequest.Inputs` and the Images edits endpoint.
- [ ] Implement OpenAI image variations, or document why variations remain
      outside the provider-neutral image surface.
- [ ] Add streaming partial image support if Sigma exposes a streaming image
      generation contract.
- [ ] Add Responses API image-tool generation if it becomes part of the
      provider-neutral image workflow.
- [ ] Decide whether image generation graduates from preview into the MVP
      boundary and update release docs accordingly.

## First-class provider rows

Provider IDs and compatibility routing may already exist, but each needs
independent provider-quality claims backed by fixtures before becoming a
first-class row in [provider parity](docs/provider-parity.md). Representative
generated metadata can exist before first-class promotion when it is
metadata-only and backed by compatibility checks.

- [ ] DeepSeek — promote to a first-class provider row with fixtures.
- [ ] Groq — promote to a first-class provider row with fixtures.
- [ ] Cerebras — promote to a first-class provider row with fixtures.
- [ ] xAI — promote to a first-class provider row with fixtures.
- [ ] Together — promote to a first-class provider row with fixtures.
- [ ] GitHub Copilot — promote to a first-class provider row with fixtures.
- [ ] Kimi — promote to a first-class provider row with fixtures.
- [ ] Xiaomi — promote to a first-class provider row with fixtures.
- [ ] Evaluate GitHub Copilot dynamic headers before making Copilot a
      first-class OpenAI-compatible row.
- [ ] Evaluate Cloudflare AI Gateway auth header rewriting before adding a
      first-class Cloudflare OpenAI-compatible row.
- [ ] For each promoted provider, add streaming, tools, usage, error, redaction,
      and cancellation coverage.

## Model registry generation

Sigma's built-in model registry is generated from a curated checked-in catalog.
That keeps release output reviewable, but refreshes are manual and the default
catalog is intentionally smaller than the provider/source metadata available
upstream.

- [ ] Add an offline-friendly refresh command that can ingest `models.dev` and
      provider catalog APIs into a candidate catalog file without replacing the
      checked-in review step.
- [ ] Keep explicit source precedence and override rules near the generator:
      `models.dev` for broad text model discovery, provider APIs for surfaces
      not covered there, and hand-curated overrides for known endpoint behavior
      mismatches.
- [ ] Emit a refresh report that lists added, removed, changed, skipped, and
      overridden models by provider so catalog review is tractable.
- [ ] Preserve Sigma's current generated-output contract: deterministic order,
      validated `internal/modeldata/catalog.json`, generated Go code, checksum
      tests, and metadata-only default registry entries.
- [ ] Add source-aware tests for provider/API mapping, tool-capable filtering,
      reasoning metadata, thinking-level maps, provider headers, auth env vars,
      and compatibility metadata.
- [ ] Expand generated metadata only after matching provider adapters or
      compatibility routes have deterministic payload/error coverage.

## OpenCode parity

Sigma now has a routed OpenCode Zen/Go preview provider for selected model
families that need Google Generative AI, Anthropic Messages, OpenAI Responses,
or OpenAI-compatible Chat Completions routes. Broader catalog coverage should
still be promoted only with deterministic evidence.

- [ ] Continue refreshing curated OpenCode Zen and OpenCode Go metadata after
      checking current provider catalogs, prioritizing remaining high-value
      routed families and avoiding advertised-but-unavailable models.
- [x] Evaluate and fixture selected OpenCode-routed OpenAI Responses,
      Anthropic Messages, and Google API models before promoting them to
      built-in metadata.
- [ ] Cover each promoted OpenCode addition with deterministic modeldata and
      golden payload tests; no live OpenCode calls should be required.
- [ ] Keep `cmd/sigma-surface-probe` as an opt-in live diagnostic tool for
      route-shape regressions, capability limits, and upstream availability
      changes; do not add live OpenCode calls to `mise run ci`.

## Fireworks parity

Sigma now has opt-in live Fireworks surface probes for the current
OpenAI-compatible Fire Pass route and an Anthropic-compatible Messages route.
These probes are diagnostics only; deterministic fixtures remain the release
evidence bar.

- [x] Add credential-gated Fireworks surface probes to
      `cmd/sigma-surface-probe`.
- [ ] Keep live Fireworks probing out of `mise run ci`; use it only for manual
      compatibility investigation with `FIREWORKS_API_KEY`.
- [ ] Promote additional Fireworks metadata only after matching deterministic
      modeldata, payload, error, and compatibility coverage exists.

## Authentication and credentials

- [ ] Implement interactive OAuth login (currently MVP uses caller-supplied
      credentials or injected OAuth token providers only).
- [ ] Implement token persistence for OAuth-based providers.
- [ ] Add Anthropic Claude Code OAuth identity headers and Claude Code
      tool-name canonicalization if Sigma adopts a first-class Anthropic OAuth
      login path.
- [ ] Wire interactive login and token persistence into OpenAI Codex Responses,
      replacing the injected-token-only path.
- [ ] Add deterministic coverage for login/refresh/persistence flows without
      live network calls.

## Anthropic-compatible routing

- [ ] Evaluate GitHub Copilot and Cloudflare AI Gateway Anthropic Messages
      routing before making either a first-class Anthropic-compatible row.
- [ ] Add opt-in live Anthropic-compatible provider probes for compatibility
      metadata, separate from the deterministic release gate.

## Transports

- [ ] Add WebSocket transport support.
- [ ] Add Codex WebSocket session caching, cleanup, and SSE fallback behavior if
      Sigma adopts Codex WebSocket transport.
- [ ] Ensure unsupported transport choices fail locally before any network call,
      with tests asserting the early failure.

## Usage and cost reporting

- [ ] Add tokenizer-based token estimates as an alternative to provider-reported
      usage.
- [ ] Reconcile tokenizer estimates against provider usage data and model
      metadata so reported cost stays consistent.

## Request controls

- [ ] Design provider-neutral structured-output and logprob semantics before
      mapping those controls beyond OpenAI-compatible API families.

## Platform reach

- [ ] Add browser-specific behavior support (the MVP package stays server/CLI
      friendly).
- [ ] Confirm build constraints / packaging keep server/CLI builds unaffected.

## Agent runtime and orchestration

- [ ] Add agent runtime integration on top of the provider-neutral primitives
      `sigma` exposes (orchestration is deferred to later integration cards).
- [ ] Implement cross-provider context handoff.
- [ ] Implement capability-loss reporting so unsupported handoff behavior remains
      explicit rather than silently degrading.
