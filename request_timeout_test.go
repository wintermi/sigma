// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/streamlifecycle"
)

// timeoutTestProvider waits for its request context to end and then either
// reports the context error, as adapters do, or writes nothing.
type timeoutTestProvider struct{ silent bool }

func (timeoutTestProvider) API() sigma.API { return sigma.APIOpenAICompletions }

func (p timeoutTestProvider) Stream(ctx context.Context, model sigma.Model, _ sigma.Request, opts sigma.Options) *sigma.Stream {
	ctx, stream, writer, cleanup := streamlifecycle.NewTextStream(ctx, model, opts)
	go func() {
		defer cleanup()
		<-ctx.Done()
		if !p.silent {
			_ = writer.Error(ctx, ctx.Err(), sigma.AssistantMessage{StopReason: sigma.StopReasonAborted})
		}
	}()
	return stream
}

// Sigma's own request timeout is a transient failure worth retrying, unlike a
// caller cancellation, which remains an abort.
func TestRequestTimeoutIsTransientNotAborted(t *testing.T) {
	t.Parallel()
	for _, silent := range []bool{false, true} {
		model := sigma.Model{ID: "timeout-test", Provider: "timeout-test", API: sigma.APIOpenAICompletions}
		registry := sigma.NewRegistry()
		if err := registry.RegisterTextProvider(model.Provider, timeoutTestProvider{silent: silent}); err != nil {
			t.Fatal(err)
		}
		if err := registry.RegisterModel(model); err != nil {
			t.Fatal(err)
		}
		client := sigma.NewClient(sigma.WithRegistry(registry))

		final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, sigma.WithTimeout(10*time.Millisecond))
		if final.StopReason != sigma.StopReasonError {
			t.Fatalf("silent=%v timeout stop reason = %q, want error", silent, final.StopReason)
		}
		if got := sigma.ClassifyError(err); got.Class != sigma.ErrorClassTransient || !got.RetryHint.Retryable {
			t.Fatalf("silent=%v timeout classification = %+v, want retryable transient", silent, got)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("silent=%v timeout error %v does not match context.DeadlineExceeded", silent, err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(10*time.Millisecond, cancel)
		final, _ = client.Complete(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
		if final.StopReason != sigma.StopReasonAborted {
			t.Fatalf("silent=%v caller cancellation stop reason = %q, want aborted", silent, final.StopReason)
		}
	}
}
