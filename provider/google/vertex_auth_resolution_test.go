// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

type vertexResolutionAuth struct{}

func (vertexResolutionAuth) Resolve(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
	return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "synthetic-token"}, nil
}

func (a vertexResolutionAuth) ResolveAuthResolution(ctx context.Context, model sigma.Model, opts sigma.Options) (sigma.AuthResolution, error) {
	credential, _ := a.Resolve(ctx, model, opts)
	return sigma.AuthResolution{Credential: credential, BaseURL: "https://auth-route.invalid/v1", Headers: map[string]string{"X-Auth-Tenant": "tenant"}, ProviderOptions: map[string]any{"project_id": "resolved-project", "location": "global"}}, nil
}

func TestRegressionVertexHonorsRichAuthRouting(t *testing.T) {
	t.Parallel()
	config := WithVertexConfig(VertexConfig{})
	opts := sigma.Options{AuthResolver: vertexResolutionAuth{}}
	model := sigma.Model{Provider: sigma.ProviderGoogleVertex, API: sigma.APIGoogleVertex, ID: "gemini-test"}
	for _, surface := range []string{"text", "images", "embeddings"} {
		t.Run(surface, func(t *testing.T) {
			var req *http.Request
			var err error
			switch surface {
			case "text":
				req, err = NewVertexProvider(config).newRequest(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}}, opts)
			case "images":
				req, err = NewVertexImagesProvider(config).newRequest(context.Background(), sigma.ImageModel{Provider: model.Provider, API: sigma.ImageAPIGoogleVertexImages, ID: "imagen-test"}, sigma.ImageRequest{Prompt: "hello"}, opts)
			case "embeddings":
				req, err = NewVertexEmbeddingsProvider(config).newRequest(context.Background(), sigma.EmbeddingModel{Provider: model.Provider, API: sigma.EmbeddingAPIGoogleVertexEmbeddings, ID: "embedding-test"}, sigma.EmbeddingRequest{Inputs: []string{"hello"}}, opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			if req.URL.Host != "auth-route.invalid" || req.Header.Get("X-Auth-Tenant") != "tenant" || !strings.Contains(req.URL.Path, "/projects/resolved-project/locations/global/") {
				t.Errorf("auth defaults lost: host=%s tenant=%q", req.URL.Host, req.Header.Get("X-Auth-Tenant"))
			}
		})
	}
}
