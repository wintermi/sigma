# Release notes: sigma v0.7.0

This is the maintainer-facing development note for the next `sigma` tag. Add
the v0.7.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.7.0 hardens existing provider protocol compatibility and
caller-directed stream recovery, refreshes the Kimi Coding, Fireworks, and
selected OpenCode Go catalogs, and adds focused xAI OpenAI Responses
registration and caller-configured device-code OAuth surfaces. It also adds a
dynamic API-key Radius gateway text provider.

## Changed

- Codex request-affinity headers now limit session IDs to 64 characters while
  preserving local session resource management. OpenRouter uses its native
  cache-affinity header, and Bedrock terminal responses with unrecognised stop
  reasons now surface typed provider errors.
- Grok 4.5 now uses the xAI OpenAI Responses route with low, medium, and high
  reasoning levels. Long-lived prompt-cache retention is omitted for that route
  while cache keys and session affinity remain available.
- xAI now supports caller-configured device-code OAuth login, token refresh,
  and opt-in provider-auth registration for its existing text routes. Token
  persistence remains owned by the application.
- Kimi Coding now includes K3 and Kimi For Coding HighSpeed with current
  context, output, image-input, tool, reasoning, and estimated cost metadata.
  K3 exposes its supported `max` reasoning level, while K3 and Kimi For Coding
  preserve empty thinking signatures during replay.
- OpenCode Go now includes Grok 4.5 and Kimi K3 on Chat Completions with
  text/image, tool, reasoning, context, output, pricing, and `max_tokens`
  metadata.
- Curated Fireworks Chat Completions and Messages models now include verified
  standard-serverless input, cached-input, and output pricing. Deterministic
  Messages coverage also protects cache-affinity headers and omitted unsupported
  tool fields.
- Premature OpenAI Responses and Anthropic Messages terminal-event gaps now
  surface as transient, retryable failures while preserving partial finals.
  Sigma does not re-dispatch a stream after its body begins; applications own
  retry and fallback decisions.
- Radius gateway models now refresh explicitly from the gateway at runtime and
  use its native text streaming protocol with image, thinking, function-tool,
  usage, and response-ID handling. There is no static Radius catalog.

## Compatibility

- `provider/xai` adds Responses registration helpers. Built-in `xai/grok-4.5`
  now dispatches through OpenAI Responses rather than Chat Completions; no
  provider ID or serialized-message shape changes.
- xAI OAuth requires an application-supplied approved client ID and scopes. It
  does not change API-key authentication, provider IDs, request routes, or
  serialized-message shapes.
- `ProviderKimiCoding` retains its existing registration API while its built-in
  model catalog expands; no serialized-message shape changes.
- `ProviderOpenCodeGo` retains its existing registration API and request route;
  the built-in catalog adds two Chat Completions models without changing
  serialized-message shapes.
- `ProviderRadius` is a new opt-in registration. Its models are empty until an
  explicit refresh succeeds; requests use standard API-key resolver precedence
  with `RADIUS_API_KEY` as the environment fallback. OAuth and persisted
  gateway catalogs remain deferred.

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

Validate this release with the process in [RELEASING.md](../RELEASING.md),
including the local CI-equivalent `mise run ci` gate before tagging.
