// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/google"
)

func TestRegisterReportsEmbeddingAndImageAPIs(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("google-surfaces")
	if err := google.RegisterEmbeddings(registry, providerID); err != nil {
		t.Fatalf("RegisterEmbeddings returned error: %v", err)
	}
	if err := google.RegisterImages(registry, providerID); err != nil {
		t.Fatalf("RegisterImages returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := providers[0].EmbeddingAPI, sigma.EmbeddingAPIGoogleEmbeddings; got != want {
		t.Fatalf("embedding API = %q, want %q", got, want)
	}
	if got, want := providers[0].ImageAPI, sigma.ImageAPIGoogleImages; got != want {
		t.Fatalf("image API = %q, want %q", got, want)
	}
}

func TestGoogleEmbeddingsSendsBatchPayloadAndMapsResponse(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"embeddings":[{"values":[0.1,0.2]},{"values":[0.3,0.4]}]}`)
	}))
	t.Cleanup(server.Close)

	model := googleEmbeddingModel(sigma.ProviderGoogle)
	client := googleEmbeddingTestClient(t, model, google.NewEmbeddingsProvider(google.WithBaseURL(server.URL)))
	got, err := client.Embed(
		context.Background(),
		model,
		sigma.EmbeddingRequest{
			Inputs:     []string{"alpha", "beta"},
			Dimensions: 128,
			InputType:  sigma.EmbeddingInputTypeDocument,
		},
		sigma.WithEmbeddingProviderOption(model.Provider, "task_type", "CLASSIFICATION"),
		sigma.WithEmbeddingAPIKey("google-key"),
	)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if gotVectors := got.Vectors; !reflect.DeepEqual(gotVectors, []sigma.Embedding{
		{Index: 0, Vector: []float32{0.1, 0.2}},
		{Index: 1, Vector: []float32{0.3, 0.4}},
	}) {
		t.Fatalf("vectors = %#v", gotVectors)
	}

	request := receiveRequest(t, requests)
	if gotPath, want := request.Path, "/models/text-embedding-004:batchEmbedContents"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	assertHeader(t, request.Headers, "X-Goog-Api-Key", "google-key")
	payload := decodeRequestPayload(t, request.Body)
	requestItems := payload["requests"].([]any)
	first := requestItems[0].(map[string]any)
	if gotTask, want := first["taskType"], "CLASSIFICATION"; gotTask != want {
		t.Fatalf("taskType = %v, want %q", gotTask, want)
	}
	if gotDims, want := first["outputDimensionality"], float64(128); gotDims != want {
		t.Fatalf("outputDimensionality = %v, want %v", gotDims, want)
	}
}

func TestVertexEmbeddingsRoutesThroughProjectLocationAndMapsUsage(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"predictions": [
				{"embeddings":{"values":[0.5,0.6],"statistics":{"token_count":3}}},
				{"embeddings":{"values":[0.7,0.8],"statistics":{"token_count":4}}}
			]
		}`)
	}))
	t.Cleanup(server.Close)

	model := googleVertexEmbeddingModel()
	client := googleEmbeddingTestClient(
		t,
		model,
		google.NewVertexEmbeddingsProvider(
			google.WithVertexBaseURL(server.URL+"/v1"),
			google.WithVertexConfig(google.VertexConfig{ProjectID: "project-123", Location: "us-central1", CredentialMode: google.VertexCredentialAPIKey}),
		),
	)
	got, err := client.Embed(
		context.Background(),
		model,
		sigma.EmbeddingRequest{Inputs: []string{"alpha", "beta"}, InputType: sigma.EmbeddingInputTypeQuery},
		sigma.WithEmbeddingAPIKey("vertex-key"),
	)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if got.Usage == nil || got.Usage.InputTokens != 7 || got.Usage.TotalTokens != 7 {
		t.Fatalf("usage = %#v, want summed token count", got.Usage)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Path, "/v1/projects/project-123/locations/us-central1/publishers/google/models/text-embedding-004:predict"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "X-Goog-Api-Key", "vertex-key")
	payload := decodeRequestPayload(t, request.Body)
	instances := payload["instances"].([]any)
	first := instances[0].(map[string]any)
	if gotTask, want := first["task_type"], "RETRIEVAL_QUERY"; gotTask != want {
		t.Fatalf("task_type = %v, want %q", gotTask, want)
	}
}

func TestGoogleImagenSendsPredictPayloadAndMapsBase64Response(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U=","mimeType":"image/png","prompt":"revised prompt"}]}`)
	}))
	t.Cleanup(server.Close)

	model := googleImageModel(sigma.ProviderGoogle, "imagen-4.0-generate-001", sigma.ImageAPIGoogleImages)
	client := googleImageTestClient(t, model, google.NewImagesProvider(google.WithBaseURL(server.URL)))
	got, err := client.GenerateImages(
		context.Background(),
		model,
		sigma.ImageRequest{Prompt: "draw a diagram", Size: "1024x1024", Count: 2},
		sigma.WithImageAPIKey("google-key"),
		sigma.WithImageProviderOption(model.Provider, "negative_prompt", "text"),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	if got.StopReason != sigma.StopReasonEndTurn || len(got.Images) != 1 || got.Images[0].Data != "aW1hZ2U=" {
		t.Fatalf("images = %#v stop = %q", got.Images, got.StopReason)
	}

	request := receiveRequest(t, requests)
	if gotPath, want := request.Path, "/models/imagen-4.0-generate-001:predict"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	payload := decodeRequestPayload(t, request.Body)
	parameters := payload["parameters"].(map[string]any)
	if gotCount, want := parameters["sampleCount"], float64(2); gotCount != want {
		t.Fatalf("sampleCount = %v, want %v", gotCount, want)
	}
	if gotAspect, want := parameters["aspectRatio"], "1:1"; gotAspect != want {
		t.Fatalf("aspectRatio = %v, want %q", gotAspect, want)
	}
}

func TestGoogleGeminiImageUsesGenerateContentAndInlineDataResponse(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"caption"},{"inlineData":{"mimeType":"image/jpeg","data":"anBlZw=="}}]}}]}`)
	}))
	t.Cleanup(server.Close)

	model := googleImageModel(sigma.ProviderGoogle, "gemini-2.5-flash-image", sigma.ImageAPIGoogleImages)
	client := googleImageTestClient(t, model, google.NewImagesProvider(google.WithBaseURL(server.URL)))
	got, err := client.GenerateImages(
		context.Background(),
		model,
		sigma.ImageRequest{Prompt: "draw", Count: 1, Size: "16:9"},
		sigma.WithImageAPIKey("google-key"),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	if len(got.Images) != 2 || got.Images[0].Text != "caption" || got.Images[1].MIMEType != "image/jpeg" {
		t.Fatalf("images = %#v", got.Images)
	}

	request := receiveRequest(t, requests)
	if gotPath, want := request.Path, "/models/gemini-2.5-flash-image:generateContent"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	payload := decodeRequestPayload(t, request.Body)
	config := payload["generationConfig"].(map[string]any)
	modalities := config["responseModalities"].([]any)
	if len(modalities) != 2 || modalities[0] != "TEXT" || modalities[1] != "IMAGE" {
		t.Fatalf("responseModalities = %#v", modalities)
	}
}

func TestGoogleGeminiImageOmitsUnsupportedImageCount(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	}))
	t.Cleanup(server.Close)

	providerID := sigma.ProviderID("google-gemini-image-count-test")
	model := googleImageModel(providerID, "gemini-2.5-flash-image", sigma.ImageAPIGoogleImages)
	client := googleImageTestClient(t, model, google.NewImagesProvider(google.WithBaseURL(server.URL)))

	_, err := client.GenerateImages(
		context.Background(),
		model,
		sigma.ImageRequest{Prompt: "draw one"},
		sigma.WithImageAPIKey("google-key"),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	request := receiveRequest(t, requests)
	var payload map[string]any
	if err := json.Unmarshal([]byte(request.Body), &payload); err != nil {
		t.Fatalf("Unmarshal request body returned error: %v", err)
	}
	generation := payload["generationConfig"].(map[string]any)
	if _, ok := generation["numberOfImages"]; ok {
		t.Fatalf("numberOfImages was sent in generationConfig: %#v", generation)
	}

	_, err = client.GenerateImages(
		context.Background(),
		model,
		sigma.ImageRequest{Prompt: "draw two", Count: 2},
		sigma.WithImageAPIKey("google-key"),
	)
	if err == nil {
		t.Fatal("GenerateImages returned nil error")
	}
	if !strings.Contains(err.Error(), "supports one image") {
		t.Fatalf("error = %v, want local count rejection", err)
	}
}

func TestVertexImagenRoutesThroughProjectLocation(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"predictions":[{"bytesBase64Encoded":"dmVydGV4","mimeType":"image/png"}]}`)
	}))
	t.Cleanup(server.Close)

	model := googleImageModel(sigma.ProviderGoogleVertex, "imagen-4.0-generate-001", sigma.ImageAPIGoogleVertexImages)
	client := googleImageTestClient(
		t,
		model,
		google.NewVertexImagesProvider(
			google.WithVertexBaseURL(server.URL+"/v1"),
			google.WithVertexConfig(google.VertexConfig{ProjectID: "project-123", Location: "us-central1", CredentialMode: google.VertexCredentialAPIKey}),
		),
	)
	got, err := client.GenerateImages(
		context.Background(),
		model,
		sigma.ImageRequest{Prompt: "draw"},
		sigma.WithImageAPIKey("vertex-key"),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	if len(got.Images) != 1 || got.Images[0].Data != "dmVydGV4" {
		t.Fatalf("images = %#v", got.Images)
	}
	request := receiveRequest(t, requests)
	if got, want := request.Path, "/v1/projects/project-123/locations/us-central1/publishers/google/models/imagen-4.0-generate-001:predict"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	assertHeader(t, request.Headers, "X-Goog-Api-Key", "vertex-key")
}

func googleEmbeddingTestClient(t *testing.T, model sigma.EmbeddingModel, provider sigma.EmbeddingProvider) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := registry.RegisterEmbeddingProvider(model.Provider, provider); err != nil {
		t.Fatalf("RegisterEmbeddingProvider returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModel(model); err != nil {
		t.Fatalf("RegisterEmbeddingModel returned error: %v", err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func googleImageTestClient(t *testing.T, model sigma.ImageModel, provider sigma.ImageProvider) *sigma.Client {
	t.Helper()

	registry := sigma.NewRegistry()
	if err := registry.RegisterImageProvider(model.Provider, provider); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	if err := registry.RegisterImageModel(model); err != nil {
		t.Fatalf("RegisterImageModel returned error: %v", err)
	}
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func googleEmbeddingModel(providerID sigma.ProviderID) sigma.EmbeddingModel {
	return sigma.EmbeddingModel{
		ID:                  "text-embedding-004",
		Provider:            providerID,
		API:                 sigma.EmbeddingAPIGoogleEmbeddings,
		InputCostPerMillion: 1,
		CostCurrency:        "USD",
	}
}

func googleVertexEmbeddingModel() sigma.EmbeddingModel {
	model := googleEmbeddingModel(sigma.ProviderGoogleVertex)
	model.API = sigma.EmbeddingAPIGoogleVertexEmbeddings
	return model
}

func googleImageModel(providerID sigma.ProviderID, id sigma.ModelID, api sigma.ImageAPI) sigma.ImageModel {
	return sigma.ImageModel{
		ID:       id,
		Provider: providerID,
		API:      api,
	}
}
