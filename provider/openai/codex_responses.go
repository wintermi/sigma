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

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamlifecycle"
)

const (
	codexOptionOAuthTokenProvider   = "oauth_token_provider"
	codexOptionOAuthTokenProviderGo = "oauthTokenProvider"
)

// WithCodexResponsesOAuthTokenProvider supplies the OAuth bearer-token source
// used by the Codex Responses provider. Interactive OAuth login and device-flow
// handling are intentionally outside the core provider.
func WithCodexResponsesOAuthTokenProvider(provider sigma.ProviderID, tokenProvider sigma.OAuthTokenProvider) sigma.Option {
	return sigma.WithProviderOption(provider, codexOptionOAuthTokenProviderGo, tokenProvider)
}

// CodexResponsesProvider adapts OpenAI Codex Responses to sigma. It reuses the
// OpenAI Responses payload and SSE parsing path, but requires an explicit OAuth
// token provider instead of reading credentials from environment or global state.
type CodexResponsesProvider struct {
	base *Provider
}

// NewCodexResponsesProvider constructs an OpenAI Codex Responses provider.
func NewCodexResponsesProvider(opts ...ProviderOption) *CodexResponsesProvider {
	return &CodexResponsesProvider{base: NewProvider(opts...)}
}

// RegisterCodexResponses adds an OpenAI Codex Responses text provider to registry.
func RegisterCodexResponses(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(providerID, NewCodexResponsesProvider(opts...))
}

// RegisterCodexResponsesDefault adds an OpenAI Codex Responses text provider to
// sigma's default registry.
func RegisterCodexResponsesDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(providerID, NewCodexResponsesProvider(opts...))
}

// API reports the OpenAI Codex Responses API surface.
func (p *CodexResponsesProvider) API() sigma.API {
	return sigma.APIOpenAICodexResponses
}

// Stream sends req to the Codex Responses endpoint and emits sigma events as
// SSE chunks arrive.
func (p *CodexResponsesProvider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	ctx, stream, writer, cleanup := streamlifecycle.NewTextStream(ctx, opts)
	go func() {
		defer cleanup()
		p.run(ctx, writer, model, req, opts)
	}()
	return stream
}

func (p *CodexResponsesProvider) run(ctx context.Context, writer sigma.StreamWriter, model sigma.Model, req sigma.Request, opts sigma.Options) {
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
		providerErr := codexResponsesResponseError(resp, model)
		final.StopReason = sigma.StopReasonError
		final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		_ = writer.Error(ctx, providerErr, final)
		return
	}

	final, err = parseResponsesStream(ctx, body, writer, model)
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

func (p *CodexResponsesProvider) do(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Response, error) {
	return sigma.DoHTTPWithRetry(
		ctx,
		p.base.httpClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return p.newRequest(ctx, model, req, opts)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return codexResponsesResponseError(resp, model)
		},
		sigma.TextResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.APIOpenAICodexResponses, model.ID),
	)
}

func (p *CodexResponsesProvider) newRequest(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Request, error) {
	if err := validateCodexResponsesTransport(model, opts.Transport); err != nil {
		return nil, err
	}
	payload, err := responsesPayload(model, req, opts)
	if err != nil {
		return nil, err
	}
	if model.OpenAICodexResponses != nil && model.OpenAICodexResponses.Model != "" {
		payload["model"] = model.OpenAICodexResponses.Model
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai codex responses: encode request: %w", err)
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
	httpReq.Header.Set("User-Agent", "sigma/openai-codex-responses")

	if err := p.addAuthHeader(ctx, httpReq, model, opts); err != nil {
		return nil, err
	}
	p.addProviderHeaders(httpReq, model.Provider, opts)
	for key, value := range p.base.headers {
		httpReq.Header.Set(key, value)
	}
	addOpenAICompatibleModelHeaders(httpReq, model)
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIOpenAICodexResponses, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

func (p *CodexResponsesProvider) addAuthHeader(ctx context.Context, req *http.Request, model sigma.Model, opts sigma.Options) error {
	tokenProvider := codexResponsesOAuthTokenProvider(model.Provider, opts)
	if tokenProvider == nil {
		return &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"oauth-token-provider"},
		}
	}

	credential, err := tokenProvider.Token(ctx, model, opts)
	if err != nil {
		if errors.Is(err, sigma.ErrCredentialUnavailable) {
			return err
		}
		return &sigma.Error{
			Code:     sigma.ErrorUnsupported,
			Message:  "openai codex responses: oauth token provider failed",
			Provider: model.Provider,
			Model:    model.ID,
			Err:      err,
		}
	}
	if credential.Value == "" {
		return &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"oauth-token-provider"},
		}
	}
	req.Header.Set("Authorization", "Bearer "+credential.Value)
	return nil
}

func (p *CodexResponsesProvider) addProviderHeaders(req *http.Request, provider sigma.ProviderID, opts sigma.Options) {
	options := providerOptions(opts, provider)
	if organization, ok := stringOption(options, providerOptionOrganization); ok {
		req.Header.Set("OpenAI-Organization", organization)
	}
	if project, ok := stringOption(options, providerOptionProject); ok {
		req.Header.Set("OpenAI-Project", project)
	}
	if opts.SessionID != "" {
		if header, ok := stringOption(options, providerOptionSessionHeader); ok {
			req.Header.Set(header, opts.SessionID)
		} else if header, ok := stringOption(options, providerOptionSessionHeaderGo); ok {
			req.Header.Set(header, opts.SessionID)
		}
	}
}

func (p *CodexResponsesProvider) endpoint(model sigma.Model, opts sigma.Options) (string, error) {
	responses := ResponsesProvider{base: p.base}
	endpoint, err := responses.endpoint(model, opts)
	if err != nil {
		return "", fmt.Errorf("openai codex responses: %w", err)
	}
	return endpoint, nil
}

func codexResponsesOAuthTokenProvider(provider sigma.ProviderID, opts sigma.Options) sigma.OAuthTokenProvider {
	options := providerOptions(opts, provider)
	if tokenProvider, ok := options[codexOptionOAuthTokenProviderGo].(sigma.OAuthTokenProvider); ok {
		return tokenProvider
	}
	if tokenProvider, ok := options[codexOptionOAuthTokenProvider].(sigma.OAuthTokenProvider); ok {
		return tokenProvider
	}
	return nil
}

func validateCodexResponsesTransport(model sigma.Model, transport sigma.Transport) error {
	switch transport {
	case "", sigma.TransportSSE:
		return nil
	case sigma.TransportHTTP:
		return codexResponsesTransportError(model, `openai codex responses: transport "http" is not supported; use "sse" for streaming`)
	case sigma.TransportWebSocket:
		return codexResponsesTransportError(model, `openai codex responses: transport "websocket" is not implemented`)
	default:
		return codexResponsesTransportError(model, fmt.Sprintf("openai codex responses: unsupported transport %q", transport))
	}
}

func codexResponsesTransportError(model sigma.Model, message string) error {
	return &sigma.Error{
		Code:     sigma.ErrorInvalidOptions,
		Message:  message,
		Provider: model.Provider,
		Model:    model.ID,
	}
}

func codexResponsesResponseError(resp *http.Response, model sigma.Model) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return sigma.NewProviderError(
		model.Provider,
		sigma.APIOpenAICodexResponses,
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		nil,
	)
}
