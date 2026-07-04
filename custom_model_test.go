// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

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
	"github.com/wintermi/sigma/provider/openai"
)

func TestOpenAICompatibleModelValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model sigma.Model
		want  string
	}{
		{
			name:  "missing model id",
			model: sigma.OpenAICompatibleModel(sigma.OpenAICompatibleModelConfig{Provider: "local", BaseURL: "http://localhost:11434/v1"}),
			want:  "model id is required",
		},
		{
			name:  "missing provider",
			model: sigma.OpenAICompatibleModel(sigma.OpenAICompatibleModelConfig{ID: "llama3.2", BaseURL: "http://localhost:11434/v1"}),
			want:  "provider is required",
		},
		{
			name:  "missing api",
			model: sigma.Model{ID: "llama3.2", Provider: "local"},
			want:  "model api is required",
		},
		{
			name:  "missing base url",
			model: sigma.OpenAICompatibleModel(sigma.OpenAICompatibleModelConfig{ID: "llama3.2", Provider: "local"}),
			want:  "base URL is required",
		},
		{
			name: "compatibility on wrong api",
			model: sigma.Model{
				ID:                      "wrong-api",
				Provider:                "local",
				API:                     sigma.APIOpenAIResponses,
				OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{},
			},
			want: "requires api openai-completions",
		},
		{
			name: "invalid header metadata",
			model: sigma.OpenAICompatibleModel(sigma.OpenAICompatibleModelConfig{
				ID:       "llama3.2",
				Provider: "local",
				BaseURL:  "http://localhost:11434/v1",
				ProviderMetadata: map[string]any{
					sigma.MetadataOpenAICompatibleHeaders: map[string]any{"X-Test": 3},
				},
			}),
			want: "headers must be strings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := sigma.RegisterModel(sigma.NewRegistry(), tt.model, sigma.WithMetadataOnly())
			if err == nil {
				t.Fatal("RegisterModel returned nil error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestOpenAICompatibleEmbeddingModelValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model sigma.EmbeddingModel
		want  string
	}{
		{
			name:  "missing model id",
			model: sigma.OpenAICompatibleEmbeddingModel(sigma.OpenAICompatibleEmbeddingModelConfig{Provider: "local", BaseURL: "http://localhost:11434/v1"}),
			want:  "model id is required",
		},
		{
			name:  "missing provider",
			model: sigma.OpenAICompatibleEmbeddingModel(sigma.OpenAICompatibleEmbeddingModelConfig{ID: "embed", BaseURL: "http://localhost:11434/v1"}),
			want:  "provider is required",
		},
		{
			name:  "missing base url",
			model: sigma.OpenAICompatibleEmbeddingModel(sigma.OpenAICompatibleEmbeddingModelConfig{ID: "embed", Provider: "local"}),
			want:  "base URL is required",
		},
		{
			name: "invalid base url",
			model: sigma.OpenAICompatibleEmbeddingModel(sigma.OpenAICompatibleEmbeddingModelConfig{
				ID:       "embed",
				Provider: "local",
				BaseURL:  "localhost:11434/v1",
			}),
			want: "base URL must be absolute",
		},
		{
			name: "compatibility on wrong api",
			model: sigma.EmbeddingModel{
				ID:       "wrong-api",
				Provider: "local",
				API:      "other-embeddings",
				ProviderMetadata: map[string]any{
					sigma.MetadataOpenAICompatible:        true,
					sigma.MetadataOpenAICompatibleBaseURL: "http://localhost:11434/v1",
				},
			},
			want: "api must be openai-embeddings",
		},
		{
			name: "invalid header metadata",
			model: sigma.OpenAICompatibleEmbeddingModel(sigma.OpenAICompatibleEmbeddingModelConfig{
				ID:       "embed",
				Provider: "local",
				BaseURL:  "http://localhost:11434/v1",
				ProviderMetadata: map[string]any{
					sigma.MetadataOpenAICompatibleHeaders: map[string]any{"X-Test": 3},
				},
			}),
			want: "headers must be strings",
		},
		{
			name: "negative max batch inputs",
			model: sigma.OpenAICompatibleEmbeddingModel(sigma.OpenAICompatibleEmbeddingModelConfig{
				ID:             "embed",
				Provider:       "local",
				BaseURL:        "http://localhost:11434/v1",
				MaxBatchInputs: -1,
			}),
			want: "max batch inputs must be non-negative",
		},
		{
			name: "negative max batch bytes",
			model: sigma.OpenAICompatibleEmbeddingModel(sigma.OpenAICompatibleEmbeddingModelConfig{
				ID:            "embed",
				Provider:      "local",
				BaseURL:       "http://localhost:11434/v1",
				MaxBatchBytes: -1,
			}),
			want: "max batch bytes must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := sigma.RegisterEmbeddingModel(sigma.NewRegistry(), tt.model, sigma.WithMetadataOnly())
			if err == nil {
				t.Fatal("RegisterEmbeddingModel returned nil error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestOpenAICompatibleEmbeddingModelUsesLocalEndpointMetadata(t *testing.T) {
	t.Parallel()

	requests := make(chan customModelRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureCustomModelRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`)
	}))
	t.Cleanup(server.Close)

	headers := map[string]string{"X-Model": "model", "X-Model-Only": "present", "Authorization": "Bearer model"}
	metadata := map[string]any{"family": "local"}
	providerID := sigma.ProviderID("local-openai-compatible-embeddings")
	model := sigma.OpenAICompatibleEmbeddingModel(sigma.OpenAICompatibleEmbeddingModelConfig{
		ID:                  "local-embed",
		Provider:            providerID,
		BaseURL:             server.URL,
		Name:                "Local Embeddings",
		Headers:             headers,
		DefaultDimensions:   1024,
		MinDimensions:       1,
		MaxDimensions:       1024,
		MaxInputTokens:      8192,
		MaxBatchInputs:      4,
		MaxBatchBytes:       4096,
		InputCostPerMillion: 0.01,
		CostCurrency:        "USD",
		ProviderMetadata:    metadata,
	})
	headers["X-Model-Only"] = "mutated"
	metadata["family"] = "mutated"

	registry := sigma.NewRegistry()
	if err := openai.RegisterEmbeddings(registry, providerID); err != nil {
		t.Fatalf("openai.RegisterEmbeddings returned error: %v", err)
	}
	if err := sigma.RegisterEmbeddingModel(registry, model); err != nil {
		t.Fatalf("RegisterEmbeddingModel returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(staticCredential("local")),
	)

	_, err := client.Embed(
		context.Background(),
		model,
		sigma.EmbeddingRequest{Inputs: []string{"hi"}, Dimensions: 512},
		sigma.WithEmbeddingHeader("X-Model", "request"),
	)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}

	request := receiveCustomModelRequest(t, requests)
	if got, want := request.Path, "/embeddings"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeaderValue(t, request.Headers, "Authorization", "Bearer local")
	assertHeaderValue(t, request.Headers, "X-Model", "request")
	assertHeaderValue(t, request.Headers, "X-Model-Only", "present")
	assertJSONMap(t, request.Body, map[string]any{
		"model":           "local-embed",
		"input":           []any{"hi"},
		"encoding_format": "float",
		"dimensions":      float64(512),
	})
	if got, want := model.ProviderMetadata["family"], "local"; got != want {
		t.Fatalf("provider metadata family = %q, want %q", got, want)
	}
	if got, want := model.MaxBatchInputs, 4; got != want {
		t.Fatalf("max batch inputs = %d, want %d", got, want)
	}
	if got, want := model.MaxBatchBytes, 4096; got != want {
		t.Fatalf("max batch bytes = %d, want %d", got, want)
	}
}

func TestOpenAICompatibleEmbeddingModelDefaultsToSingleInputBatches(t *testing.T) {
	t.Parallel()

	model := sigma.OpenAICompatibleEmbeddingModel(sigma.OpenAICompatibleEmbeddingModelConfig{
		ID:       "local-embed",
		Provider: "local-openai-compatible-embeddings",
		BaseURL:  "http://localhost:11434/v1",
	})
	if got, want := model.MaxBatchInputs, 1; got != want {
		t.Fatalf("max batch inputs = %d, want %d", got, want)
	}
	if got := model.MaxBatchBytes; got != 0 {
		t.Fatalf("max batch bytes = %d, want 0", got)
	}
}

func TestCustomModelRegistrationDuplicateAndIsolation(t *testing.T) {
	t.Parallel()

	providerID := sigma.ProviderID("isolated-local")
	model := sigma.OpenAICompatibleModel(sigma.OpenAICompatibleModelConfig{
		ID:              "acme/llama",
		Provider:        providerID,
		BaseURL:         "http://localhost:8000/v1",
		ContextWindow:   131072,
		MaxOutputTokens: 8192,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:   true,
	})

	registry := sigma.NewRegistry()
	if err := sigma.RegisterProvider(registry, providerID, customTextProvider{api: sigma.APIOpenAICompletions}); err != nil {
		t.Fatalf("RegisterProvider returned error: %v", err)
	}
	if err := sigma.RegisterModel(registry, model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	if err := sigma.RegisterModel(registry, model); err == nil {
		t.Fatal("duplicate model registration returned nil error")
	}

	model.Name = "ACME Llama"
	if err := sigma.RegisterModel(registry, model, sigma.WithOverride()); err != nil {
		t.Fatalf("override RegisterModel returned error: %v", err)
	}
	registered, ok := registry.Model(providerID, model.ID)
	if !ok {
		t.Fatal("custom model was not registered")
	}
	if registered.Name != "ACME Llama" {
		t.Fatalf("model name = %q, want %q", registered.Name, "ACME Llama")
	}
	if _, ok := sigma.DefaultRegistry().Model(providerID, model.ID); ok {
		t.Fatal("custom model leaked into default registry")
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if _, ok := client.GetModel(providerID, model.ID); !ok {
		t.Fatal("client did not use isolated registry")
	}
}

func TestOpenAICompatibleModelUsesLocalEndpointMetadata(t *testing.T) {
	t.Parallel()

	requests := make(chan customModelRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureCustomModelRequest(t, requests, r)
		writeCustomModelSSE(t, w)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("local-openai-compatible")
	model := sigma.OpenAICompatibleModel(sigma.OpenAICompatibleModelConfig{
		ID:               "local-chat",
		Provider:         providerID,
		BaseURL:          server.URL,
		Headers:          map[string]string{"X-Model": "model", "X-Model-Only": "present", "Authorization": "Bearer model"},
		ContextWindow:    32768,
		MaxOutputTokens:  4096,
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText},
		SupportsTools:    true,
		SupportsThinking: true,
		ThinkingLevelMap: map[sigma.ThinkingLevel]string{
			sigma.ThinkingLevelHigh: "deep",
		},
		InputCostPerMillion:  0.1,
		OutputCostPerMillion: 0.2,
		CostCurrency:         "USD",
		OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
			SupportsStreamingUsage: sigma.OpenAICompatSupported,
			MaxTokensField:         sigma.OpenAICompletionsMaxCompletionTokens,
			ReasoningFormat:        sigma.OpenAICompletionsReasoningObject,
			SupportsStore:          sigma.OpenAICompatUnsupported,
		},
	})

	registry := sigma.NewRegistry()
	if err := openai.Register(registry, providerID); err != nil {
		t.Fatalf("openai.Register returned error: %v", err)
	}
	if err := sigma.RegisterModel(registry, model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "resolved-token"}, nil
		})),
	)

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithMaxTokens(42),
		sigma.WithReasoningLevel(sigma.ThinkingLevelHigh),
		sigma.WithHeader("X-Model", "request"),
		sigma.WithHeader("Authorization", "Bearer request"),
		sigma.WithProviderOptions(providerID, map[string]any{
			"extra_body": map[string]any{"store": true},
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "ok"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}

	request := receiveCustomModelRequest(t, requests)
	if got, want := request.Path, "/chat/completions"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeaderValue(t, request.Headers, "Authorization", "Bearer resolved-token")
	assertHeaderValue(t, request.Headers, "X-Model", "request")
	assertHeaderValue(t, request.Headers, "X-Model-Only", "present")
	assertJSONMap(t, request.Body, map[string]any{
		"model":                 "local-chat",
		"messages":              []any{map[string]any{"role": "user", "content": "hi"}},
		"stream":                true,
		"stream_options":        map[string]any{"include_usage": true},
		"max_completion_tokens": float64(42),
		"reasoning":             map[string]any{"effort": "deep"},
	})
	if _, ok := request.Body["store"]; ok {
		t.Fatal("store was sent despite compatibility override disabling it")
	}
}

func TestOpenAICompatibleModelUsesConservativeDefaultsForLocalEndpoints(t *testing.T) {
	t.Parallel()

	requests := make(chan customModelRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureCustomModelRequest(t, requests, r)
		writeCustomModelSSE(t, w)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("conservative-local")
	model := sigma.OpenAICompatibleModel(sigma.OpenAICompatibleModelConfig{
		ID:               "local-conservative",
		Provider:         providerID,
		BaseURL:          server.URL,
		SupportsThinking: true,
	})
	registry := sigma.NewRegistry()
	if err := openai.Register(registry, providerID); err != nil {
		t.Fatalf("openai.Register returned error: %v", err)
	}
	if err := sigma.RegisterModel(registry, model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(staticCredential("local")),
	)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		SystemPrompt: "policy",
		Messages: []sigma.Message{{
			Role:    sigma.RoleDeveloper,
			Content: []sigma.ContentBlock{sigma.Text("style")},
		}},
	}, sigma.WithReasoningLevel(sigma.ThinkingLevelHigh))
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveCustomModelRequest(t, requests)
	assertJSONMap(t, request.Body, map[string]any{
		"model": "local-conservative",
		"messages": []any{
			map[string]any{"role": "system", "content": "policy"},
			map[string]any{"role": "system", "content": "style"},
		},
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	})
	for _, key := range []string{"reasoning", "reasoning_effort"} {
		if _, ok := request.Body[key]; ok {
			t.Fatalf("%s was sent despite conservative compatibility defaults", key)
		}
	}
}

func TestOpenAICompatibleModelCanDisableStreamingUsageForLocalEndpoints(t *testing.T) {
	t.Parallel()

	requests := make(chan customModelRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureCustomModelRequest(t, requests, r)
		writeCustomModelSSE(t, w)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("local-stream-usage-off")
	model := sigma.OpenAICompatibleModel(sigma.OpenAICompatibleModelConfig{
		ID:       "local-no-usage",
		Provider: providerID,
		BaseURL:  server.URL,
		OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
			SupportsStreamingUsage: sigma.OpenAICompatUnsupported,
		},
	})
	registry := sigma.NewRegistry()
	if err := openai.Register(registry, providerID); err != nil {
		t.Fatalf("openai.Register returned error: %v", err)
	}
	if err := sigma.RegisterModel(registry, model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(staticCredential("local")),
	)

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("hi")},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	request := receiveCustomModelRequest(t, requests)
	if _, ok := request.Body["stream_options"]; ok {
		t.Fatalf("stream_options was sent despite unsupported streaming usage: %#v", request.Body["stream_options"])
	}
}

type customModelRequest struct {
	Path    string
	Headers http.Header
	Body    map[string]any
}

type customTextProvider struct {
	api sigma.API
}

func (p customTextProvider) API() sigma.API {
	return p.api
}

func (p customTextProvider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	stream, writer := sigma.NewStream(ctx)
	go func() {
		_ = writer.Done(ctx, sigma.AssistantMessage{
			Model:      model.ID,
			Provider:   model.Provider,
			StopReason: sigma.StopReasonEndTurn,
		})
	}()
	return stream
}

func captureCustomModelRequest(t *testing.T, requests chan<- customModelRequest, r *http.Request) {
	t.Helper()

	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("ReadAll returned error: %v", err)
		return
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Errorf("request body did not decode: %v\n%s", err, data)
		return
	}
	requests <- customModelRequest{
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
		Body:    body,
	}
}

func receiveCustomModelRequest(t *testing.T, requests <-chan customModelRequest) customModelRequest {
	t.Helper()

	select {
	case request := <-requests:
		return request
	default:
		t.Fatal("server did not receive request")
		return customModelRequest{}
	}
}

func writeCustomModelSSE(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, err := io.WriteString(w, ""+
		`data: {"id":"chatcmpl_custom","model":"local-chat","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`+"\n\n"+
		`data: {"id":"chatcmpl_custom","model":"local-chat","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`+"\n\n"+
		`data: {"id":"chatcmpl_custom","model":"local-chat","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
		"data: [DONE]\n\n")
	if err != nil {
		t.Fatalf("WriteString returned error: %v", err)
	}
}

func assertHeaderValue(t *testing.T, headers http.Header, key string, want string) {
	t.Helper()

	if got := headers.Get(key); got != want {
		t.Fatalf("%s header = %q, want %q", key, got, want)
	}
}

func assertJSONMap(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		gotData, _ := json.MarshalIndent(got, "", "  ")
		wantData, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("JSON mismatch\nactual:\n%s\nwant:\n%s", gotData, wantData)
	}
}

func staticCredential(value string) sigma.AuthResolver {
	return sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		if value == "" {
			return sigma.Credential{}, errors.New("missing test credential")
		}
		return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: value}, nil
	})
}
