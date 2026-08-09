// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
	"github.com/wintermi/sigma/provider/githubcopilot"
	"github.com/wintermi/sigma/provider/kimi"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/provider/radius"
	"github.com/wintermi/sigma/provider/xai"
)

func TestProviderAuthIdentifiesSubscriptionOAuthFlows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth sigma.ProviderAuth
		want bool
	}{
		{name: "anthropic", auth: anthropic.ProviderAuth(anthropic.AnthropicOAuthTokenProviderOptions{}), want: true},
		{name: "github copilot", auth: githubcopilot.ProviderAuth(githubcopilot.GitHubCopilotOAuthTokenProviderOptions{}), want: true},
		{name: "kimi coding", auth: kimi.ProviderAuth(kimi.KimiCodingOAuthTokenProviderOptions{}), want: true},
		{name: "openai codex", auth: openai.CodexProviderAuth(sigma.ProviderOpenAICodex, openai.CodexOAuthTokenProviderOptions{}), want: true},
		{name: "xai", auth: xai.ProviderAuth(xai.XAIOAuthTokenProviderOptions{}), want: true},
		{name: "radius", auth: radius.ProviderAuth(radius.RadiusOAuthTokenProviderOptions{}), want: false},
		{name: "custom", auth: sigma.ProviderAuth{OAuth: &sigma.OAuthAuth{Name: "Custom OAuth"}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.auth.OAuth == nil {
				t.Fatal("oauth descriptor is nil")
			}
			if got := tt.auth.OAuth.IsSubscription; got != tt.want {
				t.Fatalf("oauth subscription = %t, want %t", got, tt.want)
			}
		})
	}
}
