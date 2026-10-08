// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package kimi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type refreshRoundTripper func(*http.Request) *http.Response

func (f refreshRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r), nil
}

func TestRefreshKimiCodingTokenRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		first     int
		wantCalls int32
		wantErr   bool
	}{
		{name: "server error is retried", first: http.StatusServiceUnavailable, wantCalls: 2},
		{name: "rate limit is retried", first: http.StatusTooManyRequests, wantCalls: 2},
		{name: "unauthorized is not retried", first: http.StatusUnauthorized, wantCalls: 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			client := &http.Client{Transport: refreshRoundTripper(func(*http.Request) *http.Response {
				status, body := http.StatusOK, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`
				if calls.Add(1) == 1 {
					status, body = tt.first, `{"error":"failure"}`
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
			})}

			credentials, err := RefreshKimiCodingToken(context.Background(), "refresh-token", KimiCodingOAuthTokenProviderOptions{HTTPClient: client})
			if (err != nil) != tt.wantErr {
				t.Fatalf("RefreshKimiCodingToken error = %v, want error %v", err, tt.wantErr)
			}
			if !tt.wantErr && credentials.AccessToken != "access" {
				t.Fatalf("access token = %q, want access", credentials.AccessToken)
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Fatalf("token requests = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}
