// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
	"github.com/wintermi/sigma/provider/openai"
)

func TestRegisterResponsesReportsResponsesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("responses-compatible")
	if err := openai.RegisterResponses(registry, providerID); err != nil {
		t.Fatalf("RegisterResponses returned error: %v", err)
	}
	if err := registry.RegisterModel(responsesTestModel(providerID)); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].TextAPI, sigma.APIOpenAIResponses; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestResponsesCompleteSendsGoldenPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-payload-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL, openai.WithHeader("X-Provider", "provider"))
	parallelToolCalls := false

	final, err := client.Complete(
		context.Background(),
		model,
		responsesRichRequest(),
		sigma.WithTemperature(0.2),
		sigma.WithMaxTokens(123),
		sigma.WithSessionID("session-123"),
		sigma.WithHeader("X-Custom", "custom"),
		sigma.WithMetadataValue("trace", "abc"),
		sigma.WithToolChoice(sigma.ToolChoiceNone),
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{
			ReasoningEffort:      sigma.ThinkingLevelHigh,
			ReasoningSummary:     "auto",
			ServiceTier:          "default",
			ToolChoice:           "auto",
			PromptCacheRetention: "24h",
			ParallelToolCalls:    &parallelToolCalls,
			TextVerbosity:        "low",
		}),
		sigma.WithProviderOptions(providerID, map[string]any{
			"session_id_header":    "X-Session-ID",
			"store":                false,
			"include":              []any{"reasoning.encrypted_content"},
			"previous_response_id": "resp_prev",
			"text":                 map[string]any{"format": map[string]any{"type": "text"}},
			"truncation":           "auto",
			"prompt_cache_key":     "cache-key",
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.ProviderMetadata["id"], "resp_complete"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer resolved-key")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	assertHeader(t, request.Headers, "X-Session-ID", "session-123")
	goldentest.AssertJSON(t, request.Body, "provider/openai/responses/rich_payload.json")
}

func TestResponsesSendsProviderNeutralToolChoice(t *testing.T) {
	t.Parallel()

	for _, choice := range []sigma.ToolChoice{sigma.ToolChoiceAuto, sigma.ToolChoiceNone} {
		choice := choice
		t.Run(string(choice), func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-neutral-tool-choice-" + string(choice))
			model := responsesTestModel(providerID)
			client := responsesTestClient(t, providerID, model, server.URL)

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{
					Messages: []sigma.Message{sigma.UserText("Use a tool.")},
					Tools:    []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}},
				},
				sigma.WithToolChoice(choice),
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
			if got := payload["tool_choice"]; got != string(choice) {
				t.Fatalf("tool choice = %#v, want %q", got, choice)
			}
			if tools, ok := payload["tools"].([]any); !ok || len(tools) != 1 {
				t.Fatalf("tools = %#v, want one tool", payload["tools"])
			}
		})
	}
}

func TestResponsesDerivesStrictToolSchemaWithoutMutatingRequest(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-strict-schema-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	schema := sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"city":  map[string]any{"type": "string"},
			"units": map[string]any{"type": "string"},
		},
		"required": []any{"city"},
	}
	wantOriginal := sigma.Schema{
		"type": "object",
		"properties": map[string]any{
			"city":  map[string]any{"type": "string"},
			"units": map[string]any{"type": "string"},
		},
		"required": []any{"city"},
	}

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("Use the weather tool.")},
		Tools: []sigma.Tool{{
			Name:             "weather",
			InputSchema:      schema,
			ProviderMetadata: map[string]any{"strict": true},
		}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if !reflect.DeepEqual(schema, wantOriginal) {
		t.Fatalf("Complete mutated schema: got %#v, want %#v", schema, wantOriginal)
	}

	payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
	tool := payload["tools"].([]any)[0].(map[string]any)
	if got, want := tool["strict"], true; got != want {
		t.Fatalf("strict = %#v, want %v", got, want)
	}
	parameters := tool["parameters"].(map[string]any)
	if got, want := parameters["required"], []any{"city", "units"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("required = %#v, want %#v", got, want)
	}
	if got := parameters["additionalProperties"]; got != false {
		t.Fatalf("additionalProperties = %#v, want false", got)
	}
	units := parameters["properties"].(map[string]any)["units"].(map[string]any)["anyOf"].([]any)
	if got, want := units[1], map[string]any{"type": "null"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("units null schema = %#v, want %#v", got, want)
	}
}

func TestResponsesRejectsUnsafeStrictSchemaBeforeRequest(t *testing.T) {
	t.Parallel()

	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- struct{}{}
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-strict-schema-error-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("Use the lookup tool.")},
		Tools: []sigma.Tool{{
			Name: "lookup",
			InputSchema: sigma.Schema{
				"type":       "object",
				"properties": map[string]any{"value": map[string]any{"$ref": "#/$defs/value"}},
			},
			ProviderMetadata: map[string]any{"strict": true},
		}},
	})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if got := err.Error(); !strings.Contains(got, `tool "lookup" strict schema`) || !strings.Contains(got, "$ref schemas are unsupported") {
		t.Fatalf("Complete error = %q, want strict tool schema context", got)
	}
	select {
	case <-requests:
		t.Fatal("strict schema error reached provider")
	default:
	}
}

func TestResponsesSendsSamplingParametersWithPrecedence(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-sampling-test")
	model := responsesTestModel(providerID)
	model.ProviderMetadata = map[string]any{
		sigma.MetadataOpenAISamplingParameters: map[string]any{
			"temperature": 0.1,
			"top_p":       0.7,
			"model_only":  "kept",
		},
	}
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("sample")}},
		sigma.WithTemperature(0.2),
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{SamplingParameters: map[string]any{
			"temperature": 0.6,
			"top_p":       0.8,
			"seed":        0,
		}}),
		sigma.WithProviderOption(providerID, "extra_body", map[string]any{
			"top_p": 0.95,
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
	for key, want := range map[string]any{
		"temperature": 0.6,
		"top_p":       0.95,
		"seed":        float64(0),
		"model_only":  "kept",
	} {
		if got := payload[key]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

func TestResponsesClampsMaxOutputTokens(t *testing.T) {
	t.Parallel()

	providerID := sigma.ProviderID("responses-max-tokens-test")
	tests := []struct {
		name        string
		options     []sigma.Option
		want        float64
		wantPresent bool
	}{
		{name: "unset"},
		{name: "zero", options: []sigma.Option{sigma.WithMaxTokens(0)}, want: 16, wantPresent: true},
		{name: "one", options: []sigma.Option{sigma.WithMaxTokens(1)}, want: 16, wantPresent: true},
		{name: "below minimum", options: []sigma.Option{sigma.WithMaxTokens(15)}, want: 16, wantPresent: true},
		{name: "at minimum", options: []sigma.Option{sigma.WithMaxTokens(16)}, want: 16, wantPresent: true},
		{name: "above minimum", options: []sigma.Option{sigma.WithMaxTokens(17)}, want: 17, wantPresent: true},
		{
			name: "sampling parameter override",
			options: []sigma.Option{
				sigma.WithMaxTokens(1),
				sigma.WithOpenAIOptions(sigma.OpenAIOptions{SamplingParameters: map[string]any{
					"max_output_tokens": 20,
				}}),
			},
			want:        20,
			wantPresent: true,
		},
		{
			name: "extra body override",
			options: []sigma.Option{
				sigma.WithMaxTokens(1),
				sigma.WithOpenAIOptions(sigma.OpenAIOptions{SamplingParameters: map[string]any{
					"max_output_tokens": 20,
				}}),
				sigma.WithProviderOption(providerID, "extra_body", map[string]any{
					"max_output_tokens": 8,
				}),
			},
			want:        8,
			wantPresent: true,
		},
	}
	requests := make(chan capturedRequest, len(tests))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				tt.options...,
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
			got, ok := payload["max_output_tokens"]
			if ok != tt.wantPresent {
				t.Fatalf("max_output_tokens presence = %v, want %v", ok, tt.wantPresent)
			}
			if ok && got != tt.want {
				t.Fatalf("max_output_tokens = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResponsesPromptCacheDoesNotUseSessionIDAsPreviousResponseID(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-cache-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID(strings.Repeat("x", 70)),
		sigma.WithCacheRetention(sigma.CacheRetentionLong),
		sigma.WithProviderOption(providerID, "prompt_cache_key", "explicit-cache-key"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := payload["prompt_cache_key"], "explicit-cache-key"; got != want {
		t.Fatalf("prompt_cache_key = %v, want %q", got, want)
	}
	if got, want := payload["prompt_cache_retention"], "24h"; got != want {
		t.Fatalf("prompt_cache_retention = %v, want %q", got, want)
	}
	if _, ok := payload["previous_response_id"]; ok {
		t.Fatalf("previous_response_id was sent from session id: %#v", payload)
	}
}

func TestResponsesSuppressesUnsupportedLongCacheRetention(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		options []sigma.Option
	}{
		{
			name: "automatic",
			options: []sigma.Option{
				sigma.WithCacheRetention(sigma.CacheRetentionLong),
			},
		},
		{
			name: "standard option",
			options: []sigma.Option{
				sigma.WithCacheRetention(sigma.CacheRetentionLong),
				sigma.WithOpenAIOptions(sigma.OpenAIOptions{PromptCacheRetention: "24h"}),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-no-long-cache-retention-test")
			model := responsesTestModel(providerID)
			model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{
				SupportsLongCacheRetention: sigma.OpenAICompatUnsupported,
			}
			client := responsesTestClient(t, providerID, model, server.URL)

			options := append([]sigma.Option{sigma.WithSessionID("responses-session")}, tt.options...)
			if _, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				options...,
			); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
			if got, want := payload["prompt_cache_key"], "responses-session"; got != want {
				t.Fatalf("prompt_cache_key = %v, want %q", got, want)
			}
			if got, ok := payload["prompt_cache_retention"]; ok {
				t.Fatalf("prompt_cache_retention = %v, want absent", got)
			}
		})
	}
}

func TestResponsesPreservesRawLongCacheRetentionOverride(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-raw-long-cache-retention-test")
	model := responsesTestModel(providerID)
	model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{
		SupportsLongCacheRetention: sigma.OpenAICompatUnsupported,
	}
	client := responsesTestClient(t, providerID, model, server.URL)

	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID("responses-session"),
		sigma.WithCacheRetention(sigma.CacheRetentionLong),
		sigma.WithProviderOption(providerID, "extra_body", map[string]any{
			"prompt_cache_retention": "24h",
		}),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
	if got, want := payload["prompt_cache_retention"], "24h"; got != want {
		t.Fatalf("prompt_cache_retention = %v, want %q", got, want)
	}
}

func TestResponsesExplicitNoCacheMode(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-explicit-no-cache-test")
	model := responsesTestModel(providerID)
	model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{SupportsExplicitPromptCacheMode: true}
	client := responsesTestClient(t, providerID, model, server.URL)

	if _, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID("responses-session"),
		sigma.WithCacheRetention(sigma.CacheRetentionNone),
	); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	payload := decodeResponsesPayload(t, request.Body)
	if got, want := payload["prompt_cache_options"], map[string]any{"mode": "explicit"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prompt_cache_options = %#v, want %#v", got, want)
	}
	goldentest.AssertNoJSONPath(t, payload, "prompt_cache_key")
	goldentest.AssertNoJSONPath(t, payload, "prompt_cache_retention")
	assertHeaderAbsent(t, request.Headers, "session_id")
	assertHeaderAbsent(t, request.Headers, "x-client-request-id")
}

func TestResponsesExplicitNoCacheModeRequiresExplicitNoneAndCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		retention  sigma.CacheRetention
		compatible bool
		wantKey    bool
		wantLong   bool
	}{
		{name: "unset", compatible: true},
		{name: "short", retention: sigma.CacheRetentionShort, compatible: true, wantKey: true},
		{name: "long legacy", retention: sigma.CacheRetentionLong, wantKey: true, wantLong: true},
		{name: "unmarked", retention: sigma.CacheRetentionNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-explicit-no-cache-" + tt.name)
			model := responsesTestModel(providerID)
			if tt.compatible {
				model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{SupportsExplicitPromptCacheMode: true}
			}
			client := responsesTestClient(t, providerID, model, server.URL)
			options := []sigma.Option{sigma.WithSessionID("responses-session")}
			if tt.retention != "" {
				options = append(options, sigma.WithCacheRetention(tt.retention))
			}

			if _, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				options...,
			); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
			goldentest.AssertNoJSONPath(t, payload, "prompt_cache_options")
			if got, ok := payload["prompt_cache_key"]; ok != tt.wantKey || (ok && got != "responses-session") {
				t.Fatalf("prompt_cache_key = %#v, present %v; want session key present %v", got, ok, tt.wantKey)
			}
			if got, ok := payload["prompt_cache_retention"]; ok != tt.wantLong || (ok && got != "24h") {
				t.Fatalf("prompt_cache_retention = %#v, present %v; want 24h present %v", got, ok, tt.wantLong)
			}
		})
	}
}

func TestResponsesExplicitPromptCacheDirectivesOverrideAutomaticNoCacheMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		options            func(sigma.ProviderID) []sigma.Option
		wantCacheKey       string
		wantCacheRetention string
		wantCacheOptions   map[string]any
	}{
		{
			name: "typed retention",
			options: func(sigma.ProviderID) []sigma.Option {
				return []sigma.Option{sigma.WithOpenAIOptions(sigma.OpenAIOptions{PromptCacheRetention: "24h"})}
			},
			wantCacheRetention: "24h",
		},
		{
			name: "provider cache key",
			options: func(providerID sigma.ProviderID) []sigma.Option {
				return []sigma.Option{sigma.WithProviderOption(providerID, "prompt_cache_key", "provider-key")}
			},
			wantCacheKey: "provider-key",
		},
		{
			name: "raw cache options",
			options: func(providerID sigma.ProviderID) []sigma.Option {
				return []sigma.Option{sigma.WithProviderOption(providerID, "extra_body", map[string]any{
					"prompt_cache_options": map[string]any{"mode": "caller"},
				})}
			},
			wantCacheOptions: map[string]any{"mode": "caller"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-cache-directive-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := responsesTestModel(providerID)
			model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{SupportsExplicitPromptCacheMode: true}
			client := responsesTestClient(t, providerID, model, server.URL)
			options := []sigma.Option{sigma.WithCacheRetention(sigma.CacheRetentionNone)}
			options = append(options, tt.options(providerID)...)

			if _, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				options...,
			); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
			if got := payload["prompt_cache_key"]; got != tt.wantCacheKey && (got != nil || tt.wantCacheKey != "") {
				t.Fatalf("prompt_cache_key = %#v, want %q", got, tt.wantCacheKey)
			}
			if got := payload["prompt_cache_retention"]; got != tt.wantCacheRetention && (got != nil || tt.wantCacheRetention != "") {
				t.Fatalf("prompt_cache_retention = %#v, want %q", got, tt.wantCacheRetention)
			}
			gotCacheOptions, ok := payload["prompt_cache_options"]
			if tt.wantCacheOptions == nil {
				if ok {
					t.Fatalf("prompt_cache_options = %#v, want absent", gotCacheOptions)
				}
			} else if !ok || !reflect.DeepEqual(gotCacheOptions, tt.wantCacheOptions) {
				t.Fatalf("prompt_cache_options = %#v, present %v; want %#v", gotCacheOptions, ok, tt.wantCacheOptions)
			}
		})
	}
}

func TestResponsesDefersMarkedClientTools(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-deferred-tools")
	model := responsesTestModel(providerID)
	model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{SupportsToolSearch: true}
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, deferredToolsRequest())
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	assertDeferredToolsPayload(t, receiveRequest(t, requests).Body)
}

func TestResponsesUsesAdditionalToolsWhenSupported(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-additional-tools")
	model := responsesTestModel(providerID)
	model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{
		SupportsAdditionalTools: true,
		SupportsToolSearch:      true,
	}
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, deferredToolsRequest())
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	assertAdditionalToolsPayload(t, receiveRequest(t, requests).Body)
}

func TestResponsesReplaysToolCallNamespaceOnlyWithCompatibleContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		additionalTools    bool
		messageProvider    sigma.ProviderID
		messageAPI         sigma.API
		messageModel       sigma.ModelID
		withLoadMarker     bool
		namespace          string
		wantNamespace      bool
		wantAdditionalItem bool
	}{
		{name: "same model", messageModel: "target", namespace: "dynamic", wantNamespace: true},
		{name: "compatible deferred tool", additionalTools: true, messageModel: "previous", withLoadMarker: true, namespace: "dynamic", wantNamespace: true, wantAdditionalItem: true},
		{name: "unsupported target", messageModel: "previous", withLoadMarker: true, namespace: "dynamic"},
		{name: "different api", additionalTools: true, messageAPI: sigma.APIOpenAICompletions, messageModel: "previous", withLoadMarker: true, namespace: "dynamic", wantAdditionalItem: true},
		{name: "different provider", additionalTools: true, messageProvider: "other", messageModel: "previous", withLoadMarker: true, namespace: "dynamic", wantAdditionalItem: true},
		{name: "ordinary call", messageModel: "target"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-namespace-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := responsesTestModel(providerID)
			model.ID = "target"
			if tt.additionalTools {
				model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{SupportsAdditionalTools: true}
			}
			messageProvider := tt.messageProvider
			if messageProvider == "" {
				messageProvider = providerID
			}
			messageAPI := tt.messageAPI
			if messageAPI == "" {
				messageAPI = sigma.APIOpenAIResponses
			}
			lateCall := sigma.ToolCallBlock("call_late", "late", map[string]any{})
			if tt.namespace != "" {
				lateCall.ProviderMetadata = map[string]any{"namespace": tt.namespace}
			}
			addedToolNames := []string(nil)
			if tt.withLoadMarker {
				addedToolNames = []string{"late"}
			}
			request := sigma.Request{
				Tools: []sigma.Tool{
					{Name: "base", InputSchema: sigma.Schema{"type": "object"}},
					{Name: "late", InputSchema: sigma.Schema{"type": "object"}},
				},
				Messages: []sigma.Message{
					{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_base", "base", map[string]any{})}},
					{Role: sigma.RoleTool, ToolCallID: "call_base", Content: []sigma.ContentBlock{sigma.Text("base result")}, AddedToolNames: addedToolNames},
					{Role: sigma.RoleAssistant, Provider: messageProvider, API: messageAPI, Model: tt.messageModel, Content: []sigma.ContentBlock{lateCall}},
					{Role: sigma.RoleTool, ToolCallID: "call_late", Content: []sigma.ContentBlock{sigma.Text("late result")}},
				},
			}
			client := responsesTestClient(t, providerID, model, server.URL)
			if _, err := client.Complete(context.Background(), model, request); err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
			var replayed map[string]any
			for _, item := range payload["input"].([]any) {
				typed := item.(map[string]any)
				if typed["type"] == "function_call" && typed["name"] == "late" {
					replayed = typed
					break
				}
			}
			if replayed == nil {
				t.Fatalf("payload omitted replayed late tool call: %#v", payload["input"])
			}
			gotNamespace, hasNamespace := replayed["namespace"]
			if tt.wantNamespace {
				if !hasNamespace || gotNamespace != tt.namespace {
					t.Fatalf("namespace = %v, present %v; want %q", gotNamespace, hasNamespace, tt.namespace)
				}
			} else if hasNamespace {
				t.Fatalf("namespace = %v, want absent", gotNamespace)
			}
			if got := hasResponsesItemType(payload, "additional_tools"); got != tt.wantAdditionalItem {
				t.Fatalf("additional_tools present = %v, want %v", got, tt.wantAdditionalItem)
			}
		})
	}
}

func TestResponsesKeepsDeferredToolMarkersEagerWhenUnsupported(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-no-deferred-tools")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, deferredToolsRequest())
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	assertDeferredToolsRemainEager(t, receiveRequest(t, requests).Body)
}

func assertDeferredToolsRemainEager(t *testing.T, body []byte) {
	t.Helper()

	payload := decodeResponsesPayload(t, body)
	tools := payload["tools"].([]any)
	if got, want := len(tools), 3; got != want {
		t.Fatalf("root tools = %#v, want %d eager tools", tools, want)
	}
	if hasResponsesItemType(payload, "tool_search_call") || hasResponsesItemType(payload, "tool_search_output") {
		t.Fatalf("unsupported payload included deferred tool records: %#v", payload["input"])
	}
}

func TestResponsesNormalizesProviderTextInPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-normalized-payload-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	invalid := invalidProviderText()

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{
			SystemPrompt: "system" + invalid,
			Messages: []sigma.Message{
				{Role: sigma.RoleDeveloper, Content: []sigma.ContentBlock{sigma.Text("developer" + invalid)}},
				sigma.UserText("user" + invalid),
				{
					Role: sigma.RoleAssistant,
					Content: []sigma.ContentBlock{
						sigma.Text("assistant" + invalid),
						sigma.Thinking("thinking"+invalid, ""),
						sigma.ToolCallBlock("call_invalid", "lookup", map[string]any{"query": "weather"}),
					},
				},
				sigma.ToolResult("call_invalid", "tool"+invalid),
			},
			Tools: []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}},
		},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Instructions string           `json:"instructions"`
		Input        []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := payload.Instructions, "systemclean"; got != want {
		t.Fatalf("instructions = %q, want %q", got, want)
	}
	assertResponsesInputText(t, payload.Input[0], "developerclean")
	assertResponsesInputText(t, payload.Input[1], "userclean")
	assertResponsesOutputText(t, payload.Input[2], "assistantclean")
	assertResponsesReasoningText(t, payload.Input[3], "thinkingclean")
	assertResponsesToolOutputText(t, payload.Input[5], "toolclean")
}

func TestResponsesSynthesizesUnansweredToolCallsBeforeUserTurn(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-drop-unanswered-tool-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_abandoned", "lookup", map[string]any{"query": "weather"}),
				},
			},
			sigma.UserText("Skip the lookup."),
		}},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	if got, want := len(payload.Input), 3; got != want {
		t.Fatalf("input count = %d, want %d", got, want)
	}
	if got, want := payload.Input[0]["type"], "function_call"; got != want {
		t.Fatalf("function call type = %v, want %q", got, want)
	}
	if got, want := payload.Input[1]["type"], "function_call_output"; got != want {
		t.Fatalf("synthetic type = %v, want %q", got, want)
	}
	if got, want := payload.Input[1]["call_id"], "call_abandoned"; got != want {
		t.Fatalf("synthetic call id = %v, want %q", got, want)
	}
	if got, want := payload.Input[1]["output"], "No result provided"; got != want {
		t.Fatalf("synthetic output = %v, want %q", got, want)
	}
	if got, want := payload.Input[2]["role"], "user"; got != want {
		t.Fatalf("user role = %v, want %q", got, want)
	}
}

func TestResponsesFiltersFailedAssistantTurnsBeforeReplay(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-failed-turn-replay-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	request := failedResponsesReplayRequest()
	before, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal request before Complete returned error: %v", err)
	}

	if _, err := client.Complete(context.Background(), model, request); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	after, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal request after Complete returned error: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("request changed during Complete:\nbefore %s\nafter  %s", before, after)
	}
	assertFailedResponsesReplayFiltered(t, receiveRequest(t, requests).Body)
}

func TestResponsesUsesPlaceholderForEmptyToolResult(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-empty-tool-output-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{
			sigma.UserText("Run the command."),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_empty", "shell", map[string]any{"command": "true"}),
				},
			},
			sigma.ToolResult("call_empty", ""),
		}},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	for _, item := range payload.Input {
		if item["type"] != "function_call_output" {
			continue
		}
		if got, want := item["output"], "(no tool output)"; got != want {
			t.Fatalf("tool output = %v, want %q", got, want)
		}
		return
	}
	t.Fatal("function_call_output was not sent")
}

func TestResponsesStreamingNormalizesInvalidUTF8(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(append([]byte(`data: {"type":"response.output_text.delta","response_id":"resp_bad","output_index":0,"item_id":"msg_bad","delta":"bad`), append([]byte{0xff}, []byte(`text"}`+"\n\n")...)...))
		_, _ = w.Write(append([]byte(`data: {"type":"response.completed","response":{"id":"resp_bad","model":"gpt-test","status":"completed","output":[{"type":"message","id":"msg_bad","role":"assistant","content":[{"type":"output_text","id":"text_bad","text":"bad`), append([]byte{0xff}, []byte(`text"}]}]}}`+"\n\n")...)...))
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-normalized-stream-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "badtext"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}
}

func TestResponsesCopilotDynamicHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		request     sigma.Request
		options     []sigma.Option
		wantHeaders map[string]string
		wantAbsent  []string
	}{
		{
			name:    "user initiated",
			request: sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			wantHeaders: map[string]string{
				"X-Initiator":   "user",
				"Openai-Intent": "conversation-edits",
			},
			wantAbsent: []string{"Copilot-Vision-Request"},
		},
		{
			name: "agent initiated with images",
			request: sigma.Request{Messages: []sigma.Message{
				sigma.UserText("inspect"),
				{Role: sigma.RoleTool, ToolCallID: "call_1", Content: []sigma.ContentBlock{sigma.ImageBase64("image/png", "aGk=")}},
			}},
			wantHeaders: map[string]string{
				"X-Initiator":            "agent",
				"Openai-Intent":          "conversation-edits",
				"Copilot-Vision-Request": "true",
			},
		},
		{
			name:    "caller override",
			request: sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			options: []sigma.Option{
				sigma.WithHeader("X-Initiator", "override"),
				sigma.WithHeader("Openai-Intent", "override-intent"),
				sigma.WithHeader("Copilot-Vision-Request", "override-vision"),
			},
			wantHeaders: map[string]string{
				"X-Initiator":            "override",
				"Openai-Intent":          "override-intent",
				"Copilot-Vision-Request": "override-vision",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			model := responsesTestModel(sigma.ProviderGitHubCopilot)
			client := responsesTestClient(t, sigma.ProviderGitHubCopilot, model, server.URL)

			_, err := client.Complete(context.Background(), model, tt.request, tt.options...)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			headers := receiveRequest(t, requests).Headers
			for key, value := range tt.wantHeaders {
				assertHeader(t, headers, key, value)
			}
			for _, key := range tt.wantAbsent {
				assertHeaderAbsent(t, headers, key)
			}
		})
	}
}

func TestResponsesCloudflareGatewayBaseURLAndAuthHeader(t *testing.T) {
	t.Setenv("CLOUDFLARE_GATEWAY_ID", "openai")

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("cloudflare-ai-gateway")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL+"/{CLOUDFLARE_GATEWAY_ID}")

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithHeader("cf-aig-authorization", "Bearer override"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/openai/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "cf-aig-authorization", "Bearer override")
	assertHeaderAbsent(t, request.Headers, "Authorization")
}

func TestResponsesSessionAffinityHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-affinity-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID("responses-session"),
		sigma.WithCacheRetention(sigma.CacheRetentionShort),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	headers := receiveRequest(t, requests).Headers
	assertHeader(t, headers, "session_id", "responses-session")
	assertHeader(t, headers, "x-client-request-id", "responses-session")
}

func TestResponsesOmitsSessionIDForNoSessionAffinityFormat(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-no-session-affinity-test")
	model := responsesTestModel(providerID)
	model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{
		SessionAffinityFormat: sigma.OpenAIResponsesSessionAffinityOpenAINoSession,
	}
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID("responses-session"),
		sigma.WithCacheRetention(sigma.CacheRetentionShort),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	headers := receiveRequest(t, requests).Headers
	assertHeaderAbsent(t, headers, "session_id")
	assertHeader(t, headers, "x-client-request-id", "responses-session")
}

func TestResponsesSessionAffinityOverridesNoSessionFormat(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-no-session-affinity-override-test")
	model := responsesTestModel(providerID)
	model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{
		SessionAffinityFormat: sigma.OpenAIResponsesSessionAffinityOpenAINoSession,
	}
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID("responses-session"),
		sigma.WithCacheRetention(sigma.CacheRetentionShort),
		sigma.WithProviderOption(providerID, "session_id_header", "X-Session-ID"),
		sigma.WithHeader("session_id", "caller-session"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	headers := receiveRequest(t, requests).Headers
	assertHeader(t, headers, "X-Session-ID", "responses-session")
	assertHeader(t, headers, "session_id", "caller-session")
	assertHeaderAbsent(t, headers, "x-client-request-id")
}

func TestResponsesOmitsSessionAffinityHeadersWhenCacheDisabled(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-affinity-disabled-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithSessionID("responses-session"),
		sigma.WithCacheRetention(sigma.CacheRetentionNone),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	headers := receiveRequest(t, requests).Headers
	assertHeaderAbsent(t, headers, "session_id")
	assertHeaderAbsent(t, headers, "x-client-request-id")
}

func TestResponsesSendsTypedResponseFormatAsTextFormat(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-format-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("judge")}},
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{
			ResponseFormat: map[string]any{
				"type": "json_schema",
				"json_schema": map[string]any{
					"name":   "judge",
					"strict": true,
					"schema": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
					},
				},
			},
			TextVerbosity: "low",
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	text, ok := payload["text"].(map[string]any)
	if !ok {
		t.Fatalf("text type = %T, want map", payload["text"])
	}
	if got, want := text["verbosity"], "low"; got != want {
		t.Fatalf("text.verbosity = %v, want %q", got, want)
	}
	format, ok := text["format"].(map[string]any)
	if !ok {
		t.Fatalf("text.format type = %T, want map", text["format"])
	}
	if got, want := format["type"], "json_schema"; got != want {
		t.Fatalf("text.format.type = %v, want %q", got, want)
	}
	if got, want := format["name"], "judge"; got != want {
		t.Fatalf("text.format.name = %v, want %q", got, want)
	}
	if got, want := format["strict"], true; got != want {
		t.Fatalf("text.format.strict = %v, want %v", got, want)
	}
	if _, ok := format["json_schema"]; ok {
		t.Fatalf("text.format contains unflattened json_schema: %#v", format)
	}
	wantSchema := map[string]any{"type": "object", "additionalProperties": false}
	if !reflect.DeepEqual(format["schema"], wantSchema) {
		t.Fatalf("text.format.schema = %#v, want %#v", format["schema"], wantSchema)
	}
}

func TestResponsesNormalizesOpenAIOptionsFunctionToolChoice(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-tool-choice-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("call a tool")}},
		sigma.WithOpenAIOptions(sigma.OpenAIOptions{ToolChoice: map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "read_file"},
		}}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	assertResponsesFunctionToolChoice(t, receiveRequest(t, requests).Body)
}

func TestResponsesNormalizesProviderOptionFunctionToolChoice(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-provider-tool-choice-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("call a tool")}},
		sigma.WithToolChoice(sigma.ToolChoiceNone),
		sigma.WithProviderOption(providerID, "tool_choice", map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "read_file"},
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	assertResponsesFunctionToolChoice(t, receiveRequest(t, requests).Body)
}

func TestResponsesUsesModelBaseURLAndHeaders(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-model-metadata-test")
	model := responsesTestModel(providerID)
	model.ProviderMetadata = map[string]any{
		"baseURL": server.URL + "/model-base",
		"headers": map[string]string{
			"Authorization": "Bearer metadata-secret",
			"X-Model":       "model",
		},
	}
	client := responsesTestClient(t, providerID, model, "https://provider-base.invalid")

	if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/model-base/responses"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer resolved-key")
	assertHeader(t, request.Headers, "X-Model", "model")
}

func TestResponsesReplayNormalizesMissingAndForeignIDs(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-replay-id-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	foreignItemID := strings.Repeat("foreign/item+", 8)
	toolCallID := "call_foreign|" + foreignItemID

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("continue"),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.Text("first"),
					sigma.Text("second"),
					sigma.Thinking("prior reasoning", ""),
					sigma.ToolCallBlock(toolCallID, "lookup", map[string]any{"query": "weather"}),
				},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: toolCallID,
				Content: []sigma.ContentBlock{
					sigma.Text("A red circle."),
					sigma.ImageBase64("image/png", "aGk="),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var messageID, textPartID, reasoningID, functionItemID, functionCallID string
	var toolOutput []any
	for _, item := range payload.Input {
		switch item["type"] {
		case "message":
			messageID, _ = item["id"].(string)
			content, _ := item["content"].([]any)
			if len(content) > 0 {
				part, _ := content[0].(map[string]any)
				textPartID, _ = part["id"].(string)
			}
		case "reasoning":
			reasoningID, _ = item["id"].(string)
		case "function_call":
			functionItemID, _ = item["id"].(string)
			functionCallID, _ = item["call_id"].(string)
		case "function_call_output":
			toolOutput, _ = item["output"].([]any)
		}
	}

	assertResponsesID(t, messageID, "msg_")
	assertResponsesID(t, textPartID, "text_")
	assertResponsesID(t, reasoningID, "rs_")
	assertResponsesID(t, functionItemID, "fc_")
	if got, want := functionCallID, "call_foreign"; got != want {
		t.Fatalf("function call_id = %q, want %q", got, want)
	}
	if len(toolOutput) != 2 {
		t.Fatalf("tool output parts = %d, want 2", len(toolOutput))
	}
	firstOutput, _ := toolOutput[0].(map[string]any)
	if got, want := firstOutput["type"], "input_text"; got != want {
		t.Fatalf("first tool output type = %v, want %v", got, want)
	}
	secondOutput, _ := toolOutput[1].(map[string]any)
	if got, want := secondOutput["type"], "input_image"; got != want {
		t.Fatalf("second tool output type = %v, want %v", got, want)
	}
}

func TestResponsesSendsDocumentContentBlocks(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-document-input-test")
	model := responsesTestModel(providerID)
	model.SupportedInputs = []sigma.ContentBlockType{
		sigma.ContentBlockText,
		sigma.ContentBlockImage,
		sigma.ContentBlockDocument,
	}
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserContent(
				sigma.Text("Review these documents."),
				sigma.DocumentBase64("application/pdf", "inline.pdf", "JVBERi0xLjQ="),
				sigma.DocumentURL("application/pdf", "remote.pdf", "https://example.test/remote.pdf"),
				sigma.DocumentFileID("application/pdf", "uploaded.pdf", "file_123"),
			),
			{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_docs", "lookup", map[string]any{"path": "report.pdf"})},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call_docs",
				Content: []sigma.ContentBlock{
					sigma.Text("Found the requested report."),
					sigma.DocumentFileID("application/pdf", "report.pdf", "file_report"),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	userContent := payload.Input[0]["content"].([]any)
	if got, want := len(userContent), 4; got != want {
		t.Fatalf("user content parts = %d, want %d", got, want)
	}
	inline := userContent[1].(map[string]any)
	if got, want := inline["type"], "input_file"; got != want {
		t.Fatalf("inline document type = %v, want %q", got, want)
	}
	if got, want := inline["file_data"], "data:application/pdf;base64,JVBERi0xLjQ="; got != want {
		t.Fatalf("inline file_data = %v, want %q", got, want)
	}
	if got, want := userContent[2].(map[string]any)["file_url"], "https://example.test/remote.pdf"; got != want {
		t.Fatalf("remote file_url = %v, want %q", got, want)
	}
	if got, want := userContent[3].(map[string]any)["file_id"], "file_123"; got != want {
		t.Fatalf("uploaded file_id = %v, want %q", got, want)
	}

	var toolOutput []any
	for _, item := range payload.Input {
		if item["type"] == "function_call_output" {
			toolOutput, _ = item["output"].([]any)
		}
	}
	if got, want := len(toolOutput), 2; got != want {
		t.Fatalf("tool output parts = %d, want %d", got, want)
	}
	toolDocument := toolOutput[1].(map[string]any)
	if got, want := toolDocument["type"], "input_file"; got != want {
		t.Fatalf("tool document type = %v, want %q", got, want)
	}
	if got, want := toolDocument["file_id"], "file_report"; got != want {
		t.Fatalf("tool document file_id = %v, want %q", got, want)
	}
}

func TestResponsesRejectsDocumentWithoutModelCapability(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server received request despite unsupported document input")
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-document-unsupported-test")
	model := responsesTestModel(providerID)
	model.SupportedInputs = []sigma.ContentBlockType{sigma.ContentBlockText}
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{
		sigma.UserContent(sigma.DocumentBase64("application/pdf", "inline.pdf", "JVBERi0xLjQ=")),
	}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorUnsupported {
		t.Fatalf("error = %v, want unsupported sigma error", err)
	}
}

func TestResponsesReplayGeneratesDistinctMessageIDsAroundReasoning(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-replay-split-message-id-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{
			sigma.UserText("continue"),
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.Text("visible first"),
					sigma.Thinking("private reasoning", ""),
					sigma.Text("visible second"),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var messageIDs []string
	var contentIDs []string
	for _, item := range payload.Input {
		if item["type"] != "message" || item["role"] != "assistant" {
			continue
		}
		id, _ := item["id"].(string)
		messageIDs = append(messageIDs, id)
		content, _ := item["content"].([]any)
		if len(content) != 1 {
			t.Fatalf("message content = %#v, want one text part", content)
		}
		part, _ := content[0].(map[string]any)
		contentID, _ := part["id"].(string)
		contentIDs = append(contentIDs, contentID)
	}
	if got, want := len(messageIDs), 2; got != want {
		t.Fatalf("assistant message items = %d, want %d: %#v", got, want, payload.Input)
	}
	if messageIDs[0] == messageIDs[1] {
		t.Fatalf("assistant message IDs are not distinct: %v", messageIDs)
	}
	for _, id := range messageIDs {
		assertResponsesID(t, id, "msg_")
	}
	for _, id := range contentIDs {
		assertResponsesID(t, id, "text_")
	}
}

func assertResponsesID(t *testing.T, id string, prefix string) {
	t.Helper()
	if !strings.HasPrefix(id, prefix) {
		t.Fatalf("id %q does not have prefix %q", id, prefix)
	}
	if len(id) > 64 {
		t.Fatalf("id %q length = %d, want <= 64", id, len(id))
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			t.Fatalf("id %q contains invalid rune %q", id, r)
		}
	}
}

func TestResponsesCompleteSendsProviderDefinedToolsPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-provider-tools-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("Search current docs.")},
		Tools: []sigma.Tool{
			{
				Name:        "lookup",
				Description: "Lookup local records",
				InputSchema: sigma.Schema{
					"type":       "object",
					"properties": map[string]any{"query": map[string]any{"type": "string"}},
					"required":   []any{"query"},
				},
			},
			openai.Tools.WebSearch(
				openai.WithSearchContextSize("low"),
				openai.WithSearchFilters(openai.WebSearchFilters{AllowedDomains: []string{"example.com"}}),
			),
		},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/openai/responses/provider_defined_tools_payload.json")
}

func TestResponsesGrammarToolsFollowMetadataAndExplicitOverrides(t *testing.T) {
	t.Parallel()

	enabled := true
	disabled := false
	tests := []struct {
		name       string
		syntax     sigma.OpenAIGrammarSyntax
		definition string
		compat     bool
		enabled    *bool
		wantCustom bool
	}{
		{
			name:       "catalog capability enables lark",
			syntax:     sigma.OpenAIGrammarLark,
			definition: "start: WORD\nWORD: /[a-z]+/",
			compat:     true,
			wantCustom: true,
		},
		{
			name:       "explicit enablement enables regex",
			syntax:     sigma.OpenAIGrammarRegex,
			definition: "[a-z]+",
			enabled:    &enabled,
			wantCustom: true,
		},
		{
			name:       "explicit disablement keeps function tool",
			syntax:     sigma.OpenAIGrammarLark,
			definition: "start: WORD\nWORD: /[a-z]+/",
			compat:     true,
			enabled:    &disabled,
			wantCustom: false,
		},
		{
			name:       "unsupported metadata keeps function tool",
			syntax:     sigma.OpenAIGrammarRegex,
			definition: "[a-z]+",
			wantCustom: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-grammar-tools-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := responsesTestModel(providerID)
			if tt.compat {
				model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{SupportsGrammarTools: true}
			}
			client := responsesTestClient(t, providerID, model, server.URL)
			options := []sigma.Option{}
			if tt.enabled != nil {
				options = append(options, sigma.WithOpenAIOptions(sigma.OpenAIOptions{EnableGrammarTools: tt.enabled}))
			}

			_, err := client.Complete(context.Background(), model, sigma.Request{
				Messages: []sigma.Message{sigma.UserText("parse this")},
				Tools:    []sigma.Tool{responsesGrammarTool("parse", tt.syntax, tt.definition)},
			}, options...)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			tools := decodeResponsesPayload(t, receiveRequest(t, requests).Body)["tools"].([]any)
			tool := tools[0].(map[string]any)
			if tt.wantCustom {
				if got, want := tool["type"], "custom"; got != want {
					t.Fatalf("tool type = %v, want %q", got, want)
				}
				format := tool["format"].(map[string]any)
				if got, want := format["type"], "grammar"; got != want {
					t.Fatalf("format type = %v, want %q", got, want)
				}
				if got, want := format["syntax"], string(tt.syntax); got != want {
					t.Fatalf("format syntax = %v, want %q", got, want)
				}
				if got, want := format["definition"], tt.definition; got != want {
					t.Fatalf("format definition = %v, want %q", got, want)
				}
				return
			}
			if got, want := tool["type"], "function"; got != want {
				t.Fatalf("tool type = %v, want %q", got, want)
			}
			if _, ok := tool["format"]; ok {
				t.Fatalf("fallback function tool included grammar format: %#v", tool)
			}
		})
	}
}

func TestResponsesGrammarToolsValidateConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool sigma.Tool
		want string
	}{
		{
			name: "syntax",
			tool: responsesGrammarTool("parse", "ebnf", "start: WORD"),
			want: "syntax must be lark or regex",
		},
		{
			name: "definition",
			tool: responsesGrammarTool("parse", sigma.OpenAIGrammarLark, " "),
			want: "definition is required",
		},
		{
			name: "required schema",
			tool: sigma.Tool{
				Name:        "parse",
				InputSchema: sigma.Schema{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}},
				OpenAIGrammar: &sigma.OpenAIGrammar{
					Syntax:     sigma.OpenAIGrammarRegex,
					Definition: ".+",
				},
			},
			want: "must require exactly one string property",
		},
		{
			name: "property schema",
			tool: sigma.Tool{
				Name: "parse",
				InputSchema: sigma.Schema{
					"type":       "object",
					"properties": map[string]any{"command": map[string]any{"type": "integer"}},
					"required":   []any{"command"},
				},
				OpenAIGrammar: &sigma.OpenAIGrammar{
					Syntax:     sigma.OpenAIGrammarRegex,
					Definition: ".+",
				},
			},
			want: "must require exactly one string property",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			providerID := sigma.ProviderID("responses-grammar-validation-" + tt.name)
			model := responsesTestModel(providerID)
			model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{SupportsGrammarTools: true}
			client := responsesTestClient(t, providerID, model, "https://example.test")
			_, err := client.Complete(context.Background(), model, sigma.Request{Tools: []sigma.Tool{tt.tool}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Complete error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestResponsesGrammarToolsReplayAndDeferredLoading(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-grammar-replay")
	model := responsesTestModel(providerID)
	model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{
		SupportsGrammarTools:    true,
		SupportsAdditionalTools: true,
	}
	client := responsesTestClient(t, providerID, model, server.URL)
	parseCall := sigma.ToolCallBlock("call_parse", "parse", map[string]any{"command": "go test"})
	parseCall.ProviderMetadata = map[string]any{"id": "ctc_parse", "namespace": "dynamic"}
	request := sigma.Request{
		Tools: []sigma.Tool{
			{Name: "base", InputSchema: sigma.Schema{"type": "object"}},
			responsesGrammarTool("parse", sigma.OpenAIGrammarRegex, ".+"),
			responsesGrammarTool("late", sigma.OpenAIGrammarLark, "start: WORD\nWORD: /[a-z]+/"),
		},
		Messages: []sigma.Message{
			sigma.UserText("start"),
			{
				Role:     sigma.RoleAssistant,
				Provider: providerID,
				API:      sigma.APIOpenAIResponses,
				Model:    model.ID,
				Content:  []sigma.ContentBlock{parseCall},
			},
			{
				Role:           sigma.RoleTool,
				ToolCallID:     "call_parse",
				Content:        []sigma.ContentBlock{sigma.Text("ok")},
				AddedToolNames: []string{"late"},
			},
		},
	}

	if _, err := client.Complete(context.Background(), model, request); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	payload := decodeResponsesPayload(t, receiveRequest(t, requests).Body)
	var sawCustomCall, sawCustomOutput, sawDeferredTool bool
	for _, item := range payload["input"].([]any) {
		typed := item.(map[string]any)
		switch typed["type"] {
		case "custom_tool_call":
			sawCustomCall = true
			if got, want := typed["input"], "go test"; got != want {
				t.Fatalf("custom tool input = %v, want %q", got, want)
			}
			if got, want := typed["id"], "ctc_parse"; got != want {
				t.Fatalf("custom tool item id = %v, want %q", got, want)
			}
			if got, want := typed["namespace"], "dynamic"; got != want {
				t.Fatalf("custom tool namespace = %v, want %q", got, want)
			}
		case "custom_tool_call_output":
			sawCustomOutput = true
			if got, want := typed["output"], "ok"; got != want {
				t.Fatalf("custom tool output = %v, want %q", got, want)
			}
		case "additional_tools":
			sawDeferredTool = true
			tools := typed["tools"].([]any)
			if got, want := tools[0].(map[string]any)["type"], "custom"; got != want {
				t.Fatalf("deferred tool type = %v, want %q", got, want)
			}
			if _, ok := tools[0].(map[string]any)["defer_loading"]; ok {
				t.Fatalf("additional grammar tool retained defer_loading: %#v", tools[0])
			}
		}
	}
	if !sawCustomCall || !sawCustomOutput || !sawDeferredTool {
		t.Fatalf("replay input omitted custom items: %#v", payload["input"])
	}
}

func TestResponsesGrammarToolStreamingBuildsObjectArguments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.output_item.added","response_id":"resp_grammar","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"parse","input":""}}

data: {"type":"response.custom_tool_call_input.delta","response_id":"resp_grammar","output_index":0,"delta":"go "}

data: {"type":"response.custom_tool_call_input.delta","response_id":"resp_grammar","output_index":0,"delta":"test"}

data: {"type":"response.custom_tool_call_input.done","response_id":"resp_grammar","output_index":0,"input":"go test"}

data: {"type":"response.output_item.done","response_id":"resp_grammar","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"parse","namespace":"dynamic","input":"go test"}}

data: {"type":"response.completed","response":{"id":"resp_grammar","status":"completed","output":[{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"parse","input":"go test"}]}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-grammar-stream")
	model := responsesTestModel(providerID)
	model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{SupportsGrammarTools: true}
	client := responsesTestClient(t, providerID, model, server.URL)
	stream := client.Stream(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("run tests")},
		Tools:    []sigma.Tool{responsesGrammarTool("parse", sigma.OpenAIGrammarRegex, ".+")},
	})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}
	if got, want := final.Content[0].ToolArguments.(map[string]any)["command"], "go test"; got != want {
		t.Fatalf("tool arguments = %v, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderMetadata["id"], "ctc_1"; got != want {
		t.Fatalf("tool item id = %v, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderMetadata["call_id"], "call_1"; got != want {
		t.Fatalf("tool call id metadata = %v, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderMetadata["namespace"], "dynamic"; got != want {
		t.Fatalf("tool namespace = %v, want %q", got, want)
	}
	if got, want := eventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindToolCallStart,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
}

func TestResponsesGrammarToolStreamingRejectsConflictingInput(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"parse","input":"first"}}

data: {"type":"response.custom_tool_call_input.done","output_index":0,"input":"second"}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-grammar-conflict")
	model := responsesTestModel(providerID)
	model.OpenAIResponsesCompat = &sigma.OpenAIResponsesCompat{SupportsGrammarTools: true}
	client := responsesTestClient(t, providerID, model, server.URL)
	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("run")},
		Tools:    []sigma.Tool{responsesGrammarTool("parse", sigma.OpenAIGrammarRegex, ".+")},
	})
	if err == nil || !strings.Contains(err.Error(), "not monotonic") {
		t.Fatalf("Complete error = %v, want non-monotonic custom input", err)
	}
}

func TestResponsesPreservesAndReplaysAssistantPhases(t *testing.T) {
	t.Parallel()

	phaseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w, responsesAssistantPhasesEvent)
	}))
	t.Cleanup(phaseServer.Close)

	providerID := sigma.ProviderID("responses-phase-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, phaseServer.URL)
	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.StopReason, sigma.StopReasonEndTurn; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := len(final.Content), 2; got != want {
		t.Fatalf("content blocks = %d, want %d", got, want)
	}
	for i, want := range []struct {
		text      string
		itemID    string
		contentID string
		phase     string
	}{
		{text: "Checking constraints.", itemID: "msg_commentary", contentID: "text_commentary", phase: "commentary"},
		{text: "The answer.", itemID: "msg_answer", contentID: "text_answer", phase: "final_answer"},
	} {
		block := final.Content[i]
		if got := block.Text; got != want.text {
			t.Fatalf("content %d text = %q, want %q", i, got, want.text)
		}
		if got := block.ProviderMetadata["id"]; got != want.itemID {
			t.Fatalf("content %d item id = %v, want %q", i, got, want.itemID)
		}
		if got := block.ProviderMetadata["content_id"]; got != want.contentID {
			t.Fatalf("content %d content id = %v, want %q", i, got, want.contentID)
		}
		if got := block.ProviderMetadata["phase"]; got != want.phase {
			t.Fatalf("content %d phase = %v, want %q", i, got, want.phase)
		}
	}

	persisted, err := sigma.MarshalRequest(sigma.Request{Messages: []sigma.Message{
		sigma.UserText("hi"),
		{
			Role:       sigma.RoleAssistant,
			Content:    final.Content,
			Provider:   final.Provider,
			API:        model.API,
			Model:      final.Model,
			StopReason: final.StopReason,
		},
	}})
	if err != nil {
		t.Fatalf("MarshalRequest returned error: %v", err)
	}
	restored, err := sigma.UnmarshalRequest(persisted)
	if err != nil {
		t.Fatalf("UnmarshalRequest returned error: %v", err)
	}
	for i, phase := range []string{"commentary", "final_answer"} {
		if got := restored.Messages[1].Content[i].ProviderMetadata["phase"]; got != phase {
			t.Fatalf("restored content %d phase = %v, want %q", i, got, phase)
		}
	}
	restored.Messages = append(restored.Messages, sigma.UserText("continue"))

	requests := make(chan capturedRequest, 1)
	replayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(replayServer.Close)
	replayClient := responsesTestClient(t, providerID, model, replayServer.URL)
	if _, err := replayClient.Complete(context.Background(), model, restored); err != nil {
		t.Fatalf("replay Complete returned error: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("decode replay payload: %v", err)
	}
	var assistantItems []map[string]any
	for _, item := range payload.Input {
		if item["type"] == "message" && item["role"] == "assistant" {
			assistantItems = append(assistantItems, item)
		}
	}
	if got, want := len(assistantItems), 2; got != want {
		t.Fatalf("replayed assistant message items = %d, want %d: %#v", got, want, assistantItems)
	}
	for i, want := range []struct {
		itemID string
		phase  string
		text   string
	}{
		{itemID: "msg_commentary", phase: "commentary", text: "Checking constraints."},
		{itemID: "msg_answer", phase: "final_answer", text: "The answer."},
	} {
		item := assistantItems[i]
		if got := item["id"]; got != want.itemID {
			t.Fatalf("replayed item %d id = %v, want %q", i, got, want.itemID)
		}
		if got := item["phase"]; got != want.phase {
			t.Fatalf("replayed item %d phase = %v, want %q", i, got, want.phase)
		}
		content := item["content"].([]any)
		if got := content[0].(map[string]any)["text"]; got != want.text {
			t.Fatalf("replayed item %d text = %v, want %q", i, got, want.text)
		}
	}
}

func TestResponsesReplayOmitsUntrustedAssistantPhases(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-phase-omission-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	tests := []struct {
		name     string
		phase    string
		provider sigma.ProviderID
		api      sigma.API
		model    sigma.ModelID
	}{
		{name: "missing", provider: providerID, api: model.API, model: model.ID},
		{name: "unknown", phase: "analysis", provider: providerID, api: model.API, model: model.ID},
		{name: "foreign-provider", phase: "commentary", provider: "other", api: model.API, model: model.ID},
		{name: "different-api", phase: "commentary", provider: providerID, api: sigma.APIOpenAICompletions, model: model.ID},
		{name: "different-model", phase: "final_answer", provider: providerID, api: model.API, model: "other"},
	}
	request := sigma.Request{Messages: []sigma.Message{sigma.UserText("start")}}
	for _, tt := range tests {
		block := sigma.Text(tt.name)
		block.ProviderMetadata = map[string]any{"id": "msg_" + strings.ReplaceAll(tt.name, "-", "_")}
		if tt.phase != "" {
			block.ProviderMetadata["phase"] = tt.phase
		}
		request.Messages = append(request.Messages, sigma.Message{
			Role:     sigma.RoleAssistant,
			Content:  []sigma.ContentBlock{block},
			Provider: tt.provider,
			API:      tt.api,
			Model:    tt.model,
		})
	}
	request.Messages = append(request.Messages, sigma.UserText("continue"))

	if _, err := client.Complete(context.Background(), model, request); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var assistantItems []map[string]any
	for _, item := range payload.Input {
		if item["type"] == "message" && item["role"] == "assistant" {
			assistantItems = append(assistantItems, item)
		}
	}
	if got, want := len(assistantItems), len(tests); got != want {
		t.Fatalf("assistant message items = %d, want %d", got, want)
	}
	for i, tt := range tests {
		if phase, ok := assistantItems[i]["phase"]; ok {
			t.Fatalf("%s replayed phase = %v, want absent", tt.name, phase)
		}
		metadata := request.Messages[i+1].Content[0].ProviderMetadata
		got, _ := metadata["phase"].(string)
		if got != tt.phase {
			t.Fatalf("%s stored phase = %q, want %q", tt.name, got, tt.phase)
		}
	}
}

func TestResponsesStreamingMapsTextReasoningUsageAndMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`event: response.created
data: {"type":"response.created","response":{"id":"resp_stream","model":"gpt-test-2026","status":"in_progress"}}

event: response.output_item.added
data: {"type":"response.output_item.added","response_id":"resp_stream","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[]}}

event: response.reasoning_summary_text.delta
data: {"type":"response.reasoning_summary_text.delta","response_id":"resp_stream","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"Checked "}

event: response.output_item.added
data: {"type":"response.output_item.added","response_id":"resp_stream","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","response_id":"resp_stream","item_id":"msg_1","output_index":1,"content_index":0,"delta":"Hello"}

event: response.output_text.delta
data: {"type":"response.output_text.delta","response_id":"resp_stream","item_id":"msg_1","output_index":1,"content_index":0,"delta":" world"}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_stream","model":"gpt-test-2026","status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Checked constraints.","signature":"think_sig"}],"encrypted_content":"enc_think"},{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","id":"text_1","text":"Hello world","signature":"text_sig"}]}],"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":3},"output_tokens":8,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":18}}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-stream-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}

	if got, want := eventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindThinkingStart,
		sigma.EventKindThinkingDelta,
		sigma.EventKindTextStart,
		sigma.EventKindTextDelta,
		sigma.EventKindTextDelta,
		sigma.EventKindThinkingEnd,
		sigma.EventKindTextEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.Content[0].ThinkingText, "Checked constraints."; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Signature, "think_sig"; got != want {
		t.Fatalf("thinking signature = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderSignature, "enc_think"; got != want {
		t.Fatalf("thinking provider signature = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "Hello world"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Signature, "text_sig"; got != want {
		t.Fatalf("text signature = %q, want %q", got, want)
	}
	if got, want := final.ProviderMetadata["id"], "resp_stream"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}
	if got, want := final.ProviderMetadata["model"], "gpt-test-2026"; got != want {
		t.Fatalf("provider model = %v, want %v", got, want)
	}
	if got, want := final.ProviderMetadata["status"], "completed"; got != want {
		t.Fatalf("response status = %v, want %v", got, want)
	}
	if final.Usage == nil {
		t.Fatal("final usage was nil")
	}
	if got, want := final.Usage.CacheReadInputTokens, 3; got != want {
		t.Fatalf("cache read tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.ThinkingTokens, 2; got != want {
		t.Fatalf("thinking tokens = %d, want %d", got, want)
	}
	if got, want := final.Usage.Provider, providerID; got != want {
		t.Fatalf("usage provider = %q, want %q", got, want)
	}
	if got, want := final.Usage.Model, model.ID; got != want {
		t.Fatalf("usage model = %q, want %q", got, want)
	}
	if got, want := final.Usage.Raw["input_tokens"], float64(10); got != want {
		t.Fatalf("raw input tokens = %v, want %v", got, want)
	}
	if events[len(events)-1].Usage == nil || events[len(events)-1].Usage.Raw["input_tokens"] != float64(10) {
		t.Fatalf("terminal usage = %#v, want raw input tokens", events[len(events)-1].Usage)
	}
}

func TestResponsesStreamingFinalizesOutputItemsAsTheyComplete(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.output_item.added","response_id":"resp_items","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[]}}

data: {"type":"response.reasoning_summary_text.delta","response_id":"resp_items","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"Checked"}

data: {"type":"response.reasoning_summary_part.done","response_id":"resp_items","item_id":"rs_1","output_index":0,"summary_index":0}

data: {"type":"response.reasoning_summary_text.delta","response_id":"resp_items","item_id":"rs_1","output_index":0,"summary_index":1,"delta":"constraints."}

data: {"type":"response.output_item.done","response_id":"resp_items","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Checked"},{"type":"summary_text","text":"constraints."}]}}

data: {"type":"response.output_item.added","response_id":"resp_items","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}

data: {"type":"response.output_text.delta","response_id":"resp_items","item_id":"msg_1","output_index":1,"content_index":0,"delta":"Hello"}

data: {"type":"response.output_item.done","response_id":"resp_items","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","id":"text_1","text":"Hello"}]}}

data: {"type":"response.output_item.added","response_id":"resp_items","output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":""}}

data: {"type":"response.output_item.done","response_id":"resp_items","output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"weather\"}"}}

data: {"type":"response.completed","response":{"id":"resp_items","status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Checked"},{"type":"summary_text","text":"constraints."}]},{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","id":"text_1","text":"Hello"}]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"weather\"}"}]}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-item-lifecycle-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}

	if got, want := eventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindThinkingStart,
		sigma.EventKindThinkingDelta,
		sigma.EventKindThinkingDelta,
		sigma.EventKindThinkingDelta,
		sigma.EventKindThinkingEnd,
		sigma.EventKindTextStart,
		sigma.EventKindTextDelta,
		sigma.EventKindTextEnd,
		sigma.EventKindToolCallStart,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.Content[0].ThinkingText, "Checked\n\nconstraints."; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "Hello"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := final.Content[2].ToolCallID, "call_1"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
}

func TestResponsesStreamingPreservesReasoningContentWithoutSummary(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.output_item.done","response_id":"resp_reasoning_content","output_index":0,"item":{"type":"reasoning","id":"rs_1","content":[{"type":"reasoning_text","text":"Checked private constraints."}]}}

data: {"type":"response.completed","response":{"id":"resp_reasoning_content","status":"completed","output":[{"type":"reasoning","id":"rs_1","content":[{"type":"reasoning_text","text":"Checked private constraints."}]}]}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-reasoning-content-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].ThinkingText, "Checked private constraints."; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
}

func TestResponsesStreamingParsesReasoningTextAndRefusal(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.output_item.added","response_id":"resp_refusal","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}

data: {"type":"response.reasoning_text.delta","response_id":"resp_refusal","item_id":"rs_1","output_index":0,"delta":"Check "}

data: {"type":"response.reasoning_text.delta","response_id":"resp_refusal","item_id":"rs_1","output_index":0,"delta":"policy."}

data: {"type":"response.output_item.added","response_id":"resp_refusal","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}

data: {"type":"response.content_part.added","response_id":"resp_refusal","item_id":"msg_1","output_index":1,"content_index":0,"part":{"type":"refusal","id":"refusal_1","refusal":""}}

data: {"type":"response.refusal.delta","response_id":"resp_refusal","item_id":"msg_1","output_index":1,"content_index":0,"delta":"I cannot"}

data: {"type":"response.refusal.delta","response_id":"resp_refusal","item_id":"msg_1","output_index":1,"content_index":0,"delta":" help."}

data: {"type":"response.completed","response":{"id":"resp_refusal","status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Check policy."}]},{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"refusal","id":"refusal_1","refusal":"I cannot help."}]}]}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-refusal-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].ThinkingText, "Check policy."; got != want {
		t.Fatalf("thinking = %q, want %q", got, want)
	}
	if got, want := final.Content[1].Text, "I cannot help."; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := final.Content[1].ProviderMetadata["content_id"], "refusal_1"; got != want {
		t.Fatalf("refusal content id = %v, want %q", got, want)
	}
}

func TestResponsesToolCallStreamingProducesFinalArguments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.output_item.added","response_id":"resp_tool","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":""}}

data: {"type":"response.function_call_arguments.delta","response_id":"resp_tool","item_id":"fc_1","output_index":0,"delta":"{\"city\""}

data: {"type":"response.function_call_arguments.delta","response_id":"resp_tool","item_id":"fc_1","output_index":0,"delta":":\"Melbourne\"}"}

data: {"type":"response.function_call_arguments.done","response_id":"resp_tool","item_id":"fc_1","output_index":0,"arguments":"{\"city\":\"Melbourne\"}"}

data: {"type":"response.output_item.done","response_id":"resp_tool","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","namespace":"dynamic","arguments":"{\"city\":\"Melbourne\"}"}}

data: {"type":"response.completed","response":{"id":"resp_tool","status":"completed","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{\"city\":\"Melbourne\"}"}]}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-tool-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("weather")}})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}

	if got, want := eventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindToolCallStart,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallDelta,
		sigma.EventKindToolCallEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if got, want := final.StopReason, sigma.StopReasonToolCalls; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ToolCallID, "call_1"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderMetadata["id"], "fc_1"; got != want {
		t.Fatalf("tool item id = %v, want %v", got, want)
	}
	if got, want := final.Content[0].ProviderMetadata["namespace"], "dynamic"; got != want {
		t.Fatalf("tool namespace = %v, want %q", got, want)
	}
	args := final.Content[0].ToolArguments.(map[string]any)
	if got, want := args["city"], "Melbourne"; got != want {
		t.Fatalf("tool city = %v, want %v", got, want)
	}
	for _, key := range []string{"arguments", "argumentsText"} {
		if _, ok := final.Content[0].ProviderMetadata[key]; ok {
			t.Fatalf("final tool metadata retained transient %q: %#v", key, final.Content[0].ProviderMetadata)
		}
	}

	var sawIncompleteArguments, sawCompletedArguments bool
	for _, event := range events {
		if event.Kind == sigma.EventKindToolCallEnd && event.ToolCall != nil {
			if got, want := event.ToolCall.ProviderMetadata["namespace"], "dynamic"; got != want {
				t.Fatalf("tool-call end namespace = %v, want %q", got, want)
			}
		}
		if event.Kind != sigma.EventKindToolCallDelta || event.PartialToolCall == nil {
			continue
		}
		metadata := event.PartialToolCall.ProviderMetadata
		switch metadata["argumentsText"] {
		case `{"city"`:
			sawIncompleteArguments = true
			if _, ok := metadata["arguments"]; ok {
				t.Fatalf("incomplete arguments exposed decoded metadata: %#v", metadata)
			}
		case `{"city":"Melbourne"}`:
			arguments, ok := metadata["arguments"].(map[string]any)
			if !ok {
				t.Fatalf("completed arguments = %#v, want decoded map", metadata["arguments"])
			}
			if got, want := arguments["city"], "Melbourne"; got != want {
				t.Fatalf("partial tool city = %v, want %v", got, want)
			}
			sawCompletedArguments = true
		}
	}
	if !sawIncompleteArguments {
		t.Fatal("tool-call deltas did not retain incomplete argument text")
	}
	if !sawCompletedArguments {
		t.Fatal("tool-call deltas did not expose decoded completed arguments")
	}
}

func TestResponsesAppliesServiceTierCostMultiplier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		modelID     sigma.ModelID
		serviceTier string
		multiplier  float64
	}{
		{name: "flex", modelID: "gpt-test", serviceTier: "flex", multiplier: 0.5},
		{name: "priority", modelID: "gpt-test", serviceTier: "priority", multiplier: 2},
		{name: "gpt-5.5 priority", modelID: "gpt-5.5", serviceTier: "priority", multiplier: 2.5},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeResponsesSSE(t, w, responsesUsageEvent(tt.serviceTier))
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-cost-test-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := responsesTestModel(providerID)
			model.ID = tt.modelID
			client := responsesTestClient(t, providerID, model, server.URL)

			final, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithOpenAIOptions(sigma.OpenAIOptions{ServiceTier: tt.serviceTier}),
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}
			if final.Cost == nil {
				t.Fatal("final cost was nil")
			}
			if got, want := final.Cost.InputCost, 1*tt.multiplier; got != want {
				t.Fatalf("input cost = %v, want %v", got, want)
			}
			if got, want := final.Cost.OutputCost, 2*tt.multiplier; got != want {
				t.Fatalf("output cost = %v, want %v", got, want)
			}
			if got, want := final.Cost.TotalCost, 3*tt.multiplier; got != want {
				t.Fatalf("total cost = %v, want %v", got, want)
			}
		})
	}
}

func TestResponsesReplayOmitsToolItemIDForSameProviderDifferentModel(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-replay-handoff-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{
			sigma.UserText("start"),
			{
				Role:     sigma.RoleAssistant,
				Provider: providerID,
				API:      sigma.APIOpenAIResponses,
				Model:    "different-responses-model",
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_prev|fc_prev", "lookup", map[string]any{"query": "weather"}),
				},
			},
		}},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	input, ok := payload["input"].([]any)
	if !ok {
		t.Fatalf("input type = %T, want []any", payload["input"])
	}
	functionCall, ok := input[1].(map[string]any)
	if !ok {
		t.Fatalf("function call type = %T, want map", input[1])
	}
	if got, want := functionCall["type"], "function_call"; got != want {
		t.Fatalf("type = %v, want %q", got, want)
	}
	if got, want := functionCall["call_id"], "call_prev"; got != want {
		t.Fatalf("call_id = %v, want %q", got, want)
	}
	if got, want := functionCall["name"], "lookup"; got != want {
		t.Fatalf("name = %v, want %q", got, want)
	}
	if _, ok := functionCall["id"]; ok {
		t.Fatalf("function_call.id was sent for same-provider different-model replay: %#v", functionCall)
	}
}

func TestResponsesStreamingParsesImageGenerationOutput(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w,
			`data: {"type":"response.created","response":{"id":"resp_image","status":"in_progress"}}

data: {"type":"response.output_item.added","response_id":"resp_image","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","status":"in_progress"}}

data: {"type":"response.image_generation_call.partial_image","response_id":"resp_image","item_id":"ig_1","output_index":0,"partial_image_b64":"cGFydGlhbA=="}

data: {"type":"response.output_item.done","response_id":"resp_image","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","status":"completed","result":"ZmluYWw="}}

data: {"type":"response.completed","response":{"id":"resp_image","status":"completed","output":[{"type":"image_generation_call","id":"ig_1","status":"completed","result":"ZmluYWw="}]}}
`,
		)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-image-generation-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	stream := client.Stream(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("draw")},
		Tools:    []sigma.Tool{openai.Tools.ImageGeneration(openai.WithPartialImages(1))},
	})
	events := collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error = %v", err)
	}
	final, ok := stream.Final()
	if !ok {
		t.Fatal("stream final was not recorded")
	}

	if got, want := eventKinds(events), []sigma.EventKind{
		sigma.EventKindStart,
		sigma.EventKindImageStart,
		sigma.EventKindImageDelta,
		sigma.EventKindImageEnd,
		sigma.EventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if len(final.Content) != 1 || final.Content[0].Type != sigma.ContentBlockImage {
		t.Fatalf("content = %#v, want one image block", final.Content)
	}
	if got, want := final.Content[0].Data, "ZmluYWw="; got != want {
		t.Fatalf("image data = %q, want %q", got, want)
	}
	if got, want := final.Content[0].ProviderMetadata["id"], "ig_1"; got != want {
		t.Fatalf("image id metadata = %v, want %q", got, want)
	}
}

func TestResponsesProviderErrorIsTypedAndRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key sk-secret123"}}`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-error-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Diagnostics[0].API, sigma.APIOpenAIResponses; got != want {
		t.Fatalf("diagnostic API = %q, want %q", got, want)
	}
	if got, want := sigma.ClassifyError(err).Class, sigma.ErrorClassAuth; got != want {
		t.Fatalf("class = %q, want %q", got, want)
	}
	if errorsContains(err, "sk-secret123") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestResponsesStreamErrorEventIsTypedProviderError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w, `event: error
data: {"type":"error","error":{"code":"rate_limit_exceeded","message":"rate limited"}}

`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-stream-error-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	classification := sigma.ClassifyError(err)
	if got, want := classification.Class, sigma.ErrorClassRateLimited; got != want {
		t.Fatalf("class = %q, want %q", got, want)
	}
	if got, want := classification.ProviderCode, "rate_limit_exceeded"; got != want {
		t.Fatalf("provider code = %q, want %q", got, want)
	}
}

func TestResponsesRequestBufferErrorIsRetryableWithoutAutomaticReplay(t *testing.T) {
	t.Parallel()

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		writeResponsesSSE(t, w, `event: error
data: {"type":"error","error":{"code":"upstream_error","message":"Error: exceeded request buffer limit while retrying upstream"}}

`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-request-buffer-error-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithMaxRetries(2),
		sigma.WithMaxRetryDelay(0),
	)
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	classification := sigma.ClassifyError(err)
	if got, want := classification.Class, sigma.ErrorClassTransient; got != want {
		t.Fatalf("class = %q, want %q", got, want)
	}
	if !classification.RetryHint.Retryable {
		t.Fatal("request buffer exhaustion was not retryable")
	}
	if got, want := attempts, 1; got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
}

func TestResponsesStreamEarlyEOFReturnsErrorWithPartialContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w, `data: {"type":"response.created","response":{"id":"resp_early","status":"in_progress"}}

data: {"type":"response.output_text.delta","response_id":"resp_early","output_index":0,"item_id":"msg_partial","delta":"partial answer"}

`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-early-eof-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !strings.Contains(err.Error(), "stream ended before terminal response event") {
		t.Fatalf("error = %v, want terminal response event error", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Text, "partial answer"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
	if got, want := final.ProviderMetadata["id"], "resp_early"; got != want {
		t.Fatalf("response id = %v, want %v", got, want)
	}
	classification := sigma.ClassifyError(err)
	if got, want := classification.Class, sigma.ErrorClassTransient; got != want {
		t.Fatalf("class = %q, want %q", got, want)
	}
	if !classification.RetryHint.Retryable {
		t.Fatal("early EOF was not retryable")
	}
}

func TestResponsesStreamIncompleteReasonHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		reasonJSON       string
		errorJSON        string
		wantStop         sigma.StopReason
		wantError        bool
		wantProviderCode string
		wantRetryable    bool
		wantReason       string
	}{
		{
			name:       "max output tokens",
			reasonJSON: `,"incomplete_details":{"reason":"max_output_tokens"}`,
			wantStop:   sigma.StopReasonMaxTokens,
			wantReason: "max_output_tokens",
		},
		{
			name:       "content filter",
			reasonJSON: `,"incomplete_details":{"reason":"content_filter"}`,
			wantStop:   sigma.StopReasonContentFilter,
			wantReason: "content_filter",
		},
		{
			name:             "unknown reason",
			reasonJSON:       `,"incomplete_details":{"reason":"max_time_limit"}`,
			wantStop:         sigma.StopReasonError,
			wantError:        true,
			wantProviderCode: "response_incomplete",
			wantReason:       "max_time_limit",
		},
		{
			name:             "omitted reason",
			wantStop:         sigma.StopReasonError,
			wantError:        true,
			wantProviderCode: "response_incomplete",
		},
		{
			name:             "explicit error takes precedence",
			reasonJSON:       `,"incomplete_details":{"reason":"max_output_tokens"}`,
			errorJSON:        `,"error":{"code":"server_error","message":"provider failed"}`,
			wantStop:         sigma.StopReasonError,
			wantError:        true,
			wantProviderCode: "server_error",
			wantRetryable:    true,
			wantReason:       "max_output_tokens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var attempts int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				writeResponsesSSE(t, w, `data: {"type":"response.incomplete","response":{"id":"resp_incomplete","model":"gpt-test-2026","status":"incomplete"`+tt.reasonJSON+tt.errorJSON+`,"output":[{"type":"message","id":"msg_incomplete","role":"assistant","content":[{"type":"output_text","id":"text_incomplete","text":"truncated"}]}],"usage":{"input_tokens":30,"input_tokens_details":{"cached_tokens":5},"output_tokens":12,"total_tokens":42}}}
`)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-incomplete-" + strings.ReplaceAll(tt.name, " ", "-"))
			model := responsesTestModel(providerID)
			client := responsesTestClient(t, providerID, model, server.URL)
			final, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithMaxRetries(1),
				sigma.WithMaxRetryDelay(0),
			)
			if tt.wantError {
				if !errors.Is(err, sigma.ErrProviderResponse) {
					t.Fatalf("Complete error = %v, want ErrProviderResponse", err)
				}
				classification := sigma.ClassifyError(err)
				if got, want := classification.ProviderCode, tt.wantProviderCode; got != want {
					t.Fatalf("provider code = %q, want %q", got, want)
				}
				if got, want := classification.RetryHint.Retryable, tt.wantRetryable; got != want {
					t.Fatalf("retryable = %v, want %v", got, want)
				}
			} else if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}
			if got, want := final.StopReason, tt.wantStop; got != want {
				t.Fatalf("stop reason = %q, want %q", got, want)
			}
			if got, want := final.Content[0].Text, "truncated"; got != want {
				t.Fatalf("text = %q, want %q", got, want)
			}
			if got, want := final.ProviderMetadata["status"], "incomplete"; got != want {
				t.Fatalf("response status = %v, want %v", got, want)
			}
			if got, want := final.ProviderMetadata["id"], "resp_incomplete"; got != want {
				t.Fatalf("response id = %v, want %v", got, want)
			}
			if got, want := final.ProviderMetadata["model"], "gpt-test-2026"; got != want {
				t.Fatalf("provider model = %v, want %v", got, want)
			}
			gotReason, hasReason := final.ProviderMetadata["incomplete_reason"]
			if tt.wantReason == "" {
				if hasReason {
					t.Fatalf("incomplete reason = %v, want absent", gotReason)
				}
			} else if gotReason != tt.wantReason {
				t.Fatalf("incomplete reason = %v, want %q", gotReason, tt.wantReason)
			}
			if final.Usage == nil || final.Cost == nil {
				t.Fatalf("usage or cost missing: usage=%#v cost=%#v", final.Usage, final.Cost)
			}
			if got, want := final.Usage.InputTokens, 25; got != want {
				t.Fatalf("input tokens = %d, want %d", got, want)
			}
			if got, want := final.Usage.CacheReadInputTokens, 5; got != want {
				t.Fatalf("cache read tokens = %d, want %d", got, want)
			}
			if got, want := final.Usage.OutputTokens, 12; got != want {
				t.Fatalf("output tokens = %d, want %d", got, want)
			}
			if got, want := attempts, 1; got != want {
				t.Fatalf("attempts = %d, want %d", got, want)
			}
		})
	}
}

func TestResponsesIncompleteMaxTokensOverridesToolCalls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w, `data: {"type":"response.incomplete","response":{"id":"resp_incomplete_tool","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"function_call","id":"fc_incomplete","call_id":"call_incomplete","name":"shell","arguments":"{\"cmd\":\"go test\"}"}],"usage":{"input_tokens":20,"output_tokens":8,"total_tokens":28}}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-incomplete-tool-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.StopReason, sigma.StopReasonMaxTokens; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if len(final.Content) != 1 || final.Content[0].Type != sigma.ContentBlockToolCall {
		t.Fatalf("content = %#v, want one tool call", final.Content)
	}
}

func TestResponsesStreamFailedTerminalIsTypedProviderError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w, `data: {"type":"response.failed","response":{"id":"resp_failed","status":"failed","error":{"code":"invalid_api_key","message":"bad key sk-secret123"}}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-failed-terminal-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil {
		t.Fatal("Complete returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.ProviderMetadata["status"], "failed"; got != want {
		t.Fatalf("response status = %v, want %v", got, want)
	}
	if errorsContains(err, "sk-secret123") {
		t.Fatalf("error leaked secret: %v", err)
	}
	if got, want := sigma.ClassifyError(err).ProviderCode, "invalid_api_key"; got != want {
		t.Fatalf("provider code = %q, want %q", got, want)
	}
}

func TestResponsesStreamDoesNotRetainTransientStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w, `data: {"type":"response.created","response":{"id":"resp_status","status":"in_progress"}}

data: {"type":"response.completed","response":{"id":"resp_status","end_turn":false,"output":[{"type":"message","id":"msg_status","role":"assistant","content":[{"type":"output_text","id":"text_status","text":"done"}]}]}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-transient-status-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if _, ok := final.ProviderMetadata["status"]; ok {
		t.Fatalf("response status = %v, want absent", final.ProviderMetadata["status"])
	}
	if _, ok := final.ProviderMetadata["end_turn"]; ok {
		t.Fatalf("end_turn = %v, want absent", final.ProviderMetadata["end_turn"])
	}
}

func TestResponsesRejectsCodexDoneTerminalAlias(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResponsesSSE(t, w, `data: {"type":"response.done","response":{"id":"resp_done","status":"completed","end_turn":true}}
`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-codex-done-alias-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)
	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err == nil || !strings.Contains(err.Error(), "stream ended before terminal response event") {
		t.Fatalf("Complete error = %v, want missing terminal response", err)
	}
	if got, want := final.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if _, ok := final.ProviderMetadata["end_turn"]; ok {
		t.Fatalf("end_turn = %v, want absent", final.ProviderMetadata["end_turn"])
	}
}

func TestResponsesCancellationAbortsStreamingRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"id":"resp_cancel","status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_text.delta","response_id":"resp_cancel","output_index":0,"item_id":"msg_partial","delta":"partial"}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-cancel-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	stream := client.Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	for {
		event := receiveEvent(t, stream)
		if event.Kind == sigma.EventKindTextDelta {
			break
		}
	}
	cancel()

	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorAborted {
		t.Fatalf("Collect error = %v, want ErrorAborted", err)
	}
	if got, want := final.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := final.Content[0].Text, "partial"; got != want {
		t.Fatalf("partial text = %q, want %q", got, want)
	}
}

func TestResponsesRetriesRetryableStatus(t *testing.T) {
	t.Parallel()

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"retry later"}}`)
			return
		}
		writeResponsesSSE(t, w, responsesCompletedEvent)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("responses-retry-test")
	model := responsesTestModel(providerID)
	client := responsesTestClient(t, providerID, model, server.URL)

	_, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithMaxRetries(1),
		sigma.WithMaxRetryDelay(0),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := attempts, 2; got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
}

func TestResponsesDefaultsStoreAndReasoningReplayInclude(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		opts          []sigma.Option
		wantStore     any
		wantInclude   any
		wantSummary   any
		wantNoInclude bool
	}{
		{
			name:        "reasoning defaults store include and summary",
			opts:        []sigma.Option{sigma.WithReasoningLevel(sigma.ThinkingLevelHigh)},
			wantStore:   false,
			wantInclude: []any{"reasoning.encrypted_content"},
			wantSummary: "auto",
		},
		{
			name: "explicit include and store are preserved",
			opts: []sigma.Option{
				sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
				sigma.WithProviderOptions(sigma.ProviderID("responses-defaults-test"), map[string]any{
					"include": []any{"file_search_call.results"},
					"store":   true,
				}),
			},
			wantStore:   true,
			wantInclude: []any{"file_search_call.results"},
			wantSummary: "auto",
		},
		{
			name:          "no reasoning keeps include absent",
			wantStore:     false,
			wantNoInclude: true,
		},
		{
			name: "explicit reasoning summary is preserved",
			opts: []sigma.Option{
				sigma.WithOpenAIOptions(sigma.OpenAIOptions{
					ReasoningEffort:  sigma.ThinkingLevelHigh,
					ReasoningSummary: "detailed",
				}),
			},
			wantStore:   false,
			wantInclude: []any{"reasoning.encrypted_content"},
			wantSummary: "detailed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("responses-defaults-test")
			model := responsesTestModel(providerID)
			client := responsesTestClient(t, providerID, model, server.URL)

			_, err := client.Complete(
				context.Background(),
				model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				tt.opts...,
			)
			if err != nil {
				t.Fatalf("Complete returned error: %v", err)
			}

			var payload map[string]any
			if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
				t.Fatalf("Unmarshal request body returned error: %v", err)
			}
			if got := payload["store"]; got != tt.wantStore {
				t.Fatalf("store = %#v, want %#v", got, tt.wantStore)
			}
			if tt.wantNoInclude {
				if _, ok := payload["include"]; ok {
					t.Fatalf("include = %#v, want absent", payload["include"])
				}
				return
			}
			if !reflect.DeepEqual(payload["include"], tt.wantInclude) {
				t.Fatalf("include = %#v, want %#v", payload["include"], tt.wantInclude)
			}
			reasoning, ok := payload["reasoning"].(map[string]any)
			if !ok {
				t.Fatalf("reasoning = %#v, want object", payload["reasoning"])
			}
			if got := reasoning["summary"]; got != tt.wantSummary {
				t.Fatalf("reasoning.summary = %#v, want %#v", got, tt.wantSummary)
			}
		})
	}
}

func responsesTestClient(t *testing.T, providerID sigma.ProviderID, model sigma.Model, baseURL string, opts ...openai.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]openai.ProviderOption{openai.WithBaseURL(baseURL)}, opts...)
	if err := registry.RegisterTextProvider(providerID, openai.NewResponsesProvider(providerOpts...)); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-key"}, nil
	})
	return sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(resolver),
		sigma.WithDefaultHeader("X-Client", "client"),
	)
}

func responsesTestModel(providerID sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:       "gpt-test",
		Provider: providerID,
		API:      sigma.APIOpenAIResponses,
		SupportedInputs: []sigma.ContentBlockType{
			sigma.ContentBlockText,
			sigma.ContentBlockImage,
		},
		SupportsTools:                true,
		SupportsThinking:             true,
		ThinkingLevelMap:             map[sigma.ThinkingLevel]string{sigma.ThinkingLevelHigh: "high"},
		InputCostPerMillion:          1,
		OutputCostPerMillion:         2,
		CacheReadInputCostPerMillion: 0.5,
	}
}

func responsesGrammarTool(name string, syntax sigma.OpenAIGrammarSyntax, definition string) sigma.Tool {
	return sigma.Tool{
		Name: name,
		InputSchema: sigma.Schema{
			"type":       "object",
			"properties": map[string]any{"command": map[string]any{"type": "string"}},
			"required":   []any{"command"},
		},
		OpenAIGrammar: &sigma.OpenAIGrammar{
			Syntax:     syntax,
			Definition: definition,
		},
	}
}

func deferredToolsRequest() sigma.Request {
	return sigma.Request{
		Tools: []sigma.Tool{
			{Name: "base", InputSchema: sigma.Schema{"type": "object"}},
			{Name: "late", InputSchema: sigma.Schema{"type": "object"}},
			{Name: "web_search", ProviderDefinedType: "web_search_preview"},
		},
		Messages: []sigma.Message{
			{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_base", "base", map[string]any{})}},
			{Role: sigma.RoleTool, ToolCallID: "call_base", Content: []sigma.ContentBlock{sigma.Text("first")}, AddedToolNames: []string{"late", "missing", "late"}},
			{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_base_second", "base", map[string]any{})}},
			{Role: sigma.RoleTool, ToolCallID: "call_base_second", Content: []sigma.ContentBlock{sigma.Text("second")}, AddedToolNames: []string{"late"}},
		},
	}
}

func failedResponsesReplayRequest() sigma.Request {
	return sigma.Request{Messages: []sigma.Message{
		sigma.UserText("before"),
		{
			Role:       sigma.RoleAssistant,
			StopReason: sigma.StopReasonEndTurn,
			Content:    []sigma.ContentBlock{sigma.Text("kept success")},
		},
		{
			Role:       sigma.RoleAssistant,
			StopReason: sigma.StopReasonError,
			Content:    []sigma.ContentBlock{sigma.Thinking("failed reasoning", "reasoning-signature")},
		},
		{
			Role:       sigma.RoleAssistant,
			StopReason: sigma.StopReasonAborted,
			Content: []sigma.ContentBlock{
				sigma.Text("failed partial"),
				sigma.ToolCallBlock("call_failed", "lookup", map[string]any{"query": "discard"}),
			},
		},
		sigma.ToolResult("call_failed", "failed tool output"),
		{
			Role:       sigma.RoleAssistant,
			StopReason: sigma.StopReasonMaxTokens,
			Content:    []sigma.ContentBlock{sigma.Text("kept max tokens")},
		},
		{
			Role:       sigma.RoleAssistant,
			StopReason: sigma.StopReasonContentFilter,
			Content:    []sigma.ContentBlock{sigma.Text("kept content filter")},
		},
		{
			Role:       sigma.RoleAssistant,
			StopReason: sigma.StopReasonToolCalls,
			Content: []sigma.ContentBlock{
				sigma.ToolCallBlock("call_valid", "lookup", map[string]any{"query": "keep"}),
			},
		},
		sigma.ToolResult("call_valid", "valid tool output"),
		{
			Role:       sigma.RoleAssistant,
			StopReason: sigma.StopReasonToolCalls,
			Content: []sigma.ContentBlock{
				sigma.ToolCallBlock("call_unanswered", "lookup", map[string]any{"query": "repair"}),
			},
		},
		sigma.UserText("after"),
	}}
}

func assertFailedResponsesReplayFiltered(t *testing.T, body []byte) {
	t.Helper()

	payload := decodeResponsesPayload(t, body)
	input := payload["input"].([]any)
	wantTypes := []string{
		"", "message", "message", "message", "function_call",
		"function_call_output", "function_call", "function_call_output", "",
	}
	if got, want := len(input), len(wantTypes); got != want {
		t.Fatalf("input count = %d, want %d: %#v", got, want, input)
	}
	for index, want := range wantTypes {
		item := input[index].(map[string]any)
		if got, _ := item["type"].(string); got != want {
			t.Fatalf("input[%d] type = %q, want %q: %#v", index, got, want, item)
		}
	}

	assertResponsesInputText(t, input[0].(map[string]any), "before")
	assertResponsesOutputText(t, input[1].(map[string]any), "kept success")
	assertResponsesOutputText(t, input[2].(map[string]any), "kept max tokens")
	assertResponsesOutputText(t, input[3].(map[string]any), "kept content filter")
	if got, want := input[4].(map[string]any)["call_id"], "call_valid"; got != want {
		t.Fatalf("valid call id = %v, want %q", got, want)
	}
	if got, want := input[5].(map[string]any)["output"], "valid tool output"; got != want {
		t.Fatalf("valid tool output = %v, want %q", got, want)
	}
	if got, want := input[6].(map[string]any)["call_id"], "call_unanswered"; got != want {
		t.Fatalf("unanswered call id = %v, want %q", got, want)
	}
	if got, want := input[7].(map[string]any)["output"], "No result provided"; got != want {
		t.Fatalf("synthetic tool output = %v, want %q", got, want)
	}
	assertResponsesInputText(t, input[8].(map[string]any), "after")

	if strings.Contains(string(body), "failed reasoning") ||
		strings.Contains(string(body), "failed partial") ||
		strings.Contains(string(body), "call_failed") ||
		strings.Contains(string(body), "failed tool output") {
		t.Fatalf("failed assistant turn was replayed: %s", body)
	}
}

func assertDeferredToolsPayload(t *testing.T, body []byte) {
	t.Helper()

	payload := decodeResponsesPayload(t, body)
	tools := payload["tools"].([]any)
	if got, want := len(tools), 2; got != want {
		t.Fatalf("root tools = %#v, want %d immediate tools", tools, want)
	}
	if got, want := tools[0].(map[string]any)["name"], "base"; got != want {
		t.Fatalf("first root tool = %v, want %q", got, want)
	}
	if got, want := tools[1].(map[string]any)["type"], "web_search_preview"; got != want {
		t.Fatalf("provider-defined root tool = %v, want %q", got, want)
	}

	input := payload["input"].([]any)
	searchCalls := 0
	searchOutputs := 0
	for index, item := range input {
		typed := item.(map[string]any)
		switch typed["type"] {
		case "tool_search_call":
			searchCalls++
			if index == 0 || input[index-1].(map[string]any)["type"] != "function_call_output" {
				t.Fatalf("tool search call at input[%d] does not follow its tool output: %#v", index, input)
			}
			if index+1 >= len(input) || input[index+1].(map[string]any)["type"] != "tool_search_output" {
				t.Fatalf("tool search call at input[%d] is not paired with output: %#v", index, input)
			}
			searchCall := typed
			searchOutput := input[index+1].(map[string]any)
			if got, want := searchCall["call_id"], searchOutput["call_id"]; got != want {
				t.Fatalf("tool search ids = %v and %v, want match", got, want)
			}
			if got, want := searchCall["execution"], "client"; got != want {
				t.Fatalf("tool search execution = %v, want %q", got, want)
			}
			if got, want := searchCall["status"], "completed"; got != want {
				t.Fatalf("tool search status = %v, want %q", got, want)
			}
		case "tool_search_output":
			searchOutputs++
		}
	}
	if got, want := searchCalls, 1; got != want {
		t.Fatalf("tool search calls = %d, want %d", got, want)
	}
	if got, want := searchOutputs, 1; got != want {
		t.Fatalf("tool search outputs = %d, want %d", got, want)
	}
	var searchOutput map[string]any
	for _, item := range input {
		typed := item.(map[string]any)
		if typed["type"] == "tool_search_output" {
			searchOutput = typed
			break
		}
	}
	searchTools := searchOutput["tools"].([]any)
	if got, want := len(searchTools), 1; got != want {
		t.Fatalf("deferred tools = %#v, want %d", searchTools, want)
	}
	deferred := searchTools[0].(map[string]any)
	if got, want := deferred["name"], "late"; got != want {
		t.Fatalf("deferred tool name = %v, want %q", got, want)
	}
	if got, want := deferred["defer_loading"], true; got != want {
		t.Fatalf("deferred tool flag = %v, want %v", got, want)
	}
}

func assertAdditionalToolsPayload(t *testing.T, body []byte) {
	t.Helper()

	payload := decodeResponsesPayload(t, body)
	tools := payload["tools"].([]any)
	if got, want := len(tools), 2; got != want {
		t.Fatalf("root tools = %#v, want %d immediate tools", tools, want)
	}
	if got, want := tools[0].(map[string]any)["name"], "base"; got != want {
		t.Fatalf("first root tool = %v, want %q", got, want)
	}
	if got, want := tools[1].(map[string]any)["type"], "web_search_preview"; got != want {
		t.Fatalf("provider-defined root tool = %v, want %q", got, want)
	}

	input := payload["input"].([]any)
	additionalCount := 0
	for index, item := range input {
		typed := item.(map[string]any)
		switch typed["type"] {
		case "tool_search_call", "tool_search_output":
			t.Fatalf("additional-tools payload included synthetic search item: %#v", typed)
		case "additional_tools":
			additionalCount++
			if index == 0 || input[index-1].(map[string]any)["type"] != "function_call_output" {
				t.Fatalf("additional tools at input[%d] do not follow their tool output: %#v", index, input)
			}
			if got, want := typed["role"], "developer"; got != want {
				t.Fatalf("additional tools role = %v, want %q", got, want)
			}
			loaded := typed["tools"].([]any)
			if got, want := len(loaded), 1; got != want {
				t.Fatalf("additional tools = %#v, want %d", loaded, want)
			}
			tool := loaded[0].(map[string]any)
			if got, want := tool["name"], "late"; got != want {
				t.Fatalf("additional tool name = %v, want %q", got, want)
			}
			if _, ok := tool["defer_loading"]; ok {
				t.Fatalf("additional tool retained defer_loading: %#v", tool)
			}
		}
	}
	if got, want := additionalCount, 1; got != want {
		t.Fatalf("additional tool items = %d, want %d", got, want)
	}
}

func decodeResponsesPayload(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	return payload
}

func hasResponsesItemType(payload map[string]any, itemType string) bool {
	for _, item := range payload["input"].([]any) {
		if item.(map[string]any)["type"] == itemType {
			return true
		}
	}
	return false
}

func assertResponsesFunctionToolChoice(t *testing.T, body []byte) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	choice, ok := payload["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice type = %T, want map", payload["tool_choice"])
	}
	if got, want := choice["type"], "function"; got != want {
		t.Fatalf("tool_choice.type = %v, want %q", got, want)
	}
	if got, want := choice["name"], "read_file"; got != want {
		t.Fatalf("tool_choice.name = %v, want %q", got, want)
	}
	if _, ok := choice["function"]; ok {
		t.Fatalf("tool_choice.function was not normalized: %#v", choice)
	}
}

func responsesRichRequest() sigma.Request {
	thinking := sigma.Thinking("Internal summary.", "think_prev_sig")
	thinking.ProviderSignature = "enc_prev"
	thinking.ProviderMetadata = map[string]any{"id": "rs_prev"}
	text := sigma.Text("Earlier answer.")
	text.Signature = "text_prev_sig"
	text.ProviderMetadata = map[string]any{"id": "msg_prev", "content_id": "text_prev"}
	toolCall := sigma.ToolCallBlock("call_prev", "lookup", map[string]any{"query": "weather"})
	toolCall.ProviderMetadata = map[string]any{"id": "fc_prev"}

	return sigma.Request{
		SystemPrompt: "You are helpful.",
		Messages: []sigma.Message{
			{
				Role:    sigma.RoleDeveloper,
				Content: []sigma.ContentBlock{sigma.Text("Use terse answers.")},
			},
			sigma.UserContent(
				sigma.Text("Describe this"),
				sigma.ImageURL("image/png", "https://example.test/cat.png"),
				sigma.ImageBase64("image/png", "aGk="),
			),
			{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{text, thinking, toolCall},
			},
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call_prev",
				Content: []sigma.ContentBlock{
					sigma.Text("Sunny"),
					sigma.ImageBase64("image/png", "aGk="),
				},
			},
		},
		Tools: []sigma.Tool{{
			Name:        "weather",
			Description: "Get weather",
			InputSchema: sigma.Schema{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
				"required":             []any{"city"},
				"additionalProperties": false,
			},
			ProviderMetadata: map[string]any{"strict": true},
		}},
	}
}

func writeResponsesSSE(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, body)
}

func errorsContains(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}

const responsesCompletedEvent = `data: {"type":"response.completed","response":{"id":"resp_complete","model":"gpt-test","status":"completed","output":[{"type":"message","id":"msg_complete","role":"assistant","content":[{"type":"output_text","id":"text_complete","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}
`

const responsesAssistantPhasesEvent = `data: {"type":"response.output_item.added","response_id":"resp_phases","output_index":0,"item":{"type":"message","id":"msg_commentary","role":"assistant","phase":"commentary","content":[]}}

data: {"type":"response.output_text.delta","response_id":"resp_phases","item_id":"msg_commentary","output_index":0,"content_index":0,"delta":"Checking constraints."}

data: {"type":"response.output_item.done","response_id":"resp_phases","output_index":0,"item":{"type":"message","id":"msg_commentary","role":"assistant","phase":"commentary","content":[{"type":"output_text","id":"text_commentary","text":"Checking constraints."}]}}

data: {"type":"response.output_item.added","response_id":"resp_phases","output_index":1,"item":{"type":"message","id":"msg_answer","role":"assistant","phase":"commentary","content":[]}}

data: {"type":"response.output_text.delta","response_id":"resp_phases","item_id":"msg_answer","output_index":1,"content_index":0,"delta":"The answer."}

data: {"type":"response.output_item.done","response_id":"resp_phases","output_index":1,"item":{"type":"message","id":"msg_answer","role":"assistant","phase":"final_answer","content":[{"type":"output_text","id":"text_answer","text":"The answer."}]}}

data: {"type":"response.completed","response":{"id":"resp_phases","model":"gpt-test","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}
`

func responsesUsageEvent(serviceTier string) string {
	return `data: {"type":"response.completed","response":{"id":"resp_usage","model":"gpt-test","status":"completed","service_tier":"` + serviceTier + `","output":[{"type":"message","id":"msg_usage","role":"assistant","content":[{"type":"output_text","id":"text_usage","text":"ok"}]}],"usage":{"input_tokens":1000000,"output_tokens":1000000,"total_tokens":2000000,"input_tokens_details":{"cached_tokens":0}}}}
`
}
