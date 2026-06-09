# Release notes: sigma v0.5.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.5.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.5.0 is open for development with focused provider hardening for
Bedrock application inference profile routing plus advanced Anthropic and
Bedrock request-shape controls.

## Added

- Bedrock Converse Stream now derives the runtime region from application
  inference profile ARNs supplied as the model ID or `inference_profile_arn`
  provider option before AWS region environment fallbacks.
- Anthropic Messages now accepts typed `sigma.AnthropicOptions.OutputFormat`
  values and sends them as native `output_format` payloads.
- Anthropic Messages can disable parallel tool use with
  `sigma.AnthropicOptions.DisableParallelToolUse`, adding the provider field to
  typed or map-shaped tool choices and synthesizing an `auto` choice when tools
  are present.
- Bedrock Converse Stream now accepts `sigma.BedrockOptions.ResponseFormat`,
  injects a synthetic schema tool, and surfaces the generated JSON arguments as
  assistant text while preserving any real tool calls emitted by the model.

## Compatibility

- Explicit Bedrock region configuration continues to win over ARN-derived
  regions. Existing AWS environment fallback, EU regional inference-profile
  endpoint fallback, and caller-supplied endpoint behavior are unchanged.
- Anthropic `OutputFormat` is explicit caller-owned behavior; Sigma does not
  infer native structured-output support from model names in this release.
- Anthropic parallel-tool suppression fails locally when combined with a raw
  non-map `tool_choice`, because the provider field must be merged into a
  map-shaped tool-choice payload.
- Bedrock structured-output mode requires tool-capable models and reserves the
  `__sigma_json_response` synthetic tool name for the generated schema tool.

## Deferred work

- Provider-neutral document/PDF content blocks, normalized source/citation
  result APIs, broad provider-neutral sampling controls, and model-inferred
  Anthropic output-format routing remain deferred and are tracked in
  [TODO.md](../TODO.md).

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md),
including the local CI-equivalent `mise run ci` gate before tagging.
