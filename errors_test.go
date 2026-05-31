// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma/internal/redact"
)

func TestErrorSentinelsMatchTypedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		target error
	}{
		{
			name:   "no provider",
			err:    &Error{Code: ErrorProviderNotFound, Message: "text provider is not registered"},
			target: ErrNoProvider,
		},
		{
			name:   "model not found",
			err:    &Error{Code: ErrorModelNotFound, Message: "model is not registered"},
			target: ErrModelNotFound,
		},
		{
			name:   "aborted",
			err:    &Error{Code: ErrorAborted, Message: "stream aborted"},
			target: ErrAborted,
		},
		{
			name:   "context overflow",
			err:    &Error{Code: ErrorContextOverflow, Message: "too many input tokens"},
			target: ErrContextOverflow,
		},
		{
			name:   "tool validation",
			err:    &Error{Code: ErrorToolValidation, Message: "tool schema is invalid"},
			target: ErrToolValidation,
		},
		{
			name:   "invalid options",
			err:    &Error{Code: ErrorInvalidOptions, Message: "temperature must be non-negative"},
			target: ErrInvalidOptions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !stderrors.Is(tt.err, tt.target) {
				t.Fatalf("errors.Is(%T, %v) = false", tt.err, tt.target)
			}
		})
	}
}

func TestProviderErrorRedactsBodyCauseAndDiagnostics(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"error": {"message": "quota exceeded"},
		"api_key": "sk-live-jsonsecret",
		"nested": {
			"access_token": "access-secret",
			"refresh_token": "refresh-secret",
			"device_code": "device-secret",
			"user_code": "user-secret"
		},
		"url": "https://example.test/download?X-Amz-Signature=signed-secret&access_token=query-secret"
	}`)
	err := NewProviderError(
		ProviderOpenAI,
		APIOpenAIResponses,
		"gpt-test",
		429,
		"req_123",
		2*time.Second,
		body,
		stderrors.New("upstream said Authorization: Bearer sk-live-cause"),
	)

	assertNoSecrets(t, err.BodyPreview)
	assertNoSecrets(t, err.Error())
	assertNoSecrets(t, fmt.Sprintf("%+v", err))
	if !stderrors.Is(err, ErrProviderResponse) {
		t.Fatal("ProviderError does not match ErrProviderResponse")
	}

	var providerErr *ProviderError
	if !stderrors.As(err, &providerErr) {
		t.Fatalf("errors.As(%T, *ProviderError) = false", err)
	}
	if providerErr.StatusCode != 429 {
		t.Fatalf("status code = %d, want 429", providerErr.StatusCode)
	}

	diagnostic := err.Diagnostic()
	assertNoSecrets(t, diagnostic.BodyPreview)
	assertNoSecrets(t, diagnostic.UnderlyingMessage)
	if diagnostic.RetryAfterMillis != 2000 {
		t.Fatalf("retry after millis = %d, want 2000", diagnostic.RetryAfterMillis)
	}
}

func TestClassifyError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		class     ErrorClass
		retryable bool
		after     time.Duration
		code      string
	}{
		{
			name:  "nil",
			class: ErrorClassUnknown,
		},
		{
			name:  "invalid options",
			err:   &Error{Code: ErrorInvalidOptions, Message: "bad option"},
			class: ErrorClassInvalidRequest,
		},
		{
			name:  "unsupported",
			err:   &Error{Code: ErrorUnsupported, Message: "unsupported"},
			class: ErrorClassInvalidRequest,
		},
		{
			name:  "credential unavailable",
			err:   &Error{Message: "missing key", Err: ErrCredentialUnavailable},
			class: ErrorClassAuth,
		},
		{
			name:  "wrapped provider",
			err:   &GenerationError{Err: NewProviderError(ProviderOpenAI, APIOpenAIResponses, "gpt-test", 401, "req_1", 0, []byte(`{"error":{"code":"unauthorized","message":"bad key"}}`), ErrProviderResponse)},
			class: ErrorClassAuth,
			code:  "unauthorized",
		},
		{
			name:  "quota",
			err:   NewProviderError(ProviderOpenAI, APIOpenAIResponses, "gpt-test", 429, "", 0, []byte(`{"error":{"code":"insufficient_quota","message":"quota exceeded"}}`), ErrProviderResponse),
			class: ErrorClassQuota,
			code:  "insufficient_quota",
		},
		{
			name:  "billing",
			err:   NewProviderError(ProviderOpenAI, APIOpenAIResponses, "gpt-test", 429, "", 0, []byte(`{"error":{"code":"usage_not_included","message":"upgrade your plan"}}`), ErrProviderResponse),
			class: ErrorClassBilling,
			code:  "usage_not_included",
		},
		{
			name:      "rate limited",
			err:       NewProviderError(ProviderOpenAI, APIOpenAIResponses, "gpt-test", 429, "", 2*time.Second, []byte(`{"error":{"message":"slow down"}}`), ErrProviderResponse),
			class:     ErrorClassRateLimited,
			retryable: true,
			after:     2 * time.Second,
		},
		{
			name:      "transient",
			err:       NewProviderError(ProviderOpenAI, APIOpenAIResponses, "gpt-test", 503, "", 0, []byte(`{"error":{"message":"try later"}}`), ErrProviderResponse),
			class:     ErrorClassTransient,
			retryable: true,
		},
		{
			name:  "context overflow",
			err:   NewProviderError(ProviderOpenAI, APIOpenAIResponses, "gpt-test", 400, "", 0, []byte(`{"error":{"code":"context_length_exceeded","message":"too many tokens"}}`), ErrContextOverflow),
			class: ErrorClassContextOverflow,
			code:  "context_length_exceeded",
		},
		{
			name:  "invalid request",
			err:   NewProviderError(ProviderOpenAI, APIOpenAIResponses, "gpt-test", 422, "", 0, []byte(`{"error":{"message":"bad request"}}`), ErrProviderResponse),
			class: ErrorClassInvalidRequest,
		},
		{
			name:  "provider",
			err:   NewProviderError(ProviderOpenAI, APIOpenAIResponses, "gpt-test", 418, "", 0, []byte(`{"error":{"message":"teapot"}}`), ErrProviderResponse),
			class: ErrorClassProvider,
		},
		{
			name:      "rate limit text wins over token overflow text on 429",
			err:       NewProviderError(ProviderAmazonBedrock, APIBedrockConverseStream, "claude-test", 429, "", 0, []byte(`{"error":{"message":"Throttling error: Too many tokens, please wait before trying again."}}`), ErrProviderResponse),
			class:     ErrorClassRateLimited,
			retryable: true,
		},
		{
			name:  "generic plan wording is not billing",
			err:   NewProviderError(ProviderOpenAI, APIOpenAIResponses, "gpt-test", 0, "", 0, []byte(`{"error":{"message":"invalid execution plan"}}`), ErrProviderResponse),
			class: ErrorClassUnknown,
		},
		{
			name:  "structured code wins over status",
			err:   NewProviderError(ProviderOpenAI, APIOpenAIResponses, "gpt-test", 400, "", 0, []byte(`{"error":{"code":"usage_not_included","message":"upgrade required"}}`), ErrProviderResponse),
			class: ErrorClassBilling,
			code:  "usage_not_included",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			classification := ClassifyError(tt.err)
			if classification.Class != tt.class {
				t.Fatalf("class = %q, want %q", classification.Class, tt.class)
			}
			if classification.RetryHint.Retryable != tt.retryable {
				t.Fatalf("retryable = %v, want %v", classification.RetryHint.Retryable, tt.retryable)
			}
			if classification.RetryHint.After != tt.after {
				t.Fatalf("retry after = %v, want %v", classification.RetryHint.After, tt.after)
			}
			if classification.ProviderCode != tt.code {
				t.Fatalf("provider code = %q, want %q", classification.ProviderCode, tt.code)
			}
		})
	}
}

func TestProviderErrorClassificationRedactsParsedFields(t *testing.T) {
	t.Parallel()

	err := NewProviderError(
		ProviderOpenAI,
		APIOpenAIResponses,
		"gpt-test",
		401,
		"req_secret",
		0,
		[]byte(`{"error":{"code":"unauthorized","message":"bad key sk-live-jsonsecret"}}`),
		ErrProviderResponse,
	)

	assertNoSecrets(t, err.ProviderMessage)
	assertNoSecrets(t, err.Error())
	classification := ClassifyError(err)
	assertNoSecrets(t, classification.Message)
	if classification.Class != ErrorClassAuth {
		t.Fatalf("class = %q, want %q", classification.Class, ErrorClassAuth)
	}
}

func TestRedactionCoversHeadersQueriesJSONAndMultilineBodies(t *testing.T) {
	t.Parallel()

	headers := redact.Headers(map[string][]string{
		"Authorization": {"Bearer sk-live-headersecret"},
		"Cookie":        {"session=header-cookie-secret; other=value"},
		"X-Debug":       {"callback=https://example.test/cb?api_key=query-secret&X-Goog-Signature=signed-secret"},
	})

	if got := headers["Authorization"][0]; got != "[redacted]" {
		t.Fatalf("authorization header = %q, want redacted", got)
	}
	if got := headers["Cookie"][0]; got != "[redacted]" {
		t.Fatalf("cookie header = %q, want redacted", got)
	}
	assertNoSecrets(t, headers["X-Debug"][0])

	multiline := strings.Join([]string{
		"HTTP/1.1 401 Unauthorized",
		"Authorization: Bearer sk-live-multiline",
		"Set-Cookie: session=multiline-cookie-secret",
		`{"api_key":"json-secret","access_token":"access-secret","refresh_token":"refresh-secret"}`,
		"https://example.test/upload?AWSAccessKeyId=aws-secret&Signature=signed-secret&device_code=device-secret",
	}, "\n")

	redacted := redact.String(multiline)
	assertNoSecrets(t, redacted)
	if !strings.Contains(redacted, "[redacted]") {
		t.Fatalf("redacted body = %q, want redaction marker", redacted)
	}
	if !strings.Contains(redacted, "401 Unauthorized") {
		t.Fatalf("redacted body lost non-secret context: %q", redacted)
	}
}

func TestGenerationErrorCarriesFinalMessageAndWrappedProviderError(t *testing.T) {
	t.Parallel()

	providerErr := NewProviderError(
		ProviderAnthropic,
		APIAnthropicMessages,
		"claude-test",
		500,
		"req_provider_failure",
		0,
		[]byte(`{"access_token":"stream-secret","error":"provider failed"}`),
		stderrors.New("provider failed"),
	)
	final := AssistantMessage{
		Content:     []ContentBlock{Text("partial answer")},
		StopReason:  StopReasonError,
		Model:       "claude-test",
		Provider:    ProviderAnthropic,
		Diagnostics: []Diagnostic{providerErr.Diagnostic()},
	}
	stream, writer := NewStream(context.Background())
	writeErr := make(chan error, 1)
	go func() {
		writeErr <- writer.Error(context.Background(), providerErr, final)
	}()

	collected, err := Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("writer returned error: %v", err)
	}
	if got, want := collected.StopReason, StopReasonError; got != want {
		t.Fatalf("collected stop reason = %q, want %q", got, want)
	}

	var generationErr *GenerationError
	if !stderrors.As(err, &generationErr) {
		t.Fatalf("Collect error type = %T, want GenerationError", err)
	}
	finalFromError, ok := generationErr.FinalMessage()
	if !ok {
		t.Fatal("GenerationError missing final message")
	}
	if got, want := finalFromError.Content[0].Text, "partial answer"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
	if !stderrors.Is(err, ErrProviderResponse) {
		t.Fatal("GenerationError does not match ErrProviderResponse")
	}

	var unwrappedProvider *ProviderError
	if !stderrors.As(err, &unwrappedProvider) {
		t.Fatal("GenerationError does not expose ProviderError")
	}
	assertNoSecrets(t, err.Error())
}

func TestGenerationErrorMapsAbortedStopReasonToSentinel(t *testing.T) {
	t.Parallel()

	stream, writer := NewStream(context.Background())
	writeErr := make(chan error, 1)
	go func() {
		writeErr <- writer.Error(
			context.Background(),
			context.Canceled,
			AssistantMessage{StopReason: StopReasonAborted},
		)
	}()

	final, err := Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("writer returned error: %v", err)
	}
	if got, want := final.StopReason, StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if !stderrors.Is(err, ErrAborted) {
		t.Fatal("GenerationError does not match ErrAborted")
	}
}

func assertNoSecrets(t *testing.T, value string) {
	t.Helper()

	secrets := []string{
		"sk-live-jsonsecret",
		"access-secret",
		"refresh-secret",
		"device-secret",
		"user-secret",
		"signed-secret",
		"query-secret",
		"sk-live-cause",
		"sk-live-headersecret",
		"header-cookie-secret",
		"multiline-cookie-secret",
		"sk-live-multiline",
		"aws-secret",
		"stream-secret",
	}
	for _, secret := range secrets {
		if strings.Contains(value, secret) {
			t.Fatalf("value leaked %q: %q", secret, value)
		}
	}
}
