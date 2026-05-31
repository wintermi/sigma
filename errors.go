// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wintermi/sigma/internal/redact"
)

// ErrorCode identifies a sigma error category.
type ErrorCode string

const (
	// ErrorUnsupported indicates a requested capability is not implemented.
	ErrorUnsupported ErrorCode = "unsupported"
	// ErrorProviderNotFound indicates no provider is registered for a model.
	ErrorProviderNotFound ErrorCode = "provider-not-found"
	// ErrorModelNotFound indicates no model metadata is registered for a model.
	ErrorModelNotFound ErrorCode = "model-not-found"
	// ErrorContextOverflow indicates the provider rejected an oversized context.
	ErrorContextOverflow ErrorCode = "context-overflow"
	// ErrorToolValidation indicates local tool schema or call validation failed.
	ErrorToolValidation ErrorCode = "tool-validation"
	// ErrorProviderResponse indicates a provider returned a failed response.
	ErrorProviderResponse ErrorCode = "provider-response"
)

var (
	// ErrNoProvider indicates no provider is registered for a model.
	ErrNoProvider = errors.New("provider unavailable")
	// ErrModelNotFound indicates no model metadata is registered for a model.
	ErrModelNotFound = errors.New("model not found")
	// ErrCredentialUnavailable indicates no resolver could provide credentials.
	ErrCredentialUnavailable = errors.New("credential unavailable")
	// ErrAborted indicates generation stopped because the request was canceled.
	ErrAborted = errors.New("generation aborted")
	// ErrContextOverflow indicates a request exceeded the provider context limit.
	ErrContextOverflow = errors.New("context overflow")
	// ErrToolValidation indicates a tool definition or tool call failed validation.
	ErrToolValidation = errors.New("tool validation failed")
	// ErrProviderResponse indicates the provider returned an error response.
	ErrProviderResponse = errors.New("provider response error")
	// ErrInvalidOptions indicates request options failed local validation.
	ErrInvalidOptions = errors.New("invalid options")
	// ErrRetryAfterExceedsMaxDelay indicates a provider asked for a retry
	// delay longer than the configured cap.
	ErrRetryAfterExceedsMaxDelay = errors.New("retry-after exceeds max retry delay")
)

// Error is the package error type.
type Error struct {
	Code     ErrorCode
	Message  string
	Provider ProviderID
	Model    ModelID
	Err      error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return redact.String(e.Message)
	}
	if sentinel := sentinelForCode(e.Code); sentinel != nil {
		return sentinel.Error()
	}
	return "sigma error"
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is supports errors.Is with sigma sentinel errors.
func (e *Error) Is(target error) bool {
	if e == nil {
		return false
	}
	if sentinel := sentinelForCode(e.Code); sentinel != nil && target == sentinel {
		return true
	}
	return false
}

// ProviderError reports a failed provider API response without exposing raw payloads.
//
// Provider implementations should map HTTP and provider failures to ProviderError
// and finish text streams with StopReasonError. Request cancellation should map to
// ErrAborted and StopReasonAborted instead of ProviderError.
type ProviderError struct {
	Provider        ProviderID
	API             API
	Model           ModelID
	StatusCode      int
	RequestID       string
	ProviderCode    string
	ProviderMessage string
	RetryAfter      time.Duration
	MaxRetryDelay   time.Duration
	BodyPreview     string
	Err             error
}

// NewProviderError builds a provider response error with a redacted body preview.
func NewProviderError(provider ProviderID, api API, model ModelID, statusCode int, requestID string, retryAfter time.Duration, body []byte, err error) *ProviderError {
	code, message := providerErrorDetails(body)
	return &ProviderError{
		Provider:        provider,
		API:             api,
		Model:           model,
		StatusCode:      statusCode,
		RequestID:       requestID,
		ProviderCode:    redact.String(code),
		ProviderMessage: redact.String(message),
		RetryAfter:      retryAfter,
		BodyPreview:     redact.Preview(string(body), 2048),
		Err:             err,
	}
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{"provider response error"}
	if e.Provider != "" {
		parts = append(parts, "provider="+string(e.Provider))
	}
	if e.API != "" {
		parts = append(parts, "api="+string(e.API))
	}
	if e.Model != "" {
		parts = append(parts, "model="+string(e.Model))
	}
	if e.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.StatusCode))
	}
	if e.RequestID != "" {
		parts = append(parts, "request_id="+redact.String(e.RequestID))
	}
	if e.ProviderCode != "" {
		parts = append(parts, "provider_code="+redact.String(e.ProviderCode))
	}
	if e.ProviderMessage != "" {
		parts = append(parts, "provider_message="+redact.String(e.ProviderMessage))
	}
	if e.RetryAfter > 0 {
		parts = append(parts, "retry_after="+e.RetryAfter.String())
	}
	if e.MaxRetryDelay > 0 {
		parts = append(parts, "max_retry_delay="+e.MaxRetryDelay.String())
	}
	if e.BodyPreview != "" {
		parts = append(parts, "body="+redact.Preview(e.BodyPreview, 512))
	}
	if e.Err != nil {
		parts = append(parts, "cause="+redact.String(e.Err.Error()))
	}
	return strings.Join(parts, " ")
}

// String returns the same diagnostic-safe text as Error.
func (e *ProviderError) String() string {
	return e.Error()
}

// Format prevents fmt from printing raw ProviderError fields with struct verbs.
func (e *ProviderError) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, e.Error())
}

// Unwrap returns the underlying provider cause.
func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is supports errors.Is(err, ErrProviderResponse).
func (e *ProviderError) Is(target error) bool {
	return target == ErrProviderResponse
}

// GenerationError carries a final assistant message alongside a terminal error.
type GenerationError struct {
	Final AssistantMessage
	Err   error
}

func (e *GenerationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return "generation error"
	}
	return redact.String(e.Err.Error())
}

// String returns the same diagnostic-safe text as Error.
func (e *GenerationError) String() string {
	return e.Error()
}

// Unwrap returns the terminal generation error.
func (e *GenerationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// FinalMessage returns the assistant message recorded at stream termination.
func (e *GenerationError) FinalMessage() (AssistantMessage, bool) {
	if e == nil {
		return AssistantMessage{}, false
	}
	return e.Final, true
}

func providerNotFoundError(provider ProviderID, model ModelID) error {
	return &Error{
		Code:     ErrorProviderNotFound,
		Message:  "text provider is not registered",
		Provider: provider,
		Model:    model,
	}
}

func modelNotFoundError(provider ProviderID, model ModelID) error {
	return &Error{
		Code:     ErrorModelNotFound,
		Message:  "model is not registered",
		Provider: provider,
		Model:    model,
	}
}

func sentinelForCode(code ErrorCode) error {
	switch code {
	case ErrorProviderNotFound:
		return ErrNoProvider
	case ErrorModelNotFound:
		return ErrModelNotFound
	case ErrorAborted:
		return ErrAborted
	case ErrorContextOverflow:
		return ErrContextOverflow
	case ErrorToolValidation:
		return ErrToolValidation
	case ErrorProviderResponse:
		return ErrProviderResponse
	case ErrorDebugHook:
		return ErrDebugHook
	case ErrorInvalidOptions:
		return ErrInvalidOptions
	default:
		return nil
	}
}

func providerErrorDetails(body []byte) (string, string) {
	if len(body) == 0 {
		return "", ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", ""
	}

	code, message := errorObjectDetails(payload["error"])
	if code == "" {
		code = stringValue(payload["code"])
	}
	if code == "" {
		code = stringValue(payload["type"])
	}
	if code == "" {
		code = stringValue(payload["status"])
	}
	if message == "" {
		message = stringValue(payload["message"])
	}
	if message == "" {
		message = stringValue(payload["error_description"])
	}
	return code, message
}

func errorObjectDetails(value any) (string, string) {
	switch typed := value.(type) {
	case map[string]any:
		code := stringValue(typed["code"])
		if code == "" {
			code = stringValue(typed["type"])
		}
		if code == "" {
			code = stringValue(typed["status"])
		}
		message := stringValue(typed["message"])
		if message == "" {
			message = stringValue(typed["error_description"])
		}
		return code, message
	case string:
		return typed, ""
	default:
		return "", ""
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}
