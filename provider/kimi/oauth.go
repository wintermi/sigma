// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package kimi

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
	kimiCodingOAuthClientID               = "17e5f671-d194-4dfb-9706-5516cb48c098"
	kimiCodingOAuthDeviceAuthorizationURL = "https://auth.kimi.com/api/oauth/device_authorization"
	kimiCodingOAuthTokenURL               = "https://auth.kimi.com/api/oauth/token"
	kimiCodingOAuthDeviceCodeGrantType    = "urn:ietf:params:oauth:grant-type:device_code"
	kimiCodingOAuthDefaultPollInterval    = 5 * time.Second
	kimiCodingOAuthSlowDownIncrement      = 5 * time.Second
	kimiCodingOAuthDefaultRefreshBefore   = time.Minute
)

// KimiCodingOAuthCredentials carries Kimi Coding subscription OAuth tokens.
// Callers own persistence; Sigma never stores these credentials.
type KimiCodingOAuthCredentials struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// KimiCodingDeviceCodeInfo reports the device code details callers should show
// while a user completes Kimi Coding subscription login.
type KimiCodingDeviceCodeInfo struct {
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresIn       time.Duration
}

// KimiCodingDeviceCodeLoginOptions configures Kimi Coding device-code login.
type KimiCodingDeviceCodeLoginOptions struct {
	HTTPClient   *http.Client
	OnDeviceCode func(KimiCodingDeviceCodeInfo)
}

// KimiCodingOAuthTokenProviderOptions configures the token provider returned
// by NewKimiCodingOAuthTokenProvider and stored-credential provider auth.
type KimiCodingOAuthTokenProviderOptions struct {
	HTTPClient    *http.Client
	Now           func() time.Time
	RefreshBefore time.Duration
	OnRefresh     func(context.Context, KimiCodingOAuthCredentials) error
}

// KimiCodingOAuthTokenProvider resolves and refreshes caller-owned Kimi
// Coding subscription OAuth credentials.
type KimiCodingOAuthTokenProvider struct {
	client        *http.Client
	now           func() time.Time
	refreshBefore time.Duration
	onRefresh     func(context.Context, KimiCodingOAuthCredentials) error

	mu          sync.Mutex
	credentials KimiCodingOAuthCredentials
}

// ProviderAuth returns Kimi Coding API-key and OAuth auth descriptors.
func ProviderAuth(opts KimiCodingOAuthTokenProviderOptions) sigma.ProviderAuth {
	return sigma.ProviderAuth{
		APIKey: sigma.EnvironmentAPIKeyAuth("Kimi API key", "KIMI_API_KEY"),
		OAuth: &sigma.OAuthAuth{
			Name:          "Kimi Coding OAuth",
			RefreshBefore: opts.RefreshBefore,
			Refresh: func(ctx context.Context, stored sigma.StoredCredential) (sigma.StoredCredential, error) {
				refreshed, err := RefreshKimiCodingToken(ctx, stored.RefreshToken, opts)
				if err != nil {
					return sigma.StoredCredential{}, err
				}
				return storedKimiCodingOAuthCredential(refreshed, stored), nil
			},
			Credential: func(_ context.Context, model sigma.Model, _ sigma.Options, stored sigma.StoredCredential) (sigma.Credential, error) {
				if stored.Value == "" {
					return sigma.Credential{}, &sigma.CredentialUnavailableError{
						Provider: model.Provider,
						Model:    model.ID,
						Sources:  []string{"kimi-coding-oauth"},
					}
				}
				source := stored.Source
				if source == "" {
					source = "credential-store:" + string(sigma.ProviderKimiCoding)
				}
				return sigma.Credential{
					Type:     sigma.CredentialTypeOAuthToken,
					Value:    stored.Value,
					Expiry:   stored.Expiry,
					Source:   source,
					Metadata: copyKimiCodingStoredMetadata(stored.Metadata),
				}, nil
			},
		},
	}
}

// RegisterAuth registers Kimi Coding auth descriptors on registry.
func RegisterAuth(registry *sigma.Registry, opts KimiCodingOAuthTokenProviderOptions, registerOpts ...sigma.RegisterOption) error {
	registerOpts = append([]sigma.RegisterOption{sigma.WithOverride()}, registerOpts...)
	if err := sigma.RegisterProviderAuth(registry, sigma.ProviderKimiCoding, ProviderAuth(opts), registerOpts...); err != nil {
		return fmt.Errorf("kimi coding auth: register provider auth: %w", err)
	}
	return nil
}

// RegisterDefaultAuth registers Kimi Coding auth descriptors on the default registry.
func RegisterDefaultAuth(opts KimiCodingOAuthTokenProviderOptions, registerOpts ...sigma.RegisterOption) error {
	registerOpts = append([]sigma.RegisterOption{sigma.WithOverride()}, registerOpts...)
	if err := sigma.RegisterDefaultProviderAuth(sigma.ProviderKimiCoding, ProviderAuth(opts), registerOpts...); err != nil {
		return fmt.Errorf("kimi coding auth: register default provider auth: %w", err)
	}
	return nil
}

// LoginKimiCodingDeviceCode runs the Kimi Coding device-code OAuth flow and
// returns credentials for caller-managed persistence.
func LoginKimiCodingDeviceCode(ctx context.Context, opts KimiCodingDeviceCodeLoginOptions) (KimiCodingOAuthCredentials, error) {
	device, err := requestKimiCodingDeviceCode(ctx, opts.HTTPClient)
	if err != nil {
		return KimiCodingOAuthCredentials{}, err
	}
	if opts.OnDeviceCode != nil {
		opts.OnDeviceCode(KimiCodingDeviceCodeInfo{
			UserCode:        device.UserCode,
			VerificationURI: device.VerificationURI,
			Interval:        kimiCodingPollInterval(device.Interval),
			ExpiresIn:       device.ExpiresIn,
		})
	}
	return pollKimiCodingDeviceCode(ctx, opts.HTTPClient, device)
}

// RefreshKimiCodingToken refreshes Kimi Coding subscription OAuth credentials
// from a refresh token.
func RefreshKimiCodingToken(ctx context.Context, refreshToken string, opts KimiCodingOAuthTokenProviderOptions) (KimiCodingOAuthCredentials, error) {
	if refreshToken == "" {
		return KimiCodingOAuthCredentials{}, &sigma.CredentialUnavailableError{Sources: []string{"kimi-coding-refresh-token"}}
	}
	body, status, err := postKimiCodingForm(ctx, opts.HTTPClient, kimiCodingOAuthTokenURL, url.Values{
		"client_id":     {kimiCodingOAuthClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		return KimiCodingOAuthCredentials{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return KimiCodingOAuthCredentials{}, kimiCodingOAuthResponseError("refresh", status, body)
	}
	return kimiCodingCredentialsFromTokenResponse(body, time.Now())
}

// NewKimiCodingOAuthTokenProvider adapts caller-managed Kimi Coding OAuth
// credentials to Sigma's OAuthTokenProvider and AuthResolver interfaces.
// Refreshed credentials are kept in memory and passed to OnRefresh for caller
// persistence.
func NewKimiCodingOAuthTokenProvider(credentials KimiCodingOAuthCredentials, opts KimiCodingOAuthTokenProviderOptions) *KimiCodingOAuthTokenProvider {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	refreshBefore := opts.RefreshBefore
	if refreshBefore == 0 {
		refreshBefore = kimiCodingOAuthDefaultRefreshBefore
	}
	return &KimiCodingOAuthTokenProvider{
		client:        opts.HTTPClient,
		now:           now,
		refreshBefore: refreshBefore,
		onRefresh:     opts.OnRefresh,
		credentials:   credentials,
	}
}

// Resolve implements sigma.AuthResolver.
func (p *KimiCodingOAuthTokenProvider) Resolve(ctx context.Context, model sigma.Model, opts sigma.Options) (sigma.Credential, error) {
	return p.Token(ctx, model, opts)
}

// Token implements sigma.OAuthTokenProvider.
func (p *KimiCodingOAuthTokenProvider) Token(ctx context.Context, model sigma.Model, _ sigma.Options) (sigma.Credential, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.credentials.AccessToken == "" {
		return sigma.Credential{}, &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"kimi-coding-oauth"},
		}
	}
	if err := p.refreshIfNeeded(ctx, model); err != nil {
		return sigma.Credential{}, err
	}
	return sigma.Credential{
		Type:   sigma.CredentialTypeOAuthToken,
		Value:  p.credentials.AccessToken,
		Expiry: p.credentials.Expiry,
		Source: "kimi-coding-oauth",
	}, nil
}

func (p *KimiCodingOAuthTokenProvider) refreshIfNeeded(ctx context.Context, model sigma.Model) error {
	if !p.shouldRefresh() {
		return nil
	}
	if p.credentials.RefreshToken == "" {
		return &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"kimi-coding-refresh-token"},
		}
	}
	refreshed, err := RefreshKimiCodingToken(ctx, p.credentials.RefreshToken, KimiCodingOAuthTokenProviderOptions{HTTPClient: p.client})
	if err != nil {
		return err
	}
	p.credentials = refreshed
	if p.onRefresh == nil {
		return nil
	}
	if err := p.onRefresh(ctx, refreshed); err != nil {
		return errors.New("kimi coding oauth: refresh callback failed")
	}
	return nil
}

func (p *KimiCodingOAuthTokenProvider) shouldRefresh() bool {
	return !p.credentials.Expiry.IsZero() && !p.now().Add(p.refreshBefore).Before(p.credentials.Expiry)
}

func storedKimiCodingOAuthCredential(credentials KimiCodingOAuthCredentials, previous sigma.StoredCredential) sigma.StoredCredential {
	source := previous.Source
	if source == "" {
		source = "credential-store:" + string(sigma.ProviderKimiCoding)
	}
	return sigma.StoredCredential{
		Type:         sigma.CredentialTypeOAuthToken,
		Value:        credentials.AccessToken,
		RefreshToken: credentials.RefreshToken,
		Expiry:       credentials.Expiry,
		Source:       source,
		ProviderEnv:  copyKimiCodingStoredStringMap(previous.ProviderEnv),
		Metadata:     copyKimiCodingStoredMetadata(previous.Metadata),
	}
}

type kimiCodingDeviceCodeResponse struct {
	DeviceCode              string   `json:"device_code"`
	UserCode                string   `json:"user_code"`
	VerificationURI         string   `json:"verification_uri"`
	VerificationURIComplete string   `json:"verification_uri_complete"`
	Interval                *float64 `json:"interval"`
	ExpiresIn               float64  `json:"expires_in"`
}

type kimiCodingDeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        *float64
	ExpiresIn       time.Duration
}

type kimiCodingDevicePollResult struct {
	credentials KimiCodingOAuthCredentials
	pending     bool
	slowDown    bool
	interval    *time.Duration
}

func requestKimiCodingDeviceCode(ctx context.Context, client *http.Client) (kimiCodingDeviceCode, error) {
	body, status, err := postKimiCodingForm(ctx, client, kimiCodingOAuthDeviceAuthorizationURL, url.Values{
		"client_id": {kimiCodingOAuthClientID},
	})
	if err != nil {
		return kimiCodingDeviceCode{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return kimiCodingDeviceCode{}, kimiCodingOAuthResponseError("device authorization", status, body)
	}

	var decoded kimiCodingDeviceCodeResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return kimiCodingDeviceCode{}, fmt.Errorf("kimi coding oauth: decode device authorization response: %w", err)
	}
	expiresIn, validExpiry := kimiCodingDurationFromSeconds(decoded.ExpiresIn)
	if decoded.DeviceCode == "" || decoded.UserCode == "" || decoded.VerificationURI == "" || decoded.VerificationURIComplete == "" || !validExpiry {
		return kimiCodingDeviceCode{}, errors.New("kimi coding oauth: device authorization response missing fields")
	}
	if _, err := safeKimiCodingVerificationURI(decoded.VerificationURI); err != nil {
		return kimiCodingDeviceCode{}, err
	}
	verificationURI, err := safeKimiCodingVerificationURI(decoded.VerificationURIComplete)
	if err != nil {
		return kimiCodingDeviceCode{}, err
	}
	return kimiCodingDeviceCode{
		DeviceCode:      decoded.DeviceCode,
		UserCode:        decoded.UserCode,
		VerificationURI: verificationURI,
		Interval:        decoded.Interval,
		ExpiresIn:       expiresIn,
	}, nil
}

func pollKimiCodingDeviceCode(ctx context.Context, client *http.Client, device kimiCodingDeviceCode) (KimiCodingOAuthCredentials, error) {
	return pollKimiCodingDeviceCodeWithWait(ctx, client, device, time.Now, kimiCodingSleepContext)
}

func pollKimiCodingDeviceCodeWithWait(
	ctx context.Context,
	client *http.Client,
	device kimiCodingDeviceCode,
	now func() time.Time,
	wait func(context.Context, time.Duration) error,
) (KimiCodingOAuthCredentials, error) {
	interval := kimiCodingPollInterval(device.Interval)
	deadline := now().Add(device.ExpiresIn)
	if err := wait(ctx, interval); err != nil {
		return KimiCodingOAuthCredentials{}, err
	}
	for {
		result, err := pollKimiCodingDeviceCodeOnce(ctx, client, device.DeviceCode, now)
		if err != nil {
			return KimiCodingOAuthCredentials{}, err
		}
		if !result.pending && !result.slowDown {
			return result.credentials, nil
		}
		if result.slowDown {
			if result.interval != nil {
				interval = *result.interval
			} else {
				interval += kimiCodingOAuthSlowDownIncrement
			}
		}
		if !now().Add(interval).Before(deadline) {
			return KimiCodingOAuthCredentials{}, errors.New("kimi coding oauth: device code expired")
		}
		if err := wait(ctx, interval); err != nil {
			return KimiCodingOAuthCredentials{}, err
		}
	}
}

func pollKimiCodingDeviceCodeOnce(ctx context.Context, client *http.Client, deviceCode string, now func() time.Time) (kimiCodingDevicePollResult, error) {
	body, status, err := postKimiCodingForm(ctx, client, kimiCodingOAuthTokenURL, url.Values{
		"client_id":   {kimiCodingOAuthClientID},
		"device_code": {deviceCode},
		"grant_type":  {kimiCodingOAuthDeviceCodeGrantType},
	})
	if err != nil {
		return kimiCodingDevicePollResult{}, err
	}
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		credentials, err := kimiCodingCredentialsFromTokenResponse(body, now())
		if err != nil {
			return kimiCodingDevicePollResult{}, err
		}
		return kimiCodingDevicePollResult{credentials: credentials}, nil
	}

	var decoded struct {
		Error    string   `json:"error"`
		Interval *float64 `json:"interval"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return kimiCodingDevicePollResult{}, fmt.Errorf("kimi coding oauth: decode device token response: %w", err)
	}
	switch decoded.Error {
	case "authorization_pending":
		return kimiCodingDevicePollResult{pending: true}, nil
	case "slow_down":
		result := kimiCodingDevicePollResult{slowDown: true}
		if interval, ok := kimiCodingDurationFromSecondsPtr(decoded.Interval); ok {
			result.interval = &interval
		}
		return result, nil
	case "access_denied", "authorization_denied":
		return kimiCodingDevicePollResult{}, errors.New("kimi coding oauth: device authorization was denied")
	case "expired_token":
		return kimiCodingDevicePollResult{}, errors.New("kimi coding oauth: device code expired")
	default:
		return kimiCodingDevicePollResult{}, kimiCodingOAuthResponseError("device token", status, body)
	}
}

func kimiCodingCredentialsFromTokenResponse(body []byte, now time.Time) (KimiCodingOAuthCredentials, error) {
	var decoded struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return KimiCodingOAuthCredentials{}, fmt.Errorf("kimi coding oauth: decode token response: %w", err)
	}
	expiresIn, validExpiry := kimiCodingDurationFromSeconds(decoded.ExpiresIn)
	if decoded.AccessToken == "" || decoded.RefreshToken == "" || !validExpiry {
		return KimiCodingOAuthCredentials{}, errors.New("kimi coding oauth: token response missing fields")
	}
	return KimiCodingOAuthCredentials{
		AccessToken:  decoded.AccessToken,
		RefreshToken: decoded.RefreshToken,
		Expiry:       now.Add(expiresIn),
	}, nil
}

func postKimiCodingForm(ctx context.Context, client *http.Client, endpoint string, values url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := kimiCodingHTTPClient(client).Do(req)
	if err != nil {
		return nil, 0, kimiCodingOAuthContextOrError(ctx, fmt.Errorf("kimi coding oauth: request: %w", err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, kimiCodingOAuthContextOrError(ctx, fmt.Errorf("kimi coding oauth: read response: %w", err))
	}
	return body, resp.StatusCode, nil
}

func kimiCodingOAuthResponseError(operation string, status int, body []byte) error {
	return fmt.Errorf("kimi coding oauth: %s failed (%d): %s", operation, status, redact.Preview(string(body), 1024))
}

func kimiCodingOAuthContextOrError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func safeKimiCodingVerificationURI(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("kimi coding oauth: untrusted verification_uri in device authorization response")
	}
	return parsed.String(), nil
}

func kimiCodingPollInterval(interval *float64) time.Duration {
	duration, ok := kimiCodingDurationFromSecondsPtr(interval)
	if !ok {
		return kimiCodingOAuthDefaultPollInterval
	}
	return duration
}

func kimiCodingDurationFromSecondsPtr(seconds *float64) (time.Duration, bool) {
	if seconds == nil {
		return 0, false
	}
	return kimiCodingDurationFromSeconds(*seconds)
}

func kimiCodingDurationFromSeconds(seconds float64) (time.Duration, bool) {
	if seconds <= 0 || math.IsInf(seconds, 0) || math.IsNaN(seconds) || seconds > float64(math.MaxInt64)/float64(time.Second) {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

func kimiCodingHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func kimiCodingSleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func copyKimiCodingStoredStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyKimiCodingStoredMetadata(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
