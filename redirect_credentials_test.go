// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDoHTTPWithRetryKeepsCredentialHeadersOffRedirectTargets(t *testing.T) {
	t.Parallel()

	var leaked atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("x-api-key"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(other.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	newRequest := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin.URL, nil)
		if err == nil {
			req.Header.Set("x-api-key", "sk-secret")
		}
		return req, err
	}
	resp, err := DoHTTPWithRetry(context.Background(), nil, Options{}, newRequest, retryProviderError)
	if err != nil {
		t.Fatalf("DoHTTPWithRetry returned error: %v", err)
	}
	_ = resp.Body.Close()
	if got, ok := leaked.Load().(string); !ok || got != "" {
		t.Fatalf("redirect target received x-api-key = %q (reached %v), want it stripped", got, ok)
	}
}
