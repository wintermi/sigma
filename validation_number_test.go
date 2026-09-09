// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestExactNumericToolConstraints(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema, input string
		valid               bool
	}{
		{"negative minimum", `{"type":"integer","minimum":-9007199254740992}`, `-9007199254740993`, false},
		{"const distinct", `{"const":9007199254740992}`, `9007199254740993`, false},
		{"const equivalent", `{"const":9007199254740992}`, `900719925474099200e-2`, true},
		{"negative fraction", `{"type":"integer"}`, `-9007199254740992.5`, false},
		{"maximum exact", `{"type":"integer","maximum":9007199254740993}`, `9007199254740993`, true},
		{"fraction bound", `{"type":"number","minimum":0.100000000000000000001}`, `0.1`, false},
		{"huge exponent", `{"type":"integer","minimum":1e999999999999999999999}`, `1e1000000000000000000000`, true},
		{"tiny fraction", `{"type":"integer"}`, `1e-999999999999999999999`, false},
		{"tiny nonzero", `{"const":0}`, `1e-999999999999999999999`, false},
		{"zero exponent", `{"type":"integer","const":0}`, `-0e-999999999999999999999`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			schema := json.RawMessage(`{"type":"object","properties":{"n":` + tc.schema + `}}`)
			args, err := ValidateToolCall([]Tool{{Name: "number", InputSchema: schema}}, ToolCall{Name: "number", Arguments: json.RawMessage(`{"n":` + tc.input + `}`)})
			if (err == nil) != tc.valid {
				t.Fatalf("validation error=%v, valid=%v", err, tc.valid)
			}
			if tc.valid && args["n"] != json.Number(tc.input) {
				t.Fatalf("number changed: %#v", args["n"])
			}
		})
	}
}

func TestExactNumberComparison(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		left, right any
		order       int
	}{
		{int64(9007199254740993), json.Number("9007199254740992"), 1},
		{uint64(math.MaxUint64), json.Number("18446744073709551615"), 0},
		{int64(math.MinInt64), json.Number("-9223372036854775808"), 0},
		{float32(0.1), json.Number("0.1"), 0},
		{float64(0.1), json.Number("0.1"), 0},
		{json.Number("-1.01"), json.Number("-1"), -1},
		{json.Number("1.001"), json.Number("1.01"), -1},
		{json.Number("10e-1"), json.Number("1.000"), 0},
	} {
		left, ok := exactNumber(tc.left)
		if !ok {
			t.Fatalf("not numeric: %v", tc.left)
		}
		right, ok := exactNumber(tc.right)
		if !ok {
			t.Fatalf("not numeric: %v", tc.right)
		}
		if got := left.compare(right); got != tc.order {
			t.Fatalf("compare %v, %v = %d; want %d", tc.left, tc.right, got, tc.order)
		}
	}
	for _, value := range []any{math.NaN(), math.Inf(1), json.Number("01"), json.Number("1."), json.Number("1e"), true} {
		if _, ok := exactNumber(value); ok {
			t.Fatalf("invalid numeric value accepted: %v", value)
		}
	}
	largeExponent := strings.Repeat("9", 10000)
	left, ok := exactNumber(json.Number("1e" + largeExponent))
	if !ok {
		t.Fatal("large exponent rejected")
	}
	right, ok := exactNumber(json.Number("2e" + largeExponent))
	if !ok || left.compare(right) != -1 {
		t.Fatal("large exponent comparison failed")
	}
}

func TestExactDecimalCoercion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input, want    string
		integer, valid bool
	}{
		{" +9007199254740993 ", "9007199254740993", true, true},
		{"9007199254740992.5", "", true, false},
		{"9007199254740992.5", "9007199254740992.5", false, true},
		{"00012.00", "12", true, true},
		{".5", "0.5", false, true},
		{"1.", "1", true, true},
		{"1e999999999999999999999", "1e999999999999999999999", true, true},
		{"0x1p2", "4", true, true},
		{"NaN", "", false, false},
		{".", "", false, false},
	} {
		got, ok := coerceNumber(tc.input, tc.integer)
		if ok != tc.valid || ok && got != json.Number(tc.want) {
			t.Fatalf("coerce %q = %v, %v; want %q, %v", tc.input, got, ok, tc.want, tc.valid)
		}
		if ok && !json.Valid([]byte(got.(json.Number))) {
			t.Fatalf("invalid JSON number: %v", got)
		}
	}
}

func TestRegressionNumericValidationBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, property, input string }{
		{"maximum", `{"type":"integer","maximum":9007199254740992}`, `9007199254740993`},
		{"integer", `{"type":"integer"}`, `9007199254740992.5`},
		{"enum", `{"type":"integer","enum":[9007199254740992]}`, `9007199254740993`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := json.RawMessage(`{"type":"object","properties":{"n":` + tc.property + `},"required":["n"]}`)
			args, err := ValidateToolCall([]Tool{{Name: "test", InputSchema: schema}}, ToolCall{Name: "test", Arguments: json.RawMessage(`{"n":` + tc.input + `}`)})
			if err == nil {
				t.Fatalf("invalid input accepted: %#v", args)
			}
		})
	}
}
