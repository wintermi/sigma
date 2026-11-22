// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

// jsonSchemaReply scripts a reply that matches the probe schema.
func jsonSchemaReply() sigmatest.Script {
	return sigmatest.Script{Final: sigma.AssistantMessage{
		Content:    []sigma.ContentBlock{sigma.Text(`{"answer":"ok"}`)},
		StopReason: sigma.StopReasonEndTurn,
	}}
}

// An upstream can accept response_format and still ignore it, so a
// schema-requesting attempt only passes when the reply matches the schema.
func TestJSONSchemaValidatesResponse(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		text string
		stop sigma.StopReason
		ok   bool
	}{
		{"object", `{"answer":"ok"}`, sigma.StopReasonEndTurn, true},
		{"other string", " \n{ \"answer\" : \"yes\" }\t", sigma.StopReasonEndTurn, true},
		{"prose", "The answer is ok.", sigma.StopReasonEndTurn, false},
		{"fenced", "```json\n{\"answer\":\"ok\"}\n```", sigma.StopReasonEndTurn, false},
		{"truncated", `{"answer":`, sigma.StopReasonEndTurn, false},
		{"array", `["ok"]`, sigma.StopReasonEndTurn, false},
		{"null object", `null`, sigma.StopReasonEndTurn, false},
		{"missing field", `{}`, sigma.StopReasonEndTurn, false},
		{"wrong field", `{"ok":true}`, sigma.StopReasonEndTurn, false},
		{"null answer", `{"answer":null}`, sigma.StopReasonEndTurn, false},
		{"number answer", `{"answer":1}`, sigma.StopReasonEndTurn, false},
		{"extra field", `{"answer":"ok","extra":1}`, sigma.StopReasonEndTurn, false},
		{"trailing JSON", `{"answer":"ok"}{}`, sigma.StopReasonEndTurn, false},
		{"token limit", `{"answer":"ok"}`, sigma.StopReasonMaxTokens, false},
	} {
		for _, attempt := range []string{"json_schema", "json_schema_more_tokens"} {
			t.Run(tt.name+"/"+attempt, func(t *testing.T) {
				t.Parallel()
				probe := singleTurnCase(attempt, "strict JSON schema", basicRequest(`Return JSON exactly {"answer":"ok"}.`), nil)
				route := openAICompatibleSigmatestProbeRoute(t, []probeCase{probe}, sigmatest.Script{
					Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text(tt.text)}, StopReason: tt.stop},
				})
				results := collectProbeModel(context.Background(), route, "test", routeCredential{apiKey: "fake"}, config{})
				if len(results) != 1 || (results[0].Outcome == "ok") != tt.ok {
					t.Fatalf("results=%+v; want success=%t", results, tt.ok)
				}
			})
		}
	}

	// Fallbacks that drop the schema are not held to it.
	probe := singleTurnCase("manual_json", "prompt-level JSON only", basicRequest("Reply."), nil)
	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{probe}, sigmatest.Script{
		Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("not JSON")}, StopReason: sigma.StopReasonEndTurn},
	})
	if results := collectProbeModel(context.Background(), route, "test", routeCredential{apiKey: "fake"}, config{}); len(results) != 1 || results[0].Outcome != "ok" {
		t.Fatalf("manual_json results=%+v; want ok", results)
	}
}
