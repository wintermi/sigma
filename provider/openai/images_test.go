// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/goldentest"
	"github.com/wintermi/sigma/provider/openai"
)

func TestRegisterImagesReportsOpenAIImagesAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := openai.RegisterImages(registry, sigma.ProviderOpenAI); err != nil {
		t.Fatalf("RegisterImages returned error: %v", err)
	}
	if err := registry.RegisterImageModel(openAIImageModel()); err != nil {
		t.Fatalf("RegisterImageModel returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].ImageAPI, sigma.ImageAPIOpenAIImages; got != want {
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
			"created": 1713833628,
			"background": "transparent",
			"output_format": "webp",
			"quality": "high",
			"size": "1536x1024",
			"data": [
				{"b64_json": "aW1hZ2U=", "revised_prompt": "A revised ceramic robot prompt."},
				{"url": "https://example.test/generated.webp"}
			],
			"usage": {
				"input_tokens": 50,
				"output_tokens": 75,
				"total_tokens": 125,
				"input_tokens_details": {"text_tokens": 12, "image_tokens": 38},
				"output_tokens_details": {"text_tokens": 0, "image_tokens": 75}
			}
		}`)
	}))
	t.Cleanup(server.Close)

	client := openAIImagesTestClient(t, server.URL, openai.WithHeader("X-Provider", "provider"))
	got, err := client.GenerateImages(
		context.Background(),
		openAIImageModel(),
		sigma.ImageRequest{
			Model:    "gpt-image-test-override",
			Prompt:   "A ceramic robot watering herbs",
			Size:     string(sigma.ImageSize1536x1024),
			Quality:  string(sigma.ImageQualityHigh),
			MIMEType: "image/webp",
			Count:    2,
		},
		sigma.WithImageAPIKey("request-key"),
		sigma.WithImageHeader("X-Custom", "custom"),
		sigma.WithImageProviderOptions(sigma.ProviderOpenAI, map[string]any{
			"organization": "org_123",
			"project":      "proj_123",
			"extra_body": map[string]any{
				"background":         "transparent",
				"moderation":         "low",
				"output_compression": 80,
			},
		}),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	if got.StopReason != sigma.StopReasonEndTurn {
		t.Fatalf("stop reason = %q, want end-turn", got.StopReason)
	}
	if got.Model != "gpt-image-1" {
		t.Fatalf("model = %q, want gpt-image-1", got.Model)
	}
	if got.Provider != sigma.ProviderOpenAI {
		t.Fatalf("provider = %q, want openai", got.Provider)
	}
	if got.Usage == nil || got.Usage.InputTokens != 50 || got.Usage.OutputTokens != 75 || got.Usage.TotalTokens != 125 {
		t.Fatalf("usage = %#v, want mapped usage", got.Usage)
	}
	if want := []sigma.ImageInput{
		sigma.ImageOutputData("image/webp", "aW1hZ2U="),
		sigma.ImageOutputURL("", "https://example.test/generated.webp"),
	}; !reflect.DeepEqual(got.Images, want) {
		t.Fatalf("images = %#v, want %#v", got.Images, want)
	}
	if got.ProviderMetadata["created"] != int64(1713833628) {
		t.Fatalf("created metadata = %#v, want 1713833628", got.ProviderMetadata["created"])
	}
	if got.ProviderMetadata["revised_prompt"] != "A revised ceramic robot prompt." {
		t.Fatalf("revised prompt metadata = %#v, want revised prompt", got.ProviderMetadata["revised_prompt"])
	}
	usageMetadata := got.ProviderMetadata["usage"].(map[string]any)
	inputDetails := usageMetadata["input_tokens_details"].(map[string]any)
	if got, want := inputDetails["image_tokens"], 38; got != want {
		t.Fatalf("image token metadata = %v, want %v", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/images/generations"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "Authorization", "Bearer request-key")
	assertHeader(t, request.Headers, "OpenAI-Organization", "org_123")
	assertHeader(t, request.Headers, "OpenAI-Project", "proj_123")
	assertHeader(t, request.Headers, "X-Client", "client")
	assertHeader(t, request.Headers, "X-Provider", "provider")
	assertHeader(t, request.Headers, "X-Custom", "custom")
	goldentest.AssertJSON(t, request.Body, "provider/openai/images/basic_payload.json")
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

	client := openAIImagesTestClient(t, server.URL)
	response, err := client.GenerateImages(context.Background(), openAIImageModel(), sigma.ImageRequest{Prompt: "hi"})
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
	if got, want := providerErr.API, sigma.API(sigma.ImageAPIOpenAIImages); got != want {
		t.Fatalf("provider error API = %q, want %q", got, want)
	}
	if providerErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", providerErr.StatusCode)
	}
	if providerErr.RetryAfter != 2*time.Second {
		t.Fatalf("retry after = %s, want 2s", providerErr.RetryAfter)
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

	client := openAIImagesTestClient(t, server.URL)
	response, err := client.GenerateImages(
		context.Background(),
		openAIImageModel(),
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

func TestGenerateImagesValidatesBeforeNetwork(t *testing.T) {
	t.Parallel()

	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	t.Cleanup(server.Close)

	client := openAIImagesTestClient(t, server.URL)
	tests := []struct {
		name string
		req  sigma.ImageRequest
		want string
	}{
		{
			name: "empty prompt",
			req:  sigma.ImageRequest{},
			want: "prompt is required",
		},
		{
			name: "variation without explicit operation",
			req: sigma.ImageRequest{
				Inputs: []sigma.ImageInput{
					sigma.ImageData("image/png", "aW5wdXQ="),
				},
				Operation: sigma.ImageOperationVariation,
			},
			want: "dall-e-2",
		},
		{
			name: "unsupported mime",
			req: sigma.ImageRequest{
				Prompt:   "hi",
				MIMEType: "image/gif",
			},
			want: "unsupported output MIME type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GenerateImages(context.Background(), openAIImageModel(), tt.req)
			if err == nil {
				t.Fatal("GenerateImages returned nil error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
	if called {
		t.Fatal("server was called for locally invalid request")
	}
}

func TestGenerateImagesSendsEditMultipartPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"ZWRpdA=="}]}`)
	}))
	t.Cleanup(server.Close)

	client := openAIImagesTestClient(t, server.URL)
	_, err := client.GenerateImages(
		context.Background(),
		openAIImageModel(),
		sigma.ImageRequest{
			Prompt:   "keep the product, change the background",
			Inputs:   []sigma.ImageInput{sigma.ImageData("image/png", "aW5wdXQ="), sigma.ImageFileID("file_123")},
			Mask:     imageInputPtr(sigma.ImageData("image/png", "bWFzaw==")),
			Size:     string(sigma.ImageSize1024x1024),
			Quality:  string(sigma.ImageQualityMedium),
			MIMEType: "image/png",
			Count:    1,
		},
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	request := receiveRequest(t, requests)
	if got, want := request.Path, "/images/edits"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	form := parseMultipartRequest(t, request)
	if got, want := form.Value["prompt"], []string{"keep the product, change the background"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prompt = %#v, want %#v", got, want)
	}
	if got, want := form.Value["model"], []string{"gpt-image-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model = %#v, want %#v", got, want)
	}
	if got, want := form.Value["image_file_id"], []string{"file_123"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("image_file_id = %#v, want %#v", got, want)
	}
	if got, want := len(form.File["image"]), 1; got != want {
		t.Fatalf("image file count = %d, want %d", got, want)
	}
	if got, want := len(form.File["mask"]), 1; got != want {
		t.Fatalf("mask file count = %d, want %d", got, want)
	}
}

func TestGenerateImagesSendsReferenceEditJSONPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"cmVmZXJlbmNl"}]}`)
	}))
	t.Cleanup(server.Close)

	client := openAIImagesTestClient(t, server.URL)
	_, err := client.GenerateImages(
		context.Background(),
		openAIImageModel(),
		sigma.ImageRequest{
			Prompt: "change the background",
			Inputs: []sigma.ImageInput{
				sigma.ImageOutputURL("image/png", "https://example.test/input.png"),
				sigma.ImageFileID("file_123"),
			},
			Mask:     imageInputPtr(sigma.ImageOutputURL("image/png", "https://example.test/mask.png")),
			Size:     string(sigma.ImageSize1024x1536),
			Quality:  string(sigma.ImageQualityHigh),
			MIMEType: "image/webp",
			Count:    2,
		},
		sigma.WithImageProviderOptions(sigma.ProviderOpenAI, map[string]any{
			"extra_body": map[string]any{
				"background":     "transparent",
				"input_fidelity": "high",
			},
		}),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	request := receiveRequest(t, requests)
	if got, want := request.Path, "/images/edits"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("Content-Type"), "application/json"; got != want {
		t.Fatalf("content type = %q, want %q", got, want)
	}
	var payload map[string]any
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	want := map[string]any{
		"model":          "gpt-image-1",
		"prompt":         "change the background",
		"images":         []any{map[string]any{"image_url": "https://example.test/input.png"}, map[string]any{"file_id": "file_123"}},
		"mask":           map[string]any{"image_url": "https://example.test/mask.png"},
		"size":           string(sigma.ImageSize1024x1536),
		"quality":        string(sigma.ImageQualityHigh),
		"output_format":  "webp",
		"n":              float64(2),
		"background":     "transparent",
		"input_fidelity": "high",
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload = %#v, want %#v", payload, want)
	}
}

func TestGenerateImagesSendsVariationMultipartPayload(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"url":"https://example.test/variation.png"}]}`)
	}))
	t.Cleanup(server.Close)

	client := openAIImagesTestClient(t, server.URL)
	_, err := client.GenerateImages(
		context.Background(),
		openAIImageModel(),
		sigma.ImageRequest{
			Model:     "dall-e-2",
			Operation: sigma.ImageOperationVariation,
			Inputs:    []sigma.ImageInput{sigma.ImageData("image/png", "aW5wdXQ=")},
			Size:      string(sigma.ImageSize1024x1024),
			Count:     1,
		},
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	request := receiveRequest(t, requests)
	if got, want := request.Path, "/images/variations"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	form := parseMultipartRequest(t, request)
	if got, want := form.Value["model"], []string{"dall-e-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model = %#v, want %#v", got, want)
	}
	if got, want := len(form.File["image"]), 1; got != want {
		t.Fatalf("image file count = %d, want %d", got, want)
	}
}

func TestStreamImagesSendsStreamingPayloadAndCollectsEvents(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"image_generation.partial_image","partial_image_index":0,"b64_json":"cGFydGlhbA=="}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"image_generation.completed","data":[{"b64_json":"ZmluYWw="}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	client := openAIImagesTestClient(t, server.URL)
	stream := client.StreamImages(
		context.Background(),
		openAIImageModel(),
		sigma.ImageRequest{Prompt: "draw", MIMEType: "image/png"},
		sigma.WithImageProviderOption(sigma.ProviderOpenAI, "partial_images", 1),
	)
	var events []sigma.ImageEvent
	for event := range stream.Events() {
		events = append(events, event)
	}
	final, err := sigma.CollectImages(context.Background(), stream)
	if err != nil {
		t.Fatalf("CollectImages returned error: %v", err)
	}
	if got, want := imageEventKinds(events), []sigma.ImageEventKind{
		sigma.ImageEventKindStart,
		sigma.ImageEventKindPartial,
		sigma.ImageEventKindImage,
		sigma.ImageEventKindDone,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if got, want := final.Images, []sigma.ImageInput{sigma.ImageOutputData("image/png", "ZmluYWw=")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("images = %#v, want %#v", got, want)
	}
	request := receiveRequest(t, requests)
	goldentest.AssertJSON(t, request.Body, "provider/openai/images/stream_payload.json")
}

func TestGenerateImagesRejectsInvalidBaseURL(t *testing.T) {
	t.Parallel()

	client := openAIImagesTestClient(t, "://bad")
	_, err := client.GenerateImages(context.Background(), openAIImageModel(), sigma.ImageRequest{Prompt: "hi"})
	if err == nil {
		t.Fatal("GenerateImages returned nil error")
	}
	if !strings.Contains(err.Error(), "invalid base URL") {
		t.Fatalf("error = %v, want invalid base URL", err)
	}
}

func TestGenerateImagesRejectsInvalidEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server was called for invalid endpoint")
	}))
	t.Cleanup(server.Close)

	client := openAIImagesTestClient(t, server.URL)
	_, err := client.GenerateImages(
		context.Background(),
		openAIImageModel(),
		sigma.ImageRequest{Prompt: "hi"},
		sigma.WithImageProviderOption(sigma.ProviderOpenAI, "endpoint", "://bad"),
	)
	if err == nil {
		t.Fatal("GenerateImages returned nil error")
	}
	if !strings.Contains(err.Error(), "invalid endpoint") {
		t.Fatalf("error = %v, want invalid endpoint", err)
	}
}

func TestGenerateImagesRunsDebugHooksWithRedactedCopies(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_debug")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}]}`)
	}))
	t.Cleanup(server.Close)

	var payloadDebug sigma.ImagePayloadDebug
	var responseDebug sigma.ImageResponseDebug
	client := openAIImagesTestClient(t, server.URL)
	_, err := client.GenerateImages(
		context.Background(),
		openAIImageModel(),
		sigma.ImageRequest{Prompt: "use sk-secret123456"},
		sigma.WithImageAPIKey("sk-request123456"),
		sigma.WithImagePayloadDebugHook(func(_ context.Context, debug sigma.ImagePayloadDebug) error {
			payloadDebug = debug
			debug.Headers.Set("Authorization", "mutated")
			return nil
		}),
		sigma.WithImageResponseDebugHook(func(_ context.Context, debug sigma.ImageResponseDebug) error {
			responseDebug = debug
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	if got := payloadDebug.Headers.Get("Authorization"); strings.Contains(got, "sk-request123456") {
		t.Fatalf("debug authorization header leaked secret: %q", got)
	}
	if strings.Contains(payloadDebug.PayloadPreview, "sk-secret123456") {
		t.Fatalf("debug payload leaked secret: %q", payloadDebug.PayloadPreview)
	}
	if got, want := responseDebug.RequestID, "req_debug"; got != want {
		t.Fatalf("debug request id = %q, want %q", got, want)
	}
	if got, want := responseDebug.API, sigma.ImageAPIOpenAIImages; got != want {
		t.Fatalf("debug API = %q, want %q", got, want)
	}
}

func openAIImagesTestClient(t *testing.T, baseURL string, opts ...openai.ProviderOption) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	providerOpts := append([]openai.ProviderOption{openai.WithBaseURL(baseURL)}, opts...)
	if err := registry.RegisterImageProvider(sigma.ProviderOpenAI, openai.NewImagesProvider(providerOpts...)); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	if err := registry.RegisterImageModel(openAIImageModel()); err != nil {
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

func openAIImageModel() sigma.ImageModel {
	return sigma.ImageModel{
		ID:       "gpt-image-1",
		Provider: sigma.ProviderOpenAI,
		API:      sigma.ImageAPIOpenAIImages,
		ProviderMetadata: map[string]any{
			"headers": map[string]string{
				"X-Model": "model",
			},
		},
	}
}

func parseMultipartRequest(t *testing.T, request capturedRequest) *multipart.Form {
	t.Helper()

	mediaType, params, err := mime.ParseMediaType(request.Headers.Get("Content-Type"))
	if err != nil {
		t.Fatalf("ParseMediaType returned error: %v", err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("content type = %q, want multipart/form-data", mediaType)
	}
	reader := multipart.NewReader(bytes.NewReader(request.Body), params["boundary"])
	form, err := reader.ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("ReadForm returned error: %v", err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form
}

func imageInputPtr(input sigma.ImageInput) *sigma.ImageInput {
	return &input
}

func imageEventKinds(events []sigma.ImageEvent) []sigma.ImageEventKind {
	kinds := make([]sigma.ImageEventKind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	return kinds
}
