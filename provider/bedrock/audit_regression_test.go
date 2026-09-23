// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestRegressionBedrockMalformedVectors(t *testing.T) {
	t.Parallel()
	result, err := decodeTitanEmbeddingResponse(sigma.EmbeddingModel{ID: "audit"}, []byte(`{}`))
	if err == nil {
		t.Errorf("accepted missing Titan embedding: %v", result.Vectors)
	}
}

func TestMissingNovaAndCohereVectors(t *testing.T) {
	t.Parallel()
	model := sigma.EmbeddingModel{ID: "test"}
	if result, err := decodeNovaEmbeddingResponse(model, []byte(`{"embeddings":[{}]}`)); err == nil || len(result.Vectors) != 0 {
		t.Fatalf("missing Nova vector: %v, %v", result, err)
	}
	for _, body := range []string{`{}`, `{"embeddings":{}}`, `{"embeddings":[]}`} {
		if result, err := decodeCohereEmbeddingResponse(model, 1, []byte(body)); err == nil || len(result.Vectors) != 0 {
			t.Fatalf("missing Cohere vector: %v, %v", result, err)
		}
	}
}
