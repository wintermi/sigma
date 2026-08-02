// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals_test

import (
	"fmt"

	"github.com/wintermi/sigma/internal/evals"
)

func ExampleNewHarnessTable() {
	baseline := evals.HarnessFunc[string, string]{Name: "baseline"}
	candidate := evals.HarnessFunc[string, string]{Name: "candidate"}

	rows, err := evals.NewHarnessTable(
		"answer-quality",
		baseline,
		[]evals.Harness[string, string]{candidate},
		2,
	)
	if err != nil {
		panic(err)
	}
	for _, row := range rows {
		fmt.Printf("%d %s\n", row.Repetition, row.Name)
	}

	// Output:
	// 1 baseline
	// 1 candidate
	// 2 baseline
	// 2 candidate
}
