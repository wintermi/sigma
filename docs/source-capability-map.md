# Source capability map

This map connects source-package provider/API concepts to the current Go
packages and metadata fields. It should be read with
[provider-parity.md](provider-parity.md), which records feature readiness.

## API family mapping

| Source API or family | Go API constant | Go package or metadata owner | Current role |
| --- | --- | --- | --- |
| OpenAI Chat Completions and OpenAI-compatible providers | `openai-completions` | [provider/openai](../provider/openai), `Model.OpenAICompletionsCompat`, `OpenAIOptions` | Shared text adapter for OpenAI-compatible endpoints, OpenRouter text routing, custom/local endpoints, typed tool choice, and compatibility-specific cache/tool-stream payloads. |
| Fireworks OpenAI-compatible Chat Completions | `openai-completions` | [provider/fireworks](../provider/fireworks), [provider/openai](../provider/openai) | Fireworks convenience wrapper over the shared Chat Completions adapter; generated metadata includes the Fire Pass Kimi K2.6 Turbo router. |
| xAI/Grok OpenAI-compatible Chat Completions | `openai-completions` | [provider/xai](../provider/xai), [provider/openai](../provider/openai) | xAI convenience wrapper over the shared Chat Completions adapter, with xAI defaults, `XAI_API_KEY` credential fallback, and Grok compatibility detection. |
| OpenCode Zen and OpenCode Go OpenAI-compatible Chat Completions | `openai-completions` | [provider/openai](../provider/openai), `Model.OpenAICompletionsCompat` | Curated built-in model metadata for the OpenCode OpenAI-compatible routes using `OPENCODE_API_KEY`; non-OpenAI OpenCode routes remain outside this mapping. |
| OpenAI Responses | `openai-responses` | [provider/openai](../provider/openai), `Model.API`, `OpenAIOptions` | Separate Responses adapter for response IDs, reasoning summaries, output blocks, tool-result images, bounded replay IDs, and Responses-specific options. |
| Azure OpenAI Responses | `azure-openai-responses` | [provider/openai](../provider/openai), `Model.AzureOpenAIResponses` | Azure endpoint/deployment/API-version wrapper over Responses semantics. |
| OpenAI Codex Responses | `openai-codex-responses` | [provider/openai](../provider/openai), `Model.OpenAICodexResponses`, `OpenAIOptions` | Codex-specific Responses wrapper with caller-supplied OAuth token provider, text verbosity, cache-retention payload fields, and transport gating. |
| Anthropic Messages | `anthropic-messages` | [provider/anthropic](../provider/anthropic), `Model.ProviderMetadata["modelFamily"]` | Text adapter for Anthropic and Anthropic-compatible variants such as Kimi, Fireworks, and Xiaomi. |
| Google Gemini API | `google-generative-ai` | [provider/google](../provider/google) | Text adapter for Gemini API payloads, streaming, tool calls, thinking parts, and usage metadata. |
| Google Vertex AI | `google-vertex` | [provider/google](../provider/google), Vertex provider config | Vertex routing/auth wrapper that reuses the Google payload and stream parser. |
| Mistral Conversations | `mistral-conversations` | [provider/mistral](../provider/mistral) | Text adapter for Mistral Conversations streaming, thinking chunks, session affinity, and tool-call deltas. |
| Amazon Bedrock Converse Stream | `bedrock-converse-stream` | [provider/bedrock](../provider/bedrock) | AWS-isolated text adapter with stdlib SigV4/EventStream transport, injectable Converse Stream client, and credential detector. |
| OpenAI Images | `openai-images` | [provider/openai](../provider/openai), [image_models_generated.go](../image_models_generated.go) | Generation-only adapter over OpenAI's dedicated Images API plus generated image model metadata. |
| OpenRouter image generation through Chat Completions | `openrouter-images` | [provider/openrouter](../provider/openrouter), `ImageModel.ProviderMetadata` | Image-generation adapter over OpenRouter chat-completions image responses. |

## Provider ID mapping

| Source provider family | Go provider ID | API path today | Notes |
| --- | --- | --- | --- |
| OpenAI | `openai` | `openai-responses`, `openai-completions`, `openai-images` | Text Responses, Chat Completions, and Images generation adapters exist. |
| Azure OpenAI | caller-chosen, usually Azure-specific | `azure-openai-responses` | Uses model/request `AzureOpenAIResponses` config rather than generated default metadata. |
| OpenAI Codex | caller-chosen, usually Codex-specific | `openai-codex-responses` | Requires explicit OAuth token provider; no interactive login. |
| Anthropic | `anthropic` | `anthropic-messages` | Generated metadata includes a Claude text model. |
| Amazon Bedrock | `amazon-bedrock` | `bedrock-converse-stream` | Generated metadata includes a Claude-on-Bedrock text model. |
| Google Gemini API | `google` | `google-generative-ai` | Generated metadata includes Gemini text. |
| Google Vertex AI | `google-vertex` | `google-vertex` | Generated metadata includes a representative Gemini Vertex route; callers still supply project/location routing. |
| Mistral | `mistral` | `mistral-conversations` | Generated metadata includes Mistral Large text plus representative adjustable and native reasoning models. |
| OpenRouter | `openrouter` | `openai-completions`, `openrouter-images` | Generated metadata includes one text route and image routes for Gemini and Grok Imagine routed models. |
| OpenCode Zen, OpenCode Go | `opencode`, `opencode-go` | `openai-completions` | Generated metadata includes curated OpenAI-compatible text routes. Register the shared OpenAI-compatible provider under these IDs to make requests. |
| xAI/Grok | `xai` | `openai-completions` | Use [provider/xai](../provider/xai) for Grok Chat Completions requests. Generated metadata includes curated Grok text, image-input, and reasoning-capable routes with `XAI_API_KEY` credential metadata. |
| DeepSeek, Groq, Cerebras, Together, GitHub Copilot | `deepseek`, `groq`, `cerebras`, `together`, `github-copilot` | `openai-completions` or `openai-responses` when caller registers compatible providers | Generated metadata includes representative metadata-only routes, but first-class provider parity still needs fixtures. |
| Fireworks | `fireworks` | `openai-completions`; `anthropic-messages` when caller registers compatible models | Generated metadata includes the Fire Pass Kimi K2.6 Turbo router for the OpenAI-compatible endpoint. Anthropic-compatible routing remains caller-registered. |
| Kimi, Xiaomi | `kimi`, `xiaomi` | `anthropic-messages` or `openai-completions` when caller registers compatible providers | Generated metadata includes representative metadata-only routes with compatibility metadata. |
| Custom/local endpoints | `custom` or caller-defined | Usually `openai-completions` | Use explicit registry entries, `WithBaseURL`, and compatibility metadata. |

## Metadata flags

| Capability | Go metadata or option | Used by |
| --- | --- | --- |
| Text API family | `Model.API` | Registry validation and provider dispatch. |
| Image API family | `ImageModel.API` | Registry validation and image-provider dispatch. |
| Text/image input support | `Model.SupportedInputs`, `Model.SupportsInput`, `Model.SupportsImages` | Client/provider validation and compatibility docs. |
| Tool support | `Model.SupportsTools` | Model discovery and provider payload decisions. |
| Thinking support | `Model.SupportsThinking`, `Model.ThinkingLevels`, `Model.ThinkingLevelMap` | `WithReasoningLevel`, provider reasoning payload mapping, and model discovery. |
| Default transport | `Model.DefaultTransport`, `Options.Transport` | Provider request transport selection and transport validation. |
| Token costs | `InputCostPerMillion`, `OutputCostPerMillion`, cache cost fields, `CostCurrency` | `CostForUsage` and stream/image response cost reporting. |
| Image costs | `ImageModel.ProviderMetadata["cost"]` | Image model metadata and release docs; provider-specific cost calculation is still limited. |
| Provider API keys | `ProviderMetadata["apiKeyEnvVars"]`, `ProviderMetadata["apiKeyEnvVar"]` | `EnvironmentAuthResolver`. |
| Provider base URL | `ProviderMetadata["baseURL"]`, provider `WithBaseURL`, provider-specific endpoint options | Default documentation and adapter endpoint construction. |
| Provider default headers | `ProviderMetadata["headers"]`, provider `WithHeader`, request `WithHeader` | Request construction and fixture assertions. |
| Routed provider identity | `ProviderMetadata["routedProvider"]` | Gateway/provider-router metadata, especially OpenRouter. |
| OpenAI-compatible behavior | `Model.OpenAICompletionsCompat` | Chat Completions payload, compatibility detection, reasoning formats, and provider replay quirks. |
| OpenAI request controls | `OpenAIOptions` | Chat Completions `tool_choice`; Responses/Codex reasoning, service tier, prompt cache retention, parallel tool calls, and text verbosity. |
| OpenRouter routing | `OpenAICompletionsCompat.OpenRouterRouting` | Chat Completions provider options for OpenRouter-style routing. |
| Vercel AI Gateway routing | `OpenAICompletionsCompat.VercelAIGatewayRouting` | Compatibility metadata; no generated built-in route today. |
| Azure Responses configuration | `Model.AzureOpenAIResponses` | Azure endpoint, deployment, API version, and credential-source selection. |
| Codex Responses configuration | `Model.OpenAICodexResponses` | Codex model-name override and OAuth-token provider requirement. |
| Image shape limits | `ImageModel.MaxWidth`, `MaxHeight`, `SupportedSizes`, `SupportedFormats` | Image model discovery and request validation docs. |

## Source capabilities not yet represented as complete Go parity

- OpenAI Images is generation-only; edits, variations, streaming partial images, and Responses image-tool generation are deferred.
- Automatic provider/model discovery is generated from curated metadata, not live provider listing calls.
- Interactive OAuth login and credential persistence are intentionally absent.
- Cross-provider context handoff and capability-loss reporting are future work.
- Source-level provider breadth is larger than generated default models. Several provider IDs exist only for caller-registered compatible models today, and OpenCode coverage is limited to curated OpenAI-compatible routes.
- GitHub Copilot dynamic headers, Cloudflare AI Gateway auth header rewriting,
  and Codex WebSocket session caching remain deferred.
- Live-test coverage is opt-in and sparse. Standard tests must remain deterministic and credential-free.
