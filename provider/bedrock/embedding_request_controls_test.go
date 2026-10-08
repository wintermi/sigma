// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"fmt"
	"testing"

	"github.com/wintermi/sigma"
)

// The neutral Dimensions and InputType fields must reach every Bedrock
// embedding family that supports them; explicit provider options still win.
func TestBedrockEmbeddingPayloadsApplyNeutralRequestControls(t *testing.T) {
	t.Parallel()

	cohereV4 := bedrockEmbeddingModel("cohere.embed-v4:0")
	cohereV4.MinDimensions, cohereV4.MaxDimensions = 256, 1536
	cohereV3 := bedrockEmbeddingModel("cohere.embed-english-v3")
	cohereV3.MinDimensions, cohereV3.MaxDimensions = 1024, 1024
	query := sigma.EmbeddingInputTypeQuery
	tests := []struct {
		name  string
		model sigma.EmbeddingModel
		req   sigma.EmbeddingRequest
		opts  map[string]any
		path  []string
		want  any
	}{
		{name: "nova dimensions", model: bedrockEmbeddingModel("amazon.nova-2-multimodal-embeddings-v1:0"), req: sigma.EmbeddingRequest{Inputs: []string{"a"}, Dimensions: 1024}, path: []string{"singleEmbeddingParams", "embeddingDimension"}, want: 1024},
		{name: "nova query purpose", model: bedrockEmbeddingModel("amazon.nova-2-multimodal-embeddings-v1:0"), req: sigma.EmbeddingRequest{Inputs: []string{"a"}, InputType: query}, path: []string{"singleEmbeddingParams", "embeddingPurpose"}, want: "GENERIC_RETRIEVAL"},
		{name: "nova document purpose", model: bedrockEmbeddingModel("amazon.nova-2-multimodal-embeddings-v1:0"), req: sigma.EmbeddingRequest{Inputs: []string{"a"}}, path: []string{"singleEmbeddingParams", "embeddingPurpose"}, want: "GENERIC_INDEX"},
		{name: "nova option wins", model: bedrockEmbeddingModel("amazon.nova-2-multimodal-embeddings-v1:0"), req: sigma.EmbeddingRequest{Inputs: []string{"a"}, Dimensions: 1024}, opts: map[string]any{"embeddingDimension": 256}, path: []string{"singleEmbeddingParams", "embeddingDimension"}, want: 256},
		{name: "titan image dimensions", model: bedrockEmbeddingModel("amazon.titan-embed-image-v1"), req: sigma.EmbeddingRequest{Inputs: []string{"a"}, Dimensions: 384}, path: []string{"embeddingConfig", "outputEmbeddingLength"}, want: 384},
		{name: "cohere variable dimensions", model: cohereV4, req: sigma.EmbeddingRequest{Inputs: []string{"a"}, Dimensions: 512}, path: []string{"output_dimension"}, want: 512},
		{name: "cohere fixed dimensions", model: cohereV3, req: sigma.EmbeddingRequest{Inputs: []string{"a"}, Dimensions: 1024}, path: []string{"output_dimension"}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := sigma.Options{}
			if tt.opts != nil {
				opts.ProviderOptions = map[sigma.ProviderID]map[string]any{sigma.ProviderAmazonBedrock: tt.opts}
			}
			payload, err := bedrockEmbeddingPayload(string(tt.model.ID), tt.model, tt.req, opts)
			if err != nil {
				t.Fatal(err)
			}
			var got any = payload
			for _, key := range tt.path {
				object, _ := got.(map[string]any)
				got = object[key]
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("%v = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
