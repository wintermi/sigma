// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

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
	"strings"
	"sync"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/redact"
)

const (
	anthropicOAuthClientID             = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	anthropicOAuthScope                = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
	anthropicOAuthCallbackPath         = "/callback"
	anthropicOAuthDefaultRefreshBefore = time.Minute
)

var (
	anthropicOAuthAuthorizeURL = "https://claude.ai/oauth/authorize"
	anthropicOAuthTokenURL     = "https://platform.claude.com/v1/oauth/token"
	// Anthropic's OAuth client registers a fixed localhost redirect URI, so the
	// callback listener must bind this exact port.
	anthropicOAuthListenAddr      = "127.0.0.1:53692"
	anthropicOAuthDefaultRedirect = "http://localhost:53692/callback"
)

// AnthropicOAuthCredentials carries Anthropic (Claude Pro/Max) OAuth tokens.
// Callers own persistence; Sigma never stores these credentials.
type AnthropicOAuthCredentials struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// AnthropicBrowserAuthInfo reports the authorization URL that callers should
// open in a browser to complete Anthropic OAuth login.
type AnthropicBrowserAuthInfo struct {
	URL          string
	Instructions string
}

// AnthropicBrowserManualPrompt describes the fallback prompt for manually
// pasting an authorization code or redirect URL.
type AnthropicBrowserManualPrompt struct {
	Message string
}

// AnthropicBrowserLoginOptions configures Anthropic browser callback login.
type AnthropicBrowserLoginOptions struct {
	HTTPClient   *http.Client
	OnAuth       func(AnthropicBrowserAuthInfo)
	OnManualCode func(context.Context, AnthropicBrowserManualPrompt) (string, error)
}

// AnthropicOAuthTokenProviderOptions configures the OAuth token provider
// returned by NewAnthropicOAuthTokenProvider.
type AnthropicOAuthTokenProviderOptions struct {
	HTTPClient    *http.Client
	Now           func() time.Time
	RefreshBefore time.Duration
	OnRefresh     func(context.Context, AnthropicOAuthCredentials) error
}

type anthropicBrowserCallbackServer struct {
	server      *http.Server
	done        chan anthropicBrowserCallbackResult
	redirectURI string
}

type anthropicBrowserCallbackResult struct {
	code string
	err  error
}

type anthropicAuthorizationInput struct {
	code  string
	state string
}

type anthropicOAuthTokenProvider struct {
	client        *http.Client
	now           func() time.Time
	refreshBefore time.Duration
	onRefresh     func(context.Context, AnthropicOAuthCredentials) error

	mu          sync.Mutex
	credentials AnthropicOAuthCredentials
}

// LoginAnthropicBrowser runs the Anthropic (Claude Pro/Max) browser callback
// OAuth flow and returns credentials for caller-managed persistence.
func LoginAnthropicBrowser(ctx context.Context, opts AnthropicBrowserLoginOptions) (AnthropicOAuthCredentials, error) {
	verifier, challenge, err := newAnthropicPKCEPair()
	if err != nil {
		return AnthropicOAuthCredentials{}, err
	}
	// Anthropic's flow uses the PKCE verifier as the OAuth state value, and the
	// token exchange echoes it back as the state field.
	state := verifier
	server, err := startAnthropicBrowserCallbackServer(state)
	if err != nil {
		return AnthropicOAuthCredentials{}, err
	}
	defer server.close()

	authURL, err := anthropicAuthorizationURL(challenge, state, server.redirectURI)
	if err != nil {
		return AnthropicOAuthCredentials{}, err
	}
	if opts.OnAuth != nil {
		opts.OnAuth(AnthropicBrowserAuthInfo{
			URL:          authURL,
			Instructions: "Open the URL in a browser and complete login. If the browser is on another machine, paste the final redirect URL or authorization code.",
		})
	}

	code, err := waitAnthropicBrowserAuthorizationCode(ctx, state, server, opts.OnManualCode)
	if err != nil {
		return AnthropicOAuthCredentials{}, err
	}
	return exchangeAnthropicAuthorizationCode(ctx, opts.HTTPClient, code, state, verifier, server.redirectURI)
}

// RefreshAnthropicToken refreshes Anthropic OAuth credentials from a refresh
// token.
func RefreshAnthropicToken(ctx context.Context, refreshToken string, opts AnthropicOAuthTokenProviderOptions) (AnthropicOAuthCredentials, error) {
	if refreshToken == "" {
		return AnthropicOAuthCredentials{}, &sigma.CredentialUnavailableError{
			Sources: []string{"anthropic-refresh-token"},
		}
	}
	return postAnthropicToken(ctx, opts.HTTPClient, map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     anthropicOAuthClientID,
		"refresh_token": refreshToken,
	}, "refresh")
}

// NewAnthropicOAuthTokenProvider adapts caller-managed Anthropic OAuth
// credentials to Sigma's OAuthTokenProvider interface. Refreshed credentials
// are kept in memory and passed to OnRefresh for caller persistence.
func NewAnthropicOAuthTokenProvider(credentials AnthropicOAuthCredentials, opts AnthropicOAuthTokenProviderOptions) sigma.OAuthTokenProvider {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	refreshBefore := opts.RefreshBefore
	if refreshBefore == 0 {
		refreshBefore = anthropicOAuthDefaultRefreshBefore
	}
	return &anthropicOAuthTokenProvider{
		client:        opts.HTTPClient,
		now:           now,
		refreshBefore: refreshBefore,
		onRefresh:     opts.OnRefresh,
		credentials:   credentials,
	}
}

func (p *anthropicOAuthTokenProvider) Token(ctx context.Context, model sigma.Model, _ sigma.Options) (sigma.Credential, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.credentials.AccessToken == "" {
		return sigma.Credential{}, &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"anthropic-oauth"},
		}
	}

	if err := p.refreshIfNeeded(ctx, model); err != nil {
		return sigma.Credential{}, err
	}

	return sigma.Credential{
		Type:   sigma.CredentialTypeOAuthToken,
		Value:  p.credentials.AccessToken,
		Expiry: p.credentials.Expiry,
		Source: "anthropic-oauth",
	}, nil
}

func (p *anthropicOAuthTokenProvider) refreshIfNeeded(ctx context.Context, model sigma.Model) error {
	if !p.shouldRefresh() {
		return nil
	}
	if p.credentials.RefreshToken == "" {
		return &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"anthropic-refresh-token"},
		}
	}
	refreshed, err := RefreshAnthropicToken(ctx, p.credentials.RefreshToken, AnthropicOAuthTokenProviderOptions{
		HTTPClient: p.client,
	})
	if err != nil {
		return err
	}
	p.credentials = refreshed
	if p.onRefresh == nil {
		return nil
	}
	if err := p.onRefresh(ctx, refreshed); err != nil {
		return errors.New("anthropic oauth: refresh callback failed")
	}
	return nil
}

func (p *anthropicOAuthTokenProvider) shouldRefresh() bool {
	if p.credentials.Expiry.IsZero() {
		return false
	}
	return !p.now().Add(p.refreshBefore).Before(p.credentials.Expiry)
}

func anthropicAuthorizationURL(challenge string, state string, redirectURI string) (string, error) {
	authURL, err := url.Parse(anthropicOAuthAuthorizeURL)
	if err != nil {
		return "", fmt.Errorf("anthropic oauth: parse authorize URL: %w", err)
	}
	values := authURL.Query()
	values.Set("code", "true")
	values.Set("client_id", anthropicOAuthClientID)
	values.Set("response_type", "code")
	values.Set("redirect_uri", redirectURI)
	values.Set("scope", anthropicOAuthScope)
	values.Set("code_challenge", challenge)
	values.Set("code_challenge_method", "S256")
	values.Set("state", state)
	authURL.RawQuery = values.Encode()
	return authURL.String(), nil
}

func startAnthropicBrowserCallbackServer(state string) (*anthropicBrowserCallbackServer, error) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", anthropicOAuthListenAddr)
	if err != nil {
		return nil, fmt.Errorf("anthropic oauth: start callback server: %w", err)
	}
	result := make(chan anthropicBrowserCallbackResult, 1)
	serverInfo := &anthropicBrowserCallbackServer{
		done:        result,
		redirectURI: anthropicBrowserRedirectURI(listener),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(anthropicOAuthCallbackPath, func(w http.ResponseWriter, req *http.Request) {
		if oauthErr := req.URL.Query().Get("error"); oauthErr != "" {
			writeAnthropicOAuthHTML(w, http.StatusBadRequest, "Authentication failed", "Anthropic authentication did not complete.")
			serverInfo.finish(anthropicBrowserCallbackResult{err: fmt.Errorf("anthropic oauth: authorization failed: %s", redact.Preview(oauthErr, 256))})
			return
		}
		if req.URL.Query().Get("state") != state {
			writeAnthropicOAuthHTML(w, http.StatusBadRequest, "Authentication failed", "State mismatch.")
			serverInfo.finish(anthropicBrowserCallbackResult{err: fmt.Errorf("anthropic oauth: state mismatch")})
			return
		}
		code := req.URL.Query().Get("code")
		if code == "" {
			writeAnthropicOAuthHTML(w, http.StatusBadRequest, "Authentication failed", "Missing authorization code.")
			serverInfo.finish(anthropicBrowserCallbackResult{err: fmt.Errorf("anthropic oauth: missing authorization code")})
			return
		}
		writeAnthropicOAuthHTML(w, http.StatusOK, "Authentication successful", "Anthropic authentication completed. You can close this window.")
		serverInfo.finish(anthropicBrowserCallbackResult{code: code})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeAnthropicOAuthHTML(w, http.StatusNotFound, "Authentication failed", "Callback route not found.")
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverInfo.server = server
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverInfo.finish(anthropicBrowserCallbackResult{err: fmt.Errorf("anthropic oauth: callback server failed: %w", err)})
		}
	}()
	return serverInfo, nil
}

func (s *anthropicBrowserCallbackServer) finish(result anthropicBrowserCallbackResult) {
	select {
	case s.done <- result:
	default:
	}
}

func (s *anthropicBrowserCallbackServer) close() {
	if s == nil || s.server == nil {
		return
	}
	_ = s.server.Close()
}

func waitAnthropicBrowserAuthorizationCode(
	ctx context.Context,
	state string,
	server *anthropicBrowserCallbackServer,
	manual func(context.Context, AnthropicBrowserManualPrompt) (string, error),
) (string, error) {
	manualResult := make(chan anthropicBrowserCallbackResult, 1)
	if manual != nil {
		go func() {
			input, err := manual(ctx, AnthropicBrowserManualPrompt{
				Message: "Paste the authorization code or full redirect URL:",
			})
			if err != nil {
				manualResult <- anthropicBrowserCallbackResult{err: err}
				return
			}
			parsed := parseAnthropicAuthorizationInput(input)
			if parsed.state != "" && parsed.state != state {
				manualResult <- anthropicBrowserCallbackResult{err: fmt.Errorf("anthropic oauth: state mismatch")}
				return
			}
			if parsed.code == "" {
				manualResult <- anthropicBrowserCallbackResult{err: fmt.Errorf("anthropic oauth: missing authorization code")}
				return
			}
			manualResult <- anthropicBrowserCallbackResult{code: parsed.code}
		}()
	}

	for {
		select {
		case result := <-server.done:
			return result.code, result.err
		case result := <-manualResult:
			return result.code, result.err
		case <-ctx.Done():
			server.close()
			return "", ctx.Err()
		}
	}
}

func parseAnthropicAuthorizationInput(input string) anthropicAuthorizationInput {
	value := strings.TrimSpace(input)
	if value == "" {
		return anthropicAuthorizationInput{}
	}
	if parsed, err := url.Parse(value); err == nil && (parsed.Scheme != "" || parsed.Host != "") {
		return anthropicAuthorizationInput{
			code:  parsed.Query().Get("code"),
			state: parsed.Query().Get("state"),
		}
	}
	// Anthropic's manual flow displays the code as "<code>#<state>".
	if strings.Contains(value, "#") && !strings.Contains(value, "://") {
		parts := strings.SplitN(value, "#", 2)
		return anthropicAuthorizationInput{code: parts[0], state: parts[1]}
	}
	value = strings.TrimPrefix(value, "?")
	if strings.Contains(value, "code=") {
		values, err := url.ParseQuery(value)
		if err == nil {
			return anthropicAuthorizationInput{
				code:  values.Get("code"),
				state: values.Get("state"),
			}
		}
	}
	return anthropicAuthorizationInput{code: value}
}

func anthropicBrowserRedirectURI(listener net.Listener) string {
	if anthropicOAuthListenAddr == "127.0.0.1:53692" {
		return anthropicOAuthDefaultRedirect
	}
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return anthropicOAuthDefaultRedirect
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   anthropicOAuthCallbackPath,
	}).String()
}

func exchangeAnthropicAuthorizationCode(ctx context.Context, client *http.Client, code string, state string, verifier string, redirectURI string) (AnthropicOAuthCredentials, error) {
	return postAnthropicToken(ctx, client, map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     anthropicOAuthClientID,
		"code":          code,
		"state":         state,
		"redirect_uri":  redirectURI,
		"code_verifier": verifier,
	}, "exchange")
}

func postAnthropicToken(ctx context.Context, client *http.Client, payload map[string]string, operation string) (AnthropicOAuthCredentials, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return AnthropicOAuthCredentials{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicOAuthTokenURL, bytes.NewReader(body))
	if err != nil {
		return AnthropicOAuthCredentials{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := anthropicOAuthHTTPClient(client).Do(req)
	if err != nil {
		return AnthropicOAuthCredentials{}, anthropicOAuthContextOrError(ctx, fmt.Errorf("anthropic oauth: token %s: %w", operation, err))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AnthropicOAuthCredentials{}, anthropicOAuthContextOrError(ctx, fmt.Errorf("anthropic oauth: read token %s response: %w", operation, err))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return AnthropicOAuthCredentials{}, fmt.Errorf("anthropic oauth: token %s failed (%d): %s", operation, resp.StatusCode, redact.Preview(string(data), 1024))
	}

	var decoded struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return AnthropicOAuthCredentials{}, fmt.Errorf("anthropic oauth: decode token %s response: %w", operation, err)
	}
	if decoded.AccessToken == "" || decoded.RefreshToken == "" || decoded.ExpiresIn <= 0 {
		return AnthropicOAuthCredentials{}, fmt.Errorf("anthropic oauth: token %s response missing fields", operation)
	}
	return AnthropicOAuthCredentials{
		AccessToken:  decoded.AccessToken,
		RefreshToken: decoded.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(decoded.ExpiresIn) * time.Second),
	}, nil
}

func newAnthropicPKCEPair() (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("anthropic oauth: generate random value: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	hash := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

func writeAnthropicOAuthHTML(w http.ResponseWriter, status int, heading string, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>%s</title></head>
<body><main><h1>%s</h1><p>%s</p></main></body>
</html>`, html.EscapeString(heading), html.EscapeString(heading), html.EscapeString(message))
}

func anthropicOAuthHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func anthropicOAuthContextOrError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}
