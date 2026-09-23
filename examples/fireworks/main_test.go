// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import "testing"

func TestDemoModelResolves(t *testing.T) {
	t.Parallel()
	_, model, err := fireworksDemoClient()
	if err != nil {
		t.Fatal(err)
	}
	if !model.SupportsTools {
		t.Fatal("demo model must support the example's tool loop")
	}
}
