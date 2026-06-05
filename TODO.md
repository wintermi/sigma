# TODO

Deferred work that is outside the current release scope. None of these are
current release gates. Each item should land with the same evidence bar as MVP
features — deterministic fixtures, golden payloads, or fake clients, plus
cancellation/error coverage (see the coverage standards in
[RELEASING.md](RELEASING.md)) — before it can be promoted out of "future" status.

## Image generation

- [ ] Decide whether image generation graduates from preview into the MVP
      boundary and update release docs accordingly.
- [ ] Add live OpenAI image validation probes for generation, edits,
      reference-only JSON edits, variations, streaming partial images, and
      Responses image-generation tool output without making live provider calls
      part of `mise run ci`.

## Embeddings

Sigma now has a first-class provider-neutral embeddings surface with OpenAI
`/v1/embeddings` support and generated metadata for the current OpenAI text
embedding models. Resilient batch hardening is available through
`Client.EmbedBatch`; small in-memory retrieval primitives are available for
local document/chunk search, while external stores and tokenizer-aware workflows
remain future work.

- [x] Add resilient embedding batch helpers for duplicate input reuse,
      retry-aware batch splitting, optional oversized-input splitting, progress
      callbacks, and aggregate usage/cost summaries.
- [x] Add typed embedding attempt telemetry with request IDs, status codes,
      provider/API/model identity, retry attempts, and per-attempt latency.
- [x] Add generic batch telemetry for total request attempts, status buckets,
      request IDs, attempts, usage, and cost.
- [x] Add model/request-level embedding batch limits for input counts and UTF-8
      byte budgets while keeping token-budget estimation caller-owned.
- [x] Add cross-call embedding cache hooks with SHA-256 input keys and
      deterministic fake-cache coverage.
- [x] Add split-recoverable classification for embedding request-too-large and
      local tokenizer EOF failures.
- [x] Add safer oversized embedding split boundaries and structured batch trace
      events for caller aggregation.
- [x] Add OpenAI-compatible embedding model construction for caller-registered
      local or private embedding endpoints.
- [x] Add an explicit local OpenAI-compatible embedding registration helper
      with Ollama-friendly defaults and normalized `/v1` base URLs.
- [x] Add explicit embedding dimension-range metadata for discovery and routing.
- [x] Add retrieval document/chunk types, deterministic character-based
      splitting, and in-memory cosine search routed through `Client.EmbedBatch`.
- [x] Add a compact query/document embedder helper around the provider-neutral
      embedding API without adding another provider adapter.
- [ ] Add external vector-store integration only as an explicit new surface,
      not as part of provider dispatch.
- [ ] Add tokenizer-aware or format-specific text chunking without making
      provider tokenizers a hidden runtime dependency.
- [x] Add similarity/ranking helpers with deterministic numeric fixtures before
      documenting them as reusable retrieval primitives.
- [ ] Add tokenizer-based embedding input estimates without making provider
      tokenizers a hidden runtime dependency.
- [ ] Add provider-selection fallback only after settling provider/model
      precedence semantics for embeddings as a separate public API decision.
- [ ] Add non-OpenAI embedding adapters only after each provider has request,
      response, usage, error, and cancellation fixtures.
- [ ] Add opt-in live embedding probes without making live provider calls part
      of `mise run ci`.

## OpenAI-compatible parity

OpenAI Chat Completions, Responses, and Codex Responses now cover the focused
v0.3 prompt-cache, replay, stream-shape, cached-token accounting, and Codex
WebSocket gaps with deterministic fixtures. Broader provider-specific
integrations remain future work until they have the same local evidence bar.

- [x] Add cache-affinity headers for OpenAI-compatible Chat Completions and
      direct OpenAI Responses when prompt caching and `sigma.WithSessionID` are
      enabled.
- [x] Add OpenAI Responses/Codex service-tier cost accounting for `flex` and
      `priority` request tiers.
- [x] Harden OpenAI Responses replay when same-provider history crosses model
      IDs with prior function-call item IDs.
- [x] Add Codex WebSocket transport, session caching, cleanup helpers, and SSE
      fallback while keeping the implementation stdlib-only.
- [ ] Keep proxy-aware Codex WebSocket dialing deferred; proxy-constrained
      environments should use SSE fallback.
- [x] Add OpenAI Codex device-code OAuth login and refresh helpers while keeping
      token persistence caller-owned.
- [x] Add OpenAI Codex browser callback OAuth login while keeping token
      persistence caller-owned.
- [ ] Keep OpenAI Codex token persistence deferred.
- [x] Evaluate GitHub Copilot dynamic headers before making Copilot a
      first-class OpenAI-compatible row.
- [x] Evaluate Cloudflare AI Gateway auth header rewriting before adding a
      first-class Cloudflare OpenAI-compatible row.
- [ ] Add opt-in structured-output capability probes for OpenAI-compatible
      routes so JSON object and strict JSON Schema support can be refreshed
      from live evidence without making provider calls part of `mise run ci`.

## First-class provider rows

Provider IDs and compatibility routing may already exist, but each needs
independent provider-quality claims backed by fixtures before becoming a
first-class row in [provider parity](docs/provider-parity.md). Representative
generated metadata can exist before first-class promotion when it is
metadata-only and backed by compatibility checks.

- [ ] DeepSeek — promote to a first-class provider row with fixtures.
- [ ] Groq — promote to a first-class provider row with fixtures.
- [ ] Cerebras — promote to a first-class provider row with fixtures.
- [ ] Together — promote to a first-class provider row with fixtures.
- [ ] GitHub Copilot — promote to a first-class provider row with fixtures.
- [ ] Cloudflare AI Gateway and Cloudflare Workers AI — promote to first-class
      provider rows with fixtures.
- [ ] NVIDIA NIM — promote to a first-class provider row with fixtures.
- [ ] Z.ai and Z.ai Coding CN — promote to first-class provider rows with
      fixtures.
- [ ] Ant Ling — promote to a first-class provider row with fixtures.
- [ ] Moonshot AI and Moonshot AI CN — promote to first-class provider rows
      with fixtures.
- [ ] MiniMax and MiniMax CN — promote to first-class provider rows with
      fixtures.
- [ ] Vercel AI Gateway — promote to a first-class provider row with fixtures.
- [ ] Kimi — promote to a first-class provider row with fixtures.
- [ ] Xiaomi — promote to a first-class provider row with fixtures.
- [ ] For each promoted provider, add streaming, tools, usage, typed error
      classification, redaction, and cancellation coverage.

## xAI/Grok parity

Sigma now has a first-class preview xAI/Grok provider over the shared
OpenAI-compatible Chat Completions adapter and curated generated metadata for
the direct xAI Grok Chat Completions routes. Future additions should still be
promoted only with deterministic request-shape evidence.

- [ ] Keep future xAI/Grok generated metadata refreshes tied to deterministic
      modeldata, payload, error, and compatibility coverage.
- [x] Add OpenRouter-routed Grok Imagine image metadata without introducing a
      direct xAI image provider.
- [x] Add opt-in live xAI/Grok surface probes only as diagnostics, keeping live
      provider calls out of `mise run ci`.
- [ ] Evaluate direct xAI/Grok image-provider semantics only after the API
      shape is backed by deterministic request and response fixtures.

## Model registry generation

Sigma's built-in model registry is generated from a curated checked-in catalog.
That keeps release output reviewable, but refreshes are manual and the default
catalog is intentionally smaller than the provider/source metadata available
upstream.

- [x] Refresh the curated v0.3 generated catalog with current rows for
      supported provider IDs while preserving Sigma runtime contracts and
      metadata-only default registration.
- [x] Add focused metadata-only provider-family rows for existing adapter
      paths, including Azure OpenAI Responses, OpenAI Codex Responses,
      Cloudflare AI Gateway, Cloudflare Workers AI, NVIDIA NIM, Z.ai, Ant Ling,
      Moonshot AI, MiniMax, Vercel AI Gateway, and expanded GitHub Copilot
      routes.
- [x] Add focused Chat Completions reasoning-format support for generated
      Together, Qwen, Z.ai, and Ant Ling metadata, including Z.ai `tool_stream`
      payloads.
- [ ] Add an offline-friendly refresh command that can ingest `models.dev` and
      provider catalog APIs into a candidate catalog file without replacing the
      checked-in review step.
- [ ] Keep explicit source precedence and override rules near the generator:
      `models.dev` for broad text model discovery, provider APIs for surfaces
      not covered there, and hand-curated overrides for known endpoint behavior
      mismatches.
- [ ] Emit a refresh report that lists added, removed, changed, skipped, and
      overridden models by provider so catalog review is tractable.
- [ ] Expand broad OpenRouter text metadata only through the catalog refresh
      workflow, with deterministic diffs and reviewable routing/cost changes
      instead of ad hoc catalog imports.
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
- [x] Promote strict OpenCode Zen/Go metadata for the covered DeepSeek V4,
      MiniMax M3, Grok Build, Kimi K2.6, and Claude adaptive-thinking rows,
      including unsupported thinking-level and temperature compatibility
      metadata.
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
- [ ] Add Fireworks Anthropic-compatible routing before promoting regular
      Fireworks model rows that do not use the OpenAI-compatible Fire Pass
      route.
- [ ] Promote additional Fireworks metadata only after matching deterministic
      modeldata, payload, error, and compatibility coverage exists.

## Google parity

The Google adapters already cover the v0.2 request and stream slice with
deterministic fixtures. The next useful hardening pass around Vertex
credentials, model-scoped routing metadata, and tool-call replay for
Google-hosted non-Gemini routes is complete. Broader model coverage should
still come through the catalog refresh workflow.

- [x] Treat Vertex API-key placeholder values such as angle-bracket markers and
      local credential sentinels as unavailable in auto credential mode, so a
      configured OAuth/ADC token provider can be used instead of sending the
      placeholder as `X-Goog-Api-Key`.
- [x] Let Google Generative AI and Vertex adapters consume concrete
      model-scoped `baseURL` and `headers` metadata where present, while keeping
      request/provider options higher precedence and ignoring generated Vertex
      base URL templates that still contain `{location}` placeholders.
- [x] Normalize Google replayed tool-call IDs for model families that require
      explicit function-call IDs, and omit empty function-response IDs for
      native Gemini requests.
- [ ] Keep live Google Gemini API and Vertex AI validation out of `mise run ci`;
      use credential-gated probes only for manual compatibility investigation.
- [ ] Add broader Vertex-specific fixtures only when Vertex behavior diverges
      from the shared Google payload or stream parser.
- [ ] Expand Google and Vertex catalog coverage only through the catalog refresh
      workflow, with deterministic modeldata, payload, error, and compatibility
      coverage for promoted rows.

## Mistral parity

- [ ] Add Mistral Conversations image input only after the Conversations request
      shape is verified and covered by deterministic payload fixtures, or add a
      separate Mistral Chat adapter if image support belongs on that API surface.
- [ ] Add Mistral built-in connector tools such as web search, code interpreter,
      image generation, and document libraries after deciding how they map to
      Sigma provider-defined tools.
- [ ] Add Mistral Conversations append and restart support only if Sigma exposes
      provider conversation lifecycle operations beyond single-turn streaming.
- [ ] Expand broad Mistral generated metadata only through the catalog refresh
      workflow, with deterministic modeldata, payload, error, and compatibility
      coverage for promoted rows.

## Bedrock parity

- [ ] Keep live Bedrock validation out of `mise run ci`; use credential-gated
      checks only for manual compatibility investigation.
- [ ] Expand broad Bedrock generated metadata only through the catalog refresh
      workflow, with deterministic modeldata, payload, error, and compatibility
      coverage for promoted rows.
- [ ] Keep AWS profile, SSO, web identity, IMDS, and shared-config loading
      outside the built-in stdlib Bedrock adapter unless Sigma adopts an AWS SDK
      credential integration.

## Authentication and credentials

- [x] Implement OpenAI Codex device-code OAuth login with caller-owned
      credential persistence.
- [x] Implement OpenAI Codex browser callback OAuth login with caller-owned
      credential persistence.
- [ ] Implement token persistence for OAuth-based providers.
- [ ] Add Anthropic Claude Code OAuth identity headers and Claude Code
      tool-name canonicalization if Sigma adopts a first-class Anthropic OAuth
      login path.
- [x] Wire OpenAI Codex browser/device-code login and refresh into Codex
      Responses through `openai.NewCodexOAuthTokenProvider`.
- [ ] Add deterministic coverage for login/refresh/persistence flows without
      live network calls.

## Anthropic-compatible routing

- [ ] Evaluate GitHub Copilot and Cloudflare AI Gateway Anthropic Messages
      routing before making either a first-class Anthropic-compatible row.
- [ ] Add opt-in live Anthropic-compatible provider probes for compatibility
      metadata, separate from the deterministic release gate.

## Transports

- [x] Add Codex Responses WebSocket transport support.
- [x] Add Codex WebSocket session caching, cleanup, and SSE fallback behavior.
- [ ] Add WebSocket transport support for other provider routes only when their
      route-specific wire protocols are covered by deterministic fixtures.
- [ ] Ensure unsupported transport choices continue to fail locally before any
      network call, with tests asserting the early failure.

## Usage and cost reporting

- [x] Separate OpenAI Responses cached input tokens from ordinary input tokens
      when provider usage includes cache-read details.
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
