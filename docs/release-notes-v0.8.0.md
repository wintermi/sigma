# Release notes: sigma v0.8.0

This is the maintainer-facing development note for the next `sigma` tag. Add
the v0.8.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.8.0 begins by tightening native Gemini 3 replay compatibility across
Google Generative AI and Vertex AI so function calls and matching tool results
retain stable normalized IDs. Amazon Bedrock Converse Stream service exceptions
also retain their requested model and AWS request ID for diagnostic correlation.

## Added

- Nothing yet.

## Compatibility

- Google Generative AI and native Vertex Gemini 3 requests now preserve
  normalized tool-call IDs on replayed function calls and matching tool
  results. Older Vertex Gemini requests continue omitting unsupported IDs.
- Amazon Bedrock Converse Stream service exceptions now retain the requested
  model and AWS request ID in typed provider errors and assistant diagnostics
  while preserving existing stop reasons and retry classification.

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

Validate this release with the process in [RELEASING.md](../RELEASING.md),
including the local CI-equivalent `mise run ci` gate before tagging.
