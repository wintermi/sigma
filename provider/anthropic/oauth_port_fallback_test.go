// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// occupyLoopbackPort holds a loopback port so a login cannot bind it.
func occupyLoopbackPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String()
}

func anthropicPortFallbackClient(t *testing.T, redirectURI *string) *http.Client {
	t.Helper()
	return anthropicOAuthTestClient(t, func(r *http.Request) *http.Response {
		if got := decodeAnthropicOAuthJSONBody(t, r)["redirect_uri"]; got != *redirectURI {
			t.Fatalf("redirect_uri = %q, want %q", got, *redirectURI)
		}
		return anthropicOAuthJSONResponse(http.StatusOK, map[string]any{"access_token": "sk-ant-oat01-access", "refresh_token": "refresh", "expires_in": 3600})
	})
}

func TestLoginAnthropicBrowserFallsBackToFreePortWhenDefaultIsTaken(t *testing.T) {
	old := anthropicOAuthListenAddr
	anthropicOAuthListenAddr = occupyLoopbackPort(t)
	t.Cleanup(func() { anthropicOAuthListenAddr = old })

	var redirectURI string
	_, err := LoginAnthropicBrowser(context.Background(), AnthropicBrowserLoginOptions{
		HTTPClient: anthropicPortFallbackClient(t, &redirectURI),
		OnAuth: func(info AnthropicBrowserAuthInfo) {
			parsed, _ := url.Parse(info.URL)
			redirectURI = parsed.Query().Get("redirect_uri")
			go func() {
				resp, err := http.Get(redirectURI + "?code=callback-code&state=" + url.QueryEscape(parsed.Query().Get("state")))
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
		},
	})
	if err != nil {
		t.Fatalf("LoginAnthropicBrowser returned error: %v", err)
	}
	if !strings.HasPrefix(redirectURI, "http://localhost:") || strings.Contains(redirectURI, anthropicOAuthListenAddr) {
		t.Fatalf("redirect URI = %q, want a fallback localhost port", redirectURI)
	}
}

func TestLoginAnthropicBrowserUsesPastedCodeWhenNoPortIsAvailable(t *testing.T) {
	oldAddr, oldFallback := anthropicOAuthListenAddr, anthropicOAuthFallbackListenAddr
	anthropicOAuthListenAddr = occupyLoopbackPort(t)
	anthropicOAuthFallbackListenAddr = anthropicOAuthListenAddr
	t.Cleanup(func() { anthropicOAuthListenAddr, anthropicOAuthFallbackListenAddr = oldAddr, oldFallback })

	redirectURI := anthropicOAuthDefaultRedirect
	_, err := LoginAnthropicBrowser(context.Background(), AnthropicBrowserLoginOptions{
		HTTPClient: anthropicPortFallbackClient(t, &redirectURI),
		OnManualCode: func(context.Context, AnthropicBrowserManualPrompt) (string, error) {
			return "manual-code", nil
		},
	})
	if err != nil {
		t.Fatalf("LoginAnthropicBrowser returned error: %v", err)
	}

	if _, err := LoginAnthropicBrowser(context.Background(), AnthropicBrowserLoginOptions{}); err == nil {
		t.Fatal("login without a callback port or pasted code succeeded")
	}
}
