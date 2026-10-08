// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// sendAnthropicCallbacks sends requests to the callback in order.
func sendAnthropicCallbacks(redirectURI string, requests ...[2]string) {
	for _, request := range requests {
		req, err := http.NewRequestWithContext(context.Background(), request[0], redirectURI+request[1], nil)
		if err != nil {
			continue
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}
}

// Requests without the login's state, such as a stale tab or a cross-site
// page, must not end the login; only the matching callback may.
func TestLoginAnthropicBrowserIgnoresCallbacksWithoutMatchingState(t *testing.T) {
	withAnthropicBrowserTestServer(t)

	client := anthropicOAuthTestClient(t, func(r *http.Request) *http.Response {
		if got := decodeAnthropicOAuthJSONBody(t, r)["code"]; got != "callback-code" {
			t.Fatalf("exchanged code = %q, want the matching callback code", got)
		}
		return anthropicOAuthJSONResponse(http.StatusOK, map[string]any{"access_token": "sk-ant-oat01-access", "refresh_token": "refresh", "expires_in": 3600})
	})
	_, err := LoginAnthropicBrowser(context.Background(), AnthropicBrowserLoginOptions{
		HTTPClient: client,
		OnAuth: func(info AnthropicBrowserAuthInfo) {
			parsed, _ := url.Parse(info.URL)
			redirectURI, state := parsed.Query().Get("redirect_uri"), url.QueryEscape(parsed.Query().Get("state"))
			go sendAnthropicCallbacks(redirectURI,
				[2]string{http.MethodGet, "?error=access_denied"},
				[2]string{http.MethodGet, "?code=attacker&state=wrong"},
				[2]string{http.MethodGet, "?state=" + state},
				[2]string{http.MethodPost, "?code=attacker&state=" + state},
				[2]string{http.MethodGet, "?code=callback-code&state=" + state},
			)
		},
	})
	if err != nil {
		t.Fatalf("LoginAnthropicBrowser returned error: %v", err)
	}
}

func TestLoginAnthropicBrowserFailsOnMatchingStateError(t *testing.T) {
	withAnthropicBrowserTestServer(t)

	_, err := LoginAnthropicBrowser(context.Background(), AnthropicBrowserLoginOptions{
		HTTPClient: anthropicOAuthTestClient(t, func(*http.Request) *http.Response {
			t.Fatal("token endpoint should not be called")
			return nil
		}),
		OnAuth: func(info AnthropicBrowserAuthInfo) {
			parsed, _ := url.Parse(info.URL)
			go sendAnthropicCallbacks(parsed.Query().Get("redirect_uri"),
				[2]string{http.MethodGet, "?error=access_denied&state=" + url.QueryEscape(parsed.Query().Get("state"))})
		},
	})
	if err == nil || !strings.Contains(err.Error(), "authorization failed") {
		t.Fatalf("error = %v, want authorization failed", err)
	}
}
