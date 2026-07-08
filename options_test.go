// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	stderrors "errors"
	"reflect"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

type optionsRecordingProvider struct {
	api    sigma.API
	opts   sigma.Options
	final  sigma.AssistantMessage
	called bool
}

func (p *optionsRecordingProvider) API() sigma.API {
	if p.api != "" {
		return p.api
	}
	return optionsTestAPI
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

func TestProviderNeutralControlsMergeAndMapToOpenAIOptions(t *testing.T) {
	t.Parallel()

	schema := map[string]any{"type": "object"}
	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(
			sigma.WithJSONSchemaOutput("answer", schema, true),
			sigma.WithTopLogprobs(3),
		),
	)
	model.API = sigma.APIOpenAICompletions
	schema["type"] = "mutated"

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if provider.opts.StructuredOutput == nil {
		t.Fatal("structured output = nil")
	}
	if got, want := provider.opts.StructuredOutput.Schema.(map[string]any)["type"], "object"; got != want {
		t.Fatalf("structured output schema type = %v, want %v", got, want)
	}
	if got, want := provider.opts.TopLogprobs, 3; got != want {
		t.Fatalf("top logprobs = %d, want %d", got, want)
	}
	if provider.opts.OpenAIOptions == nil {
		t.Fatal("openai options = nil")
	}
	responseFormat := provider.opts.OpenAIOptions.ResponseFormat.(map[string]any)
	jsonSchema := responseFormat["json_schema"].(map[string]any)
	if got, want := responseFormat["type"], "json_schema"; got != want {
		t.Fatalf("response format type = %v, want %v", got, want)
	}
	if got, want := jsonSchema["name"], "answer"; got != want {
		t.Fatalf("json schema name = %v, want %v", got, want)
	}
	if got, want := jsonSchema["strict"], true; got != want {
		t.Fatalf("json schema strict = %v, want %v", got, want)
	}
	if got, want := jsonSchema["schema"].(map[string]any)["type"], "object"; got != want {
		t.Fatalf("json schema type = %v, want %v", got, want)
	}
	if got, want := provider.opts.OpenAIOptions.TopLogprobs, 3; got != want {
		t.Fatalf("openai top logprobs = %d, want %d", got, want)
	}

	provider.opts.StructuredOutput.Schema.(map[string]any)["type"] = "provider-mutated"
	jsonSchema["schema"].(map[string]any)["type"] = "provider-mutated"
	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("second Complete returned error: %v", err)
	}
	if got, want := provider.opts.StructuredOutput.Schema.(map[string]any)["type"], "object"; got != want {
		t.Fatalf("structured output schema type after mutation = %v, want %v", got, want)
	}
	responseFormat = provider.opts.OpenAIOptions.ResponseFormat.(map[string]any)
	jsonSchema = responseFormat["json_schema"].(map[string]any)
	if got, want := jsonSchema["schema"].(map[string]any)["type"], "object"; got != want {
		t.Fatalf("json schema type after mutation = %v, want %v", got, want)
	}
}

func TestProviderSpecificStructuredOptionsWinOverNeutralControls(t *testing.T) {
	t.Parallel()

	explicit := map[string]any{"type": "json_object", "note": "explicit"}
	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(
			sigma.WithJSONSchemaOutput("neutral", sigma.Schema{"type": "object"}, true),
			sigma.WithTopLogprobs(3),
			sigma.WithOpenAIOptions(sigma.OpenAIOptions{
				ResponseFormat: explicit,
				TopLogprobs:    7,
			}),
		),
	)
	model.API = sigma.APIOpenAICompletions

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got := provider.opts.OpenAIOptions.ResponseFormat; !reflect.DeepEqual(got, explicit) {
		t.Fatalf("response format = %#v, want %#v", got, explicit)
	}
	if got, want := provider.opts.OpenAIOptions.TopLogprobs, 7; got != want {
		t.Fatalf("openai top logprobs = %d, want %d", got, want)
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
			sigma.WithSuppressedHeader("x-default-suppressed"),
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
		sigma.WithSuppressedHeaders("x-call-suppressed", " "),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	assertHeader(t, provider.opts.Headers, "x-client", "client")
	assertHeader(t, provider.opts.Headers, "x-default-option", "default-option")
	assertHeader(t, provider.opts.Headers, "x-call", "call")
	assertHeader(t, provider.opts.Headers, "x-override", "call")
	if got, want := provider.opts.SuppressedHeaders, []string{"x-default-suppressed", "x-call-suppressed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("suppressed headers = %#v, want %#v", got, want)
	}
	if got, want := provider.opts.ProviderOptions[sigma.ProviderOpenAI]["tier"], "default"; got != want {
		t.Fatalf("provider option tier = %v, want %v", got, want)
	}
	if got, want := provider.opts.ProviderOptions[sigma.ProviderOpenAI]["priority"], "high"; got != want {
		t.Fatalf("provider option priority = %v, want %v", got, want)
	}

	provider.opts.SuppressedHeaders[0] = "mutated"
	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("second Complete returned error: %v", err)
	}
	if got, want := provider.opts.SuppressedHeaders, []string{"x-default-suppressed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("suppressed headers after mutation = %#v, want %#v", got, want)
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

func TestWithMaxTokensForContextSetsOnlyUsableBudget(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithMaxTokens(300)),
	)
	model.ContextWindow = 5000
	model.MaxOutputTokens = 1000
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if _, err := client.Complete(context.Background(), model, req, sigma.WithMaxTokensForContext(model, req, 1000)); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := valueOf(provider.opts.MaxTokens), 902; got != want {
		t.Fatalf("max tokens = %d, want %d", got, want)
	}

	model.ContextWindow = 0
	model.MaxOutputTokens = 0
	if _, err := client.Complete(context.Background(), model, req, sigma.WithMaxTokensForContext(model, req, 0)); err != nil {
		t.Fatalf("Complete with empty context budget returned error: %v", err)
	}
	if got, want := valueOf(provider.opts.MaxTokens), 300; got != want {
		t.Fatalf("max tokens after empty context budget = %d, want default %d", got, want)
	}
}

func TestAutomaticMaxTokensForContextDefaultDisabledLeavesMaxTokensUnchanged(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithMaxTokens(900)),
	)
	model.ContextWindow = 4500
	model.MaxOutputTokens = 1000
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if _, err := client.Complete(context.Background(), model, req); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := valueOf(provider.opts.MaxTokens), 900; got != want {
		t.Fatalf("max tokens = %d, want unchanged %d", got, want)
	}
}

func TestAutomaticMaxTokensForContextUsesModelCapWhenUnset(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t)
	model.ContextWindow = 5000
	model.MaxOutputTokens = 1000
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if _, err := client.Complete(context.Background(), model, req, sigma.WithAutomaticMaxTokensForContext(true)); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := valueOf(provider.opts.MaxTokens), 902; got != want {
		t.Fatalf("max tokens = %d, want %d", got, want)
	}
}

func TestAutomaticMaxTokensForContextClampsExplicitMaxTokens(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t)
	model.ContextWindow = 4500
	model.MaxOutputTokens = 2000
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if _, err := client.Complete(
		context.Background(),
		model,
		req,
		sigma.WithMaxTokens(800),
		sigma.WithAutomaticMaxTokensForContext(true),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := valueOf(provider.opts.MaxTokens), 402; got != want {
		t.Fatalf("max tokens = %d, want %d", got, want)
	}
}

func TestAutomaticMaxTokensForContextRequestCanDisableDefault(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithAutomaticMaxTokensForContext(true)),
	)
	model.ContextWindow = 4500
	model.MaxOutputTokens = 2000
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if _, err := client.Complete(
		context.Background(),
		model,
		req,
		sigma.WithMaxTokens(800),
		sigma.WithAutomaticMaxTokensForContext(false),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := valueOf(provider.opts.MaxTokens), 800; got != want {
		t.Fatalf("max tokens = %d, want unclamped request value %d", got, want)
	}
}

func TestAutomaticMaxTokensForContextMissingCapsLeavesMaxTokensUnset(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t)
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if _, err := client.Complete(context.Background(), model, req, sigma.WithAutomaticMaxTokensForContext(true)); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if provider.opts.MaxTokens != nil {
		t.Fatalf("max tokens = %d, want unset", *provider.opts.MaxTokens)
	}
}

func TestAutomaticMaxTokensForContextDoesNotMaskInvalidMaxTokens(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t)
	model.ContextWindow = 5000
	model.MaxOutputTokens = 1000

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}},
		sigma.WithMaxTokens(-1),
		sigma.WithAutomaticMaxTokensForContext(true),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	assertSigmaLookupError(t, err, sigma.ErrorInvalidOptions, model.Provider, model.ID)
	if provider.called {
		t.Fatal("provider was called for invalid options")
	}
}

func TestWithReasoningBudgetForContextSetsReasoningAndBudgets(t *testing.T) {
	t.Parallel()

	client, provider, model := newOptionsTestClient(t)
	model.ContextWindow = 50000
	model.MaxOutputTokens = 20000
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("12345678")}}

	if _, err := client.Complete(context.Background(), model, req, sigma.WithReasoningBudgetForContext(model, req, sigma.ThinkingLevelLow, 1000)); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := provider.opts.ReasoningLevel, sigma.ThinkingLevelLow; got != want {
		t.Fatalf("reasoning level = %q, want %q", got, want)
	}
	if got, want := valueOf(provider.opts.MaxTokens), 3048; got != want {
		t.Fatalf("max tokens = %d, want %d", got, want)
	}
	if got, want := valueOf(provider.opts.ThinkingBudgetTokens), 2024; got != want {
		t.Fatalf("thinking budget tokens = %d, want %d", got, want)
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
	interleaved := true
	disableParallel := true
	outputFormat := map[string]any{"type": "json_schema"}
	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithAnthropicOptions(sigma.AnthropicOptions{
			ThinkingBudgetTokens:   &budget,
			ToolChoice:             &sigma.AnthropicToolChoice{Type: sigma.AnthropicToolChoiceTool, Name: "lookup"},
			ThinkingDisplay:        sigma.AnthropicThinkingDisplayOmitted,
			InterleavedThinking:    &interleaved,
			OutputFormat:           outputFormat,
			DisableParallelToolUse: &disableParallel,
		})),
	)
	budget = 256
	interleaved = false
	disableParallel = false
	outputFormat["type"] = "mutated"

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	got := provider.opts.AnthropicOptions
	if got == nil {
		t.Fatal("anthropic options = nil")
	}
	if gotBudget, want := valueOf(got.ThinkingBudgetTokens), 128; gotBudget != want {
		t.Fatalf("anthropic thinking budget tokens = %d, want %d", gotBudget, want)
	}
	if got.ToolChoice == nil || got.ToolChoice.Name != "lookup" {
		t.Fatalf("anthropic tool choice = %+v, want lookup", got.ToolChoice)
	}
	if got.ThinkingDisplay != sigma.AnthropicThinkingDisplayOmitted {
		t.Fatalf("anthropic thinking display = %q, want %q", got.ThinkingDisplay, sigma.AnthropicThinkingDisplayOmitted)
	}
	if got.InterleavedThinking == nil || !*got.InterleavedThinking {
		t.Fatalf("anthropic interleaved thinking = %v, want true", got.InterleavedThinking)
	}
	if got.OutputFormat.(map[string]any)["type"] != "json_schema" {
		t.Fatalf("anthropic output format = %#v, want json_schema", got.OutputFormat)
	}
	if got.DisableParallelToolUse == nil || !*got.DisableParallelToolUse {
		t.Fatalf("anthropic disable parallel tool use = %v, want true", got.DisableParallelToolUse)
	}

	*provider.opts.AnthropicOptions.ThinkingBudgetTokens = 512
	provider.opts.AnthropicOptions.ToolChoice.Name = "mutated"
	*provider.opts.AnthropicOptions.InterleavedThinking = false
	provider.opts.AnthropicOptions.OutputFormat.(map[string]any)["type"] = "provider-mutated"
	*provider.opts.AnthropicOptions.DisableParallelToolUse = false
	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("second Complete returned error: %v", err)
	}
	got = provider.opts.AnthropicOptions
	if gotBudget, want := valueOf(got.ThinkingBudgetTokens), 128; gotBudget != want {
		t.Fatalf("anthropic thinking budget tokens after mutation = %d, want %d", gotBudget, want)
	}
	if got.ToolChoice == nil || got.ToolChoice.Name != "lookup" {
		t.Fatalf("anthropic tool choice after mutation = %+v, want lookup", got.ToolChoice)
	}
	if got.InterleavedThinking == nil || !*got.InterleavedThinking {
		t.Fatalf("anthropic interleaved thinking after mutation = %v, want true", got.InterleavedThinking)
	}
	if got.OutputFormat.(map[string]any)["type"] != "json_schema" {
		t.Fatalf("anthropic output format after mutation = %#v, want json_schema", got.OutputFormat)
	}
	if got.DisableParallelToolUse == nil || !*got.DisableParallelToolUse {
		t.Fatalf("anthropic disable parallel tool use after mutation = %v, want true", got.DisableParallelToolUse)
	}
}

func TestOpenAIOptionsAreCopied(t *testing.T) {
	t.Parallel()

	parallelToolCalls := true
	connectTimeout := 50 * time.Millisecond
	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithOpenAIOptions(sigma.OpenAIOptions{
			ParallelToolCalls:            &parallelToolCalls,
			CodexWebSocketConnectTimeout: &connectTimeout,
		})),
	)
	parallelToolCalls = false
	connectTimeout = time.Second

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	got := provider.opts.OpenAIOptions
	if got == nil {
		t.Fatal("openai options = nil")
	}
	if got.ParallelToolCalls == nil || !*got.ParallelToolCalls {
		t.Fatalf("openai parallel tool calls = %v, want true", got.ParallelToolCalls)
	}
	if gotTimeout, want := valueOf(got.CodexWebSocketConnectTimeout), 50*time.Millisecond; gotTimeout != want {
		t.Fatalf("openai codex websocket connect timeout = %s, want %s", gotTimeout, want)
	}

	*provider.opts.OpenAIOptions.ParallelToolCalls = false
	*provider.opts.OpenAIOptions.CodexWebSocketConnectTimeout = 2 * time.Second
	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("second Complete returned error: %v", err)
	}
	got = provider.opts.OpenAIOptions
	if got.ParallelToolCalls == nil || !*got.ParallelToolCalls {
		t.Fatalf("openai parallel tool calls after mutation = %v, want true", got.ParallelToolCalls)
	}
	if gotTimeout, want := valueOf(got.CodexWebSocketConnectTimeout), 50*time.Millisecond; gotTimeout != want {
		t.Fatalf("openai codex websocket connect timeout after mutation = %s, want %s", gotTimeout, want)
	}
}

func TestBedrockOptionsAreCopied(t *testing.T) {
	t.Parallel()

	topP := 0.8
	interleaved := true
	responseFormat := map[string]any{"type": "object"}
	options := sigma.BedrockOptions{
		ToolChoice:          &sigma.BedrockToolChoice{Type: sigma.BedrockToolChoiceTool, Name: "lookup"},
		BearerToken:         "bedrock-token",
		InterleavedThinking: &interleaved,
		TopP:                &topP,
		StopSequences:       []string{"stop"},
		ResponseFormat:      responseFormat,
		RequestMetadata:     map[string]string{"trace": "original"},
		AdditionalModelRequestFields: map[string]any{
			"custom": "original",
		},
		AdditionalModelResponseFieldPaths: []string{"/stop_sequence"},
	}
	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithBedrockOptions(options)),
	)
	options.ToolChoice.Name = "mutated"
	options.StopSequences[0] = "mutated"
	options.RequestMetadata["trace"] = "mutated"
	options.AdditionalModelRequestFields["custom"] = "mutated"
	options.AdditionalModelResponseFieldPaths[0] = "/mutated"
	responseFormat["type"] = "mutated"
	topP = 0.2
	interleaved = false

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	got := provider.opts.BedrockOptions
	if got == nil {
		t.Fatal("bedrock options = nil")
	}
	if got.ToolChoice == nil || got.ToolChoice.Name != "lookup" {
		t.Fatalf("tool choice = %+v, want lookup", got.ToolChoice)
	}
	if got.BearerToken != "bedrock-token" {
		t.Fatalf("bearer token = %q, want bedrock-token", got.BearerToken)
	}
	if got.StopSequences[0] != "stop" {
		t.Fatalf("stop sequence = %q, want stop", got.StopSequences[0])
	}
	if got.RequestMetadata["trace"] != "original" {
		t.Fatalf("request metadata = %q, want original", got.RequestMetadata["trace"])
	}
	if got.AdditionalModelRequestFields["custom"] != "original" {
		t.Fatalf("additional fields = %v, want original", got.AdditionalModelRequestFields["custom"])
	}
	if got.AdditionalModelResponseFieldPaths[0] != "/stop_sequence" {
		t.Fatalf("response field path = %q, want /stop_sequence", got.AdditionalModelResponseFieldPaths[0])
	}
	if valueOf(got.TopP) != 0.8 {
		t.Fatalf("top_p = %v, want 0.8", valueOf(got.TopP))
	}
	if got.ResponseFormat.(map[string]any)["type"] != "object" {
		t.Fatalf("response format = %#v, want object", got.ResponseFormat)
	}
	if !valueOf(got.InterleavedThinking) {
		t.Fatal("interleaved thinking = false, want true")
	}

	got.ToolChoice.Name = "provider-mutated"
	got.StopSequences[0] = "provider-mutated"
	got.RequestMetadata["trace"] = "provider-mutated"
	got.AdditionalModelRequestFields["custom"] = "provider-mutated"
	got.AdditionalModelResponseFieldPaths[0] = "/provider-mutated"
	got.ResponseFormat.(map[string]any)["type"] = "provider-mutated"
	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("second Complete returned error: %v", err)
	}
	got = provider.opts.BedrockOptions
	if got.ToolChoice.Name != "lookup" ||
		got.StopSequences[0] != "stop" ||
		got.RequestMetadata["trace"] != "original" ||
		got.AdditionalModelRequestFields["custom"] != "original" ||
		got.AdditionalModelResponseFieldPaths[0] != "/stop_sequence" ||
		got.BearerToken != "bedrock-token" ||
		got.ResponseFormat.(map[string]any)["type"] != "object" {
		t.Fatalf("bedrock options were not cloned after provider mutation: %+v", got)
	}
}

func TestMistralOptionsAreCopied(t *testing.T) {
	t.Parallel()

	options := sigma.MistralOptions{
		ToolChoice: &sigma.MistralToolChoice{Type: sigma.MistralToolChoiceRequired},
	}
	client, provider, model := newOptionsTestClient(t,
		sigma.WithDefaultOptions(sigma.WithMistralOptions(options)),
	)
	options.ToolChoice.Type = sigma.MistralToolChoiceNone

	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	got := provider.opts.MistralOptions
	if got == nil {
		t.Fatal("mistral options = nil")
	}
	if got.ToolChoice == nil || got.ToolChoice.Type != sigma.MistralToolChoiceRequired {
		t.Fatalf("mistral tool choice = %+v, want required", got.ToolChoice)
	}

	got.ToolChoice.Type = sigma.MistralToolChoiceNone
	if _, err := client.Complete(context.Background(), model, sigma.Request{}); err != nil {
		t.Fatalf("second Complete returned error: %v", err)
	}
	got = provider.opts.MistralOptions
	if got.ToolChoice == nil || got.ToolChoice.Type != sigma.MistralToolChoiceRequired {
		t.Fatalf("mistral tool choice after mutation = %+v, want required", got.ToolChoice)
	}
}

func TestOptionsValidateCommonInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opt  sigma.Option
	}{
		{name: "transport", opt: sigma.WithTransport(sigma.Transport("named-pipe"))},
		{name: "temperature", opt: sigma.WithTemperature(-0.1)},
		{name: "max tokens", opt: sigma.WithMaxTokens(-1)},
		{name: "timeout", opt: sigma.WithTimeout(-time.Second)},
		{name: "max retry delay", opt: sigma.WithMaxRetryDelay(-time.Second)},
		{name: "thinking budget", opt: sigma.WithThinkingBudgetTokens(-1)},
		{name: "top logprobs", opt: sigma.WithTopLogprobs(-1)},
		{name: "openai top logprobs", opt: sigma.WithOpenAIOptions(sigma.OpenAIOptions{TopLogprobs: -1})},
		{name: "openai codex websocket connect timeout", opt: sigma.WithOpenAIOptions(sigma.OpenAIOptions{CodexWebSocketConnectTimeout: testDurationPtr(-time.Second)})},
		{name: "mistral simple tool choice with name", opt: sigma.WithMistralOptions(sigma.MistralOptions{ToolChoice: &sigma.MistralToolChoice{Type: sigma.MistralToolChoiceAuto, Name: "lookup"}})},
		{name: "mistral named tool without name", opt: sigma.WithMistralOptions(sigma.MistralOptions{ToolChoice: &sigma.MistralToolChoice{Type: sigma.MistralToolChoiceTool}})},
		{name: "mistral named tool with name", opt: sigma.WithMistralOptions(sigma.MistralOptions{ToolChoice: &sigma.MistralToolChoice{Type: sigma.MistralToolChoiceTool, Name: "lookup"}})},
		{name: "bedrock top p", opt: sigma.WithBedrockOptions(sigma.BedrockOptions{TopP: testFloat64Ptr(-0.1)})},
		{name: "bedrock tool choice", opt: sigma.WithBedrockOptions(sigma.BedrockOptions{ToolChoice: &sigma.BedrockToolChoice{Type: sigma.BedrockToolChoiceTool}})},
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

func TestProviderNeutralControlsValidateAPICompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		api  sigma.API
		opt  sigma.Option
	}{
		{
			name: "structured output rejects google",
			api:  sigma.APIGoogleGenerativeAI,
			opt:  sigma.WithJSONOutput(),
		},
		{
			name: "structured output rejects mistral",
			api:  sigma.APIMistralConversations,
			opt:  sigma.WithJSONSchemaOutput("answer", sigma.Schema{"type": "object"}, true),
		},
		{
			name: "structured output rejects custom",
			api:  optionsTestAPI,
			opt:  sigma.WithJSONOutput(),
		},
		{
			name: "structured output rejects missing schema name",
			api:  sigma.APIOpenAICompletions,
			opt:  sigma.WithJSONSchemaOutput("", sigma.Schema{"type": "object"}, true),
		},
		{
			name: "structured output rejects missing schema",
			api:  sigma.APIOpenAICompletions,
			opt:  sigma.WithJSONSchemaOutput("answer", nil, true),
		},
		{
			name: "structured output rejects unknown type",
			api:  sigma.APIOpenAICompletions,
			opt:  sigma.WithStructuredOutput(sigma.StructuredOutput{Type: "xml"}),
		},
		{
			name: "top logprobs rejects responses",
			api:  sigma.APIOpenAIResponses,
			opt:  sigma.WithTopLogprobs(2),
		},
		{
			name: "top logprobs rejects anthropic",
			api:  sigma.APIAnthropicMessages,
			opt:  sigma.WithTopLogprobs(2),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, provider, model := newOptionsTestClient(t)
			model.API = tt.api
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
				t.Fatal("provider was called for invalid provider-neutral controls")
			}
		})
	}
}

func TestTransportValidationCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		api         sigma.API
		transport   sigma.Transport
		wantError   bool
		wantCalled  bool
		wantCapture sigma.Transport
	}{
		{
			name:      "non codex built in rejects websocket",
			api:       sigma.APIOpenAIResponses,
			transport: sigma.TransportWebSocket,
			wantError: true,
		},
		{
			name:      "non codex built in rejects http",
			api:       sigma.APIAnthropicMessages,
			transport: sigma.TransportHTTP,
			wantError: true,
		},
		{
			name:        "codex accepts websocket",
			api:         sigma.APIOpenAICodexResponses,
			transport:   sigma.TransportWebSocket,
			wantCalled:  true,
			wantCapture: sigma.TransportWebSocket,
		},
		{
			name:        "custom api accepts known websocket",
			api:         optionsTestAPI,
			transport:   sigma.TransportWebSocket,
			wantCalled:  true,
			wantCapture: sigma.TransportWebSocket,
		},
		{
			name:        "custom api accepts known http",
			api:         optionsTestAPI,
			transport:   sigma.TransportHTTP,
			wantCalled:  true,
			wantCapture: sigma.TransportHTTP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, provider, model := newOptionsTestClient(t)
			model.API = tt.api

			_, err := client.Complete(context.Background(), model, sigma.Request{}, sigma.WithTransport(tt.transport))
			if tt.wantError {
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
					t.Fatal("provider was called for invalid transport")
				}
				return
			}

			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}
			if provider.called != tt.wantCalled {
				t.Fatalf("provider called = %v, want %v", provider.called, tt.wantCalled)
			}
			if got := provider.opts.Transport; got != tt.wantCapture {
				t.Fatalf("transport = %q, want %q", got, tt.wantCapture)
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

const optionsTestAPI sigma.API = "options-test"

func newOptionsTestClient(t *testing.T, opts ...sigma.ClientOption) (*sigma.Client, *optionsRecordingProvider, sigma.Model) {
	t.Helper()

	registry := sigma.NewRegistry()
	provider := &optionsRecordingProvider{
		api: optionsTestAPI,
		final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("ok")},
		},
	}
	providerID := sigma.ProviderID("options-provider")
	model := sigma.Model{ID: "options-model", Provider: providerID, API: optionsTestAPI}
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

func testDurationPtr(value time.Duration) *time.Duration {
	return &value
}

func testFloat64Ptr(value float64) *float64 {
	return &value
}
