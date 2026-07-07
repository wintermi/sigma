// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package streamblocks

import (
	"strings"
	"unicode/utf8"
)

func completePartialJSON(input string) (string, bool) {
	var out strings.Builder
	out.Grow(len(input) + 8)
	closers := make([]rune, 0, 8)
	inString := false
	escaped := false
	lastSignificant := rune(0)

	for index := 0; index < len(input); {
		r, size := utf8.DecodeRuneInString(input[index:])
		if r == utf8.RuneError && size == 1 {
			out.WriteByte(input[index])
			index++
			continue
		}
		out.WriteRune(r)
		index += size

		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch r {
			case '\\':
				escaped = true
			case '"':
				inString = false
				lastSignificant = '"'
			}
			continue
		}

		if isJSONWhitespace(r) {
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			closers = append(closers, '}')
			lastSignificant = r
		case '[':
			closers = append(closers, ']')
			lastSignificant = r
		case '}', ']':
			if len(closers) == 0 || closers[len(closers)-1] != r {
				return "", false
			}
			closers = closers[:len(closers)-1]
			lastSignificant = r
		case ':', ',':
			lastSignificant = r
		default:
			lastSignificant = r
		}
	}
	if escaped {
		out.WriteByte('\\')
	}
	if inString {
		out.WriteByte('"')
		lastSignificant = '"'
	}
	if lastSignificant == ':' || lastSignificant == ',' {
		return "", false
	}
	for index := len(closers) - 1; index >= 0; index-- {
		out.WriteRune(closers[index])
	}
	return out.String(), true
}

func repairJSON(input string) string {
	var repaired strings.Builder
	repaired.Grow(len(input))
	inString := false

	for index := 0; index < len(input); {
		r, size := utf8.DecodeRuneInString(input[index:])
		if r == utf8.RuneError && size == 1 {
			repaired.WriteByte(input[index])
			index++
			continue
		}

		if !inString {
			repaired.WriteString(input[index : index+size])
			if r == '"' {
				inString = true
			}
			index += size
			continue
		}

		switch r {
		case '"':
			repaired.WriteByte('"')
			inString = false
			index += size
		case '\\':
			if index+1 >= len(input) {
				repaired.WriteString(`\\`)
				index += size
				continue
			}
			next := input[index+1]
			if next == 'u' && index+6 <= len(input) && isHex4(input[index+2:index+6]) {
				repaired.WriteString(input[index : index+6])
				index += 6
				continue
			}
			if isJSONEscape(next) {
				repaired.WriteByte('\\')
				repaired.WriteByte(next)
				index += 2
				continue
			}
			repaired.WriteString(`\\`)
			index += size
		default:
			if r >= 0 && r <= 0x1f {
				writeEscapedControl(&repaired, r)
			} else {
				repaired.WriteString(input[index : index+size])
			}
			index += size
		}
	}
	return repaired.String()
}

func isJSONWhitespace(r rune) bool {
	return r == ' ' || r == '\n' || r == '\r' || r == '\t'
}

func isJSONEscape(value byte) bool {
	switch value {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return true
	default:
		return false
	}
}

func isHex4(value string) bool {
	if len(value) != 4 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

func writeEscapedControl(out *strings.Builder, r rune) {
	switch r {
	case '\b':
		out.WriteString(`\b`)
	case '\f':
		out.WriteString(`\f`)
	case '\n':
		out.WriteString(`\n`)
	case '\r':
		out.WriteString(`\r`)
	case '\t':
		out.WriteString(`\t`)
	default:
		const hex = "0123456789abcdef"
		value := int(r)
		out.WriteString(`\u00`)
		out.WriteByte(hex[value>>4])
		out.WriteByte(hex[value&0x0f])
	}
}
