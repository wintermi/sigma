// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"net/http"
	"time"
)

const (
	// ErrorInvalidOptions indicates request options failed local validation.
	ErrorInvalidOptions ErrorCode = "invalid-options"
)

// OpenAIOptions carries OpenAI-specific request options known to the root
// package without importing provider adapters.
type OpenAIOptions struct {
	ReasoningEffort      ThinkingLevel
	ReasoningSummary     string
	ServiceTier          string
	ToolChoice           any
	ResponseFormat       any
	TopLogprobs          int
	PromptCacheRetention string
	ParallelToolCalls    *bool
	TextVerbosity        string
}

// AnthropicOptions carries Anthropic-specific request options known to the root
// package without importing provider adapters.
type AnthropicOptions struct {
	ThinkingBudgetTokens *int
}

// GoogleOptions carries Google-specific request options known to the root
// package without importing provider adapters.
type GoogleOptions struct {
	ThinkingBudgetTokens *int
	ToolChoice           string
	DisableThinking      *bool
}

// BedrockToolChoiceType identifies Bedrock Converse tool selection behavior.
type BedrockToolChoiceType string

const (
	// BedrockToolChoiceAuto lets Bedrock choose whether to call a tool.
	BedrockToolChoiceAuto BedrockToolChoiceType = "auto"
	// BedrockToolChoiceAny requires Bedrock to call one of the supplied tools.
	BedrockToolChoiceAny BedrockToolChoiceType = "any"
	// BedrockToolChoiceNone omits tools from the Bedrock request.
	BedrockToolChoiceNone BedrockToolChoiceType = "none"
	// BedrockToolChoiceTool requires Bedrock to call the named tool.
	BedrockToolChoiceTool BedrockToolChoiceType = "tool"
)

// BedrockToolChoice carries Bedrock Converse tool choice controls.
type BedrockToolChoice struct {
	Type BedrockToolChoiceType `json:"type"`
	Name string                `json:"name,omitempty"`
}

// BedrockThinkingDisplay controls how Claude thinking content is returned by
// Bedrock when the model supports the display field.
type BedrockThinkingDisplay string

const (
	// BedrockThinkingDisplaySummarized requests summarized thinking text.
	BedrockThinkingDisplaySummarized BedrockThinkingDisplay = "summarized"
	// BedrockThinkingDisplayOmitted asks Bedrock to omit thinking text while
	// preserving signatures for replay.
	BedrockThinkingDisplayOmitted BedrockThinkingDisplay = "omitted"
)

// BedrockOptions carries Bedrock-specific request options known to the root
// package without importing the provider adapter.
type BedrockOptions struct {
	ToolChoice                        *BedrockToolChoice
	ThinkingDisplay                   BedrockThinkingDisplay
	InterleavedThinking               *bool
	StopSequences                     []string
	TopP                              *float64
	RequestMetadata                   map[string]string
	AdditionalModelRequestFields      map[string]any
	AdditionalModelResponseFieldPaths []string
}

// Options configures a single provider request.
//
// Client.Stream merges options in this order: client defaults, defaults from
// the selected model metadata, call options, then provider-specific extension
// values inside ProviderOptions. Provider packages may define their own helper
// option functions that populate ProviderOptions without changing this root
// package.
type Options struct {
	Temperature             *float64
	MaxTokens               *int
	APIKey                  string
	HTTPClient              *http.Client
	AuthResolver            AuthResolver
	Transport               Transport
	CacheRetention          CacheRetention
	SessionID               string
	Headers                 map[string]string
	Timeout                 *time.Duration
	MaxRetries              *int
	MaxRetryDelay           *time.Duration
	Metadata                map[string]any
	ReasoningLevel          ThinkingLevel
	ThinkingBudgetTokens    *int
	ProviderOptions         map[ProviderID]map[string]any
	ProviderAuthResolvers   map[ProviderID]AuthResolver
	TextPayloadDebugHooks   []TextPayloadDebugHook
	TextResponseDebugHooks  []TextResponseDebugHook
	ImagePayloadDebugHooks  []ImagePayloadDebugHook
	ImageResponseDebugHooks []ImageResponseDebugHook
	OpenAIOptions           *OpenAIOptions
	AnthropicOptions        *AnthropicOptions
	GoogleOptions           *GoogleOptions
	BedrockOptions          *BedrockOptions
}

// Option configures a single provider request.
type Option func(*Options)

// WithTemperature configures sampling temperature for a request.
func WithTemperature(temperature float64) Option {
	return func(options *Options) {
		options.Temperature = float64Ptr(temperature)
	}
}

// WithMaxTokens configures the maximum output tokens for a request.
func WithMaxTokens(maxTokens int) Option {
	return func(options *Options) {
		options.MaxTokens = intPtr(maxTokens)
	}
}

// WithAPIKey configures a request-scoped API key override.
//
// API key overrides are intentionally not retained by WithDefaultOptions.
func WithAPIKey(apiKey string) Option {
	return func(options *Options) {
		options.APIKey = apiKey
	}
}

// WithTransport configures the provider transport for a request.
func WithTransport(transport Transport) Option {
	return func(options *Options) {
		options.Transport = transport
	}
}

// WithCacheRetention configures provider-side prompt cache retention.
func WithCacheRetention(retention CacheRetention) Option {
	return func(options *Options) {
		options.CacheRetention = retention
	}
}

// WithSessionID configures a provider conversation or response session id.
func WithSessionID(sessionID string) Option {
	return func(options *Options) {
		options.SessionID = sessionID
	}
}

// WithHeader adds or replaces a request header.
func WithHeader(key, value string) Option {
	return func(options *Options) {
		if options.Headers == nil {
			options.Headers = make(map[string]string)
		}
		options.Headers[key] = value
	}
}

// WithHeaders adds or replaces request headers.
func WithHeaders(headers map[string]string) Option {
	return func(options *Options) {
		if len(headers) == 0 {
			return
		}
		if options.Headers == nil {
			options.Headers = make(map[string]string, len(headers))
		}
		for key, value := range headers {
			options.Headers[key] = value
		}
	}
}

// WithTimeout configures the per-request provider timeout. Zero disables the
// request timeout; cancellation still follows the parent context.
func WithTimeout(timeout time.Duration) Option {
	return func(options *Options) {
		options.Timeout = durationPtr(timeout)
	}
}

// WithMaxRetries configures the maximum HTTP provider retry attempts after the
// first request. The default is DefaultMaxRetries.
func WithMaxRetries(maxRetries int) Option {
	return func(options *Options) {
		options.MaxRetries = intPtr(maxRetries)
	}
}

// WithMaxRetryDelay configures the maximum delay between HTTP provider retries,
// including provider Retry-After values. The default is DefaultMaxRetryDelay.
func WithMaxRetryDelay(maxRetryDelay time.Duration) Option {
	return func(options *Options) {
		options.MaxRetryDelay = durationPtr(maxRetryDelay)
	}
}

// WithMetadata adds or replaces provider-neutral request metadata.
func WithMetadata(metadata map[string]any) Option {
	return func(options *Options) {
		if len(metadata) == 0 {
			return
		}
		if options.Metadata == nil {
			options.Metadata = make(map[string]any, len(metadata))
		}
		for key, value := range metadata {
			options.Metadata[key] = value
		}
	}
}

// WithMetadataValue adds or replaces one provider-neutral metadata value.
func WithMetadataValue(key string, value any) Option {
	return func(options *Options) {
		if options.Metadata == nil {
			options.Metadata = make(map[string]any)
		}
		options.Metadata[key] = value
	}
}

// WithReasoningLevel configures a provider-neutral reasoning level.
func WithReasoningLevel(level ThinkingLevel) Option {
	return func(options *Options) {
		options.ReasoningLevel = level
	}
}

// WithThinkingBudgetTokens configures a provider-neutral thinking budget.
func WithThinkingBudgetTokens(tokens int) Option {
	return func(options *Options) {
		options.ThinkingBudgetTokens = intPtr(tokens)
	}
}

// WithProviderOptions adds or replaces advanced provider-specific values.
func WithProviderOptions(provider ProviderID, values map[string]any) Option {
	return func(options *Options) {
		if len(values) == 0 {
			return
		}
		if options.ProviderOptions == nil {
			options.ProviderOptions = make(map[ProviderID]map[string]any)
		}
		if options.ProviderOptions[provider] == nil {
			options.ProviderOptions[provider] = make(map[string]any, len(values))
		}
		for key, value := range values {
			options.ProviderOptions[provider][key] = value
		}
	}
}

// WithProviderOption adds or replaces one advanced provider-specific value.
func WithProviderOption(provider ProviderID, key string, value any) Option {
	return func(options *Options) {
		if options.ProviderOptions == nil {
			options.ProviderOptions = make(map[ProviderID]map[string]any)
		}
		if options.ProviderOptions[provider] == nil {
			options.ProviderOptions[provider] = make(map[string]any)
		}
		options.ProviderOptions[provider][key] = value
	}
}

// WithProviderAuthResolver configures a provider-specific credential callback.
func WithProviderAuthResolver(provider ProviderID, resolver AuthResolver) Option {
	return func(options *Options) {
		if resolver == nil {
			return
		}
		if options.ProviderAuthResolvers == nil {
			options.ProviderAuthResolvers = make(map[ProviderID]AuthResolver)
		}
		options.ProviderAuthResolvers[provider] = resolver
	}
}

// WithOpenAIOptions configures known OpenAI-specific request options.
func WithOpenAIOptions(openAIOptions OpenAIOptions) Option {
	return func(options *Options) {
		options.OpenAIOptions = cloneOpenAIOptions(&openAIOptions)
	}
}

// WithAnthropicOptions configures known Anthropic-specific request options.
func WithAnthropicOptions(anthropicOptions AnthropicOptions) Option {
	return func(options *Options) {
		options.AnthropicOptions = cloneAnthropicOptions(&anthropicOptions)
	}
}

// WithGoogleOptions configures known Google-specific request options.
func WithGoogleOptions(googleOptions GoogleOptions) Option {
	return func(options *Options) {
		options.GoogleOptions = cloneGoogleOptions(&googleOptions)
	}
}

// WithBedrockOptions configures known Bedrock-specific request options.
func WithBedrockOptions(bedrockOptions BedrockOptions) Option {
	return func(options *Options) {
		options.BedrockOptions = cloneBedrockOptions(&bedrockOptions)
	}
}

func applyOptions(options Options, opts []Option) Options {
	applied := cloneOptions(options)
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	return applied
}

func cloneOptions(options Options) Options {
	return Options{
		Temperature:             cloneFloat64Ptr(options.Temperature),
		MaxTokens:               cloneIntPtr(options.MaxTokens),
		APIKey:                  options.APIKey,
		HTTPClient:              options.HTTPClient,
		AuthResolver:            options.AuthResolver,
		Transport:               options.Transport,
		CacheRetention:          options.CacheRetention,
		SessionID:               options.SessionID,
		Headers:                 copyStringStringMap(options.Headers),
		Timeout:                 cloneDurationPtr(options.Timeout),
		MaxRetries:              cloneIntPtr(options.MaxRetries),
		MaxRetryDelay:           cloneDurationPtr(options.MaxRetryDelay),
		Metadata:                copyStringAnyMap(options.Metadata),
		ReasoningLevel:          options.ReasoningLevel,
		ThinkingBudgetTokens:    cloneIntPtr(options.ThinkingBudgetTokens),
		ProviderOptions:         copyProviderOptions(options.ProviderOptions),
		ProviderAuthResolvers:   copyProviderAuthResolvers(options.ProviderAuthResolvers),
		TextPayloadDebugHooks:   append([]TextPayloadDebugHook(nil), options.TextPayloadDebugHooks...),
		TextResponseDebugHooks:  append([]TextResponseDebugHook(nil), options.TextResponseDebugHooks...),
		ImagePayloadDebugHooks:  append([]ImagePayloadDebugHook(nil), options.ImagePayloadDebugHooks...),
		ImageResponseDebugHooks: append([]ImageResponseDebugHook(nil), options.ImageResponseDebugHooks...),
		OpenAIOptions:           cloneOpenAIOptions(options.OpenAIOptions),
		AnthropicOptions:        cloneAnthropicOptions(options.AnthropicOptions),
		GoogleOptions:           cloneGoogleOptions(options.GoogleOptions),
		BedrockOptions:          cloneBedrockOptions(options.BedrockOptions),
	}
}

func copyProviderOptions(values map[ProviderID]map[string]any) map[ProviderID]map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[ProviderID]map[string]any, len(values))
	for provider, providerValues := range values {
		copied[provider] = copyStringAnyMap(providerValues)
	}
	return copied
}

func copyProviderAuthResolvers(values map[ProviderID]AuthResolver) map[ProviderID]AuthResolver {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[ProviderID]AuthResolver, len(values))
	for provider, resolver := range values {
		copied[provider] = resolver
	}
	return copied
}

func cloneOpenAIOptions(options *OpenAIOptions) *OpenAIOptions {
	if options == nil {
		return nil
	}
	copied := *options
	copied.ParallelToolCalls = cloneBoolPtr(options.ParallelToolCalls)
	return &copied
}

func cloneAnthropicOptions(options *AnthropicOptions) *AnthropicOptions {
	if options == nil {
		return nil
	}
	copied := *options
	copied.ThinkingBudgetTokens = cloneIntPtr(options.ThinkingBudgetTokens)
	return &copied
}

func cloneGoogleOptions(options *GoogleOptions) *GoogleOptions {
	if options == nil {
		return nil
	}
	copied := *options
	copied.ThinkingBudgetTokens = cloneIntPtr(options.ThinkingBudgetTokens)
	copied.DisableThinking = cloneBoolPtr(options.DisableThinking)
	return &copied
}

func cloneBedrockOptions(options *BedrockOptions) *BedrockOptions {
	if options == nil {
		return nil
	}
	copied := *options
	if options.ToolChoice != nil {
		toolChoice := *options.ToolChoice
		copied.ToolChoice = &toolChoice
	}
	copied.InterleavedThinking = cloneBoolPtr(options.InterleavedThinking)
	copied.StopSequences = append([]string(nil), options.StopSequences...)
	copied.TopP = cloneFloat64Ptr(options.TopP)
	copied.RequestMetadata = copyStringStringMap(options.RequestMetadata)
	copied.AdditionalModelRequestFields = copyStringAnyMap(options.AdditionalModelRequestFields)
	copied.AdditionalModelResponseFieldPaths = append([]string(nil), options.AdditionalModelResponseFieldPaths...)
	return &copied
}

func intPtr(value int) *int {
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}

func durationPtr(value time.Duration) *time.Duration {
	return &value
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneDurationPtr(value *time.Duration) *time.Duration {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
