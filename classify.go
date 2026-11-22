// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"errors"
	"net/http"
	"strconv"
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
	// SplitRecoverable reports whether a smaller request may recover from the
	// same error even though retrying the identical request is not useful.
	SplitRecoverable bool
	Err              error
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
		classification.SplitRecoverable = classSplitRecoverable(classification.Class)
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
		classification.SplitRecoverable = classSplitRecoverable(classification.Class)
		return classification
	}

	if errors.Is(err, ErrContextOverflow) {
		classification.Class = ErrorClassContextOverflow
		classification.RetryHint.Retryable = false
		classification.SplitRecoverable = true
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
	case ErrorProviderResponse:
		return ErrorClassProvider
	case ErrorStream:
		if errors.Is(err, ErrCredentialUnavailable) {
			return ErrorClassAuth
		}
		if errors.Is(err, errRequestTimeout) {
			return ErrorClassTransient
		}
		if RetryableNetworkError(err) {
			return ErrorClassTransient
		}
		if messageIndicatesPrematureProviderStreamTermination(sigmaErr.Message) {
			return ErrorClassTransient
		}
		if class, ok := classForMessage(normalizedErrorText(err.Error())); ok {
			return class
		}
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
	message := normalizedErrorText(firstNonEmpty(providerErr.ProviderMessage, providerErr.BodyPreview, err.Error()))
	status := effectiveErrorStatus(providerErr.StatusCode, code)
	if class, ok := classForCodeAndMessage(code, message); ok {
		// Without a structured code, a 429 is throttling even when its text
		// reads like overflow ("Too many tokens, please wait").
		if class == ErrorClassContextOverflow && codeIsStatusOrEmpty(code) && status == http.StatusTooManyRequests {
			return ErrorClassRateLimited
		}
		return class
	}
	if bodylessCerebrasOverflow(providerErr.Provider, providerErr.StatusCode, providerErr.ProviderCode, providerErr.ProviderMessage, providerErr.BodyPreview) {
		return ErrorClassContextOverflow
	}
	if class, ok := classForStatus(status); ok {
		return class
	}
	if providerErr.StatusCode != 0 {
		return ErrorClassProvider
	}
	return ErrorClassUnknown
}

// effectiveErrorStatus returns the HTTP status, or, for errors relayed without
// one, an HTTP status the provider sent as its error code ({"code":504}).
func effectiveErrorStatus(status int, code string) int {
	if status != 0 {
		return status
	}
	return httpStatusCode(code)
}

func httpStatusCode(code string) int {
	value, err := strconv.Atoi(code)
	if err != nil || value < 400 || value > 599 {
		return 0
	}
	return value
}

func codeIsStatusOrEmpty(code string) bool {
	return code == "" || httpStatusCode(code) != 0
}

// classForCodeAndMessage prefers a structured provider code, except that a
// generic invalid-request code defers to a context-overflow or capacity
// message: Anthropic and Bedrock report overflow, and Azure reports peak-load
// rejection, under their generic validation codes.
func classForCodeAndMessage(code string, message string) (ErrorClass, bool) {
	if class, ok := classForProviderCode(code); ok {
		if class == ErrorClassInvalidRequest && messageIndicatesContextOverflow(message) {
			return ErrorClassContextOverflow, true
		}
		if class == ErrorClassInvalidRequest && messageIndicatesCapacity(message) {
			return ErrorClassTransient, true
		}
		return class, true
	}
	return classForMessage(message)
}

func classForProviderCode(code string) (ErrorClass, bool) {
	switch code {
	case "context_length_exceeded", "context-window-exceeded", "context_window_exceeded", "model_context_window_exceeded", "request_too_large":
		return ErrorClassContextOverflow, true
	case "authentication_error", "unauthorized", "unauthenticated", "invalid_api_key", "invalid_api_key_error":
		return ErrorClassAuth, true
	// OpenCode sends FreeUsageLimitError and GoUsageLimitError as 429 error
	// types. 1308 is Zhipu/Z.ai's plan usage cap, a 429 that resets after hours.
	case "insufficient_quota", "quota_exceeded", "usage_limit_reached", "freeusagelimiterror", "gousagelimiterror", "1308":
		return ErrorClassQuota, true
	case "billing", "billing_error", "payment_required", "usage_not_included", "out_of_budget":
		return ErrorClassBilling, true
	case "rate_limit_error", "rate_limit_exceeded", "rate_limited", "too_many_requests", "throttlingexception":
		return ErrorClassRateLimited, true
	case "server_error", "server_busy", "internal_error", "api_error", "overloaded_error", "service_unavailable", "serviceunavailableexception", "internalserverexception", "modelstreamerrorexception", "network_error", "websocket_connection_limit_reached", "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable":
		return ErrorClassTransient, true
	case "invalid_request_error", "invalid_prompt", "validationexception":
		return ErrorClassInvalidRequest, true
	default:
		return "", false
	}
}

// bodylessCerebrasOverflow reports Cerebras's context overflow, which arrives
// as an HTTP 400 with no body.
func bodylessCerebrasOverflow(provider ProviderID, status int, code string, message string, body string) bool {
	return provider == ProviderCerebras && status == http.StatusBadRequest &&
		strings.TrimSpace(code+message+body) == ""
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
	case status == http.StatusRequestTimeout || status == http.StatusConflict:
		return ErrorClassTransient, true
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
	// Status numbers in text are checked last: a number can also be a limit
	// or count in an otherwise classifiable message.
	case containsStandaloneToken(message, "429", isASCIIAlphanumeric):
		return ErrorClassRateLimited, true
	case containsAnyStandaloneToken(message, retryableStatusTokens, isASCIIAlphanumeric):
		return ErrorClassTransient, true
	default:
		return "", false
	}
}

// retryableStatusTokens are server and Cloudflare origin statuses (520 unknown
// error, 524 timeout, 525 TLS handshake failure) that gateways relay as text.
var retryableStatusTokens = []string{"500", "502", "503", "504", "520", "524", "525"}

// socketErrorTokens are transport error codes that relays report as bare text,
// such as "read ECONNRESET" or "write EPIPE".
var socketErrorTokens = []string{"econnreset", "econnaborted", "econnrefused", "etimedout", "epipe"}

func containsAnyStandaloneToken(message string, tokens []string, isWordByte func(byte) bool) bool {
	for _, token := range tokens {
		if containsStandaloneToken(message, token, isWordByte) {
			return true
		}
	}
	return false
}

// containsStandaloneToken reports whether token occurs in message without a
// word byte immediately before or after it, so "502" does not match "15025".
func containsStandaloneToken(message string, token string, isWordByte func(byte) bool) bool {
	for offset := 0; ; {
		index := strings.Index(message[offset:], token)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(token)
		if (start == 0 || !isWordByte(message[start-1])) && (end == len(message) || !isWordByte(message[end])) {
			return true
		}
		offset = start + 1
	}
}

func isASCIIAlphanumeric(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}

func isIdentifierByte(b byte) bool {
	return isASCIIAlphanumeric(b) || b == '_'
}

func classRetryable(class ErrorClass) bool {
	return class == ErrorClassTransient || class == ErrorClassRateLimited
}

func classSplitRecoverable(class ErrorClass) bool {
	return class == ErrorClassContextOverflow
}

// IsContextOverflow reports whether a final assistant message indicates that a
// request exceeded the model context window.
//
// Error messages are detected from safe provider diagnostics. Usage-based
// detection requires a positive contextWindow supplied by the caller.
func IsContextOverflow(message AssistantMessage, contextWindow int) bool {
	if message.StopReason == StopReasonError {
		for _, diagnostic := range message.Diagnostics {
			if diagnosticClass(diagnostic) == ErrorClassContextOverflow {
				return true
			}
		}
	}
	if contextWindow <= 0 || message.Usage == nil {
		return false
	}
	inputTokens := message.Usage.InputTokens + message.Usage.CacheReadInputTokens
	if message.StopReason == StopReasonEndTurn && inputTokens > contextWindow {
		return true
	}
	return message.StopReason == StopReasonMaxTokens &&
		message.Usage.OutputTokens == 0 &&
		inputTokens*100 >= contextWindow*99
}

// IsRecoverableMaxTokens reports whether a max-token stop ended below the
// caller's intended output limit. Callers should pass the original requested
// or model limit before any context-based clamping.
func IsRecoverableMaxTokens(message AssistantMessage, desiredMaxOutput int) bool {
	return message.StopReason == StopReasonMaxTokens &&
		desiredMaxOutput > 0 &&
		message.Usage != nil &&
		message.Usage.OutputTokens < desiredMaxOutput
}

func diagnosticClass(diagnostic Diagnostic) ErrorClass {
	if diagnostic.Kind == string(ErrorContextOverflow) {
		return ErrorClassContextOverflow
	}
	code := normalizedErrorText(diagnostic.ProviderCode)
	message := normalizedErrorText(firstNonEmpty(
		diagnostic.ProviderMessage,
		diagnostic.BodyPreview,
		diagnostic.UnderlyingMessage,
		diagnostic.Message,
	))
	if class, ok := classForCodeAndMessage(code, message); ok {
		return class
	}
	if bodylessCerebrasOverflow(diagnostic.Provider, diagnostic.StatusCode, diagnostic.ProviderCode, diagnostic.ProviderMessage, diagnostic.BodyPreview) {
		return ErrorClassContextOverflow
	}
	if class, ok := classForStatus(effectiveErrorStatus(diagnostic.StatusCode, code)); ok {
		return class
	}
	return ErrorClassUnknown
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
		strings.Contains(message, "rate_limit") ||
		strings.Contains(message, "ratelimit") ||
		strings.Contains(message, "too many requests") ||
		strings.Contains(message, "throttling")
}

func messageIndicatesTransient(message string) bool {
	return messageIndicatesCapacity(message) ||
		strings.Contains(message, "overloaded") ||
		strings.Contains(message, "service unavailable") ||
		strings.Contains(message, "server error") ||
		strings.Contains(message, "deadline exceeded") ||
		strings.Contains(message, "internal error") ||
		strings.Contains(message, "provider returned error") ||
		strings.Contains(message, "exceeded request buffer limit while retrying upstream") ||
		strings.Contains(message, "upstream connect") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "connection lost") ||
		strings.Contains(message, "other side closed") ||
		strings.Contains(message, "reset before headers") ||
		strings.Contains(message, "socket hang up") ||
		strings.Contains(message, "socket connection was closed") ||
		strings.Contains(message, "websocket closed") ||
		strings.Contains(message, "websocket error") ||
		strings.Contains(message, "getaddrinfo") ||
		strings.Contains(message, "enotfound") ||
		strings.Contains(message, "eai_again") ||
		strings.Contains(message, "http2 request did not get a response") ||
		strings.Contains(message, "stream error: stream id") ||
		strings.Contains(message, "server sent goaway") ||
		strings.Contains(message, "you can retry your request") ||
		strings.Contains(message, "try your request again") ||
		strings.Contains(message, "please retry your request") ||
		strings.Contains(message, "resourceexhausted") ||
		strings.Contains(message, "service is temporarily unavailable") ||
		strings.Contains(message, "service temporarily unavailable") ||
		strings.Contains(message, "provider is temporarily unavailable") ||
		strings.Contains(message, "provider temporarily unavailable") ||
		strings.Contains(message, "admission ceiling") ||
		strings.Contains(message, "connection closed") ||
		strings.Contains(message, "stream was interrupted") ||
		strings.Contains(message, "upstream_stream_read_error") ||
		strings.Contains(message, "unable to forward upstream") ||
		strings.Contains(message, "upstream request failed") ||
		strings.Contains(message, "provider finish_reason: error") ||
		strings.Contains(message, "returned an empty response") ||
		strings.Contains(message, "stopped before completing the response") ||
		strings.Contains(message, "service is unavailable") ||
		strings.Contains(message, "internal_error") ||
		strings.Contains(message, "server_error") ||
		strings.Contains(message, "network error") ||
		strings.Contains(message, "network_error") ||
		strings.Contains(message, "connection error") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "fetch failed") ||
		strings.Contains(message, "timed out") ||
		strings.Contains(message, "http2 stream failed") ||
		strings.Contains(message, "http/2 stream failed") ||
		strings.Contains(message, "websocket connection limit reached") ||
		strings.Contains(message, "websocket_connection_limit_reached") ||
		strings.Contains(message, "stream disconnected before completion") ||
		strings.Contains(message, "cut short after the reply had begun") ||
		strings.Contains(message, "ended the response before it was complete") ||
		strings.Contains(message, "connection to the provider was reset") ||
		strings.Contains(message, "reported a failure while generating the response") ||
		strings.Contains(message, "pending stream has been canceled") ||
		strings.Contains(message, "repeat the request later") ||
		strings.Contains(message, "subscription_sharing_usage_unavailable") ||
		strings.Contains(message, "subscription_sharing_user_unavailable") ||
		containsAnyStandaloneToken(message, socketErrorTokens, isIdentifierByte) ||
		messageIndicatesPrematureProviderStreamTermination(message)
}

// messageIndicatesCapacity reports temporary provider load that clears on retry.
func messageIndicatesCapacity(message string) bool {
	return strings.Contains(message, "server_busy") ||
		strings.Contains(message, "servers are currently busy") ||
		strings.Contains(message, "currently experiencing high demand") ||
		strings.Contains(message, "model is at capacity")
}

func messageIndicatesPrematureProviderStreamTermination(message string) bool {
	return strings.Contains(message, "radius messages: stream ended without a terminal event") ||
		strings.Contains(message, "openai completions: stream ended without finish_reason") ||
		strings.Contains(message, "openai images: stream ended without completion event") ||
		strings.Contains(message, "stream ended before terminal response event") ||
		strings.Contains(message, "stream ended before message_stop") ||
		strings.Contains(message, "anthropic messages: stream ended without a stop reason") ||
		strings.Contains(message, "bedrock converse stream: stream ended without a stop reason") ||
		strings.Contains(message, "stream ended before finish reason") ||
		strings.Contains(message, "stream ended before conversation.response.done")
}

func messageIndicatesContextOverflow(message string) bool {
	if messageIndicatesRateLimit(message) || messageIndicatesTransient(message) {
		return false
	}
	return strings.Contains(message, "context_length_exceeded") ||
		strings.Contains(message, "model_context_window_exceeded") ||
		strings.Contains(message, "request_too_large") ||
		strings.Contains(message, "request too large") ||
		strings.Contains(message, "maximum prompt length") ||
		strings.Contains(message, "maximum context length") ||
		strings.Contains(message, "maximum allowed input length") ||
		strings.Contains(message, "token limit exceeded") ||
		strings.Contains(message, "exceeded model token limit") ||
		strings.Contains(message, "tokenizer") && strings.Contains(message, "eof") ||
		strings.Contains(message, "tokenizer") && strings.Contains(message, "too large") ||
		strings.Contains(message, "context") && (strings.Contains(message, "too long") || strings.Contains(message, "longer than") || strings.Contains(message, "maximum") || strings.Contains(message, "exceed") || strings.Contains(message, "greater than")) ||
		strings.Contains(message, "prompt is too long") ||
		strings.Contains(message, "prompt too long") ||
		strings.Contains(message, "prompt exceeds max length") ||
		strings.Contains(message, "but the configured context size is") ||
		strings.Contains(message, "range of input length should be") ||
		strings.Contains(message, "too many tokens") ||
		strings.Contains(message, "input is too long for requested model") ||
		strings.Contains(message, "input token count") && strings.Contains(message, "exceeds the maximum") ||
		strings.Contains(message, "prompt token count") && strings.Contains(message, "exceeds the limit") ||
		strings.Contains(message, "too large for model with") && strings.Contains(message, "maximum context length") ||
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
