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

// OpenAI models on Bedrock take a reasoning effort rather than an Anthropic
// thinking budget, and Bedrock rejects minimal effort.
func TestConverseOpenAIReasoningEffort(t *testing.T) {
	t.Parallel()

	budget := 4096
	tests := []struct {
		name string
		id   sigma.ModelID
		opts sigma.Options
		want map[string]any
	}{
		{name: "gpt-oss minimal", id: "openai.gpt-oss-120b", opts: sigma.Options{ReasoningLevel: sigma.ThinkingLevelMinimal}, want: map[string]any{"reasoning_effort": "low"}},
		{name: "gpt-oss xhigh clamps to high", id: "openai.gpt-oss-120b", opts: sigma.Options{ReasoningLevel: sigma.ThinkingLevelXHigh}, want: map[string]any{"reasoning_effort": "high"}},
		{name: "gpt minimal", id: "openai.gpt-5.5", opts: sigma.Options{ReasoningLevel: sigma.ThinkingLevelMinimal}, want: map[string]any{"reasoning": map[string]any{"effort": "low"}}},
		{name: "gpt high", id: "openai.gpt-5.5", opts: sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh}, want: map[string]any{"reasoning": map[string]any{"effort": "high"}}},
		{name: "gpt budget only", id: "openai.gpt-5.5", opts: sigma.Options{ThinkingBudgetTokens: &budget}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, ok := sigma.GetModel(sigma.ProviderAmazonBedrock, tt.id)
			if !ok {
				t.Fatalf("missing generated model %s", tt.id)
			}
			model.SupportsThinking = true
			payload, err := conversePayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, tt.opts, Config{Region: "us-east-1"})
			if err != nil {
				t.Fatal(err)
			}
			got := payload.AdditionalModelRequestFields
			if len(got) == 0 && tt.want == nil {
				return
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("additional fields = %v, want %v", got, tt.want)
			}
		})
	}
}
