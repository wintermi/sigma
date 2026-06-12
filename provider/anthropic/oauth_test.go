// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func TestAnthropicAuthorizationURL(t *testing.T) {
	t.Parallel()

	authURL, err := anthropicAuthorizationURL("challenge-abc", "state-123", anthropicOAuthDefaultRedirect)
	if err != nil {
		t.Fatalf("anthropicAuthorizationURL returned error: %v", err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if got, want := parsed.Host, "claude.ai"; got != want {
		t.Fatalf("host = %q, want %q", got, want)
	}
	query := parsed.Query()
	expectations := map[string]string{
		"code":                  "true",
		"client_id":             anthropicOAuthClientID,
		"response_type":         "code",
		"redirect_uri":          anthropicOAuthDefaultRedirect,
		"scope":                 anthropicOAuthScope,
		"code_challenge":        "challenge-abc",
		"code_challenge_method": "S256",
		"state":                 "state-123",
	}
	for key, want := range expectations {
		if got := query.Get(key); got != want {
			t.Fatalf("query %q = %q, want %q", key, got, want)
		}
	}
}

func TestLoginAnthropicBrowserCallbackSuccess(t *testing.T) {
	withAnthropicBrowserTestServer(t)

	callbackHTML := make(chan string, 1)
	var redirectURI string
	client := anthropicOAuthTestClient(t, func(r *http.Request) *http.Response {
		if r.URL.String() != anthropicOAuthTokenURL {
			t.Fatalf("unexpected OAuth URL %q", r.URL.String())
		}
		body := decodeAnthropicOAuthJSONBody(t, r)
		if got, want := body["grant_type"], "authorization_code"; got != want {
			t.Fatalf("grant_type = %q, want %q", got, want)
		}
		if got, want := body["client_id"], anthropicOAuthClientID; got != want {
			t.Fatalf("client_id = %q, want %q", got, want)
		}
		if got, want := body["code"], "callback-code"; got != want {
			t.Fatalf("code = %q, want %q", got, want)
		}
		if got, want := body["redirect_uri"], redirectURI; got != want {
			t.Fatalf("redirect_uri = %q, want %q", got, want)
		}
		if body["state"] == "" || body["state"] != body["code_verifier"] {
			t.Fatalf("state = %q, want PKCE verifier %q", body["state"], body["code_verifier"])
		}
		return anthropicOAuthJSONResponse(http.StatusOK, map[string]any{
			"access_token":  "sk-ant-oat01-access",
			"refresh_token": "refresh-token",
			"expires_in":    3600,
		})
	})

	credentials, err := LoginAnthropicBrowser(context.Background(), AnthropicBrowserLoginOptions{
		HTTPClient: client,
		OnAuth: func(info AnthropicBrowserAuthInfo) {
			parsed, err := url.Parse(info.URL)
			if err != nil {
				callbackHTML <- err.Error()
				return
			}
			redirectURI = parsed.Query().Get("redirect_uri")
			state := parsed.Query().Get("state")
			go func() {
				resp, err := http.Get(redirectURI + "?code=callback-code&state=" + url.QueryEscape(state))
				if err != nil {
					callbackHTML <- err.Error()
					return
				}
				defer resp.Body.Close()
				data, _ := io.ReadAll(resp.Body)
				callbackHTML <- string(data)
			}()
		},
	})
	if err != nil {
		t.Fatalf("LoginAnthropicBrowser returned error: %v", err)
	}
	if got, want := credentials.AccessToken, "sk-ant-oat01-access"; got != want {
		t.Fatalf("access token = %q, want %q", got, want)
	}
	if got, want := credentials.RefreshToken, "refresh-token"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
	if credentials.Expiry.IsZero() {
		t.Fatal("expiry is zero, want future expiry")
	}
	if got := <-callbackHTML; !strings.Contains(got, "Authentication successful") {
		t.Fatalf("callback HTML = %q, want success page", got)
	}
}

func TestLoginAnthropicBrowserManualCodeFallback(t *testing.T) {
	withAnthropicBrowserTestServer(t)

	client := anthropicOAuthTestClient(t, func(r *http.Request) *http.Response {
		body := decodeAnthropicOAuthJSONBody(t, r)
		if got, want := body["code"], "manual-code"; got != want {
			t.Fatalf("code = %q, want %q", got, want)
		}
		return anthropicOAuthJSONResponse(http.StatusOK, map[string]any{
			"access_token":  "sk-ant-oat01-manual",
			"refresh_token": "refresh-token",
			"expires_in":    3600,
		})
	})

	credentials, err := LoginAnthropicBrowser(context.Background(), AnthropicBrowserLoginOptions{
		HTTPClient: client,
		OnManualCode: func(context.Context, AnthropicBrowserManualPrompt) (string, error) {
			return "manual-code", nil
		},
	})
	if err != nil {
		t.Fatalf("LoginAnthropicBrowser returned error: %v", err)
	}
	if got, want := credentials.AccessToken, "sk-ant-oat01-manual"; got != want {
		t.Fatalf("access token = %q, want %q", got, want)
	}
}

func TestLoginAnthropicBrowserManualCodeRejectsStateMismatch(t *testing.T) {
	withAnthropicBrowserTestServer(t)

	_, err := LoginAnthropicBrowser(context.Background(), AnthropicBrowserLoginOptions{
		HTTPClient: anthropicOAuthTestClient(t, func(*http.Request) *http.Response {
			t.Fatal("token endpoint should not be called")
			return nil
		}),
		OnManualCode: func(context.Context, AnthropicBrowserManualPrompt) (string, error) {
			return "manual-code#wrong-state", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("error = %v, want state mismatch", err)
	}
}

func TestLoginAnthropicBrowserCancellation(t *testing.T) {
	withAnthropicBrowserTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoginAnthropicBrowser(ctx, AnthropicBrowserLoginOptions{
		HTTPClient: anthropicOAuthTestClient(t, func(*http.Request) *http.Response {
			t.Fatal("token endpoint should not be called")
			return nil
		}),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestRefreshAnthropicTokenSuccess(t *testing.T) {
	t.Parallel()

	client := anthropicOAuthTestClient(t, func(r *http.Request) *http.Response {
		body := decodeAnthropicOAuthJSONBody(t, r)
		if got, want := body["grant_type"], "refresh_token"; got != want {
			t.Fatalf("grant_type = %q, want %q", got, want)
		}
		if got, want := body["refresh_token"], "refresh-token"; got != want {
			t.Fatalf("refresh_token = %q, want %q", got, want)
		}
		if got, want := body["client_id"], anthropicOAuthClientID; got != want {
			t.Fatalf("client_id = %q, want %q", got, want)
		}
		return anthropicOAuthJSONResponse(http.StatusOK, map[string]any{
			"access_token":  "sk-ant-oat01-refreshed",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		})
	})

	credentials, err := RefreshAnthropicToken(context.Background(), "refresh-token", AnthropicOAuthTokenProviderOptions{HTTPClient: client})
	if err != nil {
		t.Fatalf("RefreshAnthropicToken returned error: %v", err)
	}
	if got, want := credentials.AccessToken, "sk-ant-oat01-refreshed"; got != want {
		t.Fatalf("access token = %q, want %q", got, want)
	}
	if got, want := credentials.RefreshToken, "new-refresh-token"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
}

func TestRefreshAnthropicTokenFailureRedactsBody(t *testing.T) {
	t.Parallel()

	const refreshToken = "secret-refresh-token"
	client := anthropicOAuthTestClient(t, func(*http.Request) *http.Response {
		return anthropicOAuthJSONResponse(http.StatusUnauthorized, map[string]any{
			"error":         "invalid_grant",
			"refresh_token": refreshToken,
		})
	})

	_, err := RefreshAnthropicToken(context.Background(), refreshToken, AnthropicOAuthTokenProviderOptions{HTTPClient: client})
	if err == nil {
		t.Fatal("RefreshAnthropicToken returned nil error")
	}
	if strings.Contains(err.Error(), refreshToken) {
		t.Fatalf("error %q leaks refresh token", err.Error())
	}
}

func TestAnthropicOAuthTokenProviderRefreshesAndCallsBack(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	refreshed := make(chan AnthropicOAuthCredentials, 1)
	client := anthropicOAuthTestClient(t, func(*http.Request) *http.Response {
		return anthropicOAuthJSONResponse(http.StatusOK, map[string]any{
			"access_token":  "sk-ant-oat01-refreshed",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		})
	})

	provider := NewAnthropicOAuthTokenProvider(AnthropicOAuthCredentials{
		AccessToken:  "sk-ant-oat01-expired",
		RefreshToken: "refresh-token",
		Expiry:       now.Add(30 * time.Second),
	}, AnthropicOAuthTokenProviderOptions{
		HTTPClient: client,
		Now:        func() time.Time { return now },
		OnRefresh: func(_ context.Context, credentials AnthropicOAuthCredentials) error {
			refreshed <- credentials
			return nil
		},
	})

	model := sigma.Model{ID: "claude-test", Provider: sigma.ProviderAnthropic}
	credential, err := provider.Token(context.Background(), model, sigma.Options{})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if got, want := credential.Type, sigma.CredentialTypeOAuthToken; got != want {
		t.Fatalf("credential type = %q, want %q", got, want)
	}
	if got, want := credential.Value, "sk-ant-oat01-refreshed"; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	select {
	case credentials := <-refreshed:
		if got, want := credentials.RefreshToken, "new-refresh-token"; got != want {
			t.Fatalf("refreshed refresh token = %q, want %q", got, want)
		}
	default:
		t.Fatal("OnRefresh was not called")
	}
}

func TestAnthropicOAuthTokenProviderSkipsRefreshBeforeExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	provider := NewAnthropicOAuthTokenProvider(AnthropicOAuthCredentials{
		AccessToken:  "sk-ant-oat01-valid",
		RefreshToken: "refresh-token",
		Expiry:       now.Add(time.Hour),
	}, AnthropicOAuthTokenProviderOptions{
		HTTPClient: anthropicOAuthTestClient(t, func(*http.Request) *http.Response {
			t.Fatal("token endpoint should not be called")
			return nil
		}),
		Now: func() time.Time { return now },
	})

	model := sigma.Model{ID: "claude-test", Provider: sigma.ProviderAnthropic}
	credential, err := provider.Token(context.Background(), model, sigma.Options{})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if got, want := credential.Value, "sk-ant-oat01-valid"; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
}

func TestAnthropicOAuthTokenProviderMissingCredentials(t *testing.T) {
	t.Parallel()

	provider := NewAnthropicOAuthTokenProvider(AnthropicOAuthCredentials{}, AnthropicOAuthTokenProviderOptions{})
	model := sigma.Model{ID: "claude-test", Provider: sigma.ProviderAnthropic}
	_, err := provider.Token(context.Background(), model, sigma.Options{})
	var unavailable *sigma.CredentialUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %v, want CredentialUnavailableError", err)
	}
}

func TestParseAnthropicAuthorizationInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		code  string
		state string
	}{
		{name: "empty", input: "  "},
		{name: "bare code", input: "raw-code", code: "raw-code"},
		{name: "code hash state", input: "the-code#the-state", code: "the-code", state: "the-state"},
		{
			name:  "redirect URL",
			input: "http://localhost:53692/callback?code=url-code&state=url-state",
			code:  "url-code",
			state: "url-state",
		},
		{name: "query string", input: "?code=query-code&state=query-state", code: "query-code", state: "query-state"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsed := parseAnthropicAuthorizationInput(tt.input)
			if parsed.code != tt.code || parsed.state != tt.state {
				t.Fatalf("parsed = %+v, want code %q state %q", parsed, tt.code, tt.state)
			}
		})
	}
}

func TestIsAnthropicOAuthCredential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		credential sigma.Credential
		want       bool
	}{
		{name: "empty", credential: sigma.Credential{}, want: false},
		{name: "api key", credential: sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "sk-ant-api03-key"}, want: false},
		{name: "oauth typed", credential: sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "token"}, want: true},
		{name: "oat token as api key", credential: sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "sk-ant-oat01-token"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isAnthropicOAuthCredential(tt.credential); got != tt.want {
				t.Fatalf("isAnthropicOAuthCredential = %v, want %v", got, tt.want)
			}
		})
	}
}

func withAnthropicBrowserTestServer(t *testing.T) {
	t.Helper()
	old := anthropicOAuthListenAddr
	anthropicOAuthListenAddr = "127.0.0.1:0"
	t.Cleanup(func() { anthropicOAuthListenAddr = old })
}

func anthropicOAuthTestClient(t *testing.T, handler func(*http.Request) *http.Response) *http.Client {
	t.Helper()
	return &http.Client{Transport: anthropicOAuthRoundTripper(handler)}
}

type anthropicOAuthRoundTripper func(*http.Request) *http.Response

func (rt anthropicOAuthRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	resp := rt(r)
	if resp == nil {
		return nil, errors.New("missing test response")
	}
	return resp, nil
}

func anthropicOAuthJSONResponse(status int, body any) *http.Response {
	data, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}
}

func decodeAnthropicOAuthJSONBody(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read OAuth request body: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode OAuth request body: %v", err)
	}
	return decoded
}
