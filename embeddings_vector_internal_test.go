// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestRegressionFiniteWeightedVectorRemainsFinite(t *testing.T) {
	t.Parallel()
	got, err := CombineEmbeddingVectors([][]float32{{math.MaxFloat32}}, []int{2})
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("weighted single positive finite vector = %v, %v; want [1], nil", got, err)
	}
}

func TestCombineEmbeddingVectorsWideArithmetic(t *testing.T) {
	t.Parallel()
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		name    string
		vectors [][]float32
		weights []int
		want    []float32
		wantErr error
	}{
		{"wide coordinates", [][]float32{{math.MaxFloat32, math.MaxFloat32}}, []int{2}, []float32{float32(1 / math.Sqrt2), float32(1 / math.Sqrt2)}, nil},
		{"large total", [][]float32{{1}, {1}}, []int{maxInt, maxInt}, []float32{1}, nil},
		{"negative total", [][]float32{{1, 0}, {0, 1}}, []int{-3, -4}, []float32{0.6, 0.8}, nil},
		{"exact zero total", [][]float32{{1}, {1}, {1}, {1}}, []int{maxInt, 1, -maxInt, -1}, nil, ErrEmbeddingVectorZeroWeight},
		{"zero vector", [][]float32{{1}, {1}}, []int{1, -1}, nil, ErrEmbeddingVectorZeroWeight},
		{"zero norm", [][]float32{{1}, {-1}}, []int{1, 1}, nil, ErrEmbeddingVectorZeroNorm},
		{"dimensions", [][]float32{{1}, {1, 2}}, []int{1, 1}, nil, ErrEmbeddingVectorDimensionMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := make([][]float32, len(tc.vectors))
			for i := range tc.vectors {
				before[i] = append([]float32(nil), tc.vectors[i]...)
			}
			got, err := CombineEmbeddingVectors(tc.vectors, tc.weights)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v; want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("vector=%v; want %v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.vectors, before) {
				t.Fatal("input mutated")
			}
		})
	}
}
