// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package headerutil

import (
	"reflect"
	"testing"
)

func TestMergePrecedenceAndOwnership(t *testing.T) {
	t.Parallel()
	base := map[string]string{"X-Tenant": "old", "X-Keep": "keep"}
	override := map[string]string{"X-TENANT": "loser", "x-tenant": "winner"}
	for range 32 {
		merged := Merge(base, override)
		if !reflect.DeepEqual(merged, map[string]string{"x-tenant": "winner", "X-Keep": "keep"}) {
			t.Fatalf("case variants did not merge deterministically: %v", merged)
		}
		merged = Merge(merged, map[string]string{"X-tENANT": "last"})
		if !reflect.DeepEqual(merged, map[string]string{"X-tENANT": "last", "X-Keep": "keep"}) {
			t.Fatalf("later override lost its spelling or value: %v", merged)
		}
		merged["X-Keep"] = "mutated"
	}
	if !reflect.DeepEqual(base, map[string]string{"X-Tenant": "old", "X-Keep": "keep"}) || !reflect.DeepEqual(override, map[string]string{"X-TENANT": "loser", "x-tenant": "winner"}) {
		t.Fatal("caller maps mutated")
	}
	copied := Merge(nil, base)
	copied["X-Keep"] = "mutated"
	if base["X-Keep"] != "keep" {
		t.Fatal("single input was not copied")
	}
	if Merge(nil, nil) != nil {
		t.Fatal("empty inputs should remain absent")
	}
}
