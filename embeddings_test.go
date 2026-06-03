// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestEmbeddingJSONRoundTripPreservesFields(t *testing.T) {
	t.Parallel()

	req := sigma.EmbeddingRequest{
		Inputs:     []string{"alpha", "beta"},
		Dimensions: 128,
		ProviderMetadata: map[string]any{
			"user": "test",
		},
	}
	roundTrippedReq := assertJSONRoundTrip(t, req)
	if got, want := roundTrippedReq.Dimensions, 128; got != want {
		t.Fatalf("dimensions = %d, want %d", got, want)
	}
	if got, want := len(roundTrippedReq.Inputs), 2; got != want {
		t.Fatalf("inputs = %d, want %d", got, want)
	}

	response := sigma.Embeddings{
		Vectors: []sigma.Embedding{
			{Index: 0, Vector: []float32{0.1, 0.2}},
			{Index: 1, Vector: []float32{0.3, 0.4}},
		},
		Usage:    &sigma.Usage{InputTokens: 4, TotalTokens: 4},
		Cost:     &sigma.Cost{InputCost: 0.01, TotalCost: 0.01, Currency: "USD"},
		Model:    "text-embedding-3-small",
		Provider: sigma.ProviderOpenAI,
		ProviderMetadata: map[string]any{
			"request_id": "req_123",
		},
	}
	roundTrippedResponse := assertJSONRoundTrip(t, response)
	if got, want := roundTrippedResponse.Model, sigma.ModelID("text-embedding-3-small"); got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
	if got, want := len(roundTrippedResponse.Vectors), 2; got != want {
		t.Fatalf("vectors = %d, want %d", got, want)
	}
}

func TestEmbedWithFauxProvider(t *testing.T) {
	t.Parallel()

	expected := sigma.Embeddings{
		Vectors: []sigma.Embedding{
			{Index: 0, Vector: []float32{0.1, 0.2, 0.3}},
			{Index: 1, Vector: []float32{0.4, 0.5, 0.6}},
		},
		Usage:    &sigma.Usage{InputTokens: 8, TotalTokens: 8},
		Cost:     &sigma.Cost{InputCost: 0.00000008, TotalCost: 0.00000008, Currency: "USD"},
		Model:    sigmatest.EmbeddingModelID,
		Provider: sigmatest.ProviderID,
	}
	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{
		Response: expected,
	})
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithDefaultHeader("x-default", "default"),
	)

	req := sigma.EmbeddingRequest{
		Inputs:     []string{"alpha", "beta"},
		Dimensions: 3,
	}
	got, err := client.Embed(
		context.Background(),
		sigmatest.EmbeddingModel(),
		req,
		sigma.WithEmbeddingHeader("x-call", "call"),
		sigma.WithEmbeddingMetadataValue("trace", "enabled"),
		sigma.WithEmbeddingProviderOption(sigmatest.ProviderID, "payloadHook", "test-hook"),
	)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("response = %#v, want %#v", got, expected)
	}

	capture, ok := provider.LastRequest()
	if !ok {
		t.Fatal("LastRequest returned no request")
	}
	if got, want := capture.Request.Dimensions, 3; got != want {
		t.Fatalf("captured dimensions = %d, want %d", got, want)
	}
	if got, want := capture.Options.Headers["x-default"], "default"; got != want {
		t.Fatalf("default header = %q, want %q", got, want)
	}
	if got, want := capture.Options.Headers["x-call"], "call"; got != want {
		t.Fatalf("call header = %q, want %q", got, want)
	}
	if got, want := capture.Options.Metadata["trace"], "enabled"; got != want {
		t.Fatalf("metadata trace = %v, want %v", got, want)
	}
	if got, want := capture.Options.ProviderOptions[sigmatest.ProviderID]["payloadHook"], "test-hook"; got != want {
		t.Fatalf("provider option = %v, want %v", got, want)
	}
}

func TestEmbedValidation(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider()
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	tests := []struct {
		name string
		req  sigma.EmbeddingRequest
	}{
		{name: "missing inputs", req: sigma.EmbeddingRequest{}},
		{name: "empty input", req: sigma.EmbeddingRequest{Inputs: []string{"   "}}},
		{name: "negative dimensions", req: sigma.EmbeddingRequest{Inputs: []string{"ok"}, Dimensions: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := client.Embed(context.Background(), sigmatest.EmbeddingModel(), tt.req)
			if err == nil {
				t.Fatal("Embed returned nil error")
			}
			if !errors.Is(err, sigma.ErrInvalidOptions) {
				t.Fatalf("error = %v, want ErrInvalidOptions", err)
			}
		})
	}
}

func TestEmbedMissingEmbeddingProvider(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := registry.RegisterEmbeddingModel(sigmatest.EmbeddingModel(), sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterEmbeddingModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	response, err := client.Embed(context.Background(), sigmatest.EmbeddingModel(), sigma.EmbeddingRequest{Inputs: []string{"hello"}})
	if err == nil {
		t.Fatal("Embed returned nil error")
	}
	if !errors.Is(err, sigma.ErrNoProvider) {
		t.Fatalf("error = %v, want ErrNoProvider", err)
	}
	if got, want := response.Provider, sigmatest.ProviderID; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
}

func TestEmbedCancellation(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{WaitForCancel: true})
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()

	response, err := client.Embed(ctx, sigmatest.EmbeddingModel(), sigma.EmbeddingRequest{Inputs: []string{"hello"}})
	if err == nil {
		t.Fatal("Embed returned nil error")
	}
	if !errors.Is(err, sigma.ErrAborted) {
		t.Fatalf("error = %v, want ErrAborted", err)
	}
	if got, want := response.Model, sigmatest.EmbeddingModelID; got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
}
