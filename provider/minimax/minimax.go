// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package minimax

import (
	"net/http"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
)

const (
	// DefaultBaseURL is the MiniMax Anthropic-compatible API base URL.
	DefaultBaseURL = "https://api.minimax.io/anthropic/v1"
	// DefaultCNBaseURL is the MiniMax CN Anthropic-compatible API base URL.
	DefaultCNBaseURL = "https://api.minimaxi.com/anthropic/v1"
)

// Provider adapts MiniMax's Anthropic-compatible Messages endpoint to sigma.
type Provider = anthropic.Provider

// ProviderOption configures a Provider.
type ProviderOption = anthropic.ProviderOption

// MessagesCompat describes Anthropic-compatible endpoint behavior overrides.
type MessagesCompat = anthropic.MessagesCompat

// NewProvider constructs a MiniMax provider.
func NewProvider(opts ...ProviderOption) *Provider {
	providerOpts := append([]ProviderOption{anthropic.WithBaseURL(DefaultBaseURL)}, opts...)
	return anthropic.NewProvider(providerOpts...)
}

// NewCNProvider constructs a MiniMax CN provider.
func NewCNProvider(opts ...ProviderOption) *Provider {
	providerOpts := append([]ProviderOption{anthropic.WithBaseURL(DefaultCNBaseURL)}, opts...)
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

// Register adds a MiniMax text provider to registry.
func Register(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderMiniMax, NewProvider(opts...))
}

// RegisterCN adds a MiniMax CN text provider to registry.
func RegisterCN(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderMiniMaxCN, NewCNProvider(opts...))
}

// RegisterDefault adds a MiniMax text provider to sigma's default registry.
func RegisterDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderMiniMax, NewProvider(opts...))
}

// RegisterCNDefault adds a MiniMax CN text provider to sigma's default registry.
func RegisterCNDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderMiniMaxCN, NewCNProvider(opts...))
}
