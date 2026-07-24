// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamlifecycle"
)

const (
	azureOptionEndpoint           = "azure_endpoint"
	azureOptionEndpointGo         = "azureEndpoint"
	azureOptionDeployment         = "azure_deployment"
	azureOptionDeploymentGo       = "azureDeployment"
	azureOptionAPIVersion         = "azure_api_version"
	azureOptionAPIVersionGo       = "azureAPIVersion"
	azureOptionCredentialSource   = "azure_credential_source"
	azureOptionCredentialSourceGo = "azureCredentialSource"
	azureOptionTokenCredential    = "azure_token_credential"
	azureOptionTokenCredentialGo  = "azureTokenCredential"

	// AzureCredentialSourceAPIKey documents API-key authentication.
	AzureCredentialSourceAPIKey = "api-key"
	// AzureCredentialSourceToken documents Microsoft Entra token authentication.
	AzureCredentialSourceToken = "token"

	defaultAzureAPIKeyEnvVar = "AZURE_OPENAI_API_KEY"
	defaultAzureTokenScope   = "https://cognitiveservices.azure.com/.default"
)

// AzureAccessToken is the minimal token shape needed by Azure Responses auth.
type AzureAccessToken struct {
	Token     string
	ExpiresOn time.Time
}

// AzureTokenRequest describes the token request made by AzureResponsesProvider.
type AzureTokenRequest struct {
	Scopes []string
}

// AzureTokenCredential is a narrow adapter interface for Microsoft Entra ID
// token sources. Azure SDK credentials can be wrapped to this interface without
// importing Azure SDK packages into sigma.
type AzureTokenCredential interface {
	GetAzureToken(context.Context, AzureTokenRequest) (AzureAccessToken, error)
}

// AzureTokenCredentialFunc adapts a function into AzureTokenCredential.
type AzureTokenCredentialFunc func(context.Context, AzureTokenRequest) (AzureAccessToken, error)

// GetAzureToken calls f.
func (f AzureTokenCredentialFunc) GetAzureToken(ctx context.Context, req AzureTokenRequest) (AzureAccessToken, error) {
	if f == nil {
		return AzureAccessToken{}, sigma.ErrCredentialUnavailable
	}
	return f(ctx, req)
}

// WithAzureResponsesEndpoint overrides the Azure OpenAI resource endpoint for a
// request. The provider appends /openai/v1/responses and api-version.
func WithAzureResponsesEndpoint(provider sigma.ProviderID, endpoint string) sigma.Option {
	return sigma.WithProviderOption(provider, azureOptionEndpointGo, endpoint)
}

// WithAzureResponsesDeployment overrides the Azure OpenAI deployment name sent
// as the Responses model for a request.
func WithAzureResponsesDeployment(provider sigma.ProviderID, deployment string) sigma.Option {
	return sigma.WithProviderOption(provider, azureOptionDeploymentGo, deployment)
}

// WithAzureResponsesAPIVersion overrides the api-version query parameter for a
// request. Azure currently documents v1 as the default, but sigma requires an
// explicit value so configuration drift is visible in tests and diagnostics.
func WithAzureResponsesAPIVersion(provider sigma.ProviderID, apiVersion string) sigma.Option {
	return sigma.WithProviderOption(provider, azureOptionAPIVersionGo, apiVersion)
}

// WithAzureResponsesCredentialSource documents the expected auth path for a
// request. Supported values are AzureCredentialSourceAPIKey and
// AzureCredentialSourceToken.
func WithAzureResponsesCredentialSource(provider sigma.ProviderID, source string) sigma.Option {
	return sigma.WithProviderOption(provider, azureOptionCredentialSourceGo, source)
}

// WithAzureResponsesTokenCredential supplies a request-scoped Microsoft Entra
// token source. API-key auth can use sigma.WithAPIKey, AZURE_OPENAI_API_KEY, or
// a normal sigma.AuthResolver.
func WithAzureResponsesTokenCredential(provider sigma.ProviderID, credential AzureTokenCredential) sigma.Option {
	return sigma.WithProviderOption(provider, azureOptionTokenCredentialGo, credential)
}

// AzureResponsesProvider adapts Azure OpenAI Responses to sigma.
type AzureResponsesProvider struct {
	base *Provider
}

// NewAzureResponsesProvider constructs an Azure OpenAI Responses provider.
func NewAzureResponsesProvider(opts ...ProviderOption) *AzureResponsesProvider {
	return &AzureResponsesProvider{base: NewProvider(opts...)}
}

// RegisterAzureResponses adds an Azure OpenAI Responses text provider to registry.
func RegisterAzureResponses(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(providerID, NewAzureResponsesProvider(opts...))
}

// RegisterAzureResponsesDefault adds an Azure OpenAI Responses text provider to
// sigma's default registry.
func RegisterAzureResponsesDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(providerID, NewAzureResponsesProvider(opts...))
}

// API reports the Azure OpenAI Responses API surface.
func (p *AzureResponsesProvider) API() sigma.API {
	return sigma.APIAzureOpenAIResponses
}

// Stream sends req to the Azure Responses endpoint and emits sigma events as
// SSE chunks arrive.
func (p *AzureResponsesProvider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	ctx, stream, writer, cleanup := streamlifecycle.NewTextStream(ctx, opts)
	go func() {
		defer cleanup()
		p.run(ctx, writer, model, req, opts)
	}()
	return stream
}

func (p *AzureResponsesProvider) run(ctx context.Context, writer sigma.StreamWriter, model sigma.Model, req sigma.Request, opts sigma.Options) {
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
		providerErr := azureResponsesResponseError(resp, model)
		final.StopReason = sigma.StopReasonError
		final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		_ = writer.Error(ctx, providerErr, final)
		return
	}

	streamOptions, err := responsesStreamOptionsForRequest(model, req, opts, responsesStreamOptions{})
	if err != nil {
		_ = writer.Error(ctx, err, final)
		return
	}
	final, err = parseResponsesStream(ctx, body, writer, model, streamOptions)
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

func (p *AzureResponsesProvider) do(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Response, error) {
	return sigma.DoHTTPWithRetry(
		ctx,
		p.base.httpClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return p.newRequest(ctx, model, req, opts)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return azureResponsesResponseError(resp, model)
		},
		sigma.TextResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.APIAzureOpenAIResponses, model.ID),
	)
}

func (p *AzureResponsesProvider) newRequest(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Request, error) {
	config, err := p.config(model, opts)
	if err != nil {
		return nil, err
	}
	payload, err := responsesPayload(model, req, opts)
	if err != nil {
		return nil, err
	}
	payload["model"] = config.deployment
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("azure openai responses: encode request: %w", err)
	}

	endpoint, err := azureResponsesEndpoint(config)
	if err != nil {
		return nil, azureConfigError(model, err.Error())
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("User-Agent", "sigma/azure-openai-responses")

	if err := p.addAuthHeader(ctx, httpReq, model, opts, config); err != nil {
		return nil, err
	}
	for key, value := range p.base.headers {
		httpReq.Header.Set(key, value)
	}
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	sigma.ApplySuppressedHeaders(httpReq.Header, opts)
	if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIAzureOpenAIResponses, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

type azureResponsesConfig struct {
	endpoint         string
	deployment       string
	apiVersion       string
	apiKeyEnvVar     string
	credentialSource string
	tokenCredential  AzureTokenCredential
}

func (p *AzureResponsesProvider) config(model sigma.Model, opts sigma.Options) (azureResponsesConfig, error) {
	config := azureResponsesConfig{}
	if model.AzureOpenAIResponses != nil {
		config.endpoint = model.AzureOpenAIResponses.Endpoint
		config.deployment = model.AzureOpenAIResponses.Deployment
		config.apiVersion = model.AzureOpenAIResponses.APIVersion
		config.apiKeyEnvVar = model.AzureOpenAIResponses.APIKeyEnvVar
		config.credentialSource = model.AzureOpenAIResponses.CredentialSource
	}
	if config.endpoint == "" && p != nil && p.base != nil && p.base.baseURL != DefaultBaseURL {
		config.endpoint = p.base.baseURL
	}

	options := providerOptions(opts, model.Provider)
	if endpoint, ok := stringOption(options, azureOptionEndpoint); ok {
		config.endpoint = endpoint
	} else if endpoint, ok := stringOption(options, azureOptionEndpointGo); ok {
		config.endpoint = endpoint
	}
	if deployment, ok := stringOption(options, azureOptionDeployment); ok {
		config.deployment = deployment
	} else if deployment, ok := stringOption(options, azureOptionDeploymentGo); ok {
		config.deployment = deployment
	}
	if apiVersion, ok := stringOption(options, azureOptionAPIVersion); ok {
		config.apiVersion = apiVersion
	} else if apiVersion, ok := stringOption(options, azureOptionAPIVersionGo); ok {
		config.apiVersion = apiVersion
	}
	if source, ok := stringOption(options, azureOptionCredentialSource); ok {
		config.credentialSource = source
	} else if source, ok := stringOption(options, azureOptionCredentialSourceGo); ok {
		config.credentialSource = source
	}
	if credential, ok := options[azureOptionTokenCredential].(AzureTokenCredential); ok {
		config.tokenCredential = credential
	} else if credential, ok := options[azureOptionTokenCredentialGo].(AzureTokenCredential); ok {
		config.tokenCredential = credential
	}

	config.endpoint = strings.TrimSpace(config.endpoint)
	config.deployment = strings.TrimSpace(config.deployment)
	config.apiVersion = strings.TrimSpace(config.apiVersion)
	config.apiKeyEnvVar = strings.TrimSpace(config.apiKeyEnvVar)
	config.credentialSource = strings.TrimSpace(config.credentialSource)
	if config.apiKeyEnvVar == "" {
		config.apiKeyEnvVar = defaultAzureAPIKeyEnvVar
	}

	switch {
	case config.endpoint == "":
		return azureResponsesConfig{}, azureConfigError(model, "azure openai responses: endpoint is required")
	case config.deployment == "":
		return azureResponsesConfig{}, azureConfigError(model, "azure openai responses: deployment is required")
	case config.apiVersion == "":
		return azureResponsesConfig{}, azureConfigError(model, "azure openai responses: api version is required")
	default:
		return config, nil
	}
}

func azureResponsesEndpoint(config azureResponsesConfig) (string, error) {
	parsed, err := url.Parse(config.endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid endpoint %q", config.endpoint)
	}
	parsed.Path = azureResponsesPath(parsed)
	query := parsed.Query()
	query.Set("api-version", config.apiVersion)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// azureResponsesPath appends /openai/v1/responses to the configured endpoint
// path unless the path already ends with those segments, so gateway prefixes
// (for example /aoai on an APIM front) are preserved regardless of hostname.
func azureResponsesPath(parsed *url.URL) string {
	clean := "/" + strings.Trim(path.Clean("/"+strings.TrimSpace(parsed.Path)), "/")
	clean = strings.TrimSuffix(clean, "/responses")
	if clean == "/openai" {
		clean = "/openai/v1"
	}
	if !strings.HasSuffix(clean, "/openai/v1") {
		clean = path.Join(clean, "openai", "v1")
	}
	return path.Join(clean, "responses")
}

func (p *AzureResponsesProvider) addAuthHeader(ctx context.Context, req *http.Request, model sigma.Model, opts sigma.Options, config azureResponsesConfig) error {
	if config.tokenCredential != nil {
		token, err := config.tokenCredential.GetAzureToken(ctx, AzureTokenRequest{Scopes: []string{defaultAzureTokenScope}})
		if err != nil {
			return err
		}
		if token.Token != "" {
			req.Header.Set("Authorization", "Bearer "+token.Token)
		}
		return nil
	}
	if opts.AuthResolver == nil {
		return azureConfigError(model, "azure openai responses: auth resolver is required")
	}

	authModel := azureAuthModel(model, config)
	credential, err := opts.AuthResolver.Resolve(ctx, authModel, opts)
	if err != nil {
		return err
	}
	switch {
	case credential.Type == sigma.CredentialTypeOAuthToken ||
		credential.Type == sigma.CredentialTypeCloudCredential ||
		credential.Type == "" && config.credentialSource == AzureCredentialSourceToken:
		if credential.Value != "" {
			req.Header.Set("Authorization", "Bearer "+credential.Value)
		}
	case credential.Value != "":
		req.Header.Set("api-key", credential.Value)
	}
	return nil
}

func azureAuthModel(model sigma.Model, config azureResponsesConfig) sigma.Model {
	if config.apiKeyEnvVar == "" {
		return model
	}
	metadata := make(map[string]any, len(model.ProviderMetadata)+1)
	for key, value := range model.ProviderMetadata {
		metadata[key] = value
	}
	metadata[sigma.MetadataAPIKeyEnvVars] = []string{config.apiKeyEnvVar, defaultAzureAPIKeyEnvVar}
	model.ProviderMetadata = metadata
	return model
}

func azureConfigError(model sigma.Model, message string) error {
	return &sigma.Error{
		Code:     sigma.ErrorInvalidOptions,
		Message:  message,
		Provider: model.Provider,
		Model:    model.ID,
	}
}

func azureResponsesResponseError(resp *http.Response, model sigma.Model) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return sigma.NewProviderError(
		model.Provider,
		sigma.APIAzureOpenAIResponses,
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		nil,
	)
}
