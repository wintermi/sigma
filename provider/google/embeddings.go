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
	googleEmbeddingOptionTaskType               = "task_type"
	googleEmbeddingOptionTaskTypeGo             = "taskType"
	googleEmbeddingOptionOutputDimensionality   = "output_dimensionality"
	googleEmbeddingOptionOutputDimensionalityGo = "outputDimensionality"
)

// EmbeddingsProvider adapts Google's Gemini embeddings API to sigma.
type EmbeddingsProvider struct {
	base *Provider
}

// NewEmbeddingsProvider constructs a Google embeddings provider.
func NewEmbeddingsProvider(opts ...ProviderOption) *EmbeddingsProvider {
	return &EmbeddingsProvider{base: NewProvider(opts...)}
}

// RegisterEmbeddings adds a Google embeddings provider to registry.
func RegisterEmbeddings(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	if err := registry.RegisterEmbeddingProvider(providerID, NewEmbeddingsProvider(opts...)); err != nil {
		return fmt.Errorf("google embeddings: register provider: %w", err)
	}
	return nil
}

// RegisterEmbeddingsDefault adds a Google embeddings provider to sigma's default registry.
func RegisterEmbeddingsDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	if err := sigma.RegisterDefaultEmbeddingProvider(providerID, NewEmbeddingsProvider(opts...)); err != nil {
		return fmt.Errorf("google embeddings: register default provider: %w", err)
	}
	return nil
}

// API reports the Google embeddings API surface.
func (p *EmbeddingsProvider) API() sigma.EmbeddingAPI {
	return sigma.EmbeddingAPIGoogleEmbeddings
}

// Embed sends req to Google's batchEmbedContents endpoint.
func (p *EmbeddingsProvider) Embed(ctx context.Context, model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) (sigma.Embeddings, error) {
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
			return googleEmbeddingsResponseError(resp, model)
		},
		sigma.EmbeddingResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.EmbeddingAPIGoogleEmbeddings, model.ID),
	)
	embeddingAttempts := googleEmbeddingAttemptsFromHTTP(model, attempts, sigma.EmbeddingAPIGoogleEmbeddings)
	if err != nil {
		response := sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return response, contextError(ctx, err)
		}
		return response, fmt.Errorf("google embeddings: request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		response := sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return response, contextError(ctx, err)
		}
		return response, fmt.Errorf("google embeddings: read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}, googleEmbeddingsProviderError(resp, model, body, nil)
	}
	embeddings, err := decodeGoogleEmbeddingsResponse(body, model, len(req.Inputs))
	embeddings.Attempts = embeddingAttempts
	return embeddings, err
}

func (p *EmbeddingsProvider) newRequest(ctx context.Context, model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) (*http.Request, error) {
	payload := googleEmbeddingsPayload(model, req, opts)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("google embeddings: encode request: %w", err)
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
	httpReq.Header.Set("User-Agent", "sigma/google-embeddings")

	textModel := embeddingAuthModel(model, sigma.API(sigma.EmbeddingAPIGoogleEmbeddings))
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
	sigma.ApplySuppressedHeaders(httpReq.Header, opts)
	if err := sigma.RunEmbeddingPayloadDebugHooks(ctx, opts, model.Provider, sigma.EmbeddingAPIGoogleEmbeddings, model.ID, body, httpReq.Header); err != nil {
		return nil, fmt.Errorf("google embeddings: payload debug hooks: %w", err)
	}
	return httpReq, nil
}

func googleEmbeddingsPayload(model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) map[string]any {
	modelPath := modelPath(model.ID)
	requests := make([]map[string]any, 0, len(req.Inputs))
	taskType := googleEmbeddingTaskType(req, opts, model.Provider)
	dimensions := googleEmbeddingOutputDimensionality(req, opts, model.Provider)
	for _, input := range req.Inputs {
		item := map[string]any{
			"model": modelPath,
			"content": map[string]any{
				"parts": []map[string]any{{"text": input}},
			},
		}
		if taskType != "" {
			item["taskType"] = taskType
		}
		if dimensions > 0 {
			item["outputDimensionality"] = dimensions
		}
		requests = append(requests, item)
	}
	payload := map[string]any{"requests": requests}
	for key, value := range req.ProviderMetadata {
		payload[key] = value
	}
	return payload
}

func googleEmbeddingTaskType(req sigma.EmbeddingRequest, opts sigma.Options, provider sigma.ProviderID) string {
	options := providerOptions(opts, provider)
	if value, ok := stringOption(options, googleEmbeddingOptionTaskType); ok {
		return value
	}
	if value, ok := stringOption(options, googleEmbeddingOptionTaskTypeGo); ok {
		return value
	}
	switch req.InputType {
	case sigma.EmbeddingInputTypeQuery:
		return "RETRIEVAL_QUERY"
	case sigma.EmbeddingInputTypeDocument:
		return "RETRIEVAL_DOCUMENT"
	default:
		return ""
	}
}

func googleEmbeddingOutputDimensionality(req sigma.EmbeddingRequest, opts sigma.Options, provider sigma.ProviderID) int {
	options := providerOptions(opts, provider)
	if value, ok := intOption(options, googleEmbeddingOptionOutputDimensionality); ok {
		return value
	}
	if value, ok := intOption(options, googleEmbeddingOptionOutputDimensionalityGo); ok {
		return value
	}
	return req.Dimensions
}

func (p *EmbeddingsProvider) endpoint(model sigma.EmbeddingModel, opts sigma.Options) (string, error) {
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
		return "", fmt.Errorf("google embeddings: invalid base URL %q", baseURL)
	}
	return baseURL + "/" + modelPath(model.ID) + ":batchEmbedContents", nil
}

type googleEmbeddingsResponse struct {
	Embeddings []googleEmbeddingValue `json:"embeddings"`
}

type googleEmbeddingValue struct {
	Values []float32 `json:"values"`
}

func decodeGoogleEmbeddingsResponse(body []byte, model sigma.EmbeddingModel, inputCount int) (sigma.Embeddings, error) {
	var decoded googleEmbeddingsResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return sigma.Embeddings{}, fmt.Errorf("google embeddings: decode response: %w", err)
	}
	if len(decoded.Embeddings) != inputCount {
		return sigma.Embeddings{}, fmt.Errorf("google embeddings: provider returned %d vectors for %d inputs", len(decoded.Embeddings), inputCount)
	}
	out := sigma.Embeddings{
		Model:    model.ID,
		Provider: model.Provider,
		Vectors:  make([]sigma.Embedding, 0, len(decoded.Embeddings)),
	}
	for index, item := range decoded.Embeddings {
		out.Vectors = append(out.Vectors, sigma.Embedding{
			Index:  index,
			Vector: append([]float32(nil), item.Values...),
		})
	}
	return out, nil
}

func googleEmbeddingsResponseError(resp *http.Response, model sigma.EmbeddingModel) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return googleEmbeddingsProviderError(resp, model, body, contextOverflowCause(body))
}

func googleEmbeddingsProviderError(resp *http.Response, model sigma.EmbeddingModel, body []byte, err error) *sigma.ProviderError {
	return sigma.NewProviderError(
		model.Provider,
		sigma.API(sigma.EmbeddingAPIGoogleEmbeddings),
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		err,
	)
}

func googleEmbeddingAttemptsFromHTTP(model sigma.EmbeddingModel, attempts []sigma.HTTPAttempt, api sigma.EmbeddingAPI) []sigma.EmbeddingAttempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]sigma.EmbeddingAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, sigma.EmbeddingAttempt{
			Provider:   model.Provider,
			API:        api,
			Model:      model.ID,
			Attempt:    attempt.Attempt,
			StatusCode: attempt.StatusCode,
			RequestID:  attempt.RequestID,
			Latency:    attempt.Latency,
		})
	}
	return out
}

func embeddingAuthModel(model sigma.EmbeddingModel, api sigma.API) sigma.Model {
	return sigma.Model{
		ID:                  model.ID,
		Provider:            model.Provider,
		API:                 api,
		Name:                model.Name,
		ContextWindow:       model.MaxInputTokens,
		InputCostPerMillion: model.InputCostPerMillion,
		CostCurrency:        model.CostCurrency,
		ProviderMetadata:    model.ProviderMetadata,
	}
}

func intOption(options map[string]any, key string) (int, bool) {
	switch value := options[key].(type) {
	case int:
		return value, true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case json.Number:
		parsed, err := value.Int64()
		return int(parsed), err == nil
	default:
		return 0, false
	}
}
