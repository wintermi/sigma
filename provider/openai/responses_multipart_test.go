// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

func TestResponsesMultipartPartLifecycle(t *testing.T) {
	t.Parallel()
	model := sigma.Model{ID: "test", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses}
	for _, opts := range []responsesStreamOptions{{}, {codexTerminalSignals: true}} {
		writer := &multipartWriter{}
		parser := newResponsesStreamParser(writer, model, opts)
		events := []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg","type":"message","phase":"final_answer","content":[]}}`,
			`{"type":"response.output_text.delta","output_index":0,"content_index":2,"delta":"last"}`,
			`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"first"}`,
			`{"type":"response.content_part.added","output_index":0,"content_index":1,"part":{"type":"refusal","refusal":""}}`,
			`{"type":"response.refusal.delta","output_index":0,"content_index":1,"delta":" middle "}`,
			`{"type":"response.output_text.done","output_index":0,"content_index":0,"text":"first"}`,
			`{"type":"response.refusal.done","output_index":0,"content_index":1,"refusal":" middle "}`,
			`{"type":"response.content_part.done","output_index":0,"content_index":2,"part":{"type":"output_text","text":"last"}}`,
			`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","id":"msg"}]}}`,
		}
		for _, event := range events {
			if _, err := parser.handleEventData(context.Background(), "", event); err != nil {
				t.Fatal(err)
			}
		}
		var texts []string
		starts := 0
		for _, event := range writer.events {
			if event.Kind == sigma.EventKindTextStart {
				starts++
			}
			if event.Kind == sigma.EventKindTextDelta {
				texts = append(texts, event.Text)
				if event.ContentIndex == nil || *event.ContentIndex != 0 {
					t.Fatal("public content index changed")
				}
			}
		}
		if starts != 1 || !reflect.DeepEqual(texts, []string{"last", "firstlast", "first middle last"}) {
			t.Fatalf("streamed texts=%v starts=%d", texts, starts)
		}
		assertMultipartText(t, parser.finalize(context.Background()), "first middle last")
		var snapshot responsesResponse
		if err := json.Unmarshal([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"first"},{"type":"output_text","text":""},{"type":"refusal","refusal":" middle last"}]}]}`), &snapshot); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			parser.captureTerminalResponse(snapshot)
			assertMultipartText(t, parser.finalize(context.Background()), "first middle last")
		}
	}
}

func assertMultipartText(t *testing.T, message sigma.AssistantMessage, want string) {
	t.Helper()
	if len(message.Content) != 1 || message.Content[0].Text != want {
		t.Fatalf("content = %#v, want one block %q", message.Content, want)
	}
}

type multipartWriter struct {
	auditWriter
	events []sigma.Event
}

func (w *multipartWriter) Emit(_ context.Context, event sigma.Event) error {
	w.events = append(w.events, event)
	return nil
}
