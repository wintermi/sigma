// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestAuditEmbeddingDimensions(t *testing.T) {
	t.Parallel()
	for _, tt := range embeddingWireCases() {
		for _, dimension := range []any{3, 2, 2.5, 0, -1, "2", nil} {
			t.Run(fmt.Sprintf("%s/%v", tt.name, dimension), func(t *testing.T) {
				t.Parallel()
				req := sigma.EmbeddingQuery("input")
				req.Dimensions = 3
				req.ProviderMetadata = map[string]any{}
				options := map[string]any{}
				switch tt.name {
				case "openai":
					req.ProviderMetadata["dimensions"] = dimension
				case "gemini", "vertex":
					options["outputDimensionality"] = dimension
				case "titan":
					options["dimensions"] = dimension
				case "titan image":
					options["outputEmbeddingLength"] = dimension
				case "nova":
					options["embeddingDimension"] = dimension
				default:
					options["output_dimension"] = dimension
				}
				calls := 0
				opts := sigma.Options{APIKey: "synthetic", AuthResolver: sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
					return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "synthetic"}, nil
				}), ProviderOptions: map[sigma.ProviderID]map[string]any{tt.model.Provider: options}, HTTPClient: &http.Client{Transport: ownershipTransport(func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": []string{"request"}, "X-Amzn-Requestid": []string{"request"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(tt.shape, "[1,2]")))}, nil
				})}}
				result, err := tt.provider.Embed(context.Background(), tt.model, req, opts)
				switch dimension {
				case 2:
					if err != nil || len(result.Vectors) != 1 {
						t.Fatalf("valid override rejected: %v", err)
					}
				case 3:
					var providerErr *sigma.ProviderError
					if !errors.As(err, &providerErr) || providerErr.StatusCode != 200 || providerErr.RequestID != "request" || len(result.Attempts) != 1 || len(result.Vectors) != 0 || calls != 1 {
						t.Fatalf("malformed response: %+v, %v, calls=%d", result, err, calls)
					}
				default:
					if !errors.Is(err, sigma.ErrInvalidOptions) || calls != 0 {
						t.Fatalf("malformed control reached transport: calls=%d err=%v", calls, err)
					}
				}
			})
		}
	}
}

func TestAuditEmbeddingDimensionsFromFinalAttempt(t *testing.T) {
	t.Parallel()
	for _, tt := range embeddingWireCases() {
		if tt.name != "gemini" && tt.name != "vertex" {
			continue
		}
		for _, metadata := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/metadata=%v", tt.name, metadata), func(t *testing.T) {
				t.Parallel()
				resolutions, calls := 0, 0
				auth := &regressionAuth{resolve: func(context.Context, sigma.Model, sigma.Options) (sigma.AuthResolution, error) {
					resolutions++
					dimension := 3
					if resolutions > 1 {
						dimension = 2
					}
					return sigma.AuthResolution{Credential: sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "synthetic"}, ProviderOptions: map[string]any{"outputDimensionality": dimension}}, nil
				}}
				req := sigma.EmbeddingQuery("input")
				req.Dimensions = 4
				want := 2
				if metadata {
					want = 1
					if tt.name == "gemini" {
						req.ProviderMetadata = map[string]any{"requests": []map[string]any{{"content": map[string]any{"parts": []map[string]any{{"text": "input"}}}, "outputDimensionality": 1}}}
					} else {
						req.ProviderMetadata = map[string]any{"parameters": map[string]any{"outputDimensionality": 1}}
					}
				}
				retries := 1
				opts := sigma.Options{AuthResolver: auth, MaxRetries: &retries, MaxRetryDelay: new(time.Duration(0)), HTTPClient: &http.Client{Transport: ownershipTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					var sent any
					if tt.name == "gemini" {
						sent = payload["requests"].([]any)[0].(map[string]any)["outputDimensionality"]
					} else {
						sent = payload["parameters"].(map[string]any)["outputDimensionality"]
					}
					expected := want
					if !metadata && calls == 1 {
						expected = 3
					}
					if sent != float64(expected) {
						t.Fatalf("sent dimension: %v, want %d", sent, expected)
					}
					status, body := 200, fmt.Sprintf(tt.shape, "[1,2]")
					if metadata {
						body = fmt.Sprintf(tt.shape, "[1]")
					}
					if calls == 1 {
						status, body = 503, `{"error":"retry"}`
					}
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
				})}}
				result, err := tt.provider.Embed(context.Background(), tt.model, req, opts)
				if err != nil || calls != 2 || resolutions != 2 || len(result.Vectors) != 1 || len(result.Vectors[0].Vector) != want {
					t.Fatalf("attempt expectation: %+v err=%v calls=%d auth=%d", result, err, calls, resolutions)
				}
			})
		}
	}
}

func TestAuditOmittedEmbeddingDimensions(t *testing.T) {
	t.Parallel()
	for _, tt := range embeddingWireCases() {
		if tt.name == "nova" || tt.name == "titan image" {
			continue
		} // These payloads always send their defaults.
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.model.DefaultDimensions = 1024
			opts := sigma.Options{AuthResolver: sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
				return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "synthetic"}, nil
			}), HTTPClient: &http.Client{Transport: ownershipTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(tt.shape, "[1,2]")))}, nil
			})}}
			result, err := tt.provider.Embed(context.Background(), tt.model, sigma.EmbeddingQuery("input"), opts)
			if err != nil || len(result.Vectors) != 1 || len(result.Vectors[0].Vector) != 2 {
				t.Fatalf("imposed catalog default: %+v %v", result, err)
			}
		})
	}
}

func TestAuditBatchDimensionConsistency(t *testing.T) {
	t.Parallel()
	script := func(vectors ...[]float32) sigmatest.EmbeddingScript {
		response := sigma.Embeddings{}
		for i, vector := range vectors {
			response.Vectors = append(response.Vectors, sigma.Embedding{Index: i, Vector: vector})
		}
		return sigmatest.EmbeddingScript{Response: response}
	}
	for _, mode := range []string{"mixed response", "empty vector", "split", "cache plus response", "cache only"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			provider := sigmatest.NewFauxEmbeddingProvider()
			client := retrievalTestClient(t, provider)
			model := sigmatest.EmbeddingModel()
			cache := newTestEmbeddingCache()
			config := sigma.EmbeddingBatchConfig{Cache: cache, CacheNamespace: "dimensions", MaxRetries: 3}
			if strings.HasPrefix(mode, "cache") {
				provider.Enqueue(script([]float32{1, 2}), script([]float32{3}))
				for _, text := range []string{"a", "b"} {
					if text == "b" && mode == "cache plus response" {
						break
					}
					if _, err := client.EmbedBatch(context.Background(), model, sigma.EmbeddingDocuments([]string{text}), config); err != nil {
						t.Fatal(err)
					}
				}
			} else if mode == "split" {
				config.MaxBatchInputs = 1
				provider.Enqueue(script([]float32{1, 2}), script([]float32{3}))
			} else if mode == "empty vector" {
				provider.Enqueue(script([]float32{1, 2}, nil))
			} else {
				provider.Enqueue(script([]float32{1, 2}, []float32{3}))
			}
			result, err := client.EmbedBatch(context.Background(), model, sigma.EmbeddingDocuments([]string{"a", "b"}), config)
			if !errors.Is(err, sigma.ErrEmbeddingVectorDimensionMismatch) || len(result.Embeddings.Vectors) != 0 {
				t.Fatalf("inconsistent batch escaped: %+v, %v", result, err)
			}
			wantWrites, wantCalls := 0, 1
			if mode == "split" || mode == "cache plus response" {
				wantWrites, wantCalls = 1, 2
			}
			if mode == "cache only" {
				wantWrites, wantCalls = 2, 2
			}
			if len(cache.values) != wantWrites || len(provider.Requests()) != wantCalls {
				t.Fatalf("writes=%d calls=%d", len(cache.values), len(provider.Requests()))
			}
		})
	}
}

func TestAuditCacheVersionTransition(t *testing.T) {
	t.Parallel()
	script := sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1, 2}}}}}
	provider := sigmatest.NewFauxEmbeddingProvider(script, script)
	client := retrievalTestClient(t, provider)
	cache := newTestEmbeddingCache()
	config := sigma.EmbeddingBatchConfig{Cache: cache, CacheNamespace: "version"}
	call := func() {
		t.Helper()
		if _, err := client.EmbedBatch(context.Background(), sigmatest.EmbeddingModel(), sigma.EmbeddingQuery("input"), config); err != nil {
			t.Fatal(err)
		}
	}
	call()
	for key, value := range cache.values {
		delete(cache.values, key)
		key.Version = 2
		cache.values[key] = value
		break
	}
	call()
	call()
	if len(provider.Requests()) != 2 || len(cache.values) != 2 {
		t.Fatalf("legacy entries must remain but miss, v3 must reuse: calls=%d entries=%d", len(provider.Requests()), len(cache.values))
	}
}

func TestAuditMixedEmbeddingResponsesSkipCache(t *testing.T) {
	t.Parallel()
	bodies := map[string]string{
		"openai":       `{"data":[{"index":0,"embedding":[1,2]},{"index":1,"embedding":[3]}]}`,
		"gemini":       `{"embeddings":[{"values":[1,2]},{"values":[3]}]}`,
		"vertex":       `{"predictions":[{"embeddings":{"values":[1,2]}},{"embeddings":{"values":[3]}}]}`,
		"cohere flat":  `{"embeddings":[[1,2],[3]]}`,
		"cohere float": `{"embeddings":{"float":[[1,2],[3]]}}`,
	}
	for _, tt := range embeddingWireCases() {
		body, ok := bodies[tt.name]
		if !ok {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := sigma.NewRegistry()
			if err := registry.RegisterEmbeddingProvider(tt.model.Provider, tt.provider); err != nil {
				t.Fatal(err)
			}
			if err := registry.RegisterEmbeddingModel(tt.model); err != nil {
				t.Fatal(err)
			}
			calls := 0
			transport := &http.Client{Transport: ownershipTransport(func(*http.Request) (*http.Response, error) {
				calls++
				status, response := 200, body
				if calls == 1 {
					status, response = 503, `{"error":"retry"}`
				}
				return &http.Response{StatusCode: status, Header: http.Header{"X-Request-Id": []string{"request"}, "X-Amzn-Requestid": []string{"request"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})}
			cache := newTestEmbeddingCache()
			result, err := sigma.NewClient(sigma.WithRegistry(registry)).EmbedBatch(context.Background(), tt.model, sigma.EmbeddingDocuments([]string{"a", "b"}), sigma.EmbeddingBatchConfig{Cache: cache, CacheNamespace: "mixed", MaxRetries: 3}, sigma.WithEmbeddingAPIKey("synthetic"), sigma.WithEmbeddingHTTPClient(transport), sigma.WithEmbeddingMaxRetries(1), sigma.WithEmbeddingMaxRetryDelay(0))
			assertMalformedEmbedding(t, tt.model, result.Embeddings, err)
			if len(cache.values) != 0 || calls != 2 {
				t.Fatalf("invalid success retried or cached: calls=%d writes=%d", calls, len(cache.values))
			}
		})
	}
}
