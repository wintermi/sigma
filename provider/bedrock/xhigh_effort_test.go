// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"testing"

	"github.com/wintermi/sigma"
)

// Native xhigh effort exists only on newer Claude families; other adaptive
// models must not receive it unless their metadata maps xhigh explicitly.
func TestConverseXHighEffortRequiresNativeSupportOrMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id   sigma.ModelID
		want string
	}{
		{id: "global.anthropic.claude-sonnet-4-6", want: "high"},
		{id: "global.anthropic.claude-opus-4-6-v1", want: "max"},
		{id: "global.anthropic.claude-opus-4-7", want: "xhigh"},
		{id: "global.anthropic.claude-sonnet-5", want: "xhigh"},
	}
	for _, tt := range tests {
		model, ok := sigma.GetModel(sigma.ProviderAmazonBedrock, tt.id)
		if !ok {
			t.Fatalf("missing generated model %s", tt.id)
		}
		payload, err := conversePayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			sigma.Options{ReasoningLevel: sigma.ThinkingLevelXHigh}, Config{Region: "us-east-1"})
		if err != nil {
			t.Fatalf("%s: %v", tt.id, err)
		}
		outputConfig, _ := payload.AdditionalModelRequestFields["output_config"].(map[string]any)
		if got := outputConfig["effort"]; got != tt.want {
			t.Errorf("%s xhigh effort = %v, want %q", tt.id, got, tt.want)
		}
	}
}
