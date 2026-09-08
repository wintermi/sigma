// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

type regressionCache map[sigma.EmbeddingCacheKey]sigma.Embedding

func (c regressionCache) Get(k sigma.EmbeddingCacheKey) (sigma.Embedding, bool, error) {
	v, ok := c[k]
	return v, ok, nil
}

func (c regressionCache) Set(k sigma.EmbeddingCacheKey, v sigma.Embedding) error {
	c[k] = v
	return nil
}

func TestRegressionEmbeddingCacheEffectiveDimensions(t *testing.T) {
	t.Parallel()
	calls := 0
	transport := regressionTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		n := int(p["dimensions"].(float64))
		data, _ := json.Marshal(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": make([]float32, n)}}})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
	})
	client := openAIEmbeddingsTestClient(t, "https://fake.invalid")
	config := sigma.EmbeddingBatchConfig{CacheNamespace: "test", Cache: make(regressionCache)}
	for _, n := range []int{2, 3} {
		req := sigma.EmbeddingRequest{Inputs: []string{"same"}, ProviderMetadata: map[string]any{"dimensions": n}}
		got, err := client.EmbedBatch(context.Background(), openAIEmbeddingModel(), req, config, sigma.WithEmbeddingAPIKey("synthetic"), sigma.WithEmbeddingHTTPClient(&http.Client{Transport: transport}))
		if err != nil {
			t.Fatal(err)
		}
		if actual := len(got.Embeddings.Vectors[0].Vector); actual != n {
			t.Errorf("requested %d dimensions, got cached %d; HTTP calls=%d", n, actual, calls)
		}
	}
}
