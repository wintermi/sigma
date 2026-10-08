// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestDetectedCompatMatchesXAIAndZAIByDomain(t *testing.T) {
	t.Parallel()

	model := sigma.Model{Provider: "custom", ID: "test"}
	tests := []struct {
		baseURL         string
		wantXAI         bool
		wantStreamUsage bool
	}{
		{baseURL: "https://api.x.ai/v1", wantXAI: true, wantStreamUsage: true},
		{baseURL: "https://x.ai/v1", wantXAI: true, wantStreamUsage: true},
		{baseURL: "https://fox.ai/v1", wantStreamUsage: true},
		{baseURL: "https://api.z.ai/api/paas/v4"},
		{baseURL: "https://fizz.ai/v1", wantStreamUsage: true},
	}
	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			t.Parallel()

			compat := detectedCompletionsCompat(model, tt.baseURL)
			if got := compat.maxTokensField == sigma.OpenAICompletionsMaxCompletionTokens; got != tt.wantXAI {
				t.Fatalf("xAI compat = %v, want %v", got, tt.wantXAI)
			}
			if compat.supportsStreamingUsage != tt.wantStreamUsage {
				t.Fatalf("supportsStreamingUsage = %v, want %v", compat.supportsStreamingUsage, tt.wantStreamUsage)
			}
		})
	}
}
