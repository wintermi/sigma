// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

// A caller that asks for a minimum validity must not receive a freshly
// refreshed token that still falls short of it.
func TestOAuthRefreshRejectsTokensShorterThanRequestedMinimum(t *testing.T) {
	t.Parallel()
	minimum := 3 * time.Hour

	t.Run("stored credential", func(t *testing.T) {
		t.Parallel()
		const provider sigma.ProviderID = "stored-oauth-minimum"
		store := sigma.NewInMemoryCredentialStore()
		if _, _, err := store.ModifyCredential(context.Background(), provider, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
			return sigma.StoredCredential{Type: sigma.CredentialTypeOAuthToken, Value: "old", RefreshToken: "r", Expiry: time.Now().Add(time.Minute)}, true, nil
		}); err != nil {
			t.Fatal(err)
		}
		registry := sigma.NewRegistry()
		if err := registry.RegisterProviderAuth(provider, sigma.ProviderAuth{OAuth: &sigma.OAuthAuth{
			Refresh: func(_ context.Context, stored sigma.StoredCredential) (sigma.StoredCredential, error) {
				stored.Value, stored.Expiry = "new", time.Now().Add(30*time.Minute)
				return stored, nil
			},
			Credential: func(_ context.Context, _ sigma.Model, _ sigma.Options, stored sigma.StoredCredential) (sigma.Credential, error) {
				return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: stored.Value, Expiry: stored.Expiry}, nil
			},
		}}); err != nil {
			t.Fatal(err)
		}
		_, err := sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}.Resolve(context.Background(),
			sigma.Model{Provider: provider, ID: "m"}, sigma.Options{OAuthMinimumValidity: &minimum})
		if !errors.Is(err, sigma.ErrCredentialUnavailable) || !strings.Contains(err.Error(), "expires too soon") {
			t.Fatalf("error = %v, want a credential error for the unmet minimum", err)
		}
	})

	for _, name := range []string{"codex", "anthropic", "copilot", "kimi", "xai", "radius"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			token := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"account"}}`)) + ".signature"
			client := &http.Client{Transport: ownershipTransport(func(*http.Request) (*http.Response, error) {
				body := fmt.Sprintf(`{"access_token":%q,"token":%q,"refresh_token":"rotated","expires_in":7200,"expires_at":%d}`, token, token, time.Now().Add(2*time.Hour).Unix())
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			provider := ownershipProvider(name, client, func(context.Context) error { return nil })
			_, err := provider.Token(context.Background(), sigma.Model{}, sigma.Options{OAuthMinimumValidity: &minimum})
			if !errors.Is(err, sigma.ErrCredentialUnavailable) || !strings.Contains(err.Error(), "expires too soon") {
				t.Fatalf("error = %v, want a credential error for the unmet minimum", err)
			}
		})
	}
}
