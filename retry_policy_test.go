// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// Providers can override the status-based retry decision with x-should-retry,
// as the OpenAI and Anthropic SDKs honor.
func TestDoHTTPWithRetryHonorsShouldRetryHeader(t *testing.T) {
	t.Parallel()

	zeroDelay := time.Duration(0)
	maxRetries := 1
	shouldRetry := func(value string) func(http.Header) {
		return func(header http.Header) { header.Set("x-should-retry", value) }
	}
	tests := []struct {
		name     string
		first    func(*http.Request) (*http.Response, error)
		attempts int
	}{
		{name: "retryable 4xx", first: retryResponse(http.StatusBadRequest, "retry me", shouldRetry("true")), attempts: 2},
		{name: "non-retryable 5xx", first: retryResponse(http.StatusServiceUnavailable, "do not retry", shouldRetry("false")), attempts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := retryHTTPClient(tt.first, retryResponse(http.StatusOK, "ok"))
			resp, err := DoHTTPWithRetry(context.Background(), client, Options{MaxRetries: &maxRetries, MaxRetryDelay: &zeroDelay}, retryRequest, retryProviderError)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if got := retryAttempts(client); got != tt.attempts {
				t.Fatalf("attempts = %d, want %d", got, tt.attempts)
			}
		})
	}
}

func TestRetryAfterAcceptsFractionalValues(t *testing.T) {
	t.Parallel()

	if got, want := ParseRetryAfter("1.5", time.Now()), 1500*time.Millisecond; got != want {
		t.Fatalf("Retry-After 1.5 = %v, want %v", got, want)
	}
	header := http.Header{"Retry-After-Ms": []string{"2.5"}}
	if got, want := RetryAfter(header), 2500*time.Microsecond; got != want {
		t.Fatalf("Retry-After-Ms 2.5 = %v, want %v", got, want)
	}
	for _, invalid := range []string{"-1", "1e3", "NaN", "1.2.3"} {
		if got := ParseRetryAfter(invalid, time.Now()); got != 0 {
			t.Fatalf("Retry-After %q = %v, want 0", invalid, got)
		}
	}
}
