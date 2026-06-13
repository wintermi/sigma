// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestCloudflareRouteDetectionKeepsWorkersAISeparate(t *testing.T) {
	t.Parallel()

	if !isCloudflareRoute(sigma.ProviderCloudflareAIGateway, "https://api.test.invalid") {
		t.Fatal("Cloudflare AI Gateway provider ID was not treated as a gateway route")
	}
	if isCloudflareRoute(sigma.ProviderCloudflareWorkersAI, "https://api.cloudflare.com/client/v4/accounts/acct/ai/v1") {
		t.Fatal("Cloudflare Workers AI direct route was treated as an AI Gateway route")
	}
	if !isCloudflareRoute(sigma.ProviderID("custom-gateway"), "https://gateway.ai.cloudflare.com/v1/acct/gateway/openai") {
		t.Fatal("Cloudflare AI Gateway host was not treated as a gateway route")
	}
}
