// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package zai

import (
	"net/http"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

const (
	// DefaultBaseURL is the Z.ai Coding Plan API base URL.
	DefaultBaseURL = "https://api.z.ai/api/coding/paas/v4"
	// DefaultCodingCNBaseURL is the Z.ai Coding Plan China API base URL.
	DefaultCodingCNBaseURL = "https://open.bigmodel.cn/api/coding/paas/v4"
)

// Provider adapts Z.ai's OpenAI-compatible Chat Completions endpoint to sigma.
type Provider = openai.Provider

// ProviderOption configures a Provider.
type ProviderOption = openai.ProviderOption

// NewProvider constructs a Z.ai provider.
func NewProvider(opts ...ProviderOption) *Provider {
	providerOpts := append([]ProviderOption{openai.WithBaseURL(DefaultBaseURL)}, opts...)
	return openai.NewProvider(providerOpts...)
}

// NewCodingCNProvider constructs a Z.ai Coding Plan China provider.
func NewCodingCNProvider(opts ...ProviderOption) *Provider {
	providerOpts := append([]ProviderOption{openai.WithBaseURL(DefaultCodingCNBaseURL)}, opts...)
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

// Register adds a Z.ai text provider to registry.
func Register(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderZAI, NewProvider(opts...))
}

// RegisterCodingCN adds a Z.ai Coding Plan China text provider to registry.
func RegisterCodingCN(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderZAICodingCN, NewCodingCNProvider(opts...))
}

// RegisterDefault adds a Z.ai text provider to sigma's default registry.
func RegisterDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderZAI, NewProvider(opts...))
}

// RegisterDefaultCodingCN adds a Z.ai Coding Plan China text provider to sigma's default registry.
func RegisterDefaultCodingCN(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderZAICodingCN, NewCodingCNProvider(opts...))
}
