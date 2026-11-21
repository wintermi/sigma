// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"testing"
	"time"
)

func TestRetryPolicyBackoffDoublesWithJitterAndCap(t *testing.T) {
	t.Parallel()

	policy := retryPolicyFromOptions(Options{})
	for _, tt := range []struct {
		attempt int
		max     time.Duration
	}{
		{attempt: 0, max: 500 * time.Millisecond},
		{attempt: 1, max: time.Second},
		{attempt: 3, max: 4 * time.Second},
		{attempt: 4, max: 8 * time.Second},
		{attempt: 40, max: 8 * time.Second},
	} {
		for range 50 {
			got := policy.delay(tt.attempt, 0)
			if got > tt.max || got < tt.max*3/4 {
				t.Fatalf("delay(%d) = %v, want within [%v, %v]", tt.attempt, got, tt.max*3/4, tt.max)
			}
		}
	}

	capped := retryPolicyFromOptions(Options{MaxRetryDelay: durationPtr(time.Second)})
	if got := capped.delay(5, 0); got > time.Second {
		t.Fatalf("capped delay = %v, want at most 1s", got)
	}
	if got := retryPolicyFromOptions(Options{MaxRetryDelay: durationPtr(0)}).delay(2, 0); got != 0 {
		t.Fatalf("zero-cap delay = %v, want 0", got)
	}
}

func TestRetryPolicyDefaultHonoursTypicalRetryAfter(t *testing.T) {
	t.Parallel()

	policy := retryPolicyFromOptions(Options{})
	retryAfter := 5 * time.Second
	if policy.retryAfterExceedsCap(retryAfter) {
		t.Fatalf("default policy rejects Retry-After %v", retryAfter)
	}
	if got := policy.delay(0, retryAfter); got != retryAfter {
		t.Fatalf("delay with Retry-After = %v, want %v", got, retryAfter)
	}
	if !policy.retryAfterExceedsCap(2 * time.Minute) {
		t.Fatal("default policy accepts a Retry-After above 60s")
	}
}
