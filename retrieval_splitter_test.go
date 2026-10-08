// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

// A separator near the start of a window must not make the splitter crawl one
// rune at a time or emit whitespace-only chunks that embedding rejects.
func TestSplitRetrievalTextHandlesSeparatorNearWindowStart(t *testing.T) {
	t.Parallel()

	text := "# Heading\n\n" + strings.Repeat("x", 2000)
	chunks, err := sigma.SplitRetrievalText(text, sigma.RetrievalSplitterConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 || chunks[0].Text != "# Heading" {
		t.Fatalf("first chunk = %q, want the heading", chunks[0].Text)
	}
	if len(chunks) > 4 {
		t.Fatalf("got %d chunks, want the heading and a few body chunks: %q", len(chunks), chunkTexts(chunks))
	}
	covered := 0
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.Text) == "" {
			t.Fatalf("whitespace-only chunk in %q", chunkTexts(chunks))
		}
		if chunk.Text != text[chunk.StartByte:chunk.EndByte] {
			t.Fatalf("chunk %q does not match its byte range", chunk.Text)
		}
		covered = max(covered, chunk.EndByte)
	}
	if covered != len(text) {
		t.Fatalf("chunks cover %d of %d bytes", covered, len(text))
	}
}
