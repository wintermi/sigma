// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

// From GPT-6.2 on, OpenAI withholds replayed reasoning after an edit of
// earlier input, or from another organization, user, or model, and reports
// it in input_transformations. The final message records those removals so
// callers know the model did not see that reasoning.
func TestResponsesFinalReportsRemovedReasoning(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, transformations string
		want                  any
	}{
		{
			name:            "removals other than turn boundaries",
			transformations: `[{"type":"reasoning","id":"rs_1","action":"removed","reason":"prefix_mismatch"},{"type":"reasoning","id":"rs_0","action":"removed","reason":"turn_boundary"},{"type":"reasoning","id":"rs_2","action":"removed","reason":"model_mismatch"}]`,
			want: []any{
				map[string]any{"type": "reasoning", "id": "rs_1", "action": "removed", "reason": "prefix_mismatch"},
				map[string]any{"type": "reasoning", "id": "rs_2", "action": "removed", "reason": "model_mismatch"},
			},
		},
		{name: "only turn boundaries", transformations: `[{"type":"reasoning","id":"rs_0","action":"removed","reason":"turn_boundary"}]`},
		{name: "malformed", transformations: `{"unexpected":true}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeResponsesSSE(t, w, `data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-test","status":"completed","input_transformations":`+tt.transformations+`,"output":[{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","id":"text_1","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}
`)
			}))
			t.Cleanup(server.Close)
			providerID := sigma.ProviderID("responses-transformations-test")
			model := responsesTestModel(providerID)
			client := responsesTestClient(t, providerID, model, server.URL)
			final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
			if err != nil {
				t.Fatal(err)
			}
			if got := final.ProviderMetadata["input_transformations"]; !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("input_transformations = %#v, want %#v", got, tt.want)
			}
		})
	}
}
