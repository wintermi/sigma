// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral

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

const DefaultBaseURL = "https://api.mistral.ai/v1"

// PayloadHook can inspect or mutate the Mistral request payload before it is encoded.
type PayloadHook func(context.Context, sigma.Model, sigma.Request, sigma.Options, map[string]any) error

// ResponseHook can inspect the Mistral HTTP response before status handling or stream parsing.
type ResponseHook func(context.Context, sigma.Model, sigma.Options, *http.Response) error

// Provider adapts the Mistral Conversations API to sigma.
//
// This provider currently implements model-backed text conversations with
// function tools, thinking chunks, and session affinity. Image inputs, built-in
// connectors, append, and restart are intentionally unsupported by this adapter.
type Provider struct {
	baseURL       string
	client        *http.Client
	headers       map[string]string
	payloadHooks  []PayloadHook
	responseHooks []ResponseHook
}

// ProviderOption configures a Provider.
type ProviderOption func(*Provider)

// NewProvider constructs a Mistral Conversations provider.
func NewProvider(opts ...ProviderOption) *Provider {
	provider := &Provider{baseURL: DefaultBaseURL}
	for _, opt := range opts {
		if opt != nil {
			opt(provider)
		}
	}
	return provider
}

// WithBaseURL configures the provider base URL, for example an httptest server URL.
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

// WithPayloadHook adds a request payload hook.
func WithPayloadHook(hook PayloadHook) ProviderOption {
	return func(provider *Provider) {
		if hook != nil {
			provider.payloadHooks = append(provider.payloadHooks, hook)
		}
	}
}

// WithResponseHook adds an HTTP response hook.
func WithResponseHook(hook ResponseHook) ProviderOption {
	return func(provider *Provider) {
		if hook != nil {
			provider.responseHooks = append(provider.responseHooks, hook)
		}
	}
}

// Register adds a Mistral Conversations text provider to registry.
func Register(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(providerID, NewProvider(opts...))
}

// RegisterDefault adds a Mistral Conversations text provider to sigma's default registry.
func RegisterDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(providerID, NewProvider(opts...))
}

// API reports the Mistral Conversations API surface.
func (p *Provider) API() sigma.API {
	return sigma.APIMistralConversations
}

// Stream sends req to the Mistral Conversations API and emits sigma events as SSE chunks arrive.
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
		providerErr := responseError(resp, model)
		final.StopReason = sigma.StopReasonError
		final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		_ = writer.Error(ctx, providerErr, final)
		return
	}

	final, err = parseConversationStream(ctx, body, writer, model)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			final.StopReason = sigma.StopReasonAborted
			_ = writer.Error(ctx, contextError(ctx, err), final)
			return
		}
		final.StopReason = sigma.StopReasonError
		var providerErr *sigma.ProviderError
		if errors.As(err, &providerErr) {
			final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		}
		_ = writer.Error(ctx, err, final)
		return
	}
	_ = writer.Done(ctx, final)
}

func (p *Provider) do(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Response, error) {
	return sigma.DoHTTPWithRetry(
		ctx,
		p.httpClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return p.newRequest(ctx, model, req, opts)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return responseError(resp, model)
		},
		func(resp *http.Response) error {
			for _, hook := range p.responseHooks {
				if err := hook(ctx, model, opts, resp); err != nil {
					return err
				}
			}
			return nil
		},
		sigma.TextResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.APIMistralConversations, model.ID),
	)
}

func (p *Provider) newRequest(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (*http.Request, error) {
	payload, err := conversationPayload(model, req, opts)
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
		return nil, fmt.Errorf("mistral conversations: encode request: %w", err)
	}

	endpoint, err := p.endpoint(model.Provider, opts)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("User-Agent", "sigma/mistral-conversations")

	if err := p.addAuthHeader(ctx, httpReq, model, opts); err != nil {
		return nil, err
	}
	for key, value := range p.headers {
		httpReq.Header.Set(key, value)
	}
	if opts.SessionID != "" && httpReq.Header.Get("x-affinity") == "" && !hasHeader(opts.Headers, "x-affinity") {
		httpReq.Header.Set("x-affinity", opts.SessionID)
	}
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIMistralConversations, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

func (p *Provider) addAuthHeader(ctx context.Context, req *http.Request, model sigma.Model, opts sigma.Options) error {
	if opts.AuthResolver == nil {
		return &sigma.Error{
			Code:     sigma.ErrorUnsupported,
			Message:  "mistral conversations: auth resolver is required",
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

func (p *Provider) endpoint(provider sigma.ProviderID, opts sigma.Options) (string, error) {
	options := providerOptions(opts, provider)
	if endpoint, ok := stringOption(options, providerOptionEndpoint); ok {
		return endpoint, nil
	}

	baseURL := p.baseURLForProvider(provider, opts)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("mistral conversations: invalid base URL %q", baseURL)
	}
	return baseURL + "/conversations", nil
}

func (p *Provider) baseURLForProvider(provider sigma.ProviderID, opts sigma.Options) string {
	baseURL := p.baseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	options := providerOptions(opts, provider)
	if value, ok := stringOption(options, providerOptionBaseURL); ok {
		baseURL = value
	} else if value, ok := stringOption(options, providerOptionBaseURLCamel); ok {
		baseURL = value
	}
	return strings.TrimRight(baseURL, "/")
}

func (p *Provider) httpClient(opts sigma.Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	if p.client != nil {
		return p.client
	}
	return http.DefaultClient
}

func hasHeader(headers map[string]string, key string) bool {
	for header := range headers {
		if strings.EqualFold(header, key) {
			return true
		}
	}
	return false
}

func responseError(resp *http.Response, model sigma.Model) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return sigma.NewProviderError(
		model.Provider,
		sigma.APIMistralConversations,
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		contextOverflowCause(body),
	)
}

func requestID(header http.Header) string {
	for _, key := range []string{"x-request-id", "x-mistral-request-id", "request-id"} {
		if value := header.Get(key); value != "" {
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
	return nil
}

func contextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return &sigma.Error{Code: sigma.ErrorAborted, Message: ctxErr.Error(), Err: ctxErr}
	}
	return &sigma.Error{Code: sigma.ErrorAborted, Message: err.Error(), Err: err}
}
