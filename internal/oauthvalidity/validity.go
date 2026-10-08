// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package oauthvalidity applies shared OAuth credential lifetime rules.
package oauthvalidity

import (
	"context"
	"time"
)

// RefreshTimeout bounds a started OAuth refresh and the persistence of its
// result.
const RefreshTimeout = 15 * time.Second

// RefreshContext returns the context for a refresh that has already started.
// Providers may rotate the refresh token as soon as they receive the request,
// so caller cancellation must not abandon the refresh or its persistence; the
// refresh is bounded by RefreshTimeout instead.
func RefreshContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), RefreshTimeout)
}

// NeedsRefresh reports whether expiry falls within the effective refresh
// window. A request minimum may lengthen, but never shorten, configured.
func NeedsRefresh(now time.Time, expiry time.Time, configured time.Duration, requested *time.Duration) bool {
	if expiry.IsZero() {
		return false
	}
	refreshBefore := configured
	if requested != nil && *requested > refreshBefore {
		refreshBefore = *requested
	}
	return !now.Add(refreshBefore).Before(expiry)
}
