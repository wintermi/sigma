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

// Splitting an oversized input averages part vectors into a lossy synthetic
// embedding, so it must only happen when the caller opts in.
func TestEmbedBatchRequiresOptInToSplitInputsOverMaxBatchBytes(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider()
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sigma.NewClient(sigma.WithRegistry(registry)).EmbedBatch(context.Background(), sigmatest.EmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"abcde"}}, sigma.EmbeddingBatchConfig{MaxBatchBytes: 3})
	if !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("error = %v, want invalid options for an oversized input", err)
	}
	if requests := provider.Requests(); len(requests) != 0 {
		t.Fatalf("provider requests = %d, want none", len(requests))
	}
}
