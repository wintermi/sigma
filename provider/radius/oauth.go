// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package radius

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
	"math"
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
	radiusOAuthDefaultPollInterval  = 5 * time.Second
	radiusOAuthSlowDownIncrement    = 5 * time.Second
	radiusOAuthDefaultRefreshBefore = time.Minute
	radiusOAuthMaxBodyBytes         = 1 << 20
	radiusOAuthDeviceCodeGrantType  = "urn:ietf:params:oauth:grant-type:device_code"
)

// RadiusOAuthClientConfig identifies a caller-owned Radius OAuth client.
// GatewayURL, ClientID, and Scopes are required. Browser login also requires a
// registered loopback HTTP RedirectURI.
type RadiusOAuthClientConfig struct {
	GatewayURL  string
	ClientID    string
	Scopes      []string
	RedirectURI string
}

// RadiusOAuthCredentials carries Radius OAuth tokens. Callers own persistence;
// Sigma never stores these credentials itself.
type RadiusOAuthCredentials struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// RadiusDeviceCodeInfo reports device login details for callers to present.
type RadiusDeviceCodeInfo struct {
	UserCode        string
	VerificationURI string
	ExpiresIn       time.Duration
	Interval        time.Duration
}

// RadiusDeviceCodeLoginOptions configures Radius device-code login.
type RadiusDeviceCodeLoginOptions struct {
	Client       RadiusOAuthClientConfig
	HTTPClient   *http.Client
	OnDeviceCode func(RadiusDeviceCodeInfo)
}

// RadiusBrowserAuthInfo reports the authorization URL for browser login.
type RadiusBrowserAuthInfo struct {
	URL          string
	Instructions string
}

// RadiusBrowserManualPrompt describes the optional manual browser-login fallback.
type RadiusBrowserManualPrompt struct {
	Message string
}

// RadiusBrowserLoginOptions configures Radius browser callback login.
type RadiusBrowserLoginOptions struct {
	Client       RadiusOAuthClientConfig
	HTTPClient   *http.Client
	OnAuth       func(RadiusBrowserAuthInfo)
	OnManualCode func(context.Context, RadiusBrowserManualPrompt) (string, error)
}

// RadiusOAuthTokenProviderOptions configures Radius OAuth token resolution.
type RadiusOAuthTokenProviderOptions struct {
	Client        RadiusOAuthClientConfig
	HTTPClient    *http.Client
	Now           func() time.Time
	RefreshBefore time.Duration
	OnRefresh     func(context.Context, RadiusOAuthCredentials) error
}

// RadiusOAuthTokenProvider resolves and refreshes caller-owned Radius OAuth credentials.
type RadiusOAuthTokenProvider struct {
	client        RadiusOAuthClientConfig
	httpClient    *http.Client
	now           func() time.Time
	refreshBefore time.Duration
	onRefresh     func(context.Context, RadiusOAuthCredentials) error

	mu          sync.Mutex
	credentials RadiusOAuthCredentials
}

// ProviderAuth returns Radius API-key and OAuth auth descriptors.
func ProviderAuth(opts RadiusOAuthTokenProviderOptions) sigma.ProviderAuth {
	return sigma.ProviderAuth{
		APIKey: sigma.EnvironmentAPIKeyAuth("Radius API key", "RADIUS_API_KEY"),
		OAuth: &sigma.OAuthAuth{
			Name:          "Radius OAuth",
			RefreshBefore: opts.RefreshBefore,
			Refresh: func(ctx context.Context, stored sigma.StoredCredential) (sigma.StoredCredential, error) {
				refreshed, err := RefreshRadiusToken(ctx, stored.RefreshToken, opts)
				if err != nil {
					return sigma.StoredCredential{}, err
				}
				return storedRadiusOAuthCredential(refreshed, stored), nil
			},
			Credential: func(_ context.Context, _ sigma.Model, _ sigma.Options, stored sigma.StoredCredential) (sigma.Credential, error) {
				if stored.Value == "" {
					return sigma.Credential{}, &sigma.CredentialUnavailableError{Sources: []string{"radius-oauth"}}
				}
				source := stored.Source
				if source == "" {
					source = "credential-store:" + string(sigma.ProviderRadius)
				}
				return sigma.Credential{
					Type:     sigma.CredentialTypeOAuthToken,
					Value:    stored.Value,
					Expiry:   stored.Expiry,
					Source:   source,
					Metadata: copyRadiusOAuthMetadata(stored.Metadata),
				}, nil
			},
		},
	}
}

// RegisterAuth registers Radius API-key and OAuth auth descriptors on registry.
func RegisterAuth(registry *sigma.Registry, opts RadiusOAuthTokenProviderOptions, registerOpts ...sigma.RegisterOption) error {
	if _, err := normalizeRadiusOAuthClient(opts.Client, false); err != nil {
		return err
	}
	registerOpts = append([]sigma.RegisterOption{sigma.WithOverride()}, registerOpts...)
	if err := sigma.RegisterProviderAuth(registry, sigma.ProviderRadius, ProviderAuth(opts), registerOpts...); err != nil {
		return fmt.Errorf("radius oauth: register provider auth: %w", err)
	}
	return nil
}

// RegisterDefaultAuth registers Radius API-key and OAuth auth descriptors on the default registry.
func RegisterDefaultAuth(opts RadiusOAuthTokenProviderOptions, registerOpts ...sigma.RegisterOption) error {
	if _, err := normalizeRadiusOAuthClient(opts.Client, false); err != nil {
		return err
	}
	registerOpts = append([]sigma.RegisterOption{sigma.WithOverride()}, registerOpts...)
	if err := sigma.RegisterDefaultProviderAuth(sigma.ProviderRadius, ProviderAuth(opts), registerOpts...); err != nil {
		return fmt.Errorf("radius oauth: register default provider auth: %w", err)
	}
	return nil
}

// LoginRadiusDeviceCode runs the Radius device-code OAuth flow and returns
// credentials for caller-managed persistence.
func LoginRadiusDeviceCode(ctx context.Context, opts RadiusDeviceCodeLoginOptions) (RadiusOAuthCredentials, error) {
	client, err := normalizeRadiusOAuthClient(opts.Client, false)
	if err != nil {
		return RadiusOAuthCredentials{}, err
	}
	device, err := requestRadiusDeviceCode(ctx, opts.HTTPClient, client)
	if err != nil {
		return RadiusOAuthCredentials{}, err
	}
	if opts.OnDeviceCode != nil {
		opts.OnDeviceCode(RadiusDeviceCodeInfo{
			UserCode:        device.userCode,
			VerificationURI: device.verificationURI,
			ExpiresIn:       device.expiresIn,
			Interval:        radiusOAuthPollInterval(device.interval),
		})
	}
	return pollRadiusDeviceCode(ctx, opts.HTTPClient, client, device)
}

// LoginRadiusBrowser runs the Radius PKCE browser callback flow and returns
// credentials for caller-managed persistence.
func LoginRadiusBrowser(ctx context.Context, opts RadiusBrowserLoginOptions) (RadiusOAuthCredentials, error) {
	client, err := normalizeRadiusOAuthClient(opts.Client, true)
	if err != nil {
		return RadiusOAuthCredentials{}, err
	}
	authorizationEndpoint, err := discoverRadiusAuthorizationEndpoint(ctx, opts.HTTPClient, client.GatewayURL)
	if err != nil {
		return RadiusOAuthCredentials{}, err
	}
	verifier, challenge, err := newRadiusPKCEPair()
	if err != nil {
		return RadiusOAuthCredentials{}, err
	}
	state, err := randomRadiusBase64URL(32)
	if err != nil {
		return RadiusOAuthCredentials{}, fmt.Errorf("radius oauth: generate state: %w", err)
	}
	server, err := startRadiusBrowserCallbackServer(client.RedirectURI, state)
	if err != nil {
		return RadiusOAuthCredentials{}, err
	}
	defer server.close()

	authURL, err := radiusAuthorizationURL(authorizationEndpoint, client, challenge, state)
	if err != nil {
		return RadiusOAuthCredentials{}, err
	}
	if opts.OnAuth != nil {
		opts.OnAuth(RadiusBrowserAuthInfo{
			URL:          authURL,
			Instructions: "Open the URL in a browser and complete login. If the browser is on another machine, paste the final redirect URL or authorization code.",
		})
	}
	code, err := waitRadiusBrowserAuthorizationCode(ctx, state, server, opts.OnManualCode)
	if err != nil {
		return RadiusOAuthCredentials{}, err
	}
	return exchangeRadiusAuthorizationCode(ctx, opts.HTTPClient, client, code, verifier)
}

// RefreshRadiusToken refreshes Radius OAuth credentials from a refresh token.
func RefreshRadiusToken(ctx context.Context, refreshToken string, opts RadiusOAuthTokenProviderOptions) (RadiusOAuthCredentials, error) {
	client, err := normalizeRadiusOAuthClient(opts.Client, false)
	if err != nil {
		return RadiusOAuthCredentials{}, err
	}
	if strings.TrimSpace(refreshToken) == "" {
		return RadiusOAuthCredentials{}, &sigma.CredentialUnavailableError{Sources: []string{"radius-refresh-token"}}
	}
	return requestRadiusOAuthToken(ctx, opts.HTTPClient, client, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {client.ClientID},
		"refresh_token": {refreshToken},
	}, refreshToken, false)
}

// NewRadiusOAuthTokenProvider adapts caller-managed Radius OAuth credentials to
// Sigma's OAuthTokenProvider interface. Refreshed credentials are retained in
// memory and passed to OnRefresh for caller persistence.
func NewRadiusOAuthTokenProvider(credentials RadiusOAuthCredentials, opts RadiusOAuthTokenProviderOptions) *RadiusOAuthTokenProvider {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	refreshBefore := opts.RefreshBefore
	if refreshBefore == 0 {
		refreshBefore = radiusOAuthDefaultRefreshBefore
	}
	return &RadiusOAuthTokenProvider{
		client:        opts.Client,
		httpClient:    opts.HTTPClient,
		now:           now,
		refreshBefore: refreshBefore,
		onRefresh:     opts.OnRefresh,
		credentials:   credentials,
	}
}

// Token implements sigma.OAuthTokenProvider.
func (p *RadiusOAuthTokenProvider) Token(ctx context.Context, model sigma.Model, _ sigma.Options) (sigma.Credential, error) {
	if p == nil {
		return sigma.Credential{}, &sigma.CredentialUnavailableError{Provider: model.Provider, Model: model.ID, Sources: []string{"radius-oauth"}}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.credentials.AccessToken == "" {
		return sigma.Credential{}, &sigma.CredentialUnavailableError{Provider: model.Provider, Model: model.ID, Sources: []string{"radius-oauth"}}
	}
	if err := p.refreshIfNeeded(ctx, model); err != nil {
		return sigma.Credential{}, err
	}
	return sigma.Credential{
		Type:   sigma.CredentialTypeOAuthToken,
		Value:  p.credentials.AccessToken,
		Expiry: p.credentials.Expiry,
		Source: "radius-oauth",
	}, nil
}

func (p *RadiusOAuthTokenProvider) shouldRefresh() bool {
	return !p.credentials.Expiry.IsZero() && !p.now().Add(p.refreshBefore).Before(p.credentials.Expiry)
}

func (p *RadiusOAuthTokenProvider) refreshIfNeeded(ctx context.Context, model sigma.Model) error {
	if !p.shouldRefresh() {
		return nil
	}
	if p.credentials.RefreshToken == "" {
		return &sigma.CredentialUnavailableError{Provider: model.Provider, Model: model.ID, Sources: []string{"radius-refresh-token"}}
	}
	refreshed, err := RefreshRadiusToken(ctx, p.credentials.RefreshToken, RadiusOAuthTokenProviderOptions{
		Client:     p.client,
		HTTPClient: p.httpClient,
	})
	if err != nil {
		return err
	}
	p.credentials = refreshed
	if p.onRefresh == nil {
		return nil
	}
	if err := p.onRefresh(ctx, refreshed); err != nil {
		return errors.New("radius oauth: refresh callback failed")
	}
	return nil
}

type radiusOAuthDeviceCode struct {
	deviceCode      string
	userCode        string
	verificationURI string
	interval        time.Duration
	expiresIn       time.Duration
}

type radiusOAuthTokenResponse struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresIn    float64 `json:"expires_in"`
}

type radiusOAuthDeviceCodeResponse struct {
	DeviceCode              string   `json:"device_code"`
	UserCode                string   `json:"user_code"`
	VerificationURI         string   `json:"verification_uri"`
	VerificationURIComplete string   `json:"verification_uri_complete"`
	ExpiresIn               float64  `json:"expires_in"`
	Interval                *float64 `json:"interval"`
}

type radiusOAuthDiscoveryResponse struct {
	AuthorizationEndpoint string `json:"authorizationEndpoint"`
}

type radiusOAuthResponseError struct {
	operation string
	status    int
	code      string
}

func (err *radiusOAuthResponseError) Error() string {
	if err.code != "" {
		return fmt.Sprintf("radius oauth: %s failed: %s", err.operation, err.code)
	}
	return fmt.Sprintf("radius oauth: %s failed: status %d", err.operation, err.status)
}

func requestRadiusDeviceCode(ctx context.Context, httpClient *http.Client, client RadiusOAuthClientConfig) (radiusOAuthDeviceCode, error) {
	endpoint, err := endpoint(client.GatewayURL, "/v1/oauth/device")
	if err != nil {
		return radiusOAuthDeviceCode{}, fmt.Errorf("radius oauth: device endpoint: %w", err)
	}
	body, err := postRadiusOAuthForm(ctx, httpClient, endpoint, url.Values{
		"client_id": {client.ClientID},
		"scope":     {strings.Join(client.Scopes, " ")},
	})
	if err != nil {
		return radiusOAuthDeviceCode{}, err
	}
	var decoded radiusOAuthDeviceCodeResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return radiusOAuthDeviceCode{}, fmt.Errorf("radius oauth: decode device authorization response: %w", err)
	}
	expiresIn, ok := radiusOAuthDuration(decoded.ExpiresIn)
	verificationURI := decoded.VerificationURI
	if decoded.VerificationURIComplete != "" {
		verificationURI = decoded.VerificationURIComplete
	}
	if decoded.DeviceCode == "" || decoded.UserCode == "" || verificationURI == "" || !ok {
		return radiusOAuthDeviceCode{}, errors.New("radius oauth: device authorization response missing fields")
	}
	verificationURI, err = safeRadiusVerificationURI(verificationURI)
	if err != nil {
		return radiusOAuthDeviceCode{}, err
	}
	interval := radiusOAuthDefaultPollInterval
	if decoded.Interval != nil {
		if value, valid := radiusOAuthDuration(*decoded.Interval); valid {
			interval = value
		} else {
			return radiusOAuthDeviceCode{}, errors.New("radius oauth: device authorization response has invalid interval")
		}
	}
	return radiusOAuthDeviceCode{
		deviceCode:      decoded.DeviceCode,
		userCode:        decoded.UserCode,
		verificationURI: verificationURI,
		interval:        interval,
		expiresIn:       expiresIn,
	}, nil
}

func pollRadiusDeviceCode(ctx context.Context, httpClient *http.Client, client RadiusOAuthClientConfig, device radiusOAuthDeviceCode) (RadiusOAuthCredentials, error) {
	return pollRadiusDeviceCodeWithWait(ctx, httpClient, client, device, time.Now, radiusOAuthSleepContext)
}

func pollRadiusDeviceCodeWithWait(
	ctx context.Context,
	httpClient *http.Client,
	client RadiusOAuthClientConfig,
	device radiusOAuthDeviceCode,
	now func() time.Time,
	wait func(context.Context, time.Duration) error,
) (RadiusOAuthCredentials, error) {
	interval := radiusOAuthPollInterval(device.interval)
	deadline := now().Add(device.expiresIn)
	if err := wait(ctx, interval); err != nil {
		return RadiusOAuthCredentials{}, err
	}
	for {
		credentials, pending, slowDown, err := pollRadiusDeviceCodeOnce(ctx, httpClient, client, device.deviceCode)
		if err != nil {
			return RadiusOAuthCredentials{}, err
		}
		if !pending && !slowDown {
			return credentials, nil
		}
		if slowDown {
			interval += radiusOAuthSlowDownIncrement
		}
		if !now().Add(interval).Before(deadline) {
			return RadiusOAuthCredentials{}, errors.New("radius oauth: device code expired")
		}
		if err := wait(ctx, interval); err != nil {
			return RadiusOAuthCredentials{}, err
		}
	}
}

func pollRadiusDeviceCodeOnce(ctx context.Context, httpClient *http.Client, client RadiusOAuthClientConfig, deviceCode string) (RadiusOAuthCredentials, bool, bool, error) {
	credentials, err := requestRadiusOAuthToken(ctx, httpClient, client, url.Values{
		"grant_type":  {radiusOAuthDeviceCodeGrantType},
		"client_id":   {client.ClientID},
		"device_code": {deviceCode},
	}, "", true)
	if err == nil {
		return credentials, false, false, nil
	}
	var responseErr *radiusOAuthResponseError
	if !errors.As(err, &responseErr) {
		return RadiusOAuthCredentials{}, false, false, err
	}
	switch responseErr.code {
	case "authorization_pending":
		return RadiusOAuthCredentials{}, true, false, nil
	case "slow_down":
		return RadiusOAuthCredentials{}, false, true, nil
	case "expired_token":
		return RadiusOAuthCredentials{}, false, false, errors.New("radius oauth: device code expired")
	case "access_denied":
		return RadiusOAuthCredentials{}, false, false, errors.New("radius oauth: device authorization denied")
	default:
		return RadiusOAuthCredentials{}, false, false, err
	}
}

func exchangeRadiusAuthorizationCode(ctx context.Context, httpClient *http.Client, client RadiusOAuthClientConfig, code, verifier string) (RadiusOAuthCredentials, error) {
	return requestRadiusOAuthToken(ctx, httpClient, client, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {client.ClientID},
		"redirect_uri":  {client.RedirectURI},
		"code":          {code},
		"code_verifier": {verifier},
	}, "", true)
}

func requestRadiusOAuthToken(ctx context.Context, httpClient *http.Client, client RadiusOAuthClientConfig, values url.Values, refreshFallback string, requireRefresh bool) (RadiusOAuthCredentials, error) {
	endpoint, err := endpoint(client.GatewayURL, "/v1/oauth/token")
	if err != nil {
		return RadiusOAuthCredentials{}, fmt.Errorf("radius oauth: token endpoint: %w", err)
	}
	body, err := postRadiusOAuthForm(ctx, httpClient, endpoint, values)
	if err != nil {
		return RadiusOAuthCredentials{}, err
	}
	var decoded radiusOAuthTokenResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return RadiusOAuthCredentials{}, fmt.Errorf("radius oauth: decode token response: %w", err)
	}
	expiresIn, ok := radiusOAuthDuration(decoded.ExpiresIn)
	refreshToken := decoded.RefreshToken
	if refreshToken == "" {
		refreshToken = refreshFallback
	}
	if decoded.AccessToken == "" || !ok || (requireRefresh && refreshToken == "") {
		return RadiusOAuthCredentials{}, errors.New("radius oauth: token response missing fields")
	}
	return RadiusOAuthCredentials{
		AccessToken:  decoded.AccessToken,
		RefreshToken: refreshToken,
		Expiry:       time.Now().Add(expiresIn),
	}, nil
}

func discoverRadiusAuthorizationEndpoint(ctx context.Context, httpClient *http.Client, gatewayURL string) (string, error) {
	endpoint, err := endpoint(gatewayURL, "/v1/oauth")
	if err != nil {
		return "", fmt.Errorf("radius oauth: discovery endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("radius oauth: create discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := radiusOAuthHTTPClient(httpClient).Do(req)
	if err != nil {
		return "", fmt.Errorf("radius oauth: discover authorization endpoint: %w", err)
	}
	defer resp.Body.Close()
	body, err := readRadiusOAuthBody(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", radiusOAuthErrorFromBody("discovery", resp.StatusCode, body)
	}
	var decoded radiusOAuthDiscoveryResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", fmt.Errorf("radius oauth: decode discovery response: %w", err)
	}
	return safeRadiusAuthorizationEndpoint(decoded.AuthorizationEndpoint)
}

func postRadiusOAuthForm(ctx context.Context, httpClient *http.Client, endpoint string, values url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(values.Encode()))
	if err != nil {
		return nil, fmt.Errorf("radius oauth: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := radiusOAuthHTTPClient(httpClient).Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("radius oauth: request: %w", err)
	}
	defer resp.Body.Close()
	body, err := readRadiusOAuthBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, radiusOAuthErrorFromBody("request", resp.StatusCode, body)
	}
	return body, nil
}

func readRadiusOAuthBody(body io.Reader) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(body, radiusOAuthMaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("radius oauth: read response: %w", err)
	}
	if len(content) > radiusOAuthMaxBodyBytes {
		return nil, errors.New("radius oauth: response body exceeds limit")
	}
	return content, nil
}

func radiusOAuthErrorFromBody(operation string, status int, body []byte) error {
	var decoded struct {
		Code string `json:"error"`
	}
	_ = json.Unmarshal(body, &decoded)
	return &radiusOAuthResponseError{operation: operation, status: status, code: decoded.Code}
}

func normalizeRadiusOAuthClient(client RadiusOAuthClientConfig, requireRedirect bool) (RadiusOAuthClientConfig, error) {
	client.ClientID = strings.TrimSpace(client.ClientID)
	if client.ClientID == "" {
		return RadiusOAuthClientConfig{}, errors.New("radius oauth: client ID is required")
	}
	client.Scopes = compactRadiusScopes(client.Scopes)
	if len(client.Scopes) == 0 {
		return RadiusOAuthClientConfig{}, errors.New("radius oauth: at least one scope is required")
	}
	gatewayURL, err := validRadiusOAuthGatewayURL(client.GatewayURL)
	if err != nil {
		return RadiusOAuthClientConfig{}, err
	}
	client.GatewayURL = gatewayURL
	if !requireRedirect {
		return client, nil
	}
	redirectURI, err := validRadiusOAuthRedirectURI(client.RedirectURI)
	if err != nil {
		return RadiusOAuthClientConfig{}, err
	}
	client.RedirectURI = redirectURI
	return client, nil
}

func compactRadiusScopes(scopes []string) []string {
	compact := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if scope = strings.TrimSpace(scope); scope != "" {
			compact = append(compact, scope)
		}
	}
	return compact
}

func validRadiusOAuthGatewayURL(raw string) (string, error) {
	baseURL, err := validBaseURL(raw)
	if err != nil {
		return "", fmt.Errorf("radius oauth: gateway URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("radius oauth: invalid gateway URL")
	}
	if parsed.Scheme == "https" || (parsed.Scheme == "http" && radiusLoopbackHost(parsed.Hostname())) {
		return baseURL, nil
	}
	return "", errors.New("radius oauth: gateway URL must use HTTPS or loopback HTTP")
}

func validRadiusOAuthRedirectURI(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path == "" || parsed.Port() == "" || !radiusLoopbackHost(parsed.Hostname()) {
		return "", errors.New("radius oauth: redirect URI must be a loopback HTTP URL with an explicit port and path")
	}
	return parsed.String(), nil
}

func safeRadiusAuthorizationEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("radius oauth: invalid authorization endpoint")
	}
	if parsed.Scheme == "https" || (parsed.Scheme == "http" && radiusLoopbackHost(parsed.Hostname())) {
		return parsed.String(), nil
	}
	return "", errors.New("radius oauth: authorization endpoint must use HTTPS or loopback HTTP")
}

func safeRadiusVerificationURI(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("radius oauth: verification URI must use HTTPS")
	}
	return parsed.String(), nil
}

func radiusLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func radiusOAuthDuration(seconds float64) (time.Duration, bool) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > float64(math.MaxInt64)/float64(time.Second) {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

func radiusOAuthPollInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return radiusOAuthDefaultPollInterval
	}
	return interval
}

func radiusOAuthSleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func radiusOAuthHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

type radiusBrowserCallbackServer struct {
	server *http.Server
	done   chan radiusBrowserCallbackResult
}

type radiusBrowserCallbackResult struct {
	code string
	err  error
}

func startRadiusBrowserCallbackServer(redirectURI string, state string) (*radiusBrowserCallbackServer, error) {
	callback, err := url.Parse(redirectURI)
	if err != nil {
		return nil, fmt.Errorf("radius oauth: parse redirect URI: %w", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", net.JoinHostPort(callback.Hostname(), callback.Port()))
	if err != nil {
		return nil, fmt.Errorf("radius oauth: start callback server: %w", err)
	}
	serverInfo := &radiusBrowserCallbackServer{done: make(chan radiusBrowserCallbackResult, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc(callback.Path, func(w http.ResponseWriter, req *http.Request) {
		if authErr := req.URL.Query().Get("error"); authErr != "" {
			writeRadiusOAuthHTML(w, http.StatusBadRequest, "Authentication failed", "Radius authentication did not complete.")
			serverInfo.finish(radiusBrowserCallbackResult{err: fmt.Errorf("radius oauth: authorization failed: %s", redact.Preview(authErr, 256))})
			return
		}
		if req.URL.Query().Get("state") != state {
			writeRadiusOAuthHTML(w, http.StatusBadRequest, "Authentication failed", "State mismatch.")
			serverInfo.finish(radiusBrowserCallbackResult{err: errors.New("radius oauth: state mismatch")})
			return
		}
		code := req.URL.Query().Get("code")
		if code == "" {
			writeRadiusOAuthHTML(w, http.StatusBadRequest, "Authentication failed", "Missing authorization code.")
			serverInfo.finish(radiusBrowserCallbackResult{err: errors.New("radius oauth: missing authorization code")})
			return
		}
		writeRadiusOAuthHTML(w, http.StatusOK, "Authentication successful", "Radius authentication completed. You can close this window.")
		serverInfo.finish(radiusBrowserCallbackResult{code: code})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeRadiusOAuthHTML(w, http.StatusNotFound, "Authentication failed", "Callback route not found.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serverInfo.server = server
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverInfo.finish(radiusBrowserCallbackResult{err: fmt.Errorf("radius oauth: callback server failed: %w", err)})
		}
	}()
	return serverInfo, nil
}

func (server *radiusBrowserCallbackServer) finish(result radiusBrowserCallbackResult) {
	select {
	case server.done <- result:
	default:
	}
}

func (server *radiusBrowserCallbackServer) close() {
	if server != nil && server.server != nil {
		_ = server.server.Close()
	}
}

func waitRadiusBrowserAuthorizationCode(ctx context.Context, state string, server *radiusBrowserCallbackServer, manual func(context.Context, RadiusBrowserManualPrompt) (string, error)) (string, error) {
	manualResult := make(chan radiusBrowserCallbackResult, 1)
	if manual != nil {
		go func() {
			input, err := manual(ctx, RadiusBrowserManualPrompt{Message: "Paste the final redirect URL or authorization code."})
			if err != nil {
				manualResult <- radiusBrowserCallbackResult{err: err}
				return
			}
			parsed := parseRadiusAuthorizationInput(input)
			if parsed.state != "" && parsed.state != state {
				manualResult <- radiusBrowserCallbackResult{err: errors.New("radius oauth: state mismatch")}
				return
			}
			if parsed.code == "" {
				manualResult <- radiusBrowserCallbackResult{err: errors.New("radius oauth: missing authorization code")}
				return
			}
			manualResult <- radiusBrowserCallbackResult{code: parsed.code}
		}()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-server.done:
		if result.err != nil {
			return "", result.err
		}
		return result.code, nil
	case result := <-manualResult:
		if result.err != nil {
			return "", result.err
		}
		return result.code, nil
	}
}

type radiusAuthorizationInput struct {
	code  string
	state string
}

func parseRadiusAuthorizationInput(input string) radiusAuthorizationInput {
	value := strings.TrimSpace(input)
	if value == "" {
		return radiusAuthorizationInput{}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return radiusAuthorizationInput{code: value}
	}
	return radiusAuthorizationInput{code: parsed.Query().Get("code"), state: parsed.Query().Get("state")}
}

func radiusAuthorizationURL(endpoint string, client RadiusOAuthClientConfig, challenge string, state string) (string, error) {
	authorizeURL, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("radius oauth: parse authorization endpoint: %w", err)
	}
	values := authorizeURL.Query()
	values.Set("response_type", "code")
	values.Set("client_id", client.ClientID)
	values.Set("redirect_uri", client.RedirectURI)
	values.Set("scope", strings.Join(client.Scopes, " "))
	values.Set("code_challenge", challenge)
	values.Set("code_challenge_method", "S256")
	values.Set("state", state)
	authorizeURL.RawQuery = values.Encode()
	return authorizeURL.String(), nil
}

func newRadiusPKCEPair() (string, string, error) {
	verifier, err := randomRadiusBase64URL(32)
	if err != nil {
		return "", "", fmt.Errorf("radius oauth: generate verifier: %w", err)
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func randomRadiusBase64URL(bytesLen int) (string, error) {
	buffer := make([]byte, bytesLen)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func writeRadiusOAuthHTML(w http.ResponseWriter, status int, heading string, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<!doctype html><html><body><h1>%s</h1><p>%s</p></body></html>", html.EscapeString(heading), html.EscapeString(message))
}

func storedRadiusOAuthCredential(credentials RadiusOAuthCredentials, previous sigma.StoredCredential) sigma.StoredCredential {
	source := previous.Source
	if source == "" {
		source = "credential-store:" + string(sigma.ProviderRadius)
	}
	return sigma.StoredCredential{
		Type:         sigma.CredentialTypeOAuthToken,
		Value:        credentials.AccessToken,
		RefreshToken: credentials.RefreshToken,
		Expiry:       credentials.Expiry,
		Source:       source,
		ProviderEnv:  copyRadiusOAuthStringMap(previous.ProviderEnv),
		Metadata:     copyRadiusOAuthMetadata(previous.Metadata),
	}
}

func copyRadiusOAuthStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyRadiusOAuthMetadata(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
