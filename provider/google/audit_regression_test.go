// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestRegressionGoogleMalformedVectors(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"embeddings":[{}]}`, `{"embeddings":[{"values":[null,1]}]}`} {
		result, err := decodeGoogleEmbeddingsResponse([]byte(body), sigma.EmbeddingModel{ID: "audit"}, 1)
		if err == nil {
			t.Errorf("accepted malformed Gemini response %s: %v", body, result.Vectors)
		}
	}
	result, err := decodeVertexEmbeddingsResponse([]byte(`{"predictions":[{}]}`), sigma.EmbeddingModel{ID: "audit"}, 1)
	if err == nil {
		t.Errorf("accepted malformed Vertex response: %v", result.Vectors)
	}
}
