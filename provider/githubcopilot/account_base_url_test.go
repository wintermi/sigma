// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package githubcopilot_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/githubcopilot"
)

// Business, enterprise, and data-residency Copilot accounts are served from
// the host in the token's proxy-ep field, so their requests must not go to the
// individual endpoint that the catalog metadata names.
func TestRequestsUseTheAccountCopilotEndpoint(t *testing.T) {
	t.Parallel()

	const (
		businessToken   = "tid=test;exp=9999999999;proxy-ep=proxy.business.githubcopilot.com;"
		individualToken = "tid=test;exp=9999999999;proxy-ep=proxy.individual.githubcopilot.com;"
	)
	tests := []struct {
		name    string
		token   string
		stored  bool
		modelID sigma.ModelID
		wantURL string
	}{
		{name: "stored business chat completions", token: businessToken, stored: true, modelID: "gemini-3.5-flash", wantURL: "https://api.business.githubcopilot.com/chat/completions"},
		{name: "stored business anthropic", token: businessToken, stored: true, modelID: "claude-fable-5", wantURL: "https://api.business.githubcopilot.com/v1/messages"},
		{name: "stored individual chat completions", token: individualToken, stored: true, modelID: "gemini-3.5-flash", wantURL: "https://api.individual.githubcopilot.com/chat/completions"},
		{name: "token provider business chat completions", token: businessToken, modelID: "gemini-3.5-flash", wantURL: "https://api.business.githubcopilot.com/chat/completions"},
		{name: "token provider business anthropic", token: businessToken, modelID: "claude-fable-5", wantURL: "https://api.business.githubcopilot.com/v1/messages"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Every request is answered locally, so a misrouted request never
			// reaches the network.
			var mu sync.Mutex
			var urls []string
			httpClient := githubCopilotOAuthTestClient(func(r *http.Request) *http.Response {
				mu.Lock()
				urls = append(urls, r.URL.Scheme+"://"+r.URL.Host+r.URL.Path)
				mu.Unlock()
				return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}
			})

			model, ok := sigma.DefaultRegistry().Model(sigma.ProviderGitHubCopilot, tt.modelID)
			if !ok {
				t.Fatalf("default registry missing github-copilot %s", tt.modelID)
			}
			registry := sigma.NewRegistry()
			register := githubcopilot.Register
			if model.API == sigma.APIAnthropicMessages {
				register = func(registry *sigma.Registry, _ ...githubcopilot.ProviderOption) error {
					return githubcopilot.RegisterAnthropic(registry)
				}
			}
			if err := register(registry); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}

			clientOpts := []sigma.ClientOption{sigma.WithRegistry(registry), sigma.WithHTTPClient(httpClient)}
			expiry := time.Now().Add(time.Hour)
			if tt.stored {
				if err := githubcopilot.RegisterAuth(registry, githubcopilot.GitHubCopilotOAuthTokenProviderOptions{}); err != nil {
					t.Fatalf("RegisterAuth returned error: %v", err)
				}
				store := sigma.NewInMemoryCredentialStore()
				if _, _, err := store.ModifyCredential(context.Background(), sigma.ProviderGitHubCopilot, func(sigma.StoredCredential, bool) (sigma.StoredCredential, bool, error) {
					return sigma.StoredCredential{Type: sigma.CredentialTypeOAuthToken, Value: tt.token, RefreshToken: "github-refresh", Expiry: expiry}, true, nil
				}); err != nil {
					t.Fatal(err)
				}
				clientOpts = append(clientOpts, sigma.WithCredentialStore(store), sigma.WithStoredProviderAuth())
			} else {
				provider := githubcopilot.NewGitHubCopilotOAuthTokenProvider(githubcopilot.GitHubCopilotOAuthCredentials{
					AccessToken:  tt.token,
					RefreshToken: "github-refresh",
					Expiry:       expiry,
				}, githubcopilot.GitHubCopilotOAuthTokenProviderOptions{})
				clientOpts = append(clientOpts, sigma.WithAuthResolver(provider))
			}
			client := sigma.NewClient(clientOpts...)

			_, _ = client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})

			mu.Lock()
			defer mu.Unlock()
			if len(urls) != 1 || urls[0] != tt.wantURL {
				t.Fatalf("request URLs = %v, want [%s]", urls, tt.wantURL)
			}
		})
	}
}
