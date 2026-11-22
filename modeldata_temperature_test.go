// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"strings"
	"testing"
)

// Claude Opus 4.7 and later, Sonnet 5, and Haiku 5.5 reject temperature, so
// every Messages route to them must say so or the adapter sends it.
func TestClaudeMessagesRowsWithoutTemperatureSupport(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatal(err)
	}
	fragments := []string{"opus-4-7", "opus-4.7", "opus-4-8", "opus-4.8", "opus-5", "opus.5", "sonnet-5", "sonnet.5", "haiku-5-5", "haiku-5.5"}
	checked := 0
	for _, model := range registry.ListModels() {
		if model.API != APIAnthropicMessages || !containsAny(string(model.ID), fragments) {
			continue
		}
		checked++
		if model.AnthropicMessagesCompat == nil || model.AnthropicMessagesCompat.SupportsTemperature != AnthropicCompatSupport("unsupported") {
			t.Errorf("%s/%s does not mark temperature unsupported", model.Provider, model.ID)
		}
	}
	if checked == 0 {
		t.Fatal("no Claude Messages rows checked")
	}
}

func containsAny(value string, fragments []string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
