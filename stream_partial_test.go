// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestToolPartialAccumulationPrecedence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, delta string
		metadata    map[string]any
		want        string
	}{
		{"delta only", `1}`, nil, `{"n":1}`},
		{"full text only", "", map[string]any{"argumentsText": `{"n":2}`}, `{"n":2}`},
		{"both", `1}`, map[string]any{"argumentsText": `{"n":2}`}, `{"n":2}`},
		{"empty full text", `1}`, map[string]any{"argumentsText": ""}, ""},
		{"non-string full text", `1}`, map[string]any{"argumentsText": nil}, `{"n":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			block := partialBlock{kind: ContentBlockToolCall, arguments: `{"n":`}
			block.applyToolPartial(&PartialToolCall{ID: "id", Name: "tool", ArgumentsDelta: tc.delta, ProviderMetadata: tc.metadata})
			if block.arguments != tc.want {
				t.Fatalf("arguments=%q; want %q", block.arguments, tc.want)
			}
			final := block.contentBlock(true)
			if final.ToolCallID != "id" || final.ToolName != "tool" || !block.hasContent {
				t.Fatalf("lost tool identity: %#v", final)
			}
			if tc.want != "" {
				data, err := json.Marshal(final.ToolArguments)
				if err != nil || string(data) != tc.want {
					t.Fatalf("snapshot=%s, %v", data, err)
				}
			}
		})
	}
	arguments := map[string]any{"n": json.Number("9007199254740993")}
	block := partialBlock{kind: ContentBlockToolCall}
	block.applyToolPartial(&PartialToolCall{ProviderMetadata: map[string]any{"arguments": arguments, "argumentsText": `{"n":9007199254740993}`}})
	if !reflect.DeepEqual(block.contentBlock(true).ToolArguments, arguments) {
		t.Fatal("decoded arguments changed")
	}
}
