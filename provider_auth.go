// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

const defaultOAuthRefreshBefore = time.Minute

// AuthResolution is provider auth resolved for one model request.
type AuthResolution struct {
	Credential      Credential
	ProviderEnv     map[string]string
	BaseURL         string
	Headers         map[string]string
	ProviderOptions map[string]any
	Source          string
}

// ProviderAuth describes supported credential flows for one provider.
type ProviderAuth struct {
	APIKey *APIKeyAuth
	OAuth  *OAuthAuth
}

// APIKeyAuth describes stored API-key and environment fallback auth.
type APIKeyAuth struct {
	Name    string
	EnvVars []string
	Resolve APIKeyAuthResolver
}

// APIKeyAuthResolver resolves API-key auth, optionally using a stored credential.
type APIKeyAuthResolver func(context.Context, Model, Options, StoredCredential, bool) (AuthResolution, bool, error)

// OAuthAuth describes a provider OAuth credential flow.
type OAuthAuth struct {
	Name          string
	RefreshBefore time.Duration
	Refresh       OAuthRefreshFunc
	Credential    OAuthCredentialFunc
}

// OAuthRefreshFunc refreshes stored OAuth credentials.
type OAuthRefreshFunc func(context.Context, StoredCredential) (StoredCredential, error)

// OAuthCredentialFunc converts stored OAuth credentials into request credentials.
type OAuthCredentialFunc func(context.Context, Model, Options, StoredCredential) (Credential, error)

// EnvironmentAPIKeyAuth constructs API-key auth that prefers a stored key and
// falls back to ordered environment variables.
func EnvironmentAPIKeyAuth(name string, envVars ...string) *APIKeyAuth {
	return &APIKeyAuth{
		Name:    name,
		EnvVars: append([]string(nil), envVars...),
		Resolve: func(_ context.Context, model Model, _ Options, stored StoredCredential, storedOK bool) (AuthResolution, bool, error) {
			if storedOK && stored.Value != "" {
				source := stored.Source
				if source == "" {
					source = "credential-store:" + string(model.Provider)
				}
				return AuthResolution{
					Credential: Credential{
						Type:     CredentialTypeAPIKey,
						Value:    stored.Value,
						Expiry:   stored.Expiry,
						Source:   source,
						Metadata: copyStringAnyMap(stored.Metadata),
					},
					ProviderEnv: copyStringStringMap(stored.ProviderEnv),
					Source:      source,
				}, true, nil
			}
			if storedOK {
				return AuthResolution{}, false, nil
			}

			sources := envVars
			if len(sources) == 0 {
				sources = EnvironmentAuthResolver{}.EnvVars(model)
			}
			for _, source := range sources {
				if value, ok := os.LookupEnv(source); ok && value != "" {
					return AuthResolution{
						Credential: Credential{
							Type:   CredentialTypeAPIKey,
							Value:  value,
							Source: "env:" + source,
						},
						Source: "env:" + source,
					}, true, nil
				}
			}
			return AuthResolution{}, false, nil
		},
	}
}

// StoredCredentialAuthResolver resolves credentials from a CredentialStore.
type StoredCredentialAuthResolver struct {
	Store    CredentialStore
	Registry *Registry
	Fallback AuthResolver
	Now      func() time.Time
}

// Resolve implements AuthResolver.
func (r StoredCredentialAuthResolver) Resolve(ctx context.Context, model Model, opts Options) (Credential, error) {
	if opts.APIKey != "" {
		return Credential{
			Type:   CredentialTypeAPIKey,
			Value:  opts.APIKey,
			Source: "request:api-key",
		}, nil
	}
	if r.Store == nil {
		return r.resolveFallback(ctx, model, opts, "credential-store")
	}

	stored, ok, err := r.Store.ReadCredential(ctx, model.Provider)
	if err != nil {
		return Credential{}, credentialStoreFailure(model.Provider, err)
	}
	auth, hasAuth := r.providerAuth(model.Provider)
	if ok {
		resolution, resolved, err := r.resolveStored(ctx, model, opts, auth, hasAuth, stored)
		if err != nil {
			return Credential{}, err
		}
		if resolved {
			return resolution.Credential, nil
		}
		return Credential{}, storedCredentialUnsupported(model, stored.Type)
	}

	return r.resolveFallback(ctx, model, opts, "credential-store:"+string(model.Provider))
}

func (r StoredCredentialAuthResolver) resolveStored(
	ctx context.Context,
	model Model,
	opts Options,
	auth ProviderAuth,
	hasAuth bool,
	stored StoredCredential,
) (AuthResolution, bool, error) {
	switch stored.Type {
	case CredentialTypeAPIKey, "":
		apiKey := auth.APIKey
		if apiKey == nil {
			apiKey = EnvironmentAPIKeyAuth("")
		}
		return resolveAPIKeyAuth(ctx, model, opts, *apiKey, stored, true)
	case CredentialTypeOAuthToken:
		if !hasAuth || auth.OAuth == nil {
			return AuthResolution{}, false, nil
		}
		credential, err := r.resolveStoredOAuth(ctx, model, opts, *auth.OAuth, stored)
		if err != nil {
			return AuthResolution{}, false, err
		}
		return AuthResolution{Credential: credential, Source: credential.Source}, true, nil
	default:
		return AuthResolution{}, false, nil
	}
}

func (r StoredCredentialAuthResolver) resolveStoredOAuth(
	ctx context.Context,
	model Model,
	opts Options,
	oauth OAuthAuth,
	stored StoredCredential,
) (Credential, error) {
	credential := stored
	refreshBefore := oauth.RefreshBefore
	if refreshBefore == 0 {
		refreshBefore = defaultOAuthRefreshBefore
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	if credentialNeedsOAuthRefresh(credential, now, refreshBefore) {
		if oauth.Refresh == nil {
			return Credential{}, storedCredentialUnsupported(model, CredentialTypeOAuthToken)
		}
		post, ok, err := r.refreshStoredOAuth(ctx, model.Provider, oauth, now, refreshBefore)
		if err != nil {
			return Credential{}, fmt.Errorf("provider oauth: refresh %s: %w", model.Provider, err)
		}
		if !ok || post.Type != CredentialTypeOAuthToken {
			return Credential{}, unavailableCredential(model, "credential-store:"+string(model.Provider))
		}
		credential = post
	}
	if oauth.Credential == nil {
		return Credential{}, storedCredentialUnsupported(model, CredentialTypeOAuthToken)
	}
	resolved, err := oauth.Credential(ctx, model, opts, credential)
	if err != nil {
		return Credential{}, err
	}
	if resolved.Source == "" {
		resolved.Source = "credential-store:" + string(model.Provider)
	}
	return resolved, nil
}

func (r StoredCredentialAuthResolver) refreshStoredOAuth(
	ctx context.Context,
	provider ProviderID,
	oauth OAuthAuth,
	now func() time.Time,
	refreshBefore time.Duration,
) (StoredCredential, bool, error) {
	credential, ok, err := r.Store.ModifyCredential(ctx, provider, func(current StoredCredential, currentOK bool) (StoredCredential, bool, error) {
		if !currentOK || current.Type != CredentialTypeOAuthToken {
			return StoredCredential{}, false, nil
		}
		if !credentialNeedsOAuthRefresh(current, now, refreshBefore) {
			return StoredCredential{}, false, nil
		}
		refreshed, err := oauth.Refresh(ctx, current)
		if err != nil {
			return StoredCredential{}, false, err
		}
		refreshed.Type = CredentialTypeOAuthToken
		return refreshed, true, nil
	})
	if err != nil {
		return StoredCredential{}, false, credentialStoreFailure(provider, err)
	}
	return credential, ok, nil
}

func credentialNeedsOAuthRefresh(credential StoredCredential, now func() time.Time, refreshBefore time.Duration) bool {
	return !credential.Expiry.IsZero() && !now().Add(refreshBefore).Before(credential.Expiry)
}

func (r StoredCredentialAuthResolver) resolveFallback(ctx context.Context, model Model, opts Options, source string) (Credential, error) {
	if r.Fallback == nil {
		return Credential{}, unavailableCredential(model, source)
	}
	credential, err := r.Fallback.Resolve(ctx, model, opts)
	if err == nil {
		return credential, nil
	}
	if errors.Is(err, ErrCredentialUnavailable) {
		sources := append([]string{source}, credentialErrorSources(err)...)
		return Credential{}, unavailableCredential(model, sources...)
	}
	return Credential{}, err
}

func (r StoredCredentialAuthResolver) providerAuth(provider ProviderID) (ProviderAuth, bool) {
	registry := r.Registry
	if registry == nil {
		registry = DefaultRegistry()
	}
	return registry.ProviderAuth(provider)
}

// ResolveProviderAuth resolves auth using a descriptor and optional store.
func ResolveProviderAuth(ctx context.Context, model Model, opts Options, auth ProviderAuth, store CredentialStore) (AuthResolution, bool, error) {
	var stored StoredCredential
	var storedOK bool
	var err error
	if store != nil {
		stored, storedOK, err = store.ReadCredential(ctx, model.Provider)
		if err != nil {
			return AuthResolution{}, false, credentialStoreFailure(model.Provider, err)
		}
	}
	if storedOK {
		switch stored.Type {
		case CredentialTypeAPIKey, "":
			if auth.APIKey == nil {
				return AuthResolution{}, false, storedCredentialUnsupported(model, stored.Type)
			}
			return resolveAPIKeyAuth(ctx, model, opts, *auth.APIKey, stored, true)
		case CredentialTypeOAuthToken:
			resolver := StoredCredentialAuthResolver{Store: store, Registry: NewRegistry()}
			credential, err := resolver.resolveStoredOAuth(ctx, model, opts, valueOrDefaultOAuth(auth.OAuth), stored)
			if err != nil {
				return AuthResolution{}, false, err
			}
			return AuthResolution{Credential: credential, Source: credential.Source}, true, nil
		default:
			return AuthResolution{}, false, storedCredentialUnsupported(model, stored.Type)
		}
	}
	if auth.APIKey == nil {
		return AuthResolution{}, false, nil
	}
	return resolveAPIKeyAuth(ctx, model, opts, *auth.APIKey, StoredCredential{}, false)
}

func resolveAPIKeyAuth(ctx context.Context, model Model, opts Options, auth APIKeyAuth, stored StoredCredential, storedOK bool) (AuthResolution, bool, error) {
	if auth.Resolve == nil {
		auth.Resolve = EnvironmentAPIKeyAuth(auth.Name, auth.EnvVars...).Resolve
	}
	return auth.Resolve(ctx, model, opts, stored, storedOK)
}

func valueOrDefaultOAuth(oauth *OAuthAuth) OAuthAuth {
	if oauth == nil {
		return OAuthAuth{}
	}
	return *oauth
}

func storedCredentialUnsupported(model Model, typ CredentialType) error {
	return &Error{
		Code:     ErrorUnsupported,
		Message:  fmt.Sprintf("stored credential type %q is not supported for provider auth", typ),
		Provider: model.Provider,
		Model:    model.ID,
	}
}

func cloneProviderAuth(auth ProviderAuth) ProviderAuth {
	if auth.APIKey != nil {
		apiKey := *auth.APIKey
		apiKey.EnvVars = append([]string(nil), auth.APIKey.EnvVars...)
		auth.APIKey = &apiKey
	}
	if auth.OAuth != nil {
		oauth := *auth.OAuth
		auth.OAuth = &oauth
	}
	return auth
}

func registerBuiltinProviderAuths(registry *Registry) {
	for provider := range defaultProviderEnvNames {
		_ = registry.RegisterProviderAuth(provider, ProviderAuth{
			APIKey: EnvironmentAPIKeyAuth(defaultProviderAuthName(provider)),
		})
	}
}

func defaultProviderAuthName(provider ProviderID) string {
	if provider == "" {
		return "API key"
	}
	return string(provider) + " API key"
}
