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
| OpenAI-compatible Chat Completions and custom endpoints | `openai-completions` | `MVP` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| OpenRouter OpenAI-compatible Chat Completions | `openai-completions` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `partial` | `partial` | `partial` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| Fireworks OpenAI-compatible Chat Completions | `openai-completions` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| xAI/Grok Chat Completions and Grok 4.5 Responses | `openai-completions`, `openai-responses` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `partial` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| NVIDIA NIM OpenAI-compatible Chat Completions and Embeddings | `openai-completions`, `openai-embeddings` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `partial` | `partial` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| Xiaomi MiMo OpenAI-compatible Chat Completions and token-plan routes | `openai-completions` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| OpenCode Zen and OpenCode Go OpenAI-compatible Chat Completions | `openai-completions` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `intentionally omitted` | `partial` | `intentionally omitted` |
| OpenAI Responses | `openai-responses` | `preview` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `intentionally omitted` |
| Azure OpenAI Responses | `azure-openai-responses` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `partial` | `partial` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` |
| OpenAI Codex Responses | `openai-codex-responses` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `partial` | `partial` | `fixture-tested` | `implemented` | `partial` | `fixture-tested` | `fixture-tested` | `intentionally omitted` |
| GitHub Copilot compatible text routes | `openai-completions`, `openai-responses`, `anthropic-messages` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `partial` | `partial` | `partial` | `partial` | `implemented` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| Cloudflare AI Gateway compatible text routes | `openai-completions`, `openai-responses`, `anthropic-messages` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `partial` | `partial` | `partial` | `partial` | `implemented` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| Cloudflare Workers AI direct Chat Completions | `openai-completions` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `partial` | `partial` | `partial` | `partial` | `implemented` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| Vercel AI Gateway Anthropic-compatible Messages | `anthropic-messages` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `partial` | `partial` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| Hugging Face Router OpenAI-compatible Chat Completions | `openai-completions` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `partial` | `partial` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| Anthropic Messages and Anthropic-compatible Kimi/Kimi Coding/Fireworks/Xiaomi routing | `anthropic-messages` | `MVP` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `implemented` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` |
| Google Generative AI | `google-generative-ai` | `preview` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `partial` | `fixture-tested` | `intentionally omitted` |
| Google Vertex AI | `google-vertex` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `not supported by provider` | `partial` | `partial` | `fixture-tested` | `implemented` | `implemented` | `fixture-tested` | `fixture-tested` | `intentionally omitted` |
| Mistral Conversations | `mistral-conversations` | `preview` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `implemented` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `intentionally omitted` |
| MiniMax and MiniMax CN Anthropic-compatible Messages | `anthropic-messages` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `implemented` | `implemented` | `implemented` | `implemented` | `fixture-tested` | `implemented` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |
| Amazon Bedrock Converse Stream | `bedrock-converse-stream` | `preview` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `implemented` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `partial` |
| OpenAI Images generation | `openai-images` | `preview` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `fixture-tested` | `implemented` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `intentionally omitted` |
| OpenRouter image generation | `openrouter-images` | `preview` | `not supported by provider` | `fixture-tested` | `fixture-tested` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `partial` | `fixture-tested` | `implemented` | `fixture-tested` | `not supported by provider` | `fixture-tested` | `intentionally omitted` |
| Google Gemini API image generation | `google-images` | `preview` | `not supported by provider` | `not supported by provider` | `fixture-tested` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `partial` | `implemented` | `fixture-tested` | `partial` | `fixture-tested` | `intentionally omitted` |
| Google Vertex AI Imagen generation | `google-vertex-images` | `preview` | `not supported by provider` | `not supported by provider` | `fixture-tested` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `not supported by provider` | `partial` | `implemented` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` |
| DeepSeek, Groq, Cerebras, and Together OpenAI-compatible Chat Completions | `openai-completions` | `preview` | `fixture-tested` | `partial` | `not supported by provider` | `fixture-tested` | `partial` | `partial` | `partial` | `partial` | `fixture-tested` | `fixture-tested` | `fixture-tested` | `intentionally omitted` | `fixture-tested` | `intentionally omitted` |

## Evidence references

- `openai-completions`: [provider/openai/completions_test.go](../provider/openai/completions_test.go), [provider/openai/compat_test.go](../provider/openai/compat_test.go), [internal/sse/testdata/openai/text_usage.sse](../internal/sse/testdata/openai/text_usage.sse), [internal/sse/testdata/openai/tool_call.sse](../internal/sse/testdata/openai/tool_call.sse).
- Fireworks `openai-completions`: [provider/fireworks/fireworks_test.go](../provider/fireworks/fireworks_test.go), [provider/openai/compat_test.go](../provider/openai/compat_test.go).
- xAI/Grok `openai-completions` and Grok 4.5 `openai-responses`: [provider/xai/xai_test.go](../provider/xai/xai_test.go), [provider/openai/responses_test.go](../provider/openai/responses_test.go).
- NVIDIA NIM `openai-completions` and `openai-embeddings`: [provider/nvidia/nvidia_test.go](../provider/nvidia/nvidia_test.go), [modeldata_test.go](../modeldata_test.go).
- Xiaomi MiMo `openai-completions`: [provider/xiaomi/xiaomi_test.go](../provider/xiaomi/xiaomi_test.go), [modeldata_test.go](../modeldata_test.go).
- OpenCode `openai-completions`: [provider/openai/compat_test.go](../provider/openai/compat_test.go), [internal/modeldata/modeldata_test.go](../internal/modeldata/modeldata_test.go).
- `openai-responses`: [provider/openai/responses_test.go](../provider/openai/responses_test.go).
- `azure-openai-responses`: [provider/azure/azure_test.go](../provider/azure/azure_test.go), [provider/openai/azure_responses_test.go](../provider/openai/azure_responses_test.go).
- `openai-codex-responses`: [provider/openai/codex_responses_test.go](../provider/openai/codex_responses_test.go).
- GitHub Copilot compatible text routes: [provider/githubcopilot/githubcopilot_test.go](../provider/githubcopilot/githubcopilot_test.go), [provider/openai/completions_test.go](../provider/openai/completions_test.go), [provider/openai/responses_test.go](../provider/openai/responses_test.go).
- Cloudflare AI Gateway compatible text routes: [provider/cloudflare/cloudflare_test.go](../provider/cloudflare/cloudflare_test.go), [provider/openai/completions_test.go](../provider/openai/completions_test.go), [provider/openai/responses_test.go](../provider/openai/responses_test.go).
- Cloudflare Workers AI direct Chat Completions: [provider/cloudflare/cloudflare_test.go](../provider/cloudflare/cloudflare_test.go), [provider/openai/completions_test.go](../provider/openai/completions_test.go).
- Vercel AI Gateway `anthropic-messages`: [provider/vercel/vercel_test.go](../provider/vercel/vercel_test.go), [provider/anthropic/anthropic_test.go](../provider/anthropic/anthropic_test.go).
- Hugging Face Router `openai-completions`: [openai_compatible_provider_rows_test.go](../openai_compatible_provider_rows_test.go), [modeldata_test.go](../modeldata_test.go), [provider/openai/completions_test.go](../provider/openai/completions_test.go).
- OpenRouter `openai-completions`: [openai_compatible_provider_rows_test.go](../openai_compatible_provider_rows_test.go), [provider/openai/compat_test.go](../provider/openai/compat_test.go), [modeldata_test.go](../modeldata_test.go).
- DeepSeek, Groq, Cerebras, and Together `openai-completions`: [openai_compatible_provider_rows_test.go](../openai_compatible_provider_rows_test.go), [provider/openai/completions_test.go](../provider/openai/completions_test.go), [provider/openai/compat_test.go](../provider/openai/compat_test.go).
- `anthropic-messages`: [provider/anthropic/anthropic_test.go](../provider/anthropic/anthropic_test.go), [provider/anthropic/oauth_test.go](../provider/anthropic/oauth_test.go), [provider/kimi/kimi_test.go](../provider/kimi/kimi_test.go).
- `google-generative-ai`: [provider/google/google_test.go](../provider/google/google_test.go).
- `google-vertex`: [provider/google/vertex_test.go](../provider/google/vertex_test.go).
- `mistral-conversations`: [provider/mistral/mistral_test.go](../provider/mistral/mistral_test.go).
- MiniMax `anthropic-messages`: [provider/minimax/minimax_test.go](../provider/minimax/minimax_test.go), [modeldata_test.go](../modeldata_test.go).
- `bedrock-converse-stream`: [provider/bedrock/bedrock_test.go](../provider/bedrock/bedrock_test.go).
- `openai-images`: [provider/openai/images_test.go](../provider/openai/images_test.go), [image_models_generated.go](../image_models_generated.go).
- `openrouter-images`: [provider/openrouter/images_test.go](../provider/openrouter/images_test.go).
- `google-images`, `google-vertex-images`: [provider/google/surfaces_test.go](../provider/google/surfaces_test.go), [image_models_generated.go](../image_models_generated.go).
- Cost calculation: [usage_test.go](../usage_test.go) and provider stream tests that assert usage mapping.
- Built-in model metadata registration: [modeldata_test.go](../modeldata_test.go).

## Known limitations and compatibility risks

- Default registry entries are metadata-only. Importing provider packages and calling their `Register` functions is still required for runtime provider dispatch.
- `openai-images` supports generation, reference-image edits, explicit `dall-e-2` variations, and streaming partial image events. Live validation remains outside deterministic CI.
- `google-images` supports Gemini API Imagen `predict` generation and Gemini
  image `generateContent` image outputs. Edits, variations, and live image
  validation remain outside deterministic CI.
- `google-vertex-images` supports Vertex Imagen `predict` generation with
  explicit project/location routing. Ambient routing and live validation remain
  outside deterministic CI.
- Azure OpenAI Responses has generated metadata and a first-class provider
  wrapper over the existing Responses adapter. Codex Responses has generated
  metadata and remains registered through the OpenAI provider package.
- Promoted OpenAI-compatible wrappers such as Fireworks, OpenRouter, xAI,
  Xiaomi, OpenCode, NVIDIA NIM, DeepSeek, Groq, Cerebras, Together, GitHub
  Copilot, Cloudflare AI Gateway, and Cloudflare Workers AI rely on shared
  compatibility detection or explicit Chat Completions metadata. Grok 4.5
  instead uses Responses compatibility metadata; unpromoted compatible rows
  are not independently release-complete.
- NVIDIA NIM uses shared OpenAI-compatible text and embedding adapters through
  a thin wrapper. Its first-class coverage is limited to direct route
  registration, text request shape, embedding request shape, generated
  metadata, and provider-specific embedding input-type mapping.
- OpenCode Zen and OpenCode Go coverage is limited to curated
  `openai-completions` models. Source-package OpenCode models that route through
  OpenAI Responses, Anthropic Messages, or Google APIs are not Go parity today.
- Caller-defined custom and local OpenAI-compatible endpoints are covered by the
  MVP `openai-completions` row when they use explicit compatibility metadata.
- OpenAI-compatible Chat Completions supports typed `tool_choice`, opt-in
  Anthropic-style cache markers, and opt-in `tool_stream`; GitHub Copilot and
  Cloudflare AI Gateway provider-specific headers plus Cloudflare Workers AI
  account placeholder resolution are fixture-tested through their wrappers.
- OpenAI Responses and Codex Responses support typed prompt cache retention,
  parallel tool calls, text verbosity, bounded replay IDs, and image-capable
  tool-result replay through fixture-tested payload coverage.
- Vercel AI Gateway uses the shared Anthropic Messages adapter through a thin
  provider wrapper. Its first-class coverage is limited to direct route
  registration, request shape, generated metadata reuse, provider errors, and
  stream cancellation.
- Hugging Face Router uses the shared OpenAI-compatible Chat Completions
  adapter through a thin provider wrapper. Its first-class coverage is limited
  to direct route registration, request shape, focused generated metadata,
  provider errors, and stream cancellation.
- Google Vertex reuses the Google payload and stream parser, but only a narrower Vertex-specific fixture set exists today.
- Codex Responses includes browser callback and device-code OAuth login, refresh
  helpers, a caller-wrapped token provider, and fixture-tested direct
  WebSocket transport with session caching and SSE fallback. Token persistence
  and proxy-aware WebSocket dialing are out of scope.
- Bedrock uses stdlib HTTP, SigV4 signing, and EventStream parsing rather than the AWS SDK. The built-in environment credential path is intentionally limited to `AWS_BEARER_TOKEN_BEDROCK` or static AWS keys; profiles, SSO, web identity, IMDS, and shared-config loading require caller-supplied credentials through Sigma auth resolvers. Typed Bedrock request controls, custom non-reserved headers, retry behavior, and response debug hooks have deterministic fixture coverage.
- The Anthropic-compatible routing in the Anthropic row title covers Kimi,
  Kimi Coding, Fireworks, and Xiaomi compat branches. Each branch has
  deterministic compatibility coverage in `provider/anthropic`; Kimi, Kimi
  Coding, Fireworks, MiniMax, and MiniMax CN also use thin provider wrappers
  over the same Anthropic-compatible adapter with direct registration and
  endpoint-path coverage. Xiaomi API-billing and token-plan rows use a separate
  OpenAI-compatible provider wrapper with deterministic registration and
  endpoint-path coverage.
- Mistral Conversations maps cache-enabled `sigma.WithSessionID` requests to
  Mistral `prompt_cache_key` and `x-affinity`, and streamed cached prompt
  tokens are accounted as cache reads. Duration-specific retention choices are
  still limited by the provider's Conversations API.
- Mistral Conversations supports base64 and URL image input for image-capable
  models, and replays image-bearing tool results as string image references.
  File image references, built-in
  connector tools, append, and restart remain deferred.
- Anthropic Messages supports Claude Pro/Max OAuth: browser callback login with
  a manual code-paste fallback, refresh helpers, an in-memory OAuth token
  provider, and automatic Claude Code identity (beta headers, identity system
  block, tool-name canonicalization) when the resolved credential is an OAuth
  token. Token persistence is caller-owned.
- The `not supported by provider` value in the OAuth column for the Bedrock row describes the provider's native authentication model, not a missing code path. The adapter still forwards an OAuth-typed `Credential` returned by an auth resolver as a bearer token, with fixture coverage.
- Live tests are skipped by default and are not an MVP readiness signal unless a row explicitly says `live-tested`.
- Cross-provider context handoff is not covered by this matrix; unsupported handoff behavior must remain explicit in later compatibility docs.
- MVP rows are the only provider rows that may be described as release-ready in
  README wording. Preview rows may be documented, but not advertised as part of
  the first release gate.
