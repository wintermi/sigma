// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// decimalNumber represents sign * digits * 10^exponent. Nonzero digits have
// neither leading nor trailing zeroes. The exponent is never expanded, keeping
// work proportional to the supplied number even for extreme JSON exponents.
type decimalNumber struct {
	sign     int
	digits   string
	exponent big.Int
}

var (
	jsonDecimalPattern   = regexp.MustCompile(`^(-?)(0|[1-9][0-9]*)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$`)
	coerceDecimalPattern = regexp.MustCompile(`^([+-]?)([0-9]*)(?:\.([0-9]*))?(?:[eE]([+-]?[0-9]+))?$`)
)

func parseDecimal(text string, coercing bool) (decimalNumber, bool) {
	pattern := jsonDecimalPattern
	if coercing {
		pattern = coerceDecimalPattern
	}
	parts := pattern.FindStringSubmatch(text)
	if parts == nil || len(parts[2])+len(parts[3]) == 0 {
		return decimalNumber{}, false
	}
	number := decimalNumber{sign: 1}
	if parts[1] == "-" {
		number.sign = -1
	}
	if parts[4] != "" {
		if _, ok := number.exponent.SetString(parts[4], 10); !ok {
			return decimalNumber{}, false
		}
	}
	digits := strings.TrimLeft(parts[2]+parts[3], "0")
	if digits == "" {
		return decimalNumber{}, true
	}
	number.digits = strings.TrimRight(digits, "0")
	shift := int64(len(digits)-len(number.digits)) - int64(len(parts[3]))
	number.exponent.Add(&number.exponent, big.NewInt(shift))
	return number, true
}

func exactNumber(value any) (decimalNumber, bool) {
	var text string
	switch v := value.(type) {
	case json.Number:
		text = string(v)
	case int, int8, int16, int32, int64:
		text = strconv.FormatInt(reflect.ValueOf(v).Int(), 10)
	case uint, uint8, uint16, uint32, uint64:
		text = strconv.FormatUint(reflect.ValueOf(v).Uint(), 10)
	case float64:
		text = strconv.FormatFloat(v, 'g', -1, 64)
	case float32:
		text = strconv.FormatFloat(float64(v), 'g', -1, 32)
	default:
		return decimalNumber{}, false
	}
	return parseDecimal(text, false)
}

func (n decimalNumber) integer() bool {
	return n.sign == 0 || n.exponent.Sign() >= 0
}

func (n decimalNumber) String() string {
	if n.sign == 0 {
		return "0"
	}
	text := n.digits
	// Preserve ordinary decimal formatting without allowing an upstream exponent
	// to demand unbounded zero padding.
	if n.exponent.IsInt64() && n.exponent.Int64() >= -324 && n.exponent.Int64() <= 308 {
		shift := int(n.exponent.Int64())
		switch {
		case shift >= 0:
			text += strings.Repeat("0", shift)
		case len(text)+shift > 0:
			point := len(text) + shift
			text = text[:point] + "." + text[point:]
		default:
			text = "0." + strings.Repeat("0", -shift-len(text)) + text
		}
	} else {
		text += "e" + n.exponent.String()
	}
	if n.sign < 0 {
		text = "-" + text
	}
	return text
}

func (n decimalNumber) compare(other decimalNumber) int {
	if n.sign != other.sign {
		if n.sign < other.sign {
			return -1
		}
		return 1
	}
	if n.sign == 0 {
		return 0
	}
	var left, right big.Int
	left.Add(&n.exponent, big.NewInt(int64(len(n.digits))))
	right.Add(&other.exponent, big.NewInt(int64(len(other.digits))))
	if order := left.Cmp(&right); order != 0 {
		return n.sign * order
	}
	for i := range max(len(n.digits), len(other.digits)) {
		a, b := byte('0'), byte('0')
		if i < len(n.digits) {
			a = n.digits[i]
		}
		if i < len(other.digits) {
			b = other.digits[i]
		}
		if a < b {
			return -n.sign
		}
		if a > b {
			return n.sign
		}
	}
	return 0
}
