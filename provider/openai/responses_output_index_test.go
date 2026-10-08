// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func responsesEventStream(events ...string) string {
	var body strings.Builder
	for _, event := range events {
		body.WriteString("data: " + event + "\n\n")
	}
	return body.String()
}

func completeResponsesStream(t *testing.T, body string) (sigma.AssistantMessage, error) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponsesSSE(t, w, body)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-output-index-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	return client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("hi")},
		Tools: []sigma.Tool{
			{Name: "read_file", InputSchema: sigma.Schema{"type": "object"}},
			{Name: "delete_file", InputSchema: sigma.Schema{"type": "object"}},
		},
	})
}

// Servers such as llama.cpp omit output_index; every item must keep its own
// identity and arguments instead of collapsing onto the first output slot.
func TestResponsesStreamWithoutOutputIndexKeepsItemsSeparate(t *testing.T) {
	t.Parallel()

	final, err := completeResponsesStream(t, responsesEventStream(
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.output_item.added","item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","delta":"Working."}`,
		`{"type":"response.output_item.done","item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Working."}]}}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","delta":"{\"path\":\"a.txt\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"delete_file","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","delta":"{\"path\":\"b.txt\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"delete_file","arguments":"{\"path\":\"b.txt\"}"}}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[`+
			`{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Working."}]},`+
			`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"a.txt\"}"},`+
			`{"type":"function_call","id":"fc_2","call_id":"call_2","name":"delete_file","arguments":"{\"path\":\"b.txt\"}"}]}}`,
	))
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var got []string
	for _, block := range final.Content {
		switch block.Type {
		case sigma.ContentBlockText:
			got = append(got, "text:"+block.Text)
		case sigma.ContentBlockToolCall:
			got = append(got, fmt.Sprintf("call:%s:%s:%v", block.ToolCallID, block.ToolName, block.ToolArguments))
		}
	}
	want := []string{
		"text:Working.",
		"call:call_1:read_file:map[path:a.txt]",
		"call:call_2:delete_file:map[path:b.txt]",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("content = %v, want %v", got, want)
	}
}

func TestResponsesStreamRejectsToolCallsSharingAnOutputIndex(t *testing.T) {
	t.Parallel()

	_, err := completeResponsesStream(t, responsesEventStream(
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":""}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":"{}"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"delete_file","arguments":""}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"delete_file","arguments":"{}"}}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[]}}`,
	))
	var providerErr *sigma.ProviderError
	if !errors.As(err, &providerErr) || !strings.Contains(err.Error(), "output index") {
		t.Fatalf("error = %v, want typed provider error for a reused output index", err)
	}
}
