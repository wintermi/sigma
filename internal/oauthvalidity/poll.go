// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package oauthvalidity

import "time"

// MinimumPollInterval is the shortest wait between device-flow token polls,
// whatever interval the server reports, so a zero or tiny interval cannot
// turn polling into a tight loop.
const MinimumPollInterval = time.Second

// PollInterval returns interval raised to MinimumPollInterval.
func PollInterval(interval time.Duration) time.Duration {
	return max(interval, MinimumPollInterval)
}
