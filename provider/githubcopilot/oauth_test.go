// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package githubcopilot_test

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
	"github.com/wintermi/sigma/provider/githubcopilot"
)

func TestLoginGitHubCopilotDeviceCodeReportsDeviceAndReturnsCredentials(t *testing.T) {
	t.Parallel()

	var deviceRequest string
	var tokenRequestAuth string
	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		switch r.URL.Path {
		case "/login/device/code":
			deviceRequest = readRequestBody(t, r)
			assertHeader(t, r.Header, "User-Agent", "GitHubCopilotChat/0.35.0")
			return githubCopilotJSONResponse(http.StatusOK, `{
				"device_code":"device-code",
				"user_code":"ABCD-EFGH",
				"verification_uri":"https://github.com/login/device",
				"interval":1,
				"expires_in":900
			}`)
		case "/login/oauth/access_token":
			body := readRequestBody(t, r)
			if !strings.Contains(body, "device_code=device-code") {
				t.Fatalf("device token body = %q, want device_code", body)
			}
			return githubCopilotJSONResponse(http.StatusOK, `{"access_token":"github-refresh-token"}`)
		case "/copilot_internal/v2/token":
			tokenRequestAuth = r.Header.Get("Authorization")
			assertHeader(t, r.Header, "Editor-Version", "vscode/1.107.0")
			return githubCopilotJSONResponse(http.StatusOK, `{
				"token":"tid=test;exp=9999999999;proxy-ep=proxy.individual.githubcopilot.com;",
				"expires_at":9999999999
			}`)
		default:
			t.Fatalf("unexpected OAuth path %q", r.URL.Path)
			return githubCopilotJSONResponse(http.StatusNotFound, `{}`)
		}
	})

	var info githubcopilot.GitHubCopilotDeviceCodeInfo
	credentials, err := githubcopilot.LoginGitHubCopilotDeviceCode(context.Background(), githubcopilot.GitHubCopilotDeviceCodeLoginOptions{
		HTTPClient: client,
		OnDeviceCode: func(device githubcopilot.GitHubCopilotDeviceCodeInfo) {
			info = device
		},
	})
	if err != nil {
		t.Fatalf("LoginGitHubCopilotDeviceCode returned error: %v", err)
	}
	if !strings.Contains(deviceRequest, "client_id=") || !strings.Contains(deviceRequest, "scope=read%3Auser") {
		t.Fatalf("device request body = %q, want client id and scope", deviceRequest)
	}
	if got, want := info.UserCode, "ABCD-EFGH"; got != want {
		t.Fatalf("device user code = %q, want %q", got, want)
	}
	if got, want := info.VerificationURI, "https://github.com/login/device"; got != want {
		t.Fatalf("verification URI = %q, want %q", got, want)
	}
	if got, want := tokenRequestAuth, "Bearer github-refresh-token"; got != want {
		t.Fatalf("refresh Authorization = %q, want %q", got, want)
	}
	if got, want := credentials.AccessToken, "tid=test;exp=9999999999;proxy-ep=proxy.individual.githubcopilot.com;"; got != want {
		t.Fatalf("access token = %q, want %q", got, want)
	}
	if got, want := credentials.RefreshToken, "github-refresh-token"; got != want {
		t.Fatalf("refresh token = %q, want %q", got, want)
	}
	if got, want := credentials.BaseURL, "https://api.individual.githubcopilot.com"; got != want {
		t.Fatalf("base URL = %q, want %q", got, want)
	}
}

func TestProviderAuthResolvesStoredGitHubCopilotOAuthMetadata(t *testing.T) {
	t.Parallel()

	store := sigma.NewInMemoryCredentialStore()
	access := "tid=abc;exp=123;proxy-ep=proxy.business.githubcopilot.com;rest"
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderGitHubCopilot, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
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
	if err := githubcopilot.RegisterAuth(registry, githubcopilot.GitHubCopilotOAuthTokenProviderOptions{}); err != nil {
		t.Fatalf("RegisterAuth returned error: %v", err)
	}
	credential, err := (sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}).Resolve(
		context.Background(),
		sigma.Model{Provider: sigma.ProviderGitHubCopilot, ID: "copilot-test"},
		sigma.Options{},
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := credential.Value, access; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	if got, want := credential.Metadata["baseURL"], "https://api.business.githubcopilot.com"; got != want {
		t.Fatalf("baseURL metadata = %v, want %q", got, want)
	}
}

func TestProviderAuthRefreshUsesStoredEnterpriseDomain(t *testing.T) {
	t.Parallel()

	var refreshHost string
	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		refreshHost = r.URL.Host
		if got, want := r.URL.Path, "/copilot_internal/v2/token"; got != want {
			t.Fatalf("refresh path = %q, want %q", got, want)
		}
		return githubCopilotJSONResponse(http.StatusOK, `{
			"token":"tid=test;exp=9999999999;",
			"expires_at":1893456000
		}`)
	})

	store := sigma.NewInMemoryCredentialStore()
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderGitHubCopilot, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Type:         sigma.CredentialTypeOAuthToken,
			Value:        "expired-token",
			RefreshToken: "github-refresh",
			Expiry:       time.Now().Add(-time.Minute),
			Metadata: map[string]any{
				"enterpriseDomain": "ghe.example.test",
			},
		}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}
	registry := sigma.NewRegistry()
	if err := githubcopilot.RegisterAuth(registry, githubcopilot.GitHubCopilotOAuthTokenProviderOptions{HTTPClient: client}); err != nil {
		t.Fatalf("RegisterAuth returned error: %v", err)
	}
	credential, err := (sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}).Resolve(
		context.Background(),
		sigma.Model{Provider: sigma.ProviderGitHubCopilot, ID: "copilot-test"},
		sigma.Options{},
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := refreshHost, "api.ghe.example.test"; got != want {
		t.Fatalf("refresh host = %q, want %q", got, want)
	}
	if got, want := credential.Metadata["baseURL"], "https://copilot-api.ghe.example.test"; got != want {
		t.Fatalf("baseURL metadata = %v, want %q", got, want)
	}
}

func TestProviderAuthRefreshPrefersConfiguredEnterpriseDomain(t *testing.T) {
	t.Parallel()

	var refreshHost string
	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		refreshHost = r.URL.Host
		return githubCopilotJSONResponse(http.StatusOK, `{
			"token":"tid=test;exp=9999999999;",
			"expires_at":1893456000
		}`)
	})

	store := sigma.NewInMemoryCredentialStore()
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderGitHubCopilot, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Type:         sigma.CredentialTypeOAuthToken,
			Value:        "expired-token",
			RefreshToken: "github-refresh",
			Expiry:       time.Now().Add(-time.Minute),
			Metadata: map[string]any{
				"enterpriseDomain": "ghe.old.example",
			},
		}, true, nil
	})
	if err != nil {
		t.Fatalf("ModifyCredential returned error: %v", err)
	}
	registry := sigma.NewRegistry()
	if err := githubcopilot.RegisterAuth(registry, githubcopilot.GitHubCopilotOAuthTokenProviderOptions{
		HTTPClient:       client,
		EnterpriseDomain: "ghe.new.example",
	}); err != nil {
		t.Fatalf("RegisterAuth returned error: %v", err)
	}
	if _, err := (sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}).Resolve(
		context.Background(),
		sigma.Model{Provider: sigma.ProviderGitHubCopilot, ID: "copilot-test"},
		sigma.Options{},
	); err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := refreshHost, "api.ghe.new.example"; got != want {
		t.Fatalf("refresh host = %q, want %q", got, want)
	}
}

func TestLoginGitHubCopilotDeviceCodeRejectsUnsafeVerificationURI(t *testing.T) {
	t.Parallel()

	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		if r.URL.Path != "/login/device/code" {
			t.Fatalf("unexpected OAuth path %q", r.URL.Path)
		}
		return githubCopilotJSONResponse(http.StatusOK, `{
			"device_code":"device-code",
			"user_code":"ABCD-EFGH",
			"verification_uri":"$(id>/tmp/pwned)",
			"expires_in":900
		}`)
	})

	called := false
	_, err := githubcopilot.LoginGitHubCopilotDeviceCode(context.Background(), githubcopilot.GitHubCopilotDeviceCodeLoginOptions{
		HTTPClient: client,
		OnDeviceCode: func(githubcopilot.GitHubCopilotDeviceCodeInfo) {
			called = true
		},
	})
	if err == nil || !strings.Contains(err.Error(), "untrusted verification_uri") {
		t.Fatalf("error = %v, want untrusted verification_uri", err)
	}
	if called {
		t.Fatal("OnDeviceCode was called for unsafe verification URI")
	}
}

func TestLoginGitHubCopilotDeviceCodeNormalizesVerificationURI(t *testing.T) {
	t.Parallel()

	rawVerificationURI := "https://github.com/login/device code"
	normalizedVerificationURI := (&url.URL{Scheme: "https", Host: "github.com", Path: "/login/device code"}).String()
	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		switch r.URL.Path {
		case "/login/device/code":
			return githubCopilotJSONResponse(http.StatusOK, `{
				"device_code":"device-code",
				"user_code":"ABCD-EFGH",
				"verification_uri":`+strconvQuote(rawVerificationURI)+`,
				"expires_in":1
			}`)
		case "/login/oauth/access_token":
			return githubCopilotJSONResponse(http.StatusOK, `{"error":"slow_down"}`)
		default:
			t.Fatalf("unexpected OAuth path %q", r.URL.Path)
			return githubCopilotJSONResponse(http.StatusNotFound, `{}`)
		}
	})

	var got string
	_, err := githubcopilot.LoginGitHubCopilotDeviceCode(context.Background(), githubcopilot.GitHubCopilotDeviceCodeLoginOptions{
		HTTPClient: client,
		OnDeviceCode: func(info githubcopilot.GitHubCopilotDeviceCodeInfo) {
			got = info.VerificationURI
		},
	})
	if err == nil || !strings.Contains(err.Error(), "slow_down") {
		t.Fatalf("error = %v, want slow_down timeout", err)
	}
	if got != normalizedVerificationURI {
		t.Fatalf("verification URI = %q, want %q", got, normalizedVerificationURI)
	}
	if got == rawVerificationURI {
		t.Fatalf("verification URI was not normalized: %q", got)
	}
}

func TestLoginGitHubCopilotDeviceCodeSlowDownTimeout(t *testing.T) {
	t.Parallel()

	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		switch r.URL.Path {
		case "/login/device/code":
			return githubCopilotJSONResponse(http.StatusOK, `{
				"device_code":"device-code",
				"user_code":"ABCD-EFGH",
				"verification_uri":"https://github.com/login/device",
				"interval":1,
				"expires_in":1
			}`)
		case "/login/oauth/access_token":
			return githubCopilotJSONResponse(http.StatusOK, `{"error":"slow_down"}`)
		default:
			t.Fatalf("unexpected OAuth path %q", r.URL.Path)
			return githubCopilotJSONResponse(http.StatusNotFound, `{}`)
		}
	})

	_, err := githubcopilot.LoginGitHubCopilotDeviceCode(context.Background(), githubcopilot.GitHubCopilotDeviceCodeLoginOptions{
		HTTPClient: client,
	})
	if err == nil || !strings.Contains(err.Error(), "slow_down") {
		t.Fatalf("error = %v, want slow_down timeout", err)
	}
}

func TestLoginGitHubCopilotDeviceCodePendingCanBeCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		switch r.URL.Path {
		case "/login/device/code":
			return githubCopilotJSONResponse(http.StatusOK, `{
				"device_code":"device-code",
				"user_code":"ABCD-EFGH",
				"verification_uri":"https://github.com/login/device",
				"interval":1,
				"expires_in":900
			}`)
		case "/login/oauth/access_token":
			cancel()
			return githubCopilotJSONResponse(http.StatusOK, `{"error":"authorization_pending"}`)
		default:
			t.Fatalf("unexpected OAuth path %q", r.URL.Path)
			return githubCopilotJSONResponse(http.StatusNotFound, `{}`)
		}
	})

	_, err := githubcopilot.LoginGitHubCopilotDeviceCode(ctx, githubcopilot.GitHubCopilotDeviceCodeLoginOptions{
		HTTPClient: client,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestRefreshGitHubCopilotTokenParsesExpiryAndRedactsErrors(t *testing.T) {
	t.Parallel()

	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		if got, want := r.URL.Host, "api.github.com"; got != want {
			t.Fatalf("refresh host = %q, want %q", got, want)
		}
		assertHeader(t, r.Header, "Authorization", "Bearer github-refresh")
		return githubCopilotJSONResponse(http.StatusOK, `{
			"token":"tid=test;exp=9999999999;proxy-ep=proxy.enterprise.test;",
			"expires_at":1893456000
		}`)
	})

	credentials, err := githubcopilot.RefreshGitHubCopilotToken(context.Background(), "github-refresh", githubcopilot.GitHubCopilotOAuthTokenProviderOptions{
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("RefreshGitHubCopilotToken returned error: %v", err)
	}
	if got, want := credentials.Expiry, time.Unix(1893456000, 0); !got.Equal(want) {
		t.Fatalf("expiry = %s, want %s", got, want)
	}
	if got, want := credentials.BaseURL, "https://api.enterprise.test"; got != want {
		t.Fatalf("base URL = %q, want %q", got, want)
	}

	errorClient := githubCopilotOAuthTestClient(func(*http.Request) *http.Response {
		return githubCopilotJSONResponse(http.StatusUnauthorized, `{"access_token":"leaked-token"}`)
	})
	_, err = githubcopilot.RefreshGitHubCopilotToken(context.Background(), "github-refresh", githubcopilot.GitHubCopilotOAuthTokenProviderOptions{
		HTTPClient: errorClient,
	})
	if err == nil {
		t.Fatal("RefreshGitHubCopilotToken returned nil error for failed refresh")
	}
	if strings.Contains(err.Error(), "leaked-token") {
		t.Fatalf("refresh error leaked token: %v", err)
	}

	_, err = githubcopilot.RefreshGitHubCopilotToken(context.Background(), "", githubcopilot.GitHubCopilotOAuthTokenProviderOptions{})
	if !errors.Is(err, sigma.ErrCredentialUnavailable) {
		t.Fatalf("empty refresh error = %v, want ErrCredentialUnavailable", err)
	}
}

func TestGitHubCopilotOAuthTokenProviderHonorsMinimumValidityAndResolves(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	minimumValidity := 30 * time.Minute
	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		if r.URL.Path != "/copilot_internal/v2/token" {
			t.Fatalf("unexpected OAuth path %q", r.URL.Path)
		}
		return githubCopilotJSONResponse(http.StatusOK, `{
			"token":"new-copilot-token",
			"expires_at":1893456000
		}`)
	})

	var refreshed githubcopilot.GitHubCopilotOAuthCredentials
	provider := githubcopilot.NewGitHubCopilotOAuthTokenProvider(githubcopilot.GitHubCopilotOAuthCredentials{
		AccessToken:  "old-copilot-token",
		RefreshToken: "github-refresh",
		Expiry:       now.Add(10 * time.Minute),
	}, githubcopilot.GitHubCopilotOAuthTokenProviderOptions{
		HTTPClient:    client,
		Now:           func() time.Time { return now },
		RefreshBefore: time.Minute,
		OnRefresh: func(_ context.Context, credentials githubcopilot.GitHubCopilotOAuthCredentials) error {
			refreshed = credentials
			return nil
		},
	})

	credential, err := provider.Resolve(context.Background(), sigma.Model{
		Provider: sigma.ProviderGitHubCopilot,
		ID:       "gpt-test",
	}, sigma.Options{OAuthMinimumValidity: &minimumValidity})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got, want := credential.Value, "new-copilot-token"; got != want {
		t.Fatalf("credential value = %q, want %q", got, want)
	}
	if got, want := refreshed.AccessToken, "new-copilot-token"; got != want {
		t.Fatalf("refreshed access token = %q, want %q", got, want)
	}
	if got, want := credential.Source, "github-copilot-oauth"; got != want {
		t.Fatalf("credential source = %q, want %q", got, want)
	}
}

func TestDiscoverGitHubCopilotModelsFiltersAccountCatalog(t *testing.T) {
	t.Parallel()

	const accessToken = "tid=test;proxy-ep=proxy.individual.githubcopilot.com;"
	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		if got, want := r.Method, http.MethodGet; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.String(), "https://copilot.test/models"; got != want {
			t.Fatalf("URL = %q, want %q", got, want)
		}
		assertHeader(t, r.Header, "Accept", "application/json")
		assertHeader(t, r.Header, "Authorization", "Bearer "+accessToken)
		assertHeader(t, r.Header, "X-GitHub-Api-Version", "2026-06-01")
		assertHeader(t, r.Header, "Copilot-Integration-Id", "vscode-chat")
		return githubCopilotJSONResponse(http.StatusOK, `{"data":[
			{"id":"gpt-picker","model_picker_enabled":true,"capabilities":{"supports":{"tool_calls":true}}},
			{"id":"claude-picker","model_picker_enabled":true},
			{"id":"disabled-picker","model_picker_enabled":true,"policy":{"state":"disabled"}},
			{"id":"no-tools","model_picker_enabled":true,"capabilities":{"supports":{"tool_calls":false}}},
			{"id":"policy-only","model_picker_enabled":false,"policy":{"state":"enabled"}},
			{"model_picker_enabled":true}
		]}`)
	})

	availability, err := githubcopilot.DiscoverGitHubCopilotModels(
		context.Background(),
		accessToken,
		githubcopilot.GitHubCopilotModelDiscoveryOptions{
			HTTPClient: client,
			BaseURL:    "https://copilot.test/",
		},
	)
	if err != nil {
		t.Fatalf("DiscoverGitHubCopilotModels returned error: %v", err)
	}
	if got, want := len(availability.ModelIDs), 2; got != want {
		t.Fatalf("available model count = %d, want %d", got, want)
	}
	if got, want := availability.ModelIDs[0], sigma.ModelID("gpt-picker"); got != want {
		t.Fatalf("first available model = %q, want %q", got, want)
	}
	if got, want := availability.ModelIDs[1], sigma.ModelID("claude-picker"); got != want {
		t.Fatalf("second available model = %q, want %q", got, want)
	}

	filter := availability.ModelFilter()
	availability.ModelIDs[0] = "mutated"
	registry := sigma.NewRegistry()
	for _, model := range []sigma.Model{
		{ID: "claude-picker", Provider: sigma.ProviderGitHubCopilot, API: sigma.APIAnthropicMessages},
		{ID: "not-available", Provider: sigma.ProviderGitHubCopilot, API: sigma.APIOpenAICompletions},
		{ID: "gpt-picker", Provider: sigma.ProviderGitHubCopilot, API: sigma.APIOpenAICompletions},
		{ID: "gpt-picker", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAICompletions},
	} {
		if err := registry.RegisterModel(model, sigma.WithMetadataOnly()); err != nil {
			t.Fatalf("RegisterModel(%s/%s) returned error: %v", model.Provider, model.ID, err)
		}
	}
	models := sigma.NewClient(sigma.WithRegistry(registry)).Models(filter)
	if got, want := len(models), 2; got != want {
		t.Fatalf("filtered model count = %d, want %d", got, want)
	}
	if got, want := models[0].ID, sigma.ModelID("claude-picker"); got != want {
		t.Fatalf("first filtered model = %q, want %q", got, want)
	}
	if got, want := models[1].ID, sigma.ModelID("gpt-picker"); got != want {
		t.Fatalf("second filtered model = %q, want %q", got, want)
	}
	if got := sigma.NewClient(sigma.WithRegistry(registry)).Models((githubcopilot.GitHubCopilotModelAvailability{}).ModelFilter()); len(got) != 0 {
		t.Fatalf("empty availability matched models: %+v", got)
	}
}

func TestDiscoverGitHubCopilotModelsPolicyFallbackIsIndividualOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		token     string
		wantCount int
	}{
		{
			name:      "individual",
			token:     "tid=test;proxy-ep=proxy.individual.githubcopilot.com;",
			wantCount: 1,
		},
		{
			name:  "business",
			token: "tid=test;proxy-ep=proxy.business.githubcopilot.com;",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := githubCopilotOAuthTestClient(func(*http.Request) *http.Response {
				return githubCopilotJSONResponse(http.StatusOK, `{"data":[
					{"id":"policy-model","policy":{"state":"enabled"}},
					{"id":"disabled-model","policy":{"state":"disabled"}},
					{"id":"no-tools","policy":{"state":"enabled"},"capabilities":{"supports":{"tool_calls":false}}}
				]}`)
			})
			availability, err := githubcopilot.DiscoverGitHubCopilotModels(
				context.Background(),
				tt.token,
				githubcopilot.GitHubCopilotModelDiscoveryOptions{HTTPClient: client, BaseURL: "https://copilot.test"},
			)
			if err != nil {
				t.Fatalf("DiscoverGitHubCopilotModels returned error: %v", err)
			}
			if got := len(availability.ModelIDs); got != tt.wantCount {
				t.Fatalf("available model count = %d, want %d", got, tt.wantCount)
			}
			if tt.wantCount == 1 && availability.ModelIDs[0] != "policy-model" {
				t.Fatalf("available models = %v, want policy-model", availability.ModelIDs)
			}
		})
	}
}

func TestDiscoverGitHubCopilotModelsUsesEnterpriseEndpoint(t *testing.T) {
	t.Parallel()

	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		if got, want := r.URL.String(), "https://copilot-api.ghe.example.test/models"; got != want {
			t.Fatalf("URL = %q, want %q", got, want)
		}
		return githubCopilotJSONResponse(http.StatusOK, `{"data":[]}`)
	})
	_, err := githubcopilot.DiscoverGitHubCopilotModels(
		context.Background(),
		"copilot-token",
		githubcopilot.GitHubCopilotModelDiscoveryOptions{
			HTTPClient:       client,
			EnterpriseDomain: "https://ghe.example.test/",
		},
	)
	if err != nil {
		t.Fatalf("DiscoverGitHubCopilotModels returned error: %v", err)
	}
}

func TestDiscoverGitHubCopilotModelsValidatesResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "empty", body: `{"data":[]}`},
		{name: "missing data", body: `{}`, wantErr: true},
		{name: "null data", body: `{"data":null}`, wantErr: true},
		{name: "wrong data type", body: `{"data":{}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := githubCopilotOAuthTestClient(func(*http.Request) *http.Response {
				return githubCopilotJSONResponse(http.StatusOK, tt.body)
			})
			availability, err := githubcopilot.DiscoverGitHubCopilotModels(
				context.Background(),
				"copilot-token",
				githubcopilot.GitHubCopilotModelDiscoveryOptions{HTTPClient: client, BaseURL: "https://copilot.test"},
			)
			if tt.wantErr && err == nil {
				t.Fatal("DiscoverGitHubCopilotModels returned nil error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("DiscoverGitHubCopilotModels returned error: %v", err)
			}
			if !tt.wantErr && len(availability.ModelIDs) != 0 {
				t.Fatalf("available models = %v, want empty", availability.ModelIDs)
			}
		})
	}

	_, err := githubcopilot.DiscoverGitHubCopilotModels(
		context.Background(),
		"",
		githubcopilot.GitHubCopilotModelDiscoveryOptions{},
	)
	if !errors.Is(err, sigma.ErrCredentialUnavailable) {
		t.Fatalf("empty token error = %v, want ErrCredentialUnavailable", err)
	}
}

func TestDiscoverGitHubCopilotModelsBoundsResponseBody(t *testing.T) {
	t.Parallel()

	client := githubCopilotOAuthTestClient(func(*http.Request) *http.Response {
		return githubCopilotJSONResponse(http.StatusOK, strings.Repeat("x", (1<<20)+1))
	})
	_, err := githubcopilot.DiscoverGitHubCopilotModels(
		context.Background(),
		"copilot-token",
		githubcopilot.GitHubCopilotModelDiscoveryOptions{HTTPClient: client, BaseURL: "https://copilot.test"},
	)
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("DiscoverGitHubCopilotModels error = %v, want bounded response error", err)
	}
}

func TestDiscoverGitHubCopilotModelsRetriesOneRateLimit(t *testing.T) {
	t.Parallel()

	requests := 0
	client := githubCopilotOAuthTestClient(func(*http.Request) *http.Response {
		requests++
		if requests == 1 {
			response := githubCopilotJSONResponse(http.StatusTooManyRequests, `{"error":"slow down"}`)
			response.Header.Set("Retry-After-Ms", "1")
			return response
		}
		return githubCopilotJSONResponse(http.StatusOK, `{"data":[{"id":"gpt-retry","model_picker_enabled":true}]}`)
	})
	availability, err := githubcopilot.DiscoverGitHubCopilotModels(
		context.Background(),
		"copilot-token",
		githubcopilot.GitHubCopilotModelDiscoveryOptions{HTTPClient: client, BaseURL: "https://copilot.test"},
	)
	if err != nil {
		t.Fatalf("DiscoverGitHubCopilotModels returned error: %v", err)
	}
	if got, want := requests, 2; got != want {
		t.Fatalf("request count = %d, want %d", got, want)
	}
	if got, want := availability.ModelIDs[0], sigma.ModelID("gpt-retry"); got != want {
		t.Fatalf("available model = %q, want %q", got, want)
	}
}

func TestDiscoverGitHubCopilotModelsDoesNotRetryTwiceOrLeakErrors(t *testing.T) {
	t.Parallel()

	requests := 0
	client := githubCopilotOAuthTestClient(func(*http.Request) *http.Response {
		requests++
		response := githubCopilotJSONResponse(http.StatusTooManyRequests, `{"access_token":"leaked-token"}`)
		response.Header.Set("Retry-After-Ms", "1")
		return response
	})
	_, err := githubcopilot.DiscoverGitHubCopilotModels(
		context.Background(),
		"copilot-token",
		githubcopilot.GitHubCopilotModelDiscoveryOptions{HTTPClient: client, BaseURL: "https://copilot.test"},
	)
	if err == nil {
		t.Fatal("DiscoverGitHubCopilotModels returned nil error")
	}
	if got, want := requests, 2; got != want {
		t.Fatalf("request count = %d, want %d", got, want)
	}
	if strings.Contains(err.Error(), "leaked-token") {
		t.Fatalf("discovery error leaked token: %v", err)
	}
}

func TestDiscoverGitHubCopilotModelsDoesNotRetryOtherFailures(t *testing.T) {
	t.Parallel()

	requests := 0
	client := githubCopilotOAuthTestClient(func(*http.Request) *http.Response {
		requests++
		return githubCopilotJSONResponse(http.StatusServiceUnavailable, `{"error":"unavailable"}`)
	})
	_, err := githubcopilot.DiscoverGitHubCopilotModels(
		context.Background(),
		"copilot-token",
		githubcopilot.GitHubCopilotModelDiscoveryOptions{HTTPClient: client, BaseURL: "https://copilot.test"},
	)
	if err == nil {
		t.Fatal("DiscoverGitHubCopilotModels returned nil error")
	}
	if got, want := requests, 1; got != want {
		t.Fatalf("request count = %d, want %d", got, want)
	}
}

func TestDiscoverGitHubCopilotModelsCancelsRateLimitWait(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	requests := 0
	client := githubCopilotOAuthTestClient(func(*http.Request) *http.Response {
		requests++
		cancel()
		response := githubCopilotJSONResponse(http.StatusTooManyRequests, `{}`)
		response.Header.Set("Retry-After", "30")
		return response
	})
	_, err := githubcopilot.DiscoverGitHubCopilotModels(
		ctx,
		"copilot-token",
		githubcopilot.GitHubCopilotModelDiscoveryOptions{HTTPClient: client, BaseURL: "https://copilot.test"},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DiscoverGitHubCopilotModels error = %v, want context.Canceled", err)
	}
	if got, want := requests, 1; got != want {
		t.Fatalf("request count = %d, want %d", got, want)
	}
}

func TestEnableGitHubCopilotModelsReportsPerModelResults(t *testing.T) {
	t.Parallel()

	var bodies []string
	var paths []string
	client := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
		paths = append(paths, r.URL.Path)
		bodies = append(bodies, readRequestBody(t, r))
		assertHeader(t, r.Header, "Authorization", "Bearer copilot-token")
		assertHeader(t, r.Header, "Openai-Intent", "chat-policy")
		assertHeader(t, r.Header, "X-Interaction-Type", "chat-policy")
		switch r.URL.Path {
		case "/models/gpt-test/policy":
			return githubCopilotJSONResponse(http.StatusOK, `{}`)
		case "/models/claude-test/policy":
			return githubCopilotJSONResponse(http.StatusForbidden, `{"access_token":"enable-leak"}`)
		default:
			t.Fatalf("unexpected enable path %q", r.URL.Path)
			return githubCopilotJSONResponse(http.StatusNotFound, `{}`)
		}
	})

	results := githubcopilot.EnableGitHubCopilotModels(
		context.Background(),
		"copilot-token",
		[]string{"gpt-test", "claude-test"},
		githubcopilot.GitHubCopilotModelEnableOptions{
			HTTPClient: client,
			BaseURL:    "https://copilot.test",
		},
	)
	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	if !results[0].Enabled || results[0].Err != nil {
		t.Fatalf("first result = %+v, want enabled", results[0])
	}
	if results[1].Enabled || results[1].Err == nil {
		t.Fatalf("second result = %+v, want failure", results[1])
	}
	if strings.Contains(results[1].Err.Error(), "enable-leak") {
		t.Fatalf("enable error leaked token: %v", results[1].Err)
	}
	if got, want := strings.Join(paths, ","), "/models/gpt-test/policy,/models/claude-test/policy"; got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	for _, body := range bodies {
		if got, want := body, `{"state":"enabled"}`; got != want {
			t.Fatalf("enable body = %q, want %q", got, want)
		}
	}
}

func TestGitHubCopilotBaseURLUsesTokenProxyEndpoint(t *testing.T) {
	t.Parallel()

	if got, want := githubcopilot.GitHubCopilotBaseURL("tid=test;proxy-ep=proxy.example.test;", ""), "https://api.example.test"; got != want {
		t.Fatalf("base URL = %q, want %q", got, want)
	}
	if got, want := githubcopilot.GitHubCopilotBaseURL("", "ghe.example.test"), "https://copilot-api.ghe.example.test"; got != want {
		t.Fatalf("enterprise base URL = %q, want %q", got, want)
	}
}

type githubCopilotRoundTripFunc func(*http.Request) *http.Response

func (f githubCopilotRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r), nil
}

func githubCopilotOAuthTestClient(fn func(*http.Request) *http.Response) *http.Client {
	return &http.Client{Transport: githubCopilotRoundTripFunc(fn)}
}

func githubCopilotJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func readRequestBody(t *testing.T, r *http.Request) string {
	t.Helper()

	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("ReadAll request body returned error: %v", err)
	}
	if err := r.Body.Close(); err != nil {
		t.Fatalf("Close request body returned error: %v", err)
	}
	return string(data)
}

func strconvQuote(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
