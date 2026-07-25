// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openrouter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

func TestOpenRouterAuthorizationURL(t *testing.T) {
	t.Parallel()

	authURL, err := openRouterAuthorizationURL("challenge-abc", "http://127.0.0.1:12345/oauth/callback/test")
	if err != nil {
		t.Fatalf("openRouterAuthorizationURL returned error: %v", err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if got, want := parsed.Host, "openrouter.ai"; got != want {
		t.Fatalf("host = %q, want %q", got, want)
	}
	query := parsed.Query()
	for key, want := range map[string]string{
		"callback_url":          "http://127.0.0.1:12345/oauth/callback/test",
		"code_challenge":        "challenge-abc",
		"code_challenge_method": "S256",
	} {
		if got := query.Get(key); got != want {
			t.Fatalf("query %q = %q, want %q", key, got, want)
		}
	}
}

func TestLoginOpenRouterBrowserCallbackSuccess(t *testing.T) {
	callbackHTML := make(chan string, 1)
	var authChallenge string
	client := openRouterOAuthTestClient(t, func(req *http.Request) *http.Response {
		if got, want := req.URL.String(), openRouterOAuthKeyURL; got != want {
			t.Fatalf("key exchange URL = %q, want %q", got, want)
		}
		if got, want := req.Method, http.MethodPost; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		var body map[string]string
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode key exchange body: %v", err)
		}
		if got, want := body["code"], "callback-code"; got != want {
			t.Fatalf("code = %q, want %q", got, want)
		}
		if got, want := body["code_challenge_method"], "S256"; got != want {
			t.Fatalf("code_challenge_method = %q, want %q", got, want)
		}
		if body["code_verifier"] == "" {
			t.Fatal("code_verifier is empty")
		}
		hash := sha256.Sum256([]byte(body["code_verifier"]))
		if got, want := authChallenge, base64.RawURLEncoding.EncodeToString(hash[:]); got != want {
			t.Fatalf("authorization challenge = %q, want challenge for exchange verifier %q", got, want)
		}
		return openRouterOAuthJSONResponse(http.StatusOK, map[string]any{"key": "sk-or-permanent-key"})
	})

	credentials, err := LoginOpenRouterBrowser(context.Background(), OpenRouterBrowserLoginOptions{
		HTTPClient: client,
		OnAuth: func(info OpenRouterBrowserAuthInfo) {
			authURL, parseErr := url.Parse(info.URL)
			if parseErr != nil {
				callbackHTML <- parseErr.Error()
				return
			}
			if got, want := authURL.Host, "openrouter.ai"; got != want {
				callbackHTML <- "unexpected authorization host: " + got
				return
			}
			query := authURL.Query()
			callbackURL := query.Get("callback_url")
			callback, callbackErr := url.Parse(callbackURL)
			if callbackErr != nil || callback.Scheme != "http" || callback.Hostname() != openRouterOAuthCallbackHost || callback.Port() == "" {
				callbackHTML <- "invalid callback URL: " + callbackURL
				return
			}
			if got, want := query.Get("code_challenge_method"), "S256"; got != want {
				callbackHTML <- "unexpected challenge method: " + got
				return
			}
			challenge := query.Get("code_challenge")
			if challenge == "" {
				callbackHTML <- "code challenge is empty"
				return
			}
			authChallenge = challenge
			go func() {
				resp, requestErr := http.Get(callbackURL + "?code=callback-code")
				if requestErr != nil {
					callbackHTML <- requestErr.Error()
					return
				}
				defer resp.Body.Close()
				data, _ := io.ReadAll(resp.Body)
				if got, want := resp.StatusCode, http.StatusOK; got != want {
					callbackHTML <- "callback status: " + resp.Status
					return
				}
				callbackHTML <- string(data)
			}()
		},
	})
	if err != nil {
		t.Fatalf("LoginOpenRouterBrowser returned error: %v", err)
	}
	if got, want := credentials.APIKey, "sk-or-permanent-key"; got != want {
		t.Fatalf("API key = %q, want %q", got, want)
	}
	if html := receiveOpenRouterOAuthString(t, callbackHTML); !strings.Contains(html, "Authentication successful") {
		t.Fatalf("callback HTML = %q, want success page", html)
	}
}

func TestLoginOpenRouterBrowserRejectsInvalidAndRepeatedCallbacks(t *testing.T) {
	t.Run("missing code", func(t *testing.T) {
		callbackResult := make(chan openRouterOAuthHTTPResult, 1)
		_, err := LoginOpenRouterBrowser(context.Background(), OpenRouterBrowserLoginOptions{
			OnAuth: func(info OpenRouterBrowserAuthInfo) {
				callbackURL := openRouterCallbackURL(t, info.URL)
				go func() { callbackResult <- getOpenRouterOAuthCallback(callbackURL) }()
			},
		})
		if err == nil || !strings.Contains(err.Error(), "missing authorization code") {
			t.Fatalf("LoginOpenRouterBrowser error = %v, want missing authorization code", err)
		}
		result := receiveOpenRouterOAuthResult(t, callbackResult)
		if result.err != nil {
			t.Fatalf("callback request error: %v", result.err)
		}
		if got, want := result.status, http.StatusBadRequest; got != want {
			t.Fatalf("callback status = %d, want %d", got, want)
		}
		if !strings.Contains(result.body, "Missing authorization code") {
			t.Fatalf("callback body = %q, want missing-code page", result.body)
		}
	})

	t.Run("denied", func(t *testing.T) {
		callbackResult := make(chan openRouterOAuthHTTPResult, 1)
		_, err := LoginOpenRouterBrowser(context.Background(), OpenRouterBrowserLoginOptions{
			OnAuth: func(info OpenRouterBrowserAuthInfo) {
				callbackURL := openRouterCallbackURL(t, info.URL)
				go func() { callbackResult <- getOpenRouterOAuthCallback(callbackURL + "?error=access_denied") }()
			},
		})
		if err == nil || !strings.Contains(err.Error(), "authorization failed") {
			t.Fatalf("LoginOpenRouterBrowser error = %v, want authorization failure", err)
		}
		result := receiveOpenRouterOAuthResult(t, callbackResult)
		if result.err != nil {
			t.Fatalf("callback request error: %v", result.err)
		}
		if got, want := result.status, http.StatusBadRequest; got != want {
			t.Fatalf("callback status = %d, want %d", got, want)
		}
	})

	t.Run("one time", func(t *testing.T) {
		exchangeStarted := make(chan struct{})
		releaseExchange := make(chan struct{})
		client := openRouterOAuthTestClient(t, func(*http.Request) *http.Response {
			close(exchangeStarted)
			<-releaseExchange
			return openRouterOAuthJSONResponse(http.StatusOK, map[string]any{"key": "sk-or-once"})
		})
		callbackURL := make(chan string, 1)
		loginResult := make(chan error, 1)
		go func() {
			_, err := LoginOpenRouterBrowser(context.Background(), OpenRouterBrowserLoginOptions{
				HTTPClient: client,
				OnAuth: func(info OpenRouterBrowserAuthInfo) {
					callbackURL <- openRouterCallbackURL(t, info.URL)
				},
			})
			loginResult <- err
		}()
		url := receiveOpenRouterOAuthString(t, callbackURL)
		firstResult := make(chan openRouterOAuthHTTPResult, 1)
		go func() { firstResult <- getOpenRouterOAuthCallback(url + "?code=first") }()
		receiveOpenRouterOAuthSignal(t, exchangeStarted)
		second := getOpenRouterOAuthCallback(url + "?code=second")
		if second.err != nil {
			t.Fatalf("second callback request error: %v", second.err)
		}
		if got, want := second.status, http.StatusConflict; got != want {
			t.Fatalf("second callback status = %d, want %d", got, want)
		}
		close(releaseExchange)
		if first := receiveOpenRouterOAuthResult(t, firstResult); first.err != nil || first.status != http.StatusOK {
			t.Fatalf("first callback result = %#v, want success", first)
		}
		if err := receiveOpenRouterOAuthError(t, loginResult); err != nil {
			t.Fatalf("LoginOpenRouterBrowser returned error: %v", err)
		}
	})
}

func TestLoginOpenRouterBrowserCancellationClosesCallbackServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callbackURL := make(chan string, 1)
	_, err := LoginOpenRouterBrowser(ctx, OpenRouterBrowserLoginOptions{
		OnAuth: func(info OpenRouterBrowserAuthInfo) {
			callbackURL <- openRouterCallbackURL(t, info.URL)
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LoginOpenRouterBrowser error = %v, want context cancellation", err)
	}
	url := receiveOpenRouterOAuthString(t, callbackURL)
	if result := getOpenRouterOAuthCallback(url + "?code=late"); result.err == nil {
		t.Fatalf("callback after cancellation succeeded: %#v", result)
	}
}

func TestExchangeOpenRouterAuthorizationCodeRejectsInvalidResponses(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "provider error is redacted", status: http.StatusUnauthorized, body: `{"api_key":"sk-or-secret-key","message":"denied"}`, want: "[redacted]"},
		{name: "invalid JSON", status: http.StatusOK, body: `{`, want: "decode key exchange response"},
		{name: "missing key", status: http.StatusOK, body: `{}`, want: "response missing key"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := openRouterOAuthTestClient(t, func(*http.Request) *http.Response {
				return &http.Response{
					StatusCode: tt.status,
					Body:       io.NopCloser(strings.NewReader(tt.body)),
					Header:     make(http.Header),
				}
			})
			_, err := exchangeOpenRouterAuthorizationCode(context.Background(), client, "authorization-code", "verifier")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("exchange error = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "sk-or-secret-key") {
				t.Fatalf("exchange error leaked key: %v", err)
			}
		})
	}
}

func TestStoreOpenRouterOAuthCredentialsResolvesForTextAndImages(t *testing.T) {
	store := sigma.NewInMemoryCredentialStore()
	_, _, err := store.ModifyCredential(context.Background(), sigma.ProviderOpenRouter, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{
			Source:      "caller:openrouter",
			ProviderEnv: map[string]string{"region": "test"},
			Metadata:    map[string]any{"account": "test"},
		}, true, nil
	})
	if err != nil {
		t.Fatalf("seed credential store: %v", err)
	}
	stored, err := StoreOpenRouterOAuthCredentials(context.Background(), store, OpenRouterOAuthCredentials{APIKey: "sk-or-stored-key"})
	if err != nil {
		t.Fatalf("StoreOpenRouterOAuthCredentials returned error: %v", err)
	}
	if got, want := stored.Type, sigma.CredentialTypeAPIKey; got != want {
		t.Fatalf("stored type = %q, want %q", got, want)
	}
	if got, want := stored.Source, "caller:openrouter"; got != want {
		t.Fatalf("stored source = %q, want %q", got, want)
	}
	if got, want := stored.ProviderEnv["region"], "test"; got != want {
		t.Fatalf("stored provider env = %q, want %q", got, want)
	}
	if got, want := stored.Metadata["account"], "test"; got != want {
		t.Fatalf("stored metadata = %q, want %q", got, want)
	}

	requests := make(chan bool, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got, want := req.Header.Get("Authorization"), "Bearer sk-or-stored-key"; got != want {
			t.Errorf("authorization = %q, want %q", got, want)
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		stream, _ := payload["stream"].(bool)
		requests <- stream
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_oauth\",\"model\":\"openai/gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"image_oauth","choices":[{"finish_reason":"stop","message":{"images":[{"type":"image_url","image_url":{"url":"https://example.test/output.png"}}]}}]}`)
	}))
	t.Cleanup(server.Close)

	registry := sigma.NewRegistry()
	if err := Register(registry, WithBaseURL(server.URL)); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := RegisterImages(registry, WithImagesBaseURL(server.URL)); err != nil {
		t.Fatalf("RegisterImages returned error: %v", err)
	}
	textModel := sigma.Model{ID: "openai/gpt-4o-mini", Provider: sigma.ProviderOpenRouter, API: sigma.APIOpenAICompletions}
	imageModel := sigma.ImageModel{ID: "google/gemini-2.5-flash-image", Provider: sigma.ProviderOpenRouter, API: sigma.ImageAPIOpenRouterImages}
	if err := registry.RegisterModel(textModel); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	if err := registry.RegisterImageModel(imageModel); err != nil {
		t.Fatalf("RegisterImageModel returned error: %v", err)
	}
	resolver := sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}
	client := sigma.NewClient(sigma.WithRegistry(registry), sigma.WithAuthResolver(resolver))
	if _, err := client.Complete(context.Background(), textModel, sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if _, err := client.GenerateImages(context.Background(), imageModel, sigma.ImageRequest{Prompt: "draw a tree"}); err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	seenStream := false
	seenImage := false
	for range 2 {
		if receiveOpenRouterOAuthBool(t, requests) {
			seenStream = true
		} else {
			seenImage = true
		}
	}
	if !seenStream || !seenImage {
		t.Fatalf("request modes = stream:%t image:%t, want both", seenStream, seenImage)
	}
}

func TestStoreOpenRouterOAuthCredentialsRejectsMissingInput(t *testing.T) {
	t.Parallel()

	if _, err := StoreOpenRouterOAuthCredentials(context.Background(), nil, OpenRouterOAuthCredentials{APIKey: "key"}); err == nil {
		t.Fatal("StoreOpenRouterOAuthCredentials accepted a nil store")
	}
	if _, err := StoreOpenRouterOAuthCredentials(context.Background(), sigma.NewInMemoryCredentialStore(), OpenRouterOAuthCredentials{}); err == nil {
		t.Fatal("StoreOpenRouterOAuthCredentials accepted an empty key")
	}
}

type openRouterOAuthRoundTripper func(*http.Request) (*http.Response, error)

func (f openRouterOAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func openRouterOAuthTestClient(t *testing.T, roundTrip func(*http.Request) *http.Response) *http.Client {
	t.Helper()
	return &http.Client{Transport: openRouterOAuthRoundTripper(func(req *http.Request) (*http.Response, error) {
		return roundTrip(req), nil
	})}
}

func openRouterOAuthJSONResponse(status int, body map[string]any) *http.Response {
	data, _ := json.Marshal(body)
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}
}

type openRouterOAuthHTTPResult struct {
	status int
	body   string
	err    error
}

func getOpenRouterOAuthCallback(callbackURL string) openRouterOAuthHTTPResult {
	resp, err := http.Get(callbackURL)
	if err != nil {
		return openRouterOAuthHTTPResult{err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return openRouterOAuthHTTPResult{status: resp.StatusCode, body: string(body), err: err}
}

func openRouterCallbackURL(t *testing.T, authURL string) string {
	t.Helper()
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	callbackURL := parsed.Query().Get("callback_url")
	if callbackURL == "" {
		t.Fatal("authorization URL has no callback URL")
	}
	return callbackURL
}

func receiveOpenRouterOAuthString(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for value")
		return ""
	}
}

func receiveOpenRouterOAuthResult(t *testing.T, ch <-chan openRouterOAuthHTTPResult) openRouterOAuthHTTPResult {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for callback result")
		return openRouterOAuthHTTPResult{}
	}
}

func receiveOpenRouterOAuthError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for login result")
		return nil
	}
}

func receiveOpenRouterOAuthSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func receiveOpenRouterOAuthBool(t *testing.T, ch <-chan bool) bool {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
		return false
	}
}

func TestOpenRouterPKCEChallengeUsesSHA256(t *testing.T) {
	t.Parallel()

	verifier, challenge, err := newOpenRouterPKCEPair()
	if err != nil {
		t.Fatalf("newOpenRouterPKCEPair returned error: %v", err)
	}
	hash := sha256.Sum256([]byte(verifier))
	if want := base64.RawURLEncoding.EncodeToString(hash[:]); challenge != want {
		t.Fatalf("challenge = %q, want %q", challenge, want)
	}
}
