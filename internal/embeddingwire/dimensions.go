// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package embeddingwire

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
)

// RequestDimensions reads dimension controls from the final serialized request.
// An asterisk in path selects each element of an array (batch embedding inputs).
// Zero represents an omitted dimension; explicit null and nonintegral controls fail.
func RequestDimensions(req *http.Request, path ...string) ([]int, error) {
	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("read embedding request dimensions: %w", err)
	}
	defer body.Close()
	var payload any
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode embedding request dimensions: %w", err)
	}
	return dimensionsAt(payload, path)
}

func dimensionsAt(value any, path []string) ([]int, error) {
	if len(path) == 0 {
		number, ok := value.(json.Number)
		if !ok {
			return nil, fmt.Errorf("embedding dimension must be a positive integer")
		}
		dimension, ok := new(big.Rat).SetString(string(number))
		if !ok || !dimension.IsInt() || !dimension.Num().IsInt64() || dimension.Sign() <= 0 || dimension.Num().Int64() > int64(int(^uint(0)>>1)) {
			return nil, fmt.Errorf("embedding dimension must be a positive integer")
		}
		return []int{int(dimension.Num().Int64())}, nil
	}
	if path[0] == "*" {
		items, ok := value.([]any)
		if !ok || len(items) == 0 {
			return nil, fmt.Errorf("embedding dimension controls require a nonempty request array")
		}
		var dimensions []int
		for _, item := range items {
			dimension, err := dimensionsAt(item, path[1:])
			if err != nil {
				return nil, err
			}
			dimensions = append(dimensions, dimension...)
		}
		return dimensions, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid embedding dimension container for %s", path[0])
	}
	nested, exists := object[path[0]]
	if !exists {
		return []int{0}, nil
	}
	return dimensionsAt(nested, path[1:])
}

// ValidateDimensions requires nonempty, consistently sized vectors and honors
// dimensions sent on the request. A single expectation applies to every vector.
func ValidateDimensions(lengths, expected []int) error {
	if len(expected) > 1 && len(expected) != len(lengths) {
		return fmt.Errorf("embedding dimension expectation count does not match response")
	}
	for i, length := range lengths {
		if length == 0 {
			return fmt.Errorf("embedding vector %d is empty", i)
		}
		if length != lengths[0] {
			return fmt.Errorf("embedding vector %d has dimension %d, want %d", i, length, lengths[0])
		}
		want := 0
		if len(expected) == 1 {
			want = expected[0]
		} else if len(expected) > 1 {
			want = expected[i]
		}
		if want > 0 && length != want {
			return fmt.Errorf("embedding vector %d has dimension %d, requested %d", i, length, want)
		}
	}
	return nil
}
