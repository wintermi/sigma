// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"fmt"
	"maps"
	"net/http"
)

// ClientOption configures a Client.
type ClientOption func(*Client)

// Client coordinates model lookup and generation requests.
type Client struct {
	registry             *Registry
	authResolver         AuthResolver
	credentialStore      CredentialStore
	providerAuthResolver bool
	httpClient           *http.Client
	defaultHeaders       map[string]string
	defaultOptions       Options
}

// NewClient constructs a Client.
func NewClient(opts ...ClientOption) *Client {
	client := &Client{}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	if client.registry == nil {
		client.registry = DefaultRegistry()
	}
	return client
}

// New constructs a Client.
func New(opts ...ClientOption) *Client {
	return NewClient(opts...)
}

// WithRegistry configures the client to use a registry.
func WithRegistry(registry *Registry) ClientOption {
	return func(client *Client) {
		client.registry = registry
	}
}

// WithHTTPClient configures the HTTP client exposed to providers.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(client *Client) {
		client.httpClient = httpClient
	}
}

// WithAuthResolver configures the credential resolver exposed to providers.
func WithAuthResolver(resolver AuthResolver) ClientOption {
	return func(client *Client) {
		client.authResolver = resolver
	}
}

// WithCredentialStore configures stored credentials for opt-in provider auth.
//
// The store is inert unless WithStoredProviderAuth is also configured.
func WithCredentialStore(store CredentialStore) ClientOption {
	return func(client *Client) {
		client.credentialStore = store
	}
}

// WithStoredProviderAuth enables store-backed provider auth resolution.
func WithStoredProviderAuth() ClientOption {
	return func(client *Client) {
		client.providerAuthResolver = true
	}
}

// WithDefaultHeader configures a default request header.
func WithDefaultHeader(key, value string) ClientOption {
	return WithDefaultHeaders(map[string]string{key: value})
}

// WithDefaultHeaders configures default request headers.
func WithDefaultHeaders(headers map[string]string) ClientOption {
	return func(client *Client) {
		if len(headers) == 0 {
			return
		}
		if client.defaultHeaders == nil {
			client.defaultHeaders = make(map[string]string, len(headers))
		}
		for key, value := range headers {
			client.defaultHeaders[key] = value
		}
	}
}

// WithDefaultOptions configures default provider request options.
func WithDefaultOptions(opts ...Option) ClientOption {
	return func(client *Client) {
		client.defaultOptions = applyOptions(client.defaultOptions, opts)
		client.defaultOptions.APIKey = ""
	}
}

// Registry returns the client's registry.
func (c *Client) Registry() *Registry {
	if c == nil || c.registry == nil {
		return nil
	}
	return c.registry
}

// GetModel returns a text model by provider and model id.
func (c *Client) GetModel(provider ProviderID, id ModelID) (Model, bool) {
	if c == nil || c.registry == nil {
		return Model{}, false
	}
	return c.registry.Model(provider, id)
}

// Models returns text models matching all filters.
func (c *Client) Models(filters ...ModelFilter) []Model {
	if c == nil || c.registry == nil {
		return nil
	}
	models := c.registry.ListModels()
	if len(filters) == 0 {
		return models
	}

	filtered := models[:0]
	for _, model := range models {
		if modelMatchesAll(model, filters) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

// RefreshTextModels refreshes runtime text model sources on the client's registry.
func (c *Client) RefreshTextModels(ctx context.Context, providers ...ProviderID) error {
	if c == nil {
		c = NewClient()
	}
	if c.registry == nil {
		return registryError("registry is required")
	}
	return c.registry.RefreshTextModels(ctx, providers...)
}

// Stream starts a provider stream for model.
func (c *Client) Stream(ctx context.Context, model Model, req Request, opts ...Option) *Stream {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		c = NewClient()
	}
	if err := ValidateModelRef(ModelRef{Provider: model.Provider, ID: model.ID}); err != nil {
		return errorStream(ctx, err, AssistantMessage{
			Model:    model.ID,
			Provider: model.Provider,
		})
	}

	registered, ok := c.GetModel(model.Provider, model.ID)
	if !ok {
		return errorStream(ctx, modelNotFoundError(model.Provider, model.ID), AssistantMessage{
			Model:    model.ID,
			Provider: model.Provider,
		})
	}
	if model.API == "" {
		model = registered
	}

	provider, ok := c.registry.TextProvider(model.Provider)
	if !ok {
		return errorStream(ctx, providerNotFoundError(model.Provider, model.ID), AssistantMessage{
			Model:    model.ID,
			Provider: model.Provider,
		})
	}

	options := applyProviderNeutralControls(model, c.requestOptions(model, opts))
	if err := validateOptions(model, options); err != nil {
		return errorStream(ctx, err, AssistantMessage{
			Model:    model.ID,
			Provider: model.Provider,
		})
	}

	stream := provider.Stream(ctx, model, req, options)
	if stream == nil {
		return errorStream(ctx, &Error{
			Code:     ErrorUnsupported,
			Message:  "text provider returned nil stream",
			Provider: model.Provider,
			Model:    model.ID,
		}, AssistantMessage{
			Model:    model.ID,
			Provider: model.Provider,
		})
	}
	return stream
}

// Complete collects a provider stream into a final assistant message.
func (c *Client) Complete(ctx context.Context, model Model, req Request, opts ...Option) (AssistantMessage, error) {
	return Collect(ctx, c.Stream(ctx, model, req, opts...))
}

// CompleteText is a text-only helper for simple prompt/response workflows.
//
// It returns an error if the final assistant message contains non-text content,
// so tool calls and thinking blocks are not silently discarded.
func (c *Client) CompleteText(ctx context.Context, model Model, prompt string, opts ...Option) (string, error) {
	final, err := c.Complete(ctx, model, Request{
		Messages: []Message{UserText(prompt)},
	}, opts...)
	if err != nil {
		return "", err
	}
	return assistantText(final)
}

func (c *Client) requestOptions(model Model, opts []Option) Options {
	options := Options{
		HTTPClient: c.httpClient,
		Headers:    copyStringStringMap(c.defaultHeaders),
	}
	options = mergeOptions(options, c.defaultOptions)
	options = mergeOptions(options, modelDefaultOptions(model))
	defaultCallbacks := options.ProviderAuthResolvers
	options.ProviderAuthResolvers = nil
	options = applyOptions(options, opts)
	requestCallbacks := options.ProviderAuthResolvers
	options.ProviderAuthResolvers = mergeProviderAuthResolvers(defaultCallbacks, requestCallbacks)
	clientResolver := c.clientAuthResolver()
	options.AuthResolver = ChainAuthResolver{
		Client:                   clientResolver,
		ProviderCallbacks:        requestCallbacks,
		DefaultProviderCallbacks: defaultCallbacks,
	}
	return options
}

func (c *Client) clientAuthResolver() AuthResolver {
	if c == nil {
		return nil
	}
	if !c.providerAuthResolver {
		return c.authResolver
	}
	return StoredCredentialAuthResolver{
		Store:    c.credentialStore,
		Registry: c.registry,
		Fallback: c.authResolver,
	}
}

func defaultClient() *Client {
	return NewClient()
}

// GetModel returns a text model from the default registry.
func GetModel(provider ProviderID, id ModelID) (Model, bool) {
	return defaultClient().GetModel(provider, id)
}

// Models returns text models from the default registry matching all filters.
func Models(filters ...ModelFilter) []Model {
	return defaultClient().Models(filters...)
}

// StreamModel starts a provider stream using the default registry.
func StreamModel(ctx context.Context, model Model, req Request, opts ...Option) *Stream {
	return defaultClient().Stream(ctx, model, req, opts...)
}

// Complete collects a provider stream using the default registry.
func Complete(ctx context.Context, model Model, req Request, opts ...Option) (AssistantMessage, error) {
	return defaultClient().Complete(ctx, model, req, opts...)
}

// CompleteText is a text-only helper using the default registry.
//
// It returns an error if the final assistant message contains non-text content,
// so tool calls and thinking blocks are not silently discarded.
func CompleteText(ctx context.Context, model Model, prompt string, opts ...Option) (string, error) {
	return defaultClient().CompleteText(ctx, model, prompt, opts...)
}

func mergeOptions(base Options, override Options) Options {
	merged := cloneOptions(base)
	if override.Temperature != nil {
		merged.Temperature = cloneFloat64Ptr(override.Temperature)
	}
	if override.MaxTokens != nil {
		merged.MaxTokens = cloneIntPtr(override.MaxTokens)
	}
	if override.APIKey != "" {
		merged.APIKey = override.APIKey
	}
	if override.HTTPClient != nil {
		merged.HTTPClient = override.HTTPClient
	}
	if override.AuthResolver != nil {
		merged.AuthResolver = override.AuthResolver
	}
	if override.Transport != "" {
		merged.Transport = override.Transport
	}
	if override.CacheRetention != "" {
		merged.CacheRetention = override.CacheRetention
	}
	if override.SessionID != "" {
		merged.SessionID = override.SessionID
	}
	if len(override.Headers) > 0 {
		if merged.Headers == nil {
			merged.Headers = make(map[string]string, len(override.Headers))
		}
		for key, value := range override.Headers {
			merged.Headers[key] = value
		}
	}
	if len(override.SuppressedHeaders) > 0 {
		merged.SuppressedHeaders = append(merged.SuppressedHeaders, override.SuppressedHeaders...)
	}
	if override.Timeout != nil {
		merged.Timeout = cloneDurationPtr(override.Timeout)
	}
	if override.MaxRetries != nil {
		merged.MaxRetries = cloneIntPtr(override.MaxRetries)
	}
	if override.MaxRetryDelay != nil {
		merged.MaxRetryDelay = cloneDurationPtr(override.MaxRetryDelay)
	}
	if len(override.Metadata) > 0 {
		if merged.Metadata == nil {
			merged.Metadata = make(map[string]any, len(override.Metadata))
		}
		for key, value := range override.Metadata {
			merged.Metadata[key] = value
		}
	}
	if override.ReasoningLevel != "" {
		merged.ReasoningLevel = override.ReasoningLevel
	}
	if override.ThinkingBudgetTokens != nil {
		merged.ThinkingBudgetTokens = cloneIntPtr(override.ThinkingBudgetTokens)
	}
	if override.StructuredOutput != nil {
		merged.StructuredOutput = cloneStructuredOutput(override.StructuredOutput)
	}
	if override.TopLogprobs != 0 {
		merged.TopLogprobs = override.TopLogprobs
	}
	if len(override.ProviderOptions) > 0 {
		if merged.ProviderOptions == nil {
			merged.ProviderOptions = make(map[ProviderID]map[string]any, len(override.ProviderOptions))
		}
		for provider, values := range override.ProviderOptions {
			if merged.ProviderOptions[provider] == nil {
				merged.ProviderOptions[provider] = make(map[string]any, len(values))
			}
			for key, value := range values {
				merged.ProviderOptions[provider][key] = value
			}
		}
	}
	if len(override.ProviderAuthResolvers) > 0 {
		if merged.ProviderAuthResolvers == nil {
			merged.ProviderAuthResolvers = make(map[ProviderID]AuthResolver, len(override.ProviderAuthResolvers))
		}
		for provider, resolver := range override.ProviderAuthResolvers {
			merged.ProviderAuthResolvers[provider] = resolver
		}
	}
	if len(override.TextPayloadDebugHooks) > 0 {
		merged.TextPayloadDebugHooks = append(merged.TextPayloadDebugHooks, override.TextPayloadDebugHooks...)
	}
	if len(override.TextResponseDebugHooks) > 0 {
		merged.TextResponseDebugHooks = append(merged.TextResponseDebugHooks, override.TextResponseDebugHooks...)
	}
	if len(override.ImagePayloadDebugHooks) > 0 {
		merged.ImagePayloadDebugHooks = append(merged.ImagePayloadDebugHooks, override.ImagePayloadDebugHooks...)
	}
	if len(override.ImageResponseDebugHooks) > 0 {
		merged.ImageResponseDebugHooks = append(merged.ImageResponseDebugHooks, override.ImageResponseDebugHooks...)
	}
	if len(override.EmbeddingPayloadDebugHooks) > 0 {
		merged.EmbeddingPayloadDebugHooks = append(merged.EmbeddingPayloadDebugHooks, override.EmbeddingPayloadDebugHooks...)
	}
	if len(override.EmbeddingResponseDebugHooks) > 0 {
		merged.EmbeddingResponseDebugHooks = append(merged.EmbeddingResponseDebugHooks, override.EmbeddingResponseDebugHooks...)
	}
	if override.OpenAIOptions != nil {
		merged.OpenAIOptions = cloneOpenAIOptions(override.OpenAIOptions)
	}
	if override.AnthropicOptions != nil {
		merged.AnthropicOptions = cloneAnthropicOptions(override.AnthropicOptions)
	}
	if override.GoogleOptions != nil {
		merged.GoogleOptions = cloneGoogleOptions(override.GoogleOptions)
	}
	if override.MistralOptions != nil {
		merged.MistralOptions = cloneMistralOptions(override.MistralOptions)
	}
	if override.BedrockOptions != nil {
		merged.BedrockOptions = cloneBedrockOptions(override.BedrockOptions)
	}
	return merged
}

// mergeProviderAuthResolvers combines default and request-scoped provider auth
// callbacks into one view, with request-scoped entries winning per provider.
func mergeProviderAuthResolvers(defaults map[ProviderID]AuthResolver, requests map[ProviderID]AuthResolver) map[ProviderID]AuthResolver {
	if len(defaults) == 0 {
		return requests
	}
	if len(requests) == 0 {
		return defaults
	}
	merged := make(map[ProviderID]AuthResolver, len(defaults)+len(requests))
	maps.Copy(merged, defaults)
	maps.Copy(merged, requests)
	return merged
}

func modelDefaultOptions(model Model) Options {
	return Options{
		Transport: model.DefaultTransport,
	}
}

func applyProviderNeutralControls(model Model, options Options) Options {
	applied := cloneOptions(options)
	api := effectiveTextAPI(model)
	if applied.StructuredOutput != nil {
		applyStructuredOutputControl(api, &applied)
	}
	if applied.TopLogprobs > 0 {
		if applied.OpenAIOptions == nil {
			applied.OpenAIOptions = &OpenAIOptions{}
		}
		if applied.OpenAIOptions.TopLogprobs == 0 {
			applied.OpenAIOptions.TopLogprobs = applied.TopLogprobs
		}
	}
	return applied
}

func applyStructuredOutputControl(api API, options *Options) {
	switch api {
	case APIOpenAICompletions, APIOpenAIResponses, APIAzureOpenAIResponses, APIOpenAICodexResponses:
		applyOpenAIStructuredOutput(options)
	case APIAnthropicMessages:
		applyAnthropicStructuredOutput(options)
	case APIBedrockConverseStream:
		applyBedrockStructuredOutput(options)
	case APIGoogleGenerativeAI, APIGoogleVertex, APIMistralConversations:
	default:
	}
}

func applyOpenAIStructuredOutput(options *Options) {
	if options.OpenAIOptions == nil {
		options.OpenAIOptions = &OpenAIOptions{}
	}
	if options.OpenAIOptions.ResponseFormat == nil {
		options.OpenAIOptions.ResponseFormat = openAIStructuredOutput(*options.StructuredOutput)
	}
}

func applyAnthropicStructuredOutput(options *Options) {
	if options.AnthropicOptions == nil {
		options.AnthropicOptions = &AnthropicOptions{}
	}
	if options.AnthropicOptions.OutputFormat == nil {
		options.AnthropicOptions.OutputFormat = schemaStructuredOutput(*options.StructuredOutput)
	}
}

func applyBedrockStructuredOutput(options *Options) {
	if options.BedrockOptions == nil {
		options.BedrockOptions = &BedrockOptions{}
	}
	if options.BedrockOptions.ResponseFormat == nil {
		options.BedrockOptions.ResponseFormat = openAIStructuredOutput(*options.StructuredOutput)
	}
}

func openAIStructuredOutput(output StructuredOutput) map[string]any {
	if output.Type == StructuredOutputJSONSchema {
		jsonSchema := map[string]any{
			"name":   output.Name,
			"schema": cloneAnyValue(output.Schema),
		}
		if output.Strict {
			jsonSchema["strict"] = true
		}
		format := map[string]any{
			"type":        "json_schema",
			"json_schema": jsonSchema,
		}
		return format
	}
	return map[string]any{"type": "json_object"}
}

func schemaStructuredOutput(output StructuredOutput) map[string]any {
	format := map[string]any{
		"type":   "json_schema",
		"schema": map[string]any{"type": "object"},
	}
	if output.Type == StructuredOutputJSONSchema {
		format["schema"] = cloneAnyValue(output.Schema)
	}
	return format
}

func validateOptions(model Model, options Options) error {
	if err := validateTransport(model, options.Transport); err != nil {
		return err
	}
	if options.Temperature != nil && *options.Temperature < 0 {
		return invalidOptionsError(model, "temperature must be non-negative")
	}
	if options.MaxTokens != nil && *options.MaxTokens < 0 {
		return invalidOptionsError(model, "max tokens must be non-negative")
	}
	if options.Timeout != nil && *options.Timeout < 0 {
		return invalidOptionsError(model, "timeout must be non-negative")
	}
	if options.MaxRetries != nil && *options.MaxRetries < 0 {
		return invalidOptionsError(model, "max retries must be non-negative")
	}
	if options.MaxRetryDelay != nil && *options.MaxRetryDelay < 0 {
		return invalidOptionsError(model, "max retry delay must be non-negative")
	}
	if options.ThinkingBudgetTokens != nil && *options.ThinkingBudgetTokens < 0 {
		return invalidOptionsError(model, "thinking budget tokens must be non-negative")
	}
	if options.TopLogprobs < 0 {
		return invalidOptionsError(model, "top logprobs must be non-negative")
	}
	api := effectiveTextAPI(model)
	if options.TopLogprobs > 0 && api != APIOpenAICompletions {
		return invalidOptionsError(model, "top logprobs are only supported by openai-completions")
	}
	if options.StructuredOutput != nil {
		if err := validateStructuredOutput(model, api, *options.StructuredOutput); err != nil {
			return err
		}
	}
	if model.Provider == ProviderFireworks &&
		options.ThinkingBudgetTokens != nil &&
		*options.ThinkingBudgetTokens < 1024 {
		return invalidOptionsError(model, "fireworks thinking budget tokens must be at least 1024")
	}
	if options.AnthropicOptions != nil &&
		options.AnthropicOptions.ThinkingBudgetTokens != nil &&
		*options.AnthropicOptions.ThinkingBudgetTokens < 0 {
		return invalidOptionsError(model, "anthropic thinking budget tokens must be non-negative")
	}
	if options.GoogleOptions != nil &&
		options.GoogleOptions.ThinkingBudgetTokens != nil &&
		*options.GoogleOptions.ThinkingBudgetTokens < 0 {
		return invalidOptionsError(model, "google thinking budget tokens must be non-negative")
	}
	if options.GoogleOptions != nil &&
		options.GoogleOptions.ToolChoice != "" &&
		!validGoogleToolChoice(options.GoogleOptions.ToolChoice) {
		return invalidOptionsError(model, "google tool choice must be auto, none, or any")
	}
	if options.MistralOptions != nil &&
		options.MistralOptions.ToolChoice != nil &&
		!validMistralToolChoice(*options.MistralOptions.ToolChoice) {
		return invalidOptionsError(model, "mistral tool choice must be auto, none, any, or required")
	}
	if options.BedrockOptions != nil {
		if options.BedrockOptions.TopP != nil && *options.BedrockOptions.TopP < 0 {
			return invalidOptionsError(model, "bedrock top_p must be non-negative")
		}
		if options.BedrockOptions.ToolChoice != nil && !validBedrockToolChoice(*options.BedrockOptions.ToolChoice) {
			return invalidOptionsError(model, "bedrock tool choice must be auto, none, any, or a named tool")
		}
	}
	if options.OpenAIOptions != nil {
		if options.OpenAIOptions.TopLogprobs < 0 {
			return invalidOptionsError(model, "openai top logprobs must be non-negative")
		}
		if options.OpenAIOptions.CodexWebSocketConnectTimeout != nil && *options.OpenAIOptions.CodexWebSocketConnectTimeout < 0 {
			return invalidOptionsError(model, "openai codex websocket connect timeout must be non-negative")
		}
		if options.OpenAIOptions.TopLogprobs > 0 && api != APIOpenAICompletions {
			return invalidOptionsError(model, "openai logprobs are only supported by openai-completions")
		}
		if options.OpenAIOptions.ResponseFormat != nil && !supportsOpenAIResponseFormat(api) {
			return invalidOptionsError(model, "openai response format is only supported by OpenAI-compatible APIs")
		}
	}
	return nil
}

func validateStructuredOutput(model Model, api API, output StructuredOutput) error {
	switch output.Type {
	case StructuredOutputJSONObject:
	case StructuredOutputJSONSchema:
		if output.Name == "" {
			return invalidOptionsError(model, "structured output schema name is required")
		}
		if output.Schema == nil {
			return invalidOptionsError(model, "structured output schema is required")
		}
	default:
		return invalidOptionsError(model, "structured output type must be json_object or json_schema")
	}
	if !supportsStructuredOutput(api) {
		return invalidOptionsError(model, "structured output is only supported by openai-compatible, anthropic-messages, and bedrock-converse-stream APIs")
	}
	return nil
}

func supportsStructuredOutput(api API) bool {
	switch api {
	case APIOpenAICompletions,
		APIOpenAIResponses,
		APIAzureOpenAIResponses,
		APIOpenAICodexResponses,
		APIAnthropicMessages,
		APIBedrockConverseStream:
		return true
	default:
		return false
	}
}

func validateTransport(model Model, transport Transport) error {
	switch transport {
	case "", TransportSSE, TransportHTTP, TransportWebSocket:
	default:
		return invalidOptionsError(model, fmt.Sprintf("unsupported transport %q", transport))
	}

	api := effectiveTextAPI(model)
	if !builtInTextAPI(api) {
		return nil
	}
	switch transport {
	case "", TransportSSE:
		return nil
	case TransportWebSocket:
		if api == APIOpenAICodexResponses {
			return nil
		}
		return invalidOptionsError(model, fmt.Sprintf("transport %q is only supported by openai-codex-responses", transport))
	case TransportHTTP:
		return invalidOptionsError(model, fmt.Sprintf("transport %q is not supported by streaming text providers", transport))
	default:
		return nil
	}
}

func builtInTextAPI(api API) bool {
	switch api {
	case APIOpenAICompletions,
		APIOpenAIResponses,
		APIAzureOpenAIResponses,
		APIOpenAICodexResponses,
		APIAnthropicMessages,
		APIBedrockConverseStream,
		APIGoogleGenerativeAI,
		APIGoogleVertex,
		APIMistralConversations:
		return true
	default:
		return false
	}
}

func validBedrockToolChoice(choice BedrockToolChoice) bool {
	switch choice.Type {
	case BedrockToolChoiceAuto, BedrockToolChoiceAny, BedrockToolChoiceNone:
		return choice.Name == ""
	case BedrockToolChoiceTool:
		return choice.Name != ""
	default:
		return false
	}
}

func validMistralToolChoice(choice MistralToolChoice) bool {
	switch choice.Type {
	case MistralToolChoiceAuto, MistralToolChoiceAny, MistralToolChoiceNone, MistralToolChoiceRequired:
		return choice.Name == ""
	default:
		return false
	}
}

func validGoogleToolChoice(choice string) bool {
	switch choice {
	case "auto", "none", "any", "AUTO", "NONE", "ANY":
		return true
	default:
		return false
	}
}

func effectiveTextAPI(model Model) API {
	if model.ProviderMetadata != nil {
		if api, ok := model.ProviderMetadata["opencodeAPI"].(string); ok && api != "" {
			return API(api)
		}
	}
	return model.API
}

func supportsOpenAIResponseFormat(api API) bool {
	switch api {
	case APIOpenAICompletions, APIOpenAIResponses, APIAzureOpenAIResponses, APIOpenAICodexResponses:
		return true
	default:
		return false
	}
}

func invalidOptionsError(model Model, message string) error {
	return &Error{
		Code:     ErrorInvalidOptions,
		Message:  message,
		Provider: model.Provider,
		Model:    model.ID,
	}
}

func assistantText(message AssistantMessage) (string, error) {
	var text string
	for _, block := range message.Content {
		if block.Type != ContentBlockText {
			return "", &Error{
				Code:     ErrorUnsupported,
				Message:  "assistant message contains non-text content",
				Provider: message.Provider,
				Model:    message.Model,
			}
		}
		text += block.Text
	}
	return text, nil
}
