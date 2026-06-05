# Release notes: sigma v0.4.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.4.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.4.0 is open for development. The first compatibility fix tightens
structured-output request shaping for an OpenCode Go DeepSeek route that does
not currently accept strict JSON Schema response formats.

## Added

- OpenCode Go DeepSeek V4 Flash Chat Completions requests now preserve JSON
  generation by downgrading unsupported strict JSON Schema response formats to
  JSON object mode.

## Compatibility

- `opencode-go/deepseek-v4-flash` no longer sends
  `response_format.type = "json_schema"` through Chat Completions. Sigma
  downgrades that request shape to `response_format.type = "json_object"` for
  this model because the route rejects strict JSON Schema response formats
  before generation.

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
