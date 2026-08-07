// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package oauthvalidity applies shared OAuth credential lifetime rules.
package oauthvalidity

import "time"

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
