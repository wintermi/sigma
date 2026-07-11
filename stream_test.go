// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestStreamCollectsFinalMessage(t *testing.T) {
	t.Parallel()

	stream, writer := sigma.NewStream(context.Background())
	writeErr := make(chan error, 1)
	go func() {
		if err := writer.Emit(context.Background(), sigma.Event{
			Kind:      sigma.EventKindTextDelta,
			DeltaText: "Hel",
		}); err != nil {
			writeErr <- err
			return
		}
		writeErr <- writer.Done(context.Background(), sigma.AssistantMessage{
			Content:    []sigma.ContentBlock{sigma.Text("Hello")},
			StopReason: sigma.StopReasonEndTurn,
		})
	}()

	final, err := sigma.Collect(context.Background(), stream)
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if err := receiveErr(t, writeErr); err != nil {
		t.Fatalf("writer returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "Hello"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonEndTurn; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if stream.Err() != nil {
		t.Fatalf("stream Err() = %v, want nil", stream.Err())
	}
}

func TestStreamCollectsFauxProviderScript(t *testing.T) {
	t.Parallel()

	expected := sigma.AssistantMessage{
		Content:    []sigma.ContentBlock{sigma.Text("Hello")},
		StopReason: sigma.StopReasonEndTurn,
		Model:      sigmatest.TextModelID,
		Provider:   sigmatest.ProviderID,
	}
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Events: []sigma.Event{
			{Kind: sigma.EventKindTextDelta, DeltaText: "Hel"},
			{Kind: sigma.EventKindTextDelta, DeltaText: "lo"},
		},
		Final: expected,
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	final, err := sigma.Collect(context.Background(), client.Stream(context.Background(), sigmatest.TextModel(), sigma.Request{}))
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if got, want := final.Content[0].Text, expected.Content[0].Text; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
}

func TestStreamCollectsProviderError(t *testing.T) {
	t.Parallel()

	stream, writer := sigma.NewStream(context.Background())
	writeErr := make(chan error, 1)
	go func() {
		writeErr <- writer.Error(
			context.Background(),
			stderrors.New("provider disconnected"),
			sigma.AssistantMessage{StopReason: sigma.StopReasonError},
		)
	}()

	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	if err := receiveErr(t, writeErr); err != nil {
		t.Fatalf("writer returned error: %v", err)
	}
	var streamErr *sigma.Error
	if !stderrors.As(err, &streamErr) {
		t.Fatalf("Collect error type = %T, want *sigma.Error", err)
	}
	if got, want := streamErr.Code, sigma.ErrorStream; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("final stop reason = %q, want %q", got, want)
	}
}

func TestStreamContextCancelEmitsAbortedError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	stream, _ := sigma.NewStream(ctx)
	cancel()

	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	var streamErr *sigma.Error
	if !stderrors.As(err, &streamErr) {
		t.Fatalf("Collect error type = %T, want *sigma.Error", err)
	}
	if got, want := streamErr.Code, sigma.ErrorAborted; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("final stop reason = %q, want %q", got, want)
	}
}

func TestStreamContextCancelReturnsPartialFinalMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		event     sigma.Event
		assertion func(*testing.T, sigma.AssistantMessage)
	}{
		{
			name: "text delta",
			event: sigma.Event{
				Kind:      sigma.EventKindTextDelta,
				DeltaText: "partial text",
				Text:      "partial text",
			},
			assertion: func(t *testing.T, final sigma.AssistantMessage) {
				t.Helper()
				if got, want := final.Content[0].Text, "partial text"; got != want {
					t.Fatalf("partial text = %q, want %q", got, want)
				}
			},
		},
		{
			name: "thinking delta",
			event: sigma.Event{
				Kind:      sigma.EventKindThinkingDelta,
				DeltaText: "partial plan",
				Thinking:  "partial plan",
			},
			assertion: func(t *testing.T, final sigma.AssistantMessage) {
				t.Helper()
				if got, want := final.Content[0].ThinkingText, "partial plan"; got != want {
					t.Fatalf("partial thinking = %q, want %q", got, want)
				}
			},
		},
		{
			name: "tool-call delta",
			event: sigma.Event{
				Kind: sigma.EventKindToolCallDelta,
				PartialToolCall: &sigma.PartialToolCall{
					ID:             "call_partial",
					Name:           "lookup",
					ArgumentsDelta: `{"city":"Melbourne"}`,
				},
			},
			assertion: func(t *testing.T, final sigma.AssistantMessage) {
				t.Helper()
				block := final.Content[0]
				if got, want := block.ToolCallID, "call_partial"; got != want {
					t.Fatalf("tool call id = %q, want %q", got, want)
				}
				if got, want := block.ToolName, "lookup"; got != want {
					t.Fatalf("tool name = %q, want %q", got, want)
				}
				args := block.ToolArguments.(map[string]any)
				if got, want := args["city"], "Melbourne"; got != want {
					t.Fatalf("tool city = %v, want %v", got, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			stream, writer := sigma.NewStream(ctx)
			if err := writer.Emit(context.Background(), tt.event); err != nil {
				t.Fatalf("Emit returned error: %v", err)
			}
			cancel()

			final, err := sigma.Collect(context.Background(), stream)
			if err == nil {
				t.Fatal("Collect returned nil error")
			}
			if !stderrors.Is(err, sigma.ErrAborted) {
				t.Fatalf("Collect error = %v, want ErrAborted", err)
			}
			if !stderrors.Is(err, context.Canceled) {
				t.Fatalf("Collect error = %v, want context.Canceled", err)
			}
			if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
				t.Fatalf("stop reason = %q, want %q", got, want)
			}
			if len(final.Content) != 1 {
				t.Fatalf("partial content count = %d, want 1", len(final.Content))
			}
			tt.assertion(t, final)
		})
	}
}

func TestStreamCollectWithCanceledContextReturnsPartialFinalMessage(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	stream, writer := sigma.NewStream(ctx)
	if err := writer.Emit(context.Background(), sigma.Event{
		Kind:      sigma.EventKindTextDelta,
		DeltaText: "partial",
		Text:      "partial",
	}); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}
	cancel()

	final, err := sigma.Collect(ctx, stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	if !stderrors.Is(err, sigma.ErrAborted) {
		t.Fatalf("Collect error = %v, want ErrAborted", err)
	}
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("Collect error = %v, want context.Canceled", err)
	}
	var generationErr *sigma.GenerationError
	if !stderrors.As(err, &generationErr) {
		t.Fatalf("Collect error type = %T, want GenerationError", err)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Text, "partial"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
}

func TestStreamCollectorCancellationAbortsLiveStream(t *testing.T) {
	t.Parallel()

	stream, writer := sigma.NewStream(context.Background())
	if err := writer.Emit(context.Background(), sigma.Event{
		Kind:      sigma.EventKindTextDelta,
		DeltaText: "partial",
		Text:      "partial",
	}); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}
	collectorCtx, cancel := context.WithCancel(context.Background())
	cancel()

	final, err := sigma.Collect(collectorCtx, stream)
	if !stderrors.Is(err, sigma.ErrAborted) {
		t.Fatalf("Collect error = %v, want ErrAborted", err)
	}
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("Collect error = %v, want context.Canceled", err)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Text, "partial"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
}

func TestClientCompleteWithCanceledContextReturnsPartialFinalMessage(t *testing.T) {
	t.Parallel()

	provider := &cancelAfterPartialProvider{emitted: make(chan struct{})}
	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(sigmatest.ProviderID, provider); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(sigmatest.TextModel()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan completeResult, 1)
	go func() {
		final, err := client.Complete(ctx, sigmatest.TextModel(), sigma.Request{
			Messages: []sigma.Message{sigma.UserText("hi")},
		})
		result <- completeResult{final: final, err: err}
	}()

	select {
	case <-provider.emitted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for partial event")
	}
	cancel()

	got := receiveCompleteResult(t, result)
	if got.err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !stderrors.Is(got.err, sigma.ErrAborted) {
		t.Fatalf("Complete error = %v, want ErrAborted", got.err)
	}
	var generationErr *sigma.GenerationError
	if !stderrors.As(got.err, &generationErr) {
		t.Fatalf("Complete error type = %T, want GenerationError", got.err)
	}
	if got, want := got.final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := got.final.Content[0].Text, "partial"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
}

func TestStreamContextCancelRecordsStableResult(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	stream, writer := sigma.NewStream(ctx)
	if err := writer.Emit(context.Background(), sigma.Event{
		Kind:      sigma.EventKindTextDelta,
		DeltaText: "partial",
		Text:      "partial",
	}); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}

	cancel()

	for event := range stream.Events() {
		if event.Kind != sigma.EventKindTextDelta && event.Kind != sigma.EventKindError {
			t.Fatalf("event kind = %q, want text delta or best-effort error", event.Kind)
		}
	}

	if !stderrors.Is(stream.Err(), sigma.ErrAborted) {
		t.Fatalf("stream error = %v, want ErrAborted", stream.Err())
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream Final returned no final message")
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Text, "partial"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
}

func TestStreamContextCancelClosesAbandonedStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	stream, writer := sigma.NewStream(ctx)
	if err := writer.Emit(context.Background(), sigma.Event{
		Kind:      sigma.EventKindTextDelta,
		DeltaText: "queued",
	}); err != nil {
		t.Fatalf("first Emit returned error: %v", err)
	}

	writeErr := make(chan error, 1)
	go func() {
		writeErr <- writer.Emit(context.Background(), sigma.Event{
			Kind:      sigma.EventKindTextDelta,
			DeltaText: "blocked",
		})
	}()

	select {
	case err := <-writeErr:
		t.Fatalf("second Emit returned before cancel: %v", err)
	case <-time.After(10 * time.Millisecond):
	}

	cancel()

	select {
	case <-stream.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stream Done")
	}
	err := receiveErr(t, writeErr)
	if err == nil {
		t.Fatal("blocked writer returned nil error")
	}
	if !stderrors.Is(stream.Err(), sigma.ErrAborted) {
		t.Fatalf("stream error = %v, want ErrAborted", stream.Err())
	}
}

func TestStreamCollectsFauxProviderPartialAfterCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Events: []sigma.Event{{
			Kind:      sigma.EventKindTextDelta,
			DeltaText: "partial",
			Text:      "partial",
		}},
		Delay: 10 * time.Millisecond,
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	stream := client.Stream(ctx, sigmatest.TextModel(), sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	event := receiveEvent(t, stream)
	if got, want := event.Kind, sigma.EventKindTextDelta; got != want {
		t.Fatalf("first event = %q, want %q", got, want)
	}
	cancel()

	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	if !stderrors.Is(err, sigma.ErrAborted) {
		t.Fatalf("Collect error = %v, want ErrAborted", err)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Text, "partial"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
}

func TestStreamRejectsDoubleTerminalEvent(t *testing.T) {
	t.Parallel()

	stream, writer := sigma.NewStream(context.Background())
	if err := writer.Done(context.Background(), sigma.AssistantMessage{
		StopReason: sigma.StopReasonEndTurn,
	}); err != nil {
		t.Fatalf("first terminal returned error: %v", err)
	}

	err := writer.Error(context.Background(), stderrors.New("too late"), sigma.AssistantMessage{})
	if err == nil {
		t.Fatal("second terminal returned nil error")
	}
	assertSigmaErrorCode(t, err, sigma.ErrorStreamClosed)

	event, ok := <-stream.Events()
	if !ok {
		t.Fatal("stream closed before terminal event")
	}
	if got, want := event.Kind, sigma.EventKindDone; got != want {
		t.Fatalf("terminal kind = %q, want %q", got, want)
	}
	if event.FinalMessage == nil {
		t.Fatal("terminal event missing final message")
	}
	if _, ok := <-stream.Events(); ok {
		t.Fatal("stream emitted more than one terminal event")
	}
}

func TestStreamRejectsSendAfterTerminalClosure(t *testing.T) {
	t.Parallel()

	stream, writer := sigma.NewStream(context.Background())
	if err := writer.Done(context.Background(), sigma.AssistantMessage{
		StopReason: sigma.StopReasonEndTurn,
	}); err != nil {
		t.Fatalf("terminal returned error: %v", err)
	}
	<-stream.Events()
	<-stream.Done()

	err := writer.Emit(context.Background(), sigma.Event{Kind: sigma.EventKindTextDelta})
	if err == nil {
		t.Fatal("send after terminal returned nil error")
	}
	assertSigmaErrorCode(t, err, sigma.ErrorStreamClosed)
}

func TestStreamConsumerCloseUnblocksWriter(t *testing.T) {
	t.Parallel()

	stream, writer := sigma.NewStream(context.Background())
	if err := writer.Emit(context.Background(), sigma.Event{
		Kind:      sigma.EventKindTextDelta,
		DeltaText: "queued",
	}); err != nil {
		t.Fatalf("emit returned error: %v", err)
	}

	writeErr := make(chan error, 1)
	go func() {
		writeErr <- writer.Done(context.Background(), sigma.AssistantMessage{
			StopReason: sigma.StopReasonEndTurn,
		})
	}()

	stream.Close()

	err := receiveErr(t, writeErr)
	if err == nil {
		t.Fatal("blocked terminal write returned nil error")
	}
	assertSigmaErrorCode(t, err, sigma.ErrorStreamClosed)

	err = writer.Emit(context.Background(), sigma.Event{Kind: sigma.EventKindTextDelta})
	if err == nil {
		t.Fatal("send after consumer close returned nil error")
	}
	assertSigmaErrorCode(t, err, sigma.ErrorStreamClosed)
}

type completeResult struct {
	final sigma.AssistantMessage
	err   error
}

type cancelAfterPartialProvider struct {
	emitted chan struct{}
}

func (p *cancelAfterPartialProvider) API() sigma.API {
	return sigmatest.TextAPI
}

func (p *cancelAfterPartialProvider) Stream(ctx context.Context, _ sigma.Model, _ sigma.Request, _ sigma.Options) *sigma.Stream {
	stream, writer := sigma.NewStream(ctx)
	go func() {
		_ = writer.Emit(context.Background(), sigma.Event{
			Kind:      sigma.EventKindTextDelta,
			DeltaText: "partial",
			Text:      "partial",
		})
		close(p.emitted)
		<-ctx.Done()
	}()
	return stream
}

func assertSigmaErrorCode(t *testing.T, err error, code sigma.ErrorCode) {
	t.Helper()

	var streamErr *sigma.Error
	if !stderrors.As(err, &streamErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if streamErr.Code != code {
		t.Fatalf("error code = %q, want %q", streamErr.Code, code)
	}
}

func receiveErr(t *testing.T, ch <-chan error) error {
	t.Helper()

	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for writer")
		return nil
	}
}

func receiveCompleteResult(t *testing.T, ch <-chan completeResult) completeResult {
	t.Helper()

	select {
	case result := <-ch:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Complete")
		return completeResult{}
	}
}

func receiveEvent(t *testing.T, stream *sigma.Stream) sigma.Event {
	t.Helper()

	select {
	case event, ok := <-stream.Events():
		if !ok {
			t.Fatal("stream closed before event")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return sigma.Event{}
	}
}

func TestStreamEmitPreservesProviderPartialMessage(t *testing.T) {
	t.Parallel()

	stream, writer := sigma.NewStream(context.Background())
	custom := &sigma.AssistantMessage{
		Content: []sigma.ContentBlock{sigma.Text("provider partial")},
	}
	if err := writer.Emit(context.Background(), sigma.Event{
		Kind:           sigma.EventKindTextDelta,
		DeltaText:      "hi",
		PartialMessage: custom,
	}); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}

	event, ok := <-stream.Events()
	if !ok {
		t.Fatal("stream closed before event")
	}
	if event.PartialMessage == nil {
		t.Fatal("partial message = nil, want provider-set partial")
	}
	if got, want := event.PartialMessage.Content[0].Text, "provider partial"; got != want {
		t.Fatalf("partial text = %q, want %q (Emit must not overwrite a provider-set partial)", got, want)
	}
	stream.Close()
}

func TestStreamPartialSnapshotDoesNotAliasAccumulatorState(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	stream, writer := sigma.NewStream(ctx)
	writeErr := make(chan error, 1)
	go func() {
		if err := writer.Emit(context.Background(), sigma.Event{
			Kind: sigma.EventKindToolCallEnd,
			ToolCall: &sigma.ToolCall{
				ID:        "call_1",
				Name:      "lookup",
				Arguments: map[string]any{"city": "Melbourne"},
			},
		}); err != nil {
			writeErr <- err
			return
		}
		if err := writer.Emit(context.Background(), sigma.Event{
			Kind:      sigma.EventKindTextDelta,
			DeltaText: "done",
		}); err != nil {
			writeErr <- err
			return
		}
		writeErr <- nil
		cancel()
	}()

	// Mutate the snapshot a consumer received; the aborted final built from
	// the accumulator must not observe the mutation.
	for event := range stream.Events() {
		if event.PartialMessage == nil {
			continue
		}
		for _, block := range event.PartialMessage.Content {
			if args, ok := block.ToolArguments.(map[string]any); ok {
				args["city"] = "corrupted"
			}
		}
	}
	if err := receiveErr(t, writeErr); err != nil {
		t.Fatalf("writer returned error: %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("Final returned no message")
	}
	for _, block := range final.Content {
		if block.Type != sigma.ContentBlockToolCall {
			continue
		}
		args := block.ToolArguments.(map[string]any)
		if got, want := args["city"], "Melbourne"; got != want {
			t.Fatalf("final tool city = %v, want %v (snapshot aliased accumulator state)", got, want)
		}
	}
}
