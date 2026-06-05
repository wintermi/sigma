// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamlifecycle"
	"github.com/wintermi/sigma/internal/vertexai"
)

const (
	defaultVertexAPIVersion       = "v1"
	defaultVertexPublisher        = "anthropic"
	defaultVertexAnthropicVersion = "vertex-2023-10-16"
	vertexOptionProjectID         = "project_id"
	vertexOptionProjectIDGo       = "projectID"
	vertexOptionLocation          = "location"
	vertexOptionRegion            = "region"
	vertexOptionPublisher         = "publisher"
	vertexOptionAPIVersion        = "api_version"
	vertexOptionAPIVersionGo      = "apiVersion"
	vertexOptionCredentialMode    = "credential_mode"
	vertexOptionCredentialModeGo  = "credentialMode"
	vertexMetadataProjectID       = "vertexProjectID"
	vertexMetadataLocation        = "vertexLocation"
	vertexMetadataRegion          = "vertexRegion"
	vertexMetadataPublisher       = "vertexPublisher"
	vertexMetadataAPIVersion      = "vertexAPIVersion"
	vertexMetadataCredentialMode  = "vertexCredentialMode"
)

// VertexCredentialMode selects the Google Vertex AI authentication path.
type VertexCredentialMode string

const (
	// VertexCredentialAuto resolves a sigma credential first, then falls back to
	// the configured token provider when no API key or token is available.
	VertexCredentialAuto VertexCredentialMode = VertexCredentialMode(vertexai.CredentialAuto)
	// VertexCredentialAPIKey requires an API-key credential.
	VertexCredentialAPIKey VertexCredentialMode = VertexCredentialMode(vertexai.CredentialAPIKey)
	// VertexCredentialToken requires an OAuth token credential.
	VertexCredentialToken VertexCredentialMode = VertexCredentialMode(vertexai.CredentialToken)
)

// VertexConfig carries Vertex Anthropic request configuration.
type VertexConfig struct {
	ProjectID      string
	Location       string
	Publisher      string
	APIVersion     string
	CredentialMode VertexCredentialMode
}

// VertexProvider adapts Anthropic Claude on Vertex AI to sigma.
type VertexProvider struct {
	config        VertexConfig
	baseURL       string
	client        *http.Client
	headers       map[string]string
	tokenProvider sigma.OAuthTokenProvider
	compat        *MessagesCompat
}

type vertexRequestConfig struct {
	ProjectID      string
	Location       string
	Publisher      string
	APIVersion     string
	CredentialMode VertexCredentialMode
	BaseURL        string
}

// VertexProviderOption configures a VertexProvider.
type VertexProviderOption func(*VertexProvider)

// NewVertexProvider constructs a Vertex Anthropic provider.
func NewVertexProvider(opts ...VertexProviderOption) *VertexProvider {
	provider := &VertexProvider{}
	for _, opt := range opts {
		if opt != nil {
			opt(provider)
		}
	}
	return provider
}

// WithVertexConfig configures provider-level Vertex routing and auth defaults.
func WithVertexConfig(config VertexConfig) VertexProviderOption {
	return func(provider *VertexProvider) {
		provider.config = config
	}
}

// WithVertexBaseURL configures the Vertex service base URL including API version.
func WithVertexBaseURL(baseURL string) VertexProviderOption {
	return func(provider *VertexProvider) {
		provider.baseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithVertexHTTPClient configures the provider fallback HTTP client.
func WithVertexHTTPClient(client *http.Client) VertexProviderOption {
	return func(provider *VertexProvider) {
		provider.client = client
	}
}

// WithVertexTokenProvider configures an ADC or OAuth token provider.
func WithVertexTokenProvider(tokenProvider sigma.OAuthTokenProvider) VertexProviderOption {
	return func(provider *VertexProvider) {
		provider.tokenProvider = tokenProvider
	}
}

// WithVertexHeader configures a provider default request header.
func WithVertexHeader(key, value string) VertexProviderOption {
	return WithVertexHeaders(map[string]string{key: value})
}

// WithVertexHeaders configures provider default request headers.
func WithVertexHeaders(headers map[string]string) VertexProviderOption {
	return func(provider *VertexProvider) {
		if len(headers) == 0 {
			return
		}
		if provider.headers == nil {
			provider.headers = make(map[string]string, len(headers))
		}
		for key, value := range headers {
			provider.headers[key] = value
		}
	}
}

// WithVertexMessagesCompat overrides detected Anthropic-compatible endpoint behavior.
func WithVertexMessagesCompat(compat MessagesCompat) VertexProviderOption {
	return func(provider *VertexProvider) {
		provider.compat = &compat
	}
}

// RegisterVertex adds a Vertex Anthropic text provider to registry.
func RegisterVertex(registry *sigma.Registry, providerID sigma.ProviderID, opts ...VertexProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(providerID, NewVertexProvider(opts...))
}

// RegisterVertexDefault adds a Vertex Anthropic text provider to sigma's default registry.
func RegisterVertexDefault(providerID sigma.ProviderID, opts ...VertexProviderOption) error {
	return sigma.RegisterDefaultTextProvider(providerID, NewVertexProvider(opts...))
}

// API reports the Anthropic Messages API surface.
func (p *VertexProvider) API() sigma.API {
	return sigma.APIAnthropicMessages
}

// Stream sends req to Vertex streamRawPredict and emits sigma events as SSE chunks arrive.
func (p *VertexProvider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	ctx, stream, writer, cleanup := streamlifecycle.NewTextStream(ctx, opts)
	go func() {
		defer cleanup()
		p.run(ctx, writer, model, req, opts)
	}()
	return stream
}

func (p *VertexProvider) run(ctx context.Context, writer sigma.StreamWriter, model sigma.Model, req sigma.Request, opts sigma.Options) {
	final := sigma.AssistantMessage{
		Model:    model.ID,
		Provider: model.Provider,
	}

	resp, err := sigma.DoHTTPWithRetry(
		ctx,
		p.httpClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return p.newRequest(ctx, model, req, opts)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return responseError(resp, model)
		},
		sigma.TextResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.APIAnthropicMessages, model.ID),
	)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			final.StopReason = sigma.StopReasonAborted
			_ = writer.Error(ctx, contextError(ctx, err), final)
			return
		}
		_ = writer.Error(ctx, err, final)
		return
	}
	defer resp.Body.Close()
	body := sse.CloseOnContextDone(ctx, resp.Body)
	defer body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		providerErr := responseError(resp, model)
		final.StopReason = sigma.StopReasonError
		final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		_ = writer.Error(ctx, providerErr, final)
		return
	}

	compat := anthropicMessagesCompat(model, p.baseURLForModel(model, opts), p.compat)
	final, err = parseMessagesStream(ctx, body, writer, model, compat)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			final.StopReason = sigma.StopReasonAborted
			_ = writer.Error(ctx, contextError(ctx, err), final)
			return
		}
		final.StopReason = sigma.StopReasonError
		_ = writer.Error(ctx, err, final)
		return
	}
	_ = writer.Done(ctx, final)
}

func (p *VertexProvider) newRequest(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Request, error) {
	baseURL := p.baseURLForModel(model, opts)
	compat := anthropicMessagesCompat(model, baseURL, p.compat)
	payload, err := messagesPayload(model, req, opts, compat)
	if err != nil {
		return nil, err
	}
	delete(payload, "model")
	payload["anthropic_version"] = vertexAnthropicVersion(model.Provider, opts)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("vertex anthropic messages: encode request: %w", err)
	}

	config, err := p.requestConfig(model, opts)
	if err != nil {
		return nil, err
	}
	endpoint, err := p.endpoint(model, opts, config)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("User-Agent", "sigma/vertex-anthropic-messages")

	p.addProviderHeaders(httpReq, model.Provider, opts, compat)
	for key, value := range p.headers {
		httpReq.Header.Set(key, value)
	}
	for key, value := range anthropicModelHeaders(model) {
		if unsafeCredentialHeader(key) {
			continue
		}
		httpReq.Header.Set(key, value)
	}
	if err := vertexai.AddAuthHeader(ctx, httpReq, model, opts, config.vertexConfig(), p.tokenProvider); err != nil {
		return nil, fmt.Errorf("vertex anthropic auth: %w", err)
	}
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIAnthropicMessages, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

func (p *VertexProvider) addProviderHeaders(req *http.Request, provider sigma.ProviderID, opts sigma.Options, compat messagesCompat) {
	if opts.SessionID == "" || !opts.CacheRetention.CacheEnabled() || !compat.sessionAffinityHeaders {
		return
	}
	options := providerOptions(opts, provider)
	if header, ok := stringOption(options, providerOptionSessionHeader); ok {
		req.Header.Set(header, opts.SessionID)
	} else if header, ok := stringOption(options, providerOptionSessionHeaderGo); ok {
		req.Header.Set(header, opts.SessionID)
	} else {
		req.Header.Set(defaultSessionAffinityHeader, opts.SessionID)
	}
}

func (p *VertexProvider) requestConfig(model sigma.Model, opts sigma.Options) (vertexRequestConfig, error) {
	config := vertexRequestConfig{
		ProjectID:      p.config.ProjectID,
		Location:       p.config.Location,
		Publisher:      p.config.Publisher,
		APIVersion:     p.config.APIVersion,
		CredentialMode: p.config.CredentialMode,
		BaseURL:        p.baseURL,
	}
	config.applyMetadata(model.ProviderMetadata)
	config.applyOptions(providerOptions(opts, model.Provider))
	if config.Publisher == "" {
		config.Publisher = defaultVertexPublisher
	}
	if config.APIVersion == "" {
		config.APIVersion = defaultVertexAPIVersion
	}
	if !vertexai.ValidateCredentialMode(vertexai.CredentialMode(config.CredentialMode)) {
		return vertexRequestConfig{}, invalidVertexOptions(model, fmt.Sprintf("google vertex: unsupported credential mode %q", config.CredentialMode), nil)
	}
	return config, nil
}

func (c *vertexRequestConfig) applyMetadata(metadata map[string]any) {
	if value := metadataString(metadata, "baseURL", ""); value != "" && !strings.Contains(value, "{location}") {
		c.BaseURL = value
	}
	c.ProjectID = metadataString(metadata, vertexMetadataProjectID, c.ProjectID)
	c.Location = metadataString(metadata, vertexMetadataLocation, c.Location)
	c.Location = metadataString(metadata, vertexMetadataRegion, c.Location)
	c.Publisher = metadataString(metadata, vertexMetadataPublisher, c.Publisher)
	c.APIVersion = metadataString(metadata, vertexMetadataAPIVersion, c.APIVersion)
	if mode := metadataString(metadata, vertexMetadataCredentialMode, ""); mode != "" {
		c.CredentialMode = VertexCredentialMode(mode)
	}
}

func (c *vertexRequestConfig) applyOptions(options map[string]any) {
	if value, ok := stringOption(options, providerOptionBaseURL); ok {
		c.BaseURL = value
	} else if value, ok := stringOption(options, providerOptionBaseURLCamel); ok {
		c.BaseURL = value
	}
	c.ProjectID = optionString(options, vertexOptionProjectID, c.ProjectID)
	c.ProjectID = optionString(options, vertexOptionProjectIDGo, c.ProjectID)
	c.Location = optionString(options, vertexOptionLocation, c.Location)
	c.Location = optionString(options, vertexOptionRegion, c.Location)
	c.Publisher = optionString(options, vertexOptionPublisher, c.Publisher)
	c.APIVersion = optionString(options, vertexOptionAPIVersion, c.APIVersion)
	c.APIVersion = optionString(options, vertexOptionAPIVersionGo, c.APIVersion)
	if mode := optionString(options, vertexOptionCredentialMode, ""); mode != "" {
		c.CredentialMode = VertexCredentialMode(mode)
	} else if mode := optionString(options, vertexOptionCredentialModeGo, ""); mode != "" {
		c.CredentialMode = VertexCredentialMode(mode)
	}
}

func (p *VertexProvider) endpoint(model sigma.Model, opts sigma.Options, config vertexRequestConfig) (string, error) {
	options := providerOptions(opts, model.Provider)
	if endpoint, ok := stringOption(options, providerOptionEndpoint); ok {
		return endpoint, nil
	}
	if strings.TrimSpace(config.ProjectID) == "" {
		return "", invalidVertexOptions(model, "google vertex: project ID is required", nil)
	}
	if strings.TrimSpace(config.Location) == "" {
		return "", invalidVertexOptions(model, "google vertex: location is required", nil)
	}
	baseURL, err := vertexai.BaseURL(config.vertexConfig())
	if err != nil {
		return "", invalidVertexOptions(model, err.Error(), err)
	}
	return baseURL + "/" + vertexai.PublisherModelResource(model.ID, config.vertexConfig()) + ":streamRawPredict", nil
}

func invalidVertexOptions(model sigma.Model, message string, err error) error {
	return fmt.Errorf("vertex anthropic config: %w", vertexai.InvalidOptions(model, message, err))
}

func (p *VertexProvider) baseURLForModel(model sigma.Model, opts sigma.Options) string {
	config, err := p.requestConfig(model, opts)
	if err != nil {
		return ""
	}
	baseURL, err := vertexai.BaseURL(config.vertexConfig())
	if err != nil {
		return ""
	}
	return baseURL
}

func (c vertexRequestConfig) vertexConfig() vertexai.Config {
	return vertexai.Config{
		ProjectID:      c.ProjectID,
		Location:       c.Location,
		Publisher:      c.Publisher,
		APIVersion:     c.APIVersion,
		CredentialMode: vertexai.CredentialMode(c.CredentialMode),
		BaseURL:        c.BaseURL,
	}
}

func vertexAnthropicVersion(provider sigma.ProviderID, opts sigma.Options) string {
	options := providerOptions(opts, provider)
	if version, ok := stringOption(options, providerOptionVersion); ok {
		return version
	}
	if version, ok := stringOption(options, providerOptionVersionGo); ok {
		return version
	}
	return defaultVertexAnthropicVersion
}

func optionString(options map[string]any, key string, fallback string) string {
	if value, ok := stringOption(options, key); ok {
		return value
	}
	return fallback
}

func metadataString(metadata map[string]any, key string, fallback string) string {
	value, ok := metadata[key]
	if !ok {
		return fallback
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return fallback
	}
	return text
}

func (p *VertexProvider) httpClient(opts sigma.Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	if p != nil && p.client != nil {
		return p.client
	}
	return http.DefaultClient
}
