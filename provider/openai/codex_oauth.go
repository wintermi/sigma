// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/redact"
)

const (
	codexOAuthClientID                = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexOAuthBrowserCallbackPath     = "/auth/callback"
	codexOAuthBrowserDefaultRedirect  = "http://localhost:1455/auth/callback"
	codexOAuthBrowserScope            = "openid profile email offline_access"
	codexOAuthDeviceVerificationURI   = "https://auth.openai.com/codex/device"
	codexOAuthDeviceRedirectURI       = "https://auth.openai.com/deviceauth/callback"
	codexOAuthDefaultPollInterval     = 5 * time.Second
	codexOAuthMinimumPollInterval     = time.Second
	codexOAuthSlowDownPollIncrement   = 5 * time.Second
	codexOAuthDefaultRefreshBefore    = time.Minute
	codexOAuthCredentialAccountID     = "accountID"
	codexOAuthCredentialChatGPTAcctID = "chatgpt_account_id"
	codexOAuthJWTClaimPath            = "https://api.openai.com/auth"
)

var (
	codexOAuthAuthorizeURL      = "https://auth.openai.com/oauth/authorize"
	codexOAuthTokenURL          = "https://auth.openai.com/oauth/token"
	codexOAuthDeviceUserCodeURL = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	codexOAuthDeviceTokenURL    = "https://auth.openai.com/api/accounts/deviceauth/token"
	codexOAuthBrowserListenAddr = "127.0.0.1:1455"
	codexOAuthDeviceTimeout     = 15 * time.Minute
)

// CodexOAuthCredentials carries OpenAI Codex OAuth tokens. Callers own
// persistence; Sigma never stores these credentials.
type CodexOAuthCredentials struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	AccountID    string
}

// CodexDeviceCodeInfo reports the user code and verification URL that should be
// shown to the caller during OpenAI Codex device-code login.
type CodexDeviceCodeInfo struct {
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresIn       time.Duration
}

// CodexDeviceCodeLoginOptions configures OpenAI Codex device-code login.
type CodexDeviceCodeLoginOptions struct {
	HTTPClient   *http.Client
	OnDeviceCode func(CodexDeviceCodeInfo)
}

// CodexBrowserAuthInfo reports the authorization URL that callers should open
// in a browser to complete OpenAI Codex OAuth login.
type CodexBrowserAuthInfo struct {
	URL          string
	Instructions string
}

// CodexBrowserManualPrompt describes the fallback prompt for manually pasting
// an authorization code or redirect URL.
type CodexBrowserManualPrompt struct {
	Message string
}

// CodexBrowserLoginOptions configures OpenAI Codex browser callback login.
type CodexBrowserLoginOptions struct {
	HTTPClient   *http.Client
	OnAuth       func(CodexBrowserAuthInfo)
	OnManualCode func(context.Context, CodexBrowserManualPrompt) (string, error)
}

// CodexOAuthTokenProviderOptions configures the OAuth token provider returned
// by NewCodexOAuthTokenProvider.
type CodexOAuthTokenProviderOptions struct {
	HTTPClient    *http.Client
	Now           func() time.Time
	RefreshBefore time.Duration
	OnRefresh     func(context.Context, CodexOAuthCredentials) error
}

// CodexProviderAuth returns OpenAI Codex OAuth auth descriptors for provider.
func CodexProviderAuth(provider sigma.ProviderID, opts CodexOAuthTokenProviderOptions) sigma.ProviderAuth {
	return sigma.ProviderAuth{
		OAuth: &sigma.OAuthAuth{
			Name:          "OpenAI Codex OAuth",
			RefreshBefore: opts.RefreshBefore,
			Refresh: func(ctx context.Context, stored sigma.StoredCredential) (sigma.StoredCredential, error) {
				refreshed, err := RefreshOpenAICodexToken(ctx, stored.RefreshToken, opts)
				if err != nil {
					return sigma.StoredCredential{}, err
				}
				return storedCodexOAuthCredential(provider, refreshed, stored), nil
			},
			Credential: func(_ context.Context, model sigma.Model, _ sigma.Options, stored sigma.StoredCredential) (sigma.Credential, error) {
				if stored.Value == "" {
					return sigma.Credential{}, &sigma.CredentialUnavailableError{Provider: model.Provider, Model: model.ID, Sources: []string{"openai-codex-oauth"}}
				}
				metadata := copyAnyMap(stored.Metadata)
				accountID := storedStringMetadata(metadata, codexOAuthCredentialAccountID)
				if accountID == "" {
					accountID = storedStringMetadata(metadata, codexOAuthCredentialChatGPTAcctID)
				}
				if accountID == "" {
					var err error
					accountID, err = codexAccountIDFromToken(stored.Value)
					if err != nil {
						return sigma.Credential{}, err
					}
					if metadata == nil {
						metadata = make(map[string]any)
					}
					metadata[codexOAuthCredentialAccountID] = accountID
					metadata[codexOAuthCredentialChatGPTAcctID] = accountID
				}
				source := stored.Source
				if source == "" {
					source = "credential-store:" + string(provider)
				}
				return sigma.Credential{
					Type:     sigma.CredentialTypeOAuthToken,
					Value:    stored.Value,
					Expiry:   stored.Expiry,
					Source:   source,
					Metadata: metadata,
				}, nil
			},
		},
	}
}

// RegisterCodexProviderAuth registers OpenAI Codex auth descriptors on registry.
func RegisterCodexProviderAuth(registry *sigma.Registry, provider sigma.ProviderID, opts CodexOAuthTokenProviderOptions, registerOpts ...sigma.RegisterOption) error {
	registerOpts = append([]sigma.RegisterOption{sigma.WithOverride()}, registerOpts...)
	if err := sigma.RegisterProviderAuth(registry, provider, CodexProviderAuth(provider, opts), registerOpts...); err != nil {
		return fmt.Errorf("openai codex auth: register provider auth: %w", err)
	}
	return nil
}

// RegisterDefaultCodexProviderAuth registers OpenAI Codex auth descriptors on the default registry.
func RegisterDefaultCodexProviderAuth(provider sigma.ProviderID, opts CodexOAuthTokenProviderOptions, registerOpts ...sigma.RegisterOption) error {
	registerOpts = append([]sigma.RegisterOption{sigma.WithOverride()}, registerOpts...)
	if err := sigma.RegisterDefaultProviderAuth(provider, CodexProviderAuth(provider, opts), registerOpts...); err != nil {
		return fmt.Errorf("openai codex auth: register default provider auth: %w", err)
	}
	return nil
}

type codexDeviceAuthInfo struct {
	deviceAuthID string
	userCode     string
	interval     time.Duration
}

type codexDeviceTokenSuccess struct {
	authorizationCode string
	codeVerifier      string
}

type codexBrowserAuthorizationFlow struct {
	verifier string
	state    string
	url      string
}

type codexBrowserCallbackServer struct {
	server      *http.Server
	done        chan codexBrowserCallbackResult
	redirectURI string
}

type codexBrowserCallbackResult struct {
	code string
	err  error
}

type codexAuthorizationInput struct {
	code  string
	state string
}

type codexOAuthToken struct {
	access  string
	refresh string
	expiry  time.Time
}

type codexOAuthTokenProvider struct {
	client        *http.Client
	now           func() time.Time
	refreshBefore time.Duration
	onRefresh     func(context.Context, CodexOAuthCredentials) error

	mu          sync.Mutex
	credentials CodexOAuthCredentials
}

// LoginOpenAICodexDeviceCode runs the OpenAI Codex device-code OAuth flow and
// returns credentials for caller-managed persistence.
func LoginOpenAICodexDeviceCode(ctx context.Context, opts CodexDeviceCodeLoginOptions) (CodexOAuthCredentials, error) {
	device, err := startOpenAICodexDeviceAuth(ctx, opts.HTTPClient)
	if err != nil {
		return CodexOAuthCredentials{}, err
	}
	if opts.OnDeviceCode != nil {
		opts.OnDeviceCode(CodexDeviceCodeInfo{
			UserCode:        device.userCode,
			VerificationURI: codexOAuthDeviceVerificationURI,
			Interval:        device.interval,
			ExpiresIn:       codexOAuthDeviceTimeout,
		})
	}

	code, err := pollOpenAICodexDeviceAuth(ctx, opts.HTTPClient, device)
	if err != nil {
		return CodexOAuthCredentials{}, err
	}
	token, err := exchangeOpenAICodexAuthorizationCode(ctx, opts.HTTPClient, code.authorizationCode, code.codeVerifier, codexOAuthDeviceRedirectURI)
	if err != nil {
		return CodexOAuthCredentials{}, err
	}
	return codexCredentialsFromToken(token)
}

// LoginOpenAICodexBrowser runs the OpenAI Codex browser callback OAuth flow and
// returns credentials for caller-managed persistence.
func LoginOpenAICodexBrowser(ctx context.Context, opts CodexBrowserLoginOptions) (CodexOAuthCredentials, error) {
	state, err := randomBase64URL(16)
	if err != nil {
		return CodexOAuthCredentials{}, err
	}
	server, err := startOpenAICodexBrowserCallbackServer(state)
	if err != nil {
		return CodexOAuthCredentials{}, err
	}
	defer server.close()

	flow, err := newOpenAICodexBrowserAuthorizationFlow(state, server.redirectURI)
	if err != nil {
		return CodexOAuthCredentials{}, err
	}
	if opts.OnAuth != nil {
		opts.OnAuth(CodexBrowserAuthInfo{
			URL:          flow.url,
			Instructions: "Open the URL in a browser and complete login. If the browser is on another machine, paste the final redirect URL or authorization code.",
		})
	}

	code, err := waitOpenAICodexBrowserAuthorizationCode(ctx, flow.state, server, opts.OnManualCode)
	if err != nil {
		return CodexOAuthCredentials{}, err
	}
	token, err := exchangeOpenAICodexAuthorizationCode(ctx, opts.HTTPClient, code, flow.verifier, server.redirectURI)
	if err != nil {
		return CodexOAuthCredentials{}, err
	}
	return codexCredentialsFromToken(token)
}

// RefreshOpenAICodexToken refreshes OpenAI Codex OAuth credentials from a
// refresh token.
func RefreshOpenAICodexToken(ctx context.Context, refreshToken string, opts CodexOAuthTokenProviderOptions) (CodexOAuthCredentials, error) {
	token, err := refreshOpenAICodexAccessToken(ctx, opts.HTTPClient, refreshToken)
	if err != nil {
		return CodexOAuthCredentials{}, err
	}
	return codexCredentialsFromToken(token)
}

// NewCodexOAuthTokenProvider adapts caller-managed OpenAI Codex OAuth
// credentials to Sigma's OAuthTokenProvider interface. Refreshed credentials are
// kept in memory and passed to OnRefresh for caller persistence.
func NewCodexOAuthTokenProvider(credentials CodexOAuthCredentials, opts CodexOAuthTokenProviderOptions) sigma.OAuthTokenProvider {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	refreshBefore := opts.RefreshBefore
	if refreshBefore == 0 {
		refreshBefore = codexOAuthDefaultRefreshBefore
	}
	return &codexOAuthTokenProvider{
		client:        opts.HTTPClient,
		now:           now,
		refreshBefore: refreshBefore,
		onRefresh:     opts.OnRefresh,
		credentials:   credentials,
	}
}

// StoreCodexOAuthCredentials stores OpenAI Codex OAuth credentials in a
// caller-supplied CredentialStore for store-backed provider auth.
func StoreCodexOAuthCredentials(ctx context.Context, store sigma.CredentialStore, provider sigma.ProviderID, credentials CodexOAuthCredentials) (sigma.StoredCredential, error) {
	if store == nil {
		return sigma.StoredCredential{}, &sigma.Error{Code: sigma.ErrorInvalidOptions, Message: "openai codex oauth: credential store is required"}
	}
	if provider == "" {
		return sigma.StoredCredential{}, &sigma.Error{Code: sigma.ErrorInvalidOptions, Message: "openai codex oauth: provider id is required"}
	}
	stored, ok, err := store.ModifyCredential(ctx, provider, func(current sigma.StoredCredential, currentOK bool) (sigma.StoredCredential, bool, error) {
		if !currentOK {
			current.Source = "credential-store:" + string(provider)
		}
		return storedCodexOAuthCredential(provider, credentials, current), true, nil
	})
	if err != nil {
		return sigma.StoredCredential{}, fmt.Errorf("openai codex oauth: store credentials: %w", err)
	}
	if !ok {
		return sigma.StoredCredential{}, &sigma.Error{Code: sigma.ErrorInvalidOptions, Message: "openai codex oauth: credential store did not persist credentials"}
	}
	return stored, nil
}

func storedCodexOAuthCredential(provider sigma.ProviderID, credentials CodexOAuthCredentials, previous sigma.StoredCredential) sigma.StoredCredential {
	source := previous.Source
	if source == "" {
		source = "credential-store:" + string(provider)
	}
	metadata := copyAnyMap(previous.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	if credentials.AccountID != "" {
		metadata[codexOAuthCredentialAccountID] = credentials.AccountID
		metadata[codexOAuthCredentialChatGPTAcctID] = credentials.AccountID
	}
	return sigma.StoredCredential{
		Type:         sigma.CredentialTypeOAuthToken,
		Value:        credentials.AccessToken,
		RefreshToken: credentials.RefreshToken,
		Expiry:       credentials.Expiry,
		Source:       source,
		ProviderEnv:  copyStoredStringMap(previous.ProviderEnv),
		Metadata:     metadata,
	}
}

func copyStoredStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func storedStringMetadata(metadata map[string]any, key string) string {
	if value, ok := metadata[key].(string); ok {
		return value
	}
	return ""
}

func (p *codexOAuthTokenProvider) Token(ctx context.Context, model sigma.Model, _ sigma.Options) (sigma.Credential, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.credentials.AccessToken == "" {
		return sigma.Credential{}, &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"openai-codex-oauth"},
		}
	}

	if err := p.refreshIfNeeded(ctx, model); err != nil {
		return sigma.Credential{}, err
	}

	accountID := p.credentials.AccountID
	if accountID == "" {
		var err error
		accountID, err = codexAccountIDFromToken(p.credentials.AccessToken)
		if err != nil {
			return sigma.Credential{}, err
		}
		p.credentials.AccountID = accountID
	}

	return sigma.Credential{
		Type:   sigma.CredentialTypeOAuthToken,
		Value:  p.credentials.AccessToken,
		Expiry: p.credentials.Expiry,
		Source: "openai-codex-oauth",
		Metadata: map[string]any{
			codexOAuthCredentialAccountID:     accountID,
			codexOAuthCredentialChatGPTAcctID: accountID,
		},
	}, nil
}

func (p *codexOAuthTokenProvider) refreshIfNeeded(ctx context.Context, model sigma.Model) error {
	if !p.shouldRefresh() {
		return nil
	}
	if p.credentials.RefreshToken == "" {
		return &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"openai-codex-refresh-token"},
		}
	}
	refreshed, err := RefreshOpenAICodexToken(ctx, p.credentials.RefreshToken, CodexOAuthTokenProviderOptions{
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
		return errors.New("openai codex oauth: refresh callback failed")
	}
	return nil
}

func (p *codexOAuthTokenProvider) shouldRefresh() bool {
	if p.credentials.Expiry.IsZero() {
		return false
	}
	return !p.now().Add(p.refreshBefore).Before(p.credentials.Expiry)
}

func newOpenAICodexBrowserAuthorizationFlow(state string, redirectURI string) (codexBrowserAuthorizationFlow, error) {
	verifier, challenge, err := newPKCEPair()
	if err != nil {
		return codexBrowserAuthorizationFlow{}, err
	}
	authURL, err := url.Parse(codexOAuthAuthorizeURL)
	if err != nil {
		return codexBrowserAuthorizationFlow{}, fmt.Errorf("openai codex oauth: parse authorize URL: %w", err)
	}
	values := authURL.Query()
	values.Set("response_type", "code")
	values.Set("client_id", codexOAuthClientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("scope", codexOAuthBrowserScope)
	values.Set("code_challenge", challenge)
	values.Set("code_challenge_method", "S256")
	values.Set("state", state)
	values.Set("id_token_add_organizations", "true")
	values.Set("codex_cli_simplified_flow", "true")
	values.Set("originator", "sigma")
	authURL.RawQuery = values.Encode()
	return codexBrowserAuthorizationFlow{
		verifier: verifier,
		state:    state,
		url:      authURL.String(),
	}, nil
}

func startOpenAICodexBrowserCallbackServer(state string) (*codexBrowserCallbackServer, error) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", codexOAuthBrowserListenAddr)
	if err != nil {
		return nil, fmt.Errorf("openai codex oauth: start callback server: %w", err)
	}
	result := make(chan codexBrowserCallbackResult, 1)
	serverInfo := &codexBrowserCallbackServer{
		done:        result,
		redirectURI: codexBrowserRedirectURI(listener),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(codexOAuthBrowserCallbackPath, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("state") != state {
			writeCodexOAuthHTML(w, http.StatusBadRequest, "Authentication failed", "State mismatch.")
			serverInfo.finish(codexBrowserCallbackResult{err: fmt.Errorf("openai codex oauth: state mismatch")})
			return
		}
		code := req.URL.Query().Get("code")
		if code == "" {
			writeCodexOAuthHTML(w, http.StatusBadRequest, "Authentication failed", "Missing authorization code.")
			serverInfo.finish(codexBrowserCallbackResult{err: fmt.Errorf("openai codex oauth: missing authorization code")})
			return
		}
		writeCodexOAuthHTML(w, http.StatusOK, "Authentication successful", "OpenAI authentication completed. You can close this window.")
		serverInfo.finish(codexBrowserCallbackResult{code: code})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeCodexOAuthHTML(w, http.StatusNotFound, "Authentication failed", "Callback route not found.")
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverInfo.server = server
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverInfo.finish(codexBrowserCallbackResult{err: fmt.Errorf("openai codex oauth: callback server failed: %w", err)})
		}
	}()
	return serverInfo, nil
}

func (s *codexBrowserCallbackServer) finish(result codexBrowserCallbackResult) {
	select {
	case s.done <- result:
	default:
	}
}

func (s *codexBrowserCallbackServer) close() {
	if s == nil || s.server == nil {
		return
	}
	_ = s.server.Close()
}

func waitOpenAICodexBrowserAuthorizationCode(
	ctx context.Context,
	state string,
	server *codexBrowserCallbackServer,
	manual func(context.Context, CodexBrowserManualPrompt) (string, error),
) (string, error) {
	manualResult := make(chan codexBrowserCallbackResult, 1)
	if manual != nil {
		go func() {
			input, err := manual(ctx, CodexBrowserManualPrompt{
				Message: "Paste the authorization code or full redirect URL:",
			})
			if err != nil {
				manualResult <- codexBrowserCallbackResult{err: err}
				return
			}
			parsed := parseCodexAuthorizationInput(input)
			if parsed.state != "" && parsed.state != state {
				manualResult <- codexBrowserCallbackResult{err: fmt.Errorf("openai codex oauth: state mismatch")}
				return
			}
			if parsed.code == "" {
				manualResult <- codexBrowserCallbackResult{err: fmt.Errorf("openai codex oauth: missing authorization code")}
				return
			}
			manualResult <- codexBrowserCallbackResult{code: parsed.code}
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

func parseCodexAuthorizationInput(input string) codexAuthorizationInput {
	value := strings.TrimSpace(input)
	if value == "" {
		return codexAuthorizationInput{}
	}
	if parsed, err := url.Parse(value); err == nil && (parsed.Scheme != "" || parsed.Host != "") {
		return codexAuthorizationInput{
			code:  parsed.Query().Get("code"),
			state: parsed.Query().Get("state"),
		}
	}
	if strings.Contains(value, "#") && !strings.Contains(value, "://") {
		parts := strings.SplitN(value, "#", 2)
		return codexAuthorizationInput{code: parts[0], state: parts[1]}
	}
	value = strings.TrimPrefix(value, "?")
	if strings.Contains(value, "code=") {
		values, err := url.ParseQuery(value)
		if err == nil {
			return codexAuthorizationInput{
				code:  values.Get("code"),
				state: values.Get("state"),
			}
		}
	}
	return codexAuthorizationInput{code: value}
}

func codexBrowserRedirectURI(listener net.Listener) string {
	if codexOAuthBrowserListenAddr == "127.0.0.1:1455" {
		return codexOAuthBrowserDefaultRedirect
	}
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return codexOAuthBrowserDefaultRedirect
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   codexOAuthBrowserCallbackPath,
	}).String()
}

func newPKCEPair() (string, string, error) {
	verifier, err := randomBase64URL(32)
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

func randomBase64URL(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("openai codex oauth: generate random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func writeCodexOAuthHTML(w http.ResponseWriter, status int, heading string, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>%s</title></head>
<body><main><h1>%s</h1><p>%s</p></main></body>
</html>`, html.EscapeString(heading), html.EscapeString(heading), html.EscapeString(message))
}

func startOpenAICodexDeviceAuth(ctx context.Context, client *http.Client) (codexDeviceAuthInfo, error) {
	body, err := json.Marshal(map[string]string{"client_id": codexOAuthClientID})
	if err != nil {
		return codexDeviceAuthInfo{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexOAuthDeviceUserCodeURL, bytes.NewReader(body))
	if err != nil {
		return codexDeviceAuthInfo{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := codexHTTPClient(client).Do(req)
	if err != nil {
		return codexDeviceAuthInfo{}, contextOrError(ctx, fmt.Errorf("openai codex oauth: request device code: %w", err))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return codexDeviceAuthInfo{}, contextOrError(ctx, fmt.Errorf("openai codex oauth: read device code response: %w", err))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return codexDeviceAuthInfo{}, fmt.Errorf("openai codex oauth: device code request failed (%d): %s", resp.StatusCode, redact.Preview(string(data), 1024))
	}

	var decoded struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		Interval     any    `json:"interval"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return codexDeviceAuthInfo{}, fmt.Errorf("openai codex oauth: decode device code response: %w", err)
	}
	interval, err := codexPollInterval(decoded.Interval)
	if err != nil {
		return codexDeviceAuthInfo{}, err
	}
	if decoded.DeviceAuthID == "" || decoded.UserCode == "" {
		return codexDeviceAuthInfo{}, fmt.Errorf("openai codex oauth: device code response missing fields")
	}
	return codexDeviceAuthInfo{
		deviceAuthID: decoded.DeviceAuthID,
		userCode:     decoded.UserCode,
		interval:     interval,
	}, nil
}

func pollOpenAICodexDeviceAuth(ctx context.Context, client *http.Client, device codexDeviceAuthInfo) (codexDeviceTokenSuccess, error) {
	deadline := time.Now().Add(codexOAuthDeviceTimeout)
	interval := device.interval
	if interval <= 0 {
		interval = codexOAuthDefaultPollInterval
	}
	if interval < codexOAuthMinimumPollInterval {
		interval = codexOAuthMinimumPollInterval
	}
	var slowDowns int

	for !time.Now().After(deadline) {
		result, err := pollOpenAICodexDeviceAuthOnce(ctx, client, device)
		if err != nil {
			return codexDeviceTokenSuccess{}, err
		}
		switch result.status {
		case "complete":
			return result.value, nil
		case "slow_down":
			slowDowns++
			interval += codexOAuthSlowDownPollIncrement
		case "pending":
		default:
			return codexDeviceTokenSuccess{}, fmt.Errorf("openai codex oauth: device auth failed")
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if err := sleepContext(ctx, minDuration(interval, remaining)); err != nil {
			return codexDeviceTokenSuccess{}, err
		}
	}
	if slowDowns > 0 {
		return codexDeviceTokenSuccess{}, fmt.Errorf("openai codex oauth: device flow timed out after slow_down responses")
	}
	return codexDeviceTokenSuccess{}, fmt.Errorf("openai codex oauth: device flow timed out")
}

type codexDevicePollResult struct {
	status string
	value  codexDeviceTokenSuccess
}

func pollOpenAICodexDeviceAuthOnce(ctx context.Context, client *http.Client, device codexDeviceAuthInfo) (codexDevicePollResult, error) {
	body, err := json.Marshal(map[string]string{
		"device_auth_id": device.deviceAuthID,
		"user_code":      device.userCode,
	})
	if err != nil {
		return codexDevicePollResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexOAuthDeviceTokenURL, bytes.NewReader(body))
	if err != nil {
		return codexDevicePollResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := codexHTTPClient(client).Do(req)
	if err != nil {
		return codexDevicePollResult{}, contextOrError(ctx, fmt.Errorf("openai codex oauth: poll device auth: %w", err))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return codexDevicePollResult{}, contextOrError(ctx, fmt.Errorf("openai codex oauth: read device auth response: %w", err))
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		var decoded struct {
			AuthorizationCode string `json:"authorization_code"`
			CodeVerifier      string `json:"code_verifier"`
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return codexDevicePollResult{}, fmt.Errorf("openai codex oauth: decode device auth response: %w", err)
		}
		if decoded.AuthorizationCode == "" || decoded.CodeVerifier == "" {
			return codexDevicePollResult{}, fmt.Errorf("openai codex oauth: device auth response missing fields")
		}
		return codexDevicePollResult{
			status: "complete",
			value: codexDeviceTokenSuccess{
				authorizationCode: decoded.AuthorizationCode,
				codeVerifier:      decoded.CodeVerifier,
			},
		}, nil
	}

	code := codexOAuthErrorCode(data)
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || code == "deviceauth_authorization_pending" {
		return codexDevicePollResult{status: "pending"}, nil
	}
	if code == "slow_down" {
		return codexDevicePollResult{status: "slow_down"}, nil
	}
	return codexDevicePollResult{}, fmt.Errorf("openai codex oauth: device auth failed (%d): %s", resp.StatusCode, redact.Preview(string(data), 1024))
}

func exchangeOpenAICodexAuthorizationCode(ctx context.Context, client *http.Client, code string, verifier string, redirectURI string) (codexOAuthToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", codexOAuthClientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirectURI)
	return postOpenAICodexToken(ctx, client, form, "exchange")
}

func refreshOpenAICodexAccessToken(ctx context.Context, client *http.Client, refreshToken string) (codexOAuthToken, error) {
	if refreshToken == "" {
		return codexOAuthToken{}, &sigma.CredentialUnavailableError{
			Sources: []string{"openai-codex-refresh-token"},
		}
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", codexOAuthClientID)
	return postOpenAICodexToken(ctx, client, form, "refresh")
}

func postOpenAICodexToken(ctx context.Context, client *http.Client, form url.Values, operation string) (codexOAuthToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return codexOAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := codexHTTPClient(client).Do(req)
	if err != nil {
		return codexOAuthToken{}, contextOrError(ctx, fmt.Errorf("openai codex oauth: token %s: %w", operation, err))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return codexOAuthToken{}, contextOrError(ctx, fmt.Errorf("openai codex oauth: read token %s response: %w", operation, err))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return codexOAuthToken{}, fmt.Errorf("openai codex oauth: token %s failed (%d): %s", operation, resp.StatusCode, redact.Preview(string(data), 1024))
	}

	var decoded struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return codexOAuthToken{}, fmt.Errorf("openai codex oauth: decode token %s response: %w", operation, err)
	}
	if decoded.AccessToken == "" || decoded.RefreshToken == "" || decoded.ExpiresIn <= 0 {
		return codexOAuthToken{}, fmt.Errorf("openai codex oauth: token %s response missing fields", operation)
	}
	return codexOAuthToken{
		access:  decoded.AccessToken,
		refresh: decoded.RefreshToken,
		expiry:  time.Now().Add(time.Duration(decoded.ExpiresIn) * time.Second),
	}, nil
}

func codexCredentialsFromToken(token codexOAuthToken) (CodexOAuthCredentials, error) {
	accountID, err := codexAccountIDFromToken(token.access)
	if err != nil {
		return CodexOAuthCredentials{}, err
	}
	return CodexOAuthCredentials{
		AccessToken:  token.access,
		RefreshToken: token.refresh,
		Expiry:       token.expiry,
		AccountID:    accountID,
	}, nil
}

func codexAccountIDFromToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("openai codex oauth: failed to extract account id from token")
	}
	payload, err := decodeJWTPayload(parts[1])
	if err != nil {
		return "", fmt.Errorf("openai codex oauth: failed to extract account id from token")
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("openai codex oauth: failed to extract account id from token")
	}
	claim, ok := decoded[codexOAuthJWTClaimPath].(map[string]any)
	if !ok {
		return "", fmt.Errorf("openai codex oauth: failed to extract account id from token")
	}
	accountID, ok := claim[codexOAuthCredentialChatGPTAcctID].(string)
	if !ok || accountID == "" {
		return "", fmt.Errorf("openai codex oauth: failed to extract account id from token")
	}
	return accountID, nil
}

func decodeJWTPayload(payload string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(payload)
		if err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("invalid jwt payload")
}

func codexPollInterval(value any) (time.Duration, error) {
	switch typed := value.(type) {
	case nil:
		return codexOAuthDefaultPollInterval, nil
	case float64:
		if typed < 0 {
			return 0, fmt.Errorf("openai codex oauth: invalid device code interval")
		}
		return time.Duration(typed * float64(time.Second)), nil
	case string:
		seconds, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil || seconds < 0 {
			return 0, fmt.Errorf("openai codex oauth: invalid device code interval")
		}
		return time.Duration(seconds * float64(time.Second)), nil
	default:
		return 0, fmt.Errorf("openai codex oauth: invalid device code interval")
	}
}

func codexOAuthErrorCode(data []byte) string {
	var decoded struct {
		Error any `json:"error"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return ""
	}
	switch typed := decoded.Error.(type) {
	case string:
		return typed
	case map[string]any:
		code, _ := typed["code"].(string)
		return code
	default:
		return ""
	}
}

func codexHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func contextOrError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
