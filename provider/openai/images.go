// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
)

const maxImagesResponseBytes = 64 << 20

// ImagesProvider adapts OpenAI's image generation API to sigma.
type ImagesProvider struct {
	base *Provider
}

// NewImagesProvider constructs an OpenAI Images API provider.
func NewImagesProvider(opts ...ProviderOption) *ImagesProvider {
	return &ImagesProvider{base: NewProvider(opts...)}
}

// RegisterImages adds an OpenAI Images API provider to registry.
func RegisterImages(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterImageProvider(providerID, NewImagesProvider(opts...))
}

// RegisterImagesDefault adds an OpenAI Images API provider to sigma's default registry.
func RegisterImagesDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	return sigma.RegisterDefaultImageProvider(providerID, NewImagesProvider(opts...))
}

// API reports the OpenAI Images API surface.
func (p *ImagesProvider) API() sigma.ImageAPI {
	return sigma.ImageAPIOpenAIImages
}

// Generate sends req to OpenAI's non-streaming image endpoint.
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
			return imagesResponseError(resp, model)
		},
		sigma.ImageResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.ImageAPIOpenAIImages, model.ID),
	)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return sigma.AssistantImages{StopReason: sigma.StopReasonAborted}, contextError(ctx, err)
		}
		return sigma.AssistantImages{StopReason: sigma.StopReasonError}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImagesResponseBytes))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return sigma.AssistantImages{StopReason: sigma.StopReasonAborted}, contextError(ctx, err)
		}
		return sigma.AssistantImages{StopReason: sigma.StopReasonError}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return sigma.AssistantImages{StopReason: sigma.StopReasonError}, imagesProviderError(resp, model, body, nil)
	}

	return decodeImagesResponse(body, model, req)
}

// StreamImages sends req to OpenAI's streaming image endpoint.
func (p *ImagesProvider) StreamImages(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) *sigma.ImageStream {
	ctx, cancel := sigma.ContextWithRequestTimeout(ctx, opts)
	stream, writer := sigma.NewImageStream(ctx)
	go func() {
		defer cancel()
		resp, err := sigma.DoHTTPWithRetry(
			ctx,
			p.base.httpClient(opts),
			opts,
			func(ctx context.Context) (*http.Request, error) {
				return p.newStreamRequest(ctx, model, req, opts)
			},
			func(resp *http.Response) *sigma.ProviderError {
				return imagesResponseError(resp, model)
			},
			sigma.ImageResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.ImageAPIOpenAIImages, model.ID),
		)
		if err != nil {
			final := sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonError}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				final.StopReason = sigma.StopReasonAborted
				_ = writer.Error(ctx, contextError(ctx, err), final)
				return
			}
			_ = writer.Error(ctx, err, final)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = writer.Error(ctx, imagesProviderError(resp, model, body, nil), sigma.AssistantImages{
				Model:      model.ID,
				Provider:   model.Provider,
				StopReason: sigma.StopReasonError,
			})
			return
		}
		final, err := parseImagesStream(ctx, resp.Body, writer, model, req)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				final.StopReason = sigma.StopReasonAborted
				_ = writer.Error(ctx, contextError(ctx, err), final)
				return
			}
			final.StopReason = sigma.StopReasonError
			_ = writer.Error(ctx, err, final)
			return
		}
		_ = writer.Done(ctx, final)
	}()
	return stream
}

func (p *ImagesProvider) newRequest(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (*http.Request, error) {
	return p.newRequestWithStream(ctx, model, req, opts, false)
}

func (p *ImagesProvider) newStreamRequest(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (*http.Request, error) {
	return p.newRequestWithStream(ctx, model, req, opts, true)
}

func (p *ImagesProvider) newRequestWithStream(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options, stream bool) (*http.Request, error) {
	body, contentType, err := imagesRequestBody(model, req, opts, stream)
	if err != nil {
		return nil, err
	}

	endpoint, err := p.endpoint(model, req, opts)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "sigma/openai-images")

	p.addProviderHeaders(httpReq, model.Provider, opts)
	for key, value := range p.base.headers {
		httpReq.Header.Set(key, value)
	}
	addImageModelHeaders(httpReq, model)
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	if err := p.addAuthHeader(ctx, httpReq, model, opts); err != nil {
		return nil, err
	}
	if err := sigma.RunImagePayloadDebugHooks(ctx, opts, model.Provider, sigma.ImageAPIOpenAIImages, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

func imagesRequestBody(model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options, stream bool) ([]byte, string, error) {
	operation := imageOperation(req)
	if stream && operation == sigma.ImageOperationVariation {
		return nil, "", fmt.Errorf("openai images: variations do not support streaming")
	}
	if operation == sigma.ImageOperationGenerate {
		payload, err := imagesPayload(model, req, opts, stream)
		if err != nil {
			return nil, "", err
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, "", fmt.Errorf("openai images: encode request: %w", err)
		}
		return body, "application/json", nil
	}
	if operation == sigma.ImageOperationEdit && referenceOnlyEditRequest(req) {
		payload, err := imagesEditPayload(model, req, opts, stream)
		if err != nil {
			return nil, "", err
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, "", fmt.Errorf("openai images: encode edit request: %w", err)
		}
		return body, "application/json", nil
	}
	return multipartImagesPayload(model, req, opts, operation, stream)
}

func imagesPayload(model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options, stream bool) (map[string]any, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("openai images: prompt is required")
	}
	if len(req.Inputs) > 0 {
		return nil, fmt.Errorf("openai images: image inputs require the edits endpoint")
	}

	payload := make(map[string]any)
	if err := addCommonImagePayloadFields(payload, model, req, opts, stream); err != nil {
		return nil, err
	}
	return payload, nil
}

func imagesEditPayload(model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options, stream bool) (map[string]any, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("openai images: prompt is required")
	}
	if len(req.Inputs) == 0 {
		return nil, fmt.Errorf("openai images: edit requires image inputs")
	}

	images := make([]map[string]any, 0, len(req.Inputs))
	for _, input := range req.Inputs {
		reference, err := imageReference(input)
		if err != nil {
			return nil, err
		}
		images = append(images, reference)
	}

	payload := map[string]any{"images": images}
	if req.Mask != nil {
		mask, err := imageReference(*req.Mask)
		if err != nil {
			return nil, err
		}
		payload["mask"] = mask
	}
	if err := addCommonImagePayloadFields(payload, model, req, opts, stream); err != nil {
		return nil, err
	}
	return payload, nil
}

func addCommonImagePayloadFields(payload map[string]any, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options, stream bool) error {
	payload["model"] = string(imageModelID(model, req))
	if req.Prompt != "" {
		payload["prompt"] = req.Prompt
	}
	if req.Count > 0 {
		payload["n"] = req.Count
	}
	if req.Size != "" {
		payload["size"] = req.Size
	}
	if req.Quality != "" {
		payload["quality"] = req.Quality
	}
	if req.MIMEType != "" {
		format, err := outputFormat(req.MIMEType)
		if err != nil {
			return err
		}
		payload["output_format"] = format
	}
	if stream {
		payload["stream"] = true
		addPartialImages(payload, opts, model.Provider)
	}
	for key, value := range extraBody(opts, model.Provider) {
		payload[key] = value
	}
	return nil
}

func referenceOnlyEditRequest(req sigma.ImageRequest) bool {
	if len(req.Inputs) == 0 {
		return false
	}
	for _, input := range req.Inputs {
		if !isImageReference(input) {
			return false
		}
	}
	return req.Mask == nil || isImageReference(*req.Mask)
}

func isImageReference(input sigma.ImageInput) bool {
	return input.Type == sigma.ImageInputImage && (input.Source == sigma.ImageSourceURL || input.Source == sigma.ImageSourceFileID)
}

func imageReference(input sigma.ImageInput) (map[string]any, error) {
	if input.Type != sigma.ImageInputImage {
		return nil, fmt.Errorf("openai images: unsupported image input type %q", input.Type)
	}
	switch input.Source {
	case sigma.ImageSourceURL:
		if input.URL == "" {
			return nil, fmt.Errorf("openai images: image input URL is required")
		}
		return map[string]any{"image_url": input.URL}, nil
	case sigma.ImageSourceFileID:
		if input.Data == "" {
			return nil, fmt.Errorf("openai images: image file ID is required")
		}
		return map[string]any{"file_id": input.Data}, nil
	default:
		return nil, fmt.Errorf("openai images: unsupported image input source %q", input.Source)
	}
}

func multipartImagesPayload(model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options, operation sigma.ImageOperation, stream bool) ([]byte, string, error) {
	if err := validateMultipartImageRequest(model, req, operation); err != nil {
		return nil, "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writeCommonImageFields(writer, model, req, opts, stream); err != nil {
		return nil, "", err
	}
	switch operation { //nolint:exhaustive
	case sigma.ImageOperationEdit:
		if err := writeEditImageFields(writer, req); err != nil {
			return nil, "", err
		}
	case sigma.ImageOperationVariation:
		if err := writeVariationImageFields(writer, req); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("openai images: encode multipart request: %w", err)
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func validateMultipartImageRequest(model sigma.ImageModel, req sigma.ImageRequest, operation sigma.ImageOperation) error {
	switch operation {
	case sigma.ImageOperationEdit:
		if strings.TrimSpace(req.Prompt) == "" {
			return fmt.Errorf("openai images: prompt is required")
		}
		if len(req.Inputs) == 0 {
			return fmt.Errorf("openai images: edit requires image inputs")
		}
	case sigma.ImageOperationVariation:
		if imageModelID(model, req) != "dall-e-2" {
			return fmt.Errorf("openai images: variations require dall-e-2")
		}
		if strings.TrimSpace(req.Prompt) != "" {
			return fmt.Errorf("openai images: variations do not support prompt")
		}
		if len(req.Inputs) != 1 {
			return fmt.Errorf("openai images: variations require exactly one image input")
		}
		input := req.Inputs[0]
		if input.Type != sigma.ImageInputImage || input.Source != sigma.ImageSourceBase64 {
			return fmt.Errorf("openai images: variations require one base64 PNG image input")
		}
		if strings.ToLower(strings.TrimSpace(input.MIMEType)) != "image/png" {
			return fmt.Errorf("openai images: variations require image/png input")
		}
	default:
		return fmt.Errorf("openai images: unsupported operation %q", operation)
	}
	return nil
}

func writeCommonImageFields(writer *multipart.Writer, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options, stream bool) error {
	fields := map[string]string{
		"model": string(imageModelID(model, req)),
	}
	if req.Prompt != "" {
		fields["prompt"] = req.Prompt
	}
	if req.Count > 0 {
		fields["n"] = fmt.Sprint(req.Count)
	}
	if req.Size != "" {
		fields["size"] = req.Size
	}
	if req.Quality != "" {
		fields["quality"] = req.Quality
	}
	if req.MIMEType != "" {
		format, err := outputFormat(req.MIMEType)
		if err != nil {
			return err
		}
		fields["output_format"] = format
	}
	if stream {
		fields["stream"] = "true"
		if partialImages, ok := partialImagesOption(opts, model.Provider); ok {
			fields["partial_images"] = fmt.Sprint(partialImages)
		}
	}
	for key, value := range extraBody(opts, model.Provider) {
		fields[key] = fmt.Sprint(value)
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return fmt.Errorf("openai images: write field %q: %w", key, err)
		}
	}
	return nil
}

func writeEditImageFields(writer *multipart.Writer, req sigma.ImageRequest) error {
	for index, input := range req.Inputs {
		if err := writeImageInputField(writer, "image", index, input); err != nil {
			return err
		}
	}
	if req.Mask != nil {
		if err := writeImageInputField(writer, "mask", 0, *req.Mask); err != nil {
			return err
		}
	}
	return nil
}

func writeVariationImageFields(writer *multipart.Writer, req sigma.ImageRequest) error {
	return writeImageInputField(writer, "image", 0, req.Inputs[0])
}

func writeImageInputField(writer *multipart.Writer, field string, index int, input sigma.ImageInput) error {
	switch input.Type {
	case sigma.ImageInputText:
		if err := writer.WriteField(field+"_text", input.Text); err != nil {
			return fmt.Errorf("openai images: write %s text field: %w", field, err)
		}
		return nil
	case sigma.ImageInputImage:
	default:
		return fmt.Errorf("openai images: unsupported image input type %q", input.Type)
	}
	switch input.Source {
	case sigma.ImageSourceBase64:
		data, err := base64.StdEncoding.DecodeString(input.Data)
		if err != nil {
			return fmt.Errorf("openai images: image input data must be base64: %w", err)
		}
		part, err := writer.CreateFormFile(field, imageInputFilename(field, index, input.MIMEType))
		if err != nil {
			return fmt.Errorf("openai images: create %s file field: %w", field, err)
		}
		if _, err := part.Write(data); err != nil {
			return fmt.Errorf("openai images: write %s file field: %w", field, err)
		}
	case sigma.ImageSourceURL:
		if input.URL == "" {
			return fmt.Errorf("openai images: image input URL is required")
		}
		if err := writer.WriteField(field+"_url", input.URL); err != nil {
			return fmt.Errorf("openai images: write %s URL field: %w", field, err)
		}
	case sigma.ImageSourceFileID:
		if input.Data == "" {
			return fmt.Errorf("openai images: image file ID is required")
		}
		if err := writer.WriteField(field+"_file_id", input.Data); err != nil {
			return fmt.Errorf("openai images: write %s file ID field: %w", field, err)
		}
	default:
		return fmt.Errorf("openai images: unsupported image input source %q", input.Source)
	}
	return nil
}

func imageInputFilename(field string, index int, mimeType string) string {
	extension := "png"
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		extension = "jpg"
	case "image/webp":
		extension = "webp"
	}
	return fmt.Sprintf("%s_%d.%s", field, index, extension)
}

func imageOperation(req sigma.ImageRequest) sigma.ImageOperation {
	if req.Operation != "" {
		return req.Operation
	}
	if len(req.Inputs) > 0 || req.Mask != nil {
		return sigma.ImageOperationEdit
	}
	return sigma.ImageOperationGenerate
}

func addPartialImages(payload map[string]any, opts sigma.Options, provider sigma.ProviderID) {
	if partialImages, ok := partialImagesOption(opts, provider); ok {
		payload["partial_images"] = partialImages
	}
}

func partialImagesOption(opts sigma.Options, provider sigma.ProviderID) (int, bool) {
	options := providerOptions(opts, provider)
	value, ok := options["partial_images"]
	if !ok {
		value, ok = options["partialImages"]
	}
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func imageModelID(model sigma.ImageModel, req sigma.ImageRequest) sigma.ModelID {
	if req.Model != "" {
		return req.Model
	}
	return model.ID
}

func outputFormat(mimeType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return "png", nil
	case "image/jpeg":
		return "jpeg", nil
	case "image/webp":
		return "webp", nil
	default:
		return "", fmt.Errorf("openai images: unsupported output MIME type %q", mimeType)
	}
}

func outputMIMEType(format string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	}
	if fallback != "" {
		return fallback
	}
	return "image/png"
}

func (p *ImagesProvider) addProviderHeaders(req *http.Request, provider sigma.ProviderID, opts sigma.Options) {
	options := providerOptions(opts, provider)
	if organization, ok := stringOption(options, providerOptionOrganization); ok {
		req.Header.Set("OpenAI-Organization", organization)
	}
	if project, ok := stringOption(options, providerOptionProject); ok {
		req.Header.Set("OpenAI-Project", project)
	}
}

func (p *ImagesProvider) addAuthHeader(ctx context.Context, req *http.Request, model sigma.ImageModel, opts sigma.Options) error {
	if opts.AuthResolver == nil {
		return &sigma.Error{
			Code:     sigma.ErrorUnsupported,
			Message:  "openai images: auth resolver is required",
			Provider: model.Provider,
			Model:    model.ID,
		}
	}
	credential, err := opts.AuthResolver.Resolve(ctx, imageAuthModel(model), opts)
	if err != nil {
		return err
	}
	if credential.Value != "" {
		req.Header.Set("Authorization", "Bearer "+credential.Value)
	}
	return nil
}

func imageAuthModel(model sigma.ImageModel) sigma.Model {
	return sigma.Model{
		ID:               model.ID,
		Provider:         model.Provider,
		API:              sigma.API(model.API),
		Name:             model.Name,
		ProviderMetadata: copyAnyMap(model.ProviderMetadata),
	}
}

func (p *ImagesProvider) endpoint(model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (string, error) {
	options := providerOptions(opts, model.Provider)
	if endpoint, ok := stringOption(options, providerOptionEndpoint); ok {
		if err := validateImagesEndpoint(endpoint); err != nil {
			return "", err
		}
		return endpoint, nil
	}

	baseURL := p.baseURLForModel(model, opts)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("openai images: invalid base URL %q", baseURL)
	}
	switch imageOperation(req) {
	case sigma.ImageOperationEdit:
		return baseURL + "/images/edits", nil
	case sigma.ImageOperationVariation:
		return baseURL + "/images/variations", nil
	default:
		return baseURL + "/images/generations", nil
	}
}

func validateImagesEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("openai images: invalid endpoint %q", endpoint)
	}
	return nil
}

func (p *ImagesProvider) baseURLForModel(model sigma.ImageModel, opts sigma.Options) string {
	baseURL := p.base.defaultBaseURL()
	if value, ok := model.ProviderMetadata[sigma.MetadataOpenAICompatibleBaseURL].(string); ok && strings.TrimSpace(value) != "" {
		baseURL = value
	} else if value, ok := model.ProviderMetadata["baseURL"].(string); ok && strings.TrimSpace(value) != "" {
		baseURL = value
	}
	options := providerOptions(opts, model.Provider)
	if value, ok := stringOption(options, providerOptionBaseURL); ok {
		baseURL = value
	} else if value, ok := stringOption(options, providerOptionBaseURLCamel); ok {
		baseURL = value
	}
	return strings.TrimRight(baseURL, "/")
}

func addImageModelHeaders(req *http.Request, model sigma.ImageModel) {
	for key, value := range imageModelHeaders(model) {
		if unsafeCredentialHeader(key) {
			continue
		}
		req.Header.Set(key, value)
	}
}

func imageModelHeaders(model sigma.ImageModel) map[string]string {
	raw := model.ProviderMetadata[sigma.MetadataOpenAICompatibleHeaders]
	if raw == nil {
		raw = model.ProviderMetadata["headers"]
	}
	switch headers := raw.(type) {
	case map[string]string:
		return headers
	case map[string]any:
		copied := make(map[string]string, len(headers))
		for key, value := range headers {
			text, ok := value.(string)
			if !ok {
				continue
			}
			copied[key] = text
		}
		return copied
	default:
		return nil
	}
}

type imagesResponse struct {
	Created      int64           `json:"created"`
	Background   string          `json:"background"`
	Data         []imageData     `json:"data"`
	OutputFormat string          `json:"output_format"`
	Quality      string          `json:"quality"`
	Size         string          `json:"size"`
	Usage        *imagesUsage    `json:"usage"`
	Error        *imagesAPIError `json:"error"`
}

type imageData struct {
	B64JSON       string `json:"b64_json"`
	URL           string `json:"url"`
	RevisedPrompt string `json:"revised_prompt"`
}

type imagesStreamEvent struct {
	Type              string          `json:"type"`
	B64JSON           string          `json:"b64_json"`
	PartialImageB64   string          `json:"partial_image_b64"`
	URL               string          `json:"url"`
	OutputIndex       *int            `json:"output_index"`
	PartialImageIndex *int            `json:"partial_image_index"`
	Data              []imageData     `json:"data"`
	Response          *imagesResponse `json:"response"`
	Usage             *imagesUsage    `json:"usage"`
	Error             *imagesAPIError `json:"error"`
}

type imagesAPIError struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

type imagesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails struct {
		ImageTokens int `json:"image_tokens"`
		TextTokens  int `json:"text_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails struct {
		ImageTokens int `json:"image_tokens"`
		TextTokens  int `json:"text_tokens"`
	} `json:"output_tokens_details"`
}

func decodeImagesResponse(body []byte, model sigma.ImageModel, req sigma.ImageRequest) (sigma.AssistantImages, error) {
	var decoded imagesResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return sigma.AssistantImages{StopReason: sigma.StopReasonError}, fmt.Errorf("openai images: decode response: %w", err)
	}
	if decoded.Error != nil {
		images := sigma.AssistantImages{
			StopReason: sigma.StopReasonError,
			Errors: []sigma.ImageError{{
				Code:    fmt.Sprint(decoded.Error.Code),
				Message: decoded.Error.Message,
				ProviderMetadata: map[string]any{
					providerToolOptionTypeKey: decoded.Error.Type,
				},
			}},
		}
		return images, sigma.NewProviderError(model.Provider, sigma.API(sigma.ImageAPIOpenAIImages), model.ID, http.StatusOK, "", 0, body, nil)
	}

	mimeType := outputMIMEType(decoded.OutputFormat, req.MIMEType)
	images := sigma.AssistantImages{
		Model:            model.ID,
		Provider:         model.Provider,
		StopReason:       sigma.StopReasonEndTurn,
		ProviderMetadata: imagesProviderMetadata(decoded),
	}
	for _, item := range decoded.Data {
		if item.B64JSON != "" {
			images.Images = append(images.Images, sigma.ImageOutputData(mimeType, item.B64JSON))
		}
		if item.URL != "" {
			images.Images = append(images.Images, sigma.ImageOutputURL("", item.URL))
		}
	}
	if decoded.Usage != nil {
		usage := decoded.Usage.sigmaUsage()
		images.Usage = &usage
	}
	return images, nil
}

func parseImagesStream(ctx context.Context, body io.Reader, writer sigma.ImageStreamWriter, model sigma.ImageModel, req sigma.ImageRequest) (sigma.AssistantImages, error) {
	final := sigma.AssistantImages{
		Model:      model.ID,
		Provider:   model.Provider,
		StopReason: sigma.StopReasonEndTurn,
	}
	started := false
	emitStart := func() error {
		if started {
			return nil
		}
		started = true
		return writer.Emit(ctx, sigma.ImageEvent{Kind: sigma.ImageEventKindStart})
	}
	err := sse.Parse(ctx, body, func(event sse.Event) error {
		if event.Done {
			return sse.ErrStop
		}
		var decoded imagesStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &decoded); err != nil {
			return fmt.Errorf("openai images: decode stream event: %w", err)
		}
		if decoded.Error != nil {
			body, _ := json.Marshal(map[string]any{"error": decoded.Error})
			return sigma.NewProviderError(model.Provider, sigma.API(sigma.ImageAPIOpenAIImages), model.ID, 0, "", 0, body, sigma.ErrProviderResponse)
		}
		if decoded.Response != nil {
			response, err := decoded.Response.assistantImages(model, req)
			if err != nil {
				return err
			}
			final = response
		}
		if decoded.Usage != nil {
			usage := decoded.Usage.sigmaUsage()
			final.Usage = &usage
		}
		images := decoded.outputImages(req)
		if len(images) == 0 {
			return nil
		}
		if err := emitStart(); err != nil {
			return err
		}
		for index := range images {
			image := images[index]
			sequence := index
			if decoded.PartialImageIndex != nil {
				sequence = *decoded.PartialImageIndex
			} else if decoded.OutputIndex != nil {
				sequence = *decoded.OutputIndex
			}
			kind := sigma.ImageEventKindImage
			if strings.Contains(decoded.Type, "partial") {
				kind = sigma.ImageEventKindPartial
			} else {
				final.Images = append(final.Images, image)
			}
			event := sigma.ImageEvent{Kind: kind, SequenceIndex: &sequence}
			if kind == sigma.ImageEventKindPartial {
				event.PartialImage = &image
			} else {
				event.Image = &image
			}
			if err := writer.Emit(ctx, event); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, sse.ErrStop) {
		err = nil
	}
	return final, err
}

func (event imagesStreamEvent) outputImages(req sigma.ImageRequest) []sigma.ImageInput {
	if len(event.Data) > 0 {
		mimeType := outputMIMEType("", req.MIMEType)
		images := make([]sigma.ImageInput, 0, len(event.Data))
		for _, item := range event.Data {
			if item.B64JSON != "" {
				images = append(images, sigma.ImageOutputData(mimeType, item.B64JSON))
			}
			if item.URL != "" {
				images = append(images, sigma.ImageOutputURL("", item.URL))
			}
		}
		return images
	}
	mimeType := outputMIMEType("", req.MIMEType)
	b64 := firstNonEmpty(event.B64JSON, event.PartialImageB64)
	if b64 != "" {
		return []sigma.ImageInput{sigma.ImageOutputData(mimeType, b64)}
	}
	if event.URL != "" {
		return []sigma.ImageInput{sigma.ImageOutputURL("", event.URL)}
	}
	return nil
}

func (decoded imagesResponse) assistantImages(model sigma.ImageModel, req sigma.ImageRequest) (sigma.AssistantImages, error) {
	body, err := json.Marshal(decoded)
	if err != nil {
		return sigma.AssistantImages{}, err
	}
	return decodeImagesResponse(body, model, req)
}

func imagesProviderMetadata(decoded imagesResponse) map[string]any {
	metadata := make(map[string]any)
	if decoded.Created != 0 {
		metadata["created"] = decoded.Created
	}
	if decoded.Background != "" {
		metadata["background"] = decoded.Background
	}
	if decoded.OutputFormat != "" {
		metadata["output_format"] = decoded.OutputFormat
	}
	if decoded.Quality != "" {
		metadata["quality"] = decoded.Quality
	}
	if decoded.Size != "" {
		metadata["size"] = decoded.Size
	}
	var revisedPrompts []string
	for _, item := range decoded.Data {
		if item.RevisedPrompt != "" {
			revisedPrompts = append(revisedPrompts, item.RevisedPrompt)
		}
	}
	if len(revisedPrompts) == 1 {
		metadata["revised_prompt"] = revisedPrompts[0]
	} else if len(revisedPrompts) > 1 {
		metadata["revised_prompts"] = revisedPrompts
	}
	if decoded.Usage != nil {
		metadata["usage"] = decoded.Usage.providerMetadata()
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func (u imagesUsage) sigmaUsage() sigma.Usage {
	return sigma.Usage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  u.TotalTokens,
	}
}

func (u imagesUsage) providerMetadata() map[string]any {
	return map[string]any{
		"input_tokens_details": map[string]any{
			"image_tokens": u.InputTokensDetails.ImageTokens,
			"text_tokens":  u.InputTokensDetails.TextTokens,
		},
		"output_tokens_details": map[string]any{
			"image_tokens": u.OutputTokensDetails.ImageTokens,
			"text_tokens":  u.OutputTokensDetails.TextTokens,
		},
	}
}

func imagesResponseError(resp *http.Response, model sigma.ImageModel) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return imagesProviderError(resp, model, body, nil)
}

func imagesProviderError(resp *http.Response, model sigma.ImageModel, body []byte, err error) *sigma.ProviderError {
	return sigma.NewProviderError(
		model.Provider,
		sigma.API(sigma.ImageAPIOpenAIImages),
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		err,
	)
}
