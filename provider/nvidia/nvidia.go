// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package nvidia

import (
	"context"
	"fmt"
	"net/http"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

const (
	DefaultBaseURL = "https://integrate.api.nvidia.com/v1"

	embeddingOptionInputType = "input_type"
)

// Provider adapts NVIDIA NIM's OpenAI-compatible Chat Completions endpoint to sigma.
type Provider = openai.Provider

// ProviderOption configures a Provider.
type ProviderOption = openai.ProviderOption

// EmbeddingsProvider adapts NVIDIA NIM's OpenAI-compatible Embeddings endpoint.
type EmbeddingsProvider struct {
	base *openai.EmbeddingsProvider
}

// NewProvider constructs an NVIDIA NIM text provider.
func NewProvider(opts ...ProviderOption) *Provider {
	providerOpts := append([]ProviderOption{openai.WithBaseURL(DefaultBaseURL)}, opts...)
	return openai.NewProvider(providerOpts...)
}

// NewEmbeddingsProvider constructs an NVIDIA NIM embeddings provider.
func NewEmbeddingsProvider(opts ...ProviderOption) *EmbeddingsProvider {
	providerOpts := append([]ProviderOption{openai.WithBaseURL(DefaultBaseURL)}, opts...)
	return &EmbeddingsProvider{base: openai.NewEmbeddingsProvider(providerOpts...)}
}

// WithBaseURL configures the provider base URL, for example an httptest server URL.
func WithBaseURL(baseURL string) ProviderOption {
	return openai.WithBaseURL(baseURL)
}

// WithHTTPClient configures the provider fallback HTTP client.
func WithHTTPClient(client *http.Client) ProviderOption {
	return openai.WithHTTPClient(client)
}

// WithHeader configures a provider default request header.
func WithHeader(key, value string) ProviderOption {
	return openai.WithHeader(key, value)
}

// WithHeaders configures provider default request headers.
func WithHeaders(headers map[string]string) ProviderOption {
	return openai.WithHeaders(headers)
}

// Register adds an NVIDIA NIM text provider to registry.
func Register(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderNVIDIA, NewProvider(opts...))
}

// RegisterEmbeddings adds an NVIDIA NIM embeddings provider to registry.
func RegisterEmbeddings(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	if err := registry.RegisterEmbeddingProvider(sigma.ProviderNVIDIA, NewEmbeddingsProvider(opts...)); err != nil {
		return fmt.Errorf("nvidia embeddings: register provider: %w", err)
	}
	return nil
}

// RegisterDefault adds an NVIDIA NIM text provider to sigma's default registry.
func RegisterDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderNVIDIA, NewProvider(opts...))
}

// RegisterDefaultEmbeddings adds an NVIDIA NIM embeddings provider to sigma's
// default registry.
func RegisterDefaultEmbeddings(opts ...ProviderOption) error {
	if err := sigma.RegisterDefaultEmbeddingProvider(sigma.ProviderNVIDIA, NewEmbeddingsProvider(opts...)); err != nil {
		return fmt.Errorf("nvidia embeddings: register default provider: %w", err)
	}
	return nil
}

// API reports the NVIDIA NIM embeddings API surface.
func (p *EmbeddingsProvider) API() sigma.EmbeddingAPI {
	return sigma.EmbeddingAPIOpenAIEmbeddings
}

// Embed sends req to NVIDIA NIM's OpenAI-compatible embeddings endpoint.
func (p *EmbeddingsProvider) Embed(ctx context.Context, model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) (sigma.Embeddings, error) {
	embeddings, err := p.base.Embed(ctx, model, withNVIDIAEmbeddingInputType(req), opts)
	if err != nil {
		return embeddings, fmt.Errorf("nvidia embeddings: %w", err)
	}
	return embeddings, nil
}

func withNVIDIAEmbeddingInputType(req sigma.EmbeddingRequest) sigma.EmbeddingRequest {
	if _, ok := req.ProviderMetadata[embeddingOptionInputType]; ok {
		return req
	}
	inputType := nvidiaEmbeddingInputType(req.InputType)
	if inputType == "" {
		return req
	}
	metadata := make(map[string]any, len(req.ProviderMetadata)+1)
	for key, value := range req.ProviderMetadata {
		metadata[key] = value
	}
	metadata[embeddingOptionInputType] = inputType
	req.ProviderMetadata = metadata
	return req
}

func nvidiaEmbeddingInputType(inputType sigma.EmbeddingInputType) string {
	switch inputType {
	case sigma.EmbeddingInputTypeQuery:
		return "query"
	case sigma.EmbeddingInputTypeDocument:
		return "passage"
	default:
		return ""
	}
}
