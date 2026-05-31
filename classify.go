// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wintermi/sigma/internal/redact"
)

// ErrorClass is a stable provider/model execution error category.
type ErrorClass string

const (
	ErrorClassUnknown         ErrorClass = "unknown"
	ErrorClassTransient       ErrorClass = "transient"
	ErrorClassRateLimited     ErrorClass = "rate-limited"
	ErrorClassAuth            ErrorClass = "auth"
	ErrorClassQuota           ErrorClass = "quota"
	ErrorClassBilling         ErrorClass = "billing"
	ErrorClassContextOverflow ErrorClass = "context-overflow"
	ErrorClassInvalidRequest  ErrorClass = "invalid-request"
	ErrorClassProvider        ErrorClass = "provider"
)

// RetryHint describes whether retrying the same request may be useful.
type RetryHint struct {
	Retryable bool
	After     time.Duration
}

// ErrorClassification exposes provider-neutral handling hints for an error.
type ErrorClassification struct {
	Class        ErrorClass
	Provider     ProviderID
	API          API
	Model        ModelID
	StatusCode   int
	ProviderCode string
	Message      string
	RequestID    string
	RetryHint    RetryHint
	Err          error
}

// ClassifyError returns stable provider-neutral classification for err.
func ClassifyError(err error) ErrorClassification {
	if err == nil {
		return ErrorClassification{Class: ErrorClassUnknown}
	}

	classification := ErrorClassification{
		Class:     ErrorClassUnknown,
		Message:   redact.String(err.Error()),
		RetryHint: RetryHint{Retryable: RetryableNetworkError(err)},
		Err:       err,
	}
	if classification.RetryHint.Retryable {
		classification.Class = ErrorClassTransient
	}

	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		classification.Provider = providerErr.Provider
		classification.API = providerErr.API
		classification.Model = providerErr.Model
		classification.StatusCode = providerErr.StatusCode
		classification.ProviderCode = providerErr.ProviderCode
		classification.RequestID = redact.String(providerErr.RequestID)
		classification.RetryHint.After = providerErr.RetryAfter
		if providerErr.ProviderMessage != "" {
			classification.Message = providerErr.ProviderMessage
		}
		classification.Class = classifyProviderError(err, providerErr)
		classification.RetryHint.Retryable = classRetryable(classification.Class)
		return classification
	}

	var sigmaErr *Error
	if errors.As(err, &sigmaErr) {
		classification.Provider = sigmaErr.Provider
		classification.Model = sigmaErr.Model
		if sigmaErr.Message != "" {
			classification.Message = redact.String(sigmaErr.Message)
		}
		classification.Class = classifySigmaError(err, sigmaErr)
		classification.RetryHint.Retryable = classRetryable(classification.Class)
		return classification
	}

	if errors.Is(err, ErrContextOverflow) {
		classification.Class = ErrorClassContextOverflow
		classification.RetryHint.Retryable = false
	}
	return classification
}

func classifySigmaError(err error, sigmaErr *Error) ErrorClass {
	if errors.Is(err, ErrContextOverflow) || sigmaErr.Code == ErrorContextOverflow {
		return ErrorClassContextOverflow
	}
	switch sigmaErr.Code {
	case ErrorInvalidOptions, ErrorUnsupported, ErrorProviderNotFound, ErrorModelNotFound, ErrorToolValidation, ErrorInvalidRequest, ErrorInvalidStreamEvent, ErrorStreamClosed:
		return ErrorClassInvalidRequest
	case ErrorProviderResponse, ErrorStream:
		return ErrorClassProvider
	case ErrorContextOverflow:
		return ErrorClassContextOverflow
	case ErrorAborted, ErrorDebugHook:
		return ErrorClassUnknown
	}
	if errors.Is(err, ErrCredentialUnavailable) {
		return ErrorClassAuth
	}
	return ErrorClassUnknown
}

func classifyProviderError(err error, providerErr *ProviderError) ErrorClass {
	if errors.Is(err, ErrContextOverflow) {
		return ErrorClassContextOverflow
	}
	if errors.Is(err, ErrRetryAfterExceedsMaxDelay) {
		return ErrorClassRateLimited
	}

	code := normalizedErrorText(providerErr.ProviderCode)
	if class, ok := classForProviderCode(code); ok {
		return class
	}

	if class, ok := classForStatus(providerErr.StatusCode); ok {
		return class
	}

	message := normalizedErrorText(firstNonEmpty(providerErr.ProviderMessage, providerErr.BodyPreview, err.Error()))
	if class, ok := classForMessage(message); ok {
		return class
	}
	if providerErr.StatusCode != 0 {
		return ErrorClassProvider
	}
	return ErrorClassUnknown
}

func classForProviderCode(code string) (ErrorClass, bool) {
	switch code {
	case "context_length_exceeded", "context-window-exceeded", "context_window_exceeded", "model_context_window_exceeded", "request_too_large":
		return ErrorClassContextOverflow, true
	case "authentication_error", "unauthorized", "unauthenticated", "invalid_api_key", "invalid_api_key_error":
		return ErrorClassAuth, true
	case "insufficient_quota", "quota_exceeded", "usage_limit_reached", "freeusage_limit_error", "gousage_limit_error":
		return ErrorClassQuota, true
	case "billing", "billing_error", "payment_required", "usage_not_included", "out_of_budget":
		return ErrorClassBilling, true
	case "rate_limit_error", "rate_limit_exceeded", "rate_limited", "too_many_requests", "throttlingexception":
		return ErrorClassRateLimited, true
	case "server_error", "internal_error", "overloaded_error", "service_unavailable", "serviceunavailableexception", "internalserverexception", "modelstreamerrorexception":
		return ErrorClassTransient, true
	case "invalid_request_error", "invalid_prompt", "validationexception":
		return ErrorClassInvalidRequest, true
	default:
		return "", false
	}
}

func classForStatus(status int) (ErrorClass, bool) {
	switch {
	case status == 0:
		return "", false
	case status == http.StatusUnauthorized:
		return ErrorClassAuth, true
	case status == http.StatusRequestEntityTooLarge:
		return ErrorClassContextOverflow, true
	case status == http.StatusTooManyRequests:
		return ErrorClassRateLimited, true
	case status >= http.StatusInternalServerError:
		return ErrorClassTransient, true
	case status == http.StatusBadRequest || status == http.StatusForbidden || status == http.StatusUnprocessableEntity:
		return ErrorClassInvalidRequest, true
	default:
		return ErrorClassProvider, true
	}
}

func classForMessage(message string) (ErrorClass, bool) {
	switch {
	case message == "":
		return "", false
	case messageIndicatesAuth(message):
		return ErrorClassAuth, true
	case messageIndicatesBilling(message):
		return ErrorClassBilling, true
	case messageIndicatesQuota(message):
		return ErrorClassQuota, true
	case messageIndicatesRateLimit(message):
		return ErrorClassRateLimited, true
	case messageIndicatesTransient(message):
		return ErrorClassTransient, true
	case messageIndicatesContextOverflow(message):
		return ErrorClassContextOverflow, true
	default:
		return "", false
	}
}

func classRetryable(class ErrorClass) bool {
	return class == ErrorClassTransient || class == ErrorClassRateLimited
}

func messageIndicatesAuth(message string) bool {
	return strings.Contains(message, "invalid api key") ||
		strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "authentication")
}

func messageIndicatesBilling(message string) bool {
	return strings.Contains(message, "usage_not_included") ||
		strings.Contains(message, "payment required") ||
		strings.Contains(message, "billing")
}

func messageIndicatesQuota(message string) bool {
	return strings.Contains(message, "insufficient_quota") ||
		strings.Contains(message, "quota exceeded") ||
		strings.Contains(message, "out of budget") ||
		strings.Contains(message, "available balance") ||
		strings.Contains(message, "monthly usage limit") ||
		strings.Contains(message, "usage limit")
}

func messageIndicatesRateLimit(message string) bool {
	return strings.Contains(message, "rate limit") ||
		strings.Contains(message, "rate limited") ||
		strings.Contains(message, "too many requests") ||
		strings.Contains(message, "throttling")
}

func messageIndicatesTransient(message string) bool {
	return strings.Contains(message, "overloaded") ||
		strings.Contains(message, "service unavailable") ||
		strings.Contains(message, "server error") ||
		strings.Contains(message, "internal error") ||
		strings.Contains(message, "upstream connect") ||
		strings.Contains(message, "connection refused")
}

func messageIndicatesContextOverflow(message string) bool {
	if messageIndicatesRateLimit(message) || messageIndicatesTransient(message) {
		return false
	}
	return strings.Contains(message, "context_length_exceeded") ||
		strings.Contains(message, "model_context_window_exceeded") ||
		strings.Contains(message, "maximum prompt length") ||
		strings.Contains(message, "token limit exceeded") ||
		strings.Contains(message, "context") && (strings.Contains(message, "too long") || strings.Contains(message, "maximum") || strings.Contains(message, "exceed")) ||
		strings.Contains(message, "prompt is too long") ||
		strings.Contains(message, "input token count") && strings.Contains(message, "exceeds the maximum") ||
		strings.Contains(message, "reduce the length of the messages")
}

func normalizedErrorText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
