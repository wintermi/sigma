// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"strings"
	"testing"
)

func BenchmarkToolPartialAccumulation(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"64KiB", 64 << 10}, {"256KiB", 256 << 10}} {
		b.Run(size.name, func(b *testing.B) {
			raw := `{"value":"` + strings.Repeat("x", size.n-12) + `"}`
			b.ReportAllocs()
			b.SetBytes(int64(size.n))
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				block := partialBlock{}
				for end := 256; end <= len(raw); end += 256 {
					block.applyToolPartial(&PartialToolCall{ArgumentsDelta: raw[end-256 : end], ProviderMetadata: map[string]any{"argumentsText": raw[:end]}})
				}
			}
		})
	}
}
