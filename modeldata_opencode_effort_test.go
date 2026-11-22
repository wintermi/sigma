// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import "testing"

// OpenCode forwards per-message effort and thinking.block_binding to Claude.
// Without drop_block, replayed thinking is rejected once the system prompt or
// tools change, so OpenCode rows must follow their direct Anthropic sibling.
func TestOpenCodeClaudeRowsFollowAnthropicMidConversationEffort(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	if err := registerBuiltinTextModels(registry); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, model := range registry.ListModels() {
		if model.Provider != ProviderOpenCode && model.Provider != ProviderOpenCodeGo {
			continue
		}
		direct, ok := registry.Model(ProviderAnthropic, model.ID)
		if !ok || direct.AnthropicMessagesCompat == nil || direct.AnthropicMessagesCompat.SupportsMidConversationEffort != AnthropicCompatSupport("supported") {
			continue
		}
		checked++
		if model.AnthropicMessagesCompat == nil || model.AnthropicMessagesCompat.SupportsMidConversationEffort != AnthropicCompatSupport("supported") {
			t.Errorf("%s/%s does not support mid-conversation effort like %s/%s", model.Provider, model.ID, direct.Provider, direct.ID)
		}
	}
	if checked == 0 {
		t.Fatal("no OpenCode Claude rows checked")
	}
}
