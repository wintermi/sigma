// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package githubcopilot

import (
	"testing"

	"github.com/wintermi/sigma"
)

// A stored base URL predating host validation, or edited in a credential
// store, must not receive the bearer token; the token's own host is used.
func TestRequestBaseURLRevalidatesStoredAccountURL(t *testing.T) {
	t.Parallel()
	const token = "tid=test;exp=9999999999;proxy-ep=proxy.business.githubcopilot.com;"
	model := sigma.Model{ID: "gpt-test", API: sigma.APIOpenAICompletions}
	for _, tt := range []struct {
		name, stored, enterprise, want string
	}{
		{name: "trusted stored", stored: "https://api.enterprise.githubcopilot.com", want: "https://api.enterprise.githubcopilot.com"},
		{name: "untrusted host", stored: "https://attacker.example", want: "https://api.business.githubcopilot.com"},
		{name: "plain http", stored: "http://api.business.githubcopilot.com", want: "https://api.business.githubcopilot.com"},
		{name: "userinfo", stored: "https://user@api.business.githubcopilot.com", want: "https://api.business.githubcopilot.com"},
		{name: "port", stored: "https://api.business.githubcopilot.com:8443", want: "https://api.business.githubcopilot.com"},
		{name: "path", stored: "https://api.business.githubcopilot.com/redirect", want: "https://api.business.githubcopilot.com"},
		{name: "enterprise domain with port", stored: "https://copilot-api.ghe.example.com", enterprise: "ghe.example.com:8443", want: "https://copilot-api.ghe.example.com"},
	} {
		if got := githubCopilotRequestBaseURL(model, tt.stored, token, tt.enterprise); got != tt.want {
			t.Errorf("%s: base URL = %q, want %q", tt.name, got, tt.want)
		}
	}
}
