// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func TestCompletionsInheritsDefaultToolSuppression(t *testing.T) {
	t.Parallel()
	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeFixture(t, w, "text_usage.sse")
	}))
	defer server.Close()
	model := openAITestModel("openai-default-choice")
	base := openAITestClient(t, model.Provider, model, server.URL)
	client := sigma.NewClient(sigma.WithRegistry(base.Registry()), sigma.WithDefaultOptions(sigma.WithToolChoice(sigma.ToolChoiceNone)))
	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("hi")},
		Tools:    []sigma.Tool{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}},
	}, sigma.WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["tool_choice"] != "none" {
		t.Fatalf("tool choice = %v, want none", payload["tool_choice"])
	}
	if tools, ok := payload["tools"].([]any); !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want retained declaration", payload["tools"])
	}
}

func TestCompletionsToolEventMutationPreservesFinal(t *testing.T) {
	t.Parallel()
	resume := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"city\\\":\\\"Melbourne\\\",\\\"number\\\":9007199254740993,\\\"empty\\\":{}}\"}}]}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-resume:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	model := openAITestModel("openai-event-ownership")
	client := openAITestClient(t, model.Provider, model, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := client.Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	defer stream.Close()
	deltaSeen, endSeen := false, false
	for event := range stream.Events() {
		if event.Kind == sigma.EventKindToolCallDelta {
			args := event.PartialToolCall.ProviderMetadata["arguments"].(map[string]any)
			args["city"] = "partial mutation"
			deltaSeen = true
			close(resume)
		}
		if event.Kind == sigma.EventKindToolCallEnd {
			args := event.ToolCall.Arguments.(map[string]any)
			if args["city"] != "Melbourne" {
				t.Errorf("end arguments = %#v", args)
			}
			args["city"] = "end mutation"
			endSeen = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	final, ok := stream.Final()
	if !ok || !deltaSeen || !endSeen || len(final.Content) != 1 {
		t.Fatalf("final=%#v, delta=%v end=%v", final, deltaSeen, endSeen)
	}
	data, err := json.Marshal(final.Content[0].ToolArguments)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"city":"Melbourne","empty":{},"number":9007199254740993}`; got != want {
		t.Fatalf("final arguments=%s, want %s", got, want)
	}
}
