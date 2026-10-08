// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openrouter_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openrouter"
)

// OpenRouter rejects text output for image-only models, so the request must
// follow each model's catalog output modalities.
func TestGenerateImagesRequestsModelOutputModalities(t *testing.T) {
	t.Parallel()

	catalogModel := func(id sigma.ModelID) sigma.ImageModel {
		model, ok := sigma.GetImageModel(sigma.ProviderOpenRouter, id)
		if !ok {
			t.Fatalf("missing generated OpenRouter image model %s", id)
		}
		return model
	}
	tests := []struct {
		name  string
		model sigma.ImageModel
		opts  []sigma.ImageOption
		want  []string
	}{
		{name: "image-only catalog model", model: catalogModel("black-forest-labs/flux.2-pro"), want: []string{"image"}},
		{name: "text-capable catalog model", model: catalogModel("google/gemini-2.5-flash-image"), want: []string{"image", "text"}},
		{name: "model without modality metadata", model: openRouterImageModel(), want: []string{"image", "text"}},
		{
			name:  "explicit modalities option",
			model: catalogModel("black-forest-labs/flux.2-pro"),
			opts:  []sigma.ImageOption{sigma.WithImageProviderOption(sigma.ProviderOpenRouter, "modalities", []string{"image", "text"})},
			want:  []string{"image", "text"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,aW1hZ2U="}}]}}]}`)
			}))
			t.Cleanup(server.Close)

			registry := sigma.NewRegistry()
			if err := registry.RegisterImageProvider(sigma.ProviderOpenRouter, openrouter.NewImagesProvider(openrouter.WithImagesBaseURL(server.URL))); err != nil {
				t.Fatal(err)
			}
			model := tt.model
			model.ProviderMetadata = cloneMetadataWithoutBaseURL(model.ProviderMetadata)
			if err := registry.RegisterImageModel(model); err != nil {
				t.Fatal(err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry))
			opts := append([]sigma.ImageOption{sigma.WithImageAPIKey("key")}, tt.opts...)
			if _, err := client.GenerateImages(context.Background(), model, sigma.ImageRequest{Prompt: "a cat"}, opts...); err != nil {
				t.Fatalf("GenerateImages returned error: %v", err)
			}

			var payload struct {
				Modalities []string `json:"modalities"`
			}
			if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(payload.Modalities) != fmt.Sprint(tt.want) {
				t.Fatalf("modalities = %v, want %v", payload.Modalities, tt.want)
			}
		})
	}
}

// cloneMetadataWithoutBaseURL routes generated models to the test server.
func cloneMetadataWithoutBaseURL(metadata map[string]any) map[string]any {
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if key != "baseURL" {
			cloned[key] = value
		}
	}
	return cloned
}
