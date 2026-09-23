// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/bedrock"
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/openai"
)

type wireBedrockCredentials struct{}

func (wireBedrockCredentials) Detect(context.Context, sigma.Model, sigma.Options, bedrock.Config) (bedrock.CredentialInfo, error) {
	return bedrock.CredentialInfo{BearerToken: "synthetic"}, nil
}

type embeddingWireCase struct {
	name     string
	model    sigma.EmbeddingModel
	provider sigma.EmbeddingProvider
	shape    string
}

func embeddingWireCases() []embeddingWireCase {
	vertex := google.NewVertexEmbeddingsProvider(google.WithVertexConfig(google.VertexConfig{ProjectID: "test", Location: "global", CredentialMode: google.VertexCredentialAPIKey}))
	bedrockProvider := bedrock.NewEmbeddingsProvider(bedrock.WithRegion("us-east-1"), bedrock.WithCredentialDetector(wireBedrockCredentials{}))
	return []embeddingWireCase{
		{"openai", sigma.EmbeddingModel{ID: "test", Provider: sigma.ProviderOpenAI, API: sigma.EmbeddingAPIOpenAIEmbeddings}, openai.NewEmbeddingsProvider(), `{"data":[{"index":0,"embedding":%s}]}`},
		{"gemini", sigma.EmbeddingModel{ID: "test", Provider: sigma.ProviderGoogle, API: sigma.EmbeddingAPIGoogleEmbeddings}, google.NewEmbeddingsProvider(), `{"embeddings":[{"values":%s}]}`},
		{"vertex", sigma.EmbeddingModel{ID: "test", Provider: sigma.ProviderGoogleVertex, API: sigma.EmbeddingAPIGoogleVertexEmbeddings}, vertex, `{"predictions":[{"embeddings":{"values":%s}}]}`},
		{"titan", sigma.EmbeddingModel{ID: "amazon.titan-embed-text-v2:0", Provider: sigma.ProviderAmazonBedrock, API: sigma.EmbeddingAPIBedrockEmbeddings}, bedrockProvider, `{"embedding":%s}`},
		{"nova", sigma.EmbeddingModel{ID: "amazon.nova-2-multimodal-embeddings-v1:0", Provider: sigma.ProviderAmazonBedrock, API: sigma.EmbeddingAPIBedrockEmbeddings}, bedrockProvider, `{"embeddings":[{"embedding":%s}]}`},
		{"cohere flat", sigma.EmbeddingModel{ID: "cohere.embed-v4:0", Provider: sigma.ProviderAmazonBedrock, API: sigma.EmbeddingAPIBedrockEmbeddings}, bedrockProvider, `{"embeddings":[%s]}`},
		{"cohere float", sigma.EmbeddingModel{ID: "cohere.embed-v4:0", Provider: sigma.ProviderAmazonBedrock, API: sigma.EmbeddingAPIBedrockEmbeddings}, bedrockProvider, `{"embeddings":{"float":[%s]}}`},
	}
}

func TestEmbeddingMalformedSuccessKeepsAttemptsAndSkipsCache(t *testing.T) {
	t.Parallel()
	for _, tt := range embeddingWireCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := sigma.NewRegistry()
			if err := registry.RegisterEmbeddingProvider(tt.model.Provider, tt.provider); err != nil {
				t.Fatal(err)
			}
			if err := registry.RegisterEmbeddingModel(tt.model); err != nil {
				t.Fatal(err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry))
			requests := 0
			transport := &http.Client{Transport: ownershipTransport(func(*http.Request) (*http.Response, error) {
				requests++
				status, body := http.StatusOK, fmt.Sprintf(tt.shape, "[null,1]")
				if requests%2 == 1 {
					status, body = http.StatusServiceUnavailable, `{"error":"retry"}`
				}
				return &http.Response{StatusCode: status, Header: http.Header{"X-Request-Id": []string{"request"}, "X-Amzn-Requestid": []string{"request"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			opts := []sigma.EmbeddingOption{sigma.WithEmbeddingAPIKey("synthetic"), sigma.WithEmbeddingHTTPClient(transport), sigma.WithEmbeddingMaxRetries(1)}
			result, err := client.Embed(context.Background(), tt.model, sigma.EmbeddingQuery("input"), opts...)
			assertMalformedEmbedding(t, tt.model, result, err)
			cache := newTestEmbeddingCache()
			for range 2 {
				batch, err := client.EmbedBatch(context.Background(), tt.model, sigma.EmbeddingQuery("input"), sigma.EmbeddingBatchConfig{Cache: cache, CacheNamespace: "test"}, opts...)
				assertMalformedEmbedding(t, tt.model, batch.Embeddings, err)
				if len(cache.values) != 0 {
					t.Fatal("malformed vectors reached cache")
				}
			}
			if requests != 6 {
				t.Fatalf("requests=%d, malformed response reused", requests)
			}
		})
	}
}

func assertMalformedEmbedding(t *testing.T, model sigma.EmbeddingModel, result sigma.Embeddings, err error) {
	t.Helper()
	var providerErr *sigma.ProviderError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != 200 || providerErr.RequestID != "request" || providerErr.Model != model.ID || providerErr.Provider != model.Provider {
		t.Fatalf("provider error: %#v (%v)", providerErr, err)
	}
	if len(result.Vectors) != 0 || len(result.Attempts) != 2 || result.Attempts[0].StatusCode != 503 || result.Attempts[1].StatusCode != 200 || result.Attempts[1].RequestID != "request" {
		t.Fatalf("result=%#v", result)
	}
}
