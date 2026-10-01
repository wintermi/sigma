// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package embeddingwire

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestReadResponseLimit(t *testing.T) {
	t.Parallel()
	for _, size := range []int{7, 8, 9, 100} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Parallel()
			input := strings.NewReader(strings.Repeat("x", size))
			body, err := readResponse(input, 8)
			if size <= 8 {
				if err != nil || len(body) != size {
					t.Fatalf("body=%q error=%v", body, err)
				}
			} else {
				if !errors.Is(err, ErrResponseTooLarge) || body != nil {
					t.Fatalf("body=%q error=%v", body, err)
				}
				if input.Size()-int64(input.Len()) != 9 {
					t.Fatal("read past limit plus one")
				}
			}
		})
	}
}

type failedReader struct{ err error }

func (r failedReader) Read([]byte) (int, error) { return 0, r.err }
func TestReadResponsePreservesReadErrors(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, io.ErrUnexpectedEOF} {
		_, err := readResponse(io.MultiReader(strings.NewReader("partial"), failedReader{cause}), 32)
		if !errors.Is(err, cause) {
			t.Fatalf("lost cause %v: %v", cause, err)
		}
	}
}
