# Release notes: sigma v0.5.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.5.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

`sigma` v0.5.0 is open for development with focused provider hardening for
Bedrock application inference profile routing.

## Added

- Bedrock Converse Stream now derives the runtime region from application
  inference profile ARNs supplied as the model ID or `inference_profile_arn`
  provider option before AWS region environment fallbacks.

## Compatibility

- Explicit Bedrock region configuration continues to win over ARN-derived
  regions. Existing AWS environment fallback, EU regional inference-profile
  endpoint fallback, and caller-supplied endpoint behavior are unchanged.

## Deferred work

- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
