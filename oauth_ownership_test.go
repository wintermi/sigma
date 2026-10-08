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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
	"github.com/wintermi/sigma/provider/githubcopilot"
	"github.com/wintermi/sigma/provider/kimi"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/provider/radius"
	"github.com/wintermi/sigma/provider/xai"
)

type ownershipTransport func(*http.Request) (*http.Response, error)

func (f ownershipTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type ownershipContext struct {
	context.Context
	waiting chan struct{}
	once    *sync.Once
}

func (c ownershipContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestOAuthOwnershipCancellation(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"codex", "anthropic", "copilot", "kimi", "xai", "radius"} {
		for _, failure := range []string{"none", "refresh", "callback"} {
			t.Run(name+"/"+failure, func(t *testing.T) {
				t.Parallel()
				started, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				defer unblock()
				var requests, callbacks atomic.Int32
				token := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"account"}}`)) + ".signature"
				client := &http.Client{Transport: ownershipTransport(func(*http.Request) (*http.Response, error) {
					n := requests.Add(1)
					if failure != "callback" && n == 1 {
						close(started)
						<-release
					}
					if failure == "refresh" && n == 1 {
						// Kimi retries transport errors, so it needs a terminal rejection.
						if name == "kimi" {
							return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`))}, nil
						}
						return nil, errors.New("refresh failed")
					}
					body := fmt.Sprintf(`{"access_token":%q,"token":%q,"refresh_token":"rotated","expires_in":7200,"expires_at":%d}`, token, token, time.Now().Add(2*time.Hour).Unix())
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
				})}
				onRefresh := func(context.Context) error {
					callbacks.Add(1)
					if failure == "callback" {
						close(started)
						<-release
						return errors.New("persistence failed")
					}
					return nil
				}
				provider := ownershipProvider(name, client, onRefresh)
				canceled, cancel := context.WithCancel(context.Background())
				cancel()
				if _, err := provider.Token(canceled, sigma.Model{}, sigma.Options{}); !errors.Is(err, context.Canceled) {
					t.Fatalf("already canceled: %v", err)
				}
				if requests.Load() != 0 {
					t.Fatal("already canceled caller refreshed")
				}
				owner := make(chan error, 1)
				go func() { _, err := provider.Token(context.Background(), sigma.Model{}, sigma.Options{}); owner <- err }()
				waitOwnership(t, started)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				waiting := make(chan struct{})
				waiter := make(chan error, 1)
				go func() {
					_, err := provider.Token(ownershipContext{ctx, waiting, &sync.Once{}}, sigma.Model{}, sigma.Options{})
					waiter <- err
				}()
				waitOwnership(t, waiting)
				cancel()
				select {
				case err := <-waiter:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("waiter: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("canceled waiter blocked")
				}
				if requests.Load() != 1 {
					t.Fatal("waiter started refresh")
				}
				waiting = make(chan struct{})
				reused := make(chan error, 1)
				go func() {
					credential, err := provider.Token(ownershipContext{context.Background(), waiting, &sync.Once{}}, sigma.Model{}, sigma.Options{})
					if err == nil && credential.Value != token {
						err = errors.New("waiter did not reuse refreshed credentials")
					}
					reused <- err
				}()
				waitOwnership(t, waiting)
				select {
				case err := <-reused:
					t.Fatalf("waiter bypassed owner: %v", err)
				default:
				}
				unblock()
				select {
				case err := <-owner:
					if (err != nil) != (failure != "none") {
						t.Fatalf("owner: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("owner blocked")
				}
				select {
				case err := <-reused:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("successful waiter blocked")
				}
				wantRequests := int32(1)
				if failure == "refresh" {
					wantRequests = 2
				}
				if requests.Load() != wantRequests || callbacks.Load() != 1 {
					t.Fatalf("requests=%d callbacks=%d", requests.Load(), callbacks.Load())
				}
			})
		}
	}
}

func waitOwnership(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not reach ownership barrier")
	}
}

func ownershipProvider(name string, client *http.Client, callback func(context.Context) error) sigma.OAuthTokenProvider {
	expiry := time.Now().Add(-time.Hour)
	switch name {
	case "codex":
		return openai.NewCodexOAuthTokenProvider(openai.CodexOAuthCredentials{AccessToken: "old", RefreshToken: "refresh", Expiry: expiry, AccountID: "account"}, openai.CodexOAuthTokenProviderOptions{HTTPClient: client, OnRefresh: func(ctx context.Context, _ openai.CodexOAuthCredentials) error { return callback(ctx) }})
	case "anthropic":
		return anthropic.NewAnthropicOAuthTokenProvider(anthropic.AnthropicOAuthCredentials{AccessToken: "old", RefreshToken: "refresh", Expiry: expiry}, anthropic.AnthropicOAuthTokenProviderOptions{HTTPClient: client, OnRefresh: func(ctx context.Context, _ anthropic.AnthropicOAuthCredentials) error { return callback(ctx) }})
	case "copilot":
		return githubcopilot.NewGitHubCopilotOAuthTokenProvider(githubcopilot.GitHubCopilotOAuthCredentials{AccessToken: "old", RefreshToken: "refresh", Expiry: expiry}, githubcopilot.GitHubCopilotOAuthTokenProviderOptions{HTTPClient: client, OnRefresh: func(ctx context.Context, _ githubcopilot.GitHubCopilotOAuthCredentials) error { return callback(ctx) }})
	case "kimi":
		return kimi.NewKimiCodingOAuthTokenProvider(kimi.KimiCodingOAuthCredentials{AccessToken: "old", RefreshToken: "refresh", Expiry: expiry}, kimi.KimiCodingOAuthTokenProviderOptions{HTTPClient: client, OnRefresh: func(ctx context.Context, _ kimi.KimiCodingOAuthCredentials) error { return callback(ctx) }})
	case "xai":
		return xai.NewXAIOAuthTokenProvider(xai.XAIOAuthCredentials{AccessToken: "old", RefreshToken: "refresh", Expiry: expiry}, xai.XAIOAuthTokenProviderOptions{Client: xai.XAIOAuthClientConfig{ClientID: "test", Scopes: []string{"openid"}}, HTTPClient: client, OnRefresh: func(ctx context.Context, _ xai.XAIOAuthCredentials) error { return callback(ctx) }})
	default:
		return radius.NewRadiusOAuthTokenProvider(radius.RadiusOAuthCredentials{AccessToken: "old", RefreshToken: "refresh", Expiry: expiry}, radius.RadiusOAuthTokenProviderOptions{Client: radius.RadiusOAuthClientConfig{ClientID: "test", Scopes: []string{"openid"}, GatewayURL: "https://example.invalid"}, HTTPClient: client, OnRefresh: func(ctx context.Context, _ radius.RadiusOAuthCredentials) error { return callback(ctx) }})
	}
}
