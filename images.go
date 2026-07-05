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

// ImageSize identifies a provider-neutral generated image size.
type ImageSize string

// ImageQuality identifies a provider-neutral generated image quality.
type ImageQuality string

// ImageError records a provider-reported image generation error that belongs
// to a response body rather than the Go error return.
type ImageError struct {
	Code             string         `json:"code,omitempty"`
	Message          string         `json:"message,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
}

const (
	// ImageSize1024x1024 requests a square image where the provider supports it.
	ImageSize1024x1024 ImageSize = "1024x1024"
	// ImageSize1024x1536 requests a portrait image where the provider supports it.
	ImageSize1024x1536 ImageSize = "1024x1536"
	// ImageSize1536x1024 requests a landscape image where the provider supports it.
	ImageSize1536x1024 ImageSize = "1536x1024"
)

const (
	// ImageQualityLow requests a lower-cost image where the provider supports it.
	ImageQualityLow ImageQuality = "low"
	// ImageQualityMedium requests a balanced image quality where the provider supports it.
	ImageQualityMedium ImageQuality = "medium"
	// ImageQualityHigh requests a higher-quality image where the provider supports it.
	ImageQualityHigh ImageQuality = "high"
)

// ImageOption configures a single image provider request.
//
// Image generation options are intentionally separate from text generation
// options. Provider adapters still receive the shared internal Options shape so
// auth, headers, HTTP clients, retry policy, metadata, and provider extension
// values follow the same conventions as text requests.
type ImageOption func(*Options)

// WithImageAPIKey configures a request-scoped image API key override.
func WithImageAPIKey(apiKey string) ImageOption {
	return imageOptionFromOption(WithAPIKey(apiKey))
}

// WithImageHTTPClient configures the HTTP client exposed to image providers.
func WithImageHTTPClient(httpClient *http.Client) ImageOption {
	return func(options *Options) {
		options.HTTPClient = httpClient
	}
}

// WithImageAuthResolver configures a request-scoped credential resolver.
func WithImageAuthResolver(resolver AuthResolver) ImageOption {
	return func(options *Options) {
		options.AuthResolver = resolver
	}
}

// WithImageHeader adds or replaces an image request header.
func WithImageHeader(key, value string) ImageOption {
	return imageOptionFromOption(WithHeader(key, value))
}

// WithImageHeaders adds or replaces image request headers.
func WithImageHeaders(headers map[string]string) ImageOption {
	return imageOptionFromOption(WithHeaders(headers))
}

// WithImageSuppressedHeader removes a final outgoing image request header.
func WithImageSuppressedHeader(key string) ImageOption {
	return imageOptionFromOption(WithSuppressedHeader(key))
}

// WithImageSuppressedHeaders removes final outgoing image request headers.
func WithImageSuppressedHeaders(keys ...string) ImageOption {
	return imageOptionFromOption(WithSuppressedHeaders(keys...))
}

// WithImageTimeout configures the per-request image provider timeout.
func WithImageTimeout(timeout time.Duration) ImageOption {
	return imageOptionFromOption(WithTimeout(timeout))
}

// WithImageMaxRetries configures the maximum image provider retry attempts.
func WithImageMaxRetries(maxRetries int) ImageOption {
	return imageOptionFromOption(WithMaxRetries(maxRetries))
}

// WithImageMaxRetryDelay configures the maximum delay between image provider retries.
func WithImageMaxRetryDelay(maxRetryDelay time.Duration) ImageOption {
	return imageOptionFromOption(WithMaxRetryDelay(maxRetryDelay))
}

// WithImageMetadata adds or replaces provider-neutral image request metadata.
func WithImageMetadata(metadata map[string]any) ImageOption {
	return imageOptionFromOption(WithMetadata(metadata))
}

// WithImageMetadataValue adds or replaces one provider-neutral image metadata value.
func WithImageMetadataValue(key string, value any) ImageOption {
	return imageOptionFromOption(WithMetadataValue(key, value))
}

// WithImageProviderOptions adds or replaces advanced provider-specific image values.
func WithImageProviderOptions(provider ProviderID, values map[string]any) ImageOption {
	return imageOptionFromOption(WithProviderOptions(provider, values))
}

// WithImageProviderOption adds or replaces one advanced provider-specific image value.
func WithImageProviderOption(provider ProviderID, key string, value any) ImageOption {
	return imageOptionFromOption(WithProviderOption(provider, key, value))
}

// WithImageProviderAuthResolver configures a provider-specific image credential callback.
func WithImageProviderAuthResolver(provider ProviderID, resolver AuthResolver) ImageOption {
	return imageOptionFromOption(WithProviderAuthResolver(provider, resolver))
}

// GenerateImages calls the registered image provider for model.
func (c *Client) GenerateImages(ctx context.Context, model ImageModel, req ImageRequest, opts ...ImageOption) (AssistantImages, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		c = NewClient()
	}
	if err := ValidateModelRef(ModelRef{Provider: model.Provider, ID: model.ID}); err != nil {
		return AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: StopReasonError}, err
	}

	registered, ok := c.GetImageModel(model.Provider, model.ID)
	if !ok {
		return AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: StopReasonError}, modelNotFoundError(model.Provider, model.ID)
	}
	if model.API == "" {
		model = registered
	}

	provider, ok := c.registry.ImageProvider(model.Provider)
	if !ok {
		return AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: StopReasonError}, imageProviderNotFoundError(model.Provider, model.ID)
	}

	options := c.imageRequestOptions(opts)
	if err := validateImageOptions(model, options); err != nil {
		return AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: StopReasonError}, err
	}
	if err := validateImageRequest(model, req); err != nil {
		return AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: StopReasonError}, err
	}
	if err := ctx.Err(); err != nil {
		return AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: StopReasonAborted}, imageAbortedError(err)
	}

	images, err := provider.Generate(ctx, model, req, options)
	images = finalImages(model, images, err)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return images, imageAbortedError(err)
		}
		return images, err
	}
	return images, nil
}

// StreamImages starts a streaming image provider call for model.
func (c *Client) StreamImages(ctx context.Context, model ImageModel, req ImageRequest, opts ...ImageOption) *ImageStream {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		c = NewClient()
	}
	if err := ValidateModelRef(ModelRef{Provider: model.Provider, ID: model.ID}); err != nil {
		return errorImageStream(ctx, err, AssistantImages{Model: model.ID, Provider: model.Provider})
	}

	registered, ok := c.GetImageModel(model.Provider, model.ID)
	if !ok {
		return errorImageStream(ctx, modelNotFoundError(model.Provider, model.ID), AssistantImages{Model: model.ID, Provider: model.Provider})
	}
	if model.API == "" {
		model = registered
	}

	provider, ok := c.registry.ImageProvider(model.Provider)
	if !ok {
		return errorImageStream(ctx, imageProviderNotFoundError(model.Provider, model.ID), AssistantImages{Model: model.ID, Provider: model.Provider})
	}

	options := c.imageRequestOptions(opts)
	if err := validateImageOptions(model, options); err != nil {
		return errorImageStream(ctx, err, AssistantImages{Model: model.ID, Provider: model.Provider})
	}
	if err := validateImageRequest(model, req); err != nil {
		return errorImageStream(ctx, err, AssistantImages{Model: model.ID, Provider: model.Provider})
	}
	if err := ctx.Err(); err != nil {
		return errorImageStream(ctx, imageAbortedError(err), AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: StopReasonAborted})
	}

	if streaming, ok := provider.(StreamingImageProvider); ok {
		return streaming.StreamImages(ctx, model, req, options)
	}
	return c.generateImagesAsStream(ctx, model, req, provider, options)
}

func (c *Client) generateImagesAsStream(ctx context.Context, model ImageModel, req ImageRequest, provider ImageProvider, options Options) *ImageStream {
	stream, writer := NewImageStream(ctx)
	go func() {
		images, err := provider.Generate(ctx, model, req, options)
		images = finalImages(model, images, err)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				_ = writer.Error(ctx, imageAbortedError(err), images)
				return
			}
			_ = writer.Error(ctx, err, images)
			return
		}
		for index := range images.Images {
			image := images.Images[index]
			sequence := index
			if err := writer.Emit(ctx, ImageEvent{Kind: ImageEventKindImage, Image: &image, SequenceIndex: &sequence}); err != nil {
				return
			}
		}
		_ = writer.Done(ctx, images)
	}()
	return stream
}

// GenerateImages calls the registered image provider using the default registry.
func GenerateImages(ctx context.Context, model ImageModel, req ImageRequest, opts ...ImageOption) (AssistantImages, error) {
	return defaultClient().GenerateImages(ctx, model, req, opts...)
}

// StreamImages starts a streaming image provider call using the default registry.
func StreamImages(ctx context.Context, model ImageModel, req ImageRequest, opts ...ImageOption) *ImageStream {
	return defaultClient().StreamImages(ctx, model, req, opts...)
}

// GetImageModel returns an image model by provider and model id.
func (c *Client) GetImageModel(provider ProviderID, id ModelID) (ImageModel, bool) {
	if c == nil || c.registry == nil {
		return ImageModel{}, false
	}
	return c.registry.ImageModel(provider, id)
}

// ImageModels returns image models from the client registry.
func (c *Client) ImageModels() []ImageModel {
	if c == nil || c.registry == nil {
		return nil
	}
	return c.registry.ListImageModels()
}

// RefreshImageModels refreshes runtime image model sources on the client's registry.
func (c *Client) RefreshImageModels(ctx context.Context, providers ...ProviderID) error {
	if c == nil {
		c = NewClient()
	}
	if c.registry == nil {
		return registryError("registry is required")
	}
	return c.registry.RefreshImageModels(ctx, providers...)
}

// GetImageModel returns an image model from the default registry.
func GetImageModel(provider ProviderID, id ModelID) (ImageModel, bool) {
	return defaultClient().GetImageModel(provider, id)
}

// ImageModels returns image models from the default registry.
func ImageModels() []ImageModel {
	return defaultClient().ImageModels()
}

func (c *Client) imageRequestOptions(opts []ImageOption) Options {
	options := Options{
		HTTPClient: c.httpClient,
		Headers:    copyStringStringMap(c.defaultHeaders),
	}
	options = mergeOptions(options, c.defaultOptions)
	defaultCallbacks := options.ProviderAuthResolvers
	options.ProviderAuthResolvers = nil
	options = applyImageOptions(options, opts)
	requestCallbacks := options.ProviderAuthResolvers
	options.ProviderAuthResolvers = mergeProviderAuthResolvers(defaultCallbacks, requestCallbacks)
	clientResolver := c.clientAuthResolver()
	if options.AuthResolver != nil {
		clientResolver = options.AuthResolver
	}
	options.AuthResolver = ChainAuthResolver{
		Client:                   clientResolver,
		ProviderCallbacks:        requestCallbacks,
		DefaultProviderCallbacks: defaultCallbacks,
	}
	return options
}

func applyImageOptions(options Options, opts []ImageOption) Options {
	applied := cloneOptions(options)
	for _, opt := range opts {
		if opt != nil {
			opt(&applied)
		}
	}
	return applied
}

func imageOptionFromOption(opt Option) ImageOption {
	return func(options *Options) {
		if opt != nil {
			opt(options)
		}
	}
}

func validateImageOptions(model ImageModel, options Options) error {
	if options.Timeout != nil && *options.Timeout < 0 {
		return invalidImageOptionsError(model, "timeout must be non-negative")
	}
	if options.MaxRetries != nil && *options.MaxRetries < 0 {
		return invalidImageOptionsError(model, "max retries must be non-negative")
	}
	if options.MaxRetryDelay != nil && *options.MaxRetryDelay < 0 {
		return invalidImageOptionsError(model, "max retry delay must be non-negative")
	}
	return nil
}

func validateImageRequest(model ImageModel, req ImageRequest) error {
	switch req.Operation {
	case "", ImageOperationGenerate, ImageOperationEdit, ImageOperationVariation:
	default:
		return invalidImageRequestError(model, "unsupported image operation %q", req.Operation)
	}
	if req.Count < 0 {
		return invalidImageRequestError(model, "count must be non-negative")
	}
	if strings.TrimSpace(req.Prompt) == "" && len(req.Inputs) == 0 {
		return invalidImageRequestError(model, "prompt or inputs are required")
	}
	for index, input := range req.Inputs {
		if err := validateImageInput(input, false); err != nil {
			return invalidImageRequestError(model, "input %d: %v", index, err)
		}
	}
	if req.Mask != nil {
		if err := validateImageInput(*req.Mask, true); err != nil {
			return invalidImageRequestError(model, "mask: %v", err)
		}
	}
	return nil
}

func validateImageInput(input ImageInput, mask bool) error {
	switch input.Type {
	case ImageInputText:
		if mask {
			return errors.New("mask must be an image input")
		}
		if strings.TrimSpace(input.Text) == "" {
			return errors.New("text is required")
		}
		return nil
	case ImageInputImage:
		switch input.Source {
		case ImageSourceBase64:
			if input.MIMEType == "" {
				return errors.New("image MIME type is required")
			}
			if input.Data == "" {
				return errors.New("base64 image data is required")
			}
			if err := validateBase64(input.Data); err != nil {
				return errors.New("base64 image data is invalid")
			}
		case ImageSourceURL:
			if strings.TrimSpace(input.URL) == "" {
				return errors.New("image URL is required")
			}
		case ImageSourceFileID:
			if strings.TrimSpace(input.Data) == "" {
				return errors.New("image file id is required")
			}
		default:
			if input.Source == "" {
				return errors.New("image source is required")
			}
			return errors.New("unsupported image source")
		}
		return nil
	default:
		if input.Type == "" {
			return errors.New("input type is required")
		}
		return errors.New("unsupported input type")
	}
}

func invalidImageOptionsError(model ImageModel, message string) error {
	return &Error{
		Code:     ErrorInvalidOptions,
		Message:  message,
		Provider: model.Provider,
		Model:    model.ID,
	}
}

func invalidImageRequestError(model ImageModel, format string, args ...any) error {
	return &Error{
		Code:     ErrorInvalidRequest,
		Message:  fmt.Sprintf(format, args...),
		Provider: model.Provider,
		Model:    model.ID,
	}
}

func imageProviderNotFoundError(provider ProviderID, model ModelID) error {
	return &Error{
		Code:     ErrorProviderNotFound,
		Message:  "image provider is not registered",
		Provider: provider,
		Model:    model,
	}
}

func imageAbortedError(err error) error {
	return &Error{
		Code:    ErrorAborted,
		Message: err.Error(),
		Err:     err,
	}
}

func finalImages(model ImageModel, images AssistantImages, err error) AssistantImages {
	if images.Model == "" {
		images.Model = model.ID
	}
	if images.Provider == "" {
		images.Provider = model.Provider
	}
	if images.StopReason == "" {
		if err != nil {
			images.StopReason = StopReasonError
		} else {
			images.StopReason = StopReasonEndTurn
		}
	}
	return images
}
