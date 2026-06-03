// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

//go:generate go run ./cmd/sigma-generate-models

// Model describes a provider model available through sigma.
type Model struct {
	ID                            ModelID                  `json:"id"`
	Provider                      ProviderID               `json:"provider"`
	API                           API                      `json:"api,omitempty"`
	Name                          string                   `json:"name,omitempty"`
	ContextWindow                 int                      `json:"contextWindow,omitempty"`
	MaxOutputTokens               int                      `json:"maxOutputTokens,omitempty"`
	SupportedInputs               []ContentBlockType       `json:"supportedInputs,omitempty"`
	SupportsTools                 bool                     `json:"supportsTools,omitempty"`
	SupportsThinking              bool                     `json:"supportsThinking,omitempty"`
	ThinkingLevels                []ThinkingLevel          `json:"thinkingLevels,omitempty"`
	ThinkingLevelMap              map[ThinkingLevel]string `json:"thinkingLevelMap,omitempty"`
	UnsupportedThinkingLevels     []ThinkingLevel          `json:"unsupportedThinkingLevels,omitempty"`
	InputCostPerMillion           float64                  `json:"inputCostPerMillion,omitempty"`
	OutputCostPerMillion          float64                  `json:"outputCostPerMillion,omitempty"`
	CacheReadInputCostPerMillion  float64                  `json:"cacheReadInputCostPerMillion,omitempty"`
	CacheWriteInputCostPerMillion float64                  `json:"cacheWriteInputCostPerMillion,omitempty"`
	CostCurrency                  string                   `json:"costCurrency,omitempty"`
	DefaultTransport              Transport                `json:"defaultTransport,omitempty"`
	// OpenAICompletionsCompat overrides provider/base-URL compatibility
	// detection for OpenAI Chat Completions-compatible custom models.
	OpenAICompletionsCompat *OpenAICompletionsCompat `json:"openAICompletionsCompat,omitempty"`
	// AnthropicMessagesCompat overrides provider/base-URL compatibility
	// detection for Anthropic Messages-compatible custom models.
	AnthropicMessagesCompat *AnthropicMessagesCompat `json:"anthropicMessagesCompat,omitempty"`
	// AzureOpenAIResponses configures Azure OpenAI Responses models. Leave nil
	// for non-Azure models.
	AzureOpenAIResponses *AzureOpenAIResponsesConfig `json:"azureOpenAIResponses,omitempty"`
	// OpenAICodexResponses configures OpenAI Codex Responses models. Leave nil
	// for non-Codex models.
	OpenAICodexResponses *OpenAICodexResponsesConfig `json:"openAICodexResponses,omitempty"`
	ProviderMetadata     map[string]any              `json:"providerMetadata,omitempty"`
}

const (
	// MetadataOpenAICompatible marks a model as caller-registered
	// OpenAI-compatible metadata that should be validated as runnable.
	MetadataOpenAICompatible = "openAICompatible"
	// MetadataOpenAICompatibleBaseURL stores the /v1-compatible endpoint for a
	// caller-registered OpenAI-compatible model.
	MetadataOpenAICompatibleBaseURL = "openAICompatibleBaseURL"
	// MetadataOpenAICompatibleHeaders stores model-scoped HTTP headers for a
	// caller-registered OpenAI-compatible model.
	MetadataOpenAICompatibleHeaders = "openAICompatibleHeaders"
)

// OpenAICompatibleModelConfig configures OpenAICompatibleModel.
type OpenAICompatibleModelConfig struct {
	ID                            ModelID
	Provider                      ProviderID
	BaseURL                       string
	Name                          string
	Headers                       map[string]string
	ContextWindow                 int
	MaxOutputTokens               int
	SupportedInputs               []ContentBlockType
	SupportsTools                 bool
	SupportsThinking              bool
	ThinkingLevels                []ThinkingLevel
	ThinkingLevelMap              map[ThinkingLevel]string
	UnsupportedThinkingLevels     []ThinkingLevel
	InputCostPerMillion           float64
	OutputCostPerMillion          float64
	CacheReadInputCostPerMillion  float64
	CacheWriteInputCostPerMillion float64
	CostCurrency                  string
	DefaultTransport              Transport
	OpenAICompletionsCompat       *OpenAICompletionsCompat
	ProviderMetadata              map[string]any
}

// OpenAICompatibleModel constructs metadata for an OpenAI Chat
// Completions-compatible model. Register the returned model on the same
// registry as an openai.Provider, then pass that registry to NewClient with
// WithRegistry for an isolated setup.
func OpenAICompatibleModel(config OpenAICompatibleModelConfig) Model {
	metadata := copyStringAnyMap(config.ProviderMetadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata[MetadataOpenAICompatible] = true
	if config.BaseURL != "" {
		metadata[MetadataOpenAICompatibleBaseURL] = config.BaseURL
	}
	if len(config.Headers) > 0 {
		metadata[MetadataOpenAICompatibleHeaders] = copyStringStringMap(config.Headers)
	}

	return Model{
		ID:                            config.ID,
		Provider:                      config.Provider,
		API:                           APIOpenAICompletions,
		Name:                          config.Name,
		ContextWindow:                 config.ContextWindow,
		MaxOutputTokens:               config.MaxOutputTokens,
		SupportedInputs:               append([]ContentBlockType(nil), config.SupportedInputs...),
		SupportsTools:                 config.SupportsTools,
		SupportsThinking:              config.SupportsThinking,
		ThinkingLevels:                append([]ThinkingLevel(nil), config.ThinkingLevels...),
		ThinkingLevelMap:              copyThinkingLevelStringMap(config.ThinkingLevelMap),
		UnsupportedThinkingLevels:     append([]ThinkingLevel(nil), config.UnsupportedThinkingLevels...),
		InputCostPerMillion:           config.InputCostPerMillion,
		OutputCostPerMillion:          config.OutputCostPerMillion,
		CacheReadInputCostPerMillion:  config.CacheReadInputCostPerMillion,
		CacheWriteInputCostPerMillion: config.CacheWriteInputCostPerMillion,
		CostCurrency:                  config.CostCurrency,
		DefaultTransport:              config.DefaultTransport,
		OpenAICompletionsCompat:       cloneOpenAICompletionsCompat(config.OpenAICompletionsCompat),
		ProviderMetadata:              metadata,
	}
}

// AzureOpenAIResponsesConfig carries Azure-specific model metadata for the
// Responses API. Endpoint is the Azure OpenAI resource endpoint, Deployment is
// the deployment name sent as the Responses model, APIVersion is the
// api-version query parameter, APIKeyEnvVar optionally overrides the default
// AZURE_OPENAI_API_KEY lookup, and CredentialSource may be "api-key" or
// "token" when callers need to document the intended auth path.
type AzureOpenAIResponsesConfig struct {
	Endpoint         string `json:"endpoint,omitempty"`
	Deployment       string `json:"deployment,omitempty"`
	APIVersion       string `json:"apiVersion,omitempty"`
	APIKeyEnvVar     string `json:"apiKeyEnvVar,omitempty"`
	CredentialSource string `json:"credentialSource,omitempty"`
}

// OpenAICodexResponsesConfig carries Codex-specific Responses metadata. Model
// is the model name sent to OpenAI when it differs from sigma's model ID.
type OpenAICodexResponsesConfig struct {
	Model string `json:"model,omitempty"`
}

// ImageModel describes a provider image model available through sigma.
type ImageModel struct {
	ID               ModelID        `json:"id"`
	Provider         ProviderID     `json:"provider"`
	API              ImageAPI       `json:"api,omitempty"`
	Name             string         `json:"name,omitempty"`
	MaxWidth         int            `json:"maxWidth,omitempty"`
	MaxHeight        int            `json:"maxHeight,omitempty"`
	SupportedSizes   []string       `json:"supportedSizes,omitempty"`
	SupportedFormats []string       `json:"supportedFormats,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
}

// EmbeddingModel describes a provider embedding model available through sigma.
type EmbeddingModel struct {
	ID                  ModelID        `json:"id"`
	Provider            ProviderID     `json:"provider"`
	API                 EmbeddingAPI   `json:"api,omitempty"`
	Name                string         `json:"name,omitempty"`
	DefaultDimensions   int            `json:"defaultDimensions,omitempty"`
	MaxInputTokens      int            `json:"maxInputTokens,omitempty"`
	InputCostPerMillion float64        `json:"inputCostPerMillion,omitempty"`
	CostCurrency        string         `json:"costCurrency,omitempty"`
	ProviderMetadata    map[string]any `json:"providerMetadata,omitempty"`
}

// ImageRequest is the provider-neutral input for image generation.
//
// This is separate from image inputs in chat/completion requests. Chat image
// input uses ContentBlock values built with ImageBase64 or ImageURL; image
// generation uses ImageInput values and returns AssistantImages.
type ImageRequest struct {
	Model            ModelID        `json:"model,omitempty"`
	Provider         ProviderID     `json:"provider,omitempty"`
	Operation        ImageOperation `json:"operation,omitempty"`
	Prompt           string         `json:"prompt,omitempty"`
	Inputs           []ImageInput   `json:"inputs,omitempty"`
	Mask             *ImageInput    `json:"mask,omitempty"`
	Size             string         `json:"size,omitempty"`
	Quality          string         `json:"quality,omitempty"`
	MIMEType         string         `json:"mimeType,omitempty"`
	Count            int            `json:"count,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
}

// ImageInput is an image or text input used by image APIs.
type ImageInput struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
	Source   string `json:"source,omitempty"`
	Data     string `json:"data,omitempty"`
	URL      string `json:"url,omitempty"`
}

// AssistantImages is provider-neutral image output plus generation metadata.
type AssistantImages struct {
	Images           []ImageInput   `json:"images,omitempty"`
	ResponseID       string         `json:"responseId,omitempty"`
	StopReason       StopReason     `json:"stopReason,omitempty"`
	Errors           []ImageError   `json:"errors,omitempty"`
	Usage            *Usage         `json:"usage,omitempty"`
	Cost             *Cost          `json:"cost,omitempty"`
	Model            ModelID        `json:"model,omitempty"`
	Provider         ProviderID     `json:"provider,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
}

// EmbeddingRequest is the provider-neutral input for vector embeddings.
type EmbeddingRequest struct {
	Inputs           []string       `json:"inputs,omitempty"`
	Dimensions       int            `json:"dimensions,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
}

// Embedding is one provider-neutral embedding vector.
type Embedding struct {
	Index  int       `json:"index"`
	Vector []float32 `json:"vector,omitempty"`
}

// Embeddings is provider-neutral embedding output plus request metadata.
type Embeddings struct {
	Vectors          []Embedding    `json:"vectors,omitempty"`
	Usage            *Usage         `json:"usage,omitempty"`
	Cost             *Cost          `json:"cost,omitempty"`
	Model            ModelID        `json:"model,omitempty"`
	Provider         ProviderID     `json:"provider,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
}

func cloneModel(model Model) Model {
	model.SupportedInputs = append([]ContentBlockType(nil), model.SupportedInputs...)
	model.ThinkingLevels = append([]ThinkingLevel(nil), model.ThinkingLevels...)
	model.ThinkingLevelMap = copyThinkingLevelStringMap(model.ThinkingLevelMap)
	model.UnsupportedThinkingLevels = append([]ThinkingLevel(nil), model.UnsupportedThinkingLevels...)
	model.OpenAICompletionsCompat = cloneOpenAICompletionsCompat(model.OpenAICompletionsCompat)
	model.AnthropicMessagesCompat = cloneAnthropicMessagesCompat(model.AnthropicMessagesCompat)
	model.AzureOpenAIResponses = cloneAzureOpenAIResponsesConfig(model.AzureOpenAIResponses)
	model.OpenAICodexResponses = cloneOpenAICodexResponsesConfig(model.OpenAICodexResponses)
	model.ProviderMetadata = copyStringAnyMap(model.ProviderMetadata)
	return model
}

func cloneImageModel(model ImageModel) ImageModel {
	model.SupportedSizes = append([]string(nil), model.SupportedSizes...)
	model.SupportedFormats = append([]string(nil), model.SupportedFormats...)
	model.ProviderMetadata = copyStringAnyMap(model.ProviderMetadata)
	return model
}

func cloneEmbeddingModel(model EmbeddingModel) EmbeddingModel {
	model.ProviderMetadata = copyStringAnyMap(model.ProviderMetadata)
	return model
}

func copyStringAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyStringStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyThinkingLevelStringMap(values map[ThinkingLevel]string) map[ThinkingLevel]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[ThinkingLevel]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func cloneOpenAICompletionsCompat(compat *OpenAICompletionsCompat) *OpenAICompletionsCompat {
	if compat == nil {
		return nil
	}
	copied := *compat
	copied.OpenRouterRouting = cloneOpenRouterRoutingPreference(compat.OpenRouterRouting)
	copied.VercelAIGatewayRouting = cloneVercelAIGatewayRoutingPreference(compat.VercelAIGatewayRouting)
	return &copied
}

func cloneAnthropicMessagesCompat(compat *AnthropicMessagesCompat) *AnthropicMessagesCompat {
	if compat == nil {
		return nil
	}
	copied := *compat
	return &copied
}

func cloneAzureOpenAIResponsesConfig(config *AzureOpenAIResponsesConfig) *AzureOpenAIResponsesConfig {
	if config == nil {
		return nil
	}
	copied := *config
	return &copied
}

func cloneOpenAICodexResponsesConfig(config *OpenAICodexResponsesConfig) *OpenAICodexResponsesConfig {
	if config == nil {
		return nil
	}
	copied := *config
	return &copied
}

func cloneOpenRouterRoutingPreference(routing *OpenRouterRoutingPreference) *OpenRouterRoutingPreference {
	if routing == nil {
		return nil
	}
	copied := *routing
	copied.Order = append([]string(nil), routing.Order...)
	copied.Only = append([]string(nil), routing.Only...)
	copied.Ignore = append([]string(nil), routing.Ignore...)
	copied.Quantizations = append([]string(nil), routing.Quantizations...)
	copied.AllowFallbacks = cloneBoolPtr(routing.AllowFallbacks)
	copied.RequireParameters = cloneBoolPtr(routing.RequireParameters)
	copied.ZDR = cloneBoolPtr(routing.ZDR)
	copied.EnforceDistillableText = cloneBoolPtr(routing.EnforceDistillableText)
	copied.MaxPrice = copyStringAnyMap(routing.MaxPrice)
	copied.PreferredMinThroughput = cloneAnyValue(routing.PreferredMinThroughput)
	copied.PreferredMaxLatency = cloneAnyValue(routing.PreferredMaxLatency)
	copied.Sort = cloneAnyValue(routing.Sort)
	return &copied
}

func cloneVercelAIGatewayRoutingPreference(routing *VercelAIGatewayRoutingPreference) *VercelAIGatewayRoutingPreference {
	if routing == nil {
		return nil
	}
	copied := *routing
	copied.Order = append([]string(nil), routing.Order...)
	copied.Only = append([]string(nil), routing.Only...)
	copied.Models = append([]string(nil), routing.Models...)
	copied.BYOK = copyStringAnyMap(routing.BYOK)
	return &copied
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return copyStringAnyMap(typed)
	case []any:
		return append([]any(nil), typed...)
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func modelMatchesAll(model Model, filters []ModelFilter) bool {
	for _, filter := range filters {
		if filter != nil && !filter(model) {
			return false
		}
	}
	return true
}
