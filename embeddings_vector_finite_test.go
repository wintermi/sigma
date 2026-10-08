// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"errors"
	"math"
	"testing"

	"github.com/wintermi/sigma"
)

func TestEmbeddingVectorUtilitiesRejectNonFiniteValues(t *testing.T) {
	t.Parallel()

	valid := []float32{1, 0}
	for _, bad := range [][]float32{{float32(math.NaN()), 0}, {float32(math.Inf(1)), 0}, {float32(math.Inf(-1)), 0}} {
		calls := map[string]func() error{
			"DotProduct":               func() error { _, err := sigma.DotProduct(valid, bad); return err },
			"CosineSimilarity":         func() error { _, err := sigma.CosineSimilarity(bad, valid); return err },
			"NormalizeEmbeddingVector": func() error { _, err := sigma.NormalizeEmbeddingVector(bad); return err },
			"CombineEmbeddingVectors": func() error {
				_, err := sigma.CombineEmbeddingVectors([][]float32{valid, bad}, []int{1, 1})
				return err
			},
			"RankEmbeddingsByCosine": func() error {
				_, err := sigma.RankEmbeddingsByCosine(valid, []sigma.Embedding{{Vector: valid}, {Index: 1, Vector: bad}})
				return err
			},
		}
		for name, call := range calls {
			if err := call(); !errors.Is(err, sigma.ErrEmbeddingVectorNonFinite) {
				t.Errorf("%s(%v) error = %v, want ErrEmbeddingVectorNonFinite", name, bad, err)
			}
		}
	}
}
