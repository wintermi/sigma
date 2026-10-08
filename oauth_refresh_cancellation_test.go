// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

// A provider may rotate the refresh token as soon as it receives the refresh
// request, so a caller that gives up mid-refresh must not discard the result.
func TestOAuthTokenProvidersKeepRotatedTokensWhenCallerCancelsMidRefresh(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"codex", "anthropic", "copilot", "kimi", "xai", "radius"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			token := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"account"}}`)) + ".signature"
			var requests atomic.Int32
			client := &http.Client{Transport: ownershipTransport(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				cancel()
				if err := r.Context().Err(); err != nil {
					return nil, err
				}
				body := fmt.Sprintf(`{"access_token":%q,"token":%q,"refresh_token":"rotated","expires_in":7200,"expires_at":%d}`, token, token, time.Now().Add(2*time.Hour).Unix())
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var persisted atomic.Int32
			provider := ownershipProvider(name, client, func(ctx context.Context) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				persisted.Add(1)
				return nil
			})

			_, _ = provider.Token(ctx, sigma.Model{}, sigma.Options{})
			if got := persisted.Load(); got != 1 {
				t.Fatalf("rotated credentials persisted %d times, want 1", got)
			}
			credential, err := provider.Token(context.Background(), sigma.Model{}, sigma.Options{})
			if err != nil {
				t.Fatalf("Token after refresh returned error: %v", err)
			}
			if credential.Value != token {
				t.Fatalf("credential value = %q, want refreshed token", credential.Value)
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("refresh requests = %d, want 1", got)
			}
		})
	}
}

func TestStoredOAuthRefreshPersistsRotationWhenCallerCancelsMidRefresh(t *testing.T) {
	t.Parallel()
	const provider sigma.ProviderID = "stored-oauth-cancel"
	store := sigma.NewInMemoryCredentialStore()
	if _, _, err := store.ModifyCredential(context.Background(), provider, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
		return sigma.StoredCredential{Type: sigma.CredentialTypeOAuthToken, Value: "old-access", RefreshToken: "rt-1", Expiry: time.Now().Add(-time.Minute)}, true, nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := sigma.NewRegistry()
	if err := registry.RegisterProviderAuth(provider, sigma.ProviderAuth{OAuth: &sigma.OAuthAuth{
		Refresh: func(refreshCtx context.Context, stored sigma.StoredCredential) (sigma.StoredCredential, error) {
			cancel()
			if err := refreshCtx.Err(); err != nil {
				return sigma.StoredCredential{}, err
			}
			if _, ok := refreshCtx.Deadline(); !ok {
				return sigma.StoredCredential{}, fmt.Errorf("refresh has no deadline")
			}
			stored.Value, stored.RefreshToken, stored.Expiry = "new-access", "rt-2", time.Now().Add(time.Hour)
			return stored, nil
		},
		Credential: func(_ context.Context, _ sigma.Model, _ sigma.Options, stored sigma.StoredCredential) (sigma.Credential, error) {
			return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: stored.Value, Expiry: stored.Expiry}, nil
		},
	}}); err != nil {
		t.Fatal(err)
	}

	resolver := sigma.StoredCredentialAuthResolver{Store: store, Registry: registry}
	_, _ = resolver.Resolve(ctx, sigma.Model{Provider: provider, ID: "m"}, sigma.Options{})

	stored, _, err := store.ReadCredential(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshToken != "rt-2" || stored.Value != "new-access" {
		t.Fatalf("stored credential = %q/%q, want rotated new-access/rt-2", stored.Value, stored.RefreshToken)
	}
}
