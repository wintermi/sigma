// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package radius

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func TestLoginRadiusDeviceCode(t *testing.T) {
	t.Parallel()

	var requestsMu sync.Mutex
	var requests []radiusOAuthTestRequest
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestsMu.Lock()
		requests = append(requests, radiusOAuthTestRequest{path: r.URL.Path, form: string(body)})
		requestsMu.Unlock()
		switch r.URL.Path {
		case "/v1/oauth/device":
			_, _ = io.WriteString(w, `{"device_code":"device-code","user_code":"ABCD-1234","verification_uri_complete":"https://radius.example/pair?code=ABCD-1234","expires_in":600,"interval":0.001}`)
		case "/v1/oauth/token":
			_, _ = io.WriteString(w, `{"access_token":"access-token","refresh_token":"refresh-token","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	var info RadiusDeviceCodeInfo
	credentials, err := LoginRadiusDeviceCode(context.Background(), RadiusDeviceCodeLoginOptions{
		Client: radiusOAuthTestClientConfig(server.URL),
		OnDeviceCode: func(got RadiusDeviceCodeInfo) {
			info = got
		},
	})
	if err != nil {
		t.Fatalf("LoginRadiusDeviceCode returned error: %v", err)
	}
	if got, want := credentials.AccessToken, "access-token"; got != want {
		t.Fatalf("access token = %q, want %q", got, want)
	}
	if got, want := credentials.RefreshToken, "refresh-token"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
	if got, want := info.UserCode, "ABCD-1234"; got != want {
		t.Fatalf("user code = %q, want %q", got, want)
	}
	if got, want := info.VerificationURI, "https://radius.example/pair?code=ABCD-1234"; got != want {
		t.Fatalf("verification URI = %q, want %q", got, want)
	}
	requestsMu.Lock()
	defer requestsMu.Unlock()
	if got, want := len(requests), 2; got != want {
		t.Fatalf("request count = %d, want %d", got, want)
	}
	assertRadiusOAuthFormValue(t, requests[0], "/v1/oauth/device", "client_id", "radius-client")
	assertRadiusOAuthFormValue(t, requests[0], "/v1/oauth/device", "scope", "gateway offline_access")
	assertRadiusOAuthFormValue(t, requests[1], "/v1/oauth/token", "grant_type", radiusOAuthDeviceCodeGrantType)
	assertRadiusOAuthFormValue(t, requests[1], "/v1/oauth/token", "device_code", "device-code")
}

func TestRadiusDeviceCodePollingSlowDownAndCancellation(t *testing.T) {
	t.Parallel()

	var calls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)
		case 2:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"slow_down"}`)
		default:
			_, _ = io.WriteString(w, `{"access_token":"access-token","refresh_token":"refresh-token","expires_in":3600}`)
		}
	}))
	t.Cleanup(server.Close)

	var waits []time.Duration
	credentials, err := pollRadiusDeviceCodeWithWait(
		context.Background(),
		nil,
		radiusOAuthTestClientConfig(server.URL),
		radiusOAuthDeviceCode{deviceCode: "device-code", expiresIn: time.Minute, interval: time.Second},
		func() time.Time { return time.Unix(0, 0) },
		func(_ context.Context, duration time.Duration) error {
			waits = append(waits, duration)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("pollRadiusDeviceCodeWithWait returned error: %v", err)
	}
	if got, want := credentials.AccessToken, "access-token"; got != want {
		t.Fatalf("access token = %q, want %q", got, want)
	}
	if got, want := waits, []time.Duration{time.Second, time.Second, 6 * time.Second}; !equalRadiusDurations(got, want) {
		t.Fatalf("waits = %#v, want %#v", got, want)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = pollRadiusDeviceCodeWithWait(
		ctx,
		nil,
		radiusOAuthTestClientConfig(server.URL),
		radiusOAuthDeviceCode{deviceCode: "device-code", expiresIn: time.Minute, interval: time.Second},
		time.Now,
		radiusOAuthSleepContext,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled polling error = %v, want context.Canceled", err)
	}
}

func TestRadiusDeviceCodeRejectsUnsafeVerificationURI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"device_code":"device-code","user_code":"ABCD-1234","verification_uri":"http://radius.example/pair","expires_in":600}`)
	}))
	t.Cleanup(server.Close)

	_, err := requestRadiusDeviceCode(context.Background(), nil, radiusOAuthTestClientConfig(server.URL))
	if err == nil || !strings.Contains(err.Error(), "verification URI must use HTTPS") {
		t.Fatalf("unsafe verification URI error = %v, want HTTPS validation", err)
	}
}

func TestLoginRadiusBrowserCallbackAndManualFallback(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth":
			_, _ = io.WriteString(w, `{"authorizationEndpoint":"`+server.URL+`/authorize"}`)
		case "/v1/oauth/token":
			form, _ := io.ReadAll(r.Body)
			values, _ := url.ParseQuery(string(form))
			if got, want := values.Get("grant_type"), "authorization_code"; got != want {
				t.Errorf("grant type = %q, want %q", got, want)
			}
			if values.Get("code_verifier") == "" {
				t.Error("code verifier is empty")
			}
			_, _ = io.WriteString(w, `{"access_token":"browser-access","refresh_token":"browser-refresh","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	callbackURL := radiusOAuthTestCallbackURL(t)
	client := radiusOAuthTestClientConfig(server.URL)
	client.RedirectURI = callbackURL
	credentials, err := LoginRadiusBrowser(context.Background(), RadiusBrowserLoginOptions{
		Client: client,
		OnAuth: func(info RadiusBrowserAuthInfo) {
			authorizeURL, err := url.Parse(info.URL)
			if err != nil {
				t.Fatalf("parse authorization URL: %v", err)
			}
			if got, want := authorizeURL.Query().Get("client_id"), "radius-client"; got != want {
				t.Fatalf("client ID = %q, want %q", got, want)
			}
			if got, want := authorizeURL.Query().Get("scope"), "gateway offline_access"; got != want {
				t.Fatalf("scope = %q, want %q", got, want)
			}
			if authorizeURL.Query().Get("code_challenge") == "" {
				t.Fatal("code challenge is empty")
			}
			redirect := callbackURL + "?code=browser-code&state=" + url.QueryEscape(authorizeURL.Query().Get("state"))
			resp, err := http.Get(redirect)
			if err != nil {
				t.Fatalf("complete browser callback: %v", err)
			}
			_ = resp.Body.Close()
		},
	})
	if err != nil {
		t.Fatalf("LoginRadiusBrowser returned error: %v", err)
	}
	if got, want := credentials.AccessToken, "browser-access"; got != want {
		t.Fatalf("browser access token = %q, want %q", got, want)
	}

	manualCallbackURL := radiusOAuthTestCallbackURL(t)
	client.RedirectURI = manualCallbackURL
	state := make(chan string, 1)
	credentials, err = LoginRadiusBrowser(context.Background(), RadiusBrowserLoginOptions{
		Client: client,
		OnAuth: func(info RadiusBrowserAuthInfo) {
			authorizeURL, parseErr := url.Parse(info.URL)
			if parseErr != nil {
				t.Fatalf("parse authorization URL: %v", parseErr)
			}
			state <- authorizeURL.Query().Get("state")
		},
		OnManualCode: func(context.Context, RadiusBrowserManualPrompt) (string, error) {
			return manualCallbackURL + "?code=manual-code&state=" + url.QueryEscape(<-state), nil
		},
	})
	if err != nil {
		t.Fatalf("manual LoginRadiusBrowser returned error: %v", err)
	}
	if got, want := credentials.AccessToken, "browser-access"; got != want {
		t.Fatalf("manual browser access token = %q, want %q", got, want)
	}
}

func TestRadiusOAuthRefreshAndTokenProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		form, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(form))
		if got, want := values.Get("grant_type"), "refresh_token"; got != want {
			t.Errorf("grant type = %q, want %q", got, want)
		}
		if got, want := values.Get("refresh_token"), "old-refresh"; got != want {
			t.Errorf("refresh token = %q, want %q", got, want)
		}
		_, _ = io.WriteString(w, `{"access_token":"refreshed-access","expires_in":3600}`)
	}))
	t.Cleanup(server.Close)

	options := RadiusOAuthTokenProviderOptions{Client: radiusOAuthTestClientConfig(server.URL)}
	credentials, err := RefreshRadiusToken(context.Background(), "old-refresh", options)
	if err != nil {
		t.Fatalf("RefreshRadiusToken returned error: %v", err)
	}
	if got, want := credentials.RefreshToken, "old-refresh"; got != want {
		t.Fatalf("refresh token = %q, want previous token %q", got, want)
	}

	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	minimumValidity := 30 * time.Minute
	refreshed := make(chan RadiusOAuthCredentials, 1)
	provider := NewRadiusOAuthTokenProvider(RadiusOAuthCredentials{
		AccessToken:  "expired-access",
		RefreshToken: "old-refresh",
		Expiry:       now.Add(10 * time.Minute),
	}, RadiusOAuthTokenProviderOptions{
		Client: radiusOAuthTestClientConfig(server.URL),
		Now:    func() time.Time { return now },
		OnRefresh: func(_ context.Context, got RadiusOAuthCredentials) error {
			refreshed <- got
			return nil
		},
	})
	credential, err := provider.Token(context.Background(), sigma.Model{ID: "radius-model", Provider: sigma.ProviderRadius}, sigma.Options{OAuthMinimumValidity: &minimumValidity})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if got, want := credential.Value, "refreshed-access"; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	if got := <-refreshed; got.AccessToken != "refreshed-access" {
		t.Fatalf("refresh callback credentials = %#v", got)
	}
}

func TestRadiusProviderAuthAndCatalogResolver(t *testing.T) {
	t.Parallel()

	var catalogAuthorization string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth/token":
			_, _ = io.WriteString(w, `{"access_token":"stored-access","refresh_token":"stored-refresh","expires_in":3600}`)
		case "/v1/config":
			catalogAuthorization = r.Header.Get("Authorization")
			_, _ = io.WriteString(w, `{"baseUrl":"`+server.URL+`","models":[{"id":"radius-oauth","name":"Radius OAuth","reasoning":false,"input":["text"],"cost":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0},"contextWindow":1024,"maxTokens":128}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	store := sigma.NewInMemoryCredentialStore()
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderRadius, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Type:         sigma.CredentialTypeOAuthToken,
			Value:        "expired-access",
			RefreshToken: "old-refresh",
			Expiry:       time.Now().Add(-time.Minute),
		}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}
	registry := sigma.NewRegistry()
	options := RadiusOAuthTokenProviderOptions{Client: radiusOAuthTestClientConfig(server.URL)}
	if err := RegisterAuth(registry, options); err != nil {
		t.Fatalf("RegisterAuth returned error: %v", err)
	}
	resolver := sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}
	failCatalogResolution := false
	catalogResolver := sigma.AuthResolverFunc(func(ctx context.Context, model sigma.Model, opts sigma.Options) (sigma.Credential, error) {
		if failCatalogResolution {
			return sigma.Credential{}, errors.New("catalog credentials unavailable")
		}
		return resolver.Resolve(ctx, model, opts)
	})
	if err := Register(registry, WithGatewayURL(server.URL), WithCatalogAuthResolver(catalogResolver)); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	if err := client.RefreshTextModels(context.Background(), sigma.ProviderRadius); err != nil {
		t.Fatalf("RefreshTextModels returned error: %v", err)
	}
	if got, want := catalogAuthorization, "Bearer stored-access"; got != want {
		t.Fatalf("catalog authorization = %q, want %q", got, want)
	}
	if _, ok := client.GetModel(sigma.ProviderRadius, "radius-oauth"); !ok {
		t.Fatal("OAuth-refreshed Radius model was not registered")
	}
	failCatalogResolution = true
	if err := client.RefreshTextModels(context.Background(), sigma.ProviderRadius); err == nil {
		t.Fatal("RefreshTextModels returned nil after OAuth catalog resolution failed")
	}
	if _, ok := client.GetModel(sigma.ProviderRadius, "radius-oauth"); !ok {
		t.Fatal("failed OAuth catalog refresh removed the prior dynamic model")
	}

	precedenceRegistry := sigma.NewRegistry()
	if err := Register(precedenceRegistry, WithGatewayURL(server.URL), WithCatalogAPIKey("explicit-key"), WithCatalogAuthResolver(sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{}, errors.New("catalog resolver should not run")
	}))); err != nil {
		t.Fatalf("Register precedence registry returned error: %v", err)
	}
	if err := precedenceRegistry.RefreshTextModels(context.Background(), sigma.ProviderRadius); err != nil {
		t.Fatalf("explicit catalog-key refresh returned error: %v", err)
	}
	if got, want := catalogAuthorization, "Bearer explicit-key"; got != want {
		t.Fatalf("explicit catalog authorization = %q, want %q", got, want)
	}
}

func TestRadiusOAuthRejectsInvalidClientAndRedactsErrors(t *testing.T) {
	t.Parallel()

	if err := RegisterAuth(sigma.NewRegistry(), RadiusOAuthTokenProviderOptions{}); err == nil {
		t.Fatal("RegisterAuth accepted an empty OAuth client")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","token":"secret-token"}`)
	}))
	t.Cleanup(server.Close)
	_, err := RefreshRadiusToken(context.Background(), "old-refresh", RadiusOAuthTokenProviderOptions{Client: radiusOAuthTestClientConfig(server.URL)})
	if err == nil {
		t.Fatal("RefreshRadiusToken returned nil for an OAuth error")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("refresh error leaked token: %v", err)
	}
}

type radiusOAuthTestRequest struct {
	path string
	form string
}

func radiusOAuthTestClientConfig(gatewayURL string) RadiusOAuthClientConfig {
	return RadiusOAuthClientConfig{
		GatewayURL: gatewayURL,
		ClientID:   "radius-client",
		Scopes:     []string{"gateway", "offline_access"},
	}
}

func radiusOAuthTestCallbackURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve callback port: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release callback port: %v", err)
	}
	return "http://" + address + "/oauth/callback"
}

func assertRadiusOAuthFormValue(t *testing.T, request radiusOAuthTestRequest, path, key, want string) {
	t.Helper()
	if got := request.path; got != path {
		t.Fatalf("request path = %q, want %q", got, path)
	}
	values, err := url.ParseQuery(request.form)
	if err != nil {
		t.Fatalf("parse form: %v", err)
	}
	if got := values.Get(key); got != want {
		t.Fatalf("form %s = %q, want %q", key, got, want)
	}
}

func equalRadiusDurations(got, want []time.Duration) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func TestRadiusBrowserManualStateMismatch(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth" {
			_, _ = io.WriteString(w, `{"authorizationEndpoint":"`+server.URL+`/authorize"}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	client := radiusOAuthTestClientConfig(server.URL)
	client.RedirectURI = radiusOAuthTestCallbackURL(t)
	_, err := LoginRadiusBrowser(context.Background(), RadiusBrowserLoginOptions{
		Client: client,
		OnManualCode: func(context.Context, RadiusBrowserManualPrompt) (string, error) {
			return client.RedirectURI + "?code=manual-code&state=wrong-state", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("manual state mismatch error = %v, want state mismatch", err)
	}
}

func TestRadiusOAuthResponseBodyLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"body": strings.Repeat("x", radiusOAuthMaxBodyBytes+1)})
	}))
	t.Cleanup(server.Close)
	_, err := RefreshRadiusToken(context.Background(), "old-refresh", RadiusOAuthTokenProviderOptions{Client: radiusOAuthTestClientConfig(server.URL)})
	if err == nil || !strings.Contains(err.Error(), "response body exceeds limit") {
		t.Fatalf("body-limit error = %v, want bounded response error", err)
	}
}
