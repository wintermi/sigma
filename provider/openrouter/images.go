// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openrouter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/wintermi/sigma"
)

const (
	DefaultBaseURL = "https://openrouter.ai/api/v1"

	providerOptionBaseURL      = "base_url"
	providerOptionBaseURLCamel = "baseURL"
	providerOptionEndpoint     = "endpoint"
	providerOptionExtraBody    = "extra_body"
	providerOptionExtraBodyGo  = "extraBody"
)

// ImagesProvider adapts OpenRouter image-capable chat completions to sigma.
type ImagesProvider struct {
	baseURL string
	client  *http.Client
	headers map[string]string
}

// ImagesProviderOption configures an ImagesProvider.
type ImagesProviderOption func(*ImagesProvider)

// NewImagesProvider constructs an OpenRouter image provider.
func NewImagesProvider(opts ...ImagesProviderOption) *ImagesProvider {
	provider := &ImagesProvider{baseURL: DefaultBaseURL}
	for _, opt := range opts {
		if opt != nil {
			opt(provider)
		}
	}
	return provider
}

// WithImagesBaseURL configures the image provider base URL, for example an
// httptest server URL ending in /api/v1.
func WithImagesBaseURL(baseURL string) ImagesProviderOption {
	return func(provider *ImagesProvider) {
		provider.baseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithImagesHTTPClient configures the image provider fallback HTTP client.
func WithImagesHTTPClient(client *http.Client) ImagesProviderOption {
	return func(provider *ImagesProvider) {
		provider.client = client
	}
}

// WithImagesHeader configures an image provider default request header.
func WithImagesHeader(key, value string) ImagesProviderOption {
	return WithImagesHeaders(map[string]string{key: value})
}

// WithImagesHeaders configures image provider default request headers.
func WithImagesHeaders(headers map[string]string) ImagesProviderOption {
	return func(provider *ImagesProvider) {
		if len(headers) == 0 {
			return
		}
		if provider.headers == nil {
			provider.headers = make(map[string]string, len(headers))
		}
		for key, value := range headers {
			provider.headers[key] = value
		}
	}
}

// RegisterImages adds an OpenRouter image provider to registry.
func RegisterImages(registry *sigma.Registry, opts ...ImagesProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterImageProvider(sigma.ProviderOpenRouter, NewImagesProvider(opts...))
}

// RegisterImagesDefault adds an OpenRouter image provider to sigma's default registry.
func RegisterImagesDefault(opts ...ImagesProviderOption) error {
	return sigma.RegisterDefaultImageProvider(sigma.ProviderOpenRouter, NewImagesProvider(opts...))
}

// API reports the OpenRouter image API surface.
func (p *ImagesProvider) API() sigma.ImageAPI {
	return sigma.ImageAPIOpenRouterImages
}

// Generate sends req to OpenRouter's non-streaming Chat Completions image path.
func (p *ImagesProvider) Generate(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (sigma.AssistantImages, error) {
	ctx, cancel := sigma.ContextWithRequestTimeout(ctx, opts)
	defer cancel()

	resp, err := sigma.DoHTTPWithRetry(
		ctx,
		p.httpClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return p.newRequest(ctx, model, req, opts)
		},
		func(resp *http.Response) *sigma.ProviderError {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return providerError(resp, model, body, nil)
		},
		sigma.ImageResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.ImageAPIOpenRouterImages, model.ID),
	)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return sigma.AssistantImages{StopReason: sigma.StopReasonAborted}, contextError(ctx, err)
		}
		return sigma.AssistantImages{StopReason: sigma.StopReasonError}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return sigma.AssistantImages{StopReason: sigma.StopReasonAborted}, contextError(ctx, err)
		}
		return sigma.AssistantImages{StopReason: sigma.StopReasonError}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return sigma.AssistantImages{StopReason: sigma.StopReasonError}, providerError(resp, model, respBody, nil)
	}

	return decodeResponse(respBody, model)
}

func (p *ImagesProvider) newRequest(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (*http.Request, error) {
	payload, err := payload(model, req, opts)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openrouter images: encode request: %w", err)
	}

	endpoint, err := p.endpoint(opts)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "sigma/openrouter-images")
	if err := addAuthHeader(ctx, httpReq, model, opts); err != nil {
		return nil, err
	}
	for key, value := range p.headers {
		httpReq.Header.Set(key, value)
	}
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	if err := sigma.RunImagePayloadDebugHooks(ctx, opts, model.Provider, sigma.ImageAPIOpenRouterImages, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

func payload(model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (map[string]any, error) {
	content, err := content(req)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"model": string(modelID(model, req)),
		"messages": []map[string]any{{
			"role":    "user",
			"content": content,
		}},
		"modalities": modalities(opts),
		"stream":     false,
	}
	if len(opts.Metadata) > 0 {
		payload["metadata"] = copyAnyMap(opts.Metadata)
	}
	if config := imageConfig(req, providerOptions(opts)); len(config) > 0 {
		payload["image_config"] = config
	}
	if routing := routing(opts); len(routing) > 0 {
		payload["provider"] = routing
	}
	for key, value := range extraBody(opts) {
		payload[key] = value
	}
	return payload, nil
}

func modelID(model sigma.ImageModel, req sigma.ImageRequest) sigma.ModelID {
	if req.Model != "" {
		return req.Model
	}
	return model.ID
}

func content(req sigma.ImageRequest) (any, error) {
	parts := make([]map[string]any, 0, len(req.Inputs)+1)
	if req.Prompt != "" {
		parts = append(parts, map[string]any{"type": "text", "text": req.Prompt})
	}
	for _, input := range req.Inputs {
		switch input.Type {
		case sigma.ImageInputText:
			if input.Text == "" {
				continue
			}
			parts = append(parts, map[string]any{"type": "text", "text": input.Text})
		case sigma.ImageInputImage:
			imageURL, err := inputImageURL(input)
			if err != nil {
				return nil, err
			}
			parts = append(parts, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": imageURL,
				},
			})
		default:
			return nil, fmt.Errorf("openrouter images: unsupported input type %q", input.Type)
		}
	}
	switch len(parts) {
	case 0:
		return nil, fmt.Errorf("openrouter images: prompt or input content is required")
	case 1:
		if text, ok := parts[0]["text"].(string); ok {
			return text, nil
		}
	}
	return parts, nil
}

func inputImageURL(input sigma.ImageInput) (string, error) {
	switch input.Source {
	case sigma.ImageSourceURL:
		if input.URL == "" {
			return "", fmt.Errorf("openrouter images: image URL is required")
		}
		return input.URL, nil
	case sigma.ImageSourceBase64:
		if input.Data == "" {
			return "", fmt.Errorf("openrouter images: image data is required")
		}
		if _, err := base64.StdEncoding.DecodeString(input.Data); err != nil {
			return "", fmt.Errorf("openrouter images: image data must be base64: %w", err)
		}
		mimeType := input.MIMEType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		return "data:" + mimeType + ";base64," + input.Data, nil
	default:
		return "", fmt.Errorf("openrouter images: unsupported image source %q", input.Source)
	}
}

func imageConfig(req sigma.ImageRequest, options map[string]any) map[string]any {
	config := make(map[string]any)
	if req.Size != "" {
		if aspectRatio := aspectRatioForSize(req.Size); aspectRatio != "" {
			config["aspect_ratio"] = aspectRatio
		} else {
			config["image_size"] = req.Size
		}
	}
	if req.Quality != "" {
		config["quality"] = req.Quality
	}
	if req.Count > 0 {
		config["count"] = req.Count
	}
	if value, ok := mapOption(options, "image_config"); ok {
		for key, item := range value {
			config[key] = item
		}
	}
	if value, ok := mapOption(options, "imageConfig"); ok {
		for key, item := range value {
			config[key] = item
		}
	}
	return config
}

func aspectRatioForSize(size string) string {
	switch size {
	case string(sigma.ImageSize1024x1024):
		return "1:1"
	case string(sigma.ImageSize1024x1536):
		return "2:3"
	case string(sigma.ImageSize1536x1024):
		return "3:2"
	default:
		return ""
	}
}

func modalities(opts sigma.Options) []string {
	options := providerOptions(opts)
	if values, ok := stringSliceOption(options, "modalities"); ok && len(values) > 0 {
		return values
	}
	return []string{"image", "text"}
}

func routing(opts sigma.Options) map[string]any {
	options := providerOptions(opts)
	if value, ok := mapOption(options, "provider"); ok {
		return value
	}
	if value, ok := mapOption(options, "routing"); ok {
		return value
	}
	return nil
}

func extraBody(opts sigma.Options) map[string]any {
	options := providerOptions(opts)
	if value, ok := mapOption(options, providerOptionExtraBody); ok {
		return value
	}
	if value, ok := mapOption(options, providerOptionExtraBodyGo); ok {
		return value
	}
	return nil
}

func providerOptions(opts sigma.Options) map[string]any {
	if len(opts.ProviderOptions) == 0 {
		return nil
	}
	if values := opts.ProviderOptions[sigma.ProviderOpenRouter]; len(values) > 0 {
		return values
	}
	return nil
}

func addAuthHeader(ctx context.Context, req *http.Request, model sigma.ImageModel, opts sigma.Options) error {
	if opts.AuthResolver == nil {
		return &sigma.Error{
			Code:     sigma.ErrorUnsupported,
			Message:  "openrouter images: auth resolver is required",
			Provider: model.Provider,
			Model:    model.ID,
		}
	}
	credential, err := opts.AuthResolver.Resolve(ctx, authModel(model), opts)
	if err != nil {
		return err
	}
	if credential.Value != "" {
		req.Header.Set("Authorization", "Bearer "+credential.Value)
	}
	return nil
}

func authModel(model sigma.ImageModel) sigma.Model {
	return sigma.Model{
		ID:               model.ID,
		Provider:         model.Provider,
		API:              sigma.API(model.API),
		Name:             model.Name,
		ProviderMetadata: copyAnyMap(model.ProviderMetadata),
	}
}

func (p *ImagesProvider) endpoint(opts sigma.Options) (string, error) {
	options := providerOptions(opts)
	if endpoint, ok := stringOption(options, providerOptionEndpoint); ok {
		return endpoint, nil
	}
	baseURL := p.baseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if value, ok := stringOption(options, providerOptionBaseURL); ok {
		baseURL = value
	} else if value, ok := stringOption(options, providerOptionBaseURLCamel); ok {
		baseURL = value
	}
	baseURL = strings.TrimRight(baseURL, "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("openrouter images: invalid base URL %q", baseURL)
	}
	return baseURL + "/chat/completions", nil
}

func (p *ImagesProvider) httpClient(opts sigma.Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	if p != nil && p.client != nil {
		return p.client
	}
	return http.DefaultClient
}

type chatResponse struct {
	ID       string         `json:"id"`
	Model    string         `json:"model"`
	Provider string         `json:"provider"`
	Choices  []imageChoice  `json:"choices"`
	Usage    *openAIUsage   `json:"usage"`
	Error    *responseError `json:"error"`
}

type imageChoice struct {
	FinishReason string       `json:"finish_reason"`
	Message      imageMessage `json:"message"`
}

type imageMessage struct {
	Content json.RawMessage `json:"content"`
	Images  []imagePart     `json:"images"`
}

type imagePart struct {
	Type     string `json:"type"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
	ImageURLCamel struct {
		URL string `json:"url"`
	} `json:"imageUrl"`
}

type responseError struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

type openAIUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens     int `json:"cached_tokens"`
		CacheWriteTokens int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func decodeResponse(body []byte, model sigma.ImageModel) (sigma.AssistantImages, error) {
	var decoded chatResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return sigma.AssistantImages{StopReason: sigma.StopReasonError}, fmt.Errorf("openrouter images: decode response: %w", err)
	}
	if decoded.Error != nil {
		images := sigma.AssistantImages{
			StopReason: sigma.StopReasonError,
			Errors: []sigma.ImageError{{
				Code:    fmt.Sprint(decoded.Error.Code),
				Message: decoded.Error.Message,
				ProviderMetadata: map[string]any{
					"type": decoded.Error.Type,
				},
			}},
		}
		return images, sigma.NewProviderError(model.Provider, sigma.API(sigma.ImageAPIOpenRouterImages), model.ID, http.StatusOK, "", 0, body, nil)
	}

	images := sigma.AssistantImages{
		ResponseID: decoded.ID,
		Model:      providerModel(decoded.Model, model.ID),
		Provider:   model.Provider,
		ProviderMetadata: map[string]any{
			"id": decoded.ID,
		},
	}
	if decoded.Provider != "" {
		images.ProviderMetadata["provider"] = decoded.Provider
	}
	if decoded.Model != "" {
		images.ProviderMetadata["model"] = decoded.Model
	}
	if len(decoded.Choices) > 0 {
		choice := decoded.Choices[0]
		images.StopReason = stopReason(choice.FinishReason)
		content, err := outputContent(choice.Message.Content)
		if err != nil {
			return sigma.AssistantImages{StopReason: sigma.StopReasonError}, err
		}
		images.Images = append(images.Images, content...)
		for _, image := range choice.Message.Images {
			output, err := outputImage(image)
			if err != nil {
				return sigma.AssistantImages{StopReason: sigma.StopReasonError}, err
			}
			images.Images = append(images.Images, output)
		}
	}
	if decoded.Usage != nil {
		usage := decoded.Usage.sigmaUsage()
		images.Usage = &usage
	}
	if images.StopReason == "" {
		images.StopReason = sigma.StopReasonEndTurn
	}
	return images, nil
}

func providerModel(providerModel string, fallback sigma.ModelID) sigma.ModelID {
	if providerModel == "" {
		return fallback
	}
	return sigma.ModelID(providerModel)
}

func outputContent(content json.RawMessage) ([]sigma.ImageInput, error) {
	if len(content) == 0 || string(content) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		if text == "" {
			return nil, nil
		}
		return []sigma.ImageInput{sigma.ImageText(text)}, nil
	}

	var parts []contentPart
	if err := json.Unmarshal(content, &parts); err != nil {
		return nil, fmt.Errorf("openrouter images: decode output content: %w", err)
	}
	out := make([]sigma.ImageInput, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text", "output_text":
			if part.Text != "" {
				out = append(out, sigma.ImageText(part.Text))
			}
		case "image_url":
			image, err := outputImage(part.imagePart())
			if err != nil {
				return nil, err
			}
			out = append(out, image)
		default:
			return nil, fmt.Errorf("openrouter images: unsupported output content type %q", part.Type)
		}
	}
	return out, nil
}

type contentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
	ImageURLCamel struct {
		URL string `json:"url"`
	} `json:"imageUrl"`
}

func (p contentPart) imagePart() imagePart {
	var out imagePart
	out.Type = p.Type
	out.ImageURL.URL = p.ImageURL.URL
	out.ImageURLCamel.URL = p.ImageURLCamel.URL
	return out
}

func outputImage(part imagePart) (sigma.ImageInput, error) {
	imageURL := part.ImageURL.URL
	if imageURL == "" {
		imageURL = part.ImageURLCamel.URL
	}
	if imageURL == "" {
		return sigma.ImageInput{}, fmt.Errorf("openrouter images: image response is missing URL")
	}
	mimeType, data, ok := strings.Cut(imageURL, ";base64,")
	if ok && strings.HasPrefix(mimeType, "data:") {
		return sigma.ImageOutputData(strings.TrimPrefix(mimeType, "data:"), data), nil
	}
	return sigma.ImageOutputURL("", imageURL), nil
}

func (u openAIUsage) sigmaUsage() sigma.Usage {
	return sigma.Usage{
		InputTokens:           max(0, u.PromptTokens-u.PromptTokensDetails.CachedTokens-u.PromptTokensDetails.CacheWriteTokens),
		OutputTokens:          u.CompletionTokens,
		TotalTokens:           u.TotalTokens,
		CacheReadInputTokens:  u.PromptTokensDetails.CachedTokens,
		CacheWriteInputTokens: u.PromptTokensDetails.CacheWriteTokens,
		ThinkingTokens:        u.CompletionTokensDetails.ReasoningTokens,
	}
}

func stopReason(reason string) sigma.StopReason {
	switch reason {
	case "", "stop":
		return sigma.StopReasonEndTurn
	case "length":
		return sigma.StopReasonMaxTokens
	case "content_filter":
		return sigma.StopReasonContentFilter
	case "tool_calls":
		return sigma.StopReasonToolCalls
	default:
		return sigma.StopReasonUnknown
	}
}

func providerError(resp *http.Response, model sigma.ImageModel, body []byte, err error) *sigma.ProviderError {
	return sigma.NewProviderError(
		model.Provider,
		sigma.API(sigma.ImageAPIOpenRouterImages),
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		err,
	)
}

func requestID(headers http.Header) string {
	for _, key := range []string{"x-request-id", "request-id", "openrouter-request-id"} {
		if value := headers.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func contextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func mapOption(options map[string]any, key string) (map[string]any, bool) {
	value, ok := options[key]
	if !ok {
		return nil, false
	}
	values, ok := value.(map[string]any)
	return values, ok
}

func stringOption(options map[string]any, key string) (string, bool) {
	value, ok := options[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok && text != ""
}

func stringSliceOption(options map[string]any, key string) ([]string, bool) {
	value, ok := options[key]
	if !ok {
		return nil, false
	}
	switch values := value.(type) {
	case []string:
		return values, true
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok || text == "" {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	default:
		return nil, false
	}
}

func copyAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
