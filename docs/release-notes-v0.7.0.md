# Release notes: sigma v0.7.0

This is the maintainer-facing development note for the next `sigma` tag. Add
the v0.7.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.7.0 hardens existing provider protocol compatibility, refreshes the
Kimi Coding catalog, and adds a focused xAI OpenAI Responses registration
surface.

## Changed

- Codex request-affinity headers now limit session IDs to 64 characters while
  preserving local session resource management. OpenRouter uses its native
  cache-affinity header, and Bedrock terminal responses with unrecognised stop
  reasons now surface typed provider errors.
- Grok 4.5 now uses the xAI OpenAI Responses route with low, medium, and high
  reasoning levels. Long-lived prompt-cache retention is omitted for that route
  while cache keys and session affinity remain available.
- Kimi Coding now includes K3 and Kimi For Coding HighSpeed with current
  context, output, image-input, tool, reasoning, and estimated cost metadata.
  K3 exposes its supported `max` reasoning level, while K3 and Kimi For Coding
  preserve empty thinking signatures during replay.

## Compatibility

- `provider/xai` adds Responses registration helpers. Built-in `xai/grok-4.5`
  now dispatches through OpenAI Responses rather than Chat Completions; no
  provider ID or serialized-message shape changes.
- `ProviderKimiCoding` retains its existing registration API while its built-in
  model catalog expands; no serialized-message shape changes.

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

Validate this release with the process in [RELEASING.md](../RELEASING.md),
including the local CI-equivalent `mise run ci` gate before tagging.
