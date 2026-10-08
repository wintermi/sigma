// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"fmt"
	"testing"

	"github.com/wintermi/sigma"
)

// Disabling thinking on a model that cannot turn it off must request the
// lowest level its metadata supports rather than a value the API rejects.
func TestDisabledThinkingRespectsModelMetadata(t *testing.T) {
	t.Parallel()

	off := true
	tests := []struct {
		id   sigma.ModelID
		want map[string]any
	}{
		{id: "gemini-3.8-flash", want: map[string]any{"thinkingLevel": "LOW"}},
		{id: "gemini-3.7-flash", want: map[string]any{"thinkingLevel": "LOW"}},
		{id: "gemini-2.5-pro", want: map[string]any{"thinkingBudget": 128}},
		{id: "gemini-2.5-flash", want: map[string]any{"thinkingBudget": 0}},
	}
	for _, tt := range tests {
		model, ok := sigma.GetModel(sigma.ProviderGoogle, tt.id)
		if !ok {
			t.Fatalf("missing generated model %s", tt.id)
		}
		for name, opts := range map[string]sigma.Options{
			"reasoning off":    {ReasoningLevel: sigma.ThinkingLevelOff},
			"disable thinking": {GoogleOptions: &sigma.GoogleOptions{DisableThinking: &off}},
		} {
			got, err := googleThinkingConfig(model, opts)
			if err != nil {
				t.Fatalf("%s %s: %v", tt.id, name, err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("%s %s: thinking config = %v, want %v", tt.id, name, got, tt.want)
			}
		}
	}
}
