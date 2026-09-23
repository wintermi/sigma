// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package redact

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestErrorPreservesCauseAndRedactsFormatting(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"api_key", "access_token", "X-Amz-Signature", "X-Goog-Credential"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			original := &url.Error{Op: "Post", URL: "https://example.invalid/?" + key + "=synthetic-secret", Err: context.Canceled}
			err := Error(original)
			for _, format := range []string{"%s", "%v", "%+v", "%#v", "%q"} {
				if got := fmt.Sprintf(format, err); strings.Contains(got, "synthetic-secret") || !strings.Contains(got, "[redacted]") {
					t.Fatalf("unsafe format %s: %s", format, got)
				}
			}
			var transport *url.Error
			if !errors.As(err, &transport) || transport != original || !errors.Is(err, context.Canceled) {
				t.Fatal("lost original cause")
			}
			if got := fmt.Sprint(err.(fmt.Stringer).String()); strings.Contains(got, "synthetic-secret") {
				t.Fatal(got)
			}
		})
	}
}

func TestErrorLeavesSafeErrorsUnchanged(t *testing.T) {
	t.Parallel()
	for _, err := range []error{nil, context.Canceled, errors.New("ordinary diagnostic"), errors.New(`{ "z": 1, "a": "safe" }`), errors.New(`{ "access_token": "[redacted]" }`)} {
		if Error(err) != err {
			t.Fatalf("changed safe error %v", err)
		}
	}
}
