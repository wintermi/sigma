// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

// ProviderID identifies a model provider.
type ProviderID string

// API identifies a chat or text generation provider API surface.
type API string

// ImageAPI identifies an image generation provider API surface.
type ImageAPI string

// EmbeddingAPI identifies a vector embedding provider API surface.
type EmbeddingAPI string

// ImageOperation identifies the requested provider-neutral image operation.
type ImageOperation string

// ModelID identifies a provider-specific model.
type ModelID string

// Role identifies the role of a persisted conversation message.
type Role string

// StopReason identifies why a model stopped generating output.
type StopReason string

// ThinkingLevel identifies a provider thinking or reasoning budget level.
type ThinkingLevel string

// CacheRetention identifies how long provider-side prompt cache entries may live.
type CacheRetention string

// Transport identifies the wire transport used for provider calls.
type Transport string

// ContentBlockType identifies the shape of a message content block.
type ContentBlockType string

// OpenAICompatSupport identifies whether an OpenAI-compatible feature is known
// to be supported by a provider or endpoint.
type OpenAICompatSupport string

// OpenAICompletionsMaxTokensField identifies the token-limit field name used by
// an OpenAI Chat Completions-compatible endpoint.
type OpenAICompletionsMaxTokensField string

// OpenAICompletionsReasoningFormat identifies how reasoning effort is encoded
// by an OpenAI Chat Completions-compatible endpoint.
type OpenAICompletionsReasoningFormat string

// OpenAICompletionsCacheControlFormat identifies how prompt cache markers are
// encoded by an OpenAI Chat Completions-compatible endpoint.
type OpenAICompletionsCacheControlFormat string

// AnthropicCompatSupport identifies whether an Anthropic Messages-compatible
// feature is known to be supported by a provider or endpoint.
type AnthropicCompatSupport string

// AnthropicThinkingFormat identifies how Anthropic Messages thinking is
// encoded by a provider or endpoint.
type AnthropicThinkingFormat string

const (
	// APIOpenAICompletions identifies the OpenAI chat completions API.
	APIOpenAICompletions API = "openai-completions"
	// APIOpenAIResponses identifies the OpenAI responses API.
	APIOpenAIResponses API = "openai-responses"
	// APIAzureOpenAIResponses identifies the Azure OpenAI responses API.
	APIAzureOpenAIResponses API = "azure-openai-responses"
	// APIOpenAICodexResponses identifies the OpenAI Codex responses API.
	APIOpenAICodexResponses API = "openai-codex-responses"
	// APIAnthropicMessages identifies the Anthropic messages API.
	APIAnthropicMessages API = "anthropic-messages"
	// APIBedrockConverseStream identifies the Amazon Bedrock converse stream API.
	APIBedrockConverseStream API = "bedrock-converse-stream"
	// APIGoogleGenerativeAI identifies the Google Generative AI API.
	APIGoogleGenerativeAI API = "google-generative-ai"
	// APIGoogleVertex identifies the Google Vertex AI API.
	APIGoogleVertex API = "google-vertex"
	// APIMistralConversations identifies the Mistral conversations API.
	APIMistralConversations API = "mistral-conversations"
)

const (
	// ProviderOpenAI identifies OpenAI.
	ProviderOpenAI ProviderID = "openai"
	// ProviderAzureOpenAIResponses identifies Azure OpenAI Responses.
	ProviderAzureOpenAIResponses ProviderID = "azure-openai-responses"
	// ProviderOpenAICodex identifies OpenAI Codex.
	ProviderOpenAICodex ProviderID = "openai-codex"
	// ProviderAnthropic identifies Anthropic.
	ProviderAnthropic ProviderID = "anthropic"
	// ProviderGoogle identifies Google Generative AI.
	ProviderGoogle ProviderID = "google"
	// ProviderGoogleVertex identifies Google Vertex AI.
	ProviderGoogleVertex ProviderID = "google-vertex"
	// ProviderGoogleVertexOpenAI identifies Vertex AI OpenAI-compatible MaaS.
	ProviderGoogleVertexOpenAI ProviderID = "google-vertex-openai"
	// ProviderGoogleVertexAnthropic identifies Anthropic Claude on Vertex AI.
	ProviderGoogleVertexAnthropic ProviderID = "google-vertex-anthropic"
	// ProviderMistral identifies Mistral AI.
	ProviderMistral ProviderID = "mistral"
	// ProviderAmazonBedrock identifies Amazon Bedrock.
	ProviderAmazonBedrock ProviderID = "amazon-bedrock"
	// ProviderOpenRouter identifies OpenRouter.
	ProviderOpenRouter ProviderID = "openrouter"
	// ProviderDeepSeek identifies DeepSeek.
	ProviderDeepSeek ProviderID = "deepseek"
	// ProviderGroq identifies Groq.
	ProviderGroq ProviderID = "groq"
	// ProviderCerebras identifies Cerebras.
	ProviderCerebras ProviderID = "cerebras"
	// ProviderXAI identifies xAI.
	ProviderXAI ProviderID = "xai"
	// ProviderTogether identifies Together AI.
	ProviderTogether ProviderID = "together"
	// ProviderCloudflareAIGateway identifies Cloudflare AI Gateway.
	ProviderCloudflareAIGateway ProviderID = "cloudflare-ai-gateway"
	// ProviderCloudflareWorkersAI identifies Cloudflare Workers AI.
	ProviderCloudflareWorkersAI ProviderID = "cloudflare-workers-ai"
	// ProviderGitHubCopilot identifies GitHub Copilot.
	ProviderGitHubCopilot ProviderID = "github-copilot"
	// ProviderNVIDIA identifies NVIDIA NIM.
	ProviderNVIDIA ProviderID = "nvidia"
	// ProviderZAI identifies Z.ai.
	ProviderZAI ProviderID = "zai"
	// ProviderZAICodingCN identifies Z.ai Coding CN.
	ProviderZAICodingCN ProviderID = "zai-coding-cn"
	// ProviderAntLing identifies Ant Ling.
	ProviderAntLing ProviderID = "ant-ling"
	// ProviderMoonshotAI identifies Moonshot AI.
	ProviderMoonshotAI ProviderID = "moonshotai"
	// ProviderMoonshotAICN identifies Moonshot AI CN.
	ProviderMoonshotAICN ProviderID = "moonshotai-cn"
	// ProviderMiniMax identifies MiniMax.
	ProviderMiniMax ProviderID = "minimax"
	// ProviderMiniMaxCN identifies MiniMax CN.
	ProviderMiniMaxCN ProviderID = "minimax-cn"
	// ProviderVercelAIGateway identifies Vercel AI Gateway.
	ProviderVercelAIGateway ProviderID = "vercel-ai-gateway"
	// ProviderOpenCode identifies OpenCode Zen.
	ProviderOpenCode ProviderID = "opencode"
	// ProviderOpenCodeGo identifies OpenCode Go.
	ProviderOpenCodeGo ProviderID = "opencode-go"
	// ProviderFireworks identifies Fireworks AI.
	ProviderFireworks ProviderID = "fireworks"
	// ProviderFireworksAnthropic identifies Fireworks AI's Anthropic-compatible
	// Messages endpoint.
	ProviderFireworksAnthropic ProviderID = "fireworks-anthropic"
	// ProviderKimi identifies Kimi.
	ProviderKimi ProviderID = "kimi"
	// ProviderXiaomi identifies Xiaomi.
	ProviderXiaomi ProviderID = "xiaomi"
	// ProviderCustom identifies a user-defined provider path.
	ProviderCustom ProviderID = "custom"
)

const (
	// RoleUser identifies a user message.
	RoleUser Role = "user"
	// RoleDeveloper identifies provider developer instructions persisted as messages.
	RoleDeveloper Role = "developer"
	// RoleAssistant identifies an assistant message.
	RoleAssistant Role = "assistant"
	// RoleTool identifies a tool-result message.
	RoleTool Role = "tool"
)

const (
	// StopReasonEndTurn indicates the assistant completed a normal turn.
	StopReasonEndTurn StopReason = "end-turn"
	// StopReasonMaxTokens indicates generation stopped at the output token limit.
	StopReasonMaxTokens StopReason = "max-tokens"
	// StopReasonStopSequence indicates generation stopped after a configured stop sequence.
	StopReasonStopSequence StopReason = "stop-sequence"
	// StopReasonToolCalls indicates generation stopped to request tool calls.
	StopReasonToolCalls StopReason = "tool-calls"
	// StopReasonContentFilter indicates generation stopped because content was filtered.
	StopReasonContentFilter StopReason = "content-filter"
	// StopReasonError indicates generation stopped because the provider returned an error.
	StopReasonError StopReason = "error"
	// StopReasonUnknown indicates the provider did not expose a stable stop reason.
	StopReasonUnknown StopReason = "unknown"
)

const (
	// ThinkingLevelOff disables provider reasoning or thinking features.
	ThinkingLevelOff ThinkingLevel = "off"
	// ThinkingLevelMinimal requests the smallest provider reasoning or thinking budget.
	ThinkingLevelMinimal ThinkingLevel = "minimal"
	// ThinkingLevelLow requests a low reasoning or thinking budget.
	ThinkingLevelLow ThinkingLevel = "low"
	// ThinkingLevelMedium requests a medium reasoning or thinking budget.
	ThinkingLevelMedium ThinkingLevel = "medium"
	// ThinkingLevelHigh requests a high reasoning or thinking budget.
	ThinkingLevelHigh ThinkingLevel = "high"
	// ThinkingLevelXHigh requests the largest provider reasoning or thinking budget.
	ThinkingLevelXHigh ThinkingLevel = "xhigh"
)

const (
	// CacheRetentionNone disables provider-side prompt caching.
	CacheRetentionNone CacheRetention = "none"
	// CacheRetentionShort identifies provider cache entries kept briefly.
	CacheRetentionShort CacheRetention = "short"
	// CacheRetentionLong identifies provider cache entries kept beyond a single request.
	CacheRetentionLong CacheRetention = "long"
	// CacheRetentionEphemeral identifies provider cache entries kept briefly.
	CacheRetentionEphemeral CacheRetention = "ephemeral"
	// CacheRetentionPersistent identifies provider cache entries kept beyond a single request.
	CacheRetentionPersistent CacheRetention = "persistent"
)

const (
	// TransportHTTP identifies ordinary HTTP request/response transport.
	TransportHTTP Transport = "http"
	// TransportSSE identifies server-sent event streaming transport.
	TransportSSE Transport = "sse"
	// TransportWebSocket identifies WebSocket streaming transport.
	TransportWebSocket Transport = "websocket"
)

const (
	// ContentBlockText identifies a text content block.
	ContentBlockText ContentBlockType = "text"
	// ContentBlockThinking identifies a thinking content block.
	ContentBlockThinking ContentBlockType = "thinking"
	// ContentBlockImage identifies an image content block.
	ContentBlockImage ContentBlockType = "image"
	// ContentBlockToolCall identifies a tool-call content block.
	ContentBlockToolCall ContentBlockType = "tool-call"
)

const (
	// OpenAICompatDefault uses provider and endpoint defaults.
	OpenAICompatDefault OpenAICompatSupport = ""
	// OpenAICompatSupported forces a compatibility feature on.
	OpenAICompatSupported OpenAICompatSupport = "supported"
	// OpenAICompatUnsupported forces a compatibility feature off.
	OpenAICompatUnsupported OpenAICompatSupport = "unsupported"
)

const (
	// OpenAICompletionsMaxTokensDefault uses provider and endpoint defaults.
	OpenAICompletionsMaxTokensDefault OpenAICompletionsMaxTokensField = ""
	// OpenAICompletionsMaxTokens sends max_tokens.
	OpenAICompletionsMaxTokens OpenAICompletionsMaxTokensField = "max_tokens"
	// OpenAICompletionsMaxCompletionTokens sends max_completion_tokens.
	OpenAICompletionsMaxCompletionTokens OpenAICompletionsMaxTokensField = "max_completion_tokens"
)

const (
	// OpenAICompletionsReasoningDefault uses provider and endpoint defaults.
	OpenAICompletionsReasoningDefault OpenAICompletionsReasoningFormat = ""
	// OpenAICompletionsReasoningUnsupported suppresses reasoning fields.
	OpenAICompletionsReasoningUnsupported OpenAICompletionsReasoningFormat = "unsupported"
	// OpenAICompletionsReasoningEffort sends reasoning_effort.
	OpenAICompletionsReasoningEffort OpenAICompletionsReasoningFormat = "reasoning_effort"
	// OpenAICompletionsReasoningObject sends a reasoning object.
	OpenAICompletionsReasoningObject OpenAICompletionsReasoningFormat = "reasoning"
	// OpenAICompletionsReasoningFireworks sends reasoning_effort for levels
	// and the Fireworks thinking object for explicit token budgets.
	OpenAICompletionsReasoningFireworks OpenAICompletionsReasoningFormat = "fireworks"
	// OpenAICompletionsReasoningDeepSeek sends a thinking object plus
	// reasoning_effort when reasoning is enabled.
	OpenAICompletionsReasoningDeepSeek OpenAICompletionsReasoningFormat = "deepseek"
	// OpenAICompletionsReasoningStringThinking sends a top-level thinking
	// string such as "none" or a provider-specific level.
	OpenAICompletionsReasoningStringThinking OpenAICompletionsReasoningFormat = "string-thinking"
	// OpenAICompletionsReasoningTogether sends Together's reasoning toggle
	// plus optional reasoning_effort.
	OpenAICompletionsReasoningTogether OpenAICompletionsReasoningFormat = "together"
	// OpenAICompletionsReasoningQwen sends a top-level Qwen enable_thinking flag.
	OpenAICompletionsReasoningQwen OpenAICompletionsReasoningFormat = "qwen"
	// OpenAICompletionsReasoningZAI sends a Z.ai thinking object with an
	// enabled or disabled type.
	OpenAICompletionsReasoningZAI OpenAICompletionsReasoningFormat = "zai"
	// OpenAICompletionsReasoningAntLing sends Ant Ling's reasoning object only
	// for explicitly supported effort levels.
	OpenAICompletionsReasoningAntLing OpenAICompletionsReasoningFormat = "ant-ling"
)

const (
	// OpenAICompletionsCacheControlDefault uses provider and endpoint defaults.
	OpenAICompletionsCacheControlDefault OpenAICompletionsCacheControlFormat = ""
	// OpenAICompletionsCacheControlUnsupported suppresses cache-control fields.
	OpenAICompletionsCacheControlUnsupported OpenAICompletionsCacheControlFormat = "unsupported"
	// OpenAICompletionsCacheControlMessage sends cache_control beside message content.
	OpenAICompletionsCacheControlMessage OpenAICompletionsCacheControlFormat = "message"
	// OpenAICompletionsCacheControlContentPart sends cache_control on content parts.
	OpenAICompletionsCacheControlContentPart OpenAICompletionsCacheControlFormat = "content-part"
	// OpenAICompletionsCacheControlAnthropic sends Anthropic-style cache markers
	// on the instruction message, last tool, and last conversation message.
	OpenAICompletionsCacheControlAnthropic OpenAICompletionsCacheControlFormat = "anthropic"
)

const (
	// AnthropicCompatDefault uses provider and endpoint defaults.
	AnthropicCompatDefault AnthropicCompatSupport = ""
	// AnthropicCompatSupported forces a compatibility feature on.
	AnthropicCompatSupported AnthropicCompatSupport = "supported"
	// AnthropicCompatUnsupported forces a compatibility feature off.
	AnthropicCompatUnsupported AnthropicCompatSupport = "unsupported"
)

const (
	// AnthropicThinkingDefault uses provider and endpoint defaults.
	AnthropicThinkingDefault AnthropicThinkingFormat = ""
	// AnthropicThinkingBudget sends budget-token thinking controls.
	AnthropicThinkingBudget AnthropicThinkingFormat = "budget"
	// AnthropicThinkingAdaptive sends adaptive thinking plus output_config effort.
	AnthropicThinkingAdaptive AnthropicThinkingFormat = "adaptive"
)

// OpenAICompletionsCompat describes Chat Completions compatibility differences
// for routers and local OpenAI-compatible endpoints. Leave fields at their zero
// value to use provider or base-URL detection, or set them when registering a
// custom model to override conservative defaults.
type OpenAICompletionsCompat struct {
	SupportsStore                               OpenAICompatSupport                 `json:"supportsStore,omitempty"`
	SupportsDeveloperRole                       OpenAICompatSupport                 `json:"supportsDeveloperRole,omitempty"`
	ReasoningFormat                             OpenAICompletionsReasoningFormat    `json:"reasoningFormat,omitempty"`
	SupportsReasoningEffort                     OpenAICompatSupport                 `json:"supportsReasoningEffort,omitempty"`
	SupportsStreamingUsage                      OpenAICompatSupport                 `json:"supportsStreamingUsage,omitempty"`
	SupportsStrictTools                         OpenAICompatSupport                 `json:"supportsStrictTools,omitempty"`
	SupportsRequiredToolChoice                  OpenAICompatSupport                 `json:"supportsRequiredToolChoice,omitempty"`
	SupportsToolStream                          OpenAICompatSupport                 `json:"supportsToolStream,omitempty"`
	SupportsJSONSchemaResponseFormat            OpenAICompatSupport                 `json:"supportsJSONSchemaResponseFormat,omitempty"`
	MaxTokensField                              OpenAICompletionsMaxTokensField     `json:"maxTokensField,omitempty"`
	CacheControlFormat                          OpenAICompletionsCacheControlFormat `json:"cacheControlFormat,omitempty"`
	SupportsSessionAffinity                     OpenAICompatSupport                 `json:"supportsSessionAffinity,omitempty"`
	RequiresToolResultName                      OpenAICompatSupport                 `json:"requiresToolResultName,omitempty"`
	RequiresAssistantAfterToolResult            OpenAICompatSupport                 `json:"requiresAssistantAfterToolResult,omitempty"`
	RequiresToolsForToolHistory                 OpenAICompatSupport                 `json:"requiresToolsForToolHistory,omitempty"`
	RequiresReasoningContentOnAssistantMessages OpenAICompatSupport                 `json:"requiresReasoningContentOnAssistantMessages,omitempty"`
	OpenRouterRouting                           *OpenRouterRoutingPreference        `json:"openRouterRouting,omitempty"`
	VercelAIGatewayRouting                      *VercelAIGatewayRoutingPreference   `json:"vercelAIGatewayRouting,omitempty"`
}

// AnthropicMessagesCompat describes Messages compatibility differences for
// Anthropic-compatible routers and custom endpoints. Leave fields at their zero
// value to use provider or base-URL detection.
type AnthropicMessagesCompat struct {
	SupportsEagerToolInputStreaming AnthropicCompatSupport  `json:"supportsEagerToolInputStreaming,omitempty"`
	SupportsLongCacheRetention      AnthropicCompatSupport  `json:"supportsLongCacheRetention,omitempty"`
	SupportsSessionAffinity         AnthropicCompatSupport  `json:"supportsSessionAffinity,omitempty"`
	SupportsCacheControlOnTools     AnthropicCompatSupport  `json:"supportsCacheControlOnTools,omitempty"`
	SupportsEmptyThinkingSignature  AnthropicCompatSupport  `json:"supportsEmptyThinkingSignature,omitempty"`
	SupportsTemperature             AnthropicCompatSupport  `json:"supportsTemperature,omitempty"`
	SupportsDisabledThinking        AnthropicCompatSupport  `json:"supportsDisabledThinking,omitempty"`
	ThinkingFormat                  AnthropicThinkingFormat `json:"thinkingFormat,omitempty"`
}

// OpenRouterRoutingPreference describes OpenRouter's provider routing request
// body.
type OpenRouterRoutingPreference struct {
	Order                  []string       `json:"order,omitempty"`
	Only                   []string       `json:"only,omitempty"`
	Ignore                 []string       `json:"ignore,omitempty"`
	AllowFallbacks         *bool          `json:"allow_fallbacks,omitempty"`
	RequireParameters      *bool          `json:"require_parameters,omitempty"`
	DataCollection         string         `json:"data_collection,omitempty"`
	ZDR                    *bool          `json:"zdr,omitempty"`
	EnforceDistillableText *bool          `json:"enforce_distillable_text,omitempty"`
	Quantizations          []string       `json:"quantizations,omitempty"`
	MaxPrice               map[string]any `json:"max_price,omitempty"`
	PreferredMinThroughput any            `json:"preferred_min_throughput,omitempty"`
	PreferredMaxLatency    any            `json:"preferred_max_latency,omitempty"`
	Sort                   any            `json:"sort,omitempty"`
}

// VercelAIGatewayRoutingPreference describes Vercel AI Gateway routing values
// accepted under providerOptions.gateway.
type VercelAIGatewayRoutingPreference struct {
	Order   []string       `json:"order,omitempty"`
	Only    []string       `json:"only,omitempty"`
	Models  []string       `json:"models,omitempty"`
	Caching string         `json:"caching,omitempty"`
	BYOK    map[string]any `json:"byok,omitempty"`
}

// ModelRef identifies a provider-specific model.
type ModelRef struct {
	Provider ProviderID `json:"provider"`
	ID       ModelID    `json:"id"`
}

// Request is the provider-neutral input for a model turn.
type Request struct {
	SystemPrompt string    `json:"systemPrompt,omitempty"`
	Messages     []Message `json:"messages,omitempty"`
	Tools        []Tool    `json:"tools,omitempty"`
}

// Message is a conversation message discriminated by Role.
//
// User, developer, and assistant messages use Content. Tool-result messages use
// Content plus ToolCallID and IsError. Provider, API, Model, and StopReason
// preserve assistant provenance for later cross-provider replay. Go cannot make
// those role-specific fields impossible to combine in a plain struct, so callers
// should prefer UserText, UserContent, ToolResult, and ToolError when constructing
// persisted conversations.
type Message struct {
	Role       Role           `json:"role"`
	Content    []ContentBlock `json:"content,omitempty"`
	ToolCallID string         `json:"toolCallID,omitempty"`
	ToolName   string         `json:"toolName,omitempty"`
	IsError    bool           `json:"isError,omitempty"`
	Provider   ProviderID     `json:"provider,omitempty"`
	API        API            `json:"api,omitempty"`
	Model      ModelID        `json:"model,omitempty"`
	StopReason StopReason     `json:"stopReason,omitempty"`
}

// ContentBlock is a discriminated unit of message content.
//
// Text blocks use Text. Thinking blocks use ThinkingText plus optional
// Signature, Redacted, and ProviderSignature. Image blocks use MIMEType,
// ImageSource, Data, and URL. Tool-call blocks use ToolCallID, ToolName, and
// ToolArguments. ProviderMetadata carries opaque provider fields for later
// replay without requiring provider-specific conversion in this package.
type ContentBlock struct {
	Type              ContentBlockType `json:"type"`
	Text              string           `json:"text,omitempty"`
	ThinkingText      string           `json:"thinking,omitempty"`
	Signature         string           `json:"signature,omitempty"`
	Redacted          bool             `json:"redacted,omitempty"`
	MIMEType          string           `json:"mimeType,omitempty"`
	ImageSource       string           `json:"imageSource,omitempty"`
	Data              string           `json:"data,omitempty"`
	URL               string           `json:"url,omitempty"`
	ToolCallID        string           `json:"toolCallID,omitempty"`
	ToolName          string           `json:"toolName,omitempty"`
	ToolArguments     any              `json:"toolArguments,omitempty"`
	ProviderSignature string           `json:"providerSignature,omitempty"`
	ProviderMetadata  map[string]any   `json:"providerMetadata,omitempty"`
}

// Usage records provider token accounting for a model turn.
type Usage struct {
	InputTokens               int `json:"inputTokens,omitempty"`
	OutputTokens              int `json:"outputTokens,omitempty"`
	TotalTokens               int `json:"totalTokens,omitempty"`
	CacheReadInputTokens      int `json:"cacheReadInputTokens,omitempty"`
	CacheWriteInputTokens     int `json:"cacheWriteInputTokens,omitempty"`
	LongCacheWriteInputTokens int `json:"longCacheWriteInputTokens,omitempty"`
	ThinkingTokens            int `json:"thinkingTokens,omitempty"`
}

// Cost records provider cost accounting for a model turn.
type Cost struct {
	InputCost           float64 `json:"inputCost,omitempty"`
	OutputCost          float64 `json:"outputCost,omitempty"`
	CacheReadInputCost  float64 `json:"cacheReadInputCost,omitempty"`
	CacheWriteInputCost float64 `json:"cacheWriteInputCost,omitempty"`
	TotalCost           float64 `json:"totalCost,omitempty"`
	Currency            string  `json:"currency,omitempty"`
}

// AssistantMessage is provider-neutral assistant output plus turn metadata.
type AssistantMessage struct {
	Content          []ContentBlock `json:"content,omitempty"`
	StopReason       StopReason     `json:"stopReason,omitempty"`
	Usage            *Usage         `json:"usage,omitempty"`
	Cost             *Cost          `json:"cost,omitempty"`
	Model            ModelID        `json:"model,omitempty"`
	Provider         ProviderID     `json:"provider,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
	Diagnostics      []Diagnostic   `json:"diagnostics,omitempty"`
}

// UserText constructs a user message with a single text block.
func UserText(text string) Message {
	return UserContent(Text(text))
}

// UserContent constructs a user message from content blocks.
func UserContent(blocks ...ContentBlock) Message {
	return Message{
		Role:    RoleUser,
		Content: blocks,
	}
}

// ToolResult constructs a successful tool-result message.
func ToolResult(toolCallID string, text string) Message {
	return Message{
		Role:       RoleTool,
		Content:    []ContentBlock{Text(text)},
		ToolCallID: toolCallID,
	}
}

// ToolError constructs a failed tool-result message.
func ToolError(toolCallID string, text string) Message {
	msg := ToolResult(toolCallID, text)
	msg.IsError = true
	return msg
}

// Text constructs a text content block.
func Text(text string) ContentBlock {
	return ContentBlock{
		Type: ContentBlockText,
		Text: text,
	}
}

// Thinking constructs a thinking content block.
func Thinking(text string, signature string) ContentBlock {
	return ContentBlock{
		Type:         ContentBlockThinking,
		ThinkingText: text,
		Signature:    signature,
	}
}

// ImageBase64 constructs an image content block backed by base64 data.
func ImageBase64(mimeType string, data string) ContentBlock {
	return ContentBlock{
		Type:        ContentBlockImage,
		MIMEType:    mimeType,
		ImageSource: "base64",
		Data:        data,
	}
}

// ImageURL constructs an image content block backed by a URL.
func ImageURL(mimeType string, url string) ContentBlock {
	return ContentBlock{
		Type:        ContentBlockImage,
		MIMEType:    mimeType,
		ImageSource: "url",
		URL:         url,
	}
}

// ToolCallBlock constructs an assistant tool-call content block.
func ToolCallBlock(id string, name string, arguments any) ContentBlock {
	return ContentBlock{
		Type:          ContentBlockToolCall,
		ToolCallID:    id,
		ToolName:      name,
		ToolArguments: arguments,
	}
}
