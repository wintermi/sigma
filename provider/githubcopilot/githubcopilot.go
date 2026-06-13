// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package githubcopilot

import (
	"net/http"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
	"github.com/wintermi/sigma/provider/openai"
)

const DefaultBaseURL = "https://api.individual.githubcopilot.com"

// Provider adapts GitHub Copilot's OpenAI-compatible Chat Completions endpoint.
type Provider = openai.Provider

// ProviderOption configures a Provider.
type ProviderOption = openai.ProviderOption

// ResponsesProvider adapts GitHub Copilot's OpenAI-compatible Responses endpoint.
type ResponsesProvider = openai.ResponsesProvider

// AnthropicProvider adapts GitHub Copilot's Anthropic-compatible Messages endpoint.
type AnthropicProvider = anthropic.Provider

// AnthropicProviderOption configures an AnthropicProvider.
type AnthropicProviderOption = anthropic.ProviderOption

// NewProvider constructs a GitHub Copilot Chat Completions provider.
func NewProvider(opts ...ProviderOption) *Provider {
	providerOpts := append([]ProviderOption{openai.WithBaseURL(DefaultBaseURL)}, opts...)
	return openai.NewProvider(providerOpts...)
}

// NewResponsesProvider constructs a GitHub Copilot Responses provider.
func NewResponsesProvider(opts ...ProviderOption) *ResponsesProvider {
	providerOpts := append([]ProviderOption{openai.WithBaseURL(DefaultBaseURL)}, opts...)
	return openai.NewResponsesProvider(providerOpts...)
}

// NewAnthropicProvider constructs a GitHub Copilot Anthropic-compatible provider.
func NewAnthropicProvider(opts ...AnthropicProviderOption) *AnthropicProvider {
	providerOpts := append([]AnthropicProviderOption{anthropic.WithBaseURL(DefaultBaseURL)}, opts...)
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

// Register adds a GitHub Copilot Chat Completions provider to registry.
func Register(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderGitHubCopilot, NewProvider(opts...))
}

// RegisterResponses adds a GitHub Copilot Responses provider to registry.
func RegisterResponses(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderGitHubCopilot, NewResponsesProvider(opts...))
}

// RegisterAnthropic adds a GitHub Copilot Anthropic-compatible provider to registry.
func RegisterAnthropic(registry *sigma.Registry, opts ...AnthropicProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderGitHubCopilot, NewAnthropicProvider(opts...))
}

// RegisterDefault adds a GitHub Copilot Chat Completions provider to sigma's default registry.
func RegisterDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderGitHubCopilot, NewProvider(opts...))
}

// RegisterResponsesDefault adds a GitHub Copilot Responses provider to sigma's default registry.
func RegisterResponsesDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderGitHubCopilot, NewResponsesProvider(opts...))
}

// RegisterDefaultAnthropic adds a GitHub Copilot Anthropic-compatible provider to sigma's default registry.
func RegisterDefaultAnthropic(opts ...AnthropicProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderGitHubCopilot, NewAnthropicProvider(opts...))
}
