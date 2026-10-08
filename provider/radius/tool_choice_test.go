// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package radius

import (
	"encoding/json"
	"testing"

	"github.com/wintermi/sigma"
)

func TestRequestPayloadForwardsToolChoice(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ID: "radius-model", Provider: sigma.ProviderRadius}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}, Tools: []sigma.Tool{{Name: "read"}}}
	for _, choice := range []sigma.ToolChoice{"", sigma.ToolChoiceNone, "required"} {
		payload, err := requestPayload(model, req, sigma.Options{ToolChoice: choice})
		if err != nil {
			t.Fatalf("requestPayload returned error: %v", err)
		}
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("Marshal returned error: %v", err)
		}
		var decoded struct {
			Options map[string]any `json:"options"`
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal returned error: %v", err)
		}
		got, present := decoded.Options["toolChoice"]
		if choice == "" {
			if present {
				t.Fatalf("unset tool choice sent as %#v", got)
			}
			continue
		}
		if got != string(choice) {
			t.Fatalf("toolChoice = %#v, want %q", got, choice)
		}
	}
}
