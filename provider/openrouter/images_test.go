// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openrouter_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
	"github.com/wintermi/sigma/provider/openrouter"
)

func TestRegisterReportsImageAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := openrouter.RegisterImages(registry); err != nil {
		t.Fatalf("RegisterImages returned error: %v", err)
	}
	if err := registry.RegisterImageModel(openRouterImageModel()); err != nil {
		t.Fatalf("RegisterImageModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].ImageAPI, sigma.ImageAPIOpenRouterImages; got != want {
		t.Fatalf("provider API = %q, want %q", got, want)
	}
}

func TestGenerateImagesSendsGoldenPayloadAndMapsResponse(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": "gen_123",
			"model": "google/gemini-2.5-flash-image",
			"provider": "Google",
			"choices": [{
				"finish_reason": "stop",
				"message": {
					"role": "assistant",
					"content": "Generated one image.",
					"images": [{
						"type": "image_url",
						"image_url": {"url": "data:image/png;base64,aW1hZ2U="}
					}]
				}
			}],
			"usage": {
				"prompt_tokens": 7,
				"completion_tokens": 3,
				"total_tokens": 10,
				"prompt_tokens_details": {"cached_tokens": 2, "cache_write_tokens": 1},
				"completion_tokens_details": {"reasoning_tokens": 1}
			}
		}`)
	}))
	t.Cleanup(server.Close)

	client := openRouterTestClient(t, server.URL, openrouter.WithImagesHeader("X-Provider", "provider"))
	req := sigma.ImageRequest{
		Prompt:  "A ceramic robot watering herbs",
		Size:    string(sigma.ImageSize1536x1024),
		Quality: string(sigma.ImageQualityHigh),
		Count:   2,
	}
	got, err := client.GenerateImages(
		context.Background(),
		openRouterImageModel(),
		req,
		sigma.WithImageAPIKey("request-key"),
		sigma.WithImageHeader("X-Custom", "custom"),
		sigma.WithImageMetadataValue("trace", "abc"),
		sigma.WithImageProviderOptions(sigma.ProviderOpenRouter, map[string]any{
			"provider": map[string]any{
				"only":            []any{"Google"},
				"allow_fallbacks": false,
			},
		}),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	if got.ResponseID != "gen_123" {
		t.Fatalf("response id = %q, want gen_123", got.ResponseID)
	}
	if got.StopReason != sigma.StopReasonEndTurn {
		t.Fatalf("stop reason = %q, want end-turn", got.StopReason)
	}
	if got.Model != "google/gemini-2.5-flash-image" {
		t.Fatalf("model = %q, want provider response model", got.Model)
	}
	if got.Provider != sigma.ProviderOpenRouter {
		t.Fatalf("provider = %q, want openrouter", got.Provider)
	}
	if got.Usage == nil || got.Usage.InputTokens != 4 || got.Usage.CacheReadInputTokens != 2 ||
		got.Usage.CacheWriteInputTokens != 1 || got.Usage.ThinkingTokens != 1 {
		t.Fatalf("usage = %#v, want mapped usage", got.Usage)
	}
	if want := []sigma.ImageInput{
		sigma.ImageText("Generated one image."),
		sigma.ImageOutputData("image/png", "aW1hZ2U="),
	}; !reflect.DeepEqual(got.Images, want) {
		t.Fatalf("images = %#v, want %#v", got.Images, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/chat/completions"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer request-key")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	goldentest.AssertJSON(t, request.Body, "provider/openrouter/images/basic_payload.json")
}

func TestGenerateImagesSendsImageInputPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": "gen_input",
			"choices": [{
				"finish_reason": "stop",
				"message": {
					"images": [{
						"type": "image_url",
						"image_url": {"url": "https://example.test/output.png"}
					}]
				}
			}]
		}`)
	}))
	t.Cleanup(server.Close)

	client := openRouterTestClient(t, server.URL)
	got, err := client.GenerateImages(
		context.Background(),
		openRouterImageModel(),
		sigma.ImageRequest{
			Prompt: "Turn this into an ink sketch",
			Inputs: []sigma.ImageInput{
				sigma.ImageText("Keep the composition intact"),
				sigma.ImageData("image/jpeg", "aW5wdXQ="),
			},
			Size: "4K",
		},
		sigma.WithImageProviderOptions(sigma.ProviderOpenRouter, map[string]any{
			"modalities": []any{"image"},
			"image_config": map[string]any{
				"aspect_ratio": "16:9",
			},
		}),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	if want := []sigma.ImageInput{sigma.ImageOutputURL("", "https://example.test/output.png")}; !reflect.DeepEqual(got.Images, want) {
		t.Fatalf("images = %#v, want %#v", got.Images, want)
	}

	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/openrouter/images/input_payload.json")
}

func TestGenerateImagesProviderErrorIsTypedAndRedacted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_123")
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key sk-secret123","code":"unauthorized"}}`)
	}))
	t.Cleanup(server.Close)

	client := openRouterTestClient(t, server.URL)
	response, err := client.GenerateImages(context.Background(), openRouterImageModel(), sigma.ImageRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("GenerateImages returned nil error")
	}
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
	if got, want := response.StopReason, sigma.StopReasonError; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	var providerErr *sigma.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want *sigma.ProviderError", err)
	}
	if got, want := providerErr.API, sigma.API(sigma.ImageAPIOpenRouterImages); got != want {
		t.Fatalf("provider error API = %q, want %q", got, want)
	}
	if providerErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", providerErr.StatusCode)
	}
	if strings.Contains(err.Error(), "sk-secret123") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestGenerateImagesTimeoutAbortsRequest(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(server.Close)

	client := openRouterTestClient(t, server.URL)
	response, err := client.GenerateImages(
		context.Background(),
		openRouterImageModel(),
		sigma.ImageRequest{Prompt: "wait"},
		sigma.WithImageTimeout(10*time.Millisecond),
	)
	close(release)
	if err == nil {
		t.Fatal("GenerateImages returned nil error")
	}
	if !errors.Is(err, sigma.ErrAborted) {
		t.Fatalf("error = %v, want ErrAborted", err)
	}
	if got, want := response.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
}

func TestGenerateImagesValidatesUnsupportedInputBeforeNetwork(t *testing.T) {
	t.Parallel()

	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	t.Cleanup(server.Close)

	client := openRouterTestClient(t, server.URL)
	_, err := client.GenerateImages(
		context.Background(),
		openRouterImageModel(),
		sigma.ImageRequest{Inputs: []sigma.ImageInput{{Type: "mask"}}},
	)
	if err == nil {
		t.Fatal("GenerateImages returned nil error")
	}
	if called {
		t.Fatal("server was called for unsupported image input")
	}
	if !strings.Contains(err.Error(), "unsupported input type") {
		t.Fatalf("error = %v, want unsupported input type", err)
	}
}

func openRouterTestClient(t *testing.T, baseURL string, opts ...openrouter.ImagesProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]openrouter.ImagesProviderOption{openrouter.WithImagesBaseURL(baseURL)}, opts...)
	if err := registry.RegisterImageProvider(sigma.ProviderOpenRouter, openrouter.NewImagesProvider(providerOpts...)); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	if err := registry.RegisterImageModel(openRouterImageModel()); err != nil {
		t.Fatalf("RegisterImageModel returned error: %v", err)
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

func openRouterImageModel() sigma.ImageModel {
	return sigma.ImageModel{
		ID:       "google/gemini-2.5-flash-image",
		Provider: sigma.ProviderOpenRouter,
		API:      sigma.ImageAPIOpenRouterImages,
	}
}

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

func captureRequest(t *testing.T, ch chan<- capturedRequest, r *http.Request) {
	t.Helper()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("ReadAll request body returned error: %v", err)
	}
	ch <- capturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
		Body:    body,
	}
}

func receiveRequest(t *testing.T, ch <-chan capturedRequest) capturedRequest {
	t.Helper()

	select {
	case request := <-ch:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request")
		return capturedRequest{}
	}
}

func assertHeader(t *testing.T, headers http.Header, key, value string) {
	t.Helper()

	if got := headers.Get(key); got != value {
		t.Fatalf("header %q = %q, want %q", key, got, value)
	}
}
