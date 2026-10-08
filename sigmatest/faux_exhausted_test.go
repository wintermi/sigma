// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigmatest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestFauxImageAndEmbeddingProvidersFailWhenScriptsAreExhausted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	images := sigmatest.NewFauxImageProvider(sigmatest.ImageScript{})
	if _, err := images.Generate(ctx, sigmatest.ImageModel(), sigma.ImageRequest{Prompt: "first"}, sigma.Options{}); err != nil {
		t.Fatalf("queued image script returned error: %v", err)
	}
	if _, err := images.Generate(ctx, sigmatest.ImageModel(), sigma.ImageRequest{Prompt: "second"}, sigma.Options{}); err == nil || !strings.Contains(err.Error(), "no scripted response queued") {
		t.Fatalf("exhausted image provider error = %v, want no scripted response", err)
	}

	embeddings := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{})
	req := sigma.EmbeddingRequest{Inputs: []string{"text"}}
	if _, err := embeddings.Embed(ctx, sigmatest.EmbeddingModel(), req, sigma.Options{}); err != nil {
		t.Fatalf("queued embedding script returned error: %v", err)
	}
	if _, err := embeddings.Embed(ctx, sigmatest.EmbeddingModel(), req, sigma.Options{}); err == nil || !strings.Contains(err.Error(), "no scripted response queued") {
		t.Fatalf("exhausted embedding provider error = %v, want no scripted response", err)
	}
}
