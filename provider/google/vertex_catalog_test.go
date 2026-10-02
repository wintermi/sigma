// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/wintermi/sigma"
)

func vertexCatalogOptions() sigma.Options {
	return sigma.Options{AuthResolver: sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "fixture-token"}, nil
	})}
}

func TestVertexCatalogImageRequests(t *testing.T) {
	t.Parallel()
	for _, id := range []sigma.ModelID{"gemini-2.5-flash-image", "gemini-3-pro-image", "gemini-3.1-flash-image"} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()
			model, ok := sigma.GetImageModel(sigma.ProviderGoogleVertex, id)
			if !ok {
				t.Fatal("missing image model")
			}
			provider := NewVertexImagesProvider(WithVertexConfig(VertexConfig{ProjectID: "fixture-project", Location: "global"}))
			req, err := provider.newRequest(context.Background(), model, sigma.ImageRequest{Prompt: "landscape", Size: "3:2"}, vertexCatalogOptions())
			if err != nil {
				t.Fatal(err)
			}
			defer req.Body.Close()
			want := "/v1/projects/fixture-project/locations/global/publishers/google/models/" + string(id) + ":generateContent"
			if req.URL.Path != want || req.URL.Host != "aiplatform.googleapis.com" {
				t.Fatalf("wrong route: %s", req.URL)
			}
			var payload struct {
				GenerationConfig struct {
					ImageConfig map[string]any `json:"imageConfig"`
				} `json:"generationConfig"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.GenerationConfig.ImageConfig["aspectRatio"] != "3:2" {
				t.Fatalf("image config = %#v", payload.GenerationConfig.ImageConfig)
			}
			if req.Header.Get("Authorization") != "Bearer fixture-token" {
				t.Fatal("missing credential")
			}
		})
	}
}

func TestVertexCatalogEmbeddingRequests(t *testing.T) {
	t.Parallel()
	for _, id := range []sigma.ModelID{"gemini-embedding-001"} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()
			model, ok := sigma.GetEmbeddingModel(sigma.ProviderGoogleVertex, id)
			if !ok {
				t.Fatal("missing embedding model")
			}
			provider := NewVertexEmbeddingsProvider(WithVertexConfig(VertexConfig{ProjectID: "fixture-project", Location: "us-central1"}))
			req, err := provider.newRequest(context.Background(), model, sigma.EmbeddingRequest{Inputs: []string{"query"}, Dimensions: 256}, vertexCatalogOptions())
			if err != nil {
				t.Fatal(err)
			}
			defer req.Body.Close()
			if !strings.HasSuffix(req.URL.Path, "/publishers/google/models/"+string(id)+":predict") {
				t.Fatalf("wrong route: %s", req.URL)
			}
			var payload struct {
				Instances  []map[string]any `json:"instances"`
				Parameters map[string]any   `json:"parameters"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Instances) != 1 || payload.Instances[0]["content"] != "query" || payload.Parameters["outputDimensionality"] != float64(256) {
				t.Fatalf("payload = %#v", payload)
			}
		})
	}
}

type vertexCatalogTransport func(*http.Request) (*http.Response, error)

func (f vertexCatalogTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestVertexCatalogEmbeddingBatchLimits(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		id    sigma.ModelID
		limit int
	}{
		{"gemini-embedding-001", 1},
	} {
		t.Run(string(tt.id), func(t *testing.T) {
			t.Parallel()
			model, ok := sigma.GetEmbeddingModel(sigma.ProviderGoogleVertex, tt.id)
			if !ok {
				t.Fatal("missing model")
			}
			var mu sync.Mutex
			calls, total := 0, 0
			transport := vertexCatalogTransport(func(req *http.Request) (*http.Response, error) {
				var payload struct {
					Instances []map[string]any `json:"instances"`
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					return nil, err
				}
				if len(payload.Instances) > tt.limit {
					return nil, fmt.Errorf("batch has %d inputs; maximum %d", len(payload.Instances), tt.limit)
				}
				mu.Lock()
				calls++
				total += len(payload.Instances)
				mu.Unlock()
				predictions := make([]any, len(payload.Instances))
				for i := range predictions {
					predictions[i] = map[string]any{"embeddings": map[string]any{"values": []float64{.1, .2}}}
				}
				data, err := json.Marshal(map[string]any{"predictions": predictions})
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(data))), Request: req}, nil
			})
			registry := sigma.DefaultRegistry()
			if err := RegisterVertexEmbeddings(registry, sigma.ProviderGoogleVertex, WithVertexConfig(VertexConfig{ProjectID: "fixture-project", Location: "us-central1"}), WithVertexHTTPClient(&http.Client{Transport: transport})); err != nil {
				t.Fatal(err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry), sigma.WithAuthResolver(vertexCatalogOptions().AuthResolver))
			result, err := client.EmbedBatch(context.Background(), model, sigma.EmbeddingRequest{Inputs: []string{"a", "b", "c", "d", "e", "f"}, Dimensions: 2}, sigma.EmbeddingBatchConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Embeddings.Vectors) != 6 || total != 6 || calls != (6+tt.limit-1)/tt.limit {
				t.Fatalf("vectors=%d total=%d calls=%d", len(result.Embeddings.Vectors), total, calls)
			}
		})
	}
}
