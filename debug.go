// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"errors"
	"net/http"

	"github.com/wintermi/sigma/internal/redact"
)

const (
	// ErrorDebugHook indicates a caller-provided debug hook failed.
	ErrorDebugHook ErrorCode = "debug-hook"

	debugPayloadPreviewLimit = 2048
)

// ErrDebugHook indicates a caller-provided debug hook failed.
var ErrDebugHook = errors.New("debug hook failed")

// TextPayloadDebugHook inspects a redacted copy of a text provider payload.
//
// Hooks run after provider payload and headers are built and before the HTTP
// request is sent. Payload replacement is intentionally unsupported: Payload is
// a redacted copy for diagnostics, so mutating it cannot change the request body
// or corrupt a later retry attempt.
type TextPayloadDebugHook func(context.Context, TextPayloadDebug) error

// TextResponseDebugHook inspects redacted response metadata before the response
// body is consumed.
type TextResponseDebugHook func(context.Context, TextResponseDebug) error

// ImagePayloadDebugHook inspects a redacted copy of an image provider payload.
//
// Hooks run after provider payload and headers are built and before the HTTP
// request is sent. Payload replacement is intentionally unsupported: Payload is
// a redacted copy for diagnostics, so mutating it cannot change the request body
// or corrupt a later retry attempt.
type ImagePayloadDebugHook func(context.Context, ImagePayloadDebug) error

// ImageResponseDebugHook inspects redacted image response metadata before the
// response body is consumed.
type ImageResponseDebugHook func(context.Context, ImageResponseDebug) error

// EmbeddingPayloadDebugHook inspects a redacted copy of an embedding provider payload.
//
// Hooks run after provider payload and headers are built and before the HTTP
// request is sent. Payload replacement is intentionally unsupported: Payload is
// a redacted copy for diagnostics, so mutating it cannot change the request body
// or corrupt a later retry attempt.
type EmbeddingPayloadDebugHook func(context.Context, EmbeddingPayloadDebug) error

// EmbeddingResponseDebugHook inspects redacted embedding response metadata
// before the response body is consumed.
type EmbeddingResponseDebugHook func(context.Context, EmbeddingResponseDebug) error

// TextPayloadDebug is the diagnostic view passed to text payload hooks.
type TextPayloadDebug struct {
	Provider       ProviderID
	API            API
	Model          ModelID
	Headers        http.Header
	Payload        []byte
	PayloadPreview string
}

// TextResponseDebug is the diagnostic view passed to text response hooks.
type TextResponseDebug struct {
	Provider   ProviderID
	API        API
	Model      ModelID
	StatusCode int
	Headers    http.Header
	RequestID  string
}

// ImagePayloadDebug is the diagnostic view passed to image payload hooks.
type ImagePayloadDebug struct {
	Provider       ProviderID
	API            ImageAPI
	Model          ModelID
	Headers        http.Header
	Payload        []byte
	PayloadPreview string
}

// ImageResponseDebug is the diagnostic view passed to image response hooks.
type ImageResponseDebug struct {
	Provider   ProviderID
	API        ImageAPI
	Model      ModelID
	StatusCode int
	Headers    http.Header
	RequestID  string
}

// EmbeddingPayloadDebug is the diagnostic view passed to embedding payload hooks.
type EmbeddingPayloadDebug struct {
	Provider       ProviderID
	API            EmbeddingAPI
	Model          ModelID
	Headers        http.Header
	Payload        []byte
	PayloadPreview string
}

// EmbeddingResponseDebug is the diagnostic view passed to embedding response hooks.
type EmbeddingResponseDebug struct {
	Provider   ProviderID
	API        EmbeddingAPI
	Model      ModelID
	StatusCode int
	Headers    http.Header
	RequestID  string
}

// WithTextPayloadDebugHook adds a safe text request payload debug hook.
func WithTextPayloadDebugHook(hook TextPayloadDebugHook) Option {
	return func(options *Options) {
		if hook != nil {
			options.TextPayloadDebugHooks = append(options.TextPayloadDebugHooks, hook)
		}
	}
}

// WithTextResponseDebugHook adds a safe text response debug hook.
func WithTextResponseDebugHook(hook TextResponseDebugHook) Option {
	return func(options *Options) {
		if hook != nil {
			options.TextResponseDebugHooks = append(options.TextResponseDebugHooks, hook)
		}
	}
}

// WithImagePayloadDebugHook adds a safe image request payload debug hook.
func WithImagePayloadDebugHook(hook ImagePayloadDebugHook) ImageOption {
	return func(options *Options) {
		if hook != nil {
			options.ImagePayloadDebugHooks = append(options.ImagePayloadDebugHooks, hook)
		}
	}
}

// WithImageResponseDebugHook adds a safe image response debug hook.
func WithImageResponseDebugHook(hook ImageResponseDebugHook) ImageOption {
	return func(options *Options) {
		if hook != nil {
			options.ImageResponseDebugHooks = append(options.ImageResponseDebugHooks, hook)
		}
	}
}

// WithEmbeddingPayloadDebugHook adds a safe embedding request payload debug hook.
func WithEmbeddingPayloadDebugHook(hook EmbeddingPayloadDebugHook) EmbeddingOption {
	return func(options *Options) {
		if hook != nil {
			options.EmbeddingPayloadDebugHooks = append(options.EmbeddingPayloadDebugHooks, hook)
		}
	}
}

// WithEmbeddingResponseDebugHook adds a safe embedding response debug hook.
func WithEmbeddingResponseDebugHook(hook EmbeddingResponseDebugHook) EmbeddingOption {
	return func(options *Options) {
		if hook != nil {
			options.EmbeddingResponseDebugHooks = append(options.EmbeddingResponseDebugHooks, hook)
		}
	}
}

// RunTextPayloadDebugHooks runs text payload hooks with redacted copies.
func RunTextPayloadDebugHooks(ctx context.Context, opts Options, provider ProviderID, api API, model ModelID, payload []byte, headers http.Header) error {
	if len(opts.TextPayloadDebugHooks) == 0 {
		return nil
	}
	debug := TextPayloadDebug{
		Provider:       provider,
		API:            api,
		Model:          model,
		Headers:        redactedHeaders(headers),
		Payload:        redactedPayload(payload),
		PayloadPreview: redact.Preview(string(payload), debugPayloadPreviewLimit),
	}
	for _, hook := range opts.TextPayloadDebugHooks {
		if hook == nil {
			continue
		}
		if err := hook(ctx, cloneTextPayloadDebug(debug)); err != nil {
			return debugHookError("text payload", provider, model, err)
		}
	}
	return nil
}

// RunImagePayloadDebugHooks runs image payload hooks with redacted copies.
func RunImagePayloadDebugHooks(ctx context.Context, opts Options, provider ProviderID, api ImageAPI, model ModelID, payload []byte, headers http.Header) error {
	if len(opts.ImagePayloadDebugHooks) == 0 {
		return nil
	}
	debug := ImagePayloadDebug{
		Provider:       provider,
		API:            api,
		Model:          model,
		Headers:        redactedHeaders(headers),
		Payload:        redactedPayload(payload),
		PayloadPreview: redact.Preview(string(payload), debugPayloadPreviewLimit),
	}
	for _, hook := range opts.ImagePayloadDebugHooks {
		if hook == nil {
			continue
		}
		if err := hook(ctx, cloneImagePayloadDebug(debug)); err != nil {
			return debugHookError("image payload", provider, model, err)
		}
	}
	return nil
}

// RunEmbeddingPayloadDebugHooks runs embedding payload hooks with redacted copies.
func RunEmbeddingPayloadDebugHooks(ctx context.Context, opts Options, provider ProviderID, api EmbeddingAPI, model ModelID, payload []byte, headers http.Header) error {
	if len(opts.EmbeddingPayloadDebugHooks) == 0 {
		return nil
	}
	debug := EmbeddingPayloadDebug{
		Provider:       provider,
		API:            api,
		Model:          model,
		Headers:        redactedHeaders(headers),
		Payload:        redactedPayload(payload),
		PayloadPreview: redact.Preview(string(payload), debugPayloadPreviewLimit),
	}
	for _, hook := range opts.EmbeddingPayloadDebugHooks {
		if hook == nil {
			continue
		}
		if err := hook(ctx, cloneEmbeddingPayloadDebug(debug)); err != nil {
			return debugHookError("embedding payload", provider, model, err)
		}
	}
	return nil
}

// TextResponseDebugHTTPHook adapts text response hooks to the shared retry helper.
func TextResponseDebugHTTPHook(ctx context.Context, opts Options, provider ProviderID, api API, model ModelID) HTTPResponseHook {
	if len(opts.TextResponseDebugHooks) == 0 {
		return nil
	}
	return func(resp *http.Response) error {
		debug := TextResponseDebug{
			Provider:   provider,
			API:        api,
			Model:      model,
			StatusCode: resp.StatusCode,
			Headers:    redactedHeaders(resp.Header),
			RequestID:  requestIDFromHeaders(resp.Header),
		}
		for _, hook := range opts.TextResponseDebugHooks {
			if hook == nil {
				continue
			}
			if err := hook(ctx, cloneTextResponseDebug(debug)); err != nil {
				return debugHookError("text response", provider, model, err)
			}
		}
		return nil
	}
}

// ImageResponseDebugHTTPHook adapts image response hooks to the shared retry helper.
func ImageResponseDebugHTTPHook(ctx context.Context, opts Options, provider ProviderID, api ImageAPI, model ModelID) HTTPResponseHook {
	if len(opts.ImageResponseDebugHooks) == 0 {
		return nil
	}
	return func(resp *http.Response) error {
		debug := ImageResponseDebug{
			Provider:   provider,
			API:        api,
			Model:      model,
			StatusCode: resp.StatusCode,
			Headers:    redactedHeaders(resp.Header),
			RequestID:  requestIDFromHeaders(resp.Header),
		}
		for _, hook := range opts.ImageResponseDebugHooks {
			if hook == nil {
				continue
			}
			if err := hook(ctx, cloneImageResponseDebug(debug)); err != nil {
				return debugHookError("image response", provider, model, err)
			}
		}
		return nil
	}
}

// EmbeddingResponseDebugHTTPHook adapts embedding response hooks to the shared retry helper.
func EmbeddingResponseDebugHTTPHook(ctx context.Context, opts Options, provider ProviderID, api EmbeddingAPI, model ModelID) HTTPResponseHook {
	if len(opts.EmbeddingResponseDebugHooks) == 0 {
		return nil
	}
	return func(resp *http.Response) error {
		debug := EmbeddingResponseDebug{
			Provider:   provider,
			API:        api,
			Model:      model,
			StatusCode: resp.StatusCode,
			Headers:    redactedHeaders(resp.Header),
			RequestID:  requestIDFromHeaders(resp.Header),
		}
		for _, hook := range opts.EmbeddingResponseDebugHooks {
			if hook == nil {
				continue
			}
			if err := hook(ctx, cloneEmbeddingResponseDebug(debug)); err != nil {
				return debugHookError("embedding response", provider, model, err)
			}
		}
		return nil
	}
}

func debugHookError(stage string, provider ProviderID, model ModelID, err error) error {
	return &Error{
		Code:     ErrorDebugHook,
		Message:  stage + " debug hook failed",
		Provider: provider,
		Model:    model,
		Err:      err,
	}
}

func redactedPayload(payload []byte) []byte {
	if len(payload) == 0 {
		return nil
	}
	return []byte(redact.String(string(payload)))
}

func redactedHeaders(headers http.Header) http.Header {
	if len(headers) == 0 {
		return nil
	}
	return http.Header(redact.Headers(headers))
}

func requestIDFromHeaders(headers http.Header) string {
	for _, key := range []string{"x-request-id", "request-id", "openai-request-id", "x-goog-request-id", "x-mistral-request-id"} {
		if value := headers.Get(key); value != "" {
			return redact.String(value)
		}
	}
	return ""
}

func cloneTextPayloadDebug(debug TextPayloadDebug) TextPayloadDebug {
	debug.Headers = debug.Headers.Clone()
	debug.Payload = cloneBytes(debug.Payload)
	return debug
}

func cloneTextResponseDebug(debug TextResponseDebug) TextResponseDebug {
	debug.Headers = debug.Headers.Clone()
	return debug
}

func cloneImagePayloadDebug(debug ImagePayloadDebug) ImagePayloadDebug {
	debug.Headers = debug.Headers.Clone()
	debug.Payload = cloneBytes(debug.Payload)
	return debug
}

func cloneImageResponseDebug(debug ImageResponseDebug) ImageResponseDebug {
	debug.Headers = debug.Headers.Clone()
	return debug
}

func cloneEmbeddingPayloadDebug(debug EmbeddingPayloadDebug) EmbeddingPayloadDebug {
	debug.Headers = debug.Headers.Clone()
	debug.Payload = cloneBytes(debug.Payload)
	return debug
}

func cloneEmbeddingResponseDebug(debug EmbeddingResponseDebug) EmbeddingResponseDebug {
	debug.Headers = debug.Headers.Clone()
	return debug
}

func cloneBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	copied := make([]byte, len(value))
	copy(copied, value)
	return copied
}
