// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestReplayJSONNumbers(t *testing.T) {
	t.Parallel()
	const raw = `{"decimal":0.123456789012345678901,"exponent":1.234567890123456789e+42,"integer":9007199254740993}`
	for _, tc := range []struct {
		name    string
		value   any
		want    string
		invalid bool
	}{
		{name: "raw", value: json.RawMessage(raw), want: raw},
		{name: "bytes", value: []byte(raw), want: raw},
		{name: "numbers", value: map[string]any{"decimal": json.Number("0.123456789012345678901"), "exponent": json.Number("1.234567890123456789e+42"), "integer": json.Number("9007199254740993")}, want: raw},
		{name: "string", value: raw, want: `"{\"decimal\":0.123456789012345678901,\"exponent\":1.234567890123456789e+42,\"integer\":9007199254740993}"`},
		{name: "nil", want: "null"},
		{name: "invalid raw", value: json.RawMessage(`{"x":`), invalid: true},
		{name: "trailing bytes", value: []byte(`{} {}`), invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			value, err := jsonValue(tc.value)
			if (err != nil) != tc.invalid {
				t.Fatalf("error = %v", err)
			}
			if tc.invalid {
				return
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.want {
				t.Fatalf("JSON = %s, want %s", data, tc.want)
			}
		})
	}
}

func TestConverseNumericPersistenceReplay(t *testing.T) {
	t.Parallel()
	const numbers = `{"decimal":0.123456789012345678901,"exponent":1.234567890123456789e+42,"integer":9007199254740993}`
	transport := &fakeConverseClient{stream: fakeStream(
		ConverseEvent{Kind: ConverseEventMessageStart, Role: "assistant"},
		ConverseEvent{Kind: ConverseEventContentBlockStart, ToolUseID: "precise", ToolName: "lookup"},
		ConverseEvent{Kind: ConverseEventContentBlockDelta, ToolInputDelta: numbers},
		ConverseEvent{Kind: ConverseEventMessageStop, StopReason: "tool_use"},
	)}
	model := bedrockTestModel("bedrock-numeric-replay")
	client := bedrockTestClient(t, model.Provider, model, transport, fakeCredentialDetector{})
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("lookup")}}
	final, err := client.Complete(context.Background(), model, req)
	if err != nil {
		t.Fatal(err)
	}
	req.Messages = append(req.Messages, sigma.Message{Role: sigma.RoleAssistant, Provider: final.Provider, API: model.API, Model: final.Model, StopReason: final.StopReason, Content: final.Content}, sigma.ToolResult("precise", "ok"))
	data, err := sigma.MarshalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := sigma.UnmarshalRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	transport.stream = nil
	if _, err := client.Complete(context.Background(), model, restored); err != nil {
		t.Fatal(err)
	}
	outgoing, err := json.Marshal(transport.request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(outgoing), numbers) {
		t.Fatalf("outgoing = %s", outgoing)
	}
}

func TestConverseInterruptedPersistenceReplay(t *testing.T) {
	t.Parallel()
	transport := &fakeConverseClient{stream: fakeStream(
		ConverseEvent{Kind: ConverseEventMessageStart, Role: "assistant"},
		ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 0, TextDelta: "visible"},
		ConverseEvent{Kind: ConverseEventContentBlockStart, ContentBlockIndex: 1, ToolUseID: "failed", ToolName: "lookup"},
		ConverseEvent{Kind: ConverseEventContentBlockDelta, ContentBlockIndex: 1, ToolInputDelta: `{"x":`},
	)}
	model := bedrockTestModel("bedrock-interrupted-replay")
	client := bedrockTestClient(t, model.Provider, model, transport, fakeCredentialDetector{})
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("lookup")}}
	final, err := client.Complete(context.Background(), model, req)
	if err == nil || final.StopReason != sigma.StopReasonError {
		t.Fatalf("final %#v, error %v", final, err)
	}
	req.Messages = append(req.Messages, sigma.Message{Role: sigma.RoleAssistant, Provider: final.Provider, API: model.API, Model: final.Model, StopReason: final.StopReason, Content: final.Content}, sigma.ToolResult("failed", "discarded"), sigma.UserText("continue"))
	data, err := sigma.MarshalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := sigma.UnmarshalRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	transport.stream = nil
	if _, err := client.Complete(context.Background(), model, restored); err != nil {
		t.Fatal(err)
	}
	outgoing, err := json.Marshal(transport.request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(outgoing), "failed") || strings.Contains(string(outgoing), "discarded") || !strings.Contains(string(outgoing), "visible") {
		t.Fatalf("outgoing = %s", outgoing)
	}
}
