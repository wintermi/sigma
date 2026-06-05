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

// AddAuthHeader resolves Vertex credentials and applies the correct request header.
func AddAuthHeader(ctx context.Context, req *http.Request, model sigma.Model, opts sigma.Options, config Config, tokenProvider sigma.OAuthTokenProvider) error {
	credential, err := Credential(ctx, model, opts, config, tokenProvider)
	if err != nil {
		return err
	}
	if credential.Value == "" {
		return CredentialUnavailable(model, credential.Source)
	}
	switch credential.Type {
	case sigma.CredentialTypeAPIKey:
		req.Header.Set("X-Goog-Api-Key", credential.Value)
	case sigma.CredentialTypeOAuthToken:
		req.Header.Set("Authorization", "Bearer "+credential.Value)
	default:
		return InvalidOptions(model, fmt.Sprintf("google vertex: unsupported credential type %q", credential.Type), nil)
	}
	return nil
}

// Credential resolves a Vertex credential using Sigma auth and optional token provider fallback.
func Credential(ctx context.Context, model sigma.Model, opts sigma.Options, config Config, tokenProvider sigma.OAuthTokenProvider) (sigma.Credential, error) {
	switch config.CredentialMode {
	case CredentialToken:
		return tokenCredential(ctx, model, opts, tokenProvider)
	case CredentialAPIKey:
		return resolvedCredential(ctx, model, opts, sigma.CredentialTypeAPIKey)
	default:
		credential, err := resolvedCredential(ctx, model, opts, "")
		if err == nil {
			if credential.Type == sigma.CredentialTypeAPIKey && APIKeyUnavailable(credential.Value) {
				if tokenProvider != nil {
					return tokenCredential(ctx, model, opts, tokenProvider)
				}
				return sigma.Credential{}, CredentialUnavailable(model, credential.Source)
			}
			return credential, nil
		}
		if !errors.Is(err, sigma.ErrCredentialUnavailable) {
			return sigma.Credential{}, err
		}
		if tokenProvider == nil {
			return sigma.Credential{}, err
		}
		return tokenCredential(ctx, model, opts, tokenProvider)
	}
}

func resolvedCredential(ctx context.Context, model sigma.Model, opts sigma.Options, want sigma.CredentialType) (sigma.Credential, error) {
	if opts.AuthResolver == nil {
		return sigma.Credential{}, CredentialUnavailable(model, "auth-resolver")
	}
	credential, err := opts.AuthResolver.Resolve(ctx, model, opts)
	if err != nil {
		if errors.Is(err, sigma.ErrCredentialUnavailable) {
			return sigma.Credential{}, err
		}
		return sigma.Credential{}, AuthError(model, "google vertex: resolve credential: "+err.Error(), err)
	}
	if credential.Type == "" {
		credential.Type = sigma.CredentialTypeAPIKey
	}
	if want == sigma.CredentialTypeAPIKey && APIKeyUnavailable(credential.Value) {
		return sigma.Credential{}, CredentialUnavailable(model, credential.Source)
	}
	if want != "" && credential.Type != want {
		return sigma.Credential{}, InvalidOptions(model, fmt.Sprintf("google vertex: credential mode %q requires %q credential, got %q", want, want, credential.Type), nil)
	}
	return credential, nil
}

func tokenCredential(ctx context.Context, model sigma.Model, opts sigma.Options, tokenProvider sigma.OAuthTokenProvider) (sigma.Credential, error) {
	if tokenProvider == nil {
		return resolvedCredential(ctx, model, opts, sigma.CredentialTypeOAuthToken)
	}
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
	if credential.Type != sigma.CredentialTypeOAuthToken {
		return sigma.Credential{}, InvalidOptions(model, fmt.Sprintf("google vertex: token provider returned %q credential", credential.Type), nil)
	}
	return credential, nil
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
