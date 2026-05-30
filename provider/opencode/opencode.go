// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package opencode

import (
	"context"
	"net/http"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/openai"
)

const (
	// ZenBaseURL is the OpenCode Zen API base URL.
	ZenBaseURL = "https://opencode.ai/zen/v1"
	// GoBaseURL is the OpenCode Go API base URL.
	GoBaseURL = "https://opencode.ai/zen/go/v1"

	metadataAPI = "opencodeAPI"
)

// Provider dispatches OpenCode models to their real API-compatible route.
type Provider struct {
	chat      sigma.TextProvider
	responses sigma.TextProvider
	anthropic sigma.TextProvider
	google    sigma.TextProvider
}

type providerConfig struct {
	baseURL string
	client  *http.Client
	headers map[string]string
}

// ProviderOption configures an OpenCode provider.
type ProviderOption func(*providerConfig)

// NewProvider constructs an OpenCode provider.
func NewProvider(opts ...ProviderOption) *Provider {
	cfg := providerConfig{baseURL: ZenBaseURL}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	openAIOpts := openAIOptions(cfg)
	anthropicOpts := anthropicOptions(cfg)
	googleOpts := googleOptions(cfg)
	return &Provider{
		chat:      openai.NewProvider(openAIOpts...),
		responses: openai.NewResponsesProvider(openAIOpts...),
		anthropic: anthropic.NewProvider(anthropicOpts...),
		google:    google.NewProvider(googleOpts...),
	}
}

// WithBaseURL configures the OpenCode route base URL.
func WithBaseURL(baseURL string) ProviderOption {
	return func(cfg *providerConfig) {
		cfg.baseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithHTTPClient configures the HTTP client for wrapped providers.
func WithHTTPClient(client *http.Client) ProviderOption {
	return func(cfg *providerConfig) {
		cfg.client = client
	}
}

// WithHeader configures a provider default request header.
func WithHeader(key, value string) ProviderOption {
	return WithHeaders(map[string]string{key: value})
}

// WithHeaders configures provider default request headers.
func WithHeaders(headers map[string]string) ProviderOption {
	return func(cfg *providerConfig) {
		if len(headers) == 0 {
			return
		}
		if cfg.headers == nil {
			cfg.headers = make(map[string]string, len(headers))
		}
		for key, value := range headers {
			cfg.headers[key] = value
		}
	}
}

// RegisterZen registers the OpenCode Zen provider.
func RegisterZen(registry *sigma.Registry, opts ...ProviderOption) error {
	return Register(registry, sigma.ProviderOpenCode, append([]ProviderOption{WithBaseURL(ZenBaseURL)}, opts...)...)
}

// RegisterGo registers the OpenCode Go provider.
func RegisterGo(registry *sigma.Registry, opts ...ProviderOption) error {
	return Register(registry, sigma.ProviderOpenCodeGo, append([]ProviderOption{WithBaseURL(GoBaseURL)}, opts...)...)
}

// RegisterDefault registers both OpenCode Zen and OpenCode Go providers.
func RegisterDefault(registry *sigma.Registry, opts ...ProviderOption) error {
	if err := RegisterZen(registry, opts...); err != nil {
		return err
	}
	return RegisterGo(registry, opts...)
}

// Register registers an OpenCode provider with a caller-selected provider ID.
func Register(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(providerID, NewProvider(opts...))
}

// RegisterZenDefault registers OpenCode Zen on sigma's default registry.
func RegisterZenDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(
		sigma.ProviderOpenCode,
		NewProvider(append([]ProviderOption{WithBaseURL(ZenBaseURL)}, opts...)...),
	)
}

// RegisterGoDefault registers OpenCode Go on sigma's default registry.
func RegisterGoDefault(opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(
		sigma.ProviderOpenCodeGo,
		NewProvider(append([]ProviderOption{WithBaseURL(GoBaseURL)}, opts...)...),
	)
}

// API reports the registry-facing API surface. The real wire API is selected
// per model at request time.
func (p *Provider) API() sigma.API {
	return sigma.APIOpenAICompletions
}

// Stream dispatches the request to the adapter matching the model metadata.
func (p *Provider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	api := OpenCodeAPI(model)
	model.API = api
	switch api {
	case sigma.APIGoogleGenerativeAI:
		return p.google.Stream(ctx, model, req, opts)
	case sigma.APIAnthropicMessages:
		return p.anthropic.Stream(ctx, model, req, opts)
	case sigma.APIOpenAIResponses:
		return p.responses.Stream(ctx, model, req, opts)
	default:
		return p.chat.Stream(ctx, model, req, opts)
	}
}

// OpenCodeAPI returns the real API family for model.
func OpenCodeAPI(model sigma.Model) sigma.API {
	if model.ProviderMetadata != nil {
		if api, ok := model.ProviderMetadata[metadataAPI].(string); ok && api != "" {
			return sigma.API(api)
		}
	}
	if model.API != "" {
		return model.API
	}
	return sigma.APIOpenAICompletions
}

func openAIOptions(cfg providerConfig) []openai.ProviderOption {
	opts := []openai.ProviderOption{openai.WithBaseURL(cfg.baseURL)}
	if cfg.client != nil {
		opts = append(opts, openai.WithHTTPClient(cfg.client))
	}
	if len(cfg.headers) > 0 {
		opts = append(opts, openai.WithHeaders(cfg.headers))
	}
	return opts
}

func anthropicOptions(cfg providerConfig) []anthropic.ProviderOption {
	opts := []anthropic.ProviderOption{anthropic.WithBaseURL(cfg.baseURL)}
	if cfg.client != nil {
		opts = append(opts, anthropic.WithHTTPClient(cfg.client))
	}
	if len(cfg.headers) > 0 {
		opts = append(opts, anthropic.WithHeaders(cfg.headers))
	}
	return opts
}

func googleOptions(cfg providerConfig) []google.ProviderOption {
	opts := []google.ProviderOption{google.WithBaseURL(cfg.baseURL)}
	if cfg.client != nil {
		opts = append(opts, google.WithHTTPClient(cfg.client))
	}
	if len(cfg.headers) > 0 {
		opts = append(opts, google.WithHeaders(cfg.headers))
	}
	return opts
}
