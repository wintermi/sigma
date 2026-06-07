// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"bytes"
	"context"
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
	maxGoogleImagesResponseBytes = 64 << 20

	googleImageOptionAspectRatio        = "aspect_ratio"
	googleImageOptionAspectRatioGo      = "aspectRatio"
	googleImageOptionImageSize          = "image_size"
	googleImageOptionImageSizeGo        = "imageSize"
	googleImageOptionNegativePrompt     = "negative_prompt"
	googleImageOptionNegativePromptGo   = "negativePrompt"
	googleImageOptionPersonGeneration   = "person_generation"
	googleImageOptionPersonGenerationGo = "personGeneration"
	googleImageOptionSafetySetting      = "safety_setting"
	googleImageOptionSafetySettingGo    = "safetySetting"
	googleImageOptionAddWatermark       = "add_watermark"
	googleImageOptionAddWatermarkGo     = "addWatermark"
	googleImageOptionSampleImageSize    = "sample_image_size"
	googleImageOptionSampleImageSizeGo  = "sampleImageSize"
	googleImageOptionSeed               = "seed"
)

// ImagesProvider adapts Google's Imagen and Gemini image generation APIs to sigma.
type ImagesProvider struct {
	base *Provider
}

// NewImagesProvider constructs a Google images provider.
func NewImagesProvider(opts ...ProviderOption) *ImagesProvider {
	return &ImagesProvider{base: NewProvider(opts...)}
}

// RegisterImages adds a Google images provider to registry.
func RegisterImages(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterImageProvider(providerID, NewImagesProvider(opts...))
}

// RegisterImagesDefault adds a Google images provider to sigma's default registry.
func RegisterImagesDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	return sigma.RegisterDefaultImageProvider(providerID, NewImagesProvider(opts...))
}

// API reports the Google images API surface.
func (p *ImagesProvider) API() sigma.ImageAPI {
	return sigma.ImageAPIGoogleImages
}

// Generate sends req to Google's Imagen predict or Gemini generateContent image API.
func (p *ImagesProvider) Generate(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (sigma.AssistantImages, error) {
	ctx, cancel := sigma.ContextWithRequestTimeout(ctx, opts)
	defer cancel()

	resp, err := sigma.DoHTTPWithRetry(
		ctx,
		p.base.httpClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return p.newRequest(ctx, model, req, opts)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return googleImagesResponseError(resp, model)
		},
		sigma.ImageResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.ImageAPIGoogleImages, model.ID),
	)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonAborted}, contextError(ctx, err)
		}
		return sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonError}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGoogleImagesResponseBytes))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonAborted}, contextError(ctx, err)
		}
		return sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonError}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonError}, googleImagesProviderError(resp, model, body, nil)
	}
	if googleImagenModel(model.ID) {
		return decodeGoogleImagenResponse(body, model)
	}
	return decodeGoogleGeminiImageResponse(body, model)
}

func (p *ImagesProvider) newRequest(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (*http.Request, error) {
	body, err := googleImagesRequestBody(model, req, opts)
	if err != nil {
		return nil, err
	}
	endpoint, err := p.endpoint(model, opts)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "sigma/google-images")

	textModel := imageAuthModel(model, sigma.API(sigma.ImageAPIGoogleImages))
	for key, value := range p.base.headers {
		httpReq.Header.Set(key, value)
	}
	for key, value := range googleModelHeaders(textModel) {
		httpReq.Header.Set(key, value)
	}
	if err := p.base.addAuthHeader(ctx, httpReq, textModel, opts); err != nil {
		return nil, err
	}
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	if err := sigma.RunImagePayloadDebugHooks(ctx, opts, model.Provider, sigma.ImageAPIGoogleImages, model.ID, body, httpReq.Header); err != nil {
		return nil, fmt.Errorf("google images: payload debug hooks: %w", err)
	}
	return httpReq, nil
}

func googleImagesRequestBody(model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) ([]byte, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("google images: prompt is required")
	}
	if req.Operation != "" && req.Operation != sigma.ImageOperationGenerate {
		return nil, fmt.Errorf("google images: unsupported image operation %q", req.Operation)
	}
	if len(req.Inputs) > 0 || req.Mask != nil {
		return nil, fmt.Errorf("google images: image inputs are not supported")
	}
	var payload map[string]any
	if googleImagenModel(model.ID) {
		payload = googleImagenPayload(model, req, opts)
	} else {
		payload = googleGeminiImagePayload(model, req, opts)
	}
	for key, value := range req.ProviderMetadata {
		payload[key] = value
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("google images: encode request: %w", err)
	}
	return body, nil
}

func googleImagenPayload(model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) map[string]any {
	parameters := googleImagenParameters(model.Provider, req, opts)
	return map[string]any{
		"instances":  []map[string]any{{"prompt": req.Prompt}},
		"parameters": parameters,
	}
}

func googleGeminiImagePayload(model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) map[string]any {
	generationConfig := map[string]any{
		"responseModalities": []string{"TEXT", "IMAGE"},
	}
	if count := imageCount(req); count > 0 {
		generationConfig["numberOfImages"] = count
	}
	imageConfig := make(map[string]any)
	if aspect := imageAspectRatio(model.Provider, req, opts); aspect != "" {
		imageConfig["aspectRatio"] = aspect
	}
	options := providerOptions(opts, model.Provider)
	if value, ok := stringOption(options, googleImageOptionImageSize); ok {
		imageConfig["imageSize"] = value
	} else if value, ok := stringOption(options, googleImageOptionImageSizeGo); ok {
		imageConfig["imageSize"] = value
	}
	if len(imageConfig) > 0 {
		generationConfig["imageConfig"] = imageConfig
	}
	return map[string]any{
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]any{{"text": req.Prompt}},
		}},
		"generationConfig": generationConfig,
	}
}

func googleImagenParameters(provider sigma.ProviderID, req sigma.ImageRequest, opts sigma.Options) map[string]any {
	parameters := map[string]any{"sampleCount": imageCount(req)}
	if aspect := imageAspectRatio(provider, req, opts); aspect != "" {
		parameters["aspectRatio"] = aspect
	}
	options := providerOptions(opts, provider)
	copyStringImageOption(parameters, options, googleImageOptionNegativePrompt, googleImageOptionNegativePromptGo, "negativePrompt")
	copyStringImageOption(parameters, options, googleImageOptionPersonGeneration, googleImageOptionPersonGenerationGo, "personGeneration")
	copyStringImageOption(parameters, options, googleImageOptionSafetySetting, googleImageOptionSafetySettingGo, "safetySetting")
	copyStringImageOption(parameters, options, googleImageOptionSampleImageSize, googleImageOptionSampleImageSizeGo, "sampleImageSize")
	if value, ok := boolOption(options, googleImageOptionAddWatermark); ok {
		parameters["addWatermark"] = value
	} else if value, ok := boolOption(options, googleImageOptionAddWatermarkGo); ok {
		parameters["addWatermark"] = value
	}
	if value, ok := intOption(options, googleImageOptionSeed); ok {
		parameters["seed"] = value
	}
	return parameters
}

func copyStringImageOption(target map[string]any, options map[string]any, snake, camel, field string) {
	if value, ok := stringOption(options, snake); ok {
		target[field] = value
		return
	}
	if value, ok := stringOption(options, camel); ok {
		target[field] = value
	}
}

func imageCount(req sigma.ImageRequest) int {
	if req.Count > 0 {
		return req.Count
	}
	return 1
}

func imageAspectRatio(provider sigma.ProviderID, req sigma.ImageRequest, opts sigma.Options) string {
	options := providerOptions(opts, provider)
	if value, ok := stringOption(options, googleImageOptionAspectRatio); ok {
		return value
	}
	if value, ok := stringOption(options, googleImageOptionAspectRatioGo); ok {
		return value
	}
	size := strings.TrimSpace(req.Size)
	if strings.Contains(size, ":") {
		return size
	}
	switch strings.ToLower(size) {
	case "256x256", "512x512", "1024x1024", "2048x2048":
		return "1:1"
	default:
		return ""
	}
}

func (p *ImagesProvider) endpoint(model sigma.ImageModel, opts sigma.Options) (string, error) {
	options := providerOptions(opts, model.Provider)
	if endpoint, ok := stringOption(options, providerOptionEndpoint); ok {
		return endpoint, nil
	}
	baseURL := p.base.baseURLForProvider(model.Provider, opts)
	if value := modelMetadataBaseURL(model.ProviderMetadata); value != "" {
		baseURL = value
	}
	if value, ok := stringOption(options, providerOptionBaseURL); ok {
		baseURL = value
	} else if value, ok := stringOption(options, providerOptionBaseURLCamel); ok {
		baseURL = value
	}
	baseURL = strings.TrimRight(baseURL, "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("google images: invalid base URL %q", baseURL)
	}
	suffix := ":generateContent"
	if googleImagenModel(model.ID) {
		suffix = ":predict"
	}
	return baseURL + "/" + modelPath(model.ID) + suffix, nil
}

func googleImagenModel(modelID sigma.ModelID) bool {
	id := strings.ToLower(string(modelID))
	return strings.HasPrefix(id, "imagen-") || strings.Contains(id, "/imagen-")
}

type googleImagenResponse struct {
	Predictions []googleImagenPrediction `json:"predictions"`
}

type googleImagenPrediction struct {
	BytesBase64Encoded string `json:"bytesBase64Encoded"`
	MIMEType           string `json:"mimeType"`
	Prompt             string `json:"prompt"`
}

func decodeGoogleImagenResponse(body []byte, model sigma.ImageModel) (sigma.AssistantImages, error) {
	var decoded googleImagenResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonError}, fmt.Errorf("google images: decode response: %w", err)
	}
	out := sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonEndTurn}
	revisedPrompts := make([]string, 0, len(decoded.Predictions))
	for _, item := range decoded.Predictions {
		if item.BytesBase64Encoded == "" {
			continue
		}
		out.Images = append(out.Images, sigma.ImageOutputData(imageMIMEType(item.MIMEType), item.BytesBase64Encoded))
		if item.Prompt != "" {
			revisedPrompts = append(revisedPrompts, item.Prompt)
		}
	}
	if len(revisedPrompts) > 0 {
		out.ProviderMetadata = map[string]any{"revisedPrompts": revisedPrompts}
	}
	return out, nil
}

type googleGeminiImageResponse struct {
	Candidates []googleGeminiImageCandidate `json:"candidates"`
}

type googleGeminiImageCandidate struct {
	Content googleGeminiContent `json:"content"`
}

type googleGeminiContent struct {
	Parts []googleGeminiPart `json:"parts"`
}

type googleGeminiPart struct {
	Text       string                  `json:"text"`
	InlineData *googleGeminiInlineData `json:"inlineData"`
}

type googleGeminiInlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

func decodeGoogleGeminiImageResponse(body []byte, model sigma.ImageModel) (sigma.AssistantImages, error) {
	var decoded googleGeminiImageResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonError}, fmt.Errorf("google images: decode response: %w", err)
	}
	out := sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonEndTurn}
	for _, candidate := range decoded.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				out.Images = append(out.Images, sigma.ImageText(part.Text))
			}
			if part.InlineData != nil && part.InlineData.Data != "" {
				out.Images = append(out.Images, sigma.ImageOutputData(imageMIMEType(part.InlineData.MIMEType), part.InlineData.Data))
			}
		}
	}
	return out, nil
}

func imageMIMEType(value string) string {
	if value == "" {
		return "image/png"
	}
	return value
}

func googleImagesResponseError(resp *http.Response, model sigma.ImageModel) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return googleImagesProviderError(resp, model, body, contextOverflowCause(body))
}

func googleImagesProviderError(resp *http.Response, model sigma.ImageModel, body []byte, err error) *sigma.ProviderError {
	return sigma.NewProviderError(
		model.Provider,
		sigma.API(sigma.ImageAPIGoogleImages),
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		err,
	)
}

func imageAuthModel(model sigma.ImageModel, api sigma.API) sigma.Model {
	return sigma.Model{
		ID:               model.ID,
		Provider:         model.Provider,
		API:              api,
		Name:             model.Name,
		ProviderMetadata: model.ProviderMetadata,
	}
}
