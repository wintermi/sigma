// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

// A stored OAuth credential owns its provider: when it cannot produce a token,
// the chain must report that failure instead of silently dispatching with an
// environment API key. Environment keys apply only when nothing is stored.
func TestChainAuthResolverDoesNotFallBackFromFailedStoredOAuth(t *testing.T) {
	t.Parallel()

	const provider sigma.ProviderID = "stored-oauth-fallback"
	minimum := 3 * time.Hour
	tests := []struct {
		name      string
		stored    bool
		refresh   func(context.Context, sigma.StoredCredential) (sigma.StoredCredential, error)
		opts      sigma.Options
		wantError bool
	}{
		{
			name:   "refresh misses the requested minimum validity",
			stored: true,
			refresh: func(_ context.Context, stored sigma.StoredCredential) (sigma.StoredCredential, error) {
				stored.Value, stored.Expiry = "new", time.Now().Add(30*time.Minute)
				return stored, nil
			},
			opts:      sigma.Options{OAuthMinimumValidity: &minimum},
			wantError: true,
		},
		{
			name:   "refresh reports the credential unavailable",
			stored: true,
			refresh: func(context.Context, sigma.StoredCredential) (sigma.StoredCredential, error) {
				return sigma.StoredCredential{}, &sigma.CredentialUnavailableError{Provider: provider, Sources: []string{"refresh-token"}}
			},
			wantError: true,
		},
		{
			name: "nothing stored uses the environment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := sigma.NewInMemoryCredentialStore()
			if tt.stored {
				if _, _, err := store.ModifyCredential(context.Background(), provider, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
					return sigma.StoredCredential{Type: sigma.CredentialTypeOAuthToken, Value: "old", RefreshToken: "r", Expiry: time.Now().Add(time.Second)}, true, nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			registry := sigma.NewRegistry()
			if err := registry.RegisterProviderAuth(provider, sigma.ProviderAuth{OAuth: &sigma.OAuthAuth{
				Refresh: tt.refresh,
				Credential: func(_ context.Context, _ sigma.Model, _ sigma.Options, stored sigma.StoredCredential) (sigma.Credential, error) {
					return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: stored.Value, Expiry: stored.Expiry}, nil
				},
			}}); err != nil {
				t.Fatal(err)
			}
			chain := sigma.ChainAuthResolver{
				Client: sigma.StoredCredentialAuthResolver{Store: store, Registry: registry},
				Environment: sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
					return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "env-key", Source: "env:TEST_API_KEY"}, nil
				}),
			}

			credential, err := chain.Resolve(context.Background(), sigma.Model{Provider: provider, ID: "m"}, tt.opts)
			if !tt.wantError {
				if err != nil || credential.Value != "env-key" {
					t.Fatalf("Resolve = %q, %v; want the environment key", credential.Value, err)
				}
				return
			}
			if credential.Value == "env-key" {
				t.Fatal("Resolve fell back to the environment API key")
			}
			if !errors.Is(err, sigma.ErrCredentialUnavailable) {
				t.Fatalf("error = %v, want one wrapping ErrCredentialUnavailable", err)
			}
		})
	}
}
