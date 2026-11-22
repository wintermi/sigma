// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"testing"

	"github.com/wintermi/sigma"
)

// MiniMax-M3.1-Flash takes adaptive thinking with output_config.effort and
// rejects disabled thinking, unlike the budget-thinking MiniMax-M3.
func TestMiniMaxM31FlashUsesAdaptiveEffort(t *testing.T) {
	t.Parallel()
	for _, provider := range []sigma.ProviderID{sigma.ProviderMiniMax, sigma.ProviderMiniMaxCN} {
		model, ok := sigma.GetModel(provider, "MiniMax-M3.1-Flash")
		if !ok {
			t.Fatalf("missing %s/MiniMax-M3.1-Flash", provider)
		}
		if model.SupportsThinkingLevel(sigma.ThinkingLevelOff) {
			t.Fatalf("%s MiniMax-M3.1-Flash supports thinking off", provider)
		}
		baseURL, _ := model.ProviderMetadata["baseURL"].(string)
		payload, err := messagesPayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			sigma.Options{ReasoningLevel: sigma.ThinkingLevelXHigh}, anthropicMessagesCompat(model, baseURL, nil))
		if err != nil {
			t.Fatal(err)
		}
		thinking, _ := payload["thinking"].(map[string]any)
		config, _ := payload["output_config"].(map[string]any)
		if thinking["type"] != "adaptive" || config["effort"] != "xhigh" {
			t.Fatalf("%s thinking = %#v, output_config = %#v; want adaptive xhigh", provider, payload["thinking"], payload["output_config"])
		}
	}
}
