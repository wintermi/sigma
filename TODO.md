# TODO

Deferred work that is outside the current release scope. None of these are
`v0.1.0` release gates. Each item should land with the same evidence bar as MVP
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
first-class row in [provider parity](docs/provider-parity.md).

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
- [ ] Add malformed Anthropic SSE JSON repair if compatible endpoints keep
      producing repairable stream chunks in practice.

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
