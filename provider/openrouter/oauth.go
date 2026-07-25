// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openrouter

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/redact"
)

const (
	openRouterOAuthCallbackHost    = "127.0.0.1"
	openRouterOAuthLoginTimeout    = 5 * time.Minute
	openRouterOAuthExchangeTimeout = 30 * time.Second
)

var (
	openRouterOAuthAuthorizeURL = "https://openrouter.ai/auth"
	openRouterOAuthKeyURL       = "https://openrouter.ai/api/v1/auth/keys"
)

// OpenRouterOAuthCredentials carries the permanent API key returned by
// OpenRouter browser OAuth. Callers own persistence; Sigma never stores this
// credential unless StoreOpenRouterOAuthCredentials is called.
type OpenRouterOAuthCredentials struct {
	APIKey string
}

// OpenRouterBrowserAuthInfo reports the authorization URL that callers should
// open in a browser to complete OpenRouter OAuth login.
type OpenRouterBrowserAuthInfo struct {
	URL          string
	Instructions string
}

// OpenRouterBrowserLoginOptions configures OpenRouter browser callback login.
type OpenRouterBrowserLoginOptions struct {
	HTTPClient *http.Client
	OnAuth     func(OpenRouterBrowserAuthInfo)
}

type openRouterBrowserCallbackServer struct {
	server      *http.Server
	done        chan openRouterBrowserCallbackResult
	redirectURI string

	mu      sync.Mutex
	claimed bool
}

type openRouterBrowserCallbackResult struct {
	credentials OpenRouterOAuthCredentials
	err         error
}

// LoginOpenRouterBrowser runs the OpenRouter PKCE browser callback flow and
// returns a permanent API key for caller-managed persistence.
func LoginOpenRouterBrowser(ctx context.Context, opts OpenRouterBrowserLoginOptions) (OpenRouterOAuthCredentials, error) {
	loginCtx, cancel := context.WithTimeout(ctx, openRouterOAuthLoginTimeout)
	defer cancel()

	verifier, challenge, err := newOpenRouterPKCEPair()
	if err != nil {
		return OpenRouterOAuthCredentials{}, err
	}
	server, err := startOpenRouterBrowserCallbackServer(loginCtx, verifier, opts.HTTPClient)
	if err != nil {
		return OpenRouterOAuthCredentials{}, err
	}
	defer server.close()

	authURL, err := openRouterAuthorizationURL(challenge, server.redirectURI)
	if err != nil {
		return OpenRouterOAuthCredentials{}, err
	}
	if opts.OnAuth != nil {
		opts.OnAuth(OpenRouterBrowserAuthInfo{
			URL:          authURL,
			Instructions: "Open the URL in a browser on this machine and complete login.",
		})
	}

	return waitOpenRouterBrowserLogin(loginCtx, server)
}

// StoreOpenRouterOAuthCredentials stores an OpenRouter browser-login API key
// in a caller-supplied CredentialStore for store-backed API-key resolution.
func StoreOpenRouterOAuthCredentials(ctx context.Context, store sigma.CredentialStore, credentials OpenRouterOAuthCredentials) (sigma.StoredCredential, error) {
	if store == nil {
		return sigma.StoredCredential{}, &sigma.Error{Code: sigma.ErrorInvalidOptions, Message: "openrouter oauth: credential store is required"}
	}
	if credentials.APIKey == "" {
		return sigma.StoredCredential{}, &sigma.Error{Code: sigma.ErrorInvalidOptions, Message: "openrouter oauth: API key is required"}
	}
	stored, ok, err := store.ModifyCredential(ctx, sigma.ProviderOpenRouter, func(current sigma.StoredCredential, currentOK bool) (sigma.StoredCredential, bool, error) {
		return storedOpenRouterOAuthCredential(credentials, current, currentOK), true, nil
	})
	if err != nil {
		return sigma.StoredCredential{}, fmt.Errorf("openrouter oauth: store credentials: %w", err)
	}
	if !ok {
		return sigma.StoredCredential{}, &sigma.Error{Code: sigma.ErrorInvalidOptions, Message: "openrouter oauth: credential store did not persist credentials"}
	}
	return stored, nil
}

func startOpenRouterBrowserCallbackServer(ctx context.Context, verifier string, client *http.Client) (*openRouterBrowserCallbackServer, error) {
	path, err := newOpenRouterCallbackPath()
	if err != nil {
		return nil, err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort(openRouterOAuthCallbackHost, "0"))
	if err != nil {
		return nil, fmt.Errorf("openrouter oauth: start callback server: %w", err)
	}
	serverInfo := &openRouterBrowserCallbackServer{
		done:        make(chan openRouterBrowserCallbackResult, 1),
		redirectURI: "http://" + listener.Addr().String() + path,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != path {
			writeOpenRouterOAuthHTML(w, http.StatusNotFound, "Authentication failed", "Callback route not found.")
			return
		}
		if !serverInfo.claim() {
			writeOpenRouterOAuthHTML(w, http.StatusConflict, "Authentication failed", "This OAuth callback has already been used.")
			return
		}
		if req.URL.Query().Get("error") != "" {
			writeOpenRouterOAuthHTML(w, http.StatusBadRequest, "Authentication failed", "OpenRouter authentication was not completed.")
			serverInfo.finish(openRouterBrowserCallbackResult{err: errors.New("openrouter oauth: authorization failed")})
			return
		}
		code := req.URL.Query().Get("code")
		if code == "" {
			writeOpenRouterOAuthHTML(w, http.StatusBadRequest, "Authentication failed", "Missing authorization code.")
			serverInfo.finish(openRouterBrowserCallbackResult{err: errors.New("openrouter oauth: missing authorization code")})
			return
		}
		credentials, err := exchangeOpenRouterAuthorizationCode(ctx, client, code, verifier)
		if err != nil {
			writeOpenRouterOAuthHTML(w, http.StatusBadGateway, "Authentication failed", "OpenRouter key exchange failed.")
			serverInfo.finish(openRouterBrowserCallbackResult{err: err})
			return
		}
		writeOpenRouterOAuthHTML(w, http.StatusOK, "Authentication successful", "OpenRouter authentication completed. You can close this window.")
		serverInfo.finish(openRouterBrowserCallbackResult{credentials: credentials})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeOpenRouterOAuthHTML(w, http.StatusNotFound, "Authentication failed", "Callback route not found.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	serverInfo.server = server
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverInfo.finish(openRouterBrowserCallbackResult{err: fmt.Errorf("openrouter oauth: callback server failed: %w", err)})
		}
	}()
	return serverInfo, nil
}

func (s *openRouterBrowserCallbackServer) claim() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimed {
		return false
	}
	s.claimed = true
	return true
}

func (s *openRouterBrowserCallbackServer) finish(result openRouterBrowserCallbackResult) {
	select {
	case s.done <- result:
	default:
	}
}

func (s *openRouterBrowserCallbackServer) close() {
	if s == nil || s.server == nil {
		return
	}
	_ = s.server.Close()
}

func waitOpenRouterBrowserLogin(ctx context.Context, server *openRouterBrowserCallbackServer) (OpenRouterOAuthCredentials, error) {
	select {
	case result := <-server.done:
		return result.credentials, result.err
	case <-ctx.Done():
		return OpenRouterOAuthCredentials{}, ctx.Err()
	}
}

func openRouterAuthorizationURL(challenge string, redirectURI string) (string, error) {
	authURL, err := url.Parse(openRouterOAuthAuthorizeURL)
	if err != nil {
		return "", fmt.Errorf("openrouter oauth: parse authorize URL: %w", err)
	}
	values := authURL.Query()
	values.Set("callback_url", redirectURI)
	values.Set("code_challenge", challenge)
	values.Set("code_challenge_method", "S256")
	authURL.RawQuery = values.Encode()
	return authURL.String(), nil
}

func exchangeOpenRouterAuthorizationCode(ctx context.Context, client *http.Client, code string, verifier string) (OpenRouterOAuthCredentials, error) {
	exchangeCtx, cancel := context.WithTimeout(ctx, openRouterOAuthExchangeTimeout)
	defer cancel()
	body, err := json.Marshal(map[string]string{
		"code":                  code,
		"code_verifier":         verifier,
		"code_challenge_method": "S256",
	})
	if err != nil {
		return OpenRouterOAuthCredentials{}, fmt.Errorf("openrouter oauth: encode key exchange: %w", err)
	}
	req, err := http.NewRequestWithContext(exchangeCtx, http.MethodPost, openRouterOAuthKeyURL, bytes.NewReader(body))
	if err != nil {
		return OpenRouterOAuthCredentials{}, fmt.Errorf("openrouter oauth: create key exchange: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := openRouterOAuthHTTPClient(client).Do(req)
	if err != nil {
		return OpenRouterOAuthCredentials{}, openRouterOAuthContextOrError(ctx, fmt.Errorf("openrouter oauth: exchange authorization code: %w", err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return OpenRouterOAuthCredentials{}, openRouterOAuthContextOrError(ctx, fmt.Errorf("openrouter oauth: read key exchange response: %w", err))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return OpenRouterOAuthCredentials{}, fmt.Errorf("openrouter oauth: key exchange failed (%d): %s", resp.StatusCode, redact.Preview(string(data), 1024))
	}
	var decoded struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return OpenRouterOAuthCredentials{}, fmt.Errorf("openrouter oauth: decode key exchange response: %w", err)
	}
	if decoded.Key == "" {
		return OpenRouterOAuthCredentials{}, errors.New("openrouter oauth: key exchange response missing key")
	}
	return OpenRouterOAuthCredentials{APIKey: decoded.Key}, nil
}

func storedOpenRouterOAuthCredential(credentials OpenRouterOAuthCredentials, previous sigma.StoredCredential, previousOK bool) sigma.StoredCredential {
	source := previous.Source
	if !previousOK || source == "" {
		source = "credential-store:" + string(sigma.ProviderOpenRouter)
	}
	return sigma.StoredCredential{
		Type:        sigma.CredentialTypeAPIKey,
		Value:       credentials.APIKey,
		Source:      source,
		ProviderEnv: copyOpenRouterStringMap(previous.ProviderEnv),
		Metadata:    copyOpenRouterAnyMap(previous.Metadata),
	}
}

func newOpenRouterPKCEPair() (string, string, error) {
	value, err := randomOpenRouterBase64URL(32)
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256([]byte(value))
	return value, base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

func newOpenRouterCallbackPath() (string, error) {
	value, err := randomOpenRouterBase64URL(16)
	if err != nil {
		return "", err
	}
	return "/oauth/callback/" + value, nil
}

func randomOpenRouterBase64URL(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("openrouter oauth: generate random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func writeOpenRouterOAuthHTML(w http.ResponseWriter, status int, heading string, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>%s</title></head>
<body><main><h1>%s</h1><p>%s</p></main></body>
</html>`, html.EscapeString(heading), html.EscapeString(heading), html.EscapeString(message))
}

func openRouterOAuthHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func openRouterOAuthContextOrError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func copyOpenRouterStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyOpenRouterAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
