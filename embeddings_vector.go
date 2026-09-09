// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"errors"
	"math"
	"math/big"
	"sort"
)

var (
	// ErrEmbeddingVectorDimensionMismatch reports vectors with incompatible dimensions.
	ErrEmbeddingVectorDimensionMismatch = errors.New("embedding vector dimensions do not match")
	// ErrEmbeddingVectorZeroNorm reports a vector that cannot be normalized.
	ErrEmbeddingVectorZeroNorm = errors.New("embedding vector norm is zero")
	// ErrEmbeddingVectorWeightMismatch reports a weight list that does not match the vector list.
	ErrEmbeddingVectorWeightMismatch = errors.New("embedding vector weights do not match vectors")
	// ErrEmbeddingVectorZeroWeight reports a weighted operation with no effective weight.
	ErrEmbeddingVectorZeroWeight = errors.New("embedding vector weight sum is zero")
)

// DotProduct calculates the dot product for two embedding vectors.
func DotProduct(a, b []float32) (float64, error) {
	if len(a) != len(b) {
		return 0, ErrEmbeddingVectorDimensionMismatch
	}
	var score float64
	for i := range a {
		score += float64(a[i]) * float64(b[i])
	}
	return score, nil
}

// CosineSimilarity calculates cosine similarity for two embedding vectors.
func CosineSimilarity(a, b []float32) (float64, error) {
	dot, err := DotProduct(a, b)
	if err != nil {
		return 0, err
	}
	normA := embeddingVectorNorm(a)
	normB := embeddingVectorNorm(b)
	if normA == 0 || normB == 0 {
		return 0, ErrEmbeddingVectorZeroNorm
	}
	return dot / (normA * normB), nil
}

// NormalizeEmbeddingVector returns a unit-length copy of vector.
func NormalizeEmbeddingVector(vector []float32) ([]float32, error) {
	norm := embeddingVectorNorm(vector)
	if norm == 0 {
		return nil, ErrEmbeddingVectorZeroNorm
	}
	normalized := make([]float32, len(vector))
	for i, value := range vector {
		normalized[i] = float32(float64(value) / norm)
	}
	return normalized, nil
}

// CombineEmbeddingVectors returns a normalized weighted average of embedding vectors.
func CombineEmbeddingVectors(vectors [][]float32, weights []int) ([]float32, error) {
	if len(vectors) != len(weights) {
		return nil, ErrEmbeddingVectorWeightMismatch
	}
	if len(vectors) == 0 {
		return nil, ErrEmbeddingVectorZeroWeight
	}
	dimensions := len(vectors[0])
	combined := make([]float64, dimensions)
	var totalWeight, weightValue big.Int
	for i, vector := range vectors {
		if len(vector) != dimensions {
			return nil, ErrEmbeddingVectorDimensionMismatch
		}
		weight := weights[i]
		totalWeight.Add(&totalWeight, weightValue.SetInt64(int64(weight)))
		for j, value := range vector {
			combined[j] += float64(value) * float64(weight)
		}
	}
	if totalWeight.Sign() == 0 {
		return nil, ErrEmbeddingVectorZeroWeight
	}
	var norm float64
	for _, value := range combined {
		norm = math.Hypot(norm, value)
	}
	if norm == 0 {
		return nil, ErrEmbeddingVectorZeroNorm
	}
	// Normalization cancels the magnitude of the total weight. Its sign still
	// determines the direction of an average with signed weights.
	result := make([]float32, dimensions)
	for i, value := range combined {
		result[i] = float32(value / norm * float64(totalWeight.Sign()))
	}
	return result, nil
}

// RankEmbeddingsByCosine scores candidates against query and sorts by descending similarity.
func RankEmbeddingsByCosine(query []float32, candidates []Embedding) ([]EmbeddingScore, error) {
	scores := make([]EmbeddingScore, 0, len(candidates))
	for _, candidate := range candidates {
		score, err := CosineSimilarity(query, candidate.Vector)
		if err != nil {
			return nil, err
		}
		scores = append(scores, EmbeddingScore{
			Embedding: Embedding{
				Index:  candidate.Index,
				Vector: append([]float32(nil), candidate.Vector...),
			},
			Score: score,
		})
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].Score == scores[j].Score {
			return scores[i].Embedding.Index < scores[j].Embedding.Index
		}
		return scores[i].Score > scores[j].Score
	})
	return scores, nil
}

func embeddingVectorNorm(vector []float32) float64 {
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	return math.Sqrt(sum)
}
