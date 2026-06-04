// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
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
		InputType:  sigma.EmbeddingInputTypeDocument,
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
	if got, want := roundTrippedReq.InputType, sigma.EmbeddingInputTypeDocument; got != want {
		t.Fatalf("input type = %q, want %q", got, want)
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

func TestEmbeddingRequestHelpers(t *testing.T) {
	t.Parallel()

	query := sigma.EmbeddingQuery("find me")
	if got, want := query.InputType, sigma.EmbeddingInputTypeQuery; got != want {
		t.Fatalf("query input type = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(query.Inputs, []string{"find me"}) {
		t.Fatalf("query inputs = %#v, want single input", query.Inputs)
	}

	texts := []string{"doc 1", "doc 2"}
	documents := sigma.EmbeddingDocuments(texts)
	texts[0] = "mutated"
	if got, want := documents.InputType, sigma.EmbeddingInputTypeDocument; got != want {
		t.Fatalf("documents input type = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(documents.Inputs, []string{"doc 1", "doc 2"}) {
		t.Fatalf("documents inputs = %#v, want cloned inputs", documents.Inputs)
	}

	inputs := []string{"line\none", "unchanged"}
	normalized := sigma.NormalizeEmbeddingNewlines(inputs)
	inputs[0] = "mutated"
	if !reflect.DeepEqual(normalized, []string{"line one", "unchanged"}) {
		t.Fatalf("normalized inputs = %#v, want newline replacement copy", normalized)
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
		InputType:  sigma.EmbeddingInputTypeDocument,
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
	if got, want := capture.Request.InputType, sigma.EmbeddingInputTypeDocument; got != want {
		t.Fatalf("captured input type = %q, want %q", got, want)
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

func TestEmbedBatchReusesDuplicateInputs(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{
		Response: sigma.Embeddings{
			Vectors: []sigma.Embedding{
				{Index: 0, Vector: []float32{1}},
				{Index: 1, Vector: []float32{2}},
			},
			Usage: &sigma.Usage{InputTokens: 4, TotalTokens: 4},
		},
	})
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	got, err := client.EmbedBatch(
		context.Background(),
		sigmatest.EmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"alpha", "beta", "alpha"}},
		sigma.EmbeddingBatchConfig{ReuseDuplicateInputs: true},
	)
	if err != nil {
		t.Fatalf("EmbedBatch returned error: %v", err)
	}
	wantVectors := []sigma.Embedding{
		{Index: 0, Vector: []float32{1}},
		{Index: 1, Vector: []float32{2}},
		{Index: 2, Vector: []float32{1}},
	}
	if !reflect.DeepEqual(got.Embeddings.Vectors, wantVectors) {
		t.Fatalf("vectors = %#v, want %#v", got.Embeddings.Vectors, wantVectors)
	}
	if !reflect.DeepEqual(got.Reused, []bool{false, false, true}) {
		t.Fatalf("reused = %#v, want duplicate marker", got.Reused)
	}
	if got.Summary.RequestCount != 1 || got.Summary.VectorCount != 2 {
		t.Fatalf("summary = %#v, want one provider request and two provider vectors", got.Summary)
	}
	requests := provider.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if !reflect.DeepEqual(requests[0].Request.Inputs, []string{"alpha", "beta"}) {
		t.Fatalf("provider inputs = %#v, want unique inputs", requests[0].Request.Inputs)
	}
}

func TestEmbedBatchUsesExternalCacheAcrossCalls(t *testing.T) {
	t.Parallel()

	cache := newTestEmbeddingCache()
	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{
		Response: sigma.Embeddings{
			Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{7}}},
		},
	})
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	req := sigma.EmbeddingRequest{Inputs: []string{"cache me"}, Dimensions: 3}
	config := sigma.EmbeddingBatchConfig{Cache: cache}

	first, err := client.EmbedBatch(context.Background(), sigmatest.EmbeddingModel(), req, config)
	if err != nil {
		t.Fatalf("first EmbedBatch returned error: %v", err)
	}
	second, err := client.EmbedBatch(context.Background(), sigmatest.EmbeddingModel(), req, config)
	if err != nil {
		t.Fatalf("second EmbedBatch returned error: %v", err)
	}
	if !reflect.DeepEqual(first.Embeddings.Vectors, second.Embeddings.Vectors) {
		t.Fatalf("cached vectors = %#v, want %#v", second.Embeddings.Vectors, first.Embeddings.Vectors)
	}
	if len(provider.Requests()) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.Requests()))
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte("cache me")))
	if len(cache.keys) == 0 {
		t.Fatal("cache recorded no keys")
	}
	for _, key := range cache.keys {
		if got, want := key.InputType, sigma.EmbeddingInputType(""); got != want {
			t.Fatalf("cache input type = %q, want zero value", got)
		}
		if got := key.InputSHA256; got != wantHash {
			t.Fatalf("cache hash = %q, want %q", got, wantHash)
		}
		if strings.Contains(key.InputSHA256, "cache me") {
			t.Fatalf("cache key leaked raw input: %#v", key)
		}
	}
	if !containsEmbeddingTracePhase(second.Summary.Trace, sigma.EmbeddingBatchPhaseCacheHit) {
		t.Fatalf("trace = %#v, want cache hit", second.Summary.Trace)
	}
}

func TestEmbedBatchCacheKeyIncludesInputType(t *testing.T) {
	t.Parallel()

	cache := newTestEmbeddingCache()
	provider := sigmatest.NewFauxEmbeddingProvider(
		sigmatest.EmbeddingScript{
			Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1}}}},
		},
		sigmatest.EmbeddingScript{
			Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{2}}}},
		},
	)
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	config := sigma.EmbeddingBatchConfig{Cache: cache}

	query, err := client.EmbedBatch(context.Background(), sigmatest.EmbeddingModel(), sigma.EmbeddingQuery("same text"), config)
	if err != nil {
		t.Fatalf("query EmbedBatch returned error: %v", err)
	}
	document, err := client.EmbedBatch(context.Background(), sigmatest.EmbeddingModel(), sigma.EmbeddingDocuments([]string{"same text"}), config)
	if err != nil {
		t.Fatalf("document EmbedBatch returned error: %v", err)
	}
	if got, want := len(provider.Requests()), 2; got != want {
		t.Fatalf("provider requests = %d, want separate query and document requests", got)
	}
	if query.Embeddings.Vectors[0].Vector[0] == document.Embeddings.Vectors[0].Vector[0] {
		t.Fatalf("vectors = %#v and %#v, want input-type-isolated cache", query.Embeddings.Vectors, document.Embeddings.Vectors)
	}

	var sawQuery, sawDocument bool
	for _, key := range cache.keys {
		sawQuery = sawQuery || key.InputType == sigma.EmbeddingInputTypeQuery
		sawDocument = sawDocument || key.InputType == sigma.EmbeddingInputTypeDocument
	}
	if !sawQuery || !sawDocument {
		t.Fatalf("cache keys = %#v, want query and document input types", cache.keys)
	}
}

func TestEmbedBatchPropagatesExternalCacheErrors(t *testing.T) {
	t.Parallel()

	t.Run("get", func(t *testing.T) {
		t.Parallel()

		cache := newTestEmbeddingCache()
		cache.getErr = errors.New("cache get failed")
		provider := sigmatest.NewFauxEmbeddingProvider()
		registry, err := sigmatest.EmbeddingRegistry(provider)
		if err != nil {
			t.Fatalf("EmbeddingRegistry returned error: %v", err)
		}
		client := sigma.NewClient(sigma.WithRegistry(registry))

		_, err = client.EmbedBatch(
			context.Background(),
			sigmatest.EmbeddingModel(),
			sigma.EmbeddingRequest{Inputs: []string{"alpha"}},
			sigma.EmbeddingBatchConfig{Cache: cache},
		)
		if err == nil || !strings.Contains(err.Error(), "cache get failed") {
			t.Fatalf("error = %v, want cache get failure", err)
		}
		if len(provider.Requests()) != 0 {
			t.Fatalf("provider requests = %d, want none", len(provider.Requests()))
		}
	})

	t.Run("set", func(t *testing.T) {
		t.Parallel()

		cache := newTestEmbeddingCache()
		cache.setErr = errors.New("cache set failed")
		provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{
			Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1}}}},
		})
		registry, err := sigmatest.EmbeddingRegistry(provider)
		if err != nil {
			t.Fatalf("EmbeddingRegistry returned error: %v", err)
		}
		client := sigma.NewClient(sigma.WithRegistry(registry))

		_, err = client.EmbedBatch(
			context.Background(),
			sigmatest.EmbeddingModel(),
			sigma.EmbeddingRequest{Inputs: []string{"alpha"}},
			sigma.EmbeddingBatchConfig{Cache: cache},
		)
		if err == nil || !strings.Contains(err.Error(), "cache set failed") {
			t.Fatalf("error = %v, want cache set failure", err)
		}
		if len(provider.Requests()) != 1 {
			t.Fatalf("provider requests = %d, want one before set failure", len(provider.Requests()))
		}
	})
}

func TestEmbedBatchSplitsRetryableBatchFailure(t *testing.T) {
	t.Parallel()

	providerErr := sigma.NewProviderError(
		sigmatest.ProviderID,
		sigma.API(sigmatest.EmbeddingAPI),
		sigmatest.EmbeddingModelID,
		429,
		"",
		0,
		[]byte(`{"error":{"message":"rate limited"}}`),
		sigma.ErrProviderResponse,
	)
	provider := sigmatest.NewFauxEmbeddingProvider(
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{
			Attempts: []sigma.EmbeddingAttempt{{
				Provider:   sigmatest.ProviderID,
				API:        sigmatest.EmbeddingAPI,
				Model:      sigmatest.EmbeddingModelID,
				StatusCode: 429,
				RequestID:  "req_failed",
			}},
		}, Err: providerErr},
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{
			Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1}}},
			Usage:   &sigma.Usage{InputTokens: 2, TotalTokens: 2},
			Attempts: []sigma.EmbeddingAttempt{{
				Provider:   sigmatest.ProviderID,
				API:        sigmatest.EmbeddingAPI,
				Model:      sigmatest.EmbeddingModelID,
				StatusCode: 200,
				RequestID:  "req_alpha",
			}},
		}},
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{
			Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{2}}},
			Usage:   &sigma.Usage{InputTokens: 3, TotalTokens: 3},
			Attempts: []sigma.EmbeddingAttempt{{
				Provider:   sigmatest.ProviderID,
				API:        sigmatest.EmbeddingAPI,
				Model:      sigmatest.EmbeddingModelID,
				StatusCode: 200,
				RequestID:  "req_beta",
			}},
		}},
	)
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	got, err := client.EmbedBatch(
		context.Background(),
		sigmatest.EmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"alpha", "beta"}},
		sigma.EmbeddingBatchConfig{MaxRetries: 1},
	)
	if err != nil {
		t.Fatalf("EmbedBatch returned error: %v", err)
	}
	wantVectors := []sigma.Embedding{
		{Index: 0, Vector: []float32{1}},
		{Index: 1, Vector: []float32{2}},
	}
	if !reflect.DeepEqual(got.Embeddings.Vectors, wantVectors) {
		t.Fatalf("vectors = %#v, want %#v", got.Embeddings.Vectors, wantVectors)
	}
	if got.Summary.RequestCount != 2 || got.Summary.ErrorCount != 1 {
		t.Fatalf("summary counts = %#v, want two successes and one error", got.Summary)
	}
	if got.Summary.TotalRequestCount != 3 {
		t.Fatalf("total request count = %d, want 3", got.Summary.TotalRequestCount)
	}
	if got.Summary.StatusBuckets[429] != 1 || got.Summary.StatusBuckets[200] != 2 {
		t.Fatalf("status buckets = %#v, want one 429 and two 200s", got.Summary.StatusBuckets)
	}
	if !reflect.DeepEqual(got.Summary.RequestIDs, []string{"req_failed", "req_alpha", "req_beta"}) {
		t.Fatalf("request ids = %#v, want attempt order", got.Summary.RequestIDs)
	}
	if len(got.Summary.Attempts) != 3 || len(got.Embeddings.Attempts) != 3 {
		t.Fatalf("attempts = summary:%#v embeddings:%#v, want aggregate attempts", got.Summary.Attempts, got.Embeddings.Attempts)
	}
	errorTrace, ok := firstEmbeddingTracePhase(got.Summary.Trace, sigma.EmbeddingBatchPhaseBatchError)
	if !ok {
		t.Fatalf("trace = %#v, want batch error event", got.Summary.Trace)
	}
	if errorTrace.ErrorClass != sigma.ErrorClassRateLimited || !errorTrace.Retryable {
		t.Fatalf("error trace = %#v, want rate-limit retry metadata", errorTrace)
	}
	if len(errorTrace.ProviderAttempts) != 1 || errorTrace.ProviderAttempts[0].RequestID != "req_failed" {
		t.Fatalf("provider attempts in error trace = %#v, want failed request attempt", errorTrace.ProviderAttempts)
	}
	if got.Summary.Usage == nil || got.Summary.Usage.InputTokens != 5 || got.Embeddings.Usage.InputTokens != 5 {
		t.Fatalf("usage = summary:%#v embeddings:%#v, want aggregated input tokens", got.Summary.Usage, got.Embeddings.Usage)
	}
	if got.Summary.Cost == nil || got.Summary.Cost.InputCost != 0.00000005 {
		t.Fatalf("cost = %#v, want aggregated embedding cost", got.Summary.Cost)
	}
	requests := provider.Requests()
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want original plus split requests", len(requests))
	}
	if !reflect.DeepEqual(requests[0].Request.Inputs, []string{"alpha", "beta"}) ||
		!reflect.DeepEqual(requests[1].Request.Inputs, []string{"alpha"}) ||
		!reflect.DeepEqual(requests[2].Request.Inputs, []string{"beta"}) {
		t.Fatalf("requests = %#v, want original batch then split singleton calls", requests)
	}
}

func TestEmbedBatchSplitsByConfiguredInputLimit(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider(
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1}}, {Index: 1, Vector: []float32{2}}}}},
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{3}}, {Index: 1, Vector: []float32{4}}}}},
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{5}}}}},
	)
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	got, err := client.EmbedBatch(
		context.Background(),
		sigmatest.EmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"a", "b", "c", "d", "e"}},
		sigma.EmbeddingBatchConfig{MaxBatchInputs: 2},
	)
	if err != nil {
		t.Fatalf("EmbedBatch returned error: %v", err)
	}
	want := []sigma.Embedding{
		{Index: 0, Vector: []float32{1}},
		{Index: 1, Vector: []float32{2}},
		{Index: 2, Vector: []float32{3}},
		{Index: 3, Vector: []float32{4}},
		{Index: 4, Vector: []float32{5}},
	}
	if !reflect.DeepEqual(got.Embeddings.Vectors, want) {
		t.Fatalf("vectors = %#v, want %#v", got.Embeddings.Vectors, want)
	}
	requests := provider.Requests()
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(requests))
	}
	if !reflect.DeepEqual(requests[0].Request.Inputs, []string{"a", "b"}) ||
		!reflect.DeepEqual(requests[1].Request.Inputs, []string{"c", "d"}) ||
		!reflect.DeepEqual(requests[2].Request.Inputs, []string{"e"}) {
		t.Fatalf("requests = %#v, want input-limit batches", requests)
	}
	if !containsEmbeddingTracePhase(got.Summary.Trace, sigma.EmbeddingBatchPhaseLimitSplit) {
		t.Fatalf("trace = %#v, want limit split event", got.Summary.Trace)
	}
}

func TestEmbedBatchSplitsByByteLimit(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider(
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1}}, {Index: 1, Vector: []float32{2}}}}},
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{3}}}}},
	)
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	_, err = client.EmbedBatch(
		context.Background(),
		sigmatest.EmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"aa", "bb", "cc"}},
		sigma.EmbeddingBatchConfig{MaxBatchBytes: 4},
	)
	if err != nil {
		t.Fatalf("EmbedBatch returned error: %v", err)
	}
	requests := provider.Requests()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if !reflect.DeepEqual(requests[0].Request.Inputs, []string{"aa", "bb"}) ||
		!reflect.DeepEqual(requests[1].Request.Inputs, []string{"cc"}) {
		t.Fatalf("requests = %#v, want byte-limit batches", requests)
	}
}

func TestEmbedBatchUsesModelLimitAndConfigOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		modelLimit  int
		configLimit int
		wantBatches [][]string
	}{
		{
			name:        "model limit",
			modelLimit:  2,
			wantBatches: [][]string{{"a", "b"}, {"c"}},
		},
		{
			name:        "config override",
			modelLimit:  2,
			configLimit: 3,
			wantBatches: [][]string{{"a", "b", "c"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scripts := make([]sigmatest.EmbeddingScript, 0, len(tt.wantBatches))
			for _, batch := range tt.wantBatches {
				vectors := make([]sigma.Embedding, 0, len(batch))
				for i := range batch {
					vectors = append(vectors, sigma.Embedding{Index: i, Vector: []float32{float32(i + 1)}})
				}
				scripts = append(scripts, sigmatest.EmbeddingScript{Response: sigma.Embeddings{Vectors: vectors}})
			}
			provider := sigmatest.NewFauxEmbeddingProvider(scripts...)
			model := sigmatest.EmbeddingModel()
			model.MaxBatchInputs = tt.modelLimit
			registry, err := sigmatest.EmbeddingRegistry(provider, model)
			if err != nil {
				t.Fatalf("EmbeddingRegistry returned error: %v", err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry))

			_, err = client.EmbedBatch(
				context.Background(),
				sigma.EmbeddingModel{ID: model.ID, Provider: model.Provider},
				sigma.EmbeddingRequest{Inputs: []string{"a", "b", "c"}},
				sigma.EmbeddingBatchConfig{MaxBatchInputs: tt.configLimit},
			)
			if err != nil {
				t.Fatalf("EmbedBatch returned error: %v", err)
			}
			requests := provider.Requests()
			if len(requests) != len(tt.wantBatches) {
				t.Fatalf("requests = %d, want %d", len(requests), len(tt.wantBatches))
			}
			for i, want := range tt.wantBatches {
				if !reflect.DeepEqual(requests[i].Request.Inputs, want) {
					t.Fatalf("request %d inputs = %#v, want %#v", i, requests[i].Request.Inputs, want)
				}
			}
		})
	}
}

func TestEmbedBatchSplitsOversizedSingleton(t *testing.T) {
	t.Parallel()

	overflowErr := sigma.NewProviderError(
		sigmatest.ProviderID,
		sigma.API(sigmatest.EmbeddingAPI),
		sigmatest.EmbeddingModelID,
		400,
		"",
		0,
		[]byte(`{"error":{"code":"context_length_exceeded","message":"too many tokens"}}`),
		sigma.ErrContextOverflow,
	)
	provider := sigmatest.NewFauxEmbeddingProvider(
		sigmatest.EmbeddingScript{Err: overflowErr},
		sigmatest.EmbeddingScript{Response: sigma.Embeddings{
			Vectors: []sigma.Embedding{
				{Index: 0, Vector: []float32{1}},
				{Index: 1, Vector: []float32{3}},
			},
		}},
	)
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	var phases []sigma.EmbeddingBatchPhase
	got, err := client.EmbedBatch(
		context.Background(),
		sigmatest.EmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"abcd"}},
		sigma.EmbeddingBatchConfig{
			MaxRetries:     2,
			SplitOversized: true,
			Progress: func(progress sigma.EmbeddingBatchProgress) error {
				phases = append(phases, progress.Phase)
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("EmbedBatch returned error: %v", err)
	}
	wantVectors := []sigma.Embedding{{Index: 0, Vector: []float32{2}}}
	if !reflect.DeepEqual(got.Embeddings.Vectors, wantVectors) {
		t.Fatalf("vectors = %#v, want weighted average", got.Embeddings.Vectors)
	}
	if !containsEmbeddingBatchPhase(phases, sigma.EmbeddingBatchPhaseSplit) {
		t.Fatalf("progress phases = %#v, want split phase", phases)
	}
	requests := provider.Requests()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want original plus split request", len(requests))
	}
	if !reflect.DeepEqual(requests[1].Request.Inputs, []string{"ab", "cd"}) {
		t.Fatalf("split inputs = %#v, want rune midpoint split", requests[1].Request.Inputs)
	}
}

func TestEmbedBatchSplitPolicyUsesSafeBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "newline",
			input: "alpha\nbeta gamma",
			want:  []string{"alpha\n", "beta gamma"},
		},
		{
			name:  "whitespace",
			input: "abcd efgh",
			want:  []string{"abcd ", "efgh"},
		},
		{
			name:  "rune fallback",
			input: "åßç∂",
			want:  []string{"åß", "ç∂"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			overflowErr := sigma.NewProviderError(
				sigmatest.ProviderID,
				sigma.API(sigmatest.EmbeddingAPI),
				sigmatest.EmbeddingModelID,
				400,
				"",
				0,
				[]byte(`{"error":{"code":"request_too_large","message":"request too large"}}`),
				sigma.ErrProviderResponse,
			)
			provider := sigmatest.NewFauxEmbeddingProvider(
				sigmatest.EmbeddingScript{Err: overflowErr},
				sigmatest.EmbeddingScript{Response: sigma.Embeddings{
					Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1}}, {Index: 1, Vector: []float32{3}}},
				}},
			)
			registry, err := sigmatest.EmbeddingRegistry(provider)
			if err != nil {
				t.Fatalf("EmbeddingRegistry returned error: %v", err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry))

			_, err = client.EmbedBatch(
				context.Background(),
				sigmatest.EmbeddingModel(),
				sigma.EmbeddingRequest{Inputs: []string{tt.input}},
				sigma.EmbeddingBatchConfig{MaxRetries: 1, SplitOversized: true},
			)
			if err != nil {
				t.Fatalf("EmbedBatch returned error: %v", err)
			}
			requests := provider.Requests()
			if len(requests) != 2 {
				t.Fatalf("requests = %d, want 2", len(requests))
			}
			if !reflect.DeepEqual(requests[1].Request.Inputs, tt.want) {
				t.Fatalf("split inputs = %#v, want %#v", requests[1].Request.Inputs, tt.want)
			}
		})
	}
}

func TestEmbedBatchRejectsUnsplittableOversizedInput(t *testing.T) {
	t.Parallel()

	overflowErr := sigma.NewProviderError(
		sigmatest.ProviderID,
		sigma.API(sigmatest.EmbeddingAPI),
		sigmatest.EmbeddingModelID,
		400,
		"",
		0,
		[]byte(`{"error":{"code":"request_too_large","message":"request too large"}}`),
		sigma.ErrProviderResponse,
	)
	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{Err: overflowErr})
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	_, err = client.EmbedBatch(
		context.Background(),
		sigmatest.EmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"x"}},
		sigma.EmbeddingBatchConfig{MaxRetries: 1, SplitOversized: true},
	)
	if err == nil || !strings.Contains(err.Error(), "cannot be split further") {
		t.Fatalf("error = %v, want unsplittable input error", err)
	}
}

func TestEmbedBatchRejectsVectorCountMismatch(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider(sigmatest.EmbeddingScript{
		Response: sigma.Embeddings{
			Vectors: []sigma.Embedding{{Index: 0, Vector: []float32{1}}},
		},
	})
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	_, err = client.EmbedBatch(
		context.Background(),
		sigmatest.EmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"alpha", "beta"}},
		sigma.EmbeddingBatchConfig{},
	)
	if err == nil {
		t.Fatal("EmbedBatch returned nil error")
	}
	if !strings.Contains(err.Error(), "returned 1 vectors for 2 inputs") {
		t.Fatalf("error = %v, want vector-count mismatch", err)
	}
}

func TestEmbedBatchProgressCanAbort(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxEmbeddingProvider()
	registry, err := sigmatest.EmbeddingRegistry(provider)
	if err != nil {
		t.Fatalf("EmbeddingRegistry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	abortErr := errors.New("stop embedding batch")

	_, err = client.EmbedBatch(
		context.Background(),
		sigmatest.EmbeddingModel(),
		sigma.EmbeddingRequest{Inputs: []string{"alpha"}},
		sigma.EmbeddingBatchConfig{
			Progress: func(progress sigma.EmbeddingBatchProgress) error {
				if progress.Phase == sigma.EmbeddingBatchPhaseBatchStart {
					return abortErr
				}
				return nil
			},
		},
	)
	if !errors.Is(err, abortErr) {
		t.Fatalf("error = %v, want progress abort", err)
	}
	if len(provider.Requests()) != 0 {
		t.Fatalf("provider requests = %d, want none after progress abort", len(provider.Requests()))
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
		{name: "invalid input type", req: sigma.EmbeddingRequest{Inputs: []string{"ok"}, InputType: "classification"}},
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

func TestEmbeddingVectorUtilities(t *testing.T) {
	t.Parallel()

	dot, err := sigma.DotProduct([]float32{1, 2, 3}, []float32{4, 5, 6})
	if err != nil {
		t.Fatalf("DotProduct returned error: %v", err)
	}
	if dot != 32 {
		t.Fatalf("dot = %v, want 32", dot)
	}

	cosine, err := sigma.CosineSimilarity([]float32{1, 0}, []float32{0, 1})
	if err != nil {
		t.Fatalf("CosineSimilarity returned error: %v", err)
	}
	if math.Abs(cosine) > 1e-12 {
		t.Fatalf("cosine = %v, want 0", cosine)
	}

	normalized, err := sigma.NormalizeEmbeddingVector([]float32{3, 4})
	if err != nil {
		t.Fatalf("NormalizeEmbeddingVector returned error: %v", err)
	}
	if math.Abs(float64(normalized[0])-0.6) > 1e-6 || math.Abs(float64(normalized[1])-0.8) > 1e-6 {
		t.Fatalf("normalized = %#v, want unit vector", normalized)
	}

	combined, err := sigma.CombineEmbeddingVectors([][]float32{{1, 0}, {0, 1}}, []int{3, 1})
	if err != nil {
		t.Fatalf("CombineEmbeddingVectors returned error: %v", err)
	}
	if math.Abs(float64(combined[0])-0.9486833) > 1e-6 || math.Abs(float64(combined[1])-0.31622776) > 1e-6 {
		t.Fatalf("combined = %#v, want normalized weighted average", combined)
	}
}

func TestRankEmbeddingsByCosine(t *testing.T) {
	t.Parallel()

	ranked, err := sigma.RankEmbeddingsByCosine([]float32{1, 0}, []sigma.Embedding{
		{Index: 2, Vector: []float32{1, 0}},
		{Index: 1, Vector: []float32{1, 0}},
		{Index: 3, Vector: []float32{0, 1}},
	})
	if err != nil {
		t.Fatalf("RankEmbeddingsByCosine returned error: %v", err)
	}
	wantIndexes := []int{1, 2, 3}
	for i, want := range wantIndexes {
		if got := ranked[i].Embedding.Index; got != want {
			t.Fatalf("ranked[%d].index = %d, want %d", i, got, want)
		}
	}
	if ranked[0].Score < ranked[2].Score {
		t.Fatalf("ranked scores = %#v, want descending", ranked)
	}
}

func TestEmbeddingVectorUtilityErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "dimension mismatch",
			err: func() error {
				_, err := sigma.DotProduct([]float32{1}, []float32{1, 2})
				return err
			}(),
			want: sigma.ErrEmbeddingVectorDimensionMismatch,
		},
		{
			name: "zero norm",
			err: func() error {
				_, err := sigma.NormalizeEmbeddingVector([]float32{0, 0})
				return err
			}(),
			want: sigma.ErrEmbeddingVectorZeroNorm,
		},
		{
			name: "weight mismatch",
			err: func() error {
				_, err := sigma.CombineEmbeddingVectors([][]float32{{1}}, []int{})
				return err
			}(),
			want: sigma.ErrEmbeddingVectorWeightMismatch,
		},
		{
			name: "zero weight",
			err: func() error {
				_, err := sigma.CombineEmbeddingVectors([][]float32{{1}}, []int{0})
				return err
			}(),
			want: sigma.ErrEmbeddingVectorZeroWeight,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !errors.Is(tt.err, tt.want) {
				t.Fatalf("error = %v, want %v", tt.err, tt.want)
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

func containsEmbeddingBatchPhase(phases []sigma.EmbeddingBatchPhase, want sigma.EmbeddingBatchPhase) bool {
	for _, phase := range phases {
		if phase == want {
			return true
		}
	}
	return false
}

func containsEmbeddingTracePhase(trace []sigma.EmbeddingBatchTraceEvent, want sigma.EmbeddingBatchPhase) bool {
	_, ok := firstEmbeddingTracePhase(trace, want)
	return ok
}

func firstEmbeddingTracePhase(trace []sigma.EmbeddingBatchTraceEvent, want sigma.EmbeddingBatchPhase) (sigma.EmbeddingBatchTraceEvent, bool) {
	for _, event := range trace {
		if event.Phase == want {
			return event, true
		}
	}
	return sigma.EmbeddingBatchTraceEvent{}, false
}

type testEmbeddingCache struct {
	values map[sigma.EmbeddingCacheKey]sigma.Embedding
	keys   []sigma.EmbeddingCacheKey
	getErr error
	setErr error
}

func newTestEmbeddingCache() *testEmbeddingCache {
	return &testEmbeddingCache{values: make(map[sigma.EmbeddingCacheKey]sigma.Embedding)}
}

func (c *testEmbeddingCache) Get(key sigma.EmbeddingCacheKey) (sigma.Embedding, bool, error) {
	c.keys = append(c.keys, key)
	if c.getErr != nil {
		return sigma.Embedding{}, false, c.getErr
	}
	embedding, ok := c.values[key]
	embedding.Vector = append([]float32(nil), embedding.Vector...)
	return embedding, ok, nil
}

func (c *testEmbeddingCache) Set(key sigma.EmbeddingCacheKey, embedding sigma.Embedding) error {
	c.keys = append(c.keys, key)
	if c.setErr != nil {
		return c.setErr
	}
	embedding.Vector = append([]float32(nil), embedding.Vector...)
	c.values[key] = embedding
	return nil
}
