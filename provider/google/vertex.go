// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamlifecycle"
)

const (
	defaultVertexAPIVersion = "v1"
	defaultVertexPublisher  = "google"

	vertexOptionProjectID        = "project_id"
	vertexOptionProjectIDGo      = "projectID"
	vertexOptionLocation         = "location"
	vertexOptionRegion           = "region"
	vertexOptionPublisher        = "publisher"
	vertexOptionModelEndpoint    = "model_endpoint"
	vertexOptionModelEndpointGo  = "modelEndpoint"
	vertexOptionAPIVersion       = "api_version"
	vertexOptionAPIVersionGo     = "apiVersion"
	vertexOptionCredentialMode   = "credential_mode"
	vertexOptionCredentialModeGo = "credentialMode"
	vertexMetadataProjectID      = "vertexProjectID"
	vertexMetadataLocation       = "vertexLocation"
	vertexMetadataRegion         = "vertexRegion"
	vertexMetadataPublisher      = "vertexPublisher"
	vertexMetadataModelEndpoint  = "vertexModelEndpoint"
	vertexMetadataAPIVersion     = "vertexAPIVersion"
	vertexMetadataCredentialMode = "vertexCredentialMode"
)

// VertexConfig carries Google Vertex AI request configuration.
//
// Applications that load environment configuration should resolve project and
// location outside the provider, then pass them here. Common environment names
// are GOOGLE_CLOUD_PROJECT, GOOGLE_CLOUD_LOCATION or GOOGLE_CLOUD_REGION for
// routing, GOOGLE_API_KEY or GOOGLE_CLOUD_API_KEY for API-key auth, and
// GOOGLE_APPLICATION_CREDENTIALS for ADC-backed token providers.
type VertexConfig struct {
	ProjectID      string
	Location       string
	Publisher      string
	ModelEndpoint  string
	APIVersion     string
	CredentialMode VertexCredentialMode
}

// VertexProvider adapts the Google Vertex AI streamGenerateContent API to sigma.
type VertexProvider struct {
	config        VertexConfig
	baseURL       string
	client        *http.Client
	headers       map[string]string
	tokenProvider sigma.OAuthTokenProvider
	payloadHooks  []PayloadHook
	responseHooks []ResponseHook
}

type vertexRequestConfig struct {
	ProjectID      string
	Location       string
	Publisher      string
	ModelEndpoint  string
	APIVersion     string
	CredentialMode VertexCredentialMode
	BaseURL        string
}

// VertexProviderOption configures a VertexProvider.
type VertexProviderOption func(*VertexProvider)

// NewVertexProvider constructs a Google Vertex AI provider.
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

// WithVertexPayloadHook adds a request payload hook.
func WithVertexPayloadHook(hook PayloadHook) VertexProviderOption {
	return func(provider *VertexProvider) {
		if hook != nil {
			provider.payloadHooks = append(provider.payloadHooks, hook)
		}
	}
}

// WithVertexResponseHook adds an HTTP response hook.
func WithVertexResponseHook(hook ResponseHook) VertexProviderOption {
	return func(provider *VertexProvider) {
		if hook != nil {
			provider.responseHooks = append(provider.responseHooks, hook)
		}
	}
}

// RegisterVertex adds a Google Vertex AI text provider to registry.
func RegisterVertex(registry *sigma.Registry, providerID sigma.ProviderID, opts ...VertexProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(providerID, NewVertexProvider(opts...))
}

// RegisterVertexDefault adds a Google Vertex AI text provider to sigma's default registry.
func RegisterVertexDefault(providerID sigma.ProviderID, opts ...VertexProviderOption) error {
	return sigma.RegisterDefaultTextProvider(providerID, NewVertexProvider(opts...))
}

// API reports the Google Vertex AI API surface.
func (p *VertexProvider) API() sigma.API {
	return sigma.APIGoogleVertex
}

// Stream sends req to streamGenerateContent and emits sigma events as SSE chunks arrive.
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

	resp, err := p.do(ctx, model, req, opts)
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
		providerErr := vertexResponseError(resp, model)
		final.StopReason = sigma.StopReasonError
		final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		_ = writer.Error(ctx, providerErr, final)
		return
	}

	final, err = parseGenerativeStream(ctx, body, writer, model)
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

func (p *VertexProvider) do(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Response, error) {
	return sigma.DoHTTPWithRetry(
		ctx,
		p.httpClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return p.newRequest(ctx, model, req, opts)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return vertexResponseError(resp, model)
		},
		func(resp *http.Response) error {
			for _, hook := range p.responseHooks {
				if err := hook(ctx, model, opts, resp); err != nil {
					return err
				}
			}
			return nil
		},
		sigma.TextResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.APIGoogleVertex, model.ID),
	)
}

func (p *VertexProvider) newRequest(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Request, error) {
	payload, err := generativePayload(model, req, opts)
	if err != nil {
		return nil, err
	}
	for _, hook := range p.payloadHooks {
		if err := hook(ctx, model, req, opts, payload); err != nil {
			return nil, err
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("google vertex: encode request: %w", err)
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
	httpReq.Header.Set("User-Agent", "sigma/google-vertex")

	for key, value := range p.headers {
		httpReq.Header.Set(key, value)
	}
	for key, value := range googleModelHeaders(model) {
		httpReq.Header.Set(key, value)
	}
	if err := p.addAuthHeader(ctx, httpReq, model, opts, config); err != nil {
		return nil, err
	}
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	sigma.ApplySuppressedHeaders(httpReq.Header, opts)
	if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIGoogleVertex, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

func (p *VertexProvider) requestConfig(model sigma.Model, opts sigma.Options) (vertexRequestConfig, error) {
	config := vertexRequestConfig{
		ProjectID:      p.config.ProjectID,
		Location:       p.config.Location,
		Publisher:      p.config.Publisher,
		ModelEndpoint:  p.config.ModelEndpoint,
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
	if !validVertexCredentialMode(config.CredentialMode) {
		return vertexRequestConfig{}, vertexInvalidOptions(model, fmt.Sprintf("google vertex: unsupported credential mode %q", config.CredentialMode), nil)
	}
	return config, nil
}

func (c *vertexRequestConfig) applyMetadata(metadata map[string]any) {
	if value := modelMetadataBaseURL(metadata); value != "" && !strings.Contains(value, "{location}") {
		c.BaseURL = value
	}
	c.ProjectID = metadataString(metadata, vertexMetadataProjectID, c.ProjectID)
	c.Location = metadataString(metadata, vertexMetadataLocation, c.Location)
	c.Location = metadataString(metadata, vertexMetadataRegion, c.Location)
	c.Publisher = metadataString(metadata, vertexMetadataPublisher, c.Publisher)
	c.ModelEndpoint = metadataString(metadata, vertexMetadataModelEndpoint, c.ModelEndpoint)
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
	c.ModelEndpoint = optionString(options, vertexOptionModelEndpoint, c.ModelEndpoint)
	c.ModelEndpoint = optionString(options, vertexOptionModelEndpointGo, c.ModelEndpoint)
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
		return "", vertexInvalidOptions(model, "google vertex: project ID is required", nil)
	}
	if strings.TrimSpace(config.Location) == "" {
		return "", vertexInvalidOptions(model, "google vertex: location is required", nil)
	}
	baseURL, err := vertexBaseURL(config)
	if err != nil {
		return "", vertexInvalidOptions(model, err.Error(), err)
	}
	resource := vertexModelResource(model.ID, config)
	endpoint := baseURL + "/" + resource + ":streamGenerateContent"
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	return endpoint + separator + "alt=sse", nil
}

func vertexBaseURL(config vertexRequestConfig) (string, error) {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		if config.Location == "global" {
			baseURL = "https://aiplatform.googleapis.com/" + config.APIVersion
		} else {
			baseURL = "https://" + config.Location + "-aiplatform.googleapis.com/" + config.APIVersion
		}
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("google vertex: invalid base URL %q", baseURL)
	}
	return baseURL, nil
}

func vertexModelResource(model sigma.ModelID, config vertexRequestConfig) string {
	if config.ModelEndpoint != "" {
		endpoint := strings.Trim(config.ModelEndpoint, "/")
		if strings.HasPrefix(endpoint, "projects/") {
			return endpoint
		}
		if strings.HasPrefix(endpoint, "endpoints/") {
			return vertexProjectLocation(config) + "/" + endpoint
		}
		return vertexProjectLocation(config) + "/endpoints/" + url.PathEscape(endpoint)
	}
	modelID := strings.Trim(string(model), "/")
	switch {
	case strings.HasPrefix(modelID, "projects/"):
		return modelID
	case strings.HasPrefix(modelID, "publishers/"):
		return vertexProjectLocation(config) + "/" + modelID
	case strings.HasPrefix(modelID, "models/"):
		return vertexProjectLocation(config) + "/publishers/" + url.PathEscape(config.Publisher) + "/" + modelID
	default:
		return vertexProjectLocation(config) + "/publishers/" + url.PathEscape(config.Publisher) + "/models/" + url.PathEscape(modelID)
	}
}

func vertexProjectLocation(config vertexRequestConfig) string {
	return "projects/" + url.PathEscape(config.ProjectID) + "/locations/" + url.PathEscape(config.Location)
}

func validVertexCredentialMode(mode VertexCredentialMode) bool {
	switch mode {
	case VertexCredentialAuto, VertexCredentialAPIKey, VertexCredentialToken:
		return true
	default:
		return false
	}
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
	if p.client != nil {
		return p.client
	}
	return http.DefaultClient
}

func vertexResponseError(resp *http.Response, model sigma.Model) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return sigma.NewProviderError(
		model.Provider,
		sigma.APIGoogleVertex,
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		contextOverflowCause(body),
	)
}

func vertexInvalidOptions(model sigma.Model, message string, err error) error {
	if err == nil {
		err = sigma.ErrInvalidOptions
	}
	return &sigma.Error{
		Code:     sigma.ErrorInvalidOptions,
		Message:  message,
		Provider: model.Provider,
		Model:    model.ID,
		Err:      err,
	}
}
