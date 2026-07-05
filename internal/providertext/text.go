// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package providertext

import (
	"encoding/json"
	"strings"
)

// Clean removes invalid UTF-8 before provider JSON encoding.
func Clean(text string) string {
	return strings.ToValidUTF8(text, "")
}

// ToolArgumentsText serializes tool-call arguments for provider payloads:
// nil becomes an empty JSON object and pre-serialized strings pass through.
func ToolArgumentsText(arguments any) (string, error) {
	if arguments == nil {
		return "{}", nil
	}
	if text, ok := arguments.(string); ok {
		return text, nil
	}
	data, err := json.Marshal(arguments)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
