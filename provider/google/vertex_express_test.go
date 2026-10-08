// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"context"
	"testing"

	"github.com/wintermi/sigma"
)

// Vertex express mode authenticates with an API key alone, without a project
// or location, through the global publisher model route.
func TestVertexAPIKeyWithoutProjectUsesExpressRoute(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ID: "gemini-2.5-flash", Provider: sigma.ProviderGoogleVertex, API: sigma.APIGoogleVertex}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}
	provider := NewVertexProvider()

	httpReq, err := provider.newRequest(context.Background(), model, req, sigma.Options{AuthResolver: vertexAPIKeyResolver("express-key")})
	if err != nil {
		t.Fatalf("newRequest returned error: %v", err)
	}
	if got, want := httpReq.URL.String(), "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-2.5-flash:streamGenerateContent?alt=sse"; got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}
