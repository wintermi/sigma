// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package headerutil merges HTTP header options without changing caller maps.
package headerutil

import (
	"sort"
	"strings"
)

// Merge copies headers in precedence order, retaining the winning name's
// spelling. Sorting each input makes case variants in one map deterministic.
func Merge(base, override map[string]string) map[string]string {
	if len(base)+len(override) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(override))
	for _, headers := range []map[string]string{base, override} {
		keys := make([]string, 0, len(headers))
		for key := range headers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			setHeader(merged, key, headers[key])
		}
	}
	return merged
}

func setHeader(headers map[string]string, key, value string) {
	for existing := range headers {
		if strings.EqualFold(existing, key) {
			delete(headers, existing)
		}
	}
	headers[key] = value
}
