// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/wintermi/sigma"
)

const (
	deferredGrammarToolPropertiesKey = "grammar_tool_input_properties"
	deferredRequestServiceTierKey    = "request_service_tier"
)

var _ sigma.DeferredTextProvider = (*ResponsesProvider)(nil)

// SubmitDeferred starts a direct OpenAI Responses background request.
func (p *ResponsesProvider) SubmitDeferred(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) (sigma.DeferredResponse, error) {
	if err := validateDirectOpenAIDeferredModel(model); err != nil {
		return sigma.DeferredResponse{}, err
	}
	payload, err := responsesPayload(model, req, opts)
	if err != nil {
		return sigma.DeferredResponse{}, err
	}
	payload["background"] = true
	payload["stream"] = false
	handle, err := deferredHandle(model, "", req, opts)
	if err != nil {
		return sigma.DeferredResponse{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return sigma.DeferredResponse{}, fmt.Errorf("openai responses: encode deferred request: %w", err)
	}

	resp, err := p.doDeferred(ctx, model, opts, func(ctx context.Context) (*http.Request, error) {
		endpoint, err := p.endpoint(model, opts)
		if err != nil {
			return nil, err
		}
		return p.newDeferredRequest(ctx, model, opts, http.MethodPost, endpoint, body)
	})
	if err != nil {
		return sigma.DeferredResponse{}, err
	}
	return p.decodeDeferredResponse(ctx, resp, model, handle, opts)
}

// FetchDeferred retrieves one direct OpenAI Responses lifecycle observation.
func (p *ResponsesProvider) FetchDeferred(ctx context.Context, model sigma.Model, handle sigma.DeferredResponseHandle, opts sigma.Options) (sigma.DeferredResponse, error) {
	if err := validateDirectOpenAIDeferredModel(model); err != nil {
		return sigma.DeferredResponse{}, err
	}
	if err := validateDirectOpenAIDeferredHandle(model, handle); err != nil {
		return sigma.DeferredResponse{}, err
	}
	endpoint, err := p.deferredEndpoint(model, handle.ID, "", opts)
	if err != nil {
		return sigma.DeferredResponse{}, err
	}
	resp, err := p.doDeferred(ctx, model, opts, func(ctx context.Context) (*http.Request, error) {
		return p.newDeferredRequest(ctx, model, opts, http.MethodGet, endpoint, nil)
	})
	if err != nil {
		return sigma.DeferredResponse{}, err
	}
	return p.decodeDeferredResponse(ctx, resp, model, handle, opts)
}

// CancelDeferred cancels one direct OpenAI Responses background request.
func (p *ResponsesProvider) CancelDeferred(ctx context.Context, model sigma.Model, handle sigma.DeferredResponseHandle, opts sigma.Options) (sigma.DeferredResponse, error) {
	if err := validateDirectOpenAIDeferredModel(model); err != nil {
		return sigma.DeferredResponse{}, err
	}
	if err := validateDirectOpenAIDeferredHandle(model, handle); err != nil {
		return sigma.DeferredResponse{}, err
	}
	endpoint, err := p.deferredEndpoint(model, handle.ID, "cancel", opts)
	if err != nil {
		return sigma.DeferredResponse{}, err
	}
	resp, err := p.doDeferred(ctx, model, opts, func(ctx context.Context) (*http.Request, error) {
		return p.newDeferredRequest(ctx, model, opts, http.MethodPost, endpoint, nil)
	})
	if err != nil {
		return sigma.DeferredResponse{}, err
	}
	return p.decodeDeferredResponse(ctx, resp, model, handle, opts)
}

func (p *ResponsesProvider) doDeferred(
	ctx context.Context,
	model sigma.Model,
	opts sigma.Options,
	request func(context.Context) (*http.Request, error),
) (*http.Response, error) {
	return sigma.DoHTTPWithRetry(
		ctx,
		p.base.httpClient(opts),
		opts,
		request,
		func(resp *http.Response) *sigma.ProviderError {
			return responsesResponseError(resp, model)
		},
		sigma.TextResponseDebugHTTPHook(ctx, opts, model.Provider, sigma.APIOpenAIResponses, model.ID),
	)
}

func (p *ResponsesProvider) newDeferredRequest(
	ctx context.Context,
	model sigma.Model,
	opts sigma.Options,
	method string,
	endpoint string,
	body []byte,
) (*http.Request, error) {
	opts, credential, err := p.base.resolveAuth(ctx, model, opts)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "sigma/openai-responses")
	if len(body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	p.base.addAuthCredentialHeader(httpReq, model, credential)
	p.addProviderHeaders(httpReq, model, opts)
	for key, value := range p.base.headers {
		httpReq.Header.Set(key, value)
	}
	addOpenAICompatibleModelHeaders(httpReq, model)
	for key, value := range opts.Headers {
		httpReq.Header.Set(key, value)
	}
	sigma.ApplySuppressedHeaders(httpReq.Header, opts)
	if len(body) > 0 {
		if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIOpenAIResponses, model.ID, body, httpReq.Header); err != nil {
			return nil, err
		}
	}
	return httpReq, nil
}

func (p *ResponsesProvider) deferredEndpoint(model sigma.Model, responseID string, action string, opts sigma.Options) (string, error) {
	endpoint, err := p.endpoint(model, opts)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("openai responses: invalid endpoint %q", endpoint)
	}
	path := strings.TrimSuffix(parsed.Path, "/") + "/" + responseID
	rawPath := strings.TrimSuffix(parsed.EscapedPath(), "/") + "/" + url.PathEscape(responseID)
	if action != "" {
		path += "/" + action
		rawPath += "/" + url.PathEscape(action)
	}
	parsed.Path = path
	parsed.RawPath = rawPath
	return parsed.String(), nil
}

func (p *ResponsesProvider) decodeDeferredResponse(
	ctx context.Context,
	resp *http.Response,
	model sigma.Model,
	handle sigma.DeferredResponseHandle,
	opts sigma.Options,
) (sigma.DeferredResponse, error) {
	deferred := sigma.DeferredResponse{Handle: handle}
	if resp == nil {
		return deferred, deferredProviderResponseError(model, "provider returned no HTTP response")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return deferred, responsesResponseError(resp, model)
	}

	var response responsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return deferred, fmt.Errorf("openai responses: decode deferred response: %w", err)
	}
	if response.ID == "" {
		return deferred, deferredProviderResponseError(model, "response ID is missing")
	}
	if handle.ID != "" && response.ID != handle.ID {
		return deferred, deferredProviderResponseError(model, "response ID does not match the deferred handle")
	}
	if handle.ID == "" {
		handle.Provider = model.Provider
		handle.Model = model.ID
		handle.API = model.API
		handle.ID = response.ID
		deferred.Handle = handle
	}

	status, ok := normalizeDeferredResponseStatus(response.Status)
	if !ok {
		return deferred, deferredProviderResponseError(model, fmt.Sprintf("unsupported response status %q", response.Status))
	}
	deferred.Status = status
	if status == sigma.DeferredResponseQueued || status == sigma.DeferredResponseInProgress || status == sigma.DeferredResponseCancelled {
		return deferred, nil
	}

	streamOptions := deferredResponsesStreamOptions(deferred.Handle, opts)
	message, err := parseResponsesObject(ctx, response, model, streamOptions)
	if err != nil {
		message.StopReason = sigma.StopReasonError
	}
	deferred.Message = &message
	return deferred, err
}

func deferredHandle(model sigma.Model, responseID string, req sigma.Request, opts sigma.Options) (sigma.DeferredResponseHandle, error) {
	properties, err := responsesGrammarToolInputProperties(model, req, opts)
	if err != nil {
		return sigma.DeferredResponseHandle{}, err
	}
	metadata := make(map[string]any)
	if len(properties) > 0 {
		copied := make(map[string]string, len(properties))
		for name, property := range properties {
			copied[name] = property
		}
		metadata[deferredGrammarToolPropertiesKey] = copied
	}
	if serviceTier := openAIRequestServiceTier(opts); serviceTier != "" {
		metadata[deferredRequestServiceTierKey] = serviceTier
	}
	if len(metadata) == 0 {
		metadata = nil
	}
	return sigma.DeferredResponseHandle{
		Provider:         model.Provider,
		Model:            model.ID,
		API:              model.API,
		ID:               responseID,
		ProviderMetadata: metadata,
	}, nil
}

func deferredResponsesStreamOptions(handle sigma.DeferredResponseHandle, opts sigma.Options) responsesStreamOptions {
	options := responsesStreamOptions{
		requestServiceTier:    openAIRequestServiceTier(opts),
		applyServiceTierCosts: true,
	}
	if value, ok := handle.ProviderMetadata[deferredRequestServiceTierKey].(string); ok {
		options.requestServiceTier = value
	}
	options.grammarToolInputProperties = deferredGrammarToolProperties(handle.ProviderMetadata[deferredGrammarToolPropertiesKey])
	return options
}

func deferredGrammarToolProperties(value any) map[string]string {
	switch properties := value.(type) {
	case map[string]string:
		copied := make(map[string]string, len(properties))
		for name, property := range properties {
			copied[name] = property
		}
		return copied
	case map[string]any:
		copied := make(map[string]string, len(properties))
		for name, value := range properties {
			property, ok := value.(string)
			if ok {
				copied[name] = property
			}
		}
		return copied
	default:
		return nil
	}
}

func normalizeDeferredResponseStatus(status string) (sigma.DeferredResponseStatus, bool) {
	switch status {
	case "queued":
		return sigma.DeferredResponseQueued, true
	case "in_progress":
		return sigma.DeferredResponseInProgress, true
	case "completed":
		return sigma.DeferredResponseCompleted, true
	case "incomplete":
		return sigma.DeferredResponseIncomplete, true
	case "failed":
		return sigma.DeferredResponseFailed, true
	case "cancelled":
		return sigma.DeferredResponseCancelled, true
	default:
		return "", false
	}
}

func validateDirectOpenAIDeferredModel(model sigma.Model) error {
	if model.Provider == sigma.ProviderOpenAI && model.API == sigma.APIOpenAIResponses {
		return nil
	}
	return &sigma.Error{
		Code:     sigma.ErrorUnsupported,
		Message:  "deferred responses require a direct OpenAI Responses model",
		Provider: model.Provider,
		Model:    model.ID,
	}
}

func validateDirectOpenAIDeferredHandle(model sigma.Model, handle sigma.DeferredResponseHandle) error {
	if handle.Provider == model.Provider && handle.Model == model.ID && handle.API == model.API && handle.ID != "" {
		return nil
	}
	return &sigma.Error{
		Code:     sigma.ErrorInvalidOptions,
		Message:  "deferred response handle does not match the direct OpenAI Responses model",
		Provider: model.Provider,
		Model:    model.ID,
		Err:      sigma.ErrInvalidOptions,
	}
}

func deferredProviderResponseError(model sigma.Model, message string) error {
	return &sigma.Error{
		Code:     sigma.ErrorProviderResponse,
		Message:  "openai responses: " + message,
		Provider: model.Provider,
		Model:    model.ID,
		Err:      sigma.ErrProviderResponse,
	}
}
