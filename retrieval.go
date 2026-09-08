// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	defaultRetrievalChunkSize    = 1000
	defaultRetrievalChunkOverlap = 200
)

// RetrievalDocument is caller-owned text plus metadata used for retrieval.
type RetrievalDocument struct {
	ID       string         `json:"id,omitempty"`
	Text     string         `json:"text,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// RetrievalChunk is one indexed text chunk.
type RetrievalChunk struct {
	ID         string         `json:"id,omitempty"`
	DocumentID string         `json:"documentID,omitempty"`
	Text       string         `json:"text,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	StartByte  int            `json:"startByte,omitempty"`
	EndByte    int            `json:"endByte,omitempty"`
}

// RetrievalResult is one retrieval hit without exposing stored vectors.
type RetrievalResult struct {
	Chunk RetrievalChunk `json:"chunk"`
	Score float64        `json:"score"`
}

// RetrievalSplitterConfig configures deterministic character-based splitting.
type RetrievalSplitterConfig struct {
	ChunkSize     int      `json:"chunkSize,omitempty"`
	ChunkOverlap  int      `json:"chunkOverlap,omitempty"`
	Separators    []string `json:"separators,omitempty"`
	KeepSeparator bool     `json:"keepSeparator,omitempty"`
}

// InMemoryRetrievalIndexConfig configures an in-memory embedding-backed index.
type InMemoryRetrievalIndexConfig struct {
	Splitter   RetrievalSplitterConfig
	Batch      EmbeddingBatchConfig
	Dimensions int
}

// InMemoryRetrievalIndex stores normalized embedding vectors in process memory.
type InMemoryRetrievalIndex struct {
	client *Client
	model  EmbeddingModel
	config InMemoryRetrievalIndexConfig
	opts   []EmbeddingOption

	dimensions int
	items      []indexedRetrievalChunk
}

type indexedRetrievalChunk struct {
	chunk  RetrievalChunk
	vector []float32
}

type retrievalSplitter struct {
	config     RetrievalSplitterConfig
	separators []string
	spans      []retrievalRuneSpan
}

type retrievalRuneSpan struct {
	start int
	end   int
}

// NewInMemoryRetrievalIndex constructs an in-memory embedding-backed retrieval index.
func NewInMemoryRetrievalIndex(client *Client, model EmbeddingModel, config InMemoryRetrievalIndexConfig, opts ...EmbeddingOption) *InMemoryRetrievalIndex {
	if client == nil {
		client = NewClient()
	}
	return &InMemoryRetrievalIndex{
		client: client,
		model:  model,
		config: config,
		opts:   append([]EmbeddingOption(nil), opts...),
	}
}

// SplitRetrievalText splits text into deterministic retrieval chunks.
func SplitRetrievalText(text string, config RetrievalSplitterConfig) ([]RetrievalChunk, error) {
	splitter, err := newRetrievalSplitter(text, config)
	if err != nil {
		return nil, err
	}
	chunks := splitter.split(text)
	for i := range chunks {
		chunks[i].ID = fmt.Sprintf("chunk-%d", i)
	}
	return chunks, nil
}

// SplitRetrievalDocuments splits documents and copies metadata onto each chunk.
func SplitRetrievalDocuments(docs []RetrievalDocument, config RetrievalSplitterConfig) ([]RetrievalChunk, error) {
	var chunks []RetrievalChunk
	for docIndex, doc := range docs {
		docID := doc.ID
		if docID == "" {
			docID = fmt.Sprintf("document-%d", docIndex)
		}
		docChunks, err := SplitRetrievalText(doc.Text, config)
		if err != nil {
			return nil, err
		}
		for chunkIndex := range docChunks {
			docChunks[chunkIndex].ID = fmt.Sprintf("%s#%d", docID, chunkIndex)
			docChunks[chunkIndex].DocumentID = docID
			docChunks[chunkIndex].Metadata = copyStringAnyMap(doc.Metadata)
			chunks = append(chunks, docChunks[chunkIndex])
		}
	}
	return chunks, nil
}

// AddDocuments splits, embeds, and indexes documents as document inputs.
func (i *InMemoryRetrievalIndex) AddDocuments(ctx context.Context, docs []RetrievalDocument) error {
	if i == nil {
		return retrievalInvalidOptionsError(EmbeddingModel{}, "retrieval index is required")
	}
	chunks, err := SplitRetrievalDocuments(docs, i.config.Splitter)
	if err != nil {
		return err
	}
	return i.AddChunks(ctx, chunks)
}

// AddChunks embeds and indexes caller-supplied chunks as document inputs.
// Failed or canceled insertions leave items and inferred dimensions unchanged.
func (i *InMemoryRetrievalIndex) AddChunks(ctx context.Context, chunks []RetrievalChunk) error {
	if i == nil {
		return retrievalInvalidOptionsError(EmbeddingModel{}, "retrieval index is required")
	}
	if len(chunks) == 0 {
		return nil
	}
	inputs := make([]string, len(chunks))
	indexedChunks := make([]RetrievalChunk, len(chunks))
	for index, chunk := range chunks {
		inputs[index] = chunk.Text
		indexedChunks[index] = cloneRetrievalChunk(chunk)
	}
	result, err := i.client.EmbedBatch(
		ctx,
		i.model,
		EmbeddingRequest{Inputs: inputs, Dimensions: i.config.Dimensions, InputType: EmbeddingInputTypeDocument},
		i.config.Batch,
		i.opts...,
	)
	if err != nil {
		return err
	}
	vectors := orderEmbeddingsByIndex(result.Embeddings.Vectors)
	if len(vectors) != len(indexedChunks) {
		return fmt.Errorf("retrieval index: embedding provider returned %d vectors for %d chunks", len(vectors), len(indexedChunks))
	}
	dimensions := i.dimensions
	if i.config.Dimensions != 0 {
		dimensions = i.config.Dimensions
	}
	pending := make([]indexedRetrievalChunk, 0, len(vectors))
	for index, embedding := range vectors {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if dimensions == 0 {
			dimensions = len(embedding.Vector)
		}
		if len(embedding.Vector) != dimensions {
			return fmt.Errorf("retrieval index: chunk %d has %d dimensions, expected %d", index, len(embedding.Vector), dimensions)
		}

		normalized, err := NormalizeEmbeddingVector(embedding.Vector)
		if err != nil {
			return fmt.Errorf("retrieval index: normalize chunk %d: %w", index, err)
		}
		pending = append(pending, indexedRetrievalChunk{
			chunk:  indexedChunks[index],
			vector: normalized,
		})
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	i.items = append(i.items, pending...)
	i.dimensions = dimensions
	return nil
}

// Search embeds query as a query input and returns cosine-ranked chunks.
func (i *InMemoryRetrievalIndex) Search(ctx context.Context, query string, limit int) ([]RetrievalResult, error) {
	if i == nil {
		return nil, retrievalInvalidOptionsError(EmbeddingModel{}, "retrieval index is required")
	}
	if limit < 0 {
		return nil, retrievalInvalidOptionsError(i.model, "retrieval search limit must be non-negative")
	}
	if limit == 0 || len(i.items) == 0 {
		return nil, nil
	}
	result, err := i.client.EmbedBatch(
		ctx,
		i.model,
		EmbeddingRequest{Inputs: []string{query}, Dimensions: i.config.Dimensions, InputType: EmbeddingInputTypeQuery},
		i.config.Batch,
		i.opts...,
	)
	if err != nil {
		return nil, err
	}
	vectors := orderEmbeddingsByIndex(result.Embeddings.Vectors)
	if len(vectors) != 1 {
		return nil, fmt.Errorf("retrieval index: embedding provider returned %d vectors for query", len(vectors))
	}
	queryVector, err := NormalizeEmbeddingVector(vectors[0].Vector)
	if err != nil {
		return nil, fmt.Errorf("retrieval index: normalize query: %w", err)
	}

	results := make([]retrievalScoredItem, 0, len(i.items))
	for _, item := range i.items {
		score, err := DotProduct(queryVector, item.vector)
		if err != nil {
			return nil, fmt.Errorf("retrieval index: score chunk %q: %w", item.chunk.ID, err)
		}
		results = append(results, retrievalScoredItem{item: item, score: score})
	}
	sort.SliceStable(results, func(left, right int) bool {
		return results[left].score > results[right].score
	})
	if limit > len(results) {
		limit = len(results)
	}
	out := make([]RetrievalResult, limit)
	for index := range out {
		out[index] = RetrievalResult{
			Chunk: cloneRetrievalChunk(results[index].item.chunk),
			Score: results[index].score,
		}
	}
	return out, nil
}

type retrievalScoredItem struct {
	item  indexedRetrievalChunk
	score float64
}

func newRetrievalSplitter(text string, config RetrievalSplitterConfig) (retrievalSplitter, error) {
	config, err := normalizeRetrievalSplitterConfig(config)
	if err != nil {
		return retrievalSplitter{}, err
	}
	return retrievalSplitter{
		config:     config,
		separators: config.Separators,
		spans:      retrievalRuneSpans(text),
	}, nil
}

func normalizeRetrievalSplitterConfig(config RetrievalSplitterConfig) (RetrievalSplitterConfig, error) {
	defaultOverlap := config.ChunkSize == 0 && config.ChunkOverlap == 0
	if config.ChunkSize < 0 {
		return RetrievalSplitterConfig{}, retrievalInvalidOptionsError(EmbeddingModel{}, "retrieval chunk size must be positive")
	}
	if config.ChunkOverlap < 0 {
		return RetrievalSplitterConfig{}, retrievalInvalidOptionsError(EmbeddingModel{}, "retrieval chunk overlap must be non-negative")
	}
	if config.ChunkSize == 0 {
		config.ChunkSize = defaultRetrievalChunkSize
	}
	if defaultOverlap {
		config.ChunkOverlap = defaultRetrievalChunkOverlap
	}
	if config.ChunkOverlap >= config.ChunkSize {
		return RetrievalSplitterConfig{}, retrievalInvalidOptionsError(EmbeddingModel{}, "retrieval chunk overlap must be smaller than chunk size")
	}
	if len(config.Separators) == 0 {
		config.Separators = []string{"\n\n", "\n", " "}
	}
	return config, nil
}

func (s retrievalSplitter) split(text string) []RetrievalChunk {
	if text == "" || len(s.spans) == 0 {
		return nil
	}
	var chunks []RetrievalChunk
	startRune := 0
	for startRune < len(s.spans) {
		endRune := startRune + s.config.ChunkSize
		if endRune > len(s.spans) {
			endRune = len(s.spans)
		}
		if endRune < len(s.spans) {
			endRune = s.preferredEndRune(text, startRune, endRune)
		}
		if endRune <= startRune {
			endRune = startRune + 1
		}
		startByte := s.spans[startRune].start
		endByte := s.spans[endRune-1].end
		chunks = append(chunks, RetrievalChunk{
			Text:      text[startByte:endByte],
			StartByte: startByte,
			EndByte:   endByte,
		})
		if endRune == len(s.spans) {
			break
		}
		nextStart := endRune - s.config.ChunkOverlap
		if nextStart <= startRune {
			nextStart = startRune + 1
		}
		startRune = nextStart
	}
	return chunks
}

func (s retrievalSplitter) preferredEndRune(text string, startRune, maxEndRune int) int {
	startByte := s.spans[startRune].start
	endByte := s.spans[maxEndRune-1].end
	window := text[startByte:endByte]
	for _, separator := range s.separators {
		if separator == "" {
			continue
		}
		offset := strings.LastIndex(window, separator)
		if offset <= 0 {
			continue
		}
		splitByte := startByte + offset
		if s.config.KeepSeparator {
			splitByte += len(separator)
		}
		if splitRune := s.runeIndexAtByte(splitByte); splitRune > startRune {
			return splitRune
		}
	}
	return maxEndRune
}

func (s retrievalSplitter) runeIndexAtByte(byteIndex int) int {
	for index, span := range s.spans {
		if span.start >= byteIndex {
			return index
		}
	}
	return len(s.spans)
}

func retrievalRuneSpans(text string) []retrievalRuneSpan {
	spans := make([]retrievalRuneSpan, 0, len(text))
	for start, r := range text {
		spans = append(spans, retrievalRuneSpan{start: start, end: start + utf8.RuneLen(r)})
	}
	return spans
}

func cloneRetrievalChunk(chunk RetrievalChunk) RetrievalChunk {
	chunk.Metadata = copyStringAnyMap(chunk.Metadata)
	return chunk
}

func retrievalInvalidOptionsError(model EmbeddingModel, message string) error {
	return &Error{
		Code:     ErrorInvalidOptions,
		Message:  message,
		Provider: model.Provider,
		Model:    model.ID,
	}
}
