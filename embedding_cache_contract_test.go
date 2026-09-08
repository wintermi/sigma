// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestEmbeddingCacheConfigurationAndNamespaceIsolation(t *testing.T) {
	t.Parallel()
	provider := sigmatest.NewFauxEmbeddingProvider()
	client := retrievalTestClient(t, provider)
	cache := newTestEmbeddingCache()
	config := sigma.EmbeddingBatchConfig{Cache: cache, CacheNamespace: "tenant-a"}
	req := sigma.EmbeddingQuery("same input")
	model := sigmatest.EmbeddingModel()
	call := func(req sigma.EmbeddingRequest, config sigma.EmbeddingBatchConfig, opts ...sigma.EmbeddingOption) {
		t.Helper()
		provider.Enqueue(sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1, 0}}}}})
		if _, err := client.EmbedBatch(context.Background(), model, req, config, opts...); err != nil {
			t.Fatal(err)
		}
	}
	call(req, config)
	call(req, config)
	if len(provider.Requests()) != 1 {
		t.Fatal("unchanged request missed cache")
	}
	config.CacheNamespace = "tenant-b"
	call(req, config)
	req.ProviderMetadata = map[string]any{"dimensions": 3}
	call(req, config)
	call(req, config, sigma.WithEmbeddingProviderOptions(model.Provider, map[string]any{"task_type": "query"}))
	call(req, config, sigma.WithEmbeddingMetadata(map[string]any{"purpose": "search"}))
	config.SplitOversized = true
	call(req, config)
	config.SplitPolicy.PreferWhitespace = true
	call(req, config)
	if len(provider.Requests()) != 7 {
		t.Fatalf("configuration changes dispatched %d requests, want 7", len(provider.Requests()))
	}
	req.ProviderMetadata = map[string]any{"a": 1, "b": 2}
	call(req, config)
	req.ProviderMetadata = map[string]any{"b": 2, "a": 1}
	call(req, config)
	if len(provider.Requests()) != 8 {
		t.Fatal("map ordering changed fingerprint")
	}
	for key := range cache.values {
		if key.Version != 2 || key.Namespace == "" || len(key.ConfigurationSHA256) != 64 {
			t.Fatalf("incomplete key: %#v", key)
		}
	}
}

func TestEmbeddingCacheValidationAndLegacyIsolation(t *testing.T) {
	t.Parallel()
	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1}}}}})
	client := retrievalTestClient(t, provider)
	cache := newTestEmbeddingCache()
	req := sigma.EmbeddingQuery("same")
	model := sigmatest.EmbeddingModel()
	config := sigma.EmbeddingBatchConfig{Cache: cache}
	for _, ns := range []string{"", " \t"} {
		config.CacheNamespace = ns
		if _, err := client.EmbedBatch(context.Background(), model, req, config); !errors.Is(err, sigma.ErrInvalidOptions) {
			t.Fatalf("missing namespace: %v", err)
		}
	}
	config.CacheNamespace = "test"
	if _, err := client.EmbedBatch(context.Background(), model, req, config); err != nil {
		t.Fatal(err)
	}
	for key, value := range cache.values {
		delete(cache.values, key)
		key.Version = 0
		key.Namespace = ""
		key.ConfigurationSHA256 = ""
		cache.values[key] = value
	}
	provider.Enqueue(sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{2}}}}})
	got, err := client.EmbedBatch(context.Background(), model, req, config)
	if err != nil || got.Embeddings.Vectors[0].Vector[0] != 2 {
		t.Fatalf("legacy entry reused: %#v, %v", got, err)
	}
	missing := model
	missing.ID = "missing"
	if _, err := client.EmbedBatch(context.Background(), missing, req, config); !errors.Is(err, sigma.ErrModelNotFound) {
		t.Fatalf("missing model: %v", err)
	}
}

func TestEmbeddingBatchAppliesOptionsOnce(t *testing.T) {
	t.Parallel()
	response := sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1}}}}}
	provider := sigmatest.NewFauxEmbeddingProvider(response, response)
	client := retrievalTestClient(t, provider)
	applied := 0
	_, err := client.EmbedBatch(context.Background(), sigmatest.EmbeddingModel(), sigma.EmbeddingRequest{Inputs: []string{"a", "b", "a"}}, sigma.EmbeddingBatchConfig{MaxBatchInputs: 1, ReuseDuplicateInputs: true}, func(opts *sigma.Options) { applied++; opts.Metadata = map[string]any{"applied": applied} })
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || len(provider.Requests()) != 2 {
		t.Fatalf("options applied %d times; requests %d", applied, len(provider.Requests()))
	}
}

type cancelingEmbeddingCache struct {
	cancel context.CancelFunc
	calls  int
}

func (c *cancelingEmbeddingCache) Get(sigma.EmbeddingCacheKey) (sigma.Embedding, bool, error) {
	c.calls++
	c.cancel()
	return sigma.Embedding{Vector: []float32{1}}, true, nil
}
func (*cancelingEmbeddingCache) Set(sigma.EmbeddingCacheKey, sigma.Embedding) error { return nil }
func TestEmbeddingCacheCancellationDuringLookup(t *testing.T) {
	t.Parallel()
	for _, inputs := range [][]string{{"a"}, {"a", "b"}} {
		ctx, cancel := context.WithCancel(context.Background())
		cache := &cancelingEmbeddingCache{cancel: cancel}
		provider := sigmatest.NewFauxEmbeddingProvider()
		client := retrievalTestClient(t, provider)
		_, err := client.EmbedBatch(ctx, sigmatest.EmbeddingModel(), sigma.EmbeddingRequest{Inputs: inputs}, sigma.EmbeddingBatchConfig{Cache: cache, CacheNamespace: "test"})
		cancel()
		if !errors.Is(err, context.Canceled) || cache.calls != 1 || len(provider.Requests()) != 0 {
			t.Fatalf("cancellation: %v, lookups %d", err, cache.calls)
		}
	}
}

func TestEmbeddingCacheCannotBypassRequestValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		request sigma.EmbeddingRequest
		option  sigma.EmbeddingOption
	}{
		{"empty inputs", sigma.EmbeddingRequest{}, nil},
		{"blank input", sigma.EmbeddingQuery(" \t"), nil},
		{"dimensions", sigma.EmbeddingRequest{Inputs: []string{"a"}, Dimensions: -1}, nil},
		{"input type", sigma.EmbeddingRequest{Inputs: []string{"a"}, InputType: "invalid"}, nil},
		{"timeout", sigma.EmbeddingQuery("a"), sigma.WithEmbeddingTimeout(-1)},
		{"retries", sigma.EmbeddingQuery("a"), sigma.WithEmbeddingMaxRetries(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			provider := sigmatest.NewFauxEmbeddingProvider()
			cache := &cancelingEmbeddingCache{cancel: func() { t.Error("cache accessed before validation") }}
			client := retrievalTestClient(t, provider)
			for _, configured := range []bool{false, true} {
				config := sigma.EmbeddingBatchConfig{}
				if configured {
					config.Cache = cache
					config.CacheNamespace = "test"
				}
				_, err := client.EmbedBatch(context.Background(), sigmatest.EmbeddingModel(), tc.request, config, tc.option)
				if !errors.Is(err, sigma.ErrInvalidOptions) {
					t.Fatalf("validation = %v", err)
				}
			}
			if cache.calls != 0 || len(provider.Requests()) != 0 {
				t.Fatal("invalid request dispatched")
			}
		})
	}
	registry := sigma.NewRegistry()
	if err := registry.RegisterEmbeddingModel(sigmatest.EmbeddingModel(), sigma.WithMetadataOnly()); err != nil {
		t.Fatal(err)
	}
	cache := &cancelingEmbeddingCache{cancel: func() { t.Error("cache accessed before provider validation") }}
	_, err := sigma.NewClient(sigma.WithRegistry(registry)).EmbedBatch(context.Background(), sigmatest.EmbeddingModel(), sigma.EmbeddingQuery("a"), sigma.EmbeddingBatchConfig{Cache: cache, CacheNamespace: "test"})
	if !errors.Is(err, sigma.ErrNoProvider) {
		t.Fatalf("unregistered provider: %v", err)
	}
}
