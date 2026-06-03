// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// EmbeddingOption configures a single embedding provider request.
type EmbeddingOption func(*Options)

// WithEmbeddingAPIKey configures a request-scoped embedding API key override.
func WithEmbeddingAPIKey(apiKey string) EmbeddingOption {
	return embeddingOptionFromOption(WithAPIKey(apiKey))
}

// WithEmbeddingHTTPClient configures the HTTP client exposed to embedding providers.
func WithEmbeddingHTTPClient(httpClient *http.Client) EmbeddingOption {
	return func(options *Options) {
		options.HTTPClient = httpClient
	}
}

// WithEmbeddingAuthResolver configures a request-scoped credential resolver.
func WithEmbeddingAuthResolver(resolver AuthResolver) EmbeddingOption {
	return func(options *Options) {
		options.AuthResolver = resolver
	}
}

// WithEmbeddingHeader adds or replaces an embedding request header.
func WithEmbeddingHeader(key, value string) EmbeddingOption {
	return embeddingOptionFromOption(WithHeader(key, value))
}

// WithEmbeddingHeaders adds or replaces embedding request headers.
func WithEmbeddingHeaders(headers map[string]string) EmbeddingOption {
	return embeddingOptionFromOption(WithHeaders(headers))
}

// WithEmbeddingTimeout configures the per-request embedding provider timeout.
func WithEmbeddingTimeout(timeout time.Duration) EmbeddingOption {
	return embeddingOptionFromOption(WithTimeout(timeout))
}

// WithEmbeddingMaxRetries configures the maximum embedding provider retry attempts.
func WithEmbeddingMaxRetries(maxRetries int) EmbeddingOption {
	return embeddingOptionFromOption(WithMaxRetries(maxRetries))
}

// WithEmbeddingMaxRetryDelay configures the maximum delay between embedding provider retries.
func WithEmbeddingMaxRetryDelay(maxRetryDelay time.Duration) EmbeddingOption {
	return embeddingOptionFromOption(WithMaxRetryDelay(maxRetryDelay))
}

// WithEmbeddingMetadata adds or replaces provider-neutral embedding request metadata.
func WithEmbeddingMetadata(metadata map[string]any) EmbeddingOption {
	return embeddingOptionFromOption(WithMetadata(metadata))
}

// WithEmbeddingMetadataValue adds or replaces one provider-neutral embedding metadata value.
func WithEmbeddingMetadataValue(key string, value any) EmbeddingOption {
	return embeddingOptionFromOption(WithMetadataValue(key, value))
}

// WithEmbeddingProviderOptions adds or replaces advanced provider-specific embedding values.
func WithEmbeddingProviderOptions(provider ProviderID, values map[string]any) EmbeddingOption {
	return embeddingOptionFromOption(WithProviderOptions(provider, values))
}

// WithEmbeddingProviderOption adds or replaces one advanced provider-specific embedding value.
func WithEmbeddingProviderOption(provider ProviderID, key string, value any) EmbeddingOption {
	return embeddingOptionFromOption(WithProviderOption(provider, key, value))
}

// WithEmbeddingProviderAuthResolver configures a provider-specific embedding credential callback.
func WithEmbeddingProviderAuthResolver(provider ProviderID, resolver AuthResolver) EmbeddingOption {
	return embeddingOptionFromOption(WithProviderAuthResolver(provider, resolver))
}

// Embed calls the registered embedding provider for model.
func (c *Client) Embed(ctx context.Context, model EmbeddingModel, req EmbeddingRequest, opts ...EmbeddingOption) (Embeddings, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		c = NewClient()
	}
	if err := ValidateModelRef(ModelRef{Provider: model.Provider, ID: model.ID}); err != nil {
		return Embeddings{Model: model.ID, Provider: model.Provider}, err
	}

	registered, ok := c.GetEmbeddingModel(model.Provider, model.ID)
	if !ok {
		return Embeddings{Model: model.ID, Provider: model.Provider}, embeddingModelNotFoundError(model.Provider, model.ID)
	}
	if model.API == "" {
		model = registered
	}

	provider, ok := c.registry.EmbeddingProvider(model.Provider)
	if !ok {
		return Embeddings{Model: model.ID, Provider: model.Provider}, embeddingProviderNotFoundError(model.Provider, model.ID)
	}

	options := c.embeddingRequestOptions(opts)
	if err := validateEmbeddingOptions(model, req, options); err != nil {
		return Embeddings{Model: model.ID, Provider: model.Provider}, err
	}
	if err := ctx.Err(); err != nil {
		return Embeddings{Model: model.ID, Provider: model.Provider}, embeddingAbortedError(err)
	}

	embeddings, err := provider.Embed(ctx, model, req, options)
	embeddings = finalEmbeddings(model, embeddings)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return embeddings, embeddingAbortedError(err)
		}
		return embeddings, fmt.Errorf("embedding provider: %w", err)
	}
	return embeddings, nil
}

// Embed calls the registered embedding provider using the default registry.
func Embed(ctx context.Context, model EmbeddingModel, req EmbeddingRequest, opts ...EmbeddingOption) (Embeddings, error) {
	return defaultClient().Embed(ctx, model, req, opts...)
}

// GetEmbeddingModel returns an embedding model by provider and model id.
func (c *Client) GetEmbeddingModel(provider ProviderID, id ModelID) (EmbeddingModel, bool) {
	if c == nil || c.registry == nil {
		return EmbeddingModel{}, false
	}
	return c.registry.EmbeddingModel(provider, id)
}

// EmbeddingModels returns embedding models from the client registry.
func (c *Client) EmbeddingModels() []EmbeddingModel {
	if c == nil || c.registry == nil {
		return nil
	}
	return c.registry.ListEmbeddingModels()
}

// GetEmbeddingModel returns an embedding model from the default registry.
func GetEmbeddingModel(provider ProviderID, id ModelID) (EmbeddingModel, bool) {
	return defaultClient().GetEmbeddingModel(provider, id)
}

// EmbeddingModels returns embedding models from the default registry.
func EmbeddingModels() []EmbeddingModel {
	return defaultClient().EmbeddingModels()
}

func (c *Client) embeddingRequestOptions(opts []EmbeddingOption) Options {
	options := Options{
		HTTPClient: c.httpClient,
		Headers:    copyStringStringMap(c.defaultHeaders),
	}
	options = mergeOptions(options, c.defaultOptions)
	options = applyEmbeddingOptions(options, opts)
	clientResolver := c.authResolver
	if options.AuthResolver != nil {
		clientResolver = options.AuthResolver
	}
	options.AuthResolver = ChainAuthResolver{
		Client:            clientResolver,
		ProviderCallbacks: options.ProviderAuthResolvers,
	}
	return options
}

func applyEmbeddingOptions(options Options, opts []EmbeddingOption) Options {
	applied := cloneOptions(options)
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	return applied
}

func embeddingOptionFromOption(opt Option) EmbeddingOption {
	return func(options *Options) {
		if opt != nil {
			opt(options)
		}
	}
}

func validateEmbeddingOptions(model EmbeddingModel, req EmbeddingRequest, options Options) error {
	if len(req.Inputs) == 0 {
		return invalidEmbeddingOptionsError(model, "embedding inputs are required")
	}
	for _, input := range req.Inputs {
		if strings.TrimSpace(input) == "" {
			return invalidEmbeddingOptionsError(model, "embedding inputs must not be empty")
		}
	}
	if req.Dimensions < 0 {
		return invalidEmbeddingOptionsError(model, "embedding dimensions must be non-negative")
	}
	if options.Timeout != nil && *options.Timeout < 0 {
		return invalidEmbeddingOptionsError(model, "timeout must be non-negative")
	}
	if options.MaxRetries != nil && *options.MaxRetries < 0 {
		return invalidEmbeddingOptionsError(model, "max retries must be non-negative")
	}
	if options.MaxRetryDelay != nil && *options.MaxRetryDelay < 0 {
		return invalidEmbeddingOptionsError(model, "max retry delay must be non-negative")
	}
	return nil
}

func invalidEmbeddingOptionsError(model EmbeddingModel, message string) error {
	return &Error{
		Code:     ErrorInvalidOptions,
		Message:  message,
		Provider: model.Provider,
		Model:    model.ID,
	}
}

func embeddingProviderNotFoundError(provider ProviderID, model ModelID) error {
	return &Error{
		Code:     ErrorProviderNotFound,
		Message:  "embedding provider is not registered",
		Provider: provider,
		Model:    model,
	}
}

func embeddingModelNotFoundError(provider ProviderID, model ModelID) error {
	return &Error{
		Code:     ErrorModelNotFound,
		Message:  "embedding model is not registered",
		Provider: provider,
		Model:    model,
	}
}

func embeddingAbortedError(err error) error {
	return &Error{
		Code:    ErrorAborted,
		Message: err.Error(),
		Err:     err,
	}
}

func finalEmbeddings(model EmbeddingModel, embeddings Embeddings) Embeddings {
	if embeddings.Model == "" {
		embeddings.Model = model.ID
	}
	if embeddings.Provider == "" {
		embeddings.Provider = model.Provider
	}
	if embeddings.Usage != nil && embeddings.Cost == nil {
		cost := CostForEmbeddingUsage(model, *embeddings.Usage)
		embeddings.Cost = &cost
	}
	return embeddings
}
