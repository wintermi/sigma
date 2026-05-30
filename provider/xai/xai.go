// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package xai

import (
	"net/http"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

const DefaultBaseURL = "https://api.x.ai/v1"

// Provider adapts xAI's OpenAI-compatible Chat Completions endpoint to sigma.
type Provider = openai.Provider

// ProviderOption configures a Provider.
type ProviderOption = openai.ProviderOption

// NewProvider constructs an xAI provider.
func NewProvider(opts ...ProviderOption) *Provider {
	providerOpts := append([]ProviderOption{openai.WithBaseURL(DefaultBaseURL)}, opts...)
	return openai.NewProvider(providerOpts...)
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

// Register adds an xAI text provider to registry.
func Register(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderXAI, NewProvider(opts...))
}

// RegisterDefault adds an xAI text provider to sigma's default registry.
func RegisterDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderXAI, NewProvider(opts...))
}
