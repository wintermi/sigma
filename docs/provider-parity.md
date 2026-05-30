# Provider parity matrix

This matrix tracks source-package feature parity against the Go implementation.
It is a release-scope artifact, not a marketing claim. Later provider cards must
update this file when a feature is added, deferred, or intentionally omitted.

Status values:

- `fixture-tested`: implemented and covered by deterministic tests or local fixtures.
- `implemented`: implemented, with the supporting behavior covered by shared tests.
- `live-tested`: covered by an opt-in live provider test that has been run.
- `partial`: some behavior exists, but coverage or provider breadth is incomplete.
- `not supported by provider`: the upstream API family does not expose this feature.
- `not yet implemented`: source capability or model metadata exists, but no Go adapter support exists yet.
- `intentionally omitted`: excluded from the current Go scope.

Release scope values:

- `MVP`: part of the first releasable scope. See the [release notes](release-notes-v0.1.0.md) for the v0.1.0 scope and [RELEASING.md](../RELEASING.md) for the coverage bar.
- `preview`: implemented enough for early adopters, but not part of the MVP release gate.
- `future`: deferred work.
- `intentionally omitted`: deliberately excluded from the Go release scope.

## Matrix

| Provider family | Built-in API | Release scope | Text | Image input | Image generation | Streaming | Tool calls | Partial tool JSON | Thinking | Cache retention | Usage | Cost | Custom headers | OAuth | Local endpoints | Live-test coverage |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| OpenAI-compatible Chat Completions, including OpenRouter text and custom endpoints | `openai-completions` | `MVP` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| Fireworks OpenAI-compatible Chat Completions | `openai-completions` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| OpenCode Zen and OpenCode Go OpenAI-compatible Chat Completions | `openai-completions` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `intentionally omitted` | `partial` | `intentionally omitted` |
| OpenAI Responses | `openai-responses` | `preview` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `intentionally omitted` |
| Azure OpenAI Responses | `azure-openai-responses` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `partial` | `partial` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` |
| OpenAI Codex Responses | `openai-codex-responses` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `partial` | `fixture-tested` | `implemented` | `partial` | `fixture-tested` | `fixture-tested` | `intentionally omitted` |
| Anthropic Messages and Anthropic-compatible Kimi/Fireworks/Xiaomi routing | `anthropic-messages` | `MVP` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `implemented` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `intentionally omitted` |
| Google Generative AI | `google-generative-ai` | `preview` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `partial` | `fixture-tested` | `intentionally omitted` |
| Google Vertex AI | `google-vertex` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `not supported by provider` | `partial` | `partial` | `fixture-tested` | `implemented` | `implemented` | `fixture-tested` | `fixture-tested` | `intentionally omitted` |
| Mistral Conversations | `mistral-conversations` | `preview` | `fixture-tested` | `not supported by provider` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `not yet implemented` | `not supported by provider` | `fixture-tested` | `implemented` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `intentionally omitted` |
| Amazon Bedrock Converse Stream | `bedrock-converse-stream` | `preview` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `implemented` | `intentionally omitted` | `not supported by provider` | `fixture-tested` | `partial` |
| OpenAI Images generation | `openai-images` | `preview` | `not supported by provider` | `not yet implemented` | `fixture-tested` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `fixture-tested` | `implemented` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `intentionally omitted` |
| OpenRouter image generation | `openrouter-images` | `preview` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `intentionally omitted` |
| Other source OpenAI-compatible provider IDs: DeepSeek, Groq, Cerebras, xAI, Together, GitHub Copilot | `openai-completions` | `future` | `partial` | `partial` | `not supported by provider` | `partial` | `partial` | `partial` | `partial` | `partial` | `partial` | `implemented` | `partial` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |

## Evidence references

- `openai-completions`: [provider/openai/completions_test.go](../provider/openai/completions_test.go), [provider/openai/compat_test.go](../provider/openai/compat_test.go), [internal/sse/testdata/openai/text_usage.sse](../internal/sse/testdata/openai/text_usage.sse), [internal/sse/testdata/openai/tool_call.sse](../internal/sse/testdata/openai/tool_call.sse).
- Fireworks `openai-completions`: [provider/fireworks/fireworks_test.go](../provider/fireworks/fireworks_test.go), [provider/openai/compat_test.go](../provider/openai/compat_test.go).
- OpenCode `openai-completions`: [provider/openai/compat_test.go](../provider/openai/compat_test.go), [internal/modeldata/modeldata_test.go](../internal/modeldata/modeldata_test.go).
- `openai-responses`: [provider/openai/responses_test.go](../provider/openai/responses_test.go).
- `azure-openai-responses`: [provider/openai/azure_responses_test.go](../provider/openai/azure_responses_test.go).
- `openai-codex-responses`: [provider/openai/codex_responses_test.go](../provider/openai/codex_responses_test.go).
- `anthropic-messages`: [provider/anthropic/anthropic_test.go](../provider/anthropic/anthropic_test.go).
- `google-generative-ai`: [provider/google/google_test.go](../provider/google/google_test.go).
- `google-vertex`: [provider/google/vertex_test.go](../provider/google/vertex_test.go).
- `mistral-conversations`: [provider/mistral/mistral_test.go](../provider/mistral/mistral_test.go).
- `bedrock-converse-stream`: [provider/bedrock/bedrock_test.go](../provider/bedrock/bedrock_test.go).
- `openai-images`: [provider/openai/images_test.go](../provider/openai/images_test.go), [image_models_generated.go](../image_models_generated.go).
- `openrouter-images`: [provider/openrouter/images_test.go](../provider/openrouter/images_test.go).
- Cost calculation: [usage_test.go](../usage_test.go) and provider stream tests that assert usage mapping.
- Built-in model metadata registration: [modeldata_test.go](../modeldata_test.go).

## Known limitations and compatibility risks

- Default registry entries are metadata-only. Importing provider packages and calling their `Register` functions is still required for runtime provider dispatch.
- `openai-images` currently implements generation-only requests. Reference-image editing, variations, streaming partial images, and Responses image-tool generation are deferred.
- Azure and Codex adapters are implemented provider packages, but their APIs are
  not represented by generated default model metadata yet. Vertex now has a
  representative metadata-only route.
- OpenAI-compatible provider IDs beyond OpenAI/OpenRouter/Fireworks/OpenCode rely
  on shared compatibility detection or explicit `OpenAICompletionsCompat`
  metadata. They are future-scope rows and are not independently
  release-complete.
- OpenCode Zen and OpenCode Go coverage is limited to curated
  `openai-completions` models. Source-package OpenCode models that route through
  OpenAI Responses, Anthropic Messages, or Google APIs are not Go parity today.
- Caller-defined custom and local OpenAI-compatible endpoints are covered by the
  MVP `openai-completions` row when they use explicit compatibility metadata.
- OpenAI-compatible Chat Completions supports typed `tool_choice`, opt-in
  Anthropic-style cache markers, and opt-in `tool_stream`; provider-specific
  dynamic headers for GitHub Copilot and Cloudflare AI Gateway remain out of
  scope.
- OpenAI Responses and Codex Responses support typed prompt cache retention,
  parallel tool calls, text verbosity, bounded replay IDs, and image-capable
  tool-result replay through fixture-tested payload coverage.
- Google Vertex reuses the Google payload and stream parser, but only a narrower Vertex-specific fixture set exists today.
- Codex Responses requires a caller-supplied OAuth token provider. Interactive login, token persistence, and WebSocket transport are out of scope.
- Bedrock uses stdlib HTTP, SigV4 signing, and EventStream parsing rather than the AWS SDK. The built-in environment credential path is intentionally limited to `AWS_BEARER_TOKEN_BEDROCK` or static AWS keys; profiles, SSO, web identity, IMDS, and shared-config loading require caller-supplied credentials through Sigma auth resolvers.
- The Anthropic-compatible routing in the Anthropic row title covers Kimi, Fireworks, and Xiaomi compat branches. Each branch has deterministic compatibility coverage in `provider/anthropic`; Fireworks is also exercised through the `openai-completions` path.
- The `not supported by provider` value in the OAuth column for the Anthropic and Bedrock rows describes the provider's native authentication model, not a missing code path. Both adapters still forward an OAuth-typed `Credential` returned by an auth resolver as a bearer token (Bedrock has fixture coverage for this).
- Live tests are skipped by default and are not an MVP readiness signal unless a row explicitly says `live-tested`.
- Cross-provider context handoff is not covered by this matrix; unsupported handoff behavior must remain explicit in later compatibility docs.
- MVP rows are the only provider rows that may be described as release-ready in
  README wording. Preview rows may be documented, but not advertised as part of
  the first release gate.
