// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package embeddingwire

import "testing"

func TestDecodeRejectsMalformedVectors(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "null", "[]", "{}", "0", "[null]", "[true]", `["1"]`, "[[1]]", "[{}]", "[1,1e100]", "[1,-1e100]", "[1,1e999]"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			vector, err := Decode([]byte(raw))
			if err == nil || vector != nil {
				t.Fatalf("accepted %s: %v, %v", raw, vector, err)
			}
		})
	}
}

func TestDecodeAcceptsZerosAndFiniteNumbers(t *testing.T) {
	t.Parallel()
	vector, err := Decode([]byte(`[0,-0,1.25,-2,3.4028234e38]`))
	if err != nil || len(vector) != 5 || vector[0] != 0 || vector[1] != 0 || vector[2] != 1.25 || vector[3] != -2 {
		t.Fatalf("%v, %v", vector, err)
	}
}
