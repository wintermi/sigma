// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package kimi

import (
	"net/http"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
)

const (
	// DefaultCodingBaseURL is the Kimi Coding Anthropic-compatible API base URL.
	DefaultCodingBaseURL = "https://api.kimi.com/coding/v1"
	// DefaultUserAgent is the user agent expected by the Kimi Coding endpoint.
	DefaultUserAgent = "KimiCLI/1.5"
)

// Provider adapts Kimi's Anthropic-compatible Messages endpoint.
type Provider = anthropic.Provider

// CodingProvider adapts Kimi Coding's Anthropic-compatible Messages endpoint.
type CodingProvider = anthropic.Provider

// ProviderOption configures a CodingProvider.
type ProviderOption = anthropic.ProviderOption

// MessagesCompat describes Anthropic-compatible endpoint behavior overrides.
type MessagesCompat = anthropic.MessagesCompat

// NewProvider constructs a Kimi provider.
func NewProvider(opts ...ProviderOption) *Provider {
	return newProvider(opts...)
}

// NewCodingProvider constructs a Kimi Coding provider.
func NewCodingProvider(opts ...ProviderOption) *CodingProvider {
	return newProvider(opts...)
}

func newProvider(opts ...ProviderOption) *anthropic.Provider {
	providerOpts := append([]ProviderOption{
		anthropic.WithBaseURL(DefaultCodingBaseURL),
		anthropic.WithHeader("User-Agent", DefaultUserAgent),
	}, opts...)
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

// Register adds a Kimi text provider to registry.
func Register(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderKimi, NewProvider(opts...))
}

// RegisterCoding adds a Kimi Coding text provider to registry.
func RegisterCoding(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderKimiCoding, NewCodingProvider(opts...))
}

// RegisterDefault adds a Kimi text provider to sigma's default registry.
func RegisterDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderKimi, NewProvider(opts...))
}

// RegisterCodingDefault adds a Kimi Coding text provider to sigma's default registry.
func RegisterCodingDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderKimiCoding, NewCodingProvider(opts...))
}
