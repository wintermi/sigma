// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package radius

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func radiusCallbackGateway(t *testing.T, onToken func(url.Values)) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth":
			_, _ = io.WriteString(w, `{"authorizationEndpoint":"`+server.URL+`/authorize"}`)
		case "/v1/oauth/token":
			form, _ := io.ReadAll(r.Body)
			values, _ := url.ParseQuery(string(form))
			onToken(values)
			_, _ = io.WriteString(w, `{"access_token":"browser-access","refresh_token":"browser-refresh","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func sendRadiusCallbacks(callbackURL string, requests ...[2]string) {
	for _, request := range requests {
		req, err := http.NewRequestWithContext(context.Background(), request[0], callbackURL+request[1], nil)
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
func TestLoginRadiusBrowserIgnoresCallbacksWithoutMatchingState(t *testing.T) {
	gateway := radiusCallbackGateway(t, func(values url.Values) {
		if got := values.Get("code"); got != "browser-code" {
			t.Errorf("exchanged code = %q, want the matching callback code", got)
		}
	})
	callbackURL := radiusOAuthTestCallbackURL(t)
	client := radiusOAuthTestClientConfig(gateway.URL)
	client.RedirectURI = callbackURL
	_, err := LoginRadiusBrowser(context.Background(), RadiusBrowserLoginOptions{
		Client: client,
		OnAuth: func(info RadiusBrowserAuthInfo) {
			parsed, _ := url.Parse(info.URL)
			state := url.QueryEscape(parsed.Query().Get("state"))
			go sendRadiusCallbacks(callbackURL,
				[2]string{http.MethodGet, "?error=access_denied"},
				[2]string{http.MethodGet, "?code=attacker&state=wrong"},
				[2]string{http.MethodGet, "?state=" + state},
				[2]string{http.MethodPost, "?code=attacker&state=" + state},
				[2]string{http.MethodGet, "?code=browser-code&state=" + state},
			)
		},
	})
	if err != nil {
		t.Fatalf("LoginRadiusBrowser returned error: %v", err)
	}
}

func TestLoginRadiusBrowserFailsOnMatchingStateError(t *testing.T) {
	gateway := radiusCallbackGateway(t, func(url.Values) { t.Error("token endpoint should not be called") })
	callbackURL := radiusOAuthTestCallbackURL(t)
	client := radiusOAuthTestClientConfig(gateway.URL)
	client.RedirectURI = callbackURL
	_, err := LoginRadiusBrowser(context.Background(), RadiusBrowserLoginOptions{
		Client: client,
		OnAuth: func(info RadiusBrowserAuthInfo) {
			parsed, _ := url.Parse(info.URL)
			go sendRadiusCallbacks(callbackURL, [2]string{http.MethodGet, "?error=access_denied&state=" + url.QueryEscape(parsed.Query().Get("state"))})
		},
	})
	if err == nil || !strings.Contains(err.Error(), "authorization failed") {
		t.Fatalf("error = %v, want authorization failed", err)
	}
}
