// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func TestXAIJSONObjectSafetyRejectionPreservesEvidence(t *testing.T) {
	t.Parallel()
	const requestID = "0173a99d-7066-909e-bda7-9eb1667b8b04"
	const instructionRequestID = "json-object-instruction-request"
	const rejection = `{"code":"permission-denied","error":"Content violates usage guidelines. Failed check: SAFETY_CHECK_TYPE_BIO"}`
	type request struct {
		Model          string                           `json:"model"`
		Messages       []struct{ Role, Content string } `json:"messages"`
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Tools json.RawMessage `json:"tools"`
	}
	requests := make(chan request, 9)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL.Path)
		}
		var body request
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case requests <- body:
		default:
			t.Error("unexpected extra request")
		}
		if body.ResponseFormat.Type == "json_object" && len(body.Messages) > 0 && body.Messages[len(body.Messages)-1].Content == `Return JSON exactly {"ok":true}.` {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-ID", requestID)
			if len(body.Messages) == 2 {
				w.Header().Set("X-Request-ID", instructionRequestID)
			}
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, rejection)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"{\\\"answer\\\":\\\"ok\\\"}\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	route := routes["xai"]
	route.BaseURL = server.URL + "/v1"
	route.Cases = func(route routeSpec, model sigma.Model) []probeCase {
		return structuredOutputProbeCases(openAICompatibleProbeCases(route, model))
	}
	results := collectProbeModel(context.Background(), route, "grok-4.6", routeCredential{apiKey: "test-key"}, config{repair: true, caseTimeout: 2 * time.Second})
	if len(results) != 2 {
		t.Fatalf("results=%+v", results)
	}
	failed := results[0]
	if failed.Case != "json_object" || failed.Attempt != "json_object" || failed.Outcome != "inconclusive" || !failed.AvailabilityOKAfterFailure {
		t.Fatalf("safety rejection classified incorrectly: %+v", failed)
	}
	if !strings.Contains(failed.Error, requestID) || !strings.Contains(failed.Error, "SAFETY_CHECK_TYPE_BIO") {
		t.Fatalf("lost provider evidence: %s", failed.Error)
	}
	assertFailedAttempts(t, failed.FailedAttempts, []failedAttempt{
		{Attempt: "json_object", Error: failed.Error},
		{Attempt: "json_object_explicit_instruction", Error: strings.ReplaceAll(failed.Error, requestID, instructionRequestID)},
	})
	if !reflect.DeepEqual(failed.SuccessfulControls, []string{"json_object_schema_control", "json_object_text_control", "json_object_string_value", "json_object_number_value", "json_object_false_value"}) {
		t.Fatalf("missing format controls: %+v", failed)
	}
	if _, ok := recommendationFor(failed); ok || failed.Hint != "" {
		t.Fatalf("safety rejection produced a repair: %+v", failed)
	}
	if results[1].Case != "json_schema" || results[1].Outcome != "ok" {
		t.Fatalf("later schema case affected: %+v", results[1])
	}
	var totals summary
	for _, result := range results {
		totals.add(result)
	}
	if totals.Total != 2 || totals.OK != 1 || totals.Inconclusive != 1 || totals.AvailabilityOKAfterFailure != 1 || totals.FixedByRepairVariant != 0 {
		t.Fatalf("summary=%+v", totals)
	}
	if len(requests) != 9 {
		t.Fatalf("requests=%d; want original, availability, six controls, and following schema case", len(requests))
	}
	for i, format := range []string{"json_object", "", "json_object", "json_schema", "text", "json_object", "json_object", "json_object", "json_schema"} {
		body := <-requests
		messageCount := 1
		if i == 2 {
			messageCount = 2
		}
		if body.Model != "grok-4.6" || body.ResponseFormat.Type != format || len(body.Messages) != messageCount || len(body.Tools) != 0 {
			t.Fatalf("request contaminated or wrong capability: %+v", body)
		}
		if i == 2 && body.Messages[0].Role != "system" {
			t.Fatalf("missing formatting instruction: %+v", body.Messages)
		}
		user := body.Messages[len(body.Messages)-1]
		if user.Role != "user" {
			t.Fatalf("missing user message: %+v", body.Messages)
		}
		if (i == 0 || i >= 2 && i <= 4) && user.Content != `Return JSON exactly {"ok":true}.` {
			t.Fatalf("JSON-object control changed task: %+v", body.Messages)
		}
		if i >= 5 && i <= 7 {
			object := []string{`{"ok":"ready"}`, `{"ok":1}`, `{"ok":false}`}[i-5]
			if user.Content != "Return JSON exactly "+object+"." {
				t.Fatalf("value control did not reach the wire: %+v", body.Messages)
			}
		}
	}
}
