// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestClientCompleteDispatchesProviderWithMergedOptions(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("hello")},
		},
	})
	providerID := sigma.ProviderID("dispatch-provider")
	model := sigma.Model{ID: "dispatch-model", Provider: providerID, API: sigmatest.TextAPI}
	if err := registry.RegisterTextProvider(providerID, provider); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	clientHTTPClient := &http.Client{}
	requestHTTPClient := &http.Client{}
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Value: "token"}, nil
	})
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithHTTPClient(clientHTTPClient),
		sigma.WithAuthResolver(resolver),
		sigma.WithDefaultHeaders(map[string]string{
			"x-default":  "client",
			"x-override": "client",
		}),
		sigma.WithDefaultOptions(
			sigma.WithHeader("x-option", "default-option"),
			sigma.WithHeader("x-override", "default-option"),
		),
	)

	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}
	final, err := client.Complete(
		context.Background(),
		model,
		req,
		sigma.WithHeader("x-request", "request"),
		sigma.WithHeader("x-override", "request"),
		sigma.WithRequestHTTPClient(requestHTTPClient),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "hello"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
	capture, ok := provider.LastRequest()
	if !ok {
		t.Fatal("provider did not capture request")
	}
	if capture.Model.ID != model.ID {
		t.Fatalf("provider model id = %q, want %q", capture.Model.ID, model.ID)
	}
	if got, want := capture.Request.Messages[0].Content[0].Text, "hi"; got != want {
		t.Fatalf("provider request text = %q, want %q", got, want)
	}
	if capture.Options.HTTPClient != requestHTTPClient {
		t.Fatal("provider options did not include request HTTP client")
	}
	credential, err := capture.Options.AuthResolver.Resolve(context.Background(), model, capture.Options)
	if err != nil {
		t.Fatalf("AuthResolver returned error: %v", err)
	}
	if credential.Value != "token" {
		t.Fatalf("credential = %q, want token", credential.Value)
	}
	assertHeader(t, capture.Options.Headers, "x-default", "client")
	assertHeader(t, capture.Options.Headers, "x-option", "default-option")
	assertHeader(t, capture.Options.Headers, "x-request", "request")
	assertHeader(t, capture.Options.Headers, "x-override", "request")
}

func TestClientModelLookupAndFilters(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("filter-provider")
	if err := registry.RegisterTextProvider(providerID, sigmatest.NewFauxProvider()); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(sigma.Model{
		ID:            "tool-model",
		Provider:      providerID,
		API:           sigmatest.TextAPI,
		SupportsTools: true,
	}); err != nil {
		t.Fatalf("RegisterModel(tool) returned error: %v", err)
	}
	if err := registry.RegisterModel(sigma.Model{
		ID:       "plain-model",
		Provider: providerID,
		API:      sigmatest.TextAPI,
	}); err != nil {
		t.Fatalf("RegisterModel(plain) returned error: %v", err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, ok := client.GetModel(providerID, "tool-model"); !ok {
		t.Fatal("GetModel did not find registered model")
	}

	models := client.Models(func(model sigma.Model) bool {
		return model.SupportsTools
	})
	if got, want := len(models), 1; got != want {
		t.Fatalf("filtered model count = %d, want %d", got, want)
	}
	if got, want := models[0].ID, sigma.ModelID("tool-model"); got != want {
		t.Fatalf("filtered model = %q, want %q", got, want)
	}
}

func TestClientCompleteReturnsTypedLookupErrors(t *testing.T) {
	t.Parallel()

	providerID := sigma.ProviderID("missing-provider")
	model := sigma.Model{ID: "known-model", Provider: providerID, API: sigma.APIOpenAIResponses}
	registry := sigma.NewRegistry()
	if err := registry.RegisterModel(model, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	_, err := client.Complete(context.Background(), model, sigma.Request{})
	assertSigmaLookupError(t, err, sigma.ErrorProviderNotFound, providerID, model.ID)

	registry = sigma.NewRegistry()
	providerID = "missing-model-provider"
	if err := registry.RegisterTextProvider(providerID, sigmatest.NewFauxProvider()); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	client = sigma.NewClient(sigma.WithRegistry(registry))

	_, err = client.Complete(context.Background(), sigma.Model{
		ID:       "unknown-model",
		Provider: providerID,
		API:      sigmatest.TextAPI,
	}, sigma.Request{})
	assertSigmaLookupError(t, err, sigma.ErrorModelNotFound, providerID, "unknown-model")
}

func TestClientCompleteTextIsTextOnly(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("text-only-provider")
	model := sigma.Model{ID: "text-only-model", Provider: providerID, API: sigmatest.TextAPI}
	provider := sigmatest.NewFauxProvider(
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{
				sigma.Text("hello "),
				sigma.Text("world"),
			},
		}},
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{
				sigma.ToolCallBlock("call_1", "lookup", map[string]any{"q": "hi"}),
			},
		}},
	)
	if err := registry.RegisterTextProvider(providerID, provider); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	text, err := client.CompleteText(context.Background(), model, "hi")
	if err != nil {
		t.Fatalf("CompleteText returned error: %v", err)
	}
	if got, want := text, "hello world"; got != want {
		t.Fatalf("CompleteText = %q, want %q", got, want)
	}

	_, err = client.CompleteText(context.Background(), model, "hi")
	assertSigmaLookupError(t, err, sigma.ErrorUnsupported, model.Provider, model.ID)
}

func TestClientContinuesAfterSerializedAbortedAssistantMessage(t *testing.T) {
	t.Parallel()

	providerID := sigma.ProviderID("continuation-provider")
	model := sigma.Model{ID: "continuation-model", Provider: providerID, API: sigmatest.TextAPI}
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Final: sigma.AssistantMessage{
			Content:    []sigma.ContentBlock{sigma.Text("continued")},
			StopReason: sigma.StopReasonEndTurn,
		},
	})
	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(providerID, provider); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	req := sigma.Request{Messages: []sigma.Message{
		sigma.UserText("start"),
		{
			Role:       sigma.RoleAssistant,
			Provider:   sigma.ProviderOpenAI,
			API:        sigma.APIOpenAICompletions,
			Model:      "gpt-test",
			StopReason: sigma.StopReasonAborted,
			Content: []sigma.ContentBlock{
				sigma.Thinking("partial plan", ""),
				sigma.Text("partial answer"),
			},
		},
		sigma.UserText("continue"),
	}}
	data, err := sigma.MarshalRequest(req)
	if err != nil {
		t.Fatalf("MarshalRequest returned error: %v", err)
	}
	decoded, err := sigma.UnmarshalRequest(data)
	if err != nil {
		t.Fatalf("UnmarshalRequest returned error: %v", err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	final, err := client.Complete(context.Background(), model, decoded)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "continued"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
	capture, ok := provider.LastRequest()
	if !ok {
		t.Fatal("provider did not capture continuation request")
	}
	aborted := capture.Request.Messages[1]
	if got, want := aborted.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("captured stop reason = %q, want %q", got, want)
	}
	if got, want := aborted.Content[0].ThinkingText, "partial plan"; got != want {
		t.Fatalf("captured thinking = %q, want %q", got, want)
	}
	if got, want := aborted.Content[1].Text, "partial answer"; got != want {
		t.Fatalf("captured text = %q, want %q", got, want)
	}
}

func TestPackageLevelHelpersUseDefaultClient(t *testing.T) {
	providerID := sigma.ProviderID("package-helper-provider")
	model := sigma.Model{ID: "package-helper-model", Provider: providerID, API: sigmatest.TextAPI}
	provider := sigmatest.NewFauxProvider(
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("package helper")},
		}},
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("package helper")},
		}},
	)
	if err := sigma.RegisterDefaultTextProvider(providerID, provider, sigma.WithOverride()); err != nil {
		t.Fatalf("RegisterDefaultTextProvider returned error: %v", err)
	}
	if err := sigma.RegisterDefaultModel(model, sigma.WithOverride()); err != nil {
		t.Fatalf("RegisterDefaultModel returned error: %v", err)
	}

	gotModel, ok := sigma.GetModel(providerID, model.ID)
	if !ok {
		t.Fatal("package GetModel did not find registered default model")
	}
	final, err := sigma.Complete(context.Background(), gotModel, sigma.Request{})
	if err != nil {
		t.Fatalf("package Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "package helper"; got != want {
		t.Fatalf("package Complete text = %q, want %q", got, want)
	}

	stream := sigma.StreamModel(context.Background(), gotModel, sigma.Request{})
	streamFinal, err := sigma.Collect(context.Background(), stream)
	if err != nil {
		t.Fatalf("package StreamModel returned stream error: %v", err)
	}
	if got, want := streamFinal.Content[0].Text, "package helper"; got != want {
		t.Fatalf("package StreamModel text = %q, want %q", got, want)
	}

	models := sigma.Models(func(model sigma.Model) bool {
		return model.Provider == providerID
	})
	if got, want := len(models), 1; got != want {
		t.Fatalf("package Models count = %d, want %d", got, want)
	}
}

func assertHeader(t *testing.T, headers map[string]string, key, value string) {
	t.Helper()

	if got := headers[key]; got != value {
		t.Fatalf("header %q = %q, want %q", key, got, value)
	}
}

func assertSigmaLookupError(t *testing.T, err error, code sigma.ErrorCode, provider sigma.ProviderID, model sigma.ModelID) {
	t.Helper()

	if err == nil {
		t.Fatal("error = nil")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if sigmaErr.Code != code {
		t.Fatalf("error code = %q, want %q", sigmaErr.Code, code)
	}
	if sigmaErr.Provider != provider {
		t.Fatalf("error provider = %q, want %q", sigmaErr.Provider, provider)
	}
	if sigmaErr.Model != model {
		t.Fatalf("error model = %q, want %q", sigmaErr.Model, model)
	}
}
