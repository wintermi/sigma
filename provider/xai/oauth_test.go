// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package xai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

var xaiOAuthTestClientConfig = XAIOAuthClientConfig{
	ClientID: "sigma-test-client",
	Scopes:   []string{"openid", "offline_access", "api:access"},
}

func TestLoginXAIDeviceCodeReportsDeviceAndReturnsCredentials(t *testing.T) {
	t.Parallel()

	var deviceForm url.Values
	var tokenForm url.Values
	client := xaiOAuthTestHTTPClient(t, func(r *http.Request) *http.Response {
		form := readXAIForm(t, r)
		switch r.URL.Path {
		case "/oauth2/device/code":
			deviceForm = form
			return xaiOAuthJSONResponse(http.StatusOK, `{
				"device_code":"device-code",
				"user_code":"ABCD-1234",
				"verification_uri":"https://auth.x.ai/device",
				"verification_uri_complete":"https://auth.x.ai/device?code=ABCD-1234",
				"interval":0.000001,
				"expires_in":900
			}`)
		case "/oauth2/token":
			tokenForm = form
			return xaiOAuthJSONResponse(http.StatusOK, `{
				"access_token":"access-token",
				"refresh_token":"refresh-token",
				"expires_in":3600
			}`)
		default:
			t.Fatalf("unexpected OAuth path %q", r.URL.Path)
			return xaiOAuthJSONResponse(http.StatusNotFound, `{}`)
		}
	})

	var info XAIDeviceCodeInfo
	credentials, err := LoginXAIDeviceCode(context.Background(), XAIDeviceCodeLoginOptions{
		Client:       xaiOAuthTestClientConfig,
		HTTPClient:   client,
		OnDeviceCode: func(value XAIDeviceCodeInfo) { info = value },
	})
	if err != nil {
		t.Fatalf("LoginXAIDeviceCode returned error: %v", err)
	}
	if got, want := deviceForm.Get("client_id"), xaiOAuthTestClientConfig.ClientID; got != want {
		t.Fatalf("device client_id = %q, want %q", got, want)
	}
	if got, want := deviceForm.Get("scope"), "openid offline_access api:access"; got != want {
		t.Fatalf("device scope = %q, want %q", got, want)
	}
	if got, want := tokenForm.Get("grant_type"), xaiOAuthDeviceCodeGrantType; got != want {
		t.Fatalf("token grant_type = %q, want %q", got, want)
	}
	if got, want := tokenForm.Get("device_code"), "device-code"; got != want {
		t.Fatalf("token device_code = %q, want %q", got, want)
	}
	if got, want := info.UserCode, "ABCD-1234"; got != want {
		t.Fatalf("device user code = %q, want %q", got, want)
	}
	if got, want := info.VerificationURI, "https://auth.x.ai/device?code=ABCD-1234"; got != want {
		t.Fatalf("device verification URI = %q, want %q", got, want)
	}
	if got, want := credentials.AccessToken, "access-token"; got != want {
		t.Fatalf("access token = %q, want %q", got, want)
	}
	if got, want := credentials.RefreshToken, "refresh-token"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
	if credentials.Expiry.Before(time.Now().Add(59 * time.Minute)) {
		t.Fatalf("expiry = %s, want approximately one hour from now", credentials.Expiry)
	}
}

func TestPollXAIDeviceCodeWaitsBeforePollingAndHonorsSlowDown(t *testing.T) {
	t.Parallel()

	responses := []string{
		`{"error":"authorization_pending"}`,
		`{"error":"slow_down"}`,
		`{"access_token":"access-token","refresh_token":"refresh-token"}`,
	}
	client := xaiOAuthTestHTTPClient(t, func(r *http.Request) *http.Response {
		if got, want := r.URL.Path, "/oauth2/token"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if len(responses) == 0 {
			t.Fatal("unexpected token poll")
		}
		body := responses[0]
		responses = responses[1:]
		status := http.StatusBadRequest
		if strings.Contains(body, "access-token") {
			status = http.StatusOK
		}
		return xaiOAuthJSONResponse(status, body)
	})

	var waits []time.Duration
	credentials, err := pollXAIDeviceCodeWithWait(
		context.Background(),
		client,
		xaiOAuthTestClientConfig,
		xaiDeviceCode{DeviceCode: "device-code", Interval: 2, ExpiresIn: time.Hour},
		func() time.Time { return time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC) },
		func(_ context.Context, duration time.Duration) error {
			waits = append(waits, duration)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("pollXAIDeviceCodeWithWait returned error: %v", err)
	}
	if got, want := waits, []time.Duration{2 * time.Second, 2 * time.Second, 7 * time.Second}; !equalDurations(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	if got, want := credentials.RefreshToken, "refresh-token"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
}

func TestLoginXAIDeviceCodeRejectsUnsafeVerificationURI(t *testing.T) {
	t.Parallel()

	client := xaiOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
		return xaiOAuthJSONResponse(http.StatusOK, `{
			"device_code":"device-code",
			"user_code":"ABCD-1234",
			"verification_uri":"http://auth.x.ai/device",
			"expires_in":900
		}`)
	})

	called := false
	_, err := LoginXAIDeviceCode(context.Background(), XAIDeviceCodeLoginOptions{
		Client:     xaiOAuthTestClientConfig,
		HTTPClient: client,
		OnDeviceCode: func(XAIDeviceCodeInfo) {
			called = true
		},
	})
	if err == nil || !strings.Contains(err.Error(), "untrusted verification_uri") {
		t.Fatalf("error = %v, want untrusted verification_uri", err)
	}
	if called {
		t.Fatal("OnDeviceCode was called for an unsafe verification URI")
	}
}

func TestLoginXAIDeviceCodeRejectsMalformedResponse(t *testing.T) {
	t.Parallel()

	client := xaiOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
		return xaiOAuthJSONResponse(http.StatusOK, `{
			"device_code":"device-code",
			"user_code":"ABCD-1234",
			"verification_uri":"https://auth.x.ai/device",
			"expires_in":0
		}`)
	})
	_, err := LoginXAIDeviceCode(context.Background(), XAIDeviceCodeLoginOptions{
		Client:     xaiOAuthTestClientConfig,
		HTTPClient: client,
	})
	if err == nil || !strings.Contains(err.Error(), "missing fields") {
		t.Fatalf("malformed response error = %v", err)
	}
}

func TestXAIOAuthClientConfigIsRequired(t *testing.T) {
	t.Parallel()

	_, err := LoginXAIDeviceCode(context.Background(), XAIDeviceCodeLoginOptions{})
	if err == nil || !strings.Contains(err.Error(), "client ID is required") {
		t.Fatalf("missing client ID error = %v", err)
	}
	_, err = LoginXAIDeviceCode(context.Background(), XAIDeviceCodeLoginOptions{Client: XAIOAuthClientConfig{ClientID: "test"}})
	if err == nil || !strings.Contains(err.Error(), "scopes are required") {
		t.Fatalf("missing scopes error = %v", err)
	}
}

func TestXAIDeviceCodePollingDefaultsAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	if got, want := xaiPollInterval(nil), 5*time.Second; got != want {
		t.Fatalf("default poll interval = %s, want %s", got, want)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pollXAIDeviceCodeWithWait(
		ctx,
		nil,
		xaiOAuthTestClientConfig,
		xaiDeviceCode{DeviceCode: "device-code", ExpiresIn: time.Hour},
		time.Now,
		func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("poll cancellation error = %v, want context.Canceled", err)
	}
}

func TestPollXAIDeviceCodeReturnsAuthorizationDenial(t *testing.T) {
	t.Parallel()

	client := xaiOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
		return xaiOAuthJSONResponse(http.StatusBadRequest, `{"error":"access_denied"}`)
	})
	_, err := pollXAIDeviceCodeWithWait(
		context.Background(),
		client,
		xaiOAuthTestClientConfig,
		xaiDeviceCode{DeviceCode: "device-code", ExpiresIn: time.Hour},
		time.Now,
		func(context.Context, time.Duration) error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "was denied") {
		t.Fatalf("authorization denial error = %v", err)
	}
}

func TestRefreshXAITokenPreservesRefreshTokenAndRedactsFailures(t *testing.T) {
	t.Parallel()

	client := xaiOAuthTestHTTPClient(t, func(r *http.Request) *http.Response {
		form := readXAIForm(t, r)
		if got, want := form.Get("grant_type"), "refresh_token"; got != want {
			t.Fatalf("grant_type = %q, want %q", got, want)
		}
		if got, want := form.Get("refresh_token"), "old-refresh"; got != want {
			t.Fatalf("refresh_token = %q, want %q", got, want)
		}
		return xaiOAuthJSONResponse(http.StatusOK, `{"access_token":"new-access"}`)
	})
	credentials, err := RefreshXAIToken(context.Background(), "old-refresh", XAIOAuthTokenProviderOptions{
		Client:     xaiOAuthTestClientConfig,
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("RefreshXAIToken returned error: %v", err)
	}
	if got, want := credentials.RefreshToken, "old-refresh"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
	rotated, err := xaiCredentialsFromTokenResponse([]byte(`{"access_token":"new-access","refresh_token":"new-refresh"}`), "old-refresh", time.Now())
	if err != nil {
		t.Fatalf("xaiCredentialsFromTokenResponse returned error: %v", err)
	}
	if got, want := rotated.RefreshToken, "new-refresh"; got != want {
		t.Fatalf("rotated refresh token = %q, want %q", got, want)
	}

	failureClient := xaiOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
		return xaiOAuthJSONResponse(http.StatusUnauthorized, `{"error":"invalid_grant","access_token":"do-not-leak"}`)
	})
	_, err = RefreshXAIToken(context.Background(), "old-refresh", XAIOAuthTokenProviderOptions{
		Client:     xaiOAuthTestClientConfig,
		HTTPClient: failureClient,
	})
	if err == nil || strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("refresh failure = %v, want redacted error", err)
	}
}

func TestXAIOAuthTokenProviderRefreshesAndReportsCallbackFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	client := xaiOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
		return xaiOAuthJSONResponse(http.StatusOK, `{"access_token":"refreshed-access"}`)
	})
	var refreshed XAIOAuthCredentials
	provider := NewXAIOAuthTokenProvider(XAIOAuthCredentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		Expiry:       now,
	}, XAIOAuthTokenProviderOptions{
		Client:     xaiOAuthTestClientConfig,
		HTTPClient: client,
		Now:        func() time.Time { return now },
		OnRefresh: func(_ context.Context, value XAIOAuthCredentials) error {
			refreshed = value
			return nil
		},
	})
	credential, err := provider.Token(context.Background(), sigma.Model{Provider: sigma.ProviderXAI, ID: "grok-test"}, sigma.Options{})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if got, want := credential.Value, "refreshed-access"; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	if got, want := refreshed.RefreshToken, "old-refresh"; got != want {
		t.Fatalf("callback refresh token = %q, want %q", got, want)
	}

	failingProvider := NewXAIOAuthTokenProvider(XAIOAuthCredentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		Expiry:       now,
	}, XAIOAuthTokenProviderOptions{
		Client:     xaiOAuthTestClientConfig,
		HTTPClient: client,
		Now:        func() time.Time { return now },
		OnRefresh: func(context.Context, XAIOAuthCredentials) error {
			return errors.New("persistence failure")
		},
	})
	_, err = failingProvider.Token(context.Background(), sigma.Model{Provider: sigma.ProviderXAI, ID: "grok-test"}, sigma.Options{})
	if err == nil || err.Error() != "xai oauth: refresh callback failed" {
		t.Fatalf("callback error = %v, want safe callback failure", err)
	}
}

func TestXAIProviderAuthRefreshesStoredCredentials(t *testing.T) {
	t.Parallel()

	store := sigma.NewInMemoryCredentialStore()
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderXAI, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Type:         sigma.CredentialTypeOAuthToken,
			Value:        "expired-access",
			RefreshToken: "stored-refresh",
			Expiry:       time.Now().Add(-time.Minute),
			Source:       "test-store",
			ProviderEnv:  map[string]string{"ROUTE": "xai"},
			Metadata:     map[string]any{"keep": "value"},
		}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}
	client := xaiOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
		return xaiOAuthJSONResponse(http.StatusOK, `{"access_token":"refreshed-access"}`)
	})
	registry := sigma.NewRegistry()
	if err := RegisterAuth(registry, XAIOAuthTokenProviderOptions{Client: xaiOAuthTestClientConfig, HTTPClient: client}); err != nil {
		t.Fatalf("RegisterAuth returned error: %v", err)
	}
	credential, err := (sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}).Resolve(
		context.Background(),
		sigma.Model{Provider: sigma.ProviderXAI, ID: "grok-test"},
		sigma.Options{},
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := credential.Value, "refreshed-access"; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	stored, ok, err := store.ReadCredential(context.Background(), sigma.ProviderXAI)
	if err != nil || !ok {
		t.Fatalf("ReadCredential = %v, %v, want stored credential", stored, err)
	}
	if got, want := stored.Source, "test-store"; got != want {
		t.Fatalf("stored source = %q, want %q", got, want)
	}
	if got, want := stored.ProviderEnv["ROUTE"], "xai"; got != want {
		t.Fatalf("stored provider env = %q, want %q", got, want)
	}
	if got, want := stored.Metadata["keep"], "value"; got != want {
		t.Fatalf("stored metadata = %v, want %q", got, want)
	}
}

func TestXAIOAuthBearerReachesBothTextRoutes(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path + ":" + r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		switch r.URL.Path {
		case "/chat/completions":
			_, _ = io.WriteString(w, "data: {\"id\":\"chat\",\"model\":\"grok-chat\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		case "/responses":
			_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response\",\"model\":\"grok-responses\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	auth := NewXAIOAuthTokenProvider(XAIOAuthCredentials{AccessToken: "oauth-access"}, XAIOAuthTokenProviderOptions{Client: xaiOAuthTestClientConfig})
	for _, test := range []struct {
		name     string
		model    sigma.Model
		provider sigma.TextProvider
	}{
		{
			name:     "chat completions",
			model:    sigma.Model{ID: "grok-chat", Provider: sigma.ProviderXAI, API: sigma.APIOpenAICompletions},
			provider: NewProvider(WithBaseURL(server.URL)),
		},
		{
			name:     "responses",
			model:    sigma.Model{ID: "grok-responses", Provider: sigma.ProviderXAI, API: sigma.APIOpenAIResponses},
			provider: NewResponsesProvider(WithBaseURL(server.URL)),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := sigma.NewRegistry()
			if err := registry.RegisterTextProvider(sigma.ProviderXAI, test.provider); err != nil {
				t.Fatalf("RegisterTextProvider returned error: %v", err)
			}
			if err := registry.RegisterModel(test.model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry), sigma.WithAuthResolver(auth))
			if _, err := client.Complete(context.Background(), test.model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}}); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}
		})
	}

	for range 2 {
		select {
		case got := <-requests:
			if !strings.HasSuffix(got, ":Bearer oauth-access") {
				t.Fatalf("Authorization = %q, want OAuth bearer token", got)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for xAI request")
		}
	}
}

func xaiOAuthTestHTTPClient(t *testing.T, handler func(*http.Request) *http.Response) *http.Client {
	t.Helper()
	return &http.Client{Transport: xaiOAuthRoundTripper(func(r *http.Request) (*http.Response, error) {
		response := handler(r)
		response.Request = r
		return response, nil
	})}
}

type xaiOAuthRoundTripper func(*http.Request) (*http.Response, error)

func (rt xaiOAuthRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

func xaiOAuthJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func readXAIForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
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

func equalDurations(left, right []time.Duration) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
