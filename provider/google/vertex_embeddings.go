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
	"strings"

	"github.com/wintermi/sigma"
)

const (
	vertexEmbeddingOptionTitle          = "title"
	vertexEmbeddingOptionAutoTruncate   = "auto_truncate"
	vertexEmbeddingOptionAutoTruncateGo = "autoTruncate"
)

// VertexEmbeddingsProvider adapts Vertex AI's native embeddings predict API to sigma.
type VertexEmbeddingsProvider struct {
	base *VertexProvider
}

// NewVertexEmbeddingsProvider constructs a Vertex AI embeddings provider.
func NewVertexEmbeddingsProvider(opts ...VertexProviderOption) *VertexEmbeddingsProvider {
	return &VertexEmbeddingsProvider{base: NewVertexProvider(opts...)}
}

// RegisterVertexEmbeddings adds a Vertex AI embeddings provider to registry.
func RegisterVertexEmbeddings(registry *sigma.Registry, providerID sigma.ProviderID, opts ...VertexProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	if err := registry.RegisterEmbeddingProvider(providerID, NewVertexEmbeddingsProvider(opts...)); err != nil {
		return fmt.Errorf("google vertex embeddings: register provider: %w", err)
	}
	return nil
}

// RegisterVertexEmbeddingsDefault adds a Vertex AI embeddings provider to sigma's default registry.
func RegisterVertexEmbeddingsDefault(providerID sigma.ProviderID, opts ...VertexProviderOption) error {
	if err := sigma.RegisterDefaultEmbeddingProvider(providerID, NewVertexEmbeddingsProvider(opts...)); err != nil {
		return fmt.Errorf("google vertex embeddings: register default provider: %w", err)
	}
	return nil
}

// API reports the Vertex AI embeddings API surface.
func (p *VertexEmbeddingsProvider) API() sigma.EmbeddingAPI {
	return sigma.EmbeddingAPIGoogleVertexEmbeddings
}

// Embed sends req to Vertex AI's models/{model}:predict embeddings endpoint.
func (p *VertexEmbeddingsProvider) Embed(ctx context.Context, model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) (sigma.Embeddings, error) {
	ctx, cancel := sigma.ContextWithRequestTimeout(ctx, opts)
	defer cancel()

	resp, attempts, err := sigma.DoHTTPWithRetryAttempts(
		ctx,
		p.base.httpClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return p.newRequest(ctx, model, req, opts)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return vertexEmbeddingsResponseError(resp, model)
		},
		sigma.EmbeddingResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.EmbeddingAPIGoogleVertexEmbeddings, model.ID),
	)
	embeddingAttempts := googleEmbeddingAttemptsFromHTTP(model, attempts, sigma.EmbeddingAPIGoogleVertexEmbeddings)
	if err != nil {
		response := sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return response, contextError(ctx, err)
		}
		return response, fmt.Errorf("google vertex embeddings: request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		response := sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return response, contextError(ctx, err)
		}
		return response, fmt.Errorf("google vertex embeddings: read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}, vertexEmbeddingsProviderError(resp, model, body, nil)
	}
	embeddings, err := decodeVertexEmbeddingsResponse(body, model, len(req.Inputs))
	embeddings.Attempts = embeddingAttempts
	return embeddings, err
}

func (p *VertexEmbeddingsProvider) newRequest(ctx context.Context, model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) (*http.Request, error) {
	payload := vertexEmbeddingsPayload(model, req, opts)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("google vertex embeddings: encode request: %w", err)
	}

	textModel := embeddingAuthModel(model, sigma.APIGoogleVertex)
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
	httpReq.Header.Set("User-Agent", "sigma/google-vertex-embeddings")

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
	sigma.ApplySuppressedHeaders(httpReq.Header, opts)
	if err := sigma.RunEmbeddingPayloadDebugHooks(ctx, opts, model.Provider, sigma.EmbeddingAPIGoogleVertexEmbeddings, model.ID, body, httpReq.Header); err != nil {
		return nil, fmt.Errorf("google vertex embeddings: payload debug hooks: %w", err)
	}
	return httpReq, nil
}

func vertexEmbeddingsPayload(model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) map[string]any {
	options := providerOptions(opts, model.Provider)
	taskType := googleEmbeddingTaskType(req, opts, model.Provider)
	title, _ := stringOption(options, vertexEmbeddingOptionTitle)

	instances := make([]map[string]any, 0, len(req.Inputs))
	for _, input := range req.Inputs {
		instance := map[string]any{"content": input}
		if taskType != "" {
			instance["task_type"] = taskType
		}
		if title != "" {
			instance["title"] = title
		}
		instances = append(instances, instance)
	}

	payload := map[string]any{"instances": instances}
	parameters := make(map[string]any)
	if dimensions := googleEmbeddingOutputDimensionality(req, opts, model.Provider); dimensions > 0 {
		parameters["outputDimensionality"] = dimensions
	}
	if value, ok := boolOption(options, vertexEmbeddingOptionAutoTruncate); ok {
		parameters["autoTruncate"] = value
	} else if value, ok := boolOption(options, vertexEmbeddingOptionAutoTruncateGo); ok {
		parameters["autoTruncate"] = value
	}
	if len(parameters) > 0 {
		payload["parameters"] = parameters
	}
	for key, value := range req.ProviderMetadata {
		payload[key] = value
	}
	return payload
}

func (p *VertexEmbeddingsProvider) endpoint(model sigma.Model, opts sigma.Options, config vertexRequestConfig) (string, error) {
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

type vertexEmbeddingsResponse struct {
	Predictions []vertexEmbeddingPrediction `json:"predictions"`
}

type vertexEmbeddingPrediction struct {
	Embeddings vertexEmbeddingValues `json:"embeddings"`
}

type vertexEmbeddingValues struct {
	Values     []float32                 `json:"values"`
	Statistics vertexEmbeddingStatistics `json:"statistics"`
}

type vertexEmbeddingStatistics struct {
	TokenCount int `json:"token_count"`
}

func decodeVertexEmbeddingsResponse(body []byte, model sigma.EmbeddingModel, inputCount int) (sigma.Embeddings, error) {
	var decoded vertexEmbeddingsResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return sigma.Embeddings{}, fmt.Errorf("google vertex embeddings: decode response: %w", err)
	}
	if len(decoded.Predictions) != inputCount {
		return sigma.Embeddings{}, fmt.Errorf("google vertex embeddings: provider returned %d vectors for %d inputs", len(decoded.Predictions), inputCount)
	}
	out := sigma.Embeddings{
		Model:    model.ID,
		Provider: model.Provider,
		Vectors:  make([]sigma.Embedding, 0, len(decoded.Predictions)),
	}
	totalTokens := 0
	for index, item := range decoded.Predictions {
		out.Vectors = append(out.Vectors, sigma.Embedding{
			Index:  index,
			Vector: append([]float32(nil), item.Embeddings.Values...),
		})
		totalTokens += item.Embeddings.Statistics.TokenCount
	}
	if totalTokens > 0 {
		usage := sigma.Usage{InputTokens: totalTokens, TotalTokens: totalTokens}
		out.Usage = &usage
		cost := sigma.CostForEmbeddingUsage(model, usage)
		out.Cost = &cost
	}
	return out, nil
}

func vertexEmbeddingsResponseError(resp *http.Response, model sigma.EmbeddingModel) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return vertexEmbeddingsProviderError(resp, model, body, contextOverflowCause(body))
}

func vertexEmbeddingsProviderError(resp *http.Response, model sigma.EmbeddingModel, body []byte, err error) *sigma.ProviderError {
	return sigma.NewProviderError(
		model.Provider,
		sigma.API(sigma.EmbeddingAPIGoogleVertexEmbeddings),
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		err,
	)
}
