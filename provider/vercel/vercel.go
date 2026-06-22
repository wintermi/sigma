// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package vercel

import (
	"net/http"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
)

const DefaultBaseURL = "https://ai-gateway.vercel.sh"

// Provider adapts Vercel AI Gateway's Anthropic-compatible Messages endpoint.
type Provider = anthropic.Provider

// ProviderOption configures a Provider.
type ProviderOption = anthropic.ProviderOption

// MessagesCompat describes Anthropic-compatible endpoint behavior overrides.
type MessagesCompat = anthropic.MessagesCompat

// NewProvider constructs a Vercel AI Gateway provider.
func NewProvider(opts ...ProviderOption) *Provider {
	providerOpts := append([]ProviderOption{anthropic.WithBaseURL(DefaultBaseURL)}, opts...)
	return anthropic.NewProvider(providerOpts...)
}

// WithBaseURL configures the provider base URL, for example an httptest server URL.
func WithBaseURL(baseURL string) ProviderOption {
	return anthropic.WithBaseURL(baseURL)
}

// WithHTTPClient configures the provider fallback HTTP client.
func WithHTTPClient(client *http.Client) ProviderOption {
	return anthropic.WithHTTPClient(client)
}

// WithHeader configures a provider default request header.
func WithHeader(key, value string) ProviderOption {
	return anthropic.WithHeader(key, value)
}

// WithHeaders configures provider default request headers.
func WithHeaders(headers map[string]string) ProviderOption {
	return anthropic.WithHeaders(headers)
}

// WithMessagesCompat overrides detected Anthropic-compatible endpoint behavior.
func WithMessagesCompat(compat MessagesCompat) ProviderOption {
	return anthropic.WithMessagesCompat(compat)
}

// Register adds a Vercel AI Gateway text provider to registry.
func Register(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderVercelAIGateway, NewProvider(opts...))
}

// RegisterDefault adds a Vercel AI Gateway text provider to sigma's default registry.
func RegisterDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderVercelAIGateway, NewProvider(opts...))
}
