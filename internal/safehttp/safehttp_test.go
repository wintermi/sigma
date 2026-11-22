// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package safehttp

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// target records the headers of the last request it received.
func target(t *testing.T) (*httptest.Server, *atomic.Pointer[http.Header]) {
	t.Helper()
	var got atomic.Pointer[http.Header]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Clone()
		got.Store(&header)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, &got
}

func redirector(t *testing.T, newServer func(http.Handler) *httptest.Server, status int, location func() string) *httptest.Server {
	t.Helper()
	server := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			if r.Header.Get("x-api-key") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, location(), status)
	}))
	t.Cleanup(server.Close)
	return server
}

func credentialRequest(t *testing.T, method, url string) *http.Request {
	t.Helper()
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(`{"prompt":"secret"}`)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"x-api-key": "sk-ant", "x-goog-api-key": "goog", "api-key": "azure",
		"cf-aig-authorization": "cf", "Authorization": "Bearer token",
		"Accept": "application/json", "User-Agent": "sigma-test",
	} {
		req.Header.Set(name, value)
	}
	return req
}

func TestCrossOriginRedirectDropsCredentialHeaders(t *testing.T) {
	t.Parallel()
	other, got := target(t)
	origin := redirector(t, httptest.NewServer, http.StatusFound, func() string { return other.URL + "/elsewhere" })

	resp, err := Client(nil).Do(credentialRequest(t, http.MethodGet, origin.URL))
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	_ = resp.Body.Close()
	header := got.Load()
	if header == nil {
		t.Fatal("redirect target was not reached")
	}
	for _, name := range []string{"x-api-key", "x-goog-api-key", "api-key", "cf-aig-authorization", "Authorization"} {
		if value := header.Get(name); value != "" {
			t.Errorf("redirect target received %s = %q", name, value)
		}
	}
	if header.Get("Accept") != "application/json" || header.Get("User-Agent") != "sigma-test" {
		t.Errorf("redirect target headers = %v, want Accept and User-Agent kept", header)
	}
}

func TestSameOriginRedirectKeepsCredentialHeaders(t *testing.T) {
	t.Parallel()
	var origin *httptest.Server
	origin = redirector(t, httptest.NewServer, http.StatusTemporaryRedirect, func() string { return origin.URL + "/final" })

	resp, err := Client(nil).Do(credentialRequest(t, http.MethodPost, origin.URL))
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want same-origin redirect to keep x-api-key", resp.StatusCode)
	}
}

func TestUnsafeRedirectsAreRefused(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		newServer func(http.Handler) *httptest.Server
		status    int
		method    string
	}{
		{name: "cross-origin body resend", newServer: httptest.NewServer, status: http.StatusTemporaryRedirect, method: http.MethodPost},
		{name: "https to http downgrade", newServer: httptest.NewTLSServer, status: http.StatusFound, method: http.MethodGet},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			other, got := target(t)
			origin := redirector(t, tt.newServer, tt.status, func() string { return other.URL })
			base := origin.Client()

			_, err := Client(base).Do(credentialRequest(t, tt.method, origin.URL))
			if !errors.Is(err, ErrUnsafeRedirect) {
				t.Fatalf("Do error = %v, want ErrUnsafeRedirect", err)
			}
			if got.Load() != nil {
				t.Fatal("redirect target was reached")
			}
		})
	}
}

func TestClientKeepsCallerRedirectPolicy(t *testing.T) {
	t.Parallel()
	other, got := target(t)
	origin := redirector(t, httptest.NewServer, http.StatusFound, func() string { return other.URL })
	caller := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := Client(caller).Do(credentialRequest(t, http.MethodGet, origin.URL))
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || got.Load() != nil {
		t.Fatalf("status = %d, target reached = %v; want the caller's policy to stop at the redirect", resp.StatusCode, got.Load() != nil)
	}
	if caller.CheckRedirect == nil || http.DefaultClient.CheckRedirect != nil {
		t.Fatal("Client modified the caller's or the default client")
	}
}
