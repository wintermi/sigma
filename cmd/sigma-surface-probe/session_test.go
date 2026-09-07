// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/wintermi/sigma"
)

func TestOpenCodeProbeSessionStability(t *testing.T) {
	t.Parallel()
	requests := make(chan string, 32)
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := r.Header.Get("x-opencode-session")
		requests <- session
		index := (count.Add(1) - 1) % 5
		if session == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"type":"MissingSessionID","message":"Request is missing x-opencode-session"}}`)
			return
		}
		switch index {
		case 0:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"output budget too small"}}`)
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	}))
	t.Cleanup(server.Close)
	route := routes["go"]
	route.BaseURL = server.URL
	route.Cases = func(r routeSpec, model sigma.Model) []probeCase {
		return openAICompatibleProbeCases(r, model)[:2]
	}
	seen := make(map[string]bool)
	for range 2 {
		results := collectProbeModel(context.Background(), route, "kimi-k3", routeCredential{apiKey: "test-key"}, config{repair: true})
		if len(results) != 2 || results[0].Outcome != "fixed_by_repair_variant" || results[1].Outcome != "ok" {
			t.Fatalf("probe results = %+v", results)
		}
		first := <-requests
		if first == "" || seen[first] {
			t.Fatalf("missing or reused conversation ID %q", first)
		}
		seen[first] = true
		for range 3 {
			if got := <-requests; got != first {
				t.Fatalf("retry/control/repair session = %q, want %q", got, first)
			}
		}
		second := <-requests
		if second == "" || seen[second] {
			t.Fatalf("second conversation reused session %q", second)
		}
		seen[second] = true
	}
}

func TestClassifyMissingSessionID(t *testing.T) {
	t.Parallel()
	providerErr := &sigma.ProviderError{StatusCode: http.StatusBadRequest, ProviderCode: "MissingSessionID"}
	for _, err := range []error{providerErr, errors.New(providerErr.Error())} {
		if got := classifyFailure(routes["go"], sigma.Model{ID: "kimi-k3"}, err); got != "sigma_request_shape" {
			t.Fatalf("classification = %q, want sigma_request_shape", got)
		}
	}
}
