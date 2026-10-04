// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package embeddingwire

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestRequestDimensions(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		body string
		path []string
		want []int
	}{
		{`{}`, []string{"dimensions"}, []int{0}},
		{`{"dimensions":2}`, []string{"dimensions"}, []int{2}},
		{`{"dimensions":2.0}`, []string{"dimensions"}, []int{2}},
		{`{"dimensions":2e1}`, []string{"dimensions"}, []int{20}},
		{`{"dimensions":2.000000000000000001}`, []string{"dimensions"}, nil},
		{`{"dimensions":9223372036854775808}`, []string{"dimensions"}, nil},
		{`{"dimensions":null}`, []string{"dimensions"}, nil},
		{`{"dimensions":true}`, []string{"dimensions"}, nil},
		{`{"dimensions":"2"}`, []string{"dimensions"}, nil},
		{`{"dimensions":0}`, []string{"dimensions"}, nil},
		{`{"dimensions":-2}`, []string{"dimensions"}, nil},
		{`{"parameters":null}`, []string{"parameters", "outputDimensionality"}, nil},
		{`{"requests":[]}`, []string{"requests", "*", "outputDimensionality"}, nil},
		{`{"requests":[{}, {"outputDimensionality":2}]}`, []string{"requests", "*", "outputDimensionality"}, []int{0, 2}},
	} {
		t.Run(tt.body, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			got, err := RequestDimensions(req, tt.path...)
			if (err != nil) != (tt.want == nil) || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestValidateDimensions(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name              string
		lengths, expected []int
		invalid           bool
	}{
		{"consistent inferred", []int{2, 2}, nil, false},
		{"consistent requested", []int{2, 2}, []int{2}, false},
		{"uniformly wrong requested", []int{2, 2}, []int{3}, true},
		{"mixed inferred", []int{2, 1}, nil, true},
		{"empty", []int{0}, nil, true},
		{"per input requested", []int{2, 2}, []int{0, 2}, false},
		{"later input wrong requested", []int{2, 2}, []int{0, 3}, true},
		{"different input dimensions", []int{2, 3}, []int{2, 3}, true},
		{"wrong expectation count", []int{2}, []int{2, 2}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateDimensions(tt.lengths, tt.expected); (err != nil) != tt.invalid {
				t.Fatalf("validation error=%v, want invalid=%v", err, tt.invalid)
			}
		})
	}
}
