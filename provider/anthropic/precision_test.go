// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"encoding/json"
	"testing"
)

func TestInitialToolInputNumberPrecision(t *testing.T) {
	t.Parallel()
	var event streamEvent
	if err := decodeStreamEvent(`{"type":"content_block_start","content_block":{"type":"tool_use","id":"call1","name":"lookup","input":{"id":9007199254740993}}}`, &event); err != nil {
		t.Fatal(err)
	}
	if got := event.Content.Input.(map[string]any)["id"]; got != json.Number("9007199254740993") {
		t.Fatalf("initial input = %#v", got)
	}
}
