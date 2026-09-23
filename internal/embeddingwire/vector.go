// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package embeddingwire validates provider vectors before conversion.
package embeddingwire

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Decode accepts a nonempty JSON array of numbers representable as finite float32s.
func Decode(raw json.RawMessage) ([]float32, error) {
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil {
		return nil, fmt.Errorf("invalid embedding vector: %w", err)
	}
	if len(elements) == 0 {
		return nil, fmt.Errorf("embedding vector is missing, null, or empty")
	}
	vector := make([]float32, len(elements))
	for i, element := range elements {
		// Parsing the raw number rejects null, booleans, strings, and compound values.
		value, err := strconv.ParseFloat(string(element), 32)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("embedding element %d is not a finite float32 number", i)
		}
		vector[i] = float32(value)
	}
	return vector, nil
}
