// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package cloudflare

import (
	"net/http"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
	"github.com/wintermi/sigma/provider/openai"
)

const (
	DefaultAIGatewayOpenAIBaseURL    = "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai"
	DefaultAIGatewayAnthropicBaseURL = "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic"
)

const (
	aiGatewayAccountIDOption = "cloudflare_ai_gateway_account_id"
	aiGatewayIDOption        = "cloudflare_ai_gateway_id"
)

// Provider adapts Cloudflare AI Gateway's OpenAI-compatible Chat Completions endpoint.
type Provider = openai.Provider

// ProviderOption configures a Provider.
type ProviderOption = openai.ProviderOption

// ResponsesProvider adapts Cloudflare AI Gateway's OpenAI-compatible Responses endpoint.
type ResponsesProvider = openai.ResponsesProvider

// AnthropicProvider adapts Cloudflare AI Gateway's Anthropic-compatible Messages endpoint.
type AnthropicProvider = anthropic.Provider

// AnthropicProviderOption configures an AnthropicProvider.
type AnthropicProviderOption = anthropic.ProviderOption

// NewAIGatewayProvider constructs a Cloudflare AI Gateway Chat Completions provider.
func NewAIGatewayProvider(opts ...ProviderOption) *Provider {
	providerOpts := append([]ProviderOption{openai.WithBaseURL(DefaultAIGatewayOpenAIBaseURL)}, opts...)
	return openai.NewProvider(providerOpts...)
}

// NewAIGatewayResponsesProvider constructs a Cloudflare AI Gateway Responses provider.
func NewAIGatewayResponsesProvider(opts ...ProviderOption) *ResponsesProvider {
	providerOpts := append([]ProviderOption{openai.WithBaseURL(DefaultAIGatewayOpenAIBaseURL)}, opts...)
	return openai.NewResponsesProvider(providerOpts...)
}

// NewAIGatewayAnthropicProvider constructs a Cloudflare AI Gateway Anthropic-compatible provider.
func NewAIGatewayAnthropicProvider(opts ...AnthropicProviderOption) *AnthropicProvider {
	providerOpts := append([]AnthropicProviderOption{anthropic.WithBaseURL(DefaultAIGatewayAnthropicBaseURL)}, opts...)
	return anthropic.NewProvider(providerOpts...)
}

// WithBaseURL configures the provider base URL, for example an httptest server URL.
func WithBaseURL(baseURL string) ProviderOption {
	return openai.WithBaseURL(baseURL)
}

// WithAnthropicBaseURL configures the Anthropic-compatible provider base URL.
func WithAnthropicBaseURL(baseURL string) AnthropicProviderOption {
	return anthropic.WithBaseURL(baseURL)
}

// WithHTTPClient configures the provider fallback HTTP client.
func WithHTTPClient(client *http.Client) ProviderOption {
	return openai.WithHTTPClient(client)
}

// WithAnthropicHTTPClient configures the Anthropic-compatible provider fallback HTTP client.
func WithAnthropicHTTPClient(client *http.Client) AnthropicProviderOption {
	return anthropic.WithHTTPClient(client)
}

// WithHeader configures a provider default request header.
func WithHeader(key, value string) ProviderOption {
	return openai.WithHeader(key, value)
}

// WithAnthropicHeader configures an Anthropic-compatible provider default request header.
func WithAnthropicHeader(key, value string) AnthropicProviderOption {
	return anthropic.WithHeader(key, value)
}

// WithHeaders configures provider default request headers.
func WithHeaders(headers map[string]string) ProviderOption {
	return openai.WithHeaders(headers)
}

// WithAnthropicHeaders configures Anthropic-compatible provider default request headers.
func WithAnthropicHeaders(headers map[string]string) AnthropicProviderOption {
	return anthropic.WithHeaders(headers)
}

// WithAnthropicMessagesCompat overrides detected Anthropic-compatible endpoint behavior.
func WithAnthropicMessagesCompat(compat anthropic.MessagesCompat) AnthropicProviderOption {
	return anthropic.WithMessagesCompat(compat)
}

// WithAIGatewayAccountID configures the request-scoped Cloudflare account ID
// used to resolve AI Gateway base URL placeholders.
func WithAIGatewayAccountID(accountID string) sigma.Option {
	return sigma.WithProviderOption(sigma.ProviderCloudflareAIGateway, aiGatewayAccountIDOption, accountID)
}

// WithAIGatewayID configures the request-scoped Cloudflare AI Gateway ID used
// to resolve AI Gateway base URL placeholders.
func WithAIGatewayID(gatewayID string) sigma.Option {
	return sigma.WithProviderOption(sigma.ProviderCloudflareAIGateway, aiGatewayIDOption, gatewayID)
}

// RegisterAIGateway adds a Cloudflare AI Gateway Chat Completions provider to registry.
func RegisterAIGateway(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderCloudflareAIGateway, NewAIGatewayProvider(opts...))
}

// RegisterAIGatewayResponses adds a Cloudflare AI Gateway Responses provider to registry.
func RegisterAIGatewayResponses(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderCloudflareAIGateway, NewAIGatewayResponsesProvider(opts...))
}

// RegisterAIGatewayAnthropic adds a Cloudflare AI Gateway Anthropic-compatible provider to registry.
func RegisterAIGatewayAnthropic(registry *sigma.Registry, opts ...AnthropicProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderCloudflareAIGateway, NewAIGatewayAnthropicProvider(opts...))
}

// RegisterDefaultAIGateway adds a Cloudflare AI Gateway Chat Completions provider to sigma's default registry.
func RegisterDefaultAIGateway(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderCloudflareAIGateway, NewAIGatewayProvider(opts...))
}

// RegisterDefaultAIGatewayResponses adds a Cloudflare AI Gateway Responses provider to sigma's default registry.
func RegisterDefaultAIGatewayResponses(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderCloudflareAIGateway, NewAIGatewayResponsesProvider(opts...))
}

// RegisterDefaultAIGatewayAnthropic adds a Cloudflare AI Gateway Anthropic-compatible provider to sigma's default registry.
func RegisterDefaultAIGatewayAnthropic(opts ...AnthropicProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderCloudflareAIGateway, NewAIGatewayAnthropicProvider(opts...))
}
