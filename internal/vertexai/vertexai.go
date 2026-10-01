// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package vertexai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/redact"
)

// CredentialMode selects the Google Vertex AI authentication path.
type CredentialMode string

const (
	// CredentialAuto resolves a sigma credential first, then falls back to the
	// configured token provider when no API key or token is available.
	CredentialAuto CredentialMode = ""
	// CredentialAPIKey requires an API-key credential.
	CredentialAPIKey CredentialMode = "api-key"
	// CredentialToken requires an OAuth token credential.
	CredentialToken CredentialMode = "token"
)

// Config carries common Vertex routing and auth settings.
type Config struct {
	ProjectID      string
	Location       string
	Publisher      string
	APIVersion     string
	CredentialMode CredentialMode
	BaseURL        string
}

// ValidateCredentialMode reports whether mode is one of the supported values.
func ValidateCredentialMode(mode CredentialMode) bool {
	switch mode {
	case CredentialAuto, CredentialAPIKey, CredentialToken:
		return true
	default:
		return false
	}
}

// BaseURL resolves a regional, global, or caller-supplied Vertex base URL.
func BaseURL(config Config) (string, error) {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		if config.Location == "global" {
			baseURL = "https://aiplatform.googleapis.com/" + config.APIVersion
		} else {
			baseURL = "https://" + config.Location + "-aiplatform.googleapis.com/" + config.APIVersion
		}
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("google vertex: invalid base URL %q", baseURL)
	}
	return baseURL, nil
}

// ProjectLocation builds a Vertex project/location resource prefix.
func ProjectLocation(config Config) string {
	return "projects/" + url.PathEscape(config.ProjectID) + "/locations/" + url.PathEscape(config.Location)
}

// PublisherModelResource builds a publisher model resource under a project and location.
func PublisherModelResource(model sigma.ModelID, config Config) string {
	modelID := strings.Trim(string(model), "/")
	switch {
	case strings.HasPrefix(modelID, "projects/"):
		return modelID
	case strings.HasPrefix(modelID, "publishers/"):
		return ProjectLocation(config) + "/" + modelID
	case strings.HasPrefix(modelID, "models/"):
		return ProjectLocation(config) + "/publishers/" + url.PathEscape(config.Publisher) + "/" + modelID
	default:
		return ProjectLocation(config) + "/publishers/" + url.PathEscape(config.Publisher) + "/models/" + url.PathEscape(modelID)
	}
}

// ApplyCredential applies an already resolved Vertex credential.
func ApplyCredential(req *http.Request, model sigma.Model, credential sigma.Credential) error {
	if err := ValidateCredential(model, credential, CredentialAuto); err != nil {
		return err
	}
	if credential.Type == sigma.CredentialTypeAPIKey {
		req.Header.Set("X-Goog-Api-Key", credential.Value)
	} else {
		req.Header.Set("Authorization", "Bearer "+credential.Value)
	}
	return nil
}

// ResolveAuth preserves rich auth defaults and resolves only once per attempt.
// An explicit token provider in token mode takes precedence over the resolver.
func ResolveAuth(ctx context.Context, model sigma.Model, opts sigma.Options, mode CredentialMode, tokenProvider sigma.OAuthTokenProvider) (sigma.Options, sigma.Credential, error) {
	if mode == CredentialToken && tokenProvider != nil {
		credential, err := tokenCredential(ctx, model, opts, tokenProvider)
		return opts, credential, err
	}
	resolved, credential, err := sigma.ResolveAuthForRequest(ctx, model, opts)
	if err != nil {
		if !errors.Is(err, sigma.ErrCredentialUnavailable) {
			return opts, sigma.Credential{}, AuthError(model, "google vertex: resolve credential: "+err.Error(), err)
		}
		if mode == CredentialAuto && tokenProvider != nil {
			credential, err = tokenCredential(ctx, model, opts, tokenProvider)
			return opts, credential, err
		}
		return opts, sigma.Credential{}, err
	}
	if credential.Type == "" {
		credential.Type = sigma.CredentialTypeAPIKey
	}
	if mode == CredentialAuto && credential.Type == sigma.CredentialTypeAPIKey && APIKeyUnavailable(credential.Value) && tokenProvider != nil {
		credential, err = tokenCredential(ctx, model, resolved, tokenProvider)
		return resolved, credential, err
	}
	return resolved, credential, ValidateCredential(model, credential, mode)
}

// ValidateCredential checks the selected credential against the final request mode.
func ValidateCredential(model sigma.Model, credential sigma.Credential, mode CredentialMode) error {
	if !ValidateCredentialMode(mode) {
		return InvalidOptions(model, fmt.Sprintf("google vertex: unsupported credential mode %q", mode), nil)
	}
	if credential.Value == "" || credential.Type == sigma.CredentialTypeAPIKey && APIKeyUnavailable(credential.Value) {
		return CredentialUnavailable(model, credential.Source)
	}
	want := sigma.CredentialType("")
	switch mode {
	case CredentialAuto:
	case CredentialAPIKey:
		want = sigma.CredentialTypeAPIKey
	case CredentialToken:
		want = sigma.CredentialTypeOAuthToken
	}
	if want != "" && credential.Type != want {
		return InvalidOptions(model, fmt.Sprintf("google vertex: credential mode %q requires %q credential, got %q", mode, want, credential.Type), nil)
	}
	if credential.Type != sigma.CredentialTypeAPIKey && credential.Type != sigma.CredentialTypeOAuthToken {
		return InvalidOptions(model, fmt.Sprintf("google vertex: unsupported credential type %q", credential.Type), nil)
	}
	return nil
}

func tokenCredential(ctx context.Context, model sigma.Model, opts sigma.Options, tokenProvider sigma.OAuthTokenProvider) (sigma.Credential, error) {
	credential, err := tokenProvider.Token(ctx, model, opts)
	if err != nil {
		if errors.Is(err, sigma.ErrCredentialUnavailable) {
			return sigma.Credential{}, err
		}
		return sigma.Credential{}, AuthError(model, "google vertex: resolve token: "+err.Error(), err)
	}
	if credential.Type == "" {
		credential.Type = sigma.CredentialTypeOAuthToken
	}
	return credential, ValidateCredential(model, credential, CredentialToken)
}

// CredentialUnavailable returns a typed missing-credential error.
func CredentialUnavailable(model sigma.Model, sources ...string) error {
	return &sigma.CredentialUnavailableError{
		Provider: model.Provider,
		Model:    model.ID,
		Sources:  sources,
	}
}

// APIKeyUnavailable reports whether value is a known placeholder or blank key.
func APIKeyUnavailable(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true
	}
	if trimmed == "gcp-vertex-credentials" {
		return true
	}
	return strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">")
}

// InvalidOptions returns a provider-scoped invalid-options error.
func InvalidOptions(model sigma.Model, message string, err error) error {
	if err == nil {
		err = sigma.ErrInvalidOptions
	}
	return &sigma.Error{
		Code:     sigma.ErrorInvalidOptions,
		Message:  message,
		Provider: model.Provider,
		Model:    model.ID,
		Err:      err,
	}
}

// AuthError returns a provider-scoped auth setup error with secrets redacted.
func AuthError(model sigma.Model, message string, err error) error {
	return &sigma.Error{
		Code:     sigma.ErrorUnsupported,
		Message:  redact.String(message),
		Provider: model.Provider,
		Model:    model.ID,
		Err:      err,
	}
}
