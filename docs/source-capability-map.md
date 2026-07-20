# Source capability map

This map connects source-package provider/API concepts to the current Go
packages and metadata fields. It should be read with
[provider-parity.md](provider-parity.md), which records feature readiness.

## API family mapping

| Source API or family | Go API constant | Go package or metadata owner | Current role |
| --- | --- | --- | --- |
| OpenAI Chat Completions and OpenAI-compatible providers | `openai-completions` | [provider/openai](../provider/openai), `Model.OpenAICompletionsCompat`, `OpenAIOptions` | Shared text adapter for OpenAI-compatible endpoints, OpenRouter text routing, custom/local endpoints, typed tool choice, and compatibility-specific cache/tool-stream payloads. |
| Fireworks OpenAI-compatible Chat Completions | `openai-completions` | [provider/fireworks](../provider/fireworks), [provider/openai](../provider/openai) | Fireworks convenience wrapper over the shared Chat Completions adapter; generated metadata includes the Fire Pass Kimi K2.6 Turbo router and Kimi K2.7 Code. |
| Fireworks Anthropic-compatible Messages | `anthropic-messages` | [provider/fireworks](../provider/fireworks), [provider/anthropic](../provider/anthropic), `Model.AnthropicMessagesCompat` | Fireworks convenience wrapper over the shared Anthropic Messages adapter; generated metadata includes the verified Kimi K2.6 and Kimi K2.7 Code Messages routes under `fireworks-anthropic`. |
| Moonshot AI OpenAI-compatible Chat Completions | `openai-completions` | [provider/moonshot](../provider/moonshot), [provider/openai](../provider/openai), `Model.OpenAICompletionsCompat` | Moonshot convenience wrappers over the shared Chat Completions adapter; generated metadata includes direct K2.7 Code and K2.7 Code HighSpeed rows for Moonshot AI and Moonshot AI CN. |
| xAI/Grok OpenAI-compatible Chat Completions | `openai-completions` | [provider/xai](../provider/xai), [provider/openai](../provider/openai) | xAI convenience wrapper over the shared Chat Completions adapter, with xAI defaults, `XAI_API_KEY` credential fallback, and Grok compatibility detection. |
| Hugging Face Router OpenAI-compatible Chat Completions | `openai-completions` | [provider/huggingface](../provider/huggingface), [provider/openai](../provider/openai), `Model.OpenAICompletionsCompat` | Hugging Face convenience wrapper over the shared Chat Completions adapter with router defaults, `HF_TOKEN` credential fallback, and focused generated metadata. |
| OpenCode Zen and OpenCode Go routed text APIs | `openai-completions`, `openai-responses`, `anthropic-messages`, `google-generative-ai` | [provider/opencode](../provider/opencode), `Model.ProviderMetadata["opencodeAPI"]` | Curated OpenCode metadata selects the model-specific route while retaining `OPENCODE_API_KEY` authentication. |
| OpenAI Responses | `openai-responses` | [provider/openai](../provider/openai), `Model.API`, `OpenAIOptions` | Separate Responses adapter for response IDs, reasoning summaries, output blocks, tool-result images, bounded replay IDs, and Responses-specific options. |
| Azure OpenAI Responses | `azure-openai-responses` | [provider/openai](../provider/openai), `Model.AzureOpenAIResponses` | Azure endpoint/deployment/API-version wrapper over Responses semantics. |
| OpenAI Codex Responses | `openai-codex-responses` | [provider/openai](../provider/openai), `Model.OpenAICodexResponses`, `OpenAIOptions` | Codex-specific Responses wrapper with browser callback and device-code OAuth helpers, caller-owned token persistence, text verbosity, cache-retention payload fields, and transport gating. |
| GitHub Copilot compatible text routes | `openai-completions`, `openai-responses`, `anthropic-messages` | [provider/githubcopilot](../provider/githubcopilot), [provider/openai](../provider/openai), [provider/anthropic](../provider/anthropic) | Convenience wrapper over shared OpenAI-compatible and Anthropic-compatible adapters with Copilot defaults, dynamic request headers, and `COPILOT_GITHUB_TOKEN` credential fallback. |
| Cloudflare AI Gateway compatible text routes | `openai-completions`, `openai-responses`, `anthropic-messages` | [provider/cloudflare](../provider/cloudflare), [provider/openai](../provider/openai), [provider/anthropic](../provider/anthropic) | Convenience wrapper over shared compatible adapters with AI Gateway placeholder base URLs, `cf-aig-authorization`, and `CLOUDFLARE_API_KEY` credential fallback. |
| Vercel AI Gateway Anthropic-compatible Messages | `anthropic-messages` | [provider/vercel](../provider/vercel), [provider/anthropic](../provider/anthropic), `Model.AnthropicMessagesCompat` | Convenience wrapper over the shared Anthropic Messages adapter with direct Vercel AI Gateway defaults and `AI_GATEWAY_API_KEY` credential fallback. |
| Anthropic Messages | `anthropic-messages` | [provider/anthropic](../provider/anthropic), `Model.ProviderMetadata["modelFamily"]` | Text adapter for Anthropic and Anthropic-compatible variants such as Kimi, Fireworks, and Xiaomi. |
| Google Gemini API | `google-generative-ai` | [provider/google](../provider/google) | Text adapter for Gemini API payloads, streaming, tool calls, thinking parts, and usage metadata. |
| Google Vertex AI | `google-vertex` | [provider/google](../provider/google), Vertex provider config | Vertex routing/auth wrapper that reuses the Google payload and stream parser. |
| Mistral Conversations | `mistral-conversations` | [provider/mistral](../provider/mistral) | Text adapter for Mistral Conversations streaming, thinking chunks, base64/URL image input, stringified image tool results, session affinity, and tool-call deltas. |
| Radius gateway Messages | `radius-messages` | [provider/radius](../provider/radius), `TextModelSource` | Runtime text adapter with explicit API-key-authenticated model refresh, native SSE streaming, image/thinking/tool replay, and typed gateway errors. |
| Amazon Bedrock Converse Stream | `bedrock-converse-stream` | [provider/bedrock](../provider/bedrock) | AWS-isolated text adapter with stdlib SigV4/EventStream transport, injectable Converse Stream client, and credential detector. |
| OpenAI Images | `openai-images` | [provider/openai](../provider/openai), [image_models_generated.go](../image_models_generated.go) | Generation-only adapter over OpenAI's dedicated Images API plus generated image model metadata. |
| OpenRouter image generation through Chat Completions | `openrouter-images` | [provider/openrouter](../provider/openrouter), `ImageModel.ProviderMetadata` | Image-generation adapter over OpenRouter chat-completions image responses. |
| Google Gemini API image generation | `google-images` | [provider/google](../provider/google), [image_models_generated.go](../image_models_generated.go) | Image adapter over Gemini API Imagen `predict` and Gemini image `generateContent` image outputs. |
| Google Vertex AI Imagen generation | `google-vertex-images` | [provider/google](../provider/google), `VertexConfig`, [image_models_generated.go](../image_models_generated.go) | Vertex Imagen `predict` adapter using explicit project/location routing and Google auth handling. |
| OpenAI Embeddings | `openai-embeddings` | [provider/openai](../provider/openai), [embedding_models_generated.go](../embedding_models_generated.go), `EmbeddingModel` | Vector embedding adapter over OpenAI's `/v1/embeddings` API plus generated and caller-registered embedding model metadata. |
| Google Gemini API embeddings | `google-embeddings` | [provider/google](../provider/google), [embedding_models_generated.go](../embedding_models_generated.go), `EmbeddingModel` | Embeddings adapter over Gemini API `batchEmbedContents`, including task type and output dimensionality mapping. |
| Google Vertex AI embeddings | `google-vertex-embeddings` | [provider/google](../provider/google), `VertexConfig`, [embedding_models_generated.go](../embedding_models_generated.go) | Vertex native embeddings `predict` adapter with explicit project/location routing, API-key or OAuth token auth, and token-count usage mapping. |
| Amazon Bedrock InvokeModel embeddings | `bedrock-embeddings` | [provider/bedrock](../provider/bedrock), [embedding_models_generated.go](../embedding_models_generated.go) | Bedrock `InvokeModel` embeddings adapter for Titan, Cohere, and Nova text embedding request shapes using the existing stdlib credential and signing path. |

## Provider ID mapping

| Source provider family | Go provider ID | API path today | Notes |
| --- | --- | --- | --- |
| OpenAI | `openai` | `openai-responses`, `openai-completions`, `openai-images`, `openai-embeddings` | Text Responses, Chat Completions, Images generation, and embeddings adapters exist. |
| Azure OpenAI | caller-chosen, usually Azure-specific | `azure-openai-responses` | Uses model/request `AzureOpenAIResponses` config rather than generated default metadata. |
| OpenAI Codex | caller-chosen, usually Codex-specific | `openai-codex-responses` | Uses explicit OAuth token providers; includes browser callback login, device-code login, and refresh helpers. |
| Anthropic | `anthropic` | `anthropic-messages` | Generated metadata includes a Claude text model. Includes browser callback OAuth login, refresh helpers, an in-memory OAuth token provider, and automatic Claude Code identity for OAuth tokens. |
| Amazon Bedrock | `amazon-bedrock` | `bedrock-converse-stream`, `bedrock-embeddings` | Generated metadata includes representative Bedrock text and embedding routes. |
| Google Gemini API | `google` | `google-generative-ai`, `google-images`, `google-embeddings` | Generated metadata includes representative Gemini text, image, and embedding routes. |
| Google Vertex AI | `google-vertex` | `google-vertex`, `google-vertex-images`, `google-vertex-embeddings` | Generated metadata includes representative Gemini Vertex text, Imagen, and embedding routes; callers still supply project/location routing. |
| Mistral | `mistral` | `mistral-conversations` | Generated metadata includes Mistral Large text plus representative adjustable and native reasoning models. |
| Radius gateway | `radius` | `radius-messages` | Use [provider/radius](../provider/radius) and explicitly refresh gateway-owned models. `RADIUS_API_KEY` is the API-key fallback; no static catalog, OAuth, or durable catalog cache is provided. |
| OpenRouter | `openrouter` | `openai-completions`, `openrouter-images` | Generated metadata includes one text route and image routes for Gemini and Grok Imagine routed models. |
| OpenCode Zen, OpenCode Go | `opencode`, `opencode-go` | `openai-completions`, `openai-responses`, `anthropic-messages`, `google-generative-ai` | Use [provider/opencode](../provider/opencode); generated metadata selects the model-specific route. |
| xAI/Grok | `xai` | `openai-completions` | Use [provider/xai](../provider/xai) for Grok Chat Completions requests. Generated metadata includes curated Grok text, image-input, and reasoning-capable routes with `XAI_API_KEY` credential metadata. |
| GitHub Copilot | `github-copilot` | `openai-completions`, `openai-responses`, `anthropic-messages` | Use [provider/githubcopilot](../provider/githubcopilot). Generated metadata includes Copilot OpenAI-compatible and Anthropic-compatible routes with static Copilot headers and `COPILOT_GITHUB_TOKEN` credential metadata. |
| Cloudflare AI Gateway | `cloudflare-ai-gateway` | `openai-completions`, `openai-responses`, `anthropic-messages` | Use [provider/cloudflare](../provider/cloudflare) for AI Gateway routes. Generated metadata includes OpenAI-compatible and Anthropic-compatible passthrough rows with environment-backed account/gateway placeholders and `CLOUDFLARE_API_KEY` credential metadata. |
| DeepSeek, Groq, Cerebras, Together | `deepseek`, `groq`, `cerebras`, `together` | `openai-completions` | Use the provider-local wrappers for direct Chat Completions requests. Generated metadata includes representative routes backed by shared OpenAI-compatible fixture coverage. |
| Hugging Face Router | `huggingface` | `openai-completions` | Use [provider/huggingface](../provider/huggingface). Generated metadata includes focused router rows with `HF_TOKEN` credential metadata and shared OpenAI-compatible fixture coverage. |
| Fireworks | `fireworks`, `fireworks-anthropic` | `openai-completions`, `anthropic-messages` | Generated metadata includes the Fire Pass Kimi K2.6 Turbo router and Kimi K2.7 Code for the OpenAI-compatible endpoint, plus Kimi K2.6 and Kimi K2.7 Code for the Anthropic-compatible Messages endpoint. |
| Moonshot AI | `moonshotai`, `moonshotai-cn` | `openai-completions` | Use [provider/moonshot](../provider/moonshot). Generated metadata includes direct Kimi K2 rows with `MOONSHOT_API_KEY` credential metadata and K2.7 disabled-thinking compatibility metadata. |
| Kimi Coding | `kimi-coding` | `anthropic-messages` | Use [provider/kimi](../provider/kimi). Generated metadata includes Kimi Coding Anthropic-compatible routes with Kimi CLI headers and `KIMI_API_KEY` credential metadata. |
| Vercel AI Gateway | `vercel-ai-gateway` | `anthropic-messages` | Use [provider/vercel](../provider/vercel). Generated metadata includes curated gateway routes with `AI_GATEWAY_API_KEY` credential metadata and route-specific Anthropic compatibility metadata. |
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
| Vercel AI Gateway routing | `OpenAICompletionsCompat.VercelAIGatewayRouting` | OpenAI-compatible gateway routing metadata for caller-registered Chat Completions routes; built-in Vercel AI Gateway text routes use `anthropic-messages`. |
| Azure Responses configuration | `Model.AzureOpenAIResponses` | Azure endpoint, deployment, API version, and credential-source selection. |
| Codex Responses configuration | `Model.OpenAICodexResponses` | Codex model-name override and OAuth-token provider requirement. |
| Image shape limits | `ImageModel.MaxWidth`, `MaxHeight`, `SupportedSizes`, `SupportedFormats` | Image model discovery and request validation docs. |
| Embedding limits | `EmbeddingModel.DefaultDimensions`, `MinDimensions`, `MaxDimensions`, `MaxInputTokens`, `InputCostPerMillion`, `CostCurrency` | Embedding model discovery, routing metadata, and deterministic cost reporting. |

## Source capabilities not yet represented as complete Go parity

- OpenAI Images is generation-only; edits, variations, streaming partial images, and Responses image-tool generation are deferred.
- Google and Vertex image adapters are generation-only. Reference edits,
  variations, and live image validation remain future work.
- Google, Vertex, and Bedrock embeddings are fixture-backed provider adapters;
  live embedding probes and tokenizer-based input estimates remain future work.
- Automatic provider/model discovery is generated from curated metadata, not live provider listing calls.
- OAuth credential persistence is intentionally absent.
- Cross-provider context handoff and capability-loss reporting are future work.
- Source-level provider breadth is larger than generated default models. Several provider IDs still exist only for caller-registered compatible models today, and OpenCode coverage is limited to curated OpenAI-compatible routes.
- Codex WebSocket proxy-aware dialing and durable session caching remain
  deferred.
- Live-test coverage is opt-in and sparse. Standard tests must remain deterministic and credential-free.
