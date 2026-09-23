// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"fmt"
	"testing"

	"github.com/wintermi/sigma"
)

func TestGeminiWireVectorValidation(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"null", "[]", "{}", "0", "[null]", "[true]", `["1"]`, "[1,1e100]", "[0,0]"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`{"embeddings":[{"values":%s}]}`, raw)
			result, err := decodeGoogleEmbeddingsResponse([]byte(body), sigma.EmbeddingModel{}, 1)
			if raw == "[0,0]" {
				if err != nil || len(result.Vectors) != 1 || len(result.Vectors[0].Vector) != 2 || result.Vectors[0].Vector[0] != 0 {
					t.Fatalf("valid zero vector: %v, %v", result, err)
				}
			} else if err == nil || len(result.Vectors) != 0 {
				t.Fatalf("malformed vector accepted: %v, %v", result, err)
			}
		})
	}
}

func TestVertexWireVectorValidation(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"null", "[]", "{}", "0", "[null]", "[true]", `["1"]`, "[1,1e100]", "[0,0]"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`{"predictions":[{"embeddings":{"values":%s}}]}`, raw)
			result, err := decodeVertexEmbeddingsResponse([]byte(body), sigma.EmbeddingModel{}, 1)
			if raw == "[0,0]" {
				if err != nil || len(result.Vectors) != 1 || len(result.Vectors[0].Vector) != 2 || result.Vectors[0].Vector[0] != 0 {
					t.Fatalf("valid zero vector: %v, %v", result, err)
				}
			} else if err == nil || len(result.Vectors) != 0 {
				t.Fatalf("malformed vector accepted: %v, %v", result, err)
			}
		})
	}
}
