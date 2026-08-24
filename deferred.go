// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"fmt"
)

// DeferredResponseStatus identifies the lifecycle state of a deferred response.
type DeferredResponseStatus string

const (
	// DeferredResponseQueued indicates the provider accepted but has not started the response.
	DeferredResponseQueued DeferredResponseStatus = "queued"
	// DeferredResponseInProgress indicates the provider is generating the response.
	DeferredResponseInProgress DeferredResponseStatus = "in-progress"
	// DeferredResponseCompleted indicates the response completed successfully.
	DeferredResponseCompleted DeferredResponseStatus = "completed"
	// DeferredResponseIncomplete indicates generation ended without a complete response.
	DeferredResponseIncomplete DeferredResponseStatus = "incomplete"
	// DeferredResponseFailed indicates response generation failed.
	DeferredResponseFailed DeferredResponseStatus = "failed"
	// DeferredResponseCancelled indicates the response was cancelled.
	DeferredResponseCancelled DeferredResponseStatus = "cancelled"
)

// DeferredResponseHandle identifies a durable provider response. Provider, API,
// and model provenance prevent a handle from being dispatched to a different
// registered route. ProviderMetadata carries opaque conversion state needed to
// reconstruct a terminal response after the handle is serialized.
type DeferredResponseHandle struct {
	Provider         ProviderID     `json:"provider"`
	Model            ModelID        `json:"model"`
	API              API            `json:"api"`
	ID               string         `json:"id"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
}

// DeferredResponse reports one lifecycle observation. Message is populated
// when the provider returns terminal assistant output.
type DeferredResponse struct {
	Handle  DeferredResponseHandle `json:"handle"`
	Status  DeferredResponseStatus `json:"status"`
	Message *AssistantMessage      `json:"message,omitempty"`
}

// SubmitDeferred starts a durable provider response without opening a stream.
func (c *Client) SubmitDeferred(ctx context.Context, model Model, req Request, opts ...Option) (DeferredResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		c = NewClient()
	}
	model, provider, err := c.deferredProviderForModel(model)
	if err != nil {
		return DeferredResponse{}, err
	}

	options := c.requestOptions(model, opts)
	options = applyAutomaticMaxTokensForContext(model, req, options)
	options = applyProviderNeutralControls(model, options)
	if err := validateOptions(model, options); err != nil {
		return DeferredResponse{}, err
	}
	response, err := provider.SubmitDeferred(ctx, model, req, options)
	if err != nil {
		return response, fmt.Errorf("submit deferred response: %w", err)
	}
	return response, nil
}

// FetchDeferred retrieves one lifecycle observation for handle. It does not
// poll or wait for a terminal response.
func (c *Client) FetchDeferred(ctx context.Context, handle DeferredResponseHandle, opts ...Option) (DeferredResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		c = NewClient()
	}
	model, provider, err := c.deferredProviderForHandle(handle)
	if err != nil {
		return DeferredResponse{}, err
	}

	options := c.requestOptions(model, opts)
	if err := validateOptions(model, options); err != nil {
		return DeferredResponse{}, err
	}
	response, err := provider.FetchDeferred(ctx, model, handle, options)
	if err != nil {
		return response, fmt.Errorf("fetch deferred response: %w", err)
	}
	return response, nil
}

// CancelDeferred requests cancellation and returns the provider's resulting
// lifecycle observation.
func (c *Client) CancelDeferred(ctx context.Context, handle DeferredResponseHandle, opts ...Option) (DeferredResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		c = NewClient()
	}
	model, provider, err := c.deferredProviderForHandle(handle)
	if err != nil {
		return DeferredResponse{}, err
	}

	options := c.requestOptions(model, opts)
	if err := validateOptions(model, options); err != nil {
		return DeferredResponse{}, err
	}
	response, err := provider.CancelDeferred(ctx, model, handle, options)
	if err != nil {
		return response, fmt.Errorf("cancel deferred response: %w", err)
	}
	return response, nil
}

func (c *Client) deferredProviderForModel(model Model) (Model, DeferredTextProvider, error) {
	if err := ValidateModelRef(ModelRef{Provider: model.Provider, ID: model.ID}); err != nil {
		return Model{}, nil, err
	}
	registered, ok := c.GetModel(model.Provider, model.ID)
	if !ok {
		return Model{}, nil, modelNotFoundError(model.Provider, model.ID)
	}
	if model.API != "" && model.API != registered.API {
		return Model{}, nil, invalidDeferredRouteError(model.Provider, model.ID)
	}
	model = registered

	provider, ok := c.registry.TextProvider(model.Provider)
	if !ok {
		return Model{}, nil, providerNotFoundError(model.Provider, model.ID)
	}
	deferred, ok := provider.(DeferredTextProvider)
	if !ok {
		return Model{}, nil, unsupportedDeferredProviderError(model)
	}
	return model, deferred, nil
}

func (c *Client) deferredProviderForHandle(handle DeferredResponseHandle) (Model, DeferredTextProvider, error) {
	if handle.Provider == "" || handle.Model == "" || handle.API == "" || handle.ID == "" {
		return Model{}, nil, invalidDeferredHandleError(handle.Provider, handle.Model, "provider, model, API, and ID are required")
	}
	model, provider, err := c.deferredProviderForModel(Model{
		Provider: handle.Provider,
		ID:       handle.Model,
		API:      handle.API,
	})
	if err != nil {
		return Model{}, nil, err
	}
	return model, provider, nil
}

func invalidDeferredHandleError(provider ProviderID, model ModelID, message string) error {
	return &Error{
		Code:     ErrorInvalidOptions,
		Message:  "deferred response handle: " + message,
		Provider: provider,
		Model:    model,
		Err:      ErrInvalidOptions,
	}
}

func invalidDeferredRouteError(provider ProviderID, model ModelID) error {
	return &Error{
		Code:     ErrorInvalidOptions,
		Message:  "deferred response model API does not match the registered route",
		Provider: provider,
		Model:    model,
		Err:      ErrInvalidOptions,
	}
}

func unsupportedDeferredProviderError(model Model) error {
	return &Error{
		Code:     ErrorUnsupported,
		Message:  "text provider does not support deferred responses",
		Provider: model.Provider,
		Model:    model.ID,
	}
}

// SubmitDeferred starts a durable response using the default registry.
func SubmitDeferred(ctx context.Context, model Model, req Request, opts ...Option) (DeferredResponse, error) {
	return defaultClient().SubmitDeferred(ctx, model, req, opts...)
}

// FetchDeferred retrieves one deferred response observation using the default registry.
func FetchDeferred(ctx context.Context, handle DeferredResponseHandle, opts ...Option) (DeferredResponse, error) {
	return defaultClient().FetchDeferred(ctx, handle, opts...)
}

// CancelDeferred cancels a deferred response using the default registry.
func CancelDeferred(ctx context.Context, handle DeferredResponseHandle, opts ...Option) (DeferredResponse, error) {
	return defaultClient().CancelDeferred(ctx, handle, opts...)
}
