// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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

func TestOpenAICodexBrowserAuthorizationURL(t *testing.T) {
	flow, err := newOpenAICodexBrowserAuthorizationFlow("state-123", codexOAuthBrowserDefaultRedirect)
	if err != nil {
		t.Fatalf("newOpenAICodexBrowserAuthorizationFlow returned error: %v", err)
	}
	parsed, err := url.Parse(flow.url)
	if err != nil {
		t.Fatalf("Parse authorization URL returned error: %v", err)
	}
	values := parsed.Query()
	if got, want := parsed.Scheme+"://"+parsed.Host+parsed.Path, codexOAuthAuthorizeURL; got != want {
		t.Fatalf("authorize URL = %q, want %q", got, want)
	}
	assertQueryValue(t, values, "response_type", "code")
	assertQueryValue(t, values, "client_id", codexOAuthClientID)
	assertQueryValue(t, values, "redirect_uri", codexOAuthBrowserDefaultRedirect)
	assertQueryValue(t, values, "scope", codexOAuthBrowserScope)
	assertQueryValue(t, values, "code_challenge_method", "S256")
	assertQueryValue(t, values, "state", "state-123")
	assertQueryValue(t, values, "id_token_add_organizations", "true")
	assertQueryValue(t, values, "codex_cli_simplified_flow", "true")
	assertQueryValue(t, values, "originator", "sigma")

	challenge := sha256.Sum256([]byte(flow.verifier))
	if got, want := values.Get("code_challenge"), base64.RawURLEncoding.EncodeToString(challenge[:]); got != want {
		t.Fatalf("code_challenge = %q, want %q", got, want)
	}
}

func TestCodexProviderAuthResolvesStoredOAuthAccountMetadata(t *testing.T) {
	t.Parallel()

	store := sigma.NewInMemoryCredentialStore()
	access := codexTestJWT("acct_store")
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderOpenAICodex, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Type:         sigma.CredentialTypeOAuthToken,
			Value:        access,
			RefreshToken: "refresh-token",
			Expiry:       time.Now().Add(time.Hour),
		}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}
	registry := sigma.NewRegistry()
	if err := RegisterCodexProviderAuth(registry, sigma.ProviderOpenAICodex, CodexOAuthTokenProviderOptions{}); err != nil {
		t.Fatalf("RegisterCodexProviderAuth returned error: %v", err)
	}
	credential, err := (sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}).Resolve(
		context.Background(),
		sigma.Model{Provider: sigma.ProviderOpenAICodex, ID: "gpt-test"},
		sigma.Options{},
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := credential.Metadata[codexOAuthCredentialAccountID], "acct_store"; got != want {
		t.Fatalf("account metadata = %v, want %q", got, want)
	}
}

func TestStoreCodexOAuthCredentialsWritesStoreCredential(t *testing.T) {
	t.Parallel()

	store := sigma.NewInMemoryCredentialStore()
	expiry := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	access := codexTestJWT("acct_saved")

	stored, err := StoreCodexOAuthCredentials(context.Background(), store, sigma.ProviderOpenAICodex, CodexOAuthCredentials{
		AccessToken:  access,
		RefreshToken: "refresh-token",
		Expiry:       expiry,
		AccountID:    "acct_saved",
	})
	if err != nil {
		t.Fatalf("StoreCodexOAuthCredentials returned error: %v", err)
	}
	assertStoredCodexCredential(t, stored, access, "refresh-token", expiry, "acct_saved")

	read, ok, err := store.ReadCredential(context.Background(), sigma.ProviderOpenAICodex)
	if err != nil {
		t.Fatalf("ReadCredential returned error: %v", err)
	}
	if !ok {
		t.Fatal("ReadCredential ok = false, want true")
	}
	assertStoredCodexCredential(t, read, access, "refresh-token", expiry, "acct_saved")
}

func TestStoreCodexOAuthCredentialsPreservesStoredConfig(t *testing.T) {
	t.Parallel()

	store := sigma.NewInMemoryCredentialStore()
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderOpenAICodex, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Type:         sigma.CredentialTypeOAuthToken,
			Value:        "old-token",
			RefreshToken: "old-refresh",
			Expiry:       time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC),
			Source:       "custom-source",
			ProviderEnv:  map[string]string{"ROUTE": "codex"},
			Metadata: map[string]any{
				"keep":                        "value",
				codexOAuthCredentialAccountID: "old-account",
			},
		}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}

	expiry := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	access := codexTestJWT("acct_new")
	stored, err := StoreCodexOAuthCredentials(context.Background(), store, sigma.ProviderOpenAICodex, CodexOAuthCredentials{
		AccessToken:  access,
		RefreshToken: "new-refresh",
		Expiry:       expiry,
		AccountID:    "acct_new",
	})
	if err != nil {
		t.Fatalf("StoreCodexOAuthCredentials returned error: %v", err)
	}
	assertStoredCodexCredential(t, stored, access, "new-refresh", expiry, "acct_new")
	if got, want := stored.Source, "custom-source"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
	if got, want := stored.ProviderEnv["ROUTE"], "codex"; got != want {
		t.Fatalf("provider env ROUTE = %q, want %q", got, want)
	}
	if got, want := stored.Metadata["keep"], "value"; got != want {
		t.Fatalf("metadata keep = %v, want %q", got, want)
	}
}

func TestStoreCodexOAuthCredentialsRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	_, err := StoreCodexOAuthCredentials(context.Background(), nil, sigma.ProviderOpenAICodex, CodexOAuthCredentials{})
	assertSigmaErrorCode(t, err, sigma.ErrorInvalidOptions)

	store := &recordingCredentialStore{}
	_, err = StoreCodexOAuthCredentials(context.Background(), store, "", CodexOAuthCredentials{})
	assertSigmaErrorCode(t, err, sigma.ErrorInvalidOptions)
	if store.modifyCalled {
		t.Fatal("ModifyCredential called for empty provider")
	}
}

func TestStoreCodexOAuthCredentialsResolvesStoredProviderAuth(t *testing.T) {
	t.Parallel()

	store := sigma.NewInMemoryCredentialStore()
	access := codexTestJWT("acct_resolved")
	_, err := StoreCodexOAuthCredentials(context.Background(), store, sigma.ProviderOpenAICodex, CodexOAuthCredentials{
		AccessToken:  access,
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour),
		AccountID:    "acct_resolved",
	})
	if err != nil {
		t.Fatalf("StoreCodexOAuthCredentials returned error: %v", err)
	}
	registry := sigma.NewRegistry()
	if err := RegisterCodexProviderAuth(registry, sigma.ProviderOpenAICodex, CodexOAuthTokenProviderOptions{}); err != nil {
		t.Fatalf("RegisterCodexProviderAuth returned error: %v", err)
	}

	credential, err := (sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}).Resolve(
		context.Background(),
		sigma.Model{Provider: sigma.ProviderOpenAICodex, ID: "gpt-test"},
		sigma.Options{},
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := credential.Value, access; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	if got, want := credential.Metadata[codexOAuthCredentialAccountID], "acct_resolved"; got != want {
		t.Fatalf("credential account metadata = %v, want %q", got, want)
	}
}

func TestLoginOpenAICodexBrowserCallbackSuccess(t *testing.T) {
	withCodexBrowserTestServer(t)

	accessToken := codexTestJWT("acct_browser")
	callbackHTML := make(chan string, 1)
	var redirectURI string
	client := codexOAuthTestClient(t, func(r *http.Request) *http.Response {
		if r.URL.String() != codexOAuthTokenURL {
			t.Fatalf("unexpected OAuth URL %q", r.URL.String())
		}
		assertCodexOAuthFormBody(t, r, map[string]string{
			"grant_type":   "authorization_code",
			"client_id":    codexOAuthClientID,
			"code":         "callback-code",
			"redirect_uri": redirectURI,
		})
		return codexOAuthJSONResponse(http.StatusOK, map[string]any{
			"access_token":  accessToken,
			"refresh_token": "refresh-token",
			"expires_in":    3600,
		})
	})

	credentials, err := LoginOpenAICodexBrowser(context.Background(), CodexBrowserLoginOptions{
		HTTPClient: client,
		OnAuth: func(info CodexBrowserAuthInfo) {
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
		t.Fatalf("LoginOpenAICodexBrowser returned error: %v", err)
	}
	if got, want := credentials.AccessToken, accessToken; got != want {
		t.Fatalf("access token = %q, want %q", got, want)
	}
	if got, want := credentials.RefreshToken, "refresh-token"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
	if got, want := credentials.AccountID, "acct_browser"; got != want {
		t.Fatalf("account id = %q, want %q", got, want)
	}
	if got := <-callbackHTML; !strings.Contains(got, "Authentication successful") {
		t.Fatalf("callback HTML = %q, want success page", got)
	}
}

func TestLoginOpenAICodexBrowserCallbackErrorsBeforeExchange(t *testing.T) {
	tests := []struct {
		name  string
		query func(url.Values) string
		want  string
	}{
		{
			name: "missing code",
			query: func(values url.Values) string {
				return "?state=" + url.QueryEscape(values.Get("state"))
			},
			want: "missing authorization code",
		},
		{
			name: "state mismatch",
			query: func(url.Values) string {
				return "?code=callback-code&state=wrong-state"
			},
			want: "state mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withCodexBrowserTestServer(t)

			var tokenCalls int
			client := codexOAuthTestClient(t, func(*http.Request) *http.Response {
				tokenCalls++
				return codexOAuthJSONResponse(http.StatusOK, map[string]any{})
			})

			_, err := LoginOpenAICodexBrowser(context.Background(), CodexBrowserLoginOptions{
				HTTPClient: client,
				OnAuth: func(info CodexBrowserAuthInfo) {
					parsed, err := url.Parse(info.URL)
					if err != nil {
						t.Errorf("Parse auth URL returned error: %v", err)
						return
					}
					redirectURI := parsed.Query().Get("redirect_uri")
					go func() {
						resp, err := http.Get(redirectURI + tt.query(parsed.Query()))
						if err == nil {
							_ = resp.Body.Close()
						}
					}()
				},
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if tokenCalls != 0 {
				t.Fatalf("token calls = %d, want 0", tokenCalls)
			}
		})
	}
}

func TestLoginOpenAICodexBrowserManualCodeFallback(t *testing.T) {
	withCodexBrowserTestServer(t)

	accessToken := codexTestJWT("acct_manual")
	var authInfo CodexBrowserAuthInfo
	var redirectURI string
	client := codexOAuthTestClient(t, func(r *http.Request) *http.Response {
		values := readCodexOAuthFormBody(t, r)
		for key, want := range map[string]string{
			"grant_type":   "authorization_code",
			"client_id":    codexOAuthClientID,
			"code":         "manual-code",
			"redirect_uri": redirectURI,
		} {
			if got := values.Get(key); got != want {
				t.Fatalf("request form[%q] = %q, want %q", key, got, want)
			}
		}
		if values.Get("code_verifier") == "" {
			t.Fatal("code_verifier was empty")
		}
		return codexOAuthJSONResponse(http.StatusOK, map[string]any{
			"access_token":  accessToken,
			"refresh_token": "refresh-token",
			"expires_in":    3600,
		})
	})

	credentials, err := LoginOpenAICodexBrowser(context.Background(), CodexBrowserLoginOptions{
		HTTPClient: client,
		OnAuth: func(info CodexBrowserAuthInfo) {
			authInfo = info
		},
		OnManualCode: func(_ context.Context, prompt CodexBrowserManualPrompt) (string, error) {
			if !strings.Contains(prompt.Message, "authorization code") {
				return "", errors.New("manual prompt did not mention authorization code")
			}
			parsed, err := url.Parse(authInfo.URL)
			if err != nil {
				return "", err
			}
			redirectURI = parsed.Query().Get("redirect_uri")
			return redirectURI + "?code=manual-code&state=" + url.QueryEscape(parsed.Query().Get("state")), nil
		},
	})
	if err != nil {
		t.Fatalf("LoginOpenAICodexBrowser returned error: %v", err)
	}
	if got, want := credentials.AccountID, "acct_manual"; got != want {
		t.Fatalf("account id = %q, want %q", got, want)
	}
}

func TestLoginOpenAICodexBrowserManualCodeRejectsStateMismatch(t *testing.T) {
	withCodexBrowserTestServer(t)

	_, err := LoginOpenAICodexBrowser(context.Background(), CodexBrowserLoginOptions{
		OnManualCode: func(context.Context, CodexBrowserManualPrompt) (string, error) {
			return "manual-code#wrong-state", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("error = %v, want state mismatch", err)
	}
}

func TestLoginOpenAICodexBrowserCancellation(t *testing.T) {
	withCodexBrowserTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	_, err := LoginOpenAICodexBrowser(ctx, CodexBrowserLoginOptions{
		OnAuth: func(CodexBrowserAuthInfo) {
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestLoginOpenAICodexBrowserExchangeFailureRedactsBody(t *testing.T) {
	withCodexBrowserTestServer(t)

	const token = "secret-access-token"
	client := codexOAuthTestClient(t, func(*http.Request) *http.Response {
		return codexOAuthJSONResponse(http.StatusUnauthorized, map[string]any{
			"error":        "invalid_grant",
			"access_token": token,
		})
	})

	_, err := LoginOpenAICodexBrowser(context.Background(), CodexBrowserLoginOptions{
		HTTPClient: client,
		OnManualCode: func(context.Context, CodexBrowserManualPrompt) (string, error) {
			return "manual-code", nil
		},
	})
	if err == nil {
		t.Fatal("LoginOpenAICodexBrowser returned nil error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaked token: %v", err)
	}
}

func TestLoginOpenAICodexDeviceCodeSuccess(t *testing.T) {
	t.Parallel()

	accessToken := codexTestJWT("acct_login")
	var deviceInfo CodexDeviceCodeInfo
	client := codexOAuthTestClient(t, func(r *http.Request) *http.Response {
		switch r.URL.String() {
		case codexOAuthDeviceUserCodeURL:
			assertCodexOAuthJSONBody(t, r, map[string]any{"client_id": codexOAuthClientID})
			return codexOAuthJSONResponse(http.StatusOK, map[string]any{
				"device_auth_id": "device-auth-id",
				"user_code":      "ABCD-1234",
				"interval":       "5",
			})
		case codexOAuthDeviceTokenURL:
			assertCodexOAuthJSONBody(t, r, map[string]any{
				"device_auth_id": "device-auth-id",
				"user_code":      "ABCD-1234",
			})
			return codexOAuthJSONResponse(http.StatusOK, map[string]any{
				"authorization_code": "oauth-code",
				"code_verifier":      "device-code-verifier",
			})
		case codexOAuthTokenURL:
			assertCodexOAuthFormBody(t, r, map[string]string{
				"grant_type":    "authorization_code",
				"client_id":     codexOAuthClientID,
				"code":          "oauth-code",
				"code_verifier": "device-code-verifier",
				"redirect_uri":  codexOAuthDeviceRedirectURI,
			})
			return codexOAuthJSONResponse(http.StatusOK, map[string]any{
				"access_token":  accessToken,
				"refresh_token": "refresh-token",
				"expires_in":    3600,
			})
		default:
			t.Fatalf("unexpected OAuth URL %q", r.URL.String())
			return nil
		}
	})

	credentials, err := LoginOpenAICodexDeviceCode(context.Background(), CodexDeviceCodeLoginOptions{
		HTTPClient:   client,
		OnDeviceCode: func(info CodexDeviceCodeInfo) { deviceInfo = info },
	})
	if err != nil {
		t.Fatalf("LoginOpenAICodexDeviceCode returned error: %v", err)
	}
	if got, want := credentials.AccessToken, accessToken; got != want {
		t.Fatalf("access token = %q, want %q", got, want)
	}
	if got, want := credentials.RefreshToken, "refresh-token"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
	if got, want := credentials.AccountID, "acct_login"; got != want {
		t.Fatalf("account id = %q, want %q", got, want)
	}
	if credentials.Expiry.IsZero() {
		t.Fatal("expiry was zero")
	}
	if got, want := deviceInfo.UserCode, "ABCD-1234"; got != want {
		t.Fatalf("device user code = %q, want %q", got, want)
	}
	if got, want := deviceInfo.VerificationURI, codexOAuthDeviceVerificationURI; got != want {
		t.Fatalf("device verification uri = %q, want %q", got, want)
	}
	if got, want := deviceInfo.Interval, 5*time.Second; got != want {
		t.Fatalf("device interval = %s, want %s", got, want)
	}
}

func TestOpenAICodexDevicePollPendingAndSlowDown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   any
		want   string
	}{
		{
			name:   "pending code",
			status: http.StatusForbidden,
			body: map[string]any{
				"error": map[string]any{"code": "deviceauth_authorization_pending"},
			},
			want: "pending",
		},
		{
			name:   "not found pending",
			status: http.StatusNotFound,
			body:   "not ready",
			want:   "pending",
		},
		{
			name:   "slow down",
			status: http.StatusBadRequest,
			body:   map[string]any{"error": "slow_down"},
			want:   "slow_down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := codexOAuthTestClient(t, func(*http.Request) *http.Response {
				return codexOAuthJSONResponse(tt.status, tt.body)
			})

			result, err := pollOpenAICodexDeviceAuthOnce(context.Background(), client, codexDeviceAuthInfo{
				deviceAuthID: "device-auth-id",
				userCode:     "ABCD-1234",
			})
			if err != nil {
				t.Fatalf("pollOpenAICodexDeviceAuthOnce returned error: %v", err)
			}
			if got := result.status; got != tt.want {
				t.Fatalf("poll status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpenAICodexDevicePollCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := codexOAuthTestClient(t, func(r *http.Request) *http.Response {
		return codexOAuthErrorResponse(r.Context().Err())
	})

	_, err := pollOpenAICodexDeviceAuthOnce(ctx, client, codexDeviceAuthInfo{
		deviceAuthID: "device-auth-id",
		userCode:     "ABCD-1234",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestOpenAICodexDevicePollTimeout(t *testing.T) {
	oldTimeout := codexOAuthDeviceTimeout
	codexOAuthDeviceTimeout = time.Millisecond
	t.Cleanup(func() { codexOAuthDeviceTimeout = oldTimeout })

	client := codexOAuthTestClient(t, func(*http.Request) *http.Response {
		return codexOAuthJSONResponse(http.StatusForbidden, map[string]any{
			"error": map[string]any{"code": "deviceauth_authorization_pending"},
		})
	})

	_, err := pollOpenAICodexDeviceAuth(context.Background(), client, codexDeviceAuthInfo{
		deviceAuthID: "device-auth-id",
		userCode:     "ABCD-1234",
		interval:     time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestLoginOpenAICodexDeviceCodeInvalidResponse(t *testing.T) {
	t.Parallel()

	client := codexOAuthTestClient(t, func(*http.Request) *http.Response {
		return codexOAuthJSONResponse(http.StatusOK, map[string]any{"user_code": "ABCD-1234"})
	})

	_, err := LoginOpenAICodexDeviceCode(context.Background(), CodexDeviceCodeLoginOptions{HTTPClient: client})
	if err == nil || !strings.Contains(err.Error(), "missing fields") {
		t.Fatalf("error = %v, want missing fields", err)
	}
}

func TestLoginOpenAICodexDeviceCodeExchangeFailureRedactsBody(t *testing.T) {
	t.Parallel()

	const token = "secret-access-token"
	client := codexOAuthTestClient(t, func(r *http.Request) *http.Response {
		switch r.URL.String() {
		case codexOAuthDeviceUserCodeURL:
			return codexOAuthJSONResponse(http.StatusOK, map[string]any{
				"device_auth_id": "device-auth-id",
				"user_code":      "ABCD-1234",
				"interval":       0,
			})
		case codexOAuthDeviceTokenURL:
			return codexOAuthJSONResponse(http.StatusOK, map[string]any{
				"authorization_code": "oauth-code",
				"code_verifier":      "device-code-verifier",
			})
		case codexOAuthTokenURL:
			return codexOAuthJSONResponse(http.StatusUnauthorized, map[string]any{
				"error":        "invalid_grant",
				"access_token": token,
			})
		default:
			t.Fatalf("unexpected OAuth URL %q", r.URL.String())
			return nil
		}
	})

	_, err := LoginOpenAICodexDeviceCode(context.Background(), CodexDeviceCodeLoginOptions{HTTPClient: client})
	if err == nil {
		t.Fatal("LoginOpenAICodexDeviceCode returned nil error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaked token: %v", err)
	}
}

func TestRefreshOpenAICodexTokenSuccess(t *testing.T) {
	t.Parallel()

	accessToken := codexTestJWT("acct_refresh")
	client := codexOAuthTestClient(t, func(r *http.Request) *http.Response {
		assertCodexOAuthFormBody(t, r, map[string]string{
			"grant_type":    "refresh_token",
			"refresh_token": "refresh-token",
			"client_id":     codexOAuthClientID,
		})
		return codexOAuthJSONResponse(http.StatusOK, map[string]any{
			"access_token":  accessToken,
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		})
	})

	credentials, err := RefreshOpenAICodexToken(context.Background(), "refresh-token", CodexOAuthTokenProviderOptions{HTTPClient: client})
	if err != nil {
		t.Fatalf("RefreshOpenAICodexToken returned error: %v", err)
	}
	if got, want := credentials.AccessToken, accessToken; got != want {
		t.Fatalf("access token = %q, want %q", got, want)
	}
	if got, want := credentials.RefreshToken, "new-refresh-token"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
	if got, want := credentials.AccountID, "acct_refresh"; got != want {
		t.Fatalf("account id = %q, want %q", got, want)
	}
}

func TestRefreshOpenAICodexTokenFailureRedactsBody(t *testing.T) {
	t.Parallel()

	const refreshToken = "secret-refresh-token"
	client := codexOAuthTestClient(t, func(*http.Request) *http.Response {
		return codexOAuthJSONResponse(http.StatusUnauthorized, map[string]any{
			"error":         "invalid_grant",
			"refresh_token": refreshToken,
		})
	})

	_, err := RefreshOpenAICodexToken(context.Background(), refreshToken, CodexOAuthTokenProviderOptions{HTTPClient: client})
	if err == nil {
		t.Fatal("RefreshOpenAICodexToken returned nil error")
	}
	if strings.Contains(err.Error(), refreshToken) {
		t.Fatalf("error leaked token: %v", err)
	}
}

func TestCodexAccountIDFromToken(t *testing.T) {
	t.Parallel()

	accountID, err := codexAccountIDFromToken(codexTestJWT("acct_jwt"))
	if err != nil {
		t.Fatalf("codexAccountIDFromToken returned error: %v", err)
	}
	if got, want := accountID, "acct_jwt"; got != want {
		t.Fatalf("account id = %q, want %q", got, want)
	}

	_, err = codexAccountIDFromToken("not-a-jwt")
	if err == nil {
		t.Fatal("codexAccountIDFromToken returned nil error for invalid token")
	}
}

func TestCodexOAuthTokenProviderRefreshesAndCallsBack(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	accessToken := codexTestJWT("acct_refreshed")
	client := codexOAuthTestClient(t, func(*http.Request) *http.Response {
		return codexOAuthJSONResponse(http.StatusOK, map[string]any{
			"access_token":  accessToken,
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		})
	})
	var refreshed CodexOAuthCredentials
	provider := NewCodexOAuthTokenProvider(CodexOAuthCredentials{
		AccessToken:  codexTestJWT("acct_old"),
		RefreshToken: "old-refresh-token",
		Expiry:       now.Add(30 * time.Second),
		AccountID:    "acct_old",
	}, CodexOAuthTokenProviderOptions{
		HTTPClient:    client,
		Now:           func() time.Time { return now },
		RefreshBefore: time.Minute,
		OnRefresh: func(_ context.Context, credentials CodexOAuthCredentials) error {
			refreshed = credentials
			return nil
		},
	})

	credential, err := provider.Token(context.Background(), sigma.Model{ID: "codex", Provider: sigma.ProviderOpenAI}, sigma.Options{})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if got, want := credential.Value, accessToken; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	if got, want := credential.Metadata[codexOAuthCredentialAccountID], "acct_refreshed"; got != want {
		t.Fatalf("credential account metadata = %v, want %q", got, want)
	}
	if got, want := refreshed.RefreshToken, "new-refresh-token"; got != want {
		t.Fatalf("refreshed token = %q, want %q", got, want)
	}
}

func TestCodexOAuthTokenProviderCallbackErrorDoesNotLeakTokens(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	newToken := codexTestJWT("acct_refreshed")
	client := codexOAuthTestClient(t, func(*http.Request) *http.Response {
		return codexOAuthJSONResponse(http.StatusOK, map[string]any{
			"access_token":  newToken,
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		})
	})
	provider := NewCodexOAuthTokenProvider(CodexOAuthCredentials{
		AccessToken:  codexTestJWT("acct_old"),
		RefreshToken: "old-refresh-token",
		Expiry:       now.Add(30 * time.Second),
		AccountID:    "acct_old",
	}, CodexOAuthTokenProviderOptions{
		HTTPClient:    client,
		Now:           func() time.Time { return now },
		RefreshBefore: time.Minute,
		OnRefresh: func(context.Context, CodexOAuthCredentials) error {
			return errors.New("failed to persist " + newToken)
		},
	})

	_, err := provider.Token(context.Background(), sigma.Model{ID: "codex", Provider: sigma.ProviderOpenAI}, sigma.Options{})
	if err == nil {
		t.Fatal("Token returned nil error")
	}
	if strings.Contains(err.Error(), newToken) {
		t.Fatalf("error leaked token: %v", err)
	}
}

func assertStoredCodexCredential(t *testing.T, credential sigma.StoredCredential, accessToken, refreshToken string, expiry time.Time, accountID string) {
	t.Helper()
	if got, want := credential.Type, sigma.CredentialTypeOAuthToken; got != want {
		t.Fatalf("credential type = %q, want %q", got, want)
	}
	if got, want := credential.Value, accessToken; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	if got, want := credential.RefreshToken, refreshToken; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
	if !credential.Expiry.Equal(expiry) {
		t.Fatalf("expiry = %s, want %s", credential.Expiry, expiry)
	}
	if got, want := credential.Metadata[codexOAuthCredentialAccountID], accountID; got != want {
		t.Fatalf("account metadata = %v, want %q", got, want)
	}
	if got, want := credential.Metadata[codexOAuthCredentialChatGPTAcctID], accountID; got != want {
		t.Fatalf("chatgpt account metadata = %v, want %q", got, want)
	}
}

func assertSigmaErrorCode(t *testing.T, err error, code sigma.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error %T is not *sigma.Error: %v", err, err)
	}
	if sigmaErr.Code != code {
		t.Fatalf("error code = %q, want %q", sigmaErr.Code, code)
	}
}

type recordingCredentialStore struct {
	modifyCalled bool
}

func (s *recordingCredentialStore) ReadCredential(context.Context, sigma.ProviderID) (sigma.StoredCredential, bool, error) {
	return sigma.StoredCredential{}, false, nil
}

func (s *recordingCredentialStore) ModifyCredential(context.Context, sigma.ProviderID, sigma.CredentialModifyFunc) (sigma.StoredCredential, bool, error) {
	s.modifyCalled = true
	return sigma.StoredCredential{}, false, nil
}

func (s *recordingCredentialStore) DeleteCredential(context.Context, sigma.ProviderID) error {
	return nil
}

func codexOAuthTestClient(t *testing.T, handler func(*http.Request) *http.Response) *http.Client {
	t.Helper()
	return &http.Client{Transport: codexOAuthRoundTripper(handler)}
}

type codexOAuthRoundTripper func(*http.Request) *http.Response

func (rt codexOAuthRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	resp := rt(r)
	if resp == nil {
		return nil, errors.New("missing test response")
	}
	return resp, nil
}

func codexOAuthJSONResponse(status int, body any) *http.Response {
	var data []byte
	switch typed := body.(type) {
	case string:
		data = []byte(typed)
	default:
		data, _ = json.Marshal(typed)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}
}

func codexOAuthErrorResponse(err error) *http.Response {
	if err == nil {
		err = errors.New("test error")
	}
	return &http.Response{
		StatusCode: 0,
		Body:       io.NopCloser(strings.NewReader(err.Error())),
	}
}

func assertCodexOAuthJSONBody(t *testing.T, r *http.Request, want map[string]any) {
	t.Helper()
	if got, want := r.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := r.Header.Get("Content-Type"), "application/json"; got != want {
		t.Fatalf("content type = %q, want %q", got, want)
	}
	var got map[string]any
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatalf("Decode request body returned error: %v", err)
	}
	for key, wantValue := range want {
		if gotValue := got[key]; gotValue != wantValue {
			t.Fatalf("request body[%q] = %v, want %v", key, gotValue, wantValue)
		}
	}
}

func assertCodexOAuthFormBody(t *testing.T, r *http.Request, want map[string]string) {
	t.Helper()
	values := readCodexOAuthFormBody(t, r)
	for key, wantValue := range want {
		if gotValue := values.Get(key); gotValue != wantValue {
			t.Fatalf("request form[%q] = %q, want %q", key, gotValue, wantValue)
		}
	}
}

func readCodexOAuthFormBody(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	if got, want := r.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := r.Header.Get("Content-Type"), "application/x-www-form-urlencoded"; got != want {
		t.Fatalf("content type = %q, want %q", got, want)
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("ReadAll request body returned error: %v", err)
	}
	values, err := url.ParseQuery(string(data))
	if err != nil {
		t.Fatalf("ParseQuery request body returned error: %v", err)
	}
	return values
}

func codexTestJWT(accountID string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]any{
		codexOAuthJWTClaimPath: map[string]string{
			codexOAuthCredentialChatGPTAcctID: accountID,
		},
	})
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func assertQueryValue(t *testing.T, values url.Values, key string, want string) {
	t.Helper()
	if got := values.Get(key); got != want {
		t.Fatalf("query[%q] = %q, want %q", key, got, want)
	}
}

func withCodexBrowserTestServer(t *testing.T) {
	t.Helper()
	old := codexOAuthBrowserListenAddr
	codexOAuthBrowserListenAddr = "127.0.0.1:0"
	t.Cleanup(func() { codexOAuthBrowserListenAddr = old })
}
