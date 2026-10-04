// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"syscall"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

func TestAuditStreamErrorClassification(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{syscall.ECONNRESET, io.ErrUnexpectedEOF, sigma.ErrCredentialUnavailable} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			registry := sigma.NewRegistry()
			model := sigma.Model{ID: "test", Provider: "audit-errors", API: sigma.APIOpenAICompletions}
			if err := openai.Register(registry, model.Provider); err != nil {
				t.Fatal(err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatal(err)
			}
			calls := 0
			client := sigma.NewClient(sigma.WithRegistry(registry), sigma.WithHTTPClient(&http.Client{Transport: ownershipTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, cause })}))
			opts := []sigma.Option{sigma.WithAPIKey("synthetic"), sigma.WithMaxRetries(0)}
			if errors.Is(cause, sigma.ErrCredentialUnavailable) {
				opts = []sigma.Option{sigma.WithProviderAuthResolver(model.Provider, sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
					return sigma.Credential{}, cause
				}))}
			}
			_, err := client.Complete(context.Background(), model, sigma.Request{}, opts...)
			if !errors.Is(err, cause) {
				t.Fatalf("lost cause: %v", err)
			}
			want := sigma.ErrorClassTransient
			action := sigma.RouteActionRetry
			if errors.Is(cause, sigma.ErrCredentialUnavailable) {
				want = sigma.ErrorClassAuth
				action = sigma.RouteActionAbort
			}
			got := sigma.ClassifyError(err)
			advice := (sigma.RoutePolicy{}).Fallback(sigma.RouteDecision{Model: sigma.ModelRef{Provider: model.Provider, ID: model.ID}}, nil, err)
			if got.Class != want || got.RetryHint.Retryable != (want == sigma.ErrorClassTransient) || advice.Action != action {
				t.Fatalf("classification=%+v advice=%+v", got, advice)
			}
			if calls > 1 {
				t.Fatalf("unexpected automatic replay: %d", calls)
			}
		})
	}
}

type auditIdentityProvider struct{ writer sigma.StreamWriter }

func (*auditIdentityProvider) API() sigma.API { return sigma.APIOpenAICompletions }
func (p *auditIdentityProvider) Stream(ctx context.Context, _ sigma.Model, _ sigma.Request, _ sigma.Options) *sigma.Stream {
	stream, writer := sigma.NewStream(ctx)
	p.writer = writer
	return stream
}

func TestAuditCancellationIdentityAndPartialContent(t *testing.T) {
	t.Parallel()
	for _, collector := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			ctx, cancel := context.WithCancel(context.Background())
			provider := &auditIdentityProvider{}
			registry := sigma.NewRegistry()
			model := sigma.Model{ID: "identity", Provider: "custom", API: provider.API()}
			if err := registry.RegisterTextProvider(model.Provider, provider); err != nil {
				t.Fatal(err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatal(err)
			}
			stream := sigma.NewClient(sigma.WithRegistry(registry)).Stream(ctx, model, sigma.Request{})
			if partial {
				events := []sigma.Event{{Kind: sigma.EventKindStart}, {Kind: sigma.EventKindTextDelta, DeltaText: "partial", PartialMessage: &sigma.AssistantMessage{ProviderThinkingLevel: "high"}}, {Kind: sigma.EventKindToolCallDelta, ContentIndex: new(1), PartialToolCall: &sigma.PartialToolCall{ID: "call", Name: "lookup", ArgumentsDelta: `{"id":1}`}}}
				for _, event := range events {
					if err := provider.writer.Emit(ctx, event); err != nil {
						t.Fatal(err)
					}
					got := <-stream.Events()
					if got.Kind == sigma.EventKindStart && (got.PartialMessage == nil || got.PartialMessage.Provider != "" || got.PartialMessage.Model != "") {
						t.Fatalf("pending snapshot changed: %+v", got)
					}
				}
			}
			var final sigma.AssistantMessage
			var err error
			if collector {
				collectCtx, stop := context.WithCancel(context.Background())
				stop()
				final, err = sigma.Collect(collectCtx, stream)
				if ctx.Err() != nil {
					t.Fatal("collector canceled caller context")
				}
			} else {
				cancel()
			}
			for event := range stream.Events() {
				if event.FinalMessage != nil && (event.FinalMessage.Model != model.ID || event.FinalMessage.Provider != model.Provider) {
					t.Fatalf("terminal identity: %+v", event.FinalMessage)
				}
			}
			if !collector {
				final, err = sigma.Collect(context.Background(), stream)
			}
			cancel()
			stored, ok := stream.Final()
			var generation *sigma.GenerationError
			if !errors.As(err, &generation) || !errors.Is(err, context.Canceled) || !ok || !reflect.DeepEqual(final, stored) || !reflect.DeepEqual(final, generation.Final) {
				t.Fatalf("inconsistent finals: %+v %+v %v", final, stored, err)
			}
			if final.Provider != model.Provider || final.Model != model.ID || final.StopReason != sigma.StopReasonAborted {
				t.Fatalf("identity: %+v", final)
			}
			if partial && (len(final.Content) != 2 || final.Content[0].Text != "partial" || final.Content[1].ToolName != "lookup" || final.Content[1].ToolArguments == nil || final.ProviderThinkingLevel != "high") {
				t.Fatalf("lost partial: %+v", final)
			}
		}
	}
}

func TestAuditClassificationPrecedence(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		err  error
		want sigma.ErrorClass
	}{
		{&sigma.Error{Code: sigma.ErrorStream, Err: sigma.NewProviderError("test", sigma.APIOpenAICompletions, "test", 401, "request", 0, nil, io.ErrUnexpectedEOF)}, sigma.ErrorClassAuth},
		{&sigma.Error{Code: sigma.ErrorAborted, Err: context.Canceled}, sigma.ErrorClassUnknown},
		{&sigma.Error{Code: sigma.ErrorInvalidOptions, Err: io.ErrUnexpectedEOF}, sigma.ErrorClassInvalidRequest},
		{&sigma.Error{Code: sigma.ErrorDebugHook, Err: io.ErrUnexpectedEOF}, sigma.ErrorClassUnknown},
		{&sigma.Error{Code: sigma.ErrorStream, Err: sigma.ErrContextOverflow}, sigma.ErrorClassContextOverflow},
	} {
		if got := sigma.ClassifyError(tt.err); got.Class != tt.want {
			t.Fatalf("%v: %+v", tt.err, got)
		}
	}
}

func TestAuditAlreadyCanceledIdentity(t *testing.T) {
	t.Parallel()
	model := sigma.Model{ID: "test", Provider: "audit-cancel", API: sigma.APIOpenAICompletions}
	registry := sigma.NewRegistry()
	if err := openai.Register(registry, model.Provider); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatal(err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	for _, direct := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var stream *sigma.Stream
		if direct {
			stream = openai.NewProvider().Stream(ctx, model, sigma.Request{}, sigma.Options{})
		} else {
			stream = client.Stream(ctx, model, sigma.Request{})
		}
		for event := range stream.Events() {
			if event.FinalMessage != nil && (event.FinalMessage.Provider != model.Provider || event.FinalMessage.Model != model.ID) {
				t.Fatalf("terminal identity: %+v", event.FinalMessage)
			}
		}
		final, err := sigma.Collect(context.Background(), stream)
		var generation *sigma.GenerationError
		if !errors.As(err, &generation) || !errors.Is(err, context.Canceled) {
			t.Fatalf("error: %v", err)
		}
		stored, ok := stream.Final()
		for _, got := range []sigma.AssistantMessage{final, stored, generation.Final} {
			if !ok || got.Provider != model.Provider || got.Model != model.ID || got.StopReason != sigma.StopReasonAborted {
				t.Fatalf("direct=%v final=%+v", direct, got)
			}
		}
	}
}
