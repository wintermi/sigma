// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestEmbeddingEmbedderEmbedsQueryWithIntent(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{
		Response: sigma.Embeddings{
			Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{0.1, 0.2}}},
		},
	})
	client := embeddingEmbedderTestClient(t, provider)
	embedder := sigma.NewEmbeddingEmbedder(
		client,
		sigmatest.EmbeddingModel(),
		sigma.EmbeddingEmbedderConfig{Dimensions: 2},
		sigma.WithEmbeddingMetadataValue("trace", "query"),
	)

	vector, err := embedder.EmbedQuery(context.Background(), "find me")
	if err != nil {
		t.Fatalf("EmbedQuery returned error: %v", err)
	}
	if !reflect.DeepEqual(vector, []float32{0.1, 0.2}) {
		t.Fatalf("vector = %#v, want query vector", vector)
	}

	capture, ok := provider.LastRequest()
	if !ok {
		t.Fatal("LastRequest returned no request")
	}
	if !reflect.DeepEqual(capture.Request.Inputs, []string{"find me"}) {
		t.Fatalf("inputs = %#v, want query input", capture.Request.Inputs)
	}
	if got, want := capture.Request.InputType, sigma.EmbeddingInputTypeQuery; got != want {
		t.Fatalf("input type = %q, want %q", got, want)
	}
	if got, want := capture.Request.Dimensions, 2; got != want {
		t.Fatalf("dimensions = %d, want %d", got, want)
	}
	if got, want := capture.Options.Metadata["trace"], "query"; got != want {
		t.Fatalf("metadata trace = %v, want %v", got, want)
	}
}

func TestEmbeddingEmbedderEmbedsDocumentsWithBatchConfig(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{
		Response: sigma.Embeddings{
			Vectors: []sigma.Embedding{
				{Index: 0, Vector: []float32{1}},
				{Index: 1, Vector: []float32{2}},
			},
		},
	})
	client := embeddingEmbedderTestClient(t, provider)
	embedder := sigma.NewEmbeddingEmbedder(
		client,
		sigmatest.EmbeddingModel(),
		sigma.EmbeddingEmbedderConfig{
			Dimensions: 1,
			Batch: sigma.EmbeddingBatchConfig{
				ReuseDuplicateInputs: true,
				MaxBatchInputs:       2,
			},
		},
		sigma.WithEmbeddingHeader("x-test", "documents"),
	)

	vectors, err := embedder.EmbedDocuments(context.Background(), []string{"alpha", "beta", "alpha"})
	if err != nil {
		t.Fatalf("EmbedDocuments returned error: %v", err)
	}
	wantVectors := [][]float32{{1}, {2}, {1}}
	if !reflect.DeepEqual(vectors, wantVectors) {
		t.Fatalf("vectors = %#v, want %#v", vectors, wantVectors)
	}

	requests := provider.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want duplicate-reused request", len(requests))
	}
	if !reflect.DeepEqual(requests[0].Request.Inputs, []string{"alpha", "beta"}) {
		t.Fatalf("inputs = %#v, want unique document inputs", requests[0].Request.Inputs)
	}
	if got, want := requests[0].Request.InputType, sigma.EmbeddingInputTypeDocument; got != want {
		t.Fatalf("input type = %q, want %q", got, want)
	}
	if got, want := requests[0].Request.Dimensions, 1; got != want {
		t.Fatalf("dimensions = %d, want %d", got, want)
	}
	if got, want := requests[0].Options.Headers["x-test"], "documents"; got != want {
		t.Fatalf("header = %q, want %q", got, want)
	}
}

func TestEmbeddingEmbedderPropagatesProviderError(t *testing.T) {
	t.Parallel()

	providerErr := errors.New("provider failed")
	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{Err: providerErr})
	client := embeddingEmbedderTestClient(t, provider)
	embedder := sigma.NewEmbeddingEmbedder(client, sigmatest.EmbeddingModel(), sigma.EmbeddingEmbedderConfig{})

	_, err := embedder.EmbedDocuments(context.Background(), []string{"alpha"})
	if !errors.Is(err, providerErr) {
		t.Fatalf("error = %v, want provider failure", err)
	}
}

func TestEmbeddingEmbedderRejectsUnexpectedQueryVectorCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		vectors  []sigma.Embedding
		wantText string
	}{
		{
			name:     "zero",
			wantText: "returned 0 vectors for 1 inputs",
		},
		{
			name: "multiple",
			vectors: []sigma.Embedding{
				{Index: 0, Vector: []float32{1}},
				{Index: 1, Vector: []float32{2}},
			},
			wantText: "returned 2 vectors for 1 inputs",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{
				Response: sigma.Embeddings{Vectors: tt.vectors},
			})
			client := embeddingEmbedderTestClient(t, provider)
			embedder := sigma.NewEmbeddingEmbedder(client, sigmatest.EmbeddingModel(), sigma.EmbeddingEmbedderConfig{})

			_, err := embedder.EmbedQuery(context.Background(), "find me")
			if err == nil || !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("error = %v, want %q", err, tt.wantText)
			}
		})
	}
}

func TestEmbeddingEmbedderDoesNotNormalizeNewlines(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{
		Response: sigma.Embeddings{
			Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1}}},
		},
	})
	client := embeddingEmbedderTestClient(t, provider)
	embedder := sigma.NewEmbeddingEmbedder(client, sigmatest.EmbeddingModel(), sigma.EmbeddingEmbedderConfig{})

	_, err := embedder.EmbedQuery(context.Background(), "line\none")
	if err != nil {
		t.Fatalf("EmbedQuery returned error: %v", err)
	}
	capture, ok := provider.LastRequest()
	if !ok {
		t.Fatal("LastRequest returned no request")
	}
	if !reflect.DeepEqual(capture.Request.Inputs, []string{"line\none"}) {
		t.Fatalf("inputs = %#v, want raw newline input", capture.Request.Inputs)
	}
}

func embeddingEmbedderTestClient(t *testing.T, provider *sigmatest.FauxEmbeddingProvider) *sigma.Client {
	t.Helper()

	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry))
}
