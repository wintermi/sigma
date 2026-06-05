# Release notes: sigma v0.4.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.4.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.4.0 is open for development. The first compatibility fixes tighten
structured-output request shaping, OpenAI Responses reasoning replay defaults,
and OpenAI-compatible Chat Completions history replay for stricter routes.

## Added

- OpenCode Go DeepSeek V4 Flash Chat Completions requests now preserve JSON
  generation by downgrading unsupported strict JSON Schema response formats to
  JSON object mode.
- OpenAI Responses requests now default to disabled provider-side storage,
  include encrypted reasoning replay metadata when reasoning is enabled, and
  default reasoning summaries to `auto` while preserving explicit caller
  overrides.
- OpenAI-compatible Chat Completions replay now skips empty assistant history
  turns and supports an opt-in compatibility flag for routes that require an
  empty tools array when replaying prior tool-call history.

## Compatibility

- `opencode-go/deepseek-v4-flash` no longer sends
  `response_format.type = "json_schema"` through Chat Completions. Sigma
  downgrades that request shape to `response_format.type = "json_object"` for
  this model because the route rejects strict JSON Schema response formats
  before generation.
- OpenAI Responses keeps explicit `include` and `store` provider options at
  caller precedence while adding safer defaults for ordinary reasoning replay.
- OpenAI-compatible Chat Completions keeps empty `tools` arrays out of default
  payloads and enables them only for compatibility routes that opt in.

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
