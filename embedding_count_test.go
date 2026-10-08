// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestEmbedRejectsVectorCountMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		vectors int
		wantErr bool
	}{
		{name: "too few", vectors: 1, wantErr: true},
		{name: "too many", vectors: 3, wantErr: true},
		{name: "matching", vectors: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vectors := make([]sigma.Embedding, tt.vectors)
			for i := range vectors {
				vectors[i] = sigma.Embedding{Index: i, Vector: []float32{0.1, 0.2, 0.3}}
			}
			provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: vectors}})
			registry, err := sigmatest.EmbeddingRegistry(provider)
			if err != nil {
				t.Fatalf("EmbeddingRegistry returned error: %v", err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry))
			got, err := client.Embed(context.Background(), sigmatest.EmbeddingModel(), sigma.EmbeddingRequest{Inputs: []string{"alpha", "beta"}})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Embed error = %v, want error %v", err, tt.wantErr)
			}
			if tt.wantErr && got.Vectors != nil {
				t.Fatalf("mismatched vectors returned: %d", len(got.Vectors))
			}
		})
	}
}
