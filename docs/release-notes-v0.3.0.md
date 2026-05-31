# Release notes: sigma v0.3.0

This is the maintainer-facing release note for the next `sigma` tag. It records
the v0.3.0 summary and scope as changes land. For the itemized change list see
[CHANGELOG.md](../CHANGELOG.md); for the validation commands and pre-tag
checklist see [RELEASING.md](../RELEASING.md).

## Release summary

v0.3.0 extends Sigma's generated image metadata with an OpenRouter-routed Grok
Imagine image model while keeping direct xAI/Grok support focused on the
preview Chat Completions adapter.

## Added

- Generated image model metadata for `x-ai/grok-imagine-image-quality` through
  the existing OpenRouter image-generation adapter, including OpenRouter
  credential discovery and xAI routed-provider metadata.

## Compatibility

- No direct xAI image provider is added in this release. Grok image generation
  is represented as OpenRouter image metadata and uses the existing
  `openrouter-images` provider path.
- The direct xAI/Grok text provider remains a preview OpenAI-compatible Chat
  Completions adapter.

## Deferred work

- Direct xAI/Grok image-provider semantics remain deferred until the request
  and response shape is covered by deterministic fixtures.
- Deferred work continues to be tracked in [TODO.md](../TODO.md).

## Validation status

This release should use the validation process in [RELEASING.md](../RELEASING.md).
No live xAI or OpenRouter provider calls are required for release validation.
