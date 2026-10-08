// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Probe output is written to stdout, stderr, and handoff files, so provider
// error bodies that echo credentials must be redacted before they reach it.
func TestProbeHTTPErrorsRedactEchoedCredentials(t *testing.T) {
	t.Parallel()

	const key = "fw-secret-key-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid key `+key+`","api_key":"`+key+`"}`)
	}))
	t.Cleanup(server.Close)

	_, discoverErr := discoverModels(context.Background(), routeSpec{BaseURL: server.URL}, key)
	_, fireworksErr := fetchFireworksModelCapabilities(context.Background(), server.URL+"/inference/v1", "model", key)
	for name, err := range map[string]error{"model discovery": discoverErr, "fireworks metadata": fireworksErr} {
		if err == nil {
			t.Fatalf("%s succeeded, want HTTP error", name)
		}
		if strings.Contains(err.Error(), key) {
			t.Fatalf("%s error leaks the credential: %v", name, err)
		}
		if !strings.Contains(err.Error(), "401") {
			t.Fatalf("%s error lost the status: %v", name, err)
		}
	}
}
