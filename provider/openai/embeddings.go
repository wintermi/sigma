// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/wintermi/sigma"
)

// EmbeddingsProvider adapts OpenAI's embeddings API to sigma.
type EmbeddingsProvider struct {
	base *Provider
}

// NewEmbeddingsProvider constructs an OpenAI Embeddings API provider.
func NewEmbeddingsProvider(opts ...ProviderOption) *EmbeddingsProvider {
	return &EmbeddingsProvider{base: NewProvider(opts...)}
}

// RegisterEmbeddings adds an OpenAI Embeddings API provider to registry.
func RegisterEmbeddings(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	if err := registry.RegisterEmbeddingProvider(providerID, NewEmbeddingsProvider(opts...)); err != nil {
		return fmt.Errorf("openai embeddings: register provider: %w", err)
	}
	return nil
}

// RegisterEmbeddingsDefault adds an OpenAI Embeddings API provider to sigma's default registry.
func RegisterEmbeddingsDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	if err := sigma.RegisterDefaultEmbeddingProvider(providerID, NewEmbeddingsProvider(opts...)); err != nil {
		return fmt.Errorf("openai embeddings: register default provider: %w", err)
	}
	return nil
}

// API reports the OpenAI Embeddings API surface.
func (p *EmbeddingsProvider) API() sigma.EmbeddingAPI {
	return sigma.EmbeddingAPIOpenAIEmbeddings
}

// Embed sends req to OpenAI's embeddings endpoint.
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
			return embeddingsResponseError(resp, model)
		},
		sigma.EmbeddingResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.EmbeddingAPIOpenAIEmbeddings, model.ID),
	)
	embeddingAttempts := embeddingAttemptsFromHTTP(model, attempts)
	if err != nil {
		response := sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return response, contextError(ctx, err)
		}
		return response, fmt.Errorf("openai embeddings: request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		response := sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return response, contextError(ctx, err)
		}
		return response, fmt.Errorf("openai embeddings: read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return sigma.Embeddings{Model: model.ID, Provider: model.Provider, Attempts: embeddingAttempts}, embeddingsProviderError(resp, model, body, nil)
	}
	embeddings, err := decodeEmbeddingsResponse(body, model)
	embeddings.Attempts = embeddingAttempts
	return embeddings, err
}

func (p *EmbeddingsProvider) newRequest(ctx context.Context, model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) (*http.Request, error) {
	payload := embeddingsPayload(model, req, opts)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: encode request: %w", err)
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
	httpReq.Header.Set("User-Agent", "sigma/openai-embeddings")

	p.addProviderHeaders(httpReq, model.Provider, opts)
	for key, value := range p.base.headers {
		httpReq.Header.Set(key, value)
	}
	addEmbeddingModelHeaders(httpReq, model)
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	if err := p.addAuthHeader(ctx, httpReq, model, opts); err != nil {
		return nil, err
	}
	sigma.ApplySuppressedHeaders(httpReq.Header, opts)
	if err := sigma.RunEmbeddingPayloadDebugHooks(ctx, opts, model.Provider, sigma.EmbeddingAPIOpenAIEmbeddings, model.ID, body, httpReq.Header); err != nil {
		return nil, fmt.Errorf("openai embeddings: payload debug hooks: %w", err)
	}
	return httpReq, nil
}

func embeddingsPayload(model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) map[string]any {
	payload := map[string]any{
		"model":           string(model.ID),
		"input":           append([]string(nil), req.Inputs...),
		"encoding_format": "float",
	}
	if req.Dimensions > 0 {
		payload["dimensions"] = req.Dimensions
	}
	if len(opts.Metadata) > 0 {
		payload["metadata"] = copyAnyMap(opts.Metadata)
	}
	for key, value := range req.ProviderMetadata {
		payload[key] = value
	}
	return payload
}

func (p *EmbeddingsProvider) addProviderHeaders(req *http.Request, provider sigma.ProviderID, opts sigma.Options) {
	options := providerOptions(opts, provider)
	if organization, ok := stringOption(options, providerOptionOrganization); ok {
		req.Header.Set("OpenAI-Organization", organization)
	}
	if project, ok := stringOption(options, providerOptionProject); ok {
		req.Header.Set("OpenAI-Project", project)
	}
}

func (p *EmbeddingsProvider) addAuthHeader(ctx context.Context, req *http.Request, model sigma.EmbeddingModel, opts sigma.Options) error {
	if opts.AuthResolver == nil {
		return &sigma.Error{
			Code:     sigma.ErrorUnsupported,
			Message:  "openai embeddings: auth resolver is required",
			Provider: model.Provider,
			Model:    model.ID,
		}
	}
	credential, err := opts.AuthResolver.Resolve(ctx, embeddingAuthModel(model), opts)
	if err != nil {
		return err
	}
	if credential.Value != "" {
		req.Header.Set("Authorization", "Bearer "+credential.Value)
	}
	return nil
}

func (p *EmbeddingsProvider) endpoint(model sigma.EmbeddingModel, opts sigma.Options) (string, error) {
	options := providerOptions(opts, model.Provider)
	if endpoint, ok := stringOption(options, providerOptionEndpoint); ok {
		return endpoint, nil
	}
	baseURL := p.baseURLForModel(model, opts)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("openai embeddings: invalid base URL %q", baseURL)
	}
	return baseURL + "/embeddings", nil
}

func (p *EmbeddingsProvider) baseURLForModel(model sigma.EmbeddingModel, opts sigma.Options) string {
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

func addEmbeddingModelHeaders(req *http.Request, model sigma.EmbeddingModel) {
	headers := embeddingModelHeaders(model)
	for key, value := range headers {
		if strings.TrimSpace(value) == "" || unsafeCredentialHeader(key) {
			continue
		}
		req.Header.Set(key, value)
	}
}

func embeddingModelHeaders(model sigma.EmbeddingModel) map[string]string {
	raw, ok := model.ProviderMetadata[sigma.MetadataOpenAICompatibleHeaders]
	if !ok {
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

func embeddingAuthModel(model sigma.EmbeddingModel) sigma.Model {
	return sigma.Model{
		ID:                  model.ID,
		Provider:            model.Provider,
		API:                 sigma.API(model.API),
		Name:                model.Name,
		ContextWindow:       model.MaxInputTokens,
		InputCostPerMillion: model.InputCostPerMillion,
		CostCurrency:        model.CostCurrency,
		ProviderMetadata:    model.ProviderMetadata,
	}
}

type embeddingsResponse struct {
	Data  []embeddingData `json:"data"`
	Model string          `json:"model"`
	Usage embeddingsUsage `json:"usage"`
}

type embeddingData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type embeddingsUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func decodeEmbeddingsResponse(body []byte, model sigma.EmbeddingModel) (sigma.Embeddings, error) {
	var decoded embeddingsResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return sigma.Embeddings{}, fmt.Errorf("openai embeddings: decode response: %w", err)
	}
	sort.SliceStable(decoded.Data, func(i, j int) bool {
		return decoded.Data[i].Index < decoded.Data[j].Index
	})
	out := sigma.Embeddings{
		Model:    model.ID,
		Provider: model.Provider,
		Vectors:  make([]sigma.Embedding, 0, len(decoded.Data)),
	}
	if decoded.Model != "" {
		out.Model = sigma.ModelID(decoded.Model)
	}
	for _, item := range decoded.Data {
		out.Vectors = append(out.Vectors, sigma.Embedding{
			Index:  item.Index,
			Vector: append([]float32(nil), item.Embedding...),
		})
	}
	if decoded.Usage.PromptTokens > 0 || decoded.Usage.TotalTokens > 0 {
		usage := sigma.Usage{
			InputTokens: decoded.Usage.PromptTokens,
			TotalTokens: decoded.Usage.TotalTokens,
		}
		out.Usage = &usage
		cost := sigma.CostForEmbeddingUsage(model, usage)
		out.Cost = &cost
	}
	return out, nil
}

func embeddingsResponseError(resp *http.Response, model sigma.EmbeddingModel) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return embeddingsProviderError(resp, model, body, contextOverflowCause(body))
}

func embeddingsProviderError(resp *http.Response, model sigma.EmbeddingModel, body []byte, err error) *sigma.ProviderError {
	return sigma.NewProviderError(
		model.Provider,
		sigma.API(sigma.EmbeddingAPIOpenAIEmbeddings),
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		err,
	)
}

func embeddingAttemptsFromHTTP(model sigma.EmbeddingModel, attempts []sigma.HTTPAttempt) []sigma.EmbeddingAttempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]sigma.EmbeddingAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, sigma.EmbeddingAttempt{
			Provider:   model.Provider,
			API:        sigma.EmbeddingAPIOpenAIEmbeddings,
			Model:      model.ID,
			Attempt:    attempt.Attempt,
			StatusCode: attempt.StatusCode,
			RequestID:  attempt.RequestID,
			Latency:    attempt.Latency,
		})
	}
	return out
}
