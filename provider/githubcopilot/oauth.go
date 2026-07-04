// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package githubcopilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/redact"
)

const (
	githubCopilotOAuthClientID              = "Iv1.b507a08c87ecfe98"
	githubCopilotOAuthDefaultDomain         = "github.com"
	githubCopilotOAuthDefaultPollInterval   = 5 * time.Second
	githubCopilotOAuthSlowDownPollIncrement = 5 * time.Second
	githubCopilotOAuthDefaultRefreshBefore  = time.Minute
)

// GitHubCopilotOAuthCredentials carries GitHub Copilot OAuth tokens. Callers
// own persistence; Sigma never stores these credentials.
type GitHubCopilotOAuthCredentials struct {
	AccessToken      string
	RefreshToken     string
	Expiry           time.Time
	EnterpriseDomain string
	BaseURL          string
}

// GitHubCopilotDeviceCodeInfo reports the user code and verification URL that
// should be shown to the caller during GitHub Copilot device-code login.
type GitHubCopilotDeviceCodeInfo struct {
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresIn       time.Duration
}

// GitHubCopilotDeviceCodeLoginOptions configures GitHub Copilot device-code
// login.
type GitHubCopilotDeviceCodeLoginOptions struct {
	HTTPClient       *http.Client
	EnterpriseDomain string
	OnDeviceCode     func(GitHubCopilotDeviceCodeInfo)
}

// GitHubCopilotOAuthTokenProviderOptions configures the OAuth token provider
// returned by NewGitHubCopilotOAuthTokenProvider.
type GitHubCopilotOAuthTokenProviderOptions struct {
	HTTPClient       *http.Client
	EnterpriseDomain string
	Now              func() time.Time
	RefreshBefore    time.Duration
	OnRefresh        func(context.Context, GitHubCopilotOAuthCredentials) error
}

// ProviderAuth returns GitHub Copilot API-key and OAuth auth descriptors.
func ProviderAuth(opts GitHubCopilotOAuthTokenProviderOptions) sigma.ProviderAuth {
	return sigma.ProviderAuth{
		APIKey: sigma.EnvironmentAPIKeyAuth("GitHub Copilot token", "COPILOT_GITHUB_TOKEN"),
		OAuth: &sigma.OAuthAuth{
			Name:          "GitHub Copilot OAuth",
			RefreshBefore: opts.RefreshBefore,
			Refresh: func(ctx context.Context, stored sigma.StoredCredential) (sigma.StoredCredential, error) {
				refreshOpts := opts
				if enterpriseDomain := stringMetadata(stored.Metadata, "enterpriseDomain"); enterpriseDomain != "" {
					refreshOpts.EnterpriseDomain = enterpriseDomain
				}
				refreshed, err := RefreshGitHubCopilotToken(ctx, stored.RefreshToken, refreshOpts)
				if err != nil {
					return sigma.StoredCredential{}, err
				}
				return storedGitHubCopilotOAuthCredential(refreshed, stored), nil
			},
			Credential: func(_ context.Context, model sigma.Model, _ sigma.Options, stored sigma.StoredCredential) (sigma.Credential, error) {
				if stored.Value == "" {
					return sigma.Credential{}, &sigma.CredentialUnavailableError{Provider: model.Provider, Model: model.ID, Sources: []string{"github-copilot-oauth"}}
				}
				metadata := copyStoredMetadata(stored.Metadata)
				baseURL := stringMetadata(metadata, "baseURL")
				enterpriseDomain := stringMetadata(metadata, "enterpriseDomain")
				if enterpriseDomain == "" {
					enterpriseDomain = opts.EnterpriseDomain
				}
				if baseURL == "" {
					baseURL = GitHubCopilotBaseURL(stored.Value, enterpriseDomain)
					if metadata == nil {
						metadata = make(map[string]any)
					}
					metadata["baseURL"] = baseURL
				}
				if enterpriseDomain != "" {
					if metadata == nil {
						metadata = make(map[string]any)
					}
					metadata["enterpriseDomain"] = enterpriseDomain
				}
				source := stored.Source
				if source == "" {
					source = "credential-store:" + string(sigma.ProviderGitHubCopilot)
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

// RegisterAuth registers GitHub Copilot auth descriptors on registry.
func RegisterAuth(registry *sigma.Registry, opts GitHubCopilotOAuthTokenProviderOptions, registerOpts ...sigma.RegisterOption) error {
	registerOpts = append([]sigma.RegisterOption{sigma.WithOverride()}, registerOpts...)
	if err := sigma.RegisterProviderAuth(registry, sigma.ProviderGitHubCopilot, ProviderAuth(opts), registerOpts...); err != nil {
		return fmt.Errorf("github copilot auth: register provider auth: %w", err)
	}
	return nil
}

// RegisterDefaultAuth registers GitHub Copilot auth descriptors on the default registry.
func RegisterDefaultAuth(opts GitHubCopilotOAuthTokenProviderOptions, registerOpts ...sigma.RegisterOption) error {
	registerOpts = append([]sigma.RegisterOption{sigma.WithOverride()}, registerOpts...)
	if err := sigma.RegisterDefaultProviderAuth(sigma.ProviderGitHubCopilot, ProviderAuth(opts), registerOpts...); err != nil {
		return fmt.Errorf("github copilot auth: register default provider auth: %w", err)
	}
	return nil
}

// GitHubCopilotModelEnableOptions configures GitHub Copilot model-policy
// enablement helpers.
type GitHubCopilotModelEnableOptions struct {
	HTTPClient       *http.Client
	BaseURL          string
	EnterpriseDomain string
}

// GitHubCopilotModelEnableResult reports the outcome for one model-policy
// enablement request.
type GitHubCopilotModelEnableResult struct {
	ModelID    string
	Enabled    bool
	StatusCode int
	Err        error
}

type githubCopilotDeviceCodeResponse struct {
	DeviceCode      string  `json:"device_code"`
	UserCode        string  `json:"user_code"`
	VerificationURI string  `json:"verification_uri"`
	Interval        float64 `json:"interval"`
	ExpiresIn       int64   `json:"expires_in"`
}

type githubCopilotDevicePollStatus string

const (
	githubCopilotDevicePollPending  githubCopilotDevicePollStatus = "pending"
	githubCopilotDevicePollSlowDown githubCopilotDevicePollStatus = "slow_down"
	githubCopilotDevicePollComplete githubCopilotDevicePollStatus = "complete"
)

type githubCopilotDevicePollResult struct {
	status githubCopilotDevicePollStatus
	token  string
}

// GitHubCopilotOAuthTokenProvider resolves and refreshes caller-owned GitHub
// Copilot OAuth credentials.
type GitHubCopilotOAuthTokenProvider struct {
	client        *http.Client
	now           func() time.Time
	refreshBefore time.Duration
	onRefresh     func(context.Context, GitHubCopilotOAuthCredentials) error

	mu          sync.Mutex
	credentials GitHubCopilotOAuthCredentials
}

// LoginGitHubCopilotDeviceCode runs the GitHub Copilot device-code OAuth flow
// and returns credentials for caller-managed persistence.
func LoginGitHubCopilotDeviceCode(ctx context.Context, opts GitHubCopilotDeviceCodeLoginOptions) (GitHubCopilotOAuthCredentials, error) {
	domain, err := githubCopilotDomain(opts.EnterpriseDomain)
	if err != nil {
		return GitHubCopilotOAuthCredentials{}, err
	}
	device, err := startGitHubCopilotDeviceFlow(ctx, opts.HTTPClient, domain)
	if err != nil {
		return GitHubCopilotOAuthCredentials{}, err
	}
	if opts.OnDeviceCode != nil {
		opts.OnDeviceCode(GitHubCopilotDeviceCodeInfo{
			UserCode:        device.UserCode,
			VerificationURI: device.VerificationURI,
			Interval:        githubCopilotPollInterval(device),
			ExpiresIn:       time.Duration(device.ExpiresIn) * time.Second,
		})
	}
	githubToken, err := pollGitHubCopilotDeviceFlow(ctx, opts.HTTPClient, domain, device)
	if err != nil {
		return GitHubCopilotOAuthCredentials{}, err
	}
	return RefreshGitHubCopilotToken(ctx, githubToken, GitHubCopilotOAuthTokenProviderOptions{
		HTTPClient:       opts.HTTPClient,
		EnterpriseDomain: opts.EnterpriseDomain,
	})
}

// RefreshGitHubCopilotToken refreshes GitHub Copilot OAuth credentials from the
// GitHub device-flow token returned during login.
func RefreshGitHubCopilotToken(ctx context.Context, refreshToken string, opts GitHubCopilotOAuthTokenProviderOptions) (GitHubCopilotOAuthCredentials, error) {
	if refreshToken == "" {
		return GitHubCopilotOAuthCredentials{}, &sigma.CredentialUnavailableError{
			Sources: []string{"github-copilot-refresh-token"},
		}
	}
	domain, err := githubCopilotDomain(opts.EnterpriseDomain)
	if err != nil {
		return GitHubCopilotOAuthCredentials{}, err
	}
	urls := githubCopilotOAuthURLs(domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urls.copilotToken, nil)
	if err != nil {
		return GitHubCopilotOAuthCredentials{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+refreshToken)
	addGitHubCopilotOAuthHeaders(req.Header)

	resp, err := githubCopilotHTTPClient(opts.HTTPClient).Do(req)
	if err != nil {
		return GitHubCopilotOAuthCredentials{}, githubCopilotContextOrError(ctx, fmt.Errorf("github copilot oauth: refresh token: %w", err))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return GitHubCopilotOAuthCredentials{}, githubCopilotContextOrError(ctx, fmt.Errorf("github copilot oauth: read refresh response: %w", err))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return GitHubCopilotOAuthCredentials{}, fmt.Errorf("github copilot oauth: refresh failed (%d): %s", resp.StatusCode, redact.Preview(string(data), 1024))
	}

	var decoded struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return GitHubCopilotOAuthCredentials{}, fmt.Errorf("github copilot oauth: decode refresh response: %w", err)
	}
	if decoded.Token == "" || decoded.ExpiresAt <= 0 {
		return GitHubCopilotOAuthCredentials{}, fmt.Errorf("github copilot oauth: refresh response missing fields")
	}
	credentials := GitHubCopilotOAuthCredentials{
		AccessToken:  decoded.Token,
		RefreshToken: refreshToken,
		Expiry:       time.Unix(decoded.ExpiresAt, 0),
		BaseURL:      GitHubCopilotBaseURL(decoded.Token, domain),
	}
	if domain != githubCopilotOAuthDefaultDomain {
		credentials.EnterpriseDomain = domain
	}
	return credentials, nil
}

// NewGitHubCopilotOAuthTokenProvider adapts caller-managed GitHub Copilot
// OAuth credentials to Sigma's OAuthTokenProvider and AuthResolver interfaces.
// Refreshed credentials are kept in memory and passed to OnRefresh for caller
// persistence.
func NewGitHubCopilotOAuthTokenProvider(credentials GitHubCopilotOAuthCredentials, opts GitHubCopilotOAuthTokenProviderOptions) *GitHubCopilotOAuthTokenProvider {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	refreshBefore := opts.RefreshBefore
	if refreshBefore == 0 {
		refreshBefore = githubCopilotOAuthDefaultRefreshBefore
	}
	return &GitHubCopilotOAuthTokenProvider{
		client:        opts.HTTPClient,
		now:           now,
		refreshBefore: refreshBefore,
		onRefresh:     opts.OnRefresh,
		credentials:   credentials,
	}
}

func storedGitHubCopilotOAuthCredential(credentials GitHubCopilotOAuthCredentials, previous sigma.StoredCredential) sigma.StoredCredential {
	source := previous.Source
	if source == "" {
		source = "credential-store:" + string(sigma.ProviderGitHubCopilot)
	}
	metadata := copyStoredMetadata(previous.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	if credentials.BaseURL != "" {
		metadata["baseURL"] = credentials.BaseURL
	}
	if credentials.EnterpriseDomain != "" {
		metadata["enterpriseDomain"] = credentials.EnterpriseDomain
	}
	return sigma.StoredCredential{
		Type:         sigma.CredentialTypeOAuthToken,
		Value:        credentials.AccessToken,
		RefreshToken: credentials.RefreshToken,
		Expiry:       credentials.Expiry,
		Source:       source,
		ProviderEnv:  copyStringMap(previous.ProviderEnv),
		Metadata:     metadata,
	}
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

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func stringMetadata(metadata map[string]any, key string) string {
	if value, ok := metadata[key].(string); ok {
		return value
	}
	return ""
}

// Resolve implements sigma.AuthResolver.
func (p *GitHubCopilotOAuthTokenProvider) Resolve(ctx context.Context, model sigma.Model, opts sigma.Options) (sigma.Credential, error) {
	return p.Token(ctx, model, opts)
}

// Token implements sigma.OAuthTokenProvider.
func (p *GitHubCopilotOAuthTokenProvider) Token(ctx context.Context, model sigma.Model, _ sigma.Options) (sigma.Credential, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.credentials.AccessToken == "" {
		return sigma.Credential{}, &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"github-copilot-oauth"},
		}
	}
	if err := p.refreshIfNeeded(ctx, model); err != nil {
		return sigma.Credential{}, err
	}
	metadata := map[string]any{}
	if p.credentials.BaseURL != "" {
		metadata["baseURL"] = p.credentials.BaseURL
	}
	if p.credentials.EnterpriseDomain != "" {
		metadata["enterpriseDomain"] = p.credentials.EnterpriseDomain
	}
	return sigma.Credential{
		Type:     sigma.CredentialTypeOAuthToken,
		Value:    p.credentials.AccessToken,
		Expiry:   p.credentials.Expiry,
		Source:   "github-copilot-oauth",
		Metadata: metadata,
	}, nil
}

func (p *GitHubCopilotOAuthTokenProvider) refreshIfNeeded(ctx context.Context, model sigma.Model) error {
	if !p.shouldRefresh() {
		return nil
	}
	if p.credentials.RefreshToken == "" {
		return &sigma.CredentialUnavailableError{
			Provider: model.Provider,
			Model:    model.ID,
			Sources:  []string{"github-copilot-refresh-token"},
		}
	}
	refreshed, err := RefreshGitHubCopilotToken(ctx, p.credentials.RefreshToken, GitHubCopilotOAuthTokenProviderOptions{
		HTTPClient:       p.client,
		EnterpriseDomain: p.credentials.EnterpriseDomain,
	})
	if err != nil {
		return err
	}
	p.credentials = refreshed
	if p.onRefresh == nil {
		return nil
	}
	if err := p.onRefresh(ctx, refreshed); err != nil {
		return fmt.Errorf("github copilot oauth: refresh callback failed")
	}
	return nil
}

func (p *GitHubCopilotOAuthTokenProvider) shouldRefresh() bool {
	if p.credentials.Expiry.IsZero() {
		return false
	}
	return !p.now().Add(p.refreshBefore).Before(p.credentials.Expiry)
}

// EnableGitHubCopilotModel enables one GitHub Copilot model policy for the
// resolved Copilot account.
func EnableGitHubCopilotModel(ctx context.Context, token string, modelID string, opts GitHubCopilotModelEnableOptions) (GitHubCopilotModelEnableResult, error) {
	result := GitHubCopilotModelEnableResult{ModelID: modelID}
	if token == "" {
		result.Err = &sigma.CredentialUnavailableError{Sources: []string{"github-copilot-oauth"}}
		return result, result.Err
	}
	if modelID == "" {
		result.Err = fmt.Errorf("github copilot oauth: model id is required")
		return result, result.Err
	}
	baseURL, err := githubCopilotModelPolicyBaseURL(token, opts)
	if err != nil {
		result.Err = err
		return result, err
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/models/" + url.PathEscape(modelID) + "/policy"
	body := bytes.NewBufferString(`{"state":"enabled"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		result.Err = err
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Openai-Intent", "chat-policy")
	req.Header.Set("X-Interaction-Type", "chat-policy")
	addGitHubCopilotOAuthHeaders(req.Header)

	resp, err := githubCopilotHTTPClient(opts.HTTPClient).Do(req)
	if err != nil {
		result.Err = githubCopilotContextOrError(ctx, fmt.Errorf("github copilot oauth: enable model %q: %w", modelID, err))
		return result, result.Err
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		result.Err = githubCopilotContextOrError(ctx, fmt.Errorf("github copilot oauth: read enable model %q response: %w", modelID, err))
		return result, result.Err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		result.Err = fmt.Errorf("github copilot oauth: enable model %q failed (%d): %s", modelID, resp.StatusCode, redact.Preview(string(data), 1024))
		return result, result.Err
	}
	result.Enabled = true
	return result, nil
}

// EnableGitHubCopilotModels enables multiple GitHub Copilot model policies and
// reports each result independently.
func EnableGitHubCopilotModels(ctx context.Context, token string, modelIDs []string, opts GitHubCopilotModelEnableOptions) []GitHubCopilotModelEnableResult {
	results := make([]GitHubCopilotModelEnableResult, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		result, err := EnableGitHubCopilotModel(ctx, token, modelID, opts)
		if err != nil {
			result.Err = err
		}
		results = append(results, result)
	}
	return results
}

// GitHubCopilotBaseURL returns the Copilot API base URL implied by a token or
// enterprise domain.
func GitHubCopilotBaseURL(token string, enterpriseDomain string) string {
	if token != "" {
		if baseURL := githubCopilotBaseURLFromToken(token); baseURL != "" {
			return baseURL
		}
	}
	if enterpriseDomain != "" && enterpriseDomain != githubCopilotOAuthDefaultDomain {
		return "https://copilot-api." + enterpriseDomain
	}
	return DefaultBaseURL
}

func startGitHubCopilotDeviceFlow(ctx context.Context, client *http.Client, domain string) (githubCopilotDeviceCodeResponse, error) {
	urls := githubCopilotOAuthURLs(domain)
	form := url.Values{}
	form.Set("client_id", githubCopilotOAuthClientID)
	form.Set("scope", "read:user")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urls.deviceCode, strings.NewReader(form.Encode()))
	if err != nil {
		return githubCopilotDeviceCodeResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")

	resp, err := githubCopilotHTTPClient(client).Do(req)
	if err != nil {
		return githubCopilotDeviceCodeResponse{}, githubCopilotContextOrError(ctx, fmt.Errorf("github copilot oauth: request device code: %w", err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return githubCopilotDeviceCodeResponse{}, githubCopilotContextOrError(ctx, fmt.Errorf("github copilot oauth: read device code response: %w", err))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return githubCopilotDeviceCodeResponse{}, fmt.Errorf("github copilot oauth: device code request failed (%d): %s", resp.StatusCode, redact.Preview(string(data), 1024))
	}

	var decoded githubCopilotDeviceCodeResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return githubCopilotDeviceCodeResponse{}, fmt.Errorf("github copilot oauth: decode device code response: %w", err)
	}
	if decoded.DeviceCode == "" || decoded.UserCode == "" || decoded.VerificationURI == "" || decoded.ExpiresIn <= 0 {
		return githubCopilotDeviceCodeResponse{}, fmt.Errorf("github copilot oauth: device code response missing fields")
	}
	verificationURI, err := safeGitHubCopilotVerificationURI(decoded.VerificationURI)
	if err != nil {
		return githubCopilotDeviceCodeResponse{}, err
	}
	decoded.VerificationURI = verificationURI
	return decoded, nil
}

func pollGitHubCopilotDeviceFlow(ctx context.Context, client *http.Client, domain string, device githubCopilotDeviceCodeResponse) (string, error) {
	interval := githubCopilotPollInterval(device)
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)
	slowed := false
	for {
		result, err := pollGitHubCopilotDeviceFlowOnce(ctx, client, domain, device.DeviceCode)
		if err != nil {
			return "", err
		}
		switch result.status {
		case githubCopilotDevicePollComplete:
			return result.token, nil
		case githubCopilotDevicePollSlowDown:
			interval += githubCopilotOAuthSlowDownPollIncrement
			slowed = true
		case githubCopilotDevicePollPending:
		default:
			return "", fmt.Errorf("github copilot oauth: device flow failed")
		}
		if time.Now().Add(interval).After(deadline) {
			if slowed {
				return "", fmt.Errorf("github copilot oauth: device flow timed out after slow_down responses")
			}
			return "", fmt.Errorf("github copilot oauth: device flow timed out")
		}
		if err := githubCopilotSleepContext(ctx, interval); err != nil {
			return "", err
		}
	}
}

func pollGitHubCopilotDeviceFlowOnce(ctx context.Context, client *http.Client, domain string, deviceCode string) (githubCopilotDevicePollResult, error) {
	urls := githubCopilotOAuthURLs(domain)
	form := url.Values{}
	form.Set("client_id", githubCopilotOAuthClientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urls.accessToken, strings.NewReader(form.Encode()))
	if err != nil {
		return githubCopilotDevicePollResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.35.0")

	resp, err := githubCopilotHTTPClient(client).Do(req)
	if err != nil {
		return githubCopilotDevicePollResult{}, githubCopilotContextOrError(ctx, fmt.Errorf("github copilot oauth: poll device token: %w", err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return githubCopilotDevicePollResult{}, githubCopilotContextOrError(ctx, fmt.Errorf("github copilot oauth: read device token response: %w", err))
	}
	var decoded struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return githubCopilotDevicePollResult{}, fmt.Errorf("github copilot oauth: decode device token response: %w", err)
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices && decoded.AccessToken != "" {
		return githubCopilotDevicePollResult{status: githubCopilotDevicePollComplete, token: decoded.AccessToken}, nil
	}
	switch decoded.Error {
	case "authorization_pending":
		return githubCopilotDevicePollResult{status: githubCopilotDevicePollPending}, nil
	case "slow_down":
		return githubCopilotDevicePollResult{status: githubCopilotDevicePollSlowDown}, nil
	case "":
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			return githubCopilotDevicePollResult{}, fmt.Errorf("github copilot oauth: device token response missing fields")
		}
	}
	if decoded.ErrorDescription != "" {
		return githubCopilotDevicePollResult{}, fmt.Errorf("github copilot oauth: device token failed: %s", redact.String(decoded.ErrorDescription))
	}
	return githubCopilotDevicePollResult{}, fmt.Errorf("github copilot oauth: device token failed (%d): %s", resp.StatusCode, redact.Preview(string(data), 1024))
}

func githubCopilotPollInterval(device githubCopilotDeviceCodeResponse) time.Duration {
	if device.Interval <= 0 {
		return githubCopilotOAuthDefaultPollInterval
	}
	return time.Duration(device.Interval * float64(time.Second))
}

func safeGitHubCopilotVerificationURI(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return "", fmt.Errorf("github copilot oauth: untrusted verification_uri in device code response")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("github copilot oauth: untrusted verification_uri in device code response")
	}
	return parsed.String(), nil
}

type githubCopilotOAuthURLSet struct {
	deviceCode   string
	accessToken  string
	copilotToken string
}

func githubCopilotOAuthURLs(domain string) githubCopilotOAuthURLSet {
	return githubCopilotOAuthURLSet{
		deviceCode:   "https://" + domain + "/login/device/code",
		accessToken:  "https://" + domain + "/login/oauth/access_token",
		copilotToken: "https://api." + domain + "/copilot_internal/v2/token",
	}
}

func githubCopilotDomain(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return githubCopilotOAuthDefaultDomain, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		parsed, err = url.Parse("https://" + trimmed)
	}
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("github copilot oauth: invalid enterprise domain")
	}
	return strings.ToLower(parsed.Host), nil
}

func githubCopilotBaseURLFromToken(token string) string {
	for _, part := range strings.Split(token, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || key != "proxy-ep" || value == "" {
			continue
		}
		host := strings.TrimPrefix(value, "proxy.")
		if host == value {
			return "https://" + host
		}
		return "https://api." + host
	}
	return ""
}

func githubCopilotModelPolicyBaseURL(token string, opts GitHubCopilotModelEnableOptions) (string, error) {
	if opts.BaseURL != "" {
		return opts.BaseURL, nil
	}
	domain, err := githubCopilotDomain(opts.EnterpriseDomain)
	if err != nil {
		return "", err
	}
	return GitHubCopilotBaseURL(token, domain), nil
}

func addGitHubCopilotOAuthHeaders(headers http.Header) {
	headers.Set("User-Agent", "GitHubCopilotChat/0.35.0")
	headers.Set("Editor-Version", "vscode/1.107.0")
	headers.Set("Editor-Plugin-Version", "copilot-chat/0.35.0")
	headers.Set("Copilot-Integration-Id", "vscode-chat")
}

func githubCopilotHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func githubCopilotSleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func githubCopilotContextOrError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}
