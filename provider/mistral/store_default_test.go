// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral

import (
	"testing"

	"github.com/wintermi/sigma"
)

// The Conversations API keeps conversations server-side unless store is
// false, so sigma opts out unless the caller asks to store.
func TestConversationStoreDefaultsToFalse(t *testing.T) {
	t.Parallel()
	model := sigma.Model{ID: "mistral-test", Provider: sigma.ProviderMistral, API: sigma.APIMistralConversations}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}
	for _, tt := range []struct {
		name string
		opts sigma.Options
		want any
	}{
		{name: "default", want: false},
		{name: "caller stores", opts: sigma.Options{ProviderOptions: map[sigma.ProviderID]map[string]any{sigma.ProviderMistral: {"store": true}}}, want: true},
	} {
		payload, err := conversationPayload(model, req, tt.opts)
		if err != nil {
			t.Fatal(err)
		}
		if got := payload["store"]; got != tt.want {
			t.Errorf("%s: store = %#v, want %#v", tt.name, got, tt.want)
		}
	}
}
