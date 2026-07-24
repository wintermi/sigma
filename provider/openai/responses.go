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

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamlifecycle"
)

// ResponsesProvider adapts the OpenAI Responses API to sigma.
type ResponsesProvider struct {
	base *Provider
}

// NewResponsesProvider constructs an OpenAI Responses API provider.
func NewResponsesProvider(opts ...ProviderOption) *ResponsesProvider {
	return &ResponsesProvider{base: NewProvider(opts...)}
}

// RegisterResponses adds an OpenAI Responses API text provider to registry.
func RegisterResponses(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(providerID, NewResponsesProvider(opts...))
}

// RegisterResponsesDefault adds an OpenAI Responses API text provider to
// sigma's default registry.
func RegisterResponsesDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(providerID, NewResponsesProvider(opts...))
}

// API reports the OpenAI Responses API surface.
func (p *ResponsesProvider) API() sigma.API {
	return sigma.APIOpenAIResponses
}

// Stream sends req to the Responses endpoint and emits sigma events as SSE
// chunks arrive.
func (p *ResponsesProvider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	ctx, stream, writer, cleanup := streamlifecycle.NewTextStream(ctx, opts)
	go func() {
		defer cleanup()
		p.run(ctx, writer, model, req, opts)
	}()
	return stream
}

func (p *ResponsesProvider) run(ctx context.Context, writer sigma.StreamWriter, model sigma.Model, req sigma.Request, opts sigma.Options) {
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
		providerErr := responsesResponseError(resp, model)
		final.StopReason = sigma.StopReasonError
		final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		_ = writer.Error(ctx, providerErr, final)
		return
	}

	streamOptions, err := openAIResponsesStreamOptions(model, req, opts)
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

func (p *ResponsesProvider) do(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Response, error) {
	return sigma.DoHTTPWithRetry(
		ctx,
		p.base.httpClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return p.newRequest(ctx, model, req, opts)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return responsesResponseError(resp, model)
		},
		sigma.TextResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.APIOpenAIResponses, model.ID),
	)
}

func (p *ResponsesProvider) newRequest(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Request, error) {
	opts, credential, err := p.base.resolveAuth(ctx, model, opts)
	if err != nil {
		return nil, err
	}
	payload, err := responsesPayload(model, req, opts)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai responses: encode request: %w", err)
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
	httpReq.Header.Set("User-Agent", "sigma/openai-responses")

	p.base.addAuthCredentialHeader(httpReq, model, credential)
	p.addProviderHeaders(httpReq, model, opts)
	for key, value := range p.base.headers {
		httpReq.Header.Set(key, value)
	}
	addOpenAICompatibleModelHeaders(httpReq, model)
	addCopilotDynamicHeaders(httpReq, model, req)
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	sigma.ApplySuppressedHeaders(httpReq.Header, opts)
	if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIOpenAIResponses, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

func (p *ResponsesProvider) addProviderHeaders(req *http.Request, model sigma.Model, opts sigma.Options) {
	options := providerOptions(opts, model.Provider)
	if organization, ok := stringOption(options, providerOptionOrganization); ok {
		req.Header.Set("OpenAI-Organization", organization)
	}
	if project, ok := stringOption(options, providerOptionProject); ok {
		req.Header.Set("OpenAI-Project", project)
	}
	if opts.SessionID == "" {
		return
	}
	if header, ok := stringOption(options, providerOptionSessionHeader); ok {
		req.Header.Set(header, opts.SessionID)
		return
	}
	if header, ok := stringOption(options, providerOptionSessionHeaderGo); ok {
		req.Header.Set(header, opts.SessionID)
		return
	}
	if !opts.CacheRetention.CacheEnabled() {
		return
	}
	if model.OpenAIResponsesCompat == nil ||
		model.OpenAIResponsesCompat.SessionAffinityFormat != sigma.OpenAIResponsesSessionAffinityOpenAINoSession {
		req.Header.Set("session_id", opts.SessionID)
	}
	req.Header.Set("x-client-request-id", opts.SessionID)
}

func (p *ResponsesProvider) endpoint(model sigma.Model, opts sigma.Options) (string, error) {
	options := providerOptions(opts, model.Provider)
	if endpoint, ok := stringOption(options, providerOptionEndpoint); ok {
		return endpoint, nil
	}

	baseURL := p.base.baseURLForModel(model, opts)
	resolved, err := resolveCloudflareBaseURL(model.Provider, baseURL, opts)
	if err != nil {
		return "", err
	}
	baseURL = resolved
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("openai responses: invalid base URL %q", baseURL)
	}
	return baseURL + "/responses", nil
}

func responsesResponseError(resp *http.Response, model sigma.Model) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return sigma.NewProviderError(
		model.Provider,
		sigma.APIOpenAIResponses,
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		nil,
	)
}
