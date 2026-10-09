// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"errors"
	"testing"

	"github.com/wintermi/sigma"
)

func TestNewProviderErrorKeepsOverflowCauseOnlyForOverflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		body     string
		want     sigma.ErrorClass
		overflow bool
	}{
		{
			name:   "gateway deadline exceeded",
			status: 500,
			body:   `{"error":{"message":"Post \"https://upstream/v1/chat\": context deadline exceeded (Client.Timeout exceeded while awaiting headers)","code":"do_request_failed"}}`,
			want:   sigma.ErrorClassTransient,
		},
		{
			name:   "rate limit code for long context",
			status: 429,
			body:   `{"error":{"code":"rate_limit_exceeded","message":"Rate limit exceeded for long context requests"}}`,
			want:   sigma.ErrorClassRateLimited,
		},
		{
			name:   "throttled token rate without code",
			status: 429,
			body:   `{"message":"Too many tokens, please wait before trying again."}`,
			want:   sigma.ErrorClassRateLimited,
		},
		{
			name:     "context length exceeded",
			status:   400,
			body:     `{"error":{"message":"This model's maximum context length is 128000 tokens.","type":"invalid_request_error"}}`,
			want:     sigma.ErrorClassContextOverflow,
			overflow: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sigma.NewProviderError("custom", sigma.APIOpenAICompletions, "model", tt.status, "", 0, []byte(tt.body), sigma.ErrContextOverflow)
			if got := errors.Is(err, sigma.ErrContextOverflow); got != tt.overflow {
				t.Fatalf("errors.Is(ErrContextOverflow) = %v, want %v", got, tt.overflow)
			}
			if !errors.Is(err, sigma.ErrProviderResponse) {
				t.Fatal("provider error lost ErrProviderResponse")
			}
			if got := sigma.ClassifyError(err).Class; got != tt.want {
				t.Fatalf("class = %q, want %q", got, tt.want)
			}
		})
	}
}
