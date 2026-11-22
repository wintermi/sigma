// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestMistralReasoningEffortOff(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		model sigma.Model
		level sigma.ThinkingLevel
		want  any
	}{
		{name: "adjustable model sends none", model: sigma.Model{ID: "mistral-medium-3-5", SupportsThinking: true}, level: sigma.ThinkingLevelOff, want: "none"},
		{
			name: "mapped off value",
			model: sigma.Model{
				ID: "mistral-small-2603", SupportsThinking: true,
				ThinkingLevelMap: map[sigma.ThinkingLevel]string{sigma.ThinkingLevelOff: "disabled"},
			},
			level: sigma.ThinkingLevelOff,
			want:  "disabled",
		},
		{name: "unset level keeps the default", model: sigma.Model{ID: "mistral-medium-3-5", SupportsThinking: true}},
		{name: "prompt-mode model", model: sigma.Model{ID: "magistral-medium-latest", SupportsThinking: true}, level: sigma.ThinkingLevelOff},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := map[string]any{}
			addMistralReasoningArgs(args, tt.model, sigma.Options{ReasoningLevel: tt.level})
			if got := args["reasoning_effort"]; got != tt.want {
				t.Fatalf("reasoning_effort = %#v, want %#v", got, tt.want)
			}
		})
	}
}
