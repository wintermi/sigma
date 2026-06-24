// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package azure

import (
	"net/http"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

const (
	// CredentialSourceAPIKey selects Azure OpenAI API-key authentication.
	CredentialSourceAPIKey = openai.AzureCredentialSourceAPIKey
	// CredentialSourceToken selects Microsoft Entra token authentication.
	CredentialSourceToken = openai.AzureCredentialSourceToken
)

// Provider adapts Azure OpenAI Responses to sigma.
type Provider = openai.AzureResponsesProvider

// ProviderOption configures a Provider.
type ProviderOption = openai.ProviderOption

// AzureAccessToken is the minimal token shape needed by Azure Responses auth.
type AzureAccessToken = openai.AzureAccessToken

// AzureTokenRequest describes the token request made by the Azure provider.
type AzureTokenRequest = openai.AzureTokenRequest

// AzureTokenCredential is a narrow adapter interface for Microsoft Entra ID
// token sources.
type AzureTokenCredential = openai.AzureTokenCredential

// AzureTokenCredentialFunc adapts a function into AzureTokenCredential.
type AzureTokenCredentialFunc = openai.AzureTokenCredentialFunc

// NewProvider constructs an Azure OpenAI Responses provider.
func NewProvider(opts ...ProviderOption) *Provider {
	return openai.NewAzureResponsesProvider(opts...)
}

// WithBaseURL configures the provider fallback Azure OpenAI endpoint.
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

// WithEndpoint overrides the Azure OpenAI resource endpoint for a request.
func WithEndpoint(endpoint string) sigma.Option {
	return openai.WithAzureResponsesEndpoint(sigma.ProviderAzureOpenAIResponses, endpoint)
}

// WithDeployment overrides the Azure OpenAI deployment name sent as the
// Responses model for a request.
func WithDeployment(deployment string) sigma.Option {
	return openai.WithAzureResponsesDeployment(sigma.ProviderAzureOpenAIResponses, deployment)
}

// WithAPIVersion overrides the api-version query parameter for a request.
func WithAPIVersion(apiVersion string) sigma.Option {
	return openai.WithAzureResponsesAPIVersion(sigma.ProviderAzureOpenAIResponses, apiVersion)
}

// WithCredentialSource documents the expected auth path for a request.
func WithCredentialSource(source string) sigma.Option {
	return openai.WithAzureResponsesCredentialSource(sigma.ProviderAzureOpenAIResponses, source)
}

// WithTokenCredential supplies a request-scoped Microsoft Entra token source.
func WithTokenCredential(credential AzureTokenCredential) sigma.Option {
	return openai.WithAzureResponsesTokenCredential(sigma.ProviderAzureOpenAIResponses, credential)
}

// Register adds the Azure OpenAI Responses text provider to registry.
func Register(registry *sigma.Registry, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(sigma.ProviderAzureOpenAIResponses, NewProvider(opts...))
}

// RegisterDefault adds the Azure OpenAI Responses text provider to sigma's
// default registry.
func RegisterDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(sigma.ProviderAzureOpenAIResponses, NewProvider(opts...))
}
