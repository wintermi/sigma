// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wintermi/sigma"
)

// VertexImagesProvider adapts Vertex AI's Imagen predict API to sigma.
type VertexImagesProvider struct {
	base *VertexProvider
}

// NewVertexImagesProvider constructs a Vertex AI images provider.
func NewVertexImagesProvider(opts ...VertexProviderOption) *VertexImagesProvider {
	return &VertexImagesProvider{base: NewVertexProvider(opts...)}
}

// RegisterVertexImages adds a Vertex AI images provider to registry.
func RegisterVertexImages(registry *sigma.Registry, providerID sigma.ProviderID, opts ...VertexProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterImageProvider(providerID, NewVertexImagesProvider(opts...))
}

// RegisterVertexImagesDefault adds a Vertex AI images provider to sigma's default registry.
func RegisterVertexImagesDefault(providerID sigma.ProviderID, opts ...VertexProviderOption) error {
	return sigma.RegisterDefaultImageProvider(providerID, NewVertexImagesProvider(opts...))
}

// API reports the Vertex AI images API surface.
func (p *VertexImagesProvider) API() sigma.ImageAPI {
	return sigma.ImageAPIGoogleVertexImages
}

// Generate sends req to Vertex AI's models/{model}:predict Imagen endpoint.
func (p *VertexImagesProvider) Generate(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (sigma.AssistantImages, error) {
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
			return vertexImagesResponseError(resp, model)
		},
		sigma.ImageResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.ImageAPIGoogleVertexImages, model.ID),
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
		return sigma.AssistantImages{Model: model.ID, Provider: model.Provider, StopReason: sigma.StopReasonError}, vertexImagesProviderError(resp, model, body, nil)
	}
	return decodeGoogleImagenResponse(body, model)
}

func (p *VertexImagesProvider) newRequest(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (*http.Request, error) {
	if !googleImagenModel(model.ID) {
		return nil, fmt.Errorf("google vertex images: unsupported image model %q", model.ID)
	}
	body, err := googleImagesRequestBody(model, req, opts)
	if err != nil {
		return nil, err
	}
	textModel := imageAuthModel(model, sigma.APIGoogleVertex)
	config, err := p.base.requestConfig(textModel, opts)
	if err != nil {
		return nil, err
	}
	endpoint, err := p.endpoint(textModel, opts, config)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "sigma/google-vertex-images")

	for key, value := range p.base.headers {
		httpReq.Header.Set(key, value)
	}
	for key, value := range googleModelHeaders(textModel) {
		httpReq.Header.Set(key, value)
	}
	if err := p.base.addAuthHeader(ctx, httpReq, textModel, opts, config); err != nil {
		return nil, err
	}
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	if err := sigma.RunImagePayloadDebugHooks(ctx, opts, model.Provider, sigma.ImageAPIGoogleVertexImages, model.ID, body, httpReq.Header); err != nil {
		return nil, fmt.Errorf("google vertex images: payload debug hooks: %w", err)
	}
	return httpReq, nil
}

func (p *VertexImagesProvider) endpoint(model sigma.Model, opts sigma.Options, config vertexRequestConfig) (string, error) {
	options := providerOptions(opts, model.Provider)
	if endpoint, ok := stringOption(options, providerOptionEndpoint); ok {
		return endpoint, nil
	}
	if strings.TrimSpace(config.ProjectID) == "" {
		return "", vertexInvalidOptions(model, "google vertex: project ID is required", nil)
	}
	if strings.TrimSpace(config.Location) == "" {
		return "", vertexInvalidOptions(model, "google vertex: location is required", nil)
	}
	baseURL, err := vertexBaseURL(config)
	if err != nil {
		return "", vertexInvalidOptions(model, err.Error(), err)
	}
	return baseURL + "/" + vertexModelResource(model.ID, config) + ":predict", nil
}

func vertexImagesResponseError(resp *http.Response, model sigma.ImageModel) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return vertexImagesProviderError(resp, model, body, contextOverflowCause(body))
}

func vertexImagesProviderError(resp *http.Response, model sigma.ImageModel, body []byte, err error) *sigma.ProviderError {
	return sigma.NewProviderError(
		model.Provider,
		sigma.API(sigma.ImageAPIGoogleVertexImages),
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		err,
	)
}
