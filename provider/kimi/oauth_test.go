// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package kimi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func TestLoginKimiCodingDeviceCodeReportsDeviceAndReturnsCredentials(t *testing.T) {
	t.Parallel()

	var deviceForm url.Values
	var tokenForm url.Values
	client := kimiCodingOAuthTestHTTPClient(t, func(r *http.Request) *http.Response {
		form := readKimiCodingOAuthForm(t, r)
		switch r.URL.Path {
		case "/api/oauth/device_authorization":
			deviceForm = form
			return kimiCodingOAuthJSONResponse(http.StatusOK, `{
				"device_code":"device-code",
				"user_code":"ABCD-1234",
				"verification_uri":"https://auth.kimi.com/device",
				"verification_uri_complete":"https://auth.kimi.com/device?code=ABCD-1234",
				"interval":0.000001,
				"expires_in":900
			}`)
		case "/api/oauth/token":
			tokenForm = form
			return kimiCodingOAuthJSONResponse(http.StatusOK, `{
				"access_token":"access-token",
				"refresh_token":"refresh-token",
				"expires_in":3600
			}`)
		default:
			t.Fatalf("unexpected OAuth path %q", r.URL.Path)
			return kimiCodingOAuthJSONResponse(http.StatusNotFound, `{}`)
		}
	})

	var info KimiCodingDeviceCodeInfo
	credentials, err := LoginKimiCodingDeviceCode(context.Background(), KimiCodingDeviceCodeLoginOptions{
		HTTPClient:   client,
		OnDeviceCode: func(value KimiCodingDeviceCodeInfo) { info = value },
	})
	if err != nil {
		t.Fatalf("LoginKimiCodingDeviceCode returned error: %v", err)
	}
	if got, want := deviceForm.Get("client_id"), kimiCodingOAuthClientID; got != want {
		t.Fatalf("device client_id = %q, want %q", got, want)
	}
	if got, want := tokenForm.Get("client_id"), kimiCodingOAuthClientID; got != want {
		t.Fatalf("token client_id = %q, want %q", got, want)
	}
	if got, want := tokenForm.Get("grant_type"), kimiCodingOAuthDeviceCodeGrantType; got != want {
		t.Fatalf("token grant_type = %q, want %q", got, want)
	}
	if got, want := tokenForm.Get("device_code"), "device-code"; got != want {
		t.Fatalf("token device_code = %q, want %q", got, want)
	}
	if got, want := info.UserCode, "ABCD-1234"; got != want {
		t.Fatalf("device user code = %q, want %q", got, want)
	}
	if got, want := info.VerificationURI, "https://auth.kimi.com/device?code=ABCD-1234"; got != want {
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

func TestPollKimiCodingDeviceCodeWaitsBeforePollingAndHonorsSlowDown(t *testing.T) {
	t.Parallel()

	responses := []string{
		`{"error":"authorization_pending"}`,
		`{"error":"slow_down"}`,
		`{"access_token":"access-token","refresh_token":"refresh-token","expires_in":3600}`,
	}
	client := kimiCodingOAuthTestHTTPClient(t, func(r *http.Request) *http.Response {
		if got, want := r.URL.Path, "/api/oauth/token"; got != want {
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
		return kimiCodingOAuthJSONResponse(status, body)
	})

	var waits []time.Duration
	credentials, err := pollKimiCodingDeviceCodeWithWait(
		context.Background(),
		client,
		kimiCodingDeviceCode{DeviceCode: "device-code", ExpiresIn: time.Hour, Interval: durationPointer(2 * time.Second)},
		func() time.Time { return time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC) },
		func(_ context.Context, duration time.Duration) error {
			waits = append(waits, duration)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("pollKimiCodingDeviceCodeWithWait returned error: %v", err)
	}
	if got, want := waits, []time.Duration{2 * time.Second, 2 * time.Second, 7 * time.Second}; !equalKimiCodingDurations(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	if got, want := credentials.RefreshToken, "refresh-token"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
}

func TestLoginKimiCodingDeviceCodeRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "unsafe verification URL",
			body: `{"device_code":"device-code","user_code":"ABCD-1234","verification_uri":"http://auth.kimi.com/device","verification_uri_complete":"https://auth.kimi.com/device?code=ABCD-1234","expires_in":900}`,
			want: "untrusted verification_uri",
		},
		{
			name: "missing complete verification URL",
			body: `{"device_code":"device-code","user_code":"ABCD-1234","verification_uri":"https://auth.kimi.com/device","expires_in":900}`,
			want: "missing fields",
		},
		{
			name: "unsafe complete verification URL",
			body: `{"device_code":"device-code","user_code":"ABCD-1234","verification_uri":"https://auth.kimi.com/device","verification_uri_complete":"http://auth.kimi.com/device?code=ABCD-1234","expires_in":900}`,
			want: "untrusted verification_uri",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			called := false
			client := kimiCodingOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
				return kimiCodingOAuthJSONResponse(http.StatusOK, tt.body)
			})
			_, err := LoginKimiCodingDeviceCode(context.Background(), KimiCodingDeviceCodeLoginOptions{
				HTTPClient: client,
				OnDeviceCode: func(KimiCodingDeviceCodeInfo) {
					called = true
				},
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if called {
				t.Fatal("OnDeviceCode was called for an invalid device response")
			}
		})
	}
}

func TestPollKimiCodingDeviceCodeHandlesTerminalFailuresAndCancellation(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{name: "denied", body: `{"error":"access_denied"}`, want: "was denied"},
		{name: "expired", body: `{"error":"expired_token"}`, want: "device code expired"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := kimiCodingOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
				return kimiCodingOAuthJSONResponse(http.StatusBadRequest, tt.body)
			})
			_, err := pollKimiCodingDeviceCodeWithWait(
				context.Background(),
				client,
				kimiCodingDeviceCode{DeviceCode: "device-code", ExpiresIn: time.Hour},
				time.Now,
				func(context.Context, time.Duration) error { return nil },
			)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pollKimiCodingDeviceCodeWithWait(
		ctx,
		nil,
		kimiCodingDeviceCode{DeviceCode: "device-code", ExpiresIn: time.Hour},
		time.Now,
		func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("poll cancellation error = %v, want context.Canceled", err)
	}
}

func TestRefreshKimiCodingTokenReturnsCredentialsAndRedactsFailures(t *testing.T) {
	t.Parallel()

	client := kimiCodingOAuthTestHTTPClient(t, func(r *http.Request) *http.Response {
		form := readKimiCodingOAuthForm(t, r)
		if got, want := form.Get("client_id"), kimiCodingOAuthClientID; got != want {
			t.Fatalf("client_id = %q, want %q", got, want)
		}
		if got, want := form.Get("grant_type"), "refresh_token"; got != want {
			t.Fatalf("grant_type = %q, want %q", got, want)
		}
		if got, want := form.Get("refresh_token"), "old-refresh"; got != want {
			t.Fatalf("refresh_token = %q, want %q", got, want)
		}
		return kimiCodingOAuthJSONResponse(http.StatusOK, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)
	})
	credentials, err := RefreshKimiCodingToken(context.Background(), "old-refresh", KimiCodingOAuthTokenProviderOptions{HTTPClient: client})
	if err != nil {
		t.Fatalf("RefreshKimiCodingToken returned error: %v", err)
	}
	if got, want := credentials.RefreshToken, "new-refresh"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}

	failureClient := kimiCodingOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
		return kimiCodingOAuthJSONResponse(http.StatusUnauthorized, `{"error":"invalid_grant","access_token":"do-not-leak"}`)
	})
	_, err = RefreshKimiCodingToken(context.Background(), "old-refresh", KimiCodingOAuthTokenProviderOptions{HTTPClient: failureClient})
	if err == nil || strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("refresh failure = %v, want redacted error", err)
	}

	malformedClient := kimiCodingOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
		return kimiCodingOAuthJSONResponse(http.StatusOK, `{"access_token":"new-access","expires_in":3600}`)
	})
	_, err = RefreshKimiCodingToken(context.Background(), "old-refresh", KimiCodingOAuthTokenProviderOptions{HTTPClient: malformedClient})
	if err == nil || !strings.Contains(err.Error(), "missing fields") {
		t.Fatalf("malformed refresh error = %v, want missing fields", err)
	}
}

func TestKimiCodingOAuthLimitsResponseBodies(t *testing.T) {
	t.Parallel()

	client := kimiCodingOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
		return kimiCodingOAuthJSONResponse(http.StatusOK, strings.Repeat("x", (1<<20)+1))
	})
	body, _, err := postKimiCodingForm(context.Background(), client, kimiCodingOAuthTokenURL, url.Values{})
	if err != nil {
		t.Fatalf("postKimiCodingForm returned error: %v", err)
	}
	if got, want := len(body), 1<<20; got != want {
		t.Fatalf("response length = %d, want %d", got, want)
	}
}

func TestKimiCodingOAuthTokenProviderRefreshesAndStoredAuthPreservesConfig(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	client := kimiCodingOAuthTestHTTPClient(t, func(*http.Request) *http.Response {
		return kimiCodingOAuthJSONResponse(http.StatusOK, `{"access_token":"refreshed-access","refresh_token":"refreshed-refresh","expires_in":3600}`)
	})
	var refreshed KimiCodingOAuthCredentials
	provider := NewKimiCodingOAuthTokenProvider(KimiCodingOAuthCredentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		Expiry:       now,
	}, KimiCodingOAuthTokenProviderOptions{
		HTTPClient: client,
		Now:        func() time.Time { return now },
		OnRefresh: func(_ context.Context, value KimiCodingOAuthCredentials) error {
			refreshed = value
			return nil
		},
	})
	credential, err := provider.Token(context.Background(), sigma.Model{Provider: sigma.ProviderKimiCoding, ID: "k3"}, sigma.Options{})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if got, want := credential.Value, "refreshed-access"; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	if got, want := refreshed.RefreshToken, "refreshed-refresh"; got != want {
		t.Fatalf("callback refresh token = %q, want %q", got, want)
	}

	store := sigma.NewInMemoryCredentialStore()
	_, _, err = store.ModifyCredential(context.Background(), sigma.ProviderKimiCoding, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Type:         sigma.CredentialTypeOAuthToken,
			Value:        "expired-access",
			RefreshToken: "stored-refresh",
			Expiry:       time.Now().Add(-time.Minute),
			Source:       "test-store",
			ProviderEnv:  map[string]string{"ROUTE": "kimi"},
			Metadata:     map[string]any{"keep": "value"},
		}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}
	registry := sigma.NewRegistry()
	if err := RegisterAuth(registry, KimiCodingOAuthTokenProviderOptions{HTTPClient: client}); err != nil {
		t.Fatalf("RegisterAuth returned error: %v", err)
	}
	credential, err = (sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}).Resolve(
		context.Background(),
		sigma.Model{Provider: sigma.ProviderKimiCoding, ID: "k3"},
		sigma.Options{},
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := credential.Value, "refreshed-access"; got != want {
		t.Fatalf("stored credential value = %q, want %q", got, want)
	}
	stored, ok, err := store.ReadCredential(context.Background(), sigma.ProviderKimiCoding)
	if err != nil || !ok {
		t.Fatalf("ReadCredential = %v, %v, want stored credential", stored, err)
	}
	if got, want := stored.Source, "test-store"; got != want {
		t.Fatalf("stored source = %q, want %q", got, want)
	}
	if got, want := stored.ProviderEnv["ROUTE"], "kimi"; got != want {
		t.Fatalf("stored provider env = %q, want %q", got, want)
	}
	if got, want := stored.Metadata["keep"], "value"; got != want {
		t.Fatalf("stored metadata = %v, want %q", got, want)
	}
}

func durationPointer(value time.Duration) *float64 {
	seconds := value.Seconds()
	return &seconds
}

func equalKimiCodingDurations(left, right []time.Duration) bool {
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

type kimiCodingOAuthRoundTripper func(*http.Request) *http.Response

func (fn kimiCodingOAuthRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request), nil
}

func kimiCodingOAuthTestHTTPClient(t *testing.T, handler func(*http.Request) *http.Response) *http.Client {
	t.Helper()
	return &http.Client{Transport: kimiCodingOAuthRoundTripper(handler)}
}

func kimiCodingOAuthJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func readKimiCodingOAuthForm(t *testing.T, request *http.Request) url.Values {
	t.Helper()
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Header.Get("Accept"), "application/json"; got != want {
		t.Fatalf("Accept = %q, want %q", got, want)
	}
	if got, want := request.Header.Get("Content-Type"), "application/x-www-form-urlencoded"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("ReadAll request body: %v", err)
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("ParseQuery request body: %v", err)
	}
	return form
}
