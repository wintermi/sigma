// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package jsonutil decodes provider JSON without rounding untyped numbers.
package jsonutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
)

// Decode reads exactly one JSON value, preserving untyped numbers as json.Number.
func Decode(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("unexpected trailing JSON value")
	}
	return nil
}

// IsOne reports whether value encodes the JSON number one, including decimal
// and exponent spellings. Strings, null, and rounded near-one values are rejected.
func IsOne(value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var decoded any
	if Decode(data, &decoded) != nil {
		return false
	}
	number, ok := decoded.(json.Number)
	if !ok {
		return false
	}
	value64, err := number.Float64()
	if err != nil || value64 != 1 {
		return false
	}
	rational, ok := new(big.Rat).SetString(string(number))
	return ok && rational.Cmp(big.NewRat(1, 1)) == 0
}
