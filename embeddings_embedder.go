// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"fmt"
)

// EmbeddingEmbedder creates query and document vectors through Sigma's
// provider-neutral embedding surface.
type EmbeddingEmbedder interface {
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// EmbeddingEmbedderConfig configures NewEmbeddingEmbedder.
type EmbeddingEmbedderConfig struct {
	Dimensions int
	Batch      EmbeddingBatchConfig
}

// ClientEmbeddingEmbedder adapts a Client and EmbeddingModel to the
// EmbeddingEmbedder interface.
type ClientEmbeddingEmbedder struct {
	client *Client
	model  EmbeddingModel
	config EmbeddingEmbedderConfig
	opts   []EmbeddingOption
}

var _ EmbeddingEmbedder = (*ClientEmbeddingEmbedder)(nil)

// NewEmbeddingEmbedder wraps client and model with query/document embedding helpers.
func NewEmbeddingEmbedder(client *Client, model EmbeddingModel, config EmbeddingEmbedderConfig, opts ...EmbeddingOption) *ClientEmbeddingEmbedder {
	if client == nil {
		client = NewClient()
	}
	return &ClientEmbeddingEmbedder{
		client: client,
		model:  model,
		config: config,
		opts:   append([]EmbeddingOption(nil), opts...),
	}
}

// EmbedDocuments embeds texts as document inputs.
func (e *ClientEmbeddingEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil {
		return nil, invalidEmbeddingOptionsError(EmbeddingModel{}, "embedding embedder is required")
	}
	result, err := e.client.EmbedBatch(
		ctx,
		e.model,
		EmbeddingRequest{Inputs: append([]string(nil), texts...), Dimensions: e.config.Dimensions, InputType: EmbeddingInputTypeDocument},
		e.config.Batch,
		e.opts...,
	)
	if err != nil {
		return nil, err
	}
	vectors := orderEmbeddingsByIndex(result.Embeddings.Vectors)
	out := make([][]float32, 0, len(vectors))
	for _, embedding := range vectors {
		out = append(out, append([]float32(nil), embedding.Vector...))
	}
	return out, nil
}

// EmbedQuery embeds text as a query input and returns its vector.
func (e *ClientEmbeddingEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if e == nil {
		return nil, invalidEmbeddingOptionsError(EmbeddingModel{}, "embedding embedder is required")
	}
	result, err := e.client.EmbedBatch(
		ctx,
		e.model,
		EmbeddingRequest{Inputs: []string{text}, Dimensions: e.config.Dimensions, InputType: EmbeddingInputTypeQuery},
		e.config.Batch,
		e.opts...,
	)
	if err != nil {
		return nil, err
	}
	vectors := orderEmbeddingsByIndex(result.Embeddings.Vectors)
	if len(vectors) != 1 {
		return nil, fmt.Errorf("embedding embedder: provider returned %d vectors for query", len(vectors))
	}
	return append([]float32(nil), vectors[0].Vector...), nil
}
