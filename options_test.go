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
)

type optionsRecordingProvider struct {
	opts   sigma.Options
	final  sigma.AssistantMessage
	called bool
}

func (p *optionsRecordingProvider) API() sigma.API {
	return sigma.APIOpenAIResponses
}

func (p *optionsRecordingProvider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	p.called = true
	p.opts = opts

	stream, writer := sigma.NewStream(ctx)
	go func() {
		final := p.final
		if final.Model == "" {
			final.Model = model.ID
		}
		if final.Provider == "" {
			final.Provider = model.Provider
		}
		if final.StopReason == "" {
			final.StopReason = sigma.StopReasonEndTurn
		}
		_ = writer.Done(ctx, final)
	}()
	return stream
}

func TestOptionsMergePrecedence(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(
			sigma.WithTemperature(0.1),
			sigma.WithMaxTokens(100),
			sigma.WithTransport(sigma.TransportHTTP),
			sigma.WithReasoningLevel(sigma.ThinkingLevelLow),
			sigma.WithThinkingBudgetTokens(64),
			sigma.WithProviderOption(sigma.ProviderOpenAI, "effort", "client"),
		),
	)
	model.DefaultTransport = sigma.TransportSSE

	_, err := client.Complete(context.Background(), model, sigma.Request{},
		sigma.WithTemperature(0.7),
		sigma.WithTransport(sigma.TransportHTTP),
		sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
		sigma.WithProviderOption(sigma.ProviderOpenAI, "effort", "call"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if got, want := valueOf(provider.opts.Temperature), 0.7; got != want {
		t.Fatalf("temperature = %v, want %v", got, want)
	}
	if got, want := valueOf(provider.opts.MaxTokens), 100; got != want {
		t.Fatalf("max tokens = %d, want %d", got, want)
	}
	if got, want := provider.opts.Transport, sigma.TransportHTTP; got != want {
		t.Fatalf("transport = %q, want %q", got, want)
	}
	if got, want := provider.opts.ReasoningLevel, sigma.ThinkingLevelHigh; got != want {
		t.Fatalf("reasoning level = %q, want %q", got, want)
	}
	if got, want := valueOf(provider.opts.ThinkingBudgetTokens), 64; got != want {
		t.Fatalf("thinking budget tokens = %d, want %d", got, want)
	}
	if got, want := provider.opts.ProviderOptions[sigma.ProviderOpenAI]["effort"], "call"; got != want {
		t.Fatalf("provider option effort = %v, want %v", got, want)
	}
}

func TestModelDefaultTransportOverridesClientDefault(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithTransport(sigma.TransportHTTP)),
	)
	model.DefaultTransport = sigma.TransportSSE

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := provider.opts.Transport, sigma.TransportSSE; got != want {
		t.Fatalf("transport = %q, want model default %q", got, want)
	}
}

func TestOptionsMergeHeadersAndProviderOptions(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultHeaders(map[string]string{
			"x-client":   "client",
			"x-override": "client",
		}),
		sigma.WithDefaultOptions(
			sigma.WithHeaders(map[string]string{
				"x-default-option": "default-option",
				"x-override":       "default-option",
			}),
			sigma.WithProviderOptions(sigma.ProviderOpenAI, map[string]any{
				"tier":     "default",
				"priority": "low",
			}),
		),
	)

	_, err := client.Complete(context.Background(), model, sigma.Request{},
		sigma.WithHeaders(map[string]string{
			"x-call":     "call",
			"x-override": "call",
		}),
		sigma.WithProviderOptions(sigma.ProviderOpenAI, map[string]any{
			"priority": "high",
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	assertHeader(t, provider.opts.Headers, "x-client", "client")
	assertHeader(t, provider.opts.Headers, "x-default-option", "default-option")
	assertHeader(t, provider.opts.Headers, "x-call", "call")
	assertHeader(t, provider.opts.Headers, "x-override", "call")
	if got, want := provider.opts.ProviderOptions[sigma.ProviderOpenAI]["tier"], "default"; got != want {
		t.Fatalf("provider option tier = %v, want %v", got, want)
	}
	if got, want := provider.opts.ProviderOptions[sigma.ProviderOpenAI]["priority"], "high"; got != want {
		t.Fatalf("provider option priority = %v, want %v", got, want)
	}
}

func TestOptionsMetadataIsCopied(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{"trace": "original"}
	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithMetadata(metadata)),
	)
	metadata["trace"] = "mutated"

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := provider.opts.Metadata["trace"], "original"; got != want {
		t.Fatalf("metadata trace = %v, want %v", got, want)
	}

	provider.opts.Metadata["trace"] = "provider-mutated"
	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("second Complete returned error: %v", err)
	}
	if got, want := provider.opts.Metadata["trace"], "original"; got != want {
		t.Fatalf("metadata trace after provider mutation = %v, want %v", got, want)
	}
}

func TestOptionsInvalidValuesShortCircuitProviderDispatch(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t)

	_, err := client.Complete(context.Background(), model, sigma.Request{}, sigma.WithMaxRetries(-1))
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	assertSigmaLookupError(t, err, sigma.ErrorInvalidOptions, model.Provider, model.ID)
	if provider.called {
		t.Fatal("provider was called for invalid options")
	}
}

func TestOptionsAPIKeyOverrideDoesNotLeakIntoDefaults(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithAPIKey("default-secret")),
	)

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete without request API key returned error: %v", err)
	}
	if provider.opts.APIKey != "" {
		t.Fatal("default API key was retained in client defaults")
	}

	if _, err := client.Complete(context.Background(), model, sigma.Request{}, sigma.WithAPIKey("request-secret")); err != nil {
		t.Fatalf("Complete with request API key returned error: %v", err)
	}
	if got, want := provider.opts.APIKey, "request-secret"; got != want {
		t.Fatalf("api key = %q, want %q", got, want)
	}
	credential, err := provider.opts.AuthResolver.Resolve(context.Background(), model, provider.opts)
	if err != nil {
		t.Fatalf("AuthResolver returned error: %v", err)
	}
	if got, want := credential.Value, "request-secret"; got != want {
		t.Fatalf("resolved api key = %q, want %q", got, want)
	}

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete after request API key returned error: %v", err)
	}
	if provider.opts.APIKey != "" {
		t.Fatal("request API key leaked into later call")
	}
}

func TestTypedProviderOptionsAreCopied(t *testing.T) {
	t.Parallel()

	budget := 128
	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithAnthropicOptions(sigma.AnthropicOptions{
			ThinkingBudgetTokens: &budget,
		})),
	)
	budget = 256

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := valueOf(provider.opts.AnthropicOptions.ThinkingBudgetTokens), 128; got != want {
		t.Fatalf("anthropic thinking budget tokens = %d, want %d", got, want)
	}

	*provider.opts.AnthropicOptions.ThinkingBudgetTokens = 512
	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("second Complete returned error: %v", err)
	}
	if got, want := valueOf(provider.opts.AnthropicOptions.ThinkingBudgetTokens), 128; got != want {
		t.Fatalf("anthropic thinking budget tokens after mutation = %d, want %d", got, want)
	}
}

func TestOptionsValidateCommonInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opt  sigma.Option
	}{
		{name: "temperature", opt: sigma.WithTemperature(-0.1)},
		{name: "max tokens", opt: sigma.WithMaxTokens(-1)},
		{name: "timeout", opt: sigma.WithTimeout(-time.Second)},
		{name: "max retry delay", opt: sigma.WithMaxRetryDelay(-time.Second)},
		{name: "thinking budget", opt: sigma.WithThinkingBudgetTokens(-1)},
		{name: "openai top logprobs", opt: sigma.WithOpenAIOptions(sigma.OpenAIOptions{TopLogprobs: -1})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, provider, model := newOptionsTestClient(t)
			_, err := client.Complete(context.Background(), model, sigma.Request{}, tt.opt)
			if err == nil {
				t.Fatal("Complete returned nil error")
			}
			var sigmaErr *sigma.Error
			if !stderrors.As(err, &sigmaErr) {
				t.Fatalf("error type = %T, want *sigma.Error", err)
			}
			if sigmaErr.Code != sigma.ErrorInvalidOptions {
				t.Fatalf("error code = %q, want %q", sigmaErr.Code, sigma.ErrorInvalidOptions)
			}
			if provider.called {
				t.Fatal("provider was called for invalid options")
			}
		})
	}
}

func TestOpenAIOptionsValidateAPICompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		api      sigma.API
		metadata map[string]any
		options  sigma.OpenAIOptions
	}{
		{
			name: "response format rejects non openai api",
			api:  sigma.APIAnthropicMessages,
			options: sigma.OpenAIOptions{
				ResponseFormat: map[string]any{"type": "json_object"},
			},
		},
		{
			name: "logprobs rejects responses api",
			api:  sigma.APIOpenAIResponses,
			options: sigma.OpenAIOptions{
				TopLogprobs: 2,
			},
		},
		{
			name: "logprobs rejects codex responses api",
			api:  sigma.APIOpenAICodexResponses,
			options: sigma.OpenAIOptions{
				TopLogprobs: 2,
			},
		},
		{
			name: "logprobs rejects non openai api",
			api:  sigma.APIAnthropicMessages,
			options: sigma.OpenAIOptions{
				TopLogprobs: 2,
			},
		},
		{
			name: "logprobs rejects routed opencode responses api",
			api:  sigma.APIOpenAICompletions,
			metadata: map[string]any{
				"opencodeAPI": string(sigma.APIOpenAIResponses),
			},
			options: sigma.OpenAIOptions{
				TopLogprobs: 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, provider, model := newOptionsTestClient(t)
			model.API = tt.api
			model.ProviderMetadata = tt.metadata

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{},
				sigma.WithOpenAIOptions(tt.options),
			)
			if err == nil {
				t.Fatal("Complete returned nil error")
			}
			var sigmaErr *sigma.Error
			if !stderrors.As(err, &sigmaErr) {
				t.Fatalf("error type = %T, want *sigma.Error", err)
			}
			if sigmaErr.Code != sigma.ErrorInvalidOptions {
				t.Fatalf("error code = %q, want %q", sigmaErr.Code, sigma.ErrorInvalidOptions)
			}
			if provider.called {
				t.Fatal("provider was called for invalid options")
			}
		})
	}
}

func newOptionsTestClient(t *testing.T, opts ...sigma.ClientOption) (*sigma.Client, *optionsRecordingProvider, sigma.Model) {
	t.Helper()

	registry := sigma.NewRegistry()
	provider := &optionsRecordingProvider{
		final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("ok")},
		},
	}
	providerID := sigma.ProviderID("options-provider")
	model := sigma.Model{ID: "options-model", Provider: providerID, API: sigma.APIOpenAIResponses}
	if err := registry.RegisterTextProvider(providerID, provider); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	clientOptions := []sigma.ClientOption{sigma.WithRegistry(registry)}
	clientOptions = append(clientOptions, opts...)
	return sigma.NewClient(clientOptions...), provider, model
}

func valueOf[T comparable](value *T) T {
	var zero T
	if value == nil {
		return zero
	}
	return *value
}
