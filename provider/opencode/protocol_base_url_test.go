// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package opencode

import (
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

// OpenCode serves each protocol from its own /inference endpoint; other base
// URLs, such as proxies and test servers, serve every protocol.
func TestProtocolBaseURL(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ base, route, want string }{
		{ZenBaseURL, "/anthropic/v1", "https://opencode.ai/inference/anthropic/v1"},
		{GoBaseURL, "/google/v1beta", "https://opencode.ai/inference/go/google/v1beta"},
		{"http://127.0.0.1:8080", "/anthropic/v1", "http://127.0.0.1:8080"},
	} {
		if got := protocolBaseURL(tt.base, tt.route); got != tt.want {
			t.Errorf("protocolBaseURL(%q, %q) = %q, want %q", tt.base, tt.route, got, tt.want)
		}
	}
}

// Built-in rows name the endpoint of the protocol they are routed through.
func TestCatalogRowsUseTheirProtocolEndpoint(t *testing.T) {
	t.Parallel()
	routes := map[sigma.API]string{
		sigma.APIOpenAICompletions:  "/openai/v1",
		sigma.APIOpenAIResponses:    "/openai/v1",
		sigma.APIAnthropicMessages:  "/anthropic/v1",
		sigma.APIGoogleGenerativeAI: "/google/v1beta",
	}
	checked := 0
	for _, model := range sigma.DefaultRegistry().ListModels() {
		if model.Provider != sigma.ProviderOpenCode && model.Provider != sigma.ProviderOpenCodeGo {
			continue
		}
		checked++
		baseURL, _ := model.ProviderMetadata["baseURL"].(string)
		route, ok := routes[OpenCodeAPI(model)]
		if !ok || !strings.HasPrefix(baseURL, "https://opencode.ai/inference/") || !strings.HasSuffix(baseURL, route) {
			t.Errorf("%s/%s routed through %s uses %q", model.Provider, model.ID, OpenCodeAPI(model), baseURL)
		}
	}
	if checked == 0 {
		t.Fatal("no OpenCode rows checked")
	}
}
