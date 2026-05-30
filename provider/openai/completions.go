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
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamlifecycle"
)

const DefaultBaseURL = "https://api.openai.com/v1"

// Provider adapts OpenAI Chat Completions-compatible HTTP APIs to sigma.
type Provider struct {
	baseURL string
	client  *http.Client
	headers map[string]string
}

// ProviderOption configures a Provider.
type ProviderOption func(*Provider)

// NewProvider constructs an OpenAI Chat Completions-compatible provider.
func NewProvider(opts ...ProviderOption) *Provider {
	provider := &Provider{baseURL: DefaultBaseURL}
	for _, opt := range opts {
		if opt != nil {
			opt(provider)
		}
	}
	return provider
}

// WithBaseURL configures the provider base URL, for example an httptest server
// URL ending in /v1 or a local OpenAI-compatible endpoint.
func WithBaseURL(baseURL string) ProviderOption {
	return func(provider *Provider) {
		provider.baseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithHTTPClient configures the provider fallback HTTP client.
func WithHTTPClient(client *http.Client) ProviderOption {
	return func(provider *Provider) {
		provider.client = client
	}
}

// WithHeader configures a provider default request header.
func WithHeader(key, value string) ProviderOption {
	return WithHeaders(map[string]string{key: value})
}

// WithHeaders configures provider default request headers.
func WithHeaders(headers map[string]string) ProviderOption {
	return func(provider *Provider) {
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

// Register adds a Chat Completions-compatible text provider to registry.
func Register(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(providerID, NewProvider(opts...))
}

// RegisterDefault adds a Chat Completions-compatible text provider to sigma's
// default registry.
func RegisterDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(providerID, NewProvider(opts...))
}

// API reports the OpenAI Chat Completions API surface.
func (p *Provider) API() sigma.API {
	return sigma.APIOpenAICompletions
}

// Stream sends req to a Chat Completions-compatible endpoint and emits sigma
// events as SSE chunks arrive.
func (p *Provider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	ctx, stream, writer, cleanup := streamlifecycle.NewTextStream(ctx, opts)
	go func() {
		defer cleanup()
		p.run(ctx, writer, model, req, opts)
	}()
	return stream
}

func (p *Provider) run(ctx context.Context, writer sigma.StreamWriter, model sigma.Model, req sigma.Request, opts sigma.Options) {
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
		sigma.TextResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.APIOpenAICompletions, model.ID),
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

	final, err = parseCompletionsStream(ctx, body, writer, model)
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

func (p *Provider) newRequest(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Request, error) {
	baseURL := p.baseURLForModel(model, opts)
	compat := openAICompletionsCompat(model, baseURL)
	payload, err := chatCompletionsPayload(model, req, opts, compat)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai completions: encode request: %w", err)
	}

	endpoint, err := p.endpoint(model, opts)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("User-Agent", "sigma/openai-completions")

	p.addProviderHeaders(httpReq, model.Provider, opts, compat)
	for key, value := range p.headers {
		httpReq.Header.Set(key, value)
	}
	addOpenAICompatibleModelHeaders(httpReq, model)
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	if err := p.addAuthHeader(ctx, httpReq, model, opts); err != nil {
		return nil, err
	}
	if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIOpenAICompletions, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

func (p *Provider) addAuthHeader(ctx context.Context, req *http.Request, model sigma.Model, opts sigma.Options) error {
	if opts.AuthResolver == nil {
		return &sigma.Error{
			Code:     sigma.ErrorUnsupported,
			Message:  "openai completions: auth resolver is required",
			Provider: model.Provider,
			Model:    model.ID,
		}
	}
	credential, err := opts.AuthResolver.Resolve(ctx, model, opts)
	if err != nil {
		return err
	}
	if credential.Value != "" {
		req.Header.Set("Authorization", "Bearer "+credential.Value)
	}
	return nil
}

func (p *Provider) addProviderHeaders(req *http.Request, provider sigma.ProviderID, opts sigma.Options, compat completionsCompat) {
	options := providerOptions(opts, provider)
	if organization, ok := stringOption(options, providerOptionOrganization); ok {
		req.Header.Set("OpenAI-Organization", organization)
	}
	if project, ok := stringOption(options, providerOptionProject); ok {
		req.Header.Set("OpenAI-Project", project)
	}
	if opts.SessionID != "" && compat.supportsSessionAffinity {
		if header, ok := stringOption(options, providerOptionSessionHeader); ok {
			req.Header.Set(header, opts.SessionID)
		} else if header, ok := stringOption(options, providerOptionSessionHeaderGo); ok {
			req.Header.Set(header, opts.SessionID)
		}
	}
}

func (p *Provider) endpoint(model sigma.Model, opts sigma.Options) (string, error) {
	options := providerOptions(opts, model.Provider)
	if endpoint, ok := stringOption(options, providerOptionEndpoint); ok {
		return endpoint, nil
	}

	baseURL := p.baseURLForModel(model, opts)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("openai completions: invalid base URL %q", baseURL)
	}
	return baseURL + "/chat/completions", nil
}

func (p *Provider) baseURLForModel(model sigma.Model, opts sigma.Options) string {
	baseURL := openAICompatibleBaseURL(model, p.defaultBaseURL())
	options := providerOptions(opts, model.Provider)
	if value, ok := stringOption(options, providerOptionBaseURL); ok {
		baseURL = value
	} else if value, ok := stringOption(options, providerOptionBaseURLCamel); ok {
		baseURL = value
	}
	return strings.TrimRight(baseURL, "/")
}

func (p *Provider) defaultBaseURL() string {
	if p.baseURL == "" {
		return DefaultBaseURL
	}
	return p.baseURL
}

func (p *Provider) httpClient(opts sigma.Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	if p != nil && p.client != nil {
		return p.client
	}
	return http.DefaultClient
}

func responseError(resp *http.Response, model sigma.Model) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return sigma.NewProviderError(
		model.Provider,
		sigma.APIOpenAICompletions,
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		contextOverflowCause(body),
	)
}

func requestID(headers http.Header) string {
	for _, key := range []string{"x-request-id", "request-id", "openai-request-id"} {
		if value := headers.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func contextOverflowCause(body []byte) error {
	text := strings.ToLower(string(body))
	if strings.Contains(text, "context") && (strings.Contains(text, "too long") || strings.Contains(text, "maximum") || strings.Contains(text, "exceed")) {
		return sigma.ErrContextOverflow
	}
	if strings.Contains(text, "maximum prompt length") {
		return sigma.ErrContextOverflow
	}
	return nil
}

func contextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}
