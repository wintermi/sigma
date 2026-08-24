// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

const deferredTestAPI sigma.API = "deferred-test"

type deferredTestProvider struct {
	mu       sync.Mutex
	submit   sigma.DeferredResponse
	fetch    sigma.DeferredResponse
	cancel   sigma.DeferredResponse
	requests []deferredTestCapture
}

type deferredTestCapture struct {
	operation string
	model     sigma.Model
	request   sigma.Request
	handle    sigma.DeferredResponseHandle
	options   sigma.Options
}

func (p *deferredTestProvider) API() sigma.API {
	return deferredTestAPI
}

func (p *deferredTestProvider) Stream(ctx context.Context, model sigma.Model, _ sigma.Request, _ sigma.Options) *sigma.Stream {
	stream, writer := sigma.NewStream(ctx)
	_ = writer.Done(ctx, sigma.AssistantMessage{Model: model.ID, Provider: model.Provider})
	return stream
}

func (p *deferredTestProvider) SubmitDeferred(_ context.Context, model sigma.Model, req sigma.Request, options sigma.Options) (sigma.DeferredResponse, error) {
	p.record(deferredTestCapture{operation: "submit", model: model, request: req, options: options})
	return p.submit, nil
}

func (p *deferredTestProvider) FetchDeferred(_ context.Context, model sigma.Model, handle sigma.DeferredResponseHandle, options sigma.Options) (sigma.DeferredResponse, error) {
	p.record(deferredTestCapture{operation: "fetch", model: model, handle: handle, options: options})
	return p.fetch, nil
}

func (p *deferredTestProvider) CancelDeferred(_ context.Context, model sigma.Model, handle sigma.DeferredResponseHandle, options sigma.Options) (sigma.DeferredResponse, error) {
	p.record(deferredTestCapture{operation: "cancel", model: model, handle: handle, options: options})
	return p.cancel, nil
}

func (p *deferredTestProvider) record(capture deferredTestCapture) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, capture)
}

func (p *deferredTestProvider) captures() []deferredTestCapture {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]deferredTestCapture(nil), p.requests...)
}

func TestClientDeferredLifecycleDispatchesWithResolvedModelAndOptions(t *testing.T) {
	t.Parallel()

	providerID := sigma.ProviderID("deferred-dispatch")
	model := sigma.Model{Provider: providerID, ID: "deferred-model", API: deferredTestAPI}
	handle := sigma.DeferredResponseHandle{
		Provider: providerID,
		Model:    model.ID,
		API:      model.API,
		ID:       "job-1",
	}
	provider := &deferredTestProvider{
		submit: sigma.DeferredResponse{Handle: handle, Status: sigma.DeferredResponseQueued},
		fetch: sigma.DeferredResponse{
			Handle: handle,
			Status: sigma.DeferredResponseCompleted,
			Message: &sigma.AssistantMessage{
				Content:    []sigma.ContentBlock{sigma.Text("done")},
				StopReason: sigma.StopReasonEndTurn,
			},
		},
		cancel: sigma.DeferredResponse{Handle: handle, Status: sigma.DeferredResponseCancelled},
	}
	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(providerID, provider); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithDefaultHeader("X-Default", "default"),
	)
	request := sigma.Request{Messages: []sigma.Message{sigma.UserText("work")}}

	submitted, err := client.SubmitDeferred(
		context.Background(),
		sigma.Model{Provider: providerID, ID: model.ID},
		request,
		sigma.WithHeader("X-Request", "submit"),
	)
	if err != nil {
		t.Fatalf("SubmitDeferred returned error: %v", err)
	}
	if submitted.Status != sigma.DeferredResponseQueued {
		t.Fatalf("submitted status = %q, want %q", submitted.Status, sigma.DeferredResponseQueued)
	}

	fetched, err := client.FetchDeferred(context.Background(), handle, sigma.WithHeader("X-Request", "fetch"))
	if err != nil {
		t.Fatalf("FetchDeferred returned error: %v", err)
	}
	if fetched.Message == nil || fetched.Message.Content[0].Text != "done" {
		t.Fatalf("fetched message = %#v, want done", fetched.Message)
	}
	cancelled, err := client.CancelDeferred(context.Background(), handle, sigma.WithHeader("X-Request", "cancel"))
	if err != nil {
		t.Fatalf("CancelDeferred returned error: %v", err)
	}
	if cancelled.Status != sigma.DeferredResponseCancelled {
		t.Fatalf("cancelled status = %q, want %q", cancelled.Status, sigma.DeferredResponseCancelled)
	}

	captures := provider.captures()
	if len(captures) != 3 {
		t.Fatalf("capture count = %d, want 3", len(captures))
	}
	if captures[0].model.API != deferredTestAPI || captures[0].request.Messages[0].Content[0].Text != "work" {
		t.Fatalf("submit capture = %#v", captures[0])
	}
	for index, operation := range []string{"submit", "fetch", "cancel"} {
		if captures[index].operation != operation {
			t.Fatalf("capture %d operation = %q, want %q", index, captures[index].operation, operation)
		}
		if captures[index].options.Headers["X-Default"] != "default" {
			t.Fatalf("capture %d default header = %#v", index, captures[index].options.Headers)
		}
	}
	if !reflect.DeepEqual(captures[1].handle, handle) || !reflect.DeepEqual(captures[2].handle, handle) {
		t.Fatalf("dispatched handles = %#v, %#v, want %#v", captures[1].handle, captures[2].handle, handle)
	}
}

func TestDeferredResponseHandleJSONRoundTrip(t *testing.T) {
	t.Parallel()

	handle := sigma.DeferredResponseHandle{
		Provider: sigma.ProviderOpenAI,
		Model:    "gpt-test",
		API:      sigma.APIOpenAIResponses,
		ID:       "resp_123",
		ProviderMetadata: map[string]any{
			"nested": map[string]any{"property": "command"},
		},
	}
	data, err := json.Marshal(handle)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var decoded sigma.DeferredResponseHandle
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if decoded.Provider != handle.Provider || decoded.Model != handle.Model || decoded.API != handle.API || decoded.ID != handle.ID {
		t.Fatalf("decoded handle = %#v, want %#v", decoded, handle)
	}
	nested, ok := decoded.ProviderMetadata["nested"].(map[string]any)
	if !ok || nested["property"] != "command" {
		t.Fatalf("decoded provider metadata = %#v", decoded.ProviderMetadata)
	}
}

func TestClientDeferredLifecycleRejectsInvalidOrUnsupportedHandles(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxProvider()
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	_, err = client.FetchDeferred(context.Background(), sigma.DeferredResponseHandle{})
	if !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("empty handle error = %v, want invalid options", err)
	}
	_, err = client.FetchDeferred(context.Background(), sigma.DeferredResponseHandle{
		Provider: sigmatest.ProviderID,
		Model:    sigmatest.TextModelID,
		API:      "wrong-api",
		ID:       "job-1",
	})
	if !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("mismatched API error = %v, want invalid options", err)
	}
	_, err = client.FetchDeferred(context.Background(), sigma.DeferredResponseHandle{
		Provider: sigmatest.ProviderID,
		Model:    sigmatest.TextModelID,
		API:      sigmatest.TextAPI,
		ID:       "job-1",
	})
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorUnsupported {
		t.Fatalf("unsupported provider error = %T %[1]v, want unsupported", err)
	}
}

func TestPackageDeferredLifecycleHelpersUseDefaultClient(t *testing.T) {
	providerID := sigma.ProviderID("package-deferred-provider")
	model := sigma.Model{Provider: providerID, ID: "package-deferred-model", API: deferredTestAPI}
	handle := sigma.DeferredResponseHandle{Provider: providerID, Model: model.ID, API: model.API, ID: "package-job"}
	provider := &deferredTestProvider{
		submit: sigma.DeferredResponse{Handle: handle, Status: sigma.DeferredResponseQueued},
		fetch:  sigma.DeferredResponse{Handle: handle, Status: sigma.DeferredResponseInProgress},
		cancel: sigma.DeferredResponse{Handle: handle, Status: sigma.DeferredResponseCancelled},
	}
	if err := sigma.RegisterDefaultTextProvider(providerID, provider, sigma.WithOverride()); err != nil {
		t.Fatalf("RegisterDefaultTextProvider returned error: %v", err)
	}
	if err := sigma.RegisterDefaultModel(model, sigma.WithOverride()); err != nil {
		t.Fatalf("RegisterDefaultModel returned error: %v", err)
	}

	submitted, err := sigma.SubmitDeferred(context.Background(), model, sigma.Request{})
	if err != nil || submitted.Status != sigma.DeferredResponseQueued {
		t.Fatalf("SubmitDeferred = %#v, %v", submitted, err)
	}
	fetched, err := sigma.FetchDeferred(context.Background(), handle)
	if err != nil || fetched.Status != sigma.DeferredResponseInProgress {
		t.Fatalf("FetchDeferred = %#v, %v", fetched, err)
	}
	cancelled, err := sigma.CancelDeferred(context.Background(), handle)
	if err != nil || cancelled.Status != sigma.DeferredResponseCancelled {
		t.Fatalf("CancelDeferred = %#v, %v", cancelled, err)
	}
}
