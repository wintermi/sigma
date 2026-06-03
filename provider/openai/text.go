// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import "strings"

func providerText(text string) string {
	return strings.ToValidUTF8(text, "")
}
