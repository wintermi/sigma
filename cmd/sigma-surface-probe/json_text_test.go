// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestJSONTextValidatesResponse(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		text string
		stop sigma.StopReason
		ok   bool
	}{
		{"object", `{"ok":true}`, sigma.StopReasonEndTurn, true},
		{"whitespace", " \n{ \"ok\" : true }\t", sigma.StopReasonEndTurn, true},
		{"prose", "I cannot do that.", sigma.StopReasonEndTurn, false},
		{"fenced", "```json\n{\"ok\":true}\n```", sigma.StopReasonEndTurn, false},
		{"truncated", `{"ok":`, sigma.StopReasonEndTurn, false},
		{"array", `[true]`, sigma.StopReasonEndTurn, false},
		{"null", `null`, sigma.StopReasonEndTurn, false},
		{"wrong value", `{"ok":false}`, sigma.StopReasonEndTurn, false},
		{"extra field", `{"ok":true,"extra":1}`, sigma.StopReasonEndTurn, false},
		{"duplicate field", `{"ok":false,"ok":true}`, sigma.StopReasonEndTurn, false},
		{"trailing JSON", `{"ok":true}{}`, sigma.StopReasonEndTurn, false},
		{"token limit", `{"ok":true}`, sigma.StopReasonMaxTokens, false},
		{"filtered", `{"ok":true}`, sigma.StopReasonContentFilter, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			probe := singleTurnCase("json_text", "locally validated JSON", basicRequest(`Return JSON exactly {"ok":true}.`), nil)
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

func TestJSONTextModeUsesPlainTextWireRequest(t *testing.T) {
	t.Parallel()
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprintf("rejected=%t", rejected), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body struct {
					Messages  []struct{ Role, Content string } `json:"messages"`
					Format    struct{ Type string }            `json:"response_format"`
					MaxTokens int                              `json:"max_completion_tokens"`
					Stream    bool                             `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Format.Type != "text" || body.MaxTokens != 256 || !body.Stream || len(body.Messages) != 1 || body.Messages[0].Role != "user" || body.Messages[0].Content != `Return JSON exactly {"ok":true}.` {
					t.Errorf("unexpected fallback request: %+v", body)
				}
				if rejected {
					w.Header().Set("X-Request-ID", "text-rejection")
					w.WriteHeader(http.StatusForbidden)
					_, _ = fmt.Fprint(w, `{"error":"Content violates usage guidelines. Failed check: SAFETY_CHECK_TYPE_BIO"}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, part := range []string{`{"ok":`, `true}`} {
					content, _ := json.Marshal(part)
					_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%s}}]}\n\n", content)
				}
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			route := routes["xai"]
			route.BaseURL = server.URL + "/v1"
			results := collectProbeModel(context.Background(), route, "grok-4.6", routeCredential{apiKey: "fake"}, config{jsonText: true, repair: true, structuredOutput: true})
			if calls.Load() != 1 || len(results) != 1 {
				t.Fatalf("unexpected extra probes: calls=%d results=%+v", calls.Load(), results)
			}
			got := results[0]
			if got.Case != "json_text" || got.Hint != "" || (got.Outcome == "ok") == rejected {
				t.Fatalf("fallback misreported: %+v", got)
			}
			if rejected && (!strings.Contains(got.Error, "text-rejection") || !strings.Contains(got.Error, "SAFETY_CHECK_TYPE_BIO")) {
				t.Fatalf("lost rejection: %+v", got)
			}
		})
	}
}
