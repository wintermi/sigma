// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestAdaptiveXHighEffortRequiresNativeSupportOrMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id   sigma.ModelID
		want string
	}{
		{id: "claude-sonnet-4-6", want: "high"},
		{id: "claude-opus-4-6", want: "max"},
		{id: "claude-opus-4-7", want: "xhigh"},
		{id: "claude-sonnet-5", want: "xhigh"},
	}
	for _, tt := range tests {
		model, ok := sigma.GetModel(sigma.ProviderAnthropic, tt.id)
		if !ok {
			t.Fatalf("missing generated model %s", tt.id)
		}
		if got := adaptiveEffort(model, sigma.Options{ReasoningLevel: sigma.ThinkingLevelXHigh}); got != tt.want {
			t.Errorf("%s xhigh effort = %q, want %q", tt.id, got, tt.want)
		}
	}
}
