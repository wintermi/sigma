// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestRegressionRetrievalFailedInsert(t *testing.T) {
	t.Parallel()
	provider := sigmatest.NewFauxEmbeddingProvider(
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1, 0}}, {Index: 1, Vector: []float32{0, 0}}}}},
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1, 0}}}}})
	index := sigma.NewInMemoryRetrievalIndex(retrievalTestClient(t, provider), sigmatest.EmbeddingModel(), sigma.InMemoryRetrievalIndexConfig{Dimensions: 2})
	err := index.AddChunks(context.Background(), []sigma.RetrievalChunk{{ID: "first", Text: "first"}, {ID: "invalid", Text: "second"}})
	if err == nil {
		t.Fatal("expected invalid-vector error")
	}
	t.Logf("insert error: %v", err)
	got, err := index.Search(context.Background(), "first", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("failed insertion retained %d chunks: %s", len(got), got[0].Chunk.ID)
	}
}

func TestRegressionCacheHitValidation(t *testing.T) {
	t.Parallel()
	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1, 0}}}}})
	client := retrievalTestClient(t, provider)
	req := sigma.EmbeddingQuery("same")
	config := sigma.EmbeddingBatchConfig{CacheNamespace: "test", Cache: newTestEmbeddingCache()}
	if _, err := client.EmbedBatch(context.Background(), sigmatest.EmbeddingModel(), req, config); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.EmbedBatch(ctx, sigmatest.EmbeddingModel(), req, config); err == nil {
		t.Error("canceled cached call succeeded")
	}
	if _, err := client.EmbedBatch(context.Background(), sigmatest.EmbeddingModel(), req, config, sigma.WithEmbeddingTimeout(-1)); err == nil {
		t.Error("invalid options accepted on cache hit")
	}
}

func TestRegressionSignedEmptyTextPersistence(t *testing.T) {
	t.Parallel()
	block := sigma.Text("")
	block.ProviderSignature = "AAAAAAAAAAAAAAAAAAAAAA=="
	req := sigma.Request{Messages: []sigma.Message{{Role: sigma.RoleAssistant, Provider: sigma.ProviderGoogle, API: sigma.APIGoogleGenerativeAI, Model: "gemini-test", Content: []sigma.ContentBlock{block}}}}
	if _, err := sigma.MarshalRequest(req); err != nil {
		t.Fatalf("valid signed empty replay block rejected: %v", err)
	}
}

func TestRetrievalFailedInsertPreservesExistingIndex(t *testing.T) {
	t.Parallel()
	for _, dims := range []int{0, 2} {
		t.Run(fmt.Sprint(dims), func(t *testing.T) {
			t.Parallel()
			vector := func(v ...float32) sigmatest.EmbeddingScript {
				return sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: v}}}}
			}
			provider := sigmatest.NewFauxEmbeddingProvider(vector(1, 0), sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{0, 1}}, {Index: 1, Vector: []float32{1, 0, 0}}}}}, vector(1, 0))
			index := sigma.NewInMemoryRetrievalIndex(retrievalTestClient(t, provider), sigmatest.EmbeddingModel(), sigma.InMemoryRetrievalIndexConfig{Dimensions: dims})
			if err := index.AddChunks(context.Background(), []sigma.RetrievalChunk{{ID: "original", Text: "original"}}); err != nil {
				t.Fatal(err)
			}
			if err := index.AddChunks(context.Background(), []sigma.RetrievalChunk{{ID: "pending", Text: "pending"}, {ID: "bad", Text: "bad"}}); err == nil {
				t.Fatal("accepted mismatched dimensions")
			}
			results, err := index.Search(context.Background(), "original", 10)
			if err != nil || len(results) != 1 || results[0].Chunk.ID != "original" {
				t.Fatalf("existing index changed: %#v, %v", results, err)
			}
		})
	}
}

func TestSignedEmptyTextRoundTripAndUnsignedRejection(t *testing.T) {
	t.Parallel()
	for _, block := range []sigma.ContentBlock{{Type: sigma.ContentBlockText, Signature: "opaque"}, {Type: sigma.ContentBlockText, ProviderSignature: "opaque"}} {
		req := sigma.Request{Messages: []sigma.Message{{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{block, sigma.ToolCallBlock("call1", "lookup", map[string]any{"id": json.Number("9007199254740993")})}}}}
		data, err := sigma.MarshalRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := sigma.UnmarshalRequest(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(req, restored) {
			t.Fatalf("roundtrip changed: %#v", restored)
		}
	}
	for _, role := range []sigma.Role{sigma.RoleAssistant, sigma.RoleUser} {
		block := sigma.Text("")
		if role == sigma.RoleUser {
			block.ProviderSignature = "opaque"
		}
		if _, err := sigma.MarshalRequest(sigma.Request{Messages: []sigma.Message{{Role: role, Content: []sigma.ContentBlock{block}}}}); err == nil {
			t.Fatalf("accepted empty %s text", role)
		}
	}
}

func TestRetrievalFailureDoesNotPublishInferredDimensions(t *testing.T) {
	t.Parallel()
	for _, cancelInsertion := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelInsertion), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bad := []float32{0, 0}
			if cancelInsertion {
				bad = []float32{1, 0}
			}
			provider := sigmatest.NewFauxEmbeddingProvider(
				sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1, 0}}, {Index: 1, Vector: bad}}}},
				sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1, 0, 0}}}}},
				sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1, 0, 0}}}}},
			)
			config := sigma.InMemoryRetrievalIndexConfig{Batch: sigma.EmbeddingBatchConfig{Progress: func(p sigma.EmbeddingBatchProgress) error {
				if cancelInsertion && p.Phase == sigma.EmbeddingBatchPhaseBatchSuccess {
					cancel()
				}
				return nil
			}}}
			index := sigma.NewInMemoryRetrievalIndex(retrievalTestClient(t, provider), sigmatest.EmbeddingModel(), config)
			if err := index.AddChunks(ctx, []sigma.RetrievalChunk{{Text: "first"}, {Text: "second"}}); err == nil {
				t.Fatal("expected failed insertion")
			}
			if err := index.AddChunks(context.Background(), []sigma.RetrievalChunk{{ID: "valid", Text: "valid"}}); err != nil {
				t.Fatalf("failed insertion locked dimensions: %v", err)
			}
			results, err := index.Search(context.Background(), "valid", 10)
			if err != nil || len(results) != 1 || results[0].Chunk.ID != "valid" {
				t.Fatalf("index = %#v, %v", results, err)
			}
		})
	}
}
