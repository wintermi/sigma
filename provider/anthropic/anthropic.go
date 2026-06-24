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
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamlifecycle"
)

const (
	DefaultBaseURL               = "https://api.anthropic.com/v1"
	defaultVersion               = "2023-06-01"
	fineGrainedToolStreamingBeta = "fine-grained-tool-streaming-2025-05-14"
	interleavedThinkingBeta      = "interleaved-thinking-2025-05-14"
	defaultSessionAffinityHeader = "x-session-affinity"
)

var cloudflareAIGatewayBaseURLVariable = regexp.MustCompile(`\{([A-Z_][A-Z0-9_]*)\}`)

const (
	cloudflareAIGatewayAccountIDOption = "cloudflare_ai_gateway_account_id"
	cloudflareAIGatewayIDOption        = "cloudflare_ai_gateway_id"
)

// Provider adapts Anthropic Messages-compatible HTTP APIs to sigma.
type Provider struct {
	baseURL string
	client  *http.Client
	headers map[string]string
	compat  *MessagesCompat
}

// ProviderOption configures a Provider.
type ProviderOption func(*Provider)

// NewProvider constructs an Anthropic Messages-compatible provider.
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
// URL ending in /v1 or an Anthropic-compatible endpoint.
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

// WithMessagesCompat overrides detected Anthropic-compatible endpoint behavior.
func WithMessagesCompat(compat MessagesCompat) ProviderOption {
	return func(provider *Provider) {
		provider.compat = &compat
	}
}

// Register adds an Anthropic Messages-compatible text provider to registry.
func Register(registry *sigma.Registry, providerID sigma.ProviderID, opts ...ProviderOption) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	return registry.RegisterTextProvider(providerID, NewProvider(opts...))
}

// RegisterDefault adds an Anthropic Messages-compatible text provider to
// sigma's default registry.
func RegisterDefault(providerID sigma.ProviderID, opts ...ProviderOption) error {
	return sigma.RegisterDefaultTextProvider(providerID, NewProvider(opts...))
}

// API reports the Anthropic Messages API surface.
func (p *Provider) API() sigma.API {
	return sigma.APIAnthropicMessages
}

// Stream sends req to a Messages-compatible endpoint and emits sigma events as
// SSE chunks arrive.
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

	credential, err := p.resolveCredential(ctx, model, opts)
	if err != nil {
		_ = writer.Error(ctx, err, final)
		return
	}
	claudeCode := isAnthropicOAuthCredential(credential)

	resp, err := p.do(ctx, model, req, opts, credential, claudeCode)
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

	compat := anthropicMessagesCompat(model, p.baseURLForProvider(model.Provider, opts), p.compat)
	compat.claudeCodeIdentity = claudeCode
	final, err = parseMessagesStream(ctx, body, writer, model, compat, req.Tools)
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

func (p *Provider) do(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options, credential sigma.Credential, claudeCode bool) (*http.Response, error) {
	return sigma.DoHTTPWithRetry(
		ctx,
		p.httpClient(opts),
		opts,
		func(ctx context.Context) (*http.Request, error) {
			return p.newRequest(ctx, model, req, opts, credential, claudeCode)
		},
		func(resp *http.Response) *sigma.ProviderError {
			return responseError(resp, model)
		},
		sigma.TextResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.APIAnthropicMessages, model.ID),
	)
}

func (p *Provider) newRequest(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options, credential sigma.Credential, claudeCode bool) (*http.Request, error) {
	baseURL := p.baseURLForModel(model, opts)
	compat := anthropicMessagesCompat(model, baseURL, p.compat)
	compat.claudeCodeIdentity = claudeCode
	payload, err := messagesPayload(model, req, opts, compat)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("anthropic messages: encode request: %w", err)
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
	httpReq.Header.Set("User-Agent", "sigma/anthropic-messages")
	httpReq.Header.Set("Anthropic-Version", anthropicVersion(model.Provider, opts))
	if compat.claudeCodeIdentity {
		httpReq.Header.Set("User-Agent", claudeCodeUserAgent)
		httpReq.Header.Set("X-App", "cli")
	}
	if beta := anthropicBeta(model, opts, compat, len(req.Tools) > 0); beta != "" {
		httpReq.Header.Set("Anthropic-Beta", beta)
	}

	applyAuthHeader(httpReq, model.Provider, credential)
	p.addProviderHeaders(httpReq, model.Provider, opts, compat)
	for key, value := range p.headers {
		httpReq.Header.Set(key, value)
	}
	addCopilotDynamicHeaders(httpReq, model, req)
	for key, value := range anthropicModelHeaders(model) {
		if unsafeCredentialHeader(key) {
			continue
		}
		httpReq.Header.Set(key, value)
	}
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	sigma.ApplySuppressedHeaders(httpReq.Header, opts)
	if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIAnthropicMessages, model.ID, body, httpReq.Header); err != nil {
		return nil, err
	}
	return httpReq, nil
}

func (p *Provider) resolveCredential(ctx context.Context, model sigma.Model, opts sigma.Options) (sigma.Credential, error) {
	if opts.AuthResolver == nil {
		return sigma.Credential{}, &sigma.Error{
			Code:     sigma.ErrorUnsupported,
			Message:  "anthropic messages: auth resolver is required",
			Provider: model.Provider,
			Model:    model.ID,
		}
	}
	return opts.AuthResolver.Resolve(ctx, model, opts)
}

func applyAuthHeader(req *http.Request, provider sigma.ProviderID, credential sigma.Credential) {
	if credential.Value == "" {
		return
	}
	if provider == sigma.ProviderCloudflareAIGateway {
		req.Header.Set("cf-aig-authorization", "Bearer "+credential.Value)
		return
	}
	if provider == sigma.ProviderGitHubCopilot {
		req.Header.Set("Authorization", "Bearer "+credential.Value)
		return
	}
	if credential.Type == sigma.CredentialTypeOAuthToken || strings.Contains(credential.Value, anthropicOAuthTokenMark) {
		req.Header.Set("Authorization", "Bearer "+credential.Value)
		return
	}
	req.Header.Set("X-Api-Key", credential.Value)
}

func (p *Provider) addProviderHeaders(req *http.Request, provider sigma.ProviderID, opts sigma.Options, compat messagesCompat) {
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

func (p *Provider) endpoint(model sigma.Model, opts sigma.Options) (string, error) {
	options := providerOptions(opts, model.Provider)
	if endpoint, ok := stringOption(options, providerOptionEndpoint); ok {
		return endpoint, nil
	}

	baseURL := p.baseURLForModel(model, opts)
	resolved, err := resolveCloudflareAIGatewayBaseURL(model.Provider, baseURL, opts)
	if err != nil {
		return "", err
	}
	baseURL = resolved
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("anthropic messages: invalid base URL %q", baseURL)
	}
	return baseURL + "/messages", nil
}

func (p *Provider) baseURLForModel(model sigma.Model, opts sigma.Options) string {
	baseURL := p.baseURLForProvider(model.Provider, opts)
	if value, ok := model.ProviderMetadata["baseURL"].(string); ok && strings.TrimSpace(value) != "" {
		baseURL = value
	}
	options := providerOptions(opts, model.Provider)
	if value, ok := stringOption(options, providerOptionBaseURL); ok {
		baseURL = value
	} else if value, ok := stringOption(options, providerOptionBaseURLCamel); ok {
		baseURL = value
	}
	return strings.TrimRight(baseURL, "/")
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

func addCopilotDynamicHeaders(req *http.Request, model sigma.Model, request sigma.Request) {
	if model.Provider != sigma.ProviderGitHubCopilot {
		return
	}
	req.Header.Set("X-Initiator", copilotInitiator(request.Messages))
	req.Header.Set("Openai-Intent", "conversation-edits")
	if hasCopilotVisionInput(request.Messages) {
		req.Header.Set("Copilot-Vision-Request", "true")
	}
}

func copilotInitiator(messages []sigma.Message) string {
	if len(messages) == 0 || messages[len(messages)-1].Role == sigma.RoleUser {
		return "user"
	}
	return "agent"
}

func hasCopilotVisionInput(messages []sigma.Message) bool {
	for _, message := range messages {
		if message.Role != sigma.RoleUser && message.Role != sigma.RoleTool {
			continue
		}
		for _, block := range message.Content {
			if block.Type == sigma.ContentBlockImage {
				return true
			}
		}
	}
	return false
}

func resolveCloudflareAIGatewayBaseURL(provider sigma.ProviderID, baseURL string, opts sigma.Options) (string, error) {
	if provider != sigma.ProviderCloudflareAIGateway {
		return baseURL, nil
	}
	var missing string
	resolved := cloudflareAIGatewayBaseURLVariable.ReplaceAllStringFunc(baseURL, func(match string) string {
		if missing != "" {
			return match
		}
		name := strings.Trim(match, "{}")
		value := cloudflareAIGatewayBaseURLVariableValue(provider, opts, name)
		if value == "" {
			missing = name
			return match
		}
		return value
	})
	if missing != "" {
		return "", fmt.Errorf("anthropic messages: %s is required for Cloudflare base URL", missing)
	}
	return resolved, nil
}

func cloudflareAIGatewayBaseURLVariableValue(provider sigma.ProviderID, opts sigma.Options, name string) string {
	options := providerOptions(opts, provider)
	switch name {
	case "CLOUDFLARE_ACCOUNT_ID":
		if value, ok := stringOption(options, cloudflareAIGatewayAccountIDOption); ok {
			return value
		}
	case "CLOUDFLARE_GATEWAY_ID":
		if value, ok := stringOption(options, cloudflareAIGatewayIDOption); ok {
			return value
		}
	}
	return os.Getenv(name)
}

func anthropicModelHeaders(model sigma.Model) map[string]string {
	raw := model.ProviderMetadata["headers"]
	switch headers := raw.(type) {
	case map[string]string:
		return headers
	case map[string]any:
		copied := make(map[string]string, len(headers))
		for key, value := range headers {
			text, ok := value.(string)
			if !ok {
				continue
			}
			copied[key] = text
		}
		return copied
	default:
		return nil
	}
}

func unsafeCredentialHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization":
		return true
	default:
		return false
	}
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

func anthropicVersion(provider sigma.ProviderID, opts sigma.Options) string {
	options := providerOptions(opts, provider)
	if version, ok := stringOption(options, providerOptionVersion); ok {
		return version
	}
	if version, ok := stringOption(options, providerOptionVersionGo); ok {
		return version
	}
	return defaultVersion
}

func anthropicBeta(model sigma.Model, opts sigma.Options, compat messagesCompat, hasTools bool) string {
	betas := make([]string, 0, 4)
	provider := model.Provider
	if compat.claudeCodeIdentity {
		betas = appendBetas(betas, claudeCodeBetaHeader)
		betas = appendBetas(betas, claudeCodeOAuthBeta)
	}
	options := providerOptions(opts, provider)
	if beta, ok := stringOption(options, providerOptionBeta); ok {
		betas = appendBetas(betas, beta)
	} else if beta, ok := stringOption(options, providerOptionBetaGo); ok {
		betas = appendBetas(betas, beta)
	}
	if hasTools && !compat.eagerToolInputStreaming {
		betas = appendBetas(betas, fineGrainedToolStreamingBeta)
	}
	if anthropicInterleavedThinking(opts) && thinkingRequested(opts) && thinkingFormat(model, compat) != sigma.AnthropicThinkingAdaptive {
		betas = appendBetas(betas, interleavedThinkingBeta)
	}
	return strings.Join(betas, ",")
}

func anthropicInterleavedThinking(opts sigma.Options) bool {
	return opts.AnthropicOptions != nil &&
		opts.AnthropicOptions.InterleavedThinking != nil &&
		*opts.AnthropicOptions.InterleavedThinking
}

func appendBetas(betas []string, value string) []string {
	for _, beta := range strings.Split(value, ",") {
		beta = strings.TrimSpace(beta)
		if beta == "" {
			continue
		}
		seen := false
		for _, existing := range betas {
			if existing == beta {
				seen = true
				break
			}
		}
		if !seen {
			betas = append(betas, beta)
		}
	}
	return betas
}

func responseError(resp *http.Response, model sigma.Model) *sigma.ProviderError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return sigma.NewProviderError(
		model.Provider,
		sigma.APIAnthropicMessages,
		model.ID,
		resp.StatusCode,
		requestID(resp.Header),
		sigma.RetryAfter(resp.Header),
		body,
		contextOverflowCause(body),
	)
}

func requestID(header http.Header) string {
	for _, key := range []string{"request-id", "x-request-id", "anthropic-request-id"} {
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
