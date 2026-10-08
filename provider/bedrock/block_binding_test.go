// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"fmt"
	"testing"

	"github.com/wintermi/sigma"
)

// Replayed signed thinking is bound to its original system prompt and tools;
// models that support it must drop stale blocks instead of failing the replay.
func TestConverseAdaptiveThinkingBlockBinding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id      sigma.ModelID
		region  string
		binding bool
	}{
		{id: "global.anthropic.claude-sonnet-5", region: "us-east-1", binding: true},
		{id: "global.anthropic.claude-opus-4-7", region: "us-east-1", binding: true},
		{id: "global.anthropic.claude-sonnet-4-6", region: "us-east-1"},
		{id: "global.anthropic.claude-sonnet-5", region: "us-gov-west-1"},
	}
	for _, tt := range tests {
		model, ok := sigma.GetModel(sigma.ProviderAmazonBedrock, tt.id)
		if !ok {
			t.Fatalf("missing generated model %s", tt.id)
		}
		payload, err := conversePayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh}, Config{Region: tt.region})
		if err != nil {
			t.Fatalf("%s: %v", tt.id, err)
		}
		fields := payload.AdditionalModelRequestFields
		thinking, _ := fields["thinking"].(map[string]any)
		_, hasBinding := thinking["block_binding"]
		beta := fmt.Sprint(fields["anthropic_beta"])
		if hasBinding != tt.binding || (tt.binding && beta != "[thinking-binding-controls-2026-08-01]") {
			t.Errorf("%s in %s: block_binding=%v anthropic_beta=%s, want binding %v", tt.id, tt.region, thinking["block_binding"], beta, tt.binding)
		}
		if tt.binding && fmt.Sprint(thinking["block_binding"]) != "map[prefix_mismatch_behavior:drop_block]" {
			t.Errorf("%s block_binding = %v, want drop_block", tt.id, thinking["block_binding"])
		}
	}
}
