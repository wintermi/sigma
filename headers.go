// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"net/http"
	"strings"
)

// ApplySuppressedHeaders removes request headers configured with
// WithSuppressedHeader or WithSuppressedHeaders.
func ApplySuppressedHeaders(headers http.Header, opts Options) {
	if len(headers) == 0 || len(opts.SuppressedHeaders) == 0 {
		return
	}
	for _, suppressed := range opts.SuppressedHeaders {
		key := strings.TrimSpace(suppressed)
		if key == "" || protectedSuppressedHeader(key) {
			continue
		}
		deleteHeaderCaseInsensitive(headers, key)
	}
}

func deleteHeaderCaseInsensitive(headers http.Header, key string) {
	expected := strings.ToLower(key)
	for existing := range headers {
		if strings.ToLower(existing) == expected {
			delete(headers, existing)
		}
	}
}

func protectedSuppressedHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization", "x-api-key", string(CredentialTypeAPIKey), "cf-aig-authorization", "x-goog-api-key":
		return true
	default:
		return false
	}
}
