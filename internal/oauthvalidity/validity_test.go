// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package oauthvalidity

import (
	"testing"
	"time"
)

func TestNeedsRefresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	zero := time.Duration(0)
	shorter := 30 * time.Second
	longer := 10 * time.Minute
	tests := []struct {
		name       string
		expiry     time.Time
		configured time.Duration
		requested  *time.Duration
		want       bool
	}{
		{name: "omitted outside configured window", expiry: now.Add(2 * time.Minute), configured: time.Minute},
		{name: "zero does not extend configured window", expiry: now.Add(2 * time.Minute), configured: time.Minute, requested: &zero},
		{name: "shorter cannot reduce configured window", expiry: now.Add(45 * time.Second), configured: time.Minute, requested: &shorter, want: true},
		{name: "longer extends configured window", expiry: now.Add(5 * time.Minute), configured: time.Minute, requested: &longer, want: true},
		{name: "exact boundary refreshes", expiry: now.Add(10 * time.Minute), configured: time.Minute, requested: &longer, want: true},
		{name: "unknown expiry never refreshes", configured: time.Minute, requested: &longer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := NeedsRefresh(now, tt.expiry, tt.configured, tt.requested); got != tt.want {
				t.Fatalf("NeedsRefresh() = %v, want %v", got, tt.want)
			}
		})
	}
}
