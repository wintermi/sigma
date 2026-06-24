# Providers

Sigma separates model metadata from provider implementations. A model entry says
what can be requested; a provider entry makes the request runnable. Register
both on the same `*sigma.Registry`, then pass that registry to `sigma.NewClient`.

```go
registry := sigma.NewRegistry()
if err := openai.Register(registry, sigma.ProviderOpenAI); err != nil {
	return err
}
model := sigma.Model{
	ID:              "gpt-4o-mini",
	Provider:        sigma.ProviderOpenAI,
	API:             sigma.APIOpenAICompletions,
	SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
}
if err := registry.RegisterModel(model); err != nil {
	return err
}
client := sigma.NewClient(sigma.WithRegistry(registry))
```

`sigma.DefaultRegistry` returns a clone of built-in metadata. Provider packages
are not automatically imported for you; import and register the providers your
application uses. Use `sigma.NewRegistry` for tests, local endpoints, and
applications that need explicit provider control.

## Request Flow

```text
Client.CompleteText
  -> Client.Complete
  -> Client.Stream
  -> Registry.Model and Registry.TextProvider
  -> provider.Stream
  -> Stream.Events
  -> Collect
  -> AssistantMessage
```

Image calls use the same registry pattern, but dispatch through
`Registry.ImageProvider` and return `AssistantImages`.

## Credentials

Provider adapters receive credentials through `sigma.Options.AuthResolver`.
Application code can provide credentials in four ways:

- `sigma.WithAPIKey("...")` for a single request.
- `sigma.WithAuthResolver(...)` on a client.
- `sigma.WithProviderAuthResolver(provider, ...)` for one provider.
- Environment variables through `sigma.EnvironmentAuthResolver`, which is part
  of the default credential chain.

Use `EnvironmentAuthResolver.EnvVars` to show the ordered environment variables
Sigma would check for a model, or `ConfiguredEnvVars` to show which of those
names are currently set without exposing secret values:

```go
resolver := sigma.EnvironmentAuthResolver{}
names := resolver.EnvVars(model)
configured := resolver.ConfiguredEnvVars(model)
_, _ = names, configured
```

Do not put credentials in `Request`, `ProviderMetadata`, tool arguments, or
persisted JSON. See [Security](security.md) for redaction behavior.

## Setup Snippets

### OpenAI Chat Completions

```go
registry := sigma.NewRegistry()
_ = openai.Register(registry, sigma.ProviderOpenAI)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `OPENAI_API_KEY`.

Use `openai.Register` for `APIOpenAICompletions`. This adapter also supports
OpenAI-compatible routers and local endpoints when model metadata describes the
endpoint compatibility.

### OpenAI Responses

```go
registry := sigma.NewRegistry()
_ = openai.RegisterResponses(registry, sigma.ProviderOpenAI)
```

Environment: `OPENAI_API_KEY`.

Use `APIOpenAIResponses` model metadata. Responses supports streaming output,
reasoning summaries, tool calls, image input, and usage where the upstream
response includes it.

### Azure OpenAI Responses

```go
registry := sigma.NewRegistry()
_ = azure.Register(registry)
```

Environment: `AZURE_OPENAI_API_KEY` for API-key auth. Microsoft Entra auth uses
`azure.WithTokenCredential` with a caller-supplied token source.

Model metadata should include `AzureOpenAIResponsesConfig`, or requests should
set endpoint, deployment, API version, and credential source with the Azure
option helpers. Use `openai.RegisterAzureResponses` when registering a custom
provider ID instead of the built-in Azure OpenAI Responses provider ID.

### OpenAI Codex Responses

```go
registry := sigma.NewRegistry()
_ = openai.RegisterCodexResponses(registry, sigma.ProviderGitHubCopilot)
```

Codex Responses requires OAuth credentials through
`openai.WithCodexResponsesOAuthTokenProvider`. Use
`openai.LoginOpenAICodexBrowser`, `openai.LoginOpenAICodexDeviceCode`,
`openai.RefreshOpenAICodexToken`, and `openai.NewCodexOAuthTokenProvider` for
stdlib-only login and refresh. Sigma does not implement token storage or
WebSocket transport. Codex image input should use HTTPS image URLs; ChatGPT
Codex rejects base64 image payloads.

### GitHub Copilot

```go
registry := sigma.DefaultRegistry()
_ = githubcopilot.RegisterResponses(registry)
_ = githubcopilot.RegisterAnthropic(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `COPILOT_GITHUB_TOKEN`.

`provider/githubcopilot` is a thin wrapper over the shared OpenAI-compatible and
Anthropic-compatible adapters. Use `Register` for Chat Completions models,
`RegisterResponses` for Responses models, and `RegisterAnthropic` for
Anthropic Messages models. The wrapper applies the Copilot base URL, the
Copilot dynamic initiator/intent/vision headers, static model headers from
metadata, and bearer-token auth.

### Cloudflare AI Gateway

```go
registry := sigma.DefaultRegistry()
_ = cloudflare.RegisterAIGatewayResponses(registry)
_ = cloudflare.RegisterAIGatewayAnthropic(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `CLOUDFLARE_API_KEY`, `CLOUDFLARE_ACCOUNT_ID`, and
`CLOUDFLARE_GATEWAY_ID`.

`provider/cloudflare` exposes AI Gateway helpers for OpenAI-compatible and
Anthropic-compatible text routes. The wrapper resolves account and gateway
placeholders in the base URL and sends API keys with Cloudflare's
`cf-aig-authorization` header.

Direct Cloudflare Workers AI Chat Completions routes use the same package with
normal bearer-token auth:

```go
registry := sigma.DefaultRegistry()
_ = cloudflare.RegisterWorkersAI(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `CLOUDFLARE_API_KEY` and `CLOUDFLARE_ACCOUNT_ID`.

Use `cloudflare.WithWorkersAIAccountID` for request-scoped account placeholder
resolution when the process environment should not provide the account ID.

### Vercel AI Gateway

```go
registry := sigma.DefaultRegistry()
_ = vercel.Register(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `AI_GATEWAY_API_KEY`.

The Vercel AI Gateway wrapper uses Sigma's shared Anthropic-compatible Messages
adapter with the direct Vercel AI Gateway base URL. Built-in metadata is
available under `ProviderVercelAIGateway` for curated gateway text routes,
including adaptive thinking and temperature compatibility metadata where
required by the route.

### Hugging Face Router

```go
registry := sigma.DefaultRegistry()
_ = huggingface.Register(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `HF_TOKEN`.

`provider/huggingface` is a thin wrapper over Sigma's shared
OpenAI-compatible Chat Completions adapter with the Hugging Face Router base
URL. Built-in metadata is intentionally focused on a small reviewed set of
router text routes; broad router catalog expansion stays in the reviewed
catalog workflow.

### Anthropic Messages

```go
registry := sigma.NewRegistry()
_ = anthropic.Register(registry, sigma.ProviderAnthropic)
```

Environment: `ANTHROPIC_API_KEY`.

Claude Pro/Max subscriptions can authenticate with OAuth instead of an API
key. Use `anthropic.LoginAnthropicBrowser`, `anthropic.RefreshAnthropicToken`,
and `anthropic.NewAnthropicOAuthTokenProvider` for stdlib-only browser
callback login, refresh, and request-time token resolution; credential
persistence stays caller-owned. When the resolved credential is an Anthropic
OAuth token, the adapter automatically sends the required Claude Code identity
(beta headers, identity system block, and canonical tool-name casing, with
streamed tool names restored to the caller's casing). Browser login binds the
provider-registered callback at `http://localhost:53692/callback`.

This adapter also handles Anthropic-compatible endpoints used by some Kimi,
Fireworks, and Xiaomi routes. Compatibility varies by endpoint; check
[provider parity](provider-parity.md).

### Kimi and Kimi Coding

```go
registry := sigma.DefaultRegistry()
_ = kimi.Register(registry)
_ = kimi.RegisterCoding(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `KIMI_API_KEY`.

The Kimi wrappers use Sigma's shared Anthropic-compatible Messages adapter with
the Kimi endpoint base URL and Kimi CLI request header defaults. Built-in
metadata is available under `ProviderKimi` for the canonical `kimi-for-coding`
route. `ProviderKimiCoding` carries the expanded coding model family with
`k2p7`, `kimi-for-coding`, and `kimi-k2-thinking`, including adaptive thinking,
session-affinity, tool-use, and image-input metadata where supported by the
model.

### Fireworks AI

```go
registry := sigma.DefaultRegistry()
_ = fireworks.Register(registry)
_ = fireworks.RegisterAnthropic(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `FIREWORKS_API_KEY`.

The built-in Fireworks text model route is the Fire Pass router
`accounts/fireworks/routers/kimi-k2p6-turbo`, named
`Kimi K2.6 Turbo (Firepass)`. The same OpenAI-compatible path also includes
`accounts/fireworks/models/kimi-k2p7-code`. The adapter uses Fireworks'
OpenAI-compatible Chat Completions endpoint and supports streaming text, usage,
thinking, and function tools in the shared `openai-completions` path.
`sigma.WithReasoningLevel` maps to Fireworks `reasoning_effort`;
`sigma.WithThinkingBudgetTokens` maps to the Fireworks `thinking` object.

The built-in Fireworks Anthropic-compatible routes are
`accounts/fireworks/models/kimi-k2p6` and
`accounts/fireworks/models/kimi-k2p7-code` under
`ProviderFireworksAnthropic`. Register them with
`fireworks.RegisterAnthropic`; they use the shared Anthropic Messages adapter
against Fireworks' `/messages` endpoint and carry compatibility metadata for
image input, thinking levels, cache behavior, and tool use.

### NVIDIA NIM

```go
registry := sigma.DefaultRegistry()
_ = nvidia.Register(registry)
_ = nvidia.RegisterEmbeddings(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `NVIDIA_API_KEY`.

The NVIDIA NIM wrapper uses Sigma's shared OpenAI-compatible Chat Completions
and Embeddings adapters with the direct NIM base URL. Built-in text metadata is
available under `ProviderNVIDIA`, and built-in embedding metadata includes
`nvidia/nv-embedqa-e5-v5`. The embedding wrapper maps
`sigma.EmbeddingInputTypeQuery` to NVIDIA `input_type: "query"` and
`sigma.EmbeddingInputTypeDocument` to `input_type: "passage"` unless callers
set `EmbeddingRequest.ProviderMetadata["input_type"]` explicitly.

### Moonshot AI

```go
registry := sigma.DefaultRegistry()
_ = moonshot.Register(registry)
_ = moonshot.RegisterCN(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `MOONSHOT_API_KEY`.

The Moonshot wrappers use Sigma's shared OpenAI-compatible Chat Completions
adapter with direct Moonshot AI and Moonshot AI CN base URL defaults. Built-in
metadata includes direct Kimi K2 rows such as `kimi-k2.7-code` and
`kimi-k2.7-code-highspeed`, including image input, tool use, DeepSeek-style
thinking controls, streaming usage, and K2.7 compatibility metadata that omits
explicit disabled-thinking payloads by default.

### Xiaomi MiMo

```go
registry := sigma.DefaultRegistry()
_ = xiaomi.Register(registry)
_ = xiaomi.RegisterTokenPlanAMS(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `XIAOMI_API_KEY` for `ProviderXiaomi`,
`XIAOMI_TOKEN_PLAN_CN_API_KEY` for `ProviderXiaomiTokenPlanCN`,
`XIAOMI_TOKEN_PLAN_AMS_API_KEY` for `ProviderXiaomiTokenPlanAMS`, and
`XIAOMI_TOKEN_PLAN_SGP_API_KEY` for `ProviderXiaomiTokenPlanSGP`.

The Xiaomi wrapper uses Sigma's shared OpenAI-compatible Chat Completions
adapter for the API-billing and regional token-plan `/v1` routes. Built-in
metadata includes the API-billing MiMo rows plus token-plan rows for
`mimo-v2-omni`, `mimo-v2-pro`, `mimo-v2.5`, `mimo-v2.5-pro`, and
`mimo-v2.5-pro-ultraspeed`. Token-plan metadata intentionally omits
`mimo-v2-flash`, which remains scoped to the API-billing provider.

### Google Generative AI

```go
registry := sigma.NewRegistry()
_ = google.Register(registry, sigma.ProviderGoogle)
```

Environment: `GOOGLE_API_KEY` or `GOOGLE_CLOUD_API_KEY`.

The Gemini API adapter supports text, image input, streaming, tools, thinking
metadata, and usage in fixture-tested paths.

### Google Vertex AI

```go
registry := sigma.NewRegistry()
_ = google.RegisterVertex(registry, sigma.ProviderGoogleVertex,
	google.WithVertexConfig(google.VertexConfig{
		ProjectID: "my-project",
		Location:  "us-central1",
	}),
)
```

Applications commonly resolve routing from `GOOGLE_CLOUD_PROJECT` and
`GOOGLE_CLOUD_LOCATION` or `GOOGLE_CLOUD_REGION`, then pass it through
`VertexConfig` or provider options. API-key auth can use `GOOGLE_API_KEY` or
`GOOGLE_CLOUD_API_KEY`; ADC/OAuth auth should be supplied with
`google.WithVertexTokenProvider`.

### Mistral Conversations

```go
registry := sigma.NewRegistry()
_ = mistral.Register(registry, sigma.ProviderMistral)
```

Environment: `MISTRAL_API_KEY`.

The current adapter covers streaming text, streamed thinking chunks, function
tools, request-scoped `x-affinity` session reuse through `sigma.WithSessionID`,
base64 image input, image-bearing tool results, and replay of cross-provider
tool-call IDs. URL/file image references, built-in connectors, append, and
restart are not implemented.

### Amazon Bedrock Converse Stream

```go
registry := sigma.NewRegistry()
_ = bedrock.Register(registry, sigma.ProviderAmazonBedrock,
	bedrock.WithRegion("us-east-1"),
)
```

The Bedrock adapter uses stdlib HTTP, SigV4 signing, and EventStream parsing; it
does not import the AWS SDK. Configure the region with `bedrock.WithRegion` or
provider options; if neither is set, it falls back to `AWS_REGION` and then
`AWS_DEFAULT_REGION`. The built-in environment credential path supports
`AWS_BEARER_TOKEN_BEDROCK`, or `AWS_ACCESS_KEY_ID` plus
`AWS_SECRET_ACCESS_KEY` and optional `AWS_SESSION_TOKEN`. AWS profiles, SSO,
web identity, IMDS, and shared-config loading are intentionally not implemented;
applications that need them should resolve credentials before calling Sigma and
pass them through `sigma.WithAuthResolver` or a provider-specific auth resolver.
Tests can inject `ConverseStreamClient` and `CredentialDetector` fakes, or use
`bedrock.WithEndpoint` with an `httptest.Server`.

Use `sigma.WithBedrockOptions` for Bedrock-specific request controls such as
tool choice, thinking display, interleaved thinking, stop sequences, top-p,
request metadata, additional model request fields, and response field paths.
Request headers from `sigma.WithHeader` and `sigma.WithHeaders` are applied
before SigV4 signing; `authorization`, `host`, and `x-amz-*` headers remain
owned by the adapter.

### OpenRouter Chat Completions

```go
registry := sigma.NewRegistry()
_ = openrouter.Register(registry)
```

Environment: `OPENROUTER_API_KEY`.

OpenRouter text generation uses the shared OpenAI-compatible Chat Completions
adapter with OpenRouter base URL defaults, generated model metadata, prompt
cache markers, nested reasoning requests, and request-scoped routing options.

### OpenRouter Images

```go
registry := sigma.NewRegistry()
_ = openrouter.RegisterImages(registry)
```

Environment: `OPENROUTER_API_KEY`.

OpenRouter image generation is non-streaming and uses image-capable Chat
Completions responses. OpenAI Images uses the dedicated OpenAI Images adapter
for generation, edits, variations, and streaming partial image events.

## Provider Options

Use root options for common behavior:

- `sigma.WithTimeout`
- `sigma.WithMaxRetries`
- `sigma.WithMaxRetryDelay`
- `sigma.WithHeader` and `sigma.WithHeaders`
- `sigma.WithSessionID`
- `sigma.WithProviderOption` and `sigma.WithProviderOptions`

Provider-specific helper functions are thin wrappers over `ProviderOptions`.
Prefer helpers when they exist because they document the expected key names.

## Current Coverage

The authoritative implementation status is the
[provider parity matrix](provider-parity.md). Do not assume a provider ID is
runnable just because metadata exists.
