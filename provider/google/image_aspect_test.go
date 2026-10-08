// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestImagePayloadMapsPortraitAndLandscapeSizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		modelID sigma.ModelID
		size    sigma.ImageSize
		want    any
	}{
		{modelID: "gemini-2.5-flash-image", size: sigma.ImageSize1024x1536, want: "2:3"},
		{modelID: "gemini-2.5-flash-image", size: sigma.ImageSize1536x1024, want: "3:2"},
		{modelID: "gemini-2.5-flash-image", size: sigma.ImageSize1024x1024, want: "1:1"},
		// Imagen has no 2:3 or 3:2 ratio, so those sizes keep the model default.
		{modelID: "imagen-4.0-generate-001", size: sigma.ImageSize1024x1536, want: nil},
	}
	for _, tt := range tests {
		t.Run(string(tt.modelID)+"/"+string(tt.size), func(t *testing.T) {
			t.Parallel()

			model := sigma.ImageModel{ID: tt.modelID, Provider: sigma.ProviderGoogle, API: sigma.ImageAPIGoogleImages}
			req := sigma.ImageRequest{Prompt: "image", Size: string(tt.size)}
			var got any
			if googleImagenModel(model.ID) {
				got = googleImagenPayload(model, req, sigma.Options{})["parameters"].(map[string]any)["aspectRatio"]
			} else {
				config, _ := googleGeminiImagePayload(model, req, sigma.Options{})["generationConfig"].(map[string]any)["imageConfig"].(map[string]any)
				got = config["aspectRatio"]
			}
			if got != tt.want {
				t.Fatalf("aspectRatio = %#v, want %#v", got, tt.want)
			}
		})
	}
}
