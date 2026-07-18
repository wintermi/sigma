// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package xai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/redact"
)

const (
	xaiOAuthDeviceCodeURL         = "https://auth.x.ai/oauth2/device/code"
	xaiOAuthTokenURL              = "https://auth.x.ai/oauth2/token"
	xaiOAuthDefaultPollInterval   = 5 * time.Second
	xaiOAuthSlowDownPollIncrement = 5 * time.Second
	xaiOAuthDefaultRefreshBefore  = time.Minute
	xaiOAuthDefaultTokenLifetime  = time.Hour
	xaiOAuthDeviceCodeGrantType   = "urn:ietf:params:oauth:grant-type:device_code"
)

// XAIOAuthClientConfig identifies the caller-owned public OAuth client used
// for xAI device-code login and token refresh.
type XAIOAuthClientConfig struct {
	ClientID string
	Scopes   []string
}

// XAIOAuthCredentials carries xAI OAuth tokens. Callers own persistence;
// Sigma never stores these credentials.
type XAIOAuthCredentials struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// XAIDeviceCodeInfo reports the user code and verification URL callers should
// show during xAI device-code login.
type XAIDeviceCodeInfo struct {
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresIn       time.Duration
}

// XAIDeviceCodeLoginOptions configures xAI device-code login.
type XAIDeviceCodeLoginOptions struct {
	Client       XAIOAuthClientConfig
	HTTPClient   *http.Client
	OnDeviceCode func(XAIDeviceCodeInfo)
}

// XAIOAuthTokenProviderOptions configures the OAuth token provider returned
// by NewXAIOAuthTokenProvider.
type XAIOAuthTokenProviderOptions struct {
	Client        XAIOAuthClientConfig
	HTTPClient    *http.Client
	Now           func() time.Time
	RefreshBefore time.Duration
	OnRefresh     func(context.Context, XAIOAuthCredentials) error
}

// XAIOAuthTokenProvider resolves and refreshes caller-owned xAI OAuth
// credentials.
type XAIOAuthTokenProvider struct {
	client        XAIOAuthClientConfig
	httpClient    *http.Client
	now           func() time.Time
	refreshBefore time.Duration
	onRefresh     func(context.Context, XAIOAuthCredentials) error

	mu          sync.Mutex
	credentials XAIOAuthCredentials
}

// ProviderAuth returns xAI API-key and OAuth auth descriptors.
func ProviderAuth(opts XAIOAuthTokenProviderOptions) sigma.ProviderAuth {
	return sigma.ProviderAuth{
		APIKey: sigma.EnvironmentAPIKeyAuth("xAI API key", "XAI_API_KEY"),
		OAuth: &sigma.OAuthAuth{
			Name:          "xAI OAuth",
			RefreshBefore: opts.RefreshBefore,
			Refresh: func(ctx context.Context, stored sigma.StoredCredential) (sigma.StoredCredential, error) {
				refreshed, err := RefreshXAIToken(ctx, stored.RefreshToken, opts)
				if err != nil {
					return sigma.StoredCredential{}, err
				}
				return storedXAIOAuthCredential(refreshed, stored), nil
			},
			Credential: func(_ context.Context, model sigma.Model, _ sigma.Options, stored sigma.StoredCredential) (sigma.Credential, error) {
				if stored.Value == "" {
					return sigma.Credential{}, &sigma.CredentialUnavailableError{
						Provider: model.Provider,
						Model:    model.ID,
						Sources:  []string{"xai-oauth"},
					}
				}
				source := stored.Source
				if source == "" {
					source = "credential-store:" + string(sigma.ProviderXAI)
				}
				return sigma.Credential{
					Type:     sigma.CredentialTypeOAuthToken,
					Value:    stored.Value,
					Expiry:   stored.Expiry,
					Source:   source,
					Metadata: copyStoredMetadata(stored.Metadata),
				}, nil
			},
		},
	}
}

// RegisterAuth registers xAI auth descriptors on registry.
func RegisterAuth(registry *sigma.Registry, opts XAIOAuthTokenProviderOptions, registerOpts ...sigma.RegisterOption) error {
	registerOpts = append([]sigma.RegisterOption{sigma.WithOverride()}, registerOpts...)
	if err := sigma.RegisterProviderAuth(registry, sigma.ProviderXAI, ProviderAuth(opts), registerOpts...); err != nil {
		return fmt.Errorf("xai auth: register provider auth: %w", err)
	}
	return nil
}

// RegisterDefaultAuth registers xAI auth descriptors on the default registry.
func RegisterDefaultAuth(opts XAIOAuthTokenProviderOptions, registerOpts ...sigma.RegisterOption) error {
	registerOpts = append([]sigma.RegisterOption{sigma.WithOverride()}, registerOpts...)
	if err := sigma.RegisterDefaultProviderAuth(sigma.ProviderXAI, ProviderAuth(opts), registerOpts...); err != nil {
		return fmt.Errorf("xai auth: register default provider auth: %w", err)
	}
	return nil
}

// LoginXAIDeviceCode runs the xAI device-code OAuth flow and returns
// credentials for caller-managed persistence.
func LoginXAIDeviceCode(ctx context.Context, opts XAIDeviceCodeLoginOptions) (XAIOAuthCredentials, error) {
	if err := validateXAIOAuthClient(opts.Client); err != nil {
		return XAIOAuthCredentials{}, err
	}
	device, err := requestXAIDeviceCode(ctx, opts.HTTPClient, opts.Client)
	if err != nil {
		return XAIOAuthCredentials{}, err
	}
	if opts.OnDeviceCode != nil {
		opts.OnDeviceCode(XAIDeviceCodeInfo{
			UserCode:        device.UserCode,
			VerificationURI: device.VerificationURI,
			Interval:        xaiPollInterval(&device.Interval),
			ExpiresIn:       device.ExpiresIn,
		})
	}
	return pollXAIDeviceCode(ctx, opts.HTTPClient, opts.Client, device)
}

// RefreshXAIToken refreshes xAI OAuth credentials from a refresh token.
func RefreshXAIToken(ctx context.Context, refreshToken string, opts XAIOAuthTokenProviderOptions) (XAIOAuthCredentials, error) {
	if refreshToken == "" {
		return XAIOAuthCredentials{}, &sigma.CredentialUnavailableError{Sources: []string{"xai-refresh-token"}}
	}
	if err := validateXAIOAuthClient(opts.Client); err != nil {
		return XAIOAuthCredentials{}, err
	}
	body, status, err := postXAIForm(ctx, opts.HTTPClient, xaiOAuthTokenURL, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {opts.Client.ClientID},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		return XAIOAuthCredentials{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return XAIOAuthCredentials{}, xaiOAuthResponseError("refresh", status, body)
	}
	return xaiCredentialsFromTokenResponse(body, refreshToken, time.Now())
}

// NewXAIOAuthTokenProvider adapts caller-managed xAI OAuth credentials to
// Sigma's OAuthTokenProvider and AuthResolver interfaces. Refreshed
// credentials are kept in memory and passed to OnRefresh for caller
// persistence.
func NewXAIOAuthTokenProvider(credentials XAIOAuthCredentials, opts XAIOAuthTokenProviderOptions) *XAIOAuthTokenProvider {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	refreshBefore := opts.RefreshBefore
	if refreshBefore == 0 {
		refreshBefore = xaiOAuthDefaultRefreshBefore
	}
	return &XAIOAuthTokenProvider{
		client:        opts.Client,
		httpClient:    opts.HTTPClient,
		now:           now,
		refreshBefore: refreshBefore,
		onRefresh:     opts.OnRefresh,
		credentials:   credentials,
	}
}

// Resolve implements sigma.AuthResolver.
func (p *XAIOAuthTokenProvider) Resolve(ctx context.Context, model sigma.Model, opts sigma.Options) (sigma.Credential, error) {
	return p.Token(ctx, model, opts)
}

// Token implements sigma.OAuthTokenProvider.
func (p *XAIOAuthTokenProvider) Token(ctx context.Context, model sigma.Model, _ sigma.Options) (sigma.Credential, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.credentials.AccessToken == "" {
		return sigma.Credential{}, &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"xai-oauth"},
		}
	}
	if err := p.refreshIfNeeded(ctx, model); err != nil {
		return sigma.Credential{}, err
	}
	return sigma.Credential{
		Type:   sigma.CredentialTypeOAuthToken,
		Value:  p.credentials.AccessToken,
		Expiry: p.credentials.Expiry,
		Source: "xai-oauth",
	}, nil
}

func (p *XAIOAuthTokenProvider) refreshIfNeeded(ctx context.Context, model sigma.Model) error {
	if !p.shouldRefresh() {
		return nil
	}
	if p.credentials.RefreshToken == "" {
		return &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"xai-refresh-token"},
		}
	}
	refreshed, err := RefreshXAIToken(ctx, p.credentials.RefreshToken, XAIOAuthTokenProviderOptions{
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
		return errors.New("xai oauth: refresh callback failed")
	}
	return nil
}

func (p *XAIOAuthTokenProvider) shouldRefresh() bool {
	return !p.credentials.Expiry.IsZero() && !p.now().Add(p.refreshBefore).Before(p.credentials.Expiry)
}

func storedXAIOAuthCredential(credentials XAIOAuthCredentials, previous sigma.StoredCredential) sigma.StoredCredential {
	source := previous.Source
	if source == "" {
		source = "credential-store:" + string(sigma.ProviderXAI)
	}
	return sigma.StoredCredential{
		Type:         sigma.CredentialTypeOAuthToken,
		Value:        credentials.AccessToken,
		RefreshToken: credentials.RefreshToken,
		Expiry:       credentials.Expiry,
		Source:       source,
		ProviderEnv:  copyStoredStringMap(previous.ProviderEnv),
		Metadata:     copyStoredMetadata(previous.Metadata),
	}
}

type xaiDeviceCodeResponse struct {
	DeviceCode              string   `json:"device_code"`
	UserCode                string   `json:"user_code"`
	VerificationURI         string   `json:"verification_uri"`
	VerificationURIComplete string   `json:"verification_uri_complete"`
	Interval                *float64 `json:"interval"`
	ExpiresIn               float64  `json:"expires_in"`
}

type xaiDeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        float64
	ExpiresIn       time.Duration
}

type xaiDevicePollResult struct {
	credentials XAIOAuthCredentials
	pending     bool
	slowDown    bool
}

func requestXAIDeviceCode(ctx context.Context, client *http.Client, config XAIOAuthClientConfig) (xaiDeviceCode, error) {
	body, status, err := postXAIForm(ctx, client, xaiOAuthDeviceCodeURL, url.Values{
		"client_id": {config.ClientID},
		"scope":     {strings.Join(config.Scopes, " ")},
	})
	if err != nil {
		return xaiDeviceCode{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return xaiDeviceCode{}, xaiOAuthResponseError("device authorization", status, body)
	}

	var decoded xaiDeviceCodeResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return xaiDeviceCode{}, fmt.Errorf("xai oauth: decode device authorization response: %w", err)
	}
	expiresIn, validExpiry := durationFromSeconds(decoded.ExpiresIn)
	if decoded.DeviceCode == "" || decoded.UserCode == "" || decoded.VerificationURI == "" || !validExpiry {
		return xaiDeviceCode{}, errors.New("xai oauth: device authorization response missing fields")
	}
	verificationURI := decoded.VerificationURI
	if decoded.VerificationURIComplete != "" {
		verificationURI = decoded.VerificationURIComplete
	}
	verificationURI, err = safeXAIVerificationURI(verificationURI)
	if err != nil {
		return xaiDeviceCode{}, err
	}
	interval := 0.0
	if decoded.Interval != nil {
		interval = *decoded.Interval
	}
	return xaiDeviceCode{
		DeviceCode:      decoded.DeviceCode,
		UserCode:        decoded.UserCode,
		VerificationURI: verificationURI,
		Interval:        interval,
		ExpiresIn:       expiresIn,
	}, nil
}

func pollXAIDeviceCode(ctx context.Context, client *http.Client, config XAIOAuthClientConfig, device xaiDeviceCode) (XAIOAuthCredentials, error) {
	return pollXAIDeviceCodeWithWait(ctx, client, config, device, time.Now, xaiSleepContext)
}

func pollXAIDeviceCodeWithWait(
	ctx context.Context,
	client *http.Client,
	config XAIOAuthClientConfig,
	device xaiDeviceCode,
	now func() time.Time,
	wait func(context.Context, time.Duration) error,
) (XAIOAuthCredentials, error) {
	interval := xaiPollInterval(&device.Interval)
	deadline := now().Add(device.ExpiresIn)
	if err := wait(ctx, interval); err != nil {
		return XAIOAuthCredentials{}, err
	}
	for {
		result, err := pollXAIDeviceCodeOnce(ctx, client, config, device.DeviceCode)
		if err != nil {
			return XAIOAuthCredentials{}, err
		}
		if !result.pending && !result.slowDown {
			return result.credentials, nil
		}
		if result.slowDown {
			interval += xaiOAuthSlowDownPollIncrement
		}
		if now().Add(interval).After(deadline) {
			return XAIOAuthCredentials{}, errors.New("xai oauth: device code expired")
		}
		if err := wait(ctx, interval); err != nil {
			return XAIOAuthCredentials{}, err
		}
	}
}

func pollXAIDeviceCodeOnce(ctx context.Context, client *http.Client, config XAIOAuthClientConfig, deviceCode string) (xaiDevicePollResult, error) {
	body, status, err := postXAIForm(ctx, client, xaiOAuthTokenURL, url.Values{
		"grant_type":  {xaiOAuthDeviceCodeGrantType},
		"client_id":   {config.ClientID},
		"device_code": {deviceCode},
	})
	if err != nil {
		return xaiDevicePollResult{}, err
	}

	var decoded struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return xaiDevicePollResult{}, fmt.Errorf("xai oauth: decode device token response: %w", err)
	}
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		credentials, err := xaiCredentialsFromTokenResponse(body, "", time.Now())
		if err != nil {
			return xaiDevicePollResult{}, err
		}
		return xaiDevicePollResult{credentials: credentials}, nil
	}
	switch decoded.Error {
	case "authorization_pending":
		return xaiDevicePollResult{pending: true}, nil
	case "slow_down":
		return xaiDevicePollResult{slowDown: true}, nil
	case "access_denied", "authorization_denied":
		return xaiDevicePollResult{}, errors.New("xai oauth: device authorization was denied")
	case "expired_token":
		return xaiDevicePollResult{}, errors.New("xai oauth: device code expired")
	default:
		return xaiDevicePollResult{}, xaiOAuthResponseError("device token", status, body)
	}
}

func xaiCredentialsFromTokenResponse(body []byte, previousRefreshToken string, now time.Time) (XAIOAuthCredentials, error) {
	var decoded struct {
		AccessToken  string   `json:"access_token"`
		RefreshToken *string  `json:"refresh_token"`
		ExpiresIn    *float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return XAIOAuthCredentials{}, fmt.Errorf("xai oauth: decode token response: %w", err)
	}
	if decoded.AccessToken == "" {
		return XAIOAuthCredentials{}, errors.New("xai oauth: token response missing access_token")
	}
	refreshToken := previousRefreshToken
	if decoded.RefreshToken != nil {
		refreshToken = *decoded.RefreshToken
	}
	if refreshToken == "" {
		return XAIOAuthCredentials{}, errors.New("xai oauth: token response missing refresh_token")
	}
	expiresIn := xaiOAuthDefaultTokenLifetime
	if decoded.ExpiresIn != nil {
		var valid bool
		expiresIn, valid = durationFromSeconds(*decoded.ExpiresIn)
		if !valid {
			return XAIOAuthCredentials{}, errors.New("xai oauth: token response has invalid expires_in")
		}
	}
	return XAIOAuthCredentials{
		AccessToken:  decoded.AccessToken,
		RefreshToken: refreshToken,
		Expiry:       now.Add(expiresIn),
	}, nil
}

func postXAIForm(ctx context.Context, client *http.Client, endpoint string, values url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := xaiHTTPClient(client).Do(req)
	if err != nil {
		return nil, 0, xaiOAuthContextOrError(ctx, fmt.Errorf("xai oauth: request: %w", err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, xaiOAuthContextOrError(ctx, fmt.Errorf("xai oauth: read response: %w", err))
	}
	return body, resp.StatusCode, nil
}

func xaiOAuthResponseError(operation string, status int, body []byte) error {
	return fmt.Errorf("xai oauth: %s failed (%d): %s", operation, status, redact.Preview(string(body), 1024))
}

func validateXAIOAuthClient(config XAIOAuthClientConfig) error {
	if strings.TrimSpace(config.ClientID) == "" {
		return errors.New("xai oauth: client ID is required")
	}
	if len(config.Scopes) == 0 {
		return errors.New("xai oauth: scopes are required")
	}
	for _, scope := range config.Scopes {
		if strings.TrimSpace(scope) == "" {
			return errors.New("xai oauth: scopes must not contain empty values")
		}
	}
	return nil
}

func safeXAIVerificationURI(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("xai oauth: untrusted verification_uri in device authorization response")
	}
	return parsed.String(), nil
}

func xaiPollInterval(interval *float64) time.Duration {
	if interval == nil {
		return xaiOAuthDefaultPollInterval
	}
	duration, ok := durationFromSeconds(*interval)
	if !ok {
		return xaiOAuthDefaultPollInterval
	}
	return duration
}

func durationFromSeconds(seconds float64) (time.Duration, bool) {
	if seconds <= 0 || math.IsInf(seconds, 0) || math.IsNaN(seconds) || seconds > float64(math.MaxInt64)/float64(time.Second) {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

func xaiHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func xaiSleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func xaiOAuthContextOrError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func copyStoredMetadata(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
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
