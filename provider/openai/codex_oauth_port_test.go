// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"net"
	"net/http"
	"testing"
)

// Port 1455 is shared with the Codex CLI; when it is taken, a pasted redirect
// URL or code must still complete the login.
func TestLoginOpenAICodexBrowserUsesPastedCodeWhenPortIsTaken(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	old := codexOAuthBrowserListenAddr
	codexOAuthBrowserListenAddr = listener.Addr().String()
	t.Cleanup(func() { codexOAuthBrowserListenAddr = old })

	client := codexOAuthTestClient(t, func(r *http.Request) *http.Response {
		if got := readCodexOAuthFormBody(t, r).Get("redirect_uri"); got != codexOAuthBrowserDefaultRedirect {
			t.Fatalf("redirect_uri = %q, want %q", got, codexOAuthBrowserDefaultRedirect)
		}
		return codexOAuthJSONResponse(http.StatusOK, map[string]any{"access_token": codexTestJWT("acct_port"), "refresh_token": "refresh", "expires_in": 3600})
	})
	_, err = LoginOpenAICodexBrowser(context.Background(), CodexBrowserLoginOptions{
		HTTPClient: client,
		OnManualCode: func(context.Context, CodexBrowserManualPrompt) (string, error) {
			return "manual-code", nil
		},
	})
	if err != nil {
		t.Fatalf("LoginOpenAICodexBrowser returned error: %v", err)
	}

	if _, err := LoginOpenAICodexBrowser(context.Background(), CodexBrowserLoginOptions{}); err == nil {
		t.Fatal("login without a callback port or pasted code succeeded")
	}
}
