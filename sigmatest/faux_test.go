// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigmatest_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestFauxProviderScriptedSuccess(t *testing.T) {
	t.Parallel()

	expected := sigma.AssistantMessage{
		Content:    []sigma.ContentBlock{sigma.Text("hello")},
		StopReason: sigma.StopReasonEndTurn,
		Usage:      &sigma.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3},
		Model:      sigmatest.TextModelID,
		Provider:   sigmatest.ProviderID,
	}
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Events: []sigma.Event{
			{Kind: sigma.EventKindStart},
			{Kind: sigma.EventKindTextStart, ContentIndex: intPtr(0)},
			{Kind: sigma.EventKindTextDelta, ContentIndex: intPtr(0), DeltaText: "hello"},
			{Kind: sigma.EventKindTextEnd, ContentIndex: intPtr(0), Text: "hello"},
		},
		Final: expected,
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	final, err := client.Complete(context.Background(), sigmatest.TextModel(), sigma.Request{
		Messages: []sigma.Message{sigma.UserText("hi")},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if !reflect.DeepEqual(final, expected) {
		t.Fatalf("final = %#v, want %#v", final, expected)
	}
}

func TestFauxProviderScriptedError(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Err: errors.New("script failed"),
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	final, err := client.Complete(context.Background(), sigmatest.TextModel(), sigma.Request{})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if got, want := sigmaErr.Code, sigma.ErrorStream; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("final stop reason = %q, want %q", got, want)
	}
	if got, want := final.Model, sigmatest.TextModelID; got != want {
		t.Fatalf("final model = %q, want %q", got, want)
	}
}

func TestFauxProviderScriptExhaustionErrors(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxProvider()
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	final, err := client.Complete(context.Background(), sigmatest.TextModel(), sigma.Request{})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !strings.Contains(err.Error(), "no scripted response queued") {
		t.Fatalf("Complete error = %v, want script exhaustion", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("final stop reason = %q, want %q", got, want)
	}
}

func TestFauxProviderCancellation(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxProvider(sigmatest.Script{WaitForCancel: true})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	ctx, cancel := context.WithCancel(context.Background())
	stream := client.Stream(ctx, sigmatest.TextModel(), sigma.Request{})
	cancel()

	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if got, want := sigmaErr.Code, sigma.ErrorAborted; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("final stop reason = %q, want %q", got, want)
	}
}

func TestFauxProviderToolCallStreaming(t *testing.T) {
	t.Parallel()

	final := sigma.AssistantMessage{
		Content: []sigma.ContentBlock{
			sigma.Text("checking"),
			sigma.ToolCallBlock("call_1", "lookup", map[string]any{"q": "weather"}),
		},
		StopReason: sigma.StopReasonToolCalls,
		Model:      sigmatest.TextModelID,
		Provider:   sigmatest.ProviderID,
	}
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Events: []sigma.Event{
			{Kind: sigma.EventKindTextStart, ContentIndex: intPtr(0)},
			{Kind: sigma.EventKindToolCallStart, ContentIndex: intPtr(1), PartialToolCall: &sigma.PartialToolCall{ID: "call_1", Name: "lookup"}},
			{Kind: sigma.EventKindTextDelta, ContentIndex: intPtr(0), DeltaText: "checking"},
			{Kind: sigma.EventKindToolCallDelta, ContentIndex: intPtr(1), PartialToolCall: &sigma.PartialToolCall{ArgumentsDelta: `{"q"`}},
			{Kind: sigma.EventKindToolCallDelta, ContentIndex: intPtr(1), PartialToolCall: &sigma.PartialToolCall{ArgumentsDelta: `:"weather"}`}},
			{Kind: sigma.EventKindToolCallEnd, ContentIndex: intPtr(1), ToolCall: &sigma.ToolCall{ID: "call_1", Name: "lookup", Arguments: map[string]any{"q": "weather"}}},
			{Kind: sigma.EventKindTextEnd, ContentIndex: intPtr(0), Text: "checking"},
		},
		Final: final,
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	events := collectEvents(client.Stream(context.Background(), sigmatest.TextModel(), sigma.Request{}))
	if got, want := len(events), 8; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if got, want := events[3].PartialToolCall.ArgumentsDelta, `{"q"`; got != want {
		t.Fatalf("first tool delta = %q, want %q", got, want)
	}
	if got, want := events[4].PartialToolCall.ArgumentsDelta, `:"weather"}`; got != want {
		t.Fatalf("second tool delta = %q, want %q", got, want)
	}
	if events[5].ToolCall == nil {
		t.Fatal("tool-call end missing final tool call")
	}
	if got, want := *events[5].ContentIndex, 1; got != want {
		t.Fatalf("tool-call index = %d, want %d", got, want)
	}
	assertFinalEvent(t, events[len(events)-1], final)
}

func TestFauxProviderSynthesizesLifecycleFromFinalContent(t *testing.T) {
	t.Parallel()

	final := sigma.AssistantMessage{
		Content: []sigma.ContentBlock{
			sigma.Text("checking"),
			sigma.Thinking("plan", "sig"),
			sigma.ToolCallBlock("call_1", "lookup", map[string]any{"id": int64(9007199254740993)}),
		},
		StopReason: sigma.StopReasonToolCalls,
		Model:      sigmatest.TextModelID,
		Provider:   sigmatest.ProviderID,
	}
	provider := sigmatest.NewFauxProvider(sigmatest.Script{Final: final})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	events := collectEvents(client.Stream(context.Background(), sigmatest.TextModel(), sigma.Request{}))
	kinds := make([]sigma.EventKind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	wantKinds := []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindTextStart,
		sigma.EventKindTextDelta,
		sigma.EventKindTextEnd,
		sigma.EventKindThinkingStart,
		sigma.EventKindThinkingDelta,
		sigma.EventKindThinkingEnd,
		sigma.EventKindToolCallStart,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("event kinds = %#v, want %#v", kinds, wantKinds)
	}
	assertPartialText(t, events[1], "")
	assertPartialText(t, events[2], "checking")
	if events[4].PartialMessage == nil || len(events[4].PartialMessage.Content) != 2 {
		t.Fatalf("thinking start partial = %#v, want text plus empty thinking", events[4].PartialMessage)
	}
	if got, want := events[5].PartialMessage.Content[1].ThinkingText, "plan"; got != want {
		t.Fatalf("thinking partial = %q, want %q", got, want)
	}
	if events[7].PartialMessage == nil || len(events[7].PartialMessage.Content) != 3 {
		t.Fatalf("tool start partial = %#v, want three blocks", events[7].PartialMessage)
	}
	if got, want := events[8].PartialToolCall.ArgumentsDelta, `{"id":9007199254740993}`; got != want {
		t.Fatalf("tool arguments delta = %q, want %q", got, want)
	}
	assertFinalEvent(t, events[len(events)-1], final)
}

func TestFauxProviderThinkingStreaming(t *testing.T) {
	t.Parallel()

	final := sigma.AssistantMessage{
		Content: []sigma.ContentBlock{
			sigma.Thinking("plan", "sig"),
			sigma.Text("answer"),
		},
		StopReason: sigma.StopReasonEndTurn,
		Model:      sigmatest.TextModelID,
		Provider:   sigmatest.ProviderID,
	}
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Events: []sigma.Event{
			{Kind: sigma.EventKindThinkingStart, ContentIndex: intPtr(0)},
			{Kind: sigma.EventKindTextStart, ContentIndex: intPtr(1)},
			{Kind: sigma.EventKindThinkingDelta, ContentIndex: intPtr(0), DeltaText: "pl"},
			{Kind: sigma.EventKindTextDelta, ContentIndex: intPtr(1), DeltaText: "answer"},
			{Kind: sigma.EventKindThinkingDelta, ContentIndex: intPtr(0), DeltaText: "an"},
			{Kind: sigma.EventKindThinkingEnd, ContentIndex: intPtr(0), Thinking: "plan"},
			{Kind: sigma.EventKindTextEnd, ContentIndex: intPtr(1), Text: "answer"},
		},
		Final: final,
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	events := collectEvents(client.Stream(context.Background(), sigmatest.TextModel(), sigma.Request{}))
	if got, want := events[2].Kind, sigma.EventKindThinkingDelta; got != want {
		t.Fatalf("event[2] kind = %q, want %q", got, want)
	}
	if got, want := *events[2].ContentIndex, 0; got != want {
		t.Fatalf("thinking index = %d, want %d", got, want)
	}
	if got, want := *events[3].ContentIndex, 1; got != want {
		t.Fatalf("text index = %d, want %d", got, want)
	}
	assertFinalEvent(t, events[len(events)-1], final)
}

func TestFauxProviderRequestCaptureCopiesInputs(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("ok")}},
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	req := sigma.Request{
		Messages: []sigma.Message{sigma.UserText("original")},
		Tools: []sigma.Tool{{
			Name:        "lookup",
			InputSchema: map[string]any{"type": "object"},
		}},
	}
	if _, err := client.Complete(context.Background(), sigmatest.TextModel(), req, sigma.WithHeader("x-test", "one")); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	req.Messages[0].Content[0].Text = "mutated"

	capture, ok := provider.LastRequest()
	if !ok {
		t.Fatal("LastRequest returned no request")
	}
	if got, want := capture.Request.Messages[0].Content[0].Text, "original"; got != want {
		t.Fatalf("captured request text = %q, want %q", got, want)
	}
	capture.Request.Messages[0].Content[0].Text = "changed capture"
	capture.Options.Headers["x-test"] = "changed capture"

	capture, ok = provider.LastRequest()
	if !ok {
		t.Fatal("LastRequest returned no request after mutation")
	}
	if got, want := capture.Request.Messages[0].Content[0].Text, "original"; got != want {
		t.Fatalf("captured request text after mutation = %q, want %q", got, want)
	}
	if got, want := capture.Options.Headers["x-test"], "one"; got != want {
		t.Fatalf("captured header after mutation = %q, want %q", got, want)
	}
}

func ExampleFauxProvider() {
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("hello")},
		},
	})
	registry, _ := sigmatest.Registry(provider)
	client := sigma.NewClient(sigma.WithRegistry(registry))

	final, _ := client.Complete(context.Background(), sigmatest.TextModel(), sigma.Request{
		Messages: []sigma.Message{sigma.UserText("hi")},
	})
	fmt.Println(final.Content[0].Text)

	// Output: hello
}

func collectEvents(stream *sigma.Stream) []sigma.Event {
	var events []sigma.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events
}

func assertPartialText(t *testing.T, event sigma.Event, text string) {
	t.Helper()

	if event.PartialMessage == nil {
		t.Fatal("event missing partial message")
	}
	if got, want := len(event.PartialMessage.Content), 1; got != want {
		t.Fatalf("partial content count = %d, want %d", got, want)
	}
	if got, want := event.PartialMessage.Content[0].Text, text; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
}

func assertFinalEvent(t *testing.T, event sigma.Event, final sigma.AssistantMessage) {
	t.Helper()

	if got, want := event.Kind, sigma.EventKindDone; got != want {
		t.Fatalf("terminal event kind = %q, want %q", got, want)
	}
	if event.FinalMessage == nil {
		t.Fatal("terminal event missing final message")
	}
	if !reflect.DeepEqual(*event.FinalMessage, final) {
		t.Fatalf("terminal final = %#v, want %#v", *event.FinalMessage, final)
	}
}

func intPtr(value int) *int {
	return &value
}
