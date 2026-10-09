// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"testing"

	"github.com/wintermi/sigma"
)

// Bedrock applies a 4096-token default output cap to Claude when the request
// leaves maxTokens out, truncating long answers and adaptive thinking.
func TestConversePayloadDefaultsClaudeMaxTokensToModelLimit(t *testing.T) {
	t.Parallel()

	claude := bedrockTestModel(sigma.ProviderAmazonBedrock)
	claude.ID = "us.anthropic.claude-opus-4-7"
	claude.MaxOutputTokens = 128000
	nova := bedrockTestModel(sigma.ProviderAmazonBedrock)
	nova.ID = "amazon.nova-pro-v1:0"
	nova.MaxOutputTokens = 10000
	explicit := 2048

	tests := []struct {
		name  string
		model sigma.Model
		opts  sigma.Options
		want  int
	}{
		{name: "claude without options", model: claude, want: 128000},
		{name: "claude adaptive thinking", model: claude, opts: sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh}, want: 128000},
		{name: "claude explicit max tokens", model: claude, opts: sigma.Options{MaxTokens: &explicit}, want: 2048},
		{name: "non-claude keeps provider default", model: nova, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload, err := conversePayload(tt.model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, tt.opts, Config{})
			if err != nil {
				t.Fatal(err)
			}
			got := 0
			if payload.InferenceConfig != nil && payload.InferenceConfig.MaxTokens != nil {
				got = *payload.InferenceConfig.MaxTokens
			}
			if got != tt.want {
				t.Fatalf("inferenceConfig.maxTokens = %d, want %d", got, tt.want)
			}
		})
	}
}
