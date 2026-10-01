// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package vertexai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

type resolutionFunc func(context.Context, sigma.Model, sigma.Options) (sigma.AuthResolution, error)

func (f resolutionFunc) Resolve(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
	panic("plain resolver must not be called")
}

func (f resolutionFunc) ResolveAuthResolution(ctx context.Context, model sigma.Model, opts sigma.Options) (sigma.AuthResolution, error) {
	return f(ctx, model, opts)
}

func TestResolveAuthCredentialModesAndFallback(t *testing.T) {
	t.Parallel()
	failure := errors.New("auth failed")
	for _, tt := range []struct {
		name                  string
		mode                  CredentialMode
		credential            sigma.Credential
		resolveErr            error
		tokenProvider         bool
		tokenType             sigma.CredentialType
		wantCalls, wantTokens int
		wantErr               error
	}{
		{name: "api key", mode: CredentialAPIKey, credential: sigma.Credential{Value: "key"}, wantCalls: 1},
		{name: "api rejects token", mode: CredentialAPIKey, credential: sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "token"}, wantCalls: 1, wantErr: sigma.ErrInvalidOptions},
		{name: "token resolver", mode: CredentialToken, credential: sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "token"}, wantCalls: 1},
		{name: "token rejects untyped key", mode: CredentialToken, credential: sigma.Credential{Value: "key"}, wantCalls: 1, wantErr: sigma.ErrInvalidOptions},
		{name: "explicit token provider", mode: CredentialToken, tokenProvider: true, wantTokens: 1},
		{name: "token provider rejects key", mode: CredentialToken, tokenProvider: true, tokenType: sigma.CredentialTypeAPIKey, wantTokens: 1, wantErr: sigma.ErrInvalidOptions},
		{name: "auto prefers key", credential: sigma.Credential{Value: "key"}, tokenProvider: true, wantCalls: 1},
		{name: "auto placeholder fallback", credential: sigma.Credential{Value: "gcp-vertex-credentials"}, tokenProvider: true, wantCalls: 1, wantTokens: 1},
		{name: "auto empty fallback", tokenProvider: true, wantCalls: 1, wantTokens: 1},
		{name: "auto unavailable fallback", resolveErr: &sigma.CredentialUnavailableError{}, tokenProvider: true, wantCalls: 1, wantTokens: 1},
		{name: "auto hard failure", resolveErr: failure, tokenProvider: true, wantCalls: 1, wantErr: failure},
		{name: "api no fallback", mode: CredentialAPIKey, credential: sigma.Credential{Value: "<placeholder>"}, tokenProvider: true, wantCalls: 1, wantErr: sigma.ErrCredentialUnavailable},
		{name: "auto missing", wantCalls: 1, wantErr: sigma.ErrCredentialUnavailable},
		{name: "auto blank token no fallback", credential: sigma.Credential{Type: sigma.CredentialTypeOAuthToken}, tokenProvider: true, wantCalls: 1, wantErr: sigma.ErrCredentialUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls, tokens := 0, 0
			validity := time.Minute
			model := sigma.Model{Provider: sigma.ProviderGoogleVertex, ID: "test"}
			opts := sigma.Options{OAuthMinimumValidity: &validity, AuthResolver: resolutionFunc(func(_ context.Context, _ sigma.Model, opts sigma.Options) (sigma.AuthResolution, error) {
				calls++
				if opts.OAuthMinimumValidity == nil || *opts.OAuthMinimumValidity != validity {
					t.Fatal("validity was lost")
				}
				return sigma.AuthResolution{Credential: tt.credential, BaseURL: "https://resolved.invalid", Headers: map[string]string{"X-Tenant": "tenant"}}, tt.resolveErr
			})}
			var provider sigma.OAuthTokenProvider
			if tt.tokenProvider {
				provider = sigma.OAuthTokenProviderFunc(func(_ context.Context, _ sigma.Model, opts sigma.Options) (sigma.Credential, error) {
					tokens++
					if opts.OAuthMinimumValidity == nil || *opts.OAuthMinimumValidity != validity {
						t.Fatal("token validity was lost")
					}
					return sigma.Credential{Type: tt.tokenType, Value: "token"}, nil
				})
			}
			resolved, credential, err := ResolveAuth(context.Background(), model, opts, tt.mode, provider)
			if (tt.wantErr == nil && err != nil) || (tt.wantErr != nil && !errors.Is(err, tt.wantErr)) {
				t.Fatalf("error=%v want=%v", err, tt.wantErr)
			}
			if calls != tt.wantCalls || tokens != tt.wantTokens {
				t.Fatalf("resolver=%d token=%d", calls, tokens)
			}
			if err == nil && calls == 1 && tt.resolveErr == nil && resolved.Headers["X-Tenant"] != "tenant" {
				t.Fatal("fallback lost resolved defaults")
			}
			if err == nil && tokens == 1 && credential.Type != sigma.CredentialTypeOAuthToken {
				t.Fatal("token type not normalized")
			}
		})
	}
}
