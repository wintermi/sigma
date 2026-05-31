// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDoHTTPWithRetryDoesNotRetryByDefault(t *testing.T) {
	t.Parallel()

	client := retryHTTPClient(
		retryResponse(http.StatusInternalServerError, "temporary"),
		retryResponse(http.StatusOK, "ok"),
	)

	resp, err := DoHTTPWithRetry(context.Background(), client, Options{}, retryRequest, retryProviderError)
	if err != nil {
		t.Fatalf("DoHTTPWithRetry returned error: %v", err)
	}
	defer resp.Body.Close()

	if got, want := resp.StatusCode, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := retryAttempts(client), 1; got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
}

func TestDoHTTPWithRetryRetries429And5xx(t *testing.T) {
	t.Parallel()

	zeroDelay := time.Duration(0)
	maxRetries := 2
	client := retryHTTPClient(
		retryResponse(http.StatusInternalServerError, "temporary"),
		retryResponse(http.StatusTooManyRequests, "rate limited"),
		retryResponse(http.StatusOK, "ok"),
	)

	resp, err := DoHTTPWithRetry(context.Background(), client, Options{
		MaxRetries:    &maxRetries,
		MaxRetryDelay: &zeroDelay,
	}, retryRequest, retryProviderError)
	if err != nil {
		t.Fatalf("DoHTTPWithRetry returned error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := string(body), "ok"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got, want := retryAttempts(client), 3; got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
}

func TestDoHTTPWithRetryDoesNotRetryValidation4xx(t *testing.T) {
	t.Parallel()

	zeroDelay := time.Duration(0)
	maxRetries := 1
	client := retryHTTPClient(
		retryResponse(http.StatusBadRequest, "bad request"),
		retryResponse(http.StatusOK, "ok"),
	)

	resp, err := DoHTTPWithRetry(context.Background(), client, Options{
		MaxRetries:    &maxRetries,
		MaxRetryDelay: &zeroDelay,
	}, retryRequest, retryProviderError)
	if err != nil {
		t.Fatalf("DoHTTPWithRetry returned error: %v", err)
	}
	defer resp.Body.Close()

	if got, want := resp.StatusCode, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := retryAttempts(client), 1; got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "seconds", value: "3", want: 3 * time.Second},
		{name: "date", value: now.Add(5 * time.Second).Format(http.TimeFormat), want: 5 * time.Second},
		{name: "past date", value: now.Add(-time.Second).Format(http.TimeFormat), want: 0},
		{name: "invalid", value: "later", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ParseRetryAfter(tt.value, now); got != tt.want {
				t.Fatalf("ParseRetryAfter(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestRetryAfterPrefersMillisecondHeader(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set("Retry-After", "3")
	header.Set("Retry-After-Ms", "250")

	if got, want := RetryAfter(header), 250*time.Millisecond; got != want {
		t.Fatalf("RetryAfter = %v, want %v", got, want)
	}
}

func TestDoHTTPWithRetryReturnsProviderErrorWhenRetryAfterExceedsCap(t *testing.T) {
	t.Parallel()

	maxRetries := 1
	maxDelay := time.Second
	client := retryHTTPClient(retryResponse(http.StatusTooManyRequests, "slow down", retryHeader("Retry-After", "3")))

	_, err := DoHTTPWithRetry(context.Background(), client, Options{
		MaxRetries:    &maxRetries,
		MaxRetryDelay: &maxDelay,
	}, retryRequest, retryProviderError)
	if err == nil {
		t.Fatal("DoHTTPWithRetry returned nil error")
	}
	if !errors.Is(err, ErrRetryAfterExceedsMaxDelay) {
		t.Fatalf("error does not match ErrRetryAfterExceedsMaxDelay: %v", err)
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *ProviderError", err)
	}
	if got, want := providerErr.RetryAfter, 3*time.Second; got != want {
		t.Fatalf("retry after = %v, want %v", got, want)
	}
	if got := providerErr.MaxRetryDelay; got != maxDelay {
		t.Fatalf("max retry delay = %v, want %v", got, maxDelay)
	}
}

func TestDoHTTPWithRetryReturnsContextErrorDuringBackoff(t *testing.T) {
	t.Parallel()

	maxRetries := 1
	maxDelay := time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	client := retryHTTPClient(func(*http.Request) (*http.Response, error) {
		cancel()
		return retryResponse(http.StatusInternalServerError, "temporary")(nil)
	})

	_, err := DoHTTPWithRetry(ctx, client, Options{
		MaxRetries:    &maxRetries,
		MaxRetryDelay: &maxDelay,
	}, retryRequest, retryProviderError)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got, want := retryAttempts(client), 1; got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
}

func TestDoHTTPWithRetryRetriesTransientNetworkError(t *testing.T) {
	t.Parallel()

	zeroDelay := time.Duration(0)
	maxRetries := 1
	client := retryHTTPClient(
		func(*http.Request) (*http.Response, error) {
			return nil, temporaryNetworkError{}
		},
		retryResponse(http.StatusOK, "ok"),
	)

	resp, err := DoHTTPWithRetry(context.Background(), client, Options{
		MaxRetries:    &maxRetries,
		MaxRetryDelay: &zeroDelay,
	}, retryRequest, retryProviderError)
	if err != nil {
		t.Fatalf("DoHTTPWithRetry returned error: %v", err)
	}
	defer resp.Body.Close()
	if got, want := retryAttempts(client), 2; got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
}

func TestDoHTTPWithRetryLeavesStreamingResponseBodyUnread(t *testing.T) {
	t.Parallel()

	zeroDelay := time.Duration(0)
	maxRetries := 1
	successBody := &countingReadCloser{Reader: strings.NewReader("data: ok\n\n")}
	client := retryHTTPClient(
		retryResponse(http.StatusInternalServerError, "temporary"),
		func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       successBody,
			}, nil
		},
	)

	resp, err := DoHTTPWithRetry(context.Background(), client, Options{
		MaxRetries:    &maxRetries,
		MaxRetryDelay: &zeroDelay,
	}, retryRequest, retryProviderError)
	if err != nil {
		t.Fatalf("DoHTTPWithRetry returned error: %v", err)
	}
	defer resp.Body.Close()
	if successBody.reads != 0 {
		t.Fatalf("success body was read %d times before caller consumed it", successBody.reads)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if got, want := string(body), "data: ok\n\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

type retryRoundTripper struct {
	handlers []func(*http.Request) (*http.Response, error)
	attempts int
}

func retryHTTPClient(handlers ...func(*http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: &retryRoundTripper{handlers: handlers}}
}

func (t *retryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	t.attempts++
	if t.attempts > len(t.handlers) {
		return retryResponse(http.StatusTeapot, "unexpected attempt")(req)
	}
	return t.handlers[t.attempts-1](req)
}

func retryAttempts(client *http.Client) int {
	return client.Transport.(*retryRoundTripper).attempts
}

func retryRequest(ctx context.Context) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test/retry", strings.NewReader("{}"))
}

func retryResponse(status int, body string, headers ...func(http.Header)) func(*http.Request) (*http.Response, error) {
	return func(*http.Request) (*http.Response, error) {
		header := make(http.Header)
		for _, set := range headers {
			set(header)
		}
		return &http.Response{
			StatusCode: status,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}
}

func retryHeader(key, value string) func(http.Header) {
	return func(header http.Header) {
		header.Set(key, value)
	}
}

func retryProviderError(resp *http.Response) *ProviderError {
	body, _ := io.ReadAll(resp.Body)
	return NewProviderError("retry-provider", APIOpenAIResponses, "retry-model", resp.StatusCode, "", RetryAfter(resp.Header), body, nil)
}

type temporaryNetworkError struct{}

func (temporaryNetworkError) Error() string   { return "temporary network error" }
func (temporaryNetworkError) Timeout() bool   { return false }
func (temporaryNetworkError) Temporary() bool { return true }

type countingReadCloser struct {
	*strings.Reader
	reads int
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

func (r *countingReadCloser) Close() error {
	return nil
}
