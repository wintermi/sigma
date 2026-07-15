// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestModelMetadataJSONRoundTrip(t *testing.T) {
	t.Parallel()

	model := sigma.Model{
		ID:               "gpt-4.1",
		Provider:         sigma.ProviderOpenAI,
		API:              sigma.APIOpenAIResponses,
		Name:             "GPT-4.1",
		ContextWindow:    1_000_000,
		MaxOutputTokens:  32_768,
		SupportsTools:    true,
		SupportsThinking: true,
		ThinkingLevels:   []sigma.ThinkingLevel{sigma.ThinkingLevelLow, sigma.ThinkingLevelHigh},
		UnsupportedThinkingLevels: []sigma.ThinkingLevel{
			sigma.ThinkingLevelOff,
		},
		DefaultTransport: sigma.TransportSSE,
		AnthropicMessagesCompat: &sigma.AnthropicMessagesCompat{
			SupportsTemperature: sigma.AnthropicCompatUnsupported,
		},
		OpenAIResponsesCompat: &sigma.OpenAIResponsesCompat{
			SessionAffinityFormat: sigma.OpenAIResponsesSessionAffinityOpenAINoSession,
		},
		ProviderMetadata: map[string]any{
			"family": "gpt",
		},
	}

	roundTripped := assertJSONRoundTrip(t, model)
	if roundTripped.Provider != sigma.ProviderOpenAI {
		t.Fatalf("provider changed after round trip: got %q", roundTripped.Provider)
	}
	if len(roundTripped.UnsupportedThinkingLevels) != 1 || roundTripped.UnsupportedThinkingLevels[0] != sigma.ThinkingLevelOff {
		t.Fatalf("unsupported thinking levels changed after round trip: %#v", roundTripped.UnsupportedThinkingLevels)
	}
	if roundTripped.AnthropicMessagesCompat == nil ||
		roundTripped.AnthropicMessagesCompat.SupportsTemperature != sigma.AnthropicCompatUnsupported {
		t.Fatalf("anthropic compat changed after round trip: %#v", roundTripped.AnthropicMessagesCompat)
	}
	if roundTripped.OpenAIResponsesCompat == nil ||
		roundTripped.OpenAIResponsesCompat.SessionAffinityFormat != sigma.OpenAIResponsesSessionAffinityOpenAINoSession {
		t.Fatalf("responses compat changed after round trip: %#v", roundTripped.OpenAIResponsesCompat)
	}
}

func TestImageTypesJSONRoundTrip(t *testing.T) {
	t.Parallel()

	imageModel := sigma.ImageModel{
		ID:               "image-1",
		Provider:         sigma.ProviderOpenAI,
		API:              "openai-images",
		Name:             "Image 1",
		MaxWidth:         2048,
		MaxHeight:        2048,
		SupportedSizes:   []string{"1024x1024", "1024x1536"},
		SupportedFormats: []string{"image/png", "image/jpeg"},
		ProviderMetadata: map[string]any{"quality": "high"},
	}

	imageRequest := sigma.ImageRequest{
		Model:    imageModel.ID,
		Provider: imageModel.Provider,
		Prompt:   "A clean product render",
		Inputs: []sigma.ImageInput{
			{
				Type:     "image",
				MIMEType: "image/png",
				Source:   "base64",
				Data:     "aGVsbG8=",
			},
		},
		Size:     "1024x1024",
		MIMEType: "image/png",
		Count:    1,
	}

	assistantImages := sigma.AssistantImages{
		Images: []sigma.ImageInput{
			{
				Type:     "image",
				MIMEType: "image/png",
				Source:   "url",
				URL:      "https://example.test/output.png",
			},
		},
		Usage:    &sigma.Usage{InputTokens: 12, OutputTokens: 1, TotalTokens: 13},
		Cost:     &sigma.Cost{TotalCost: 0.01, Currency: "USD"},
		Model:    imageModel.ID,
		Provider: imageModel.Provider,
	}

	assertJSONRoundTrip(t, imageModel)
	assertJSONRoundTrip(t, imageRequest)
	roundTripped := assertJSONRoundTrip(t, assistantImages)
	if got, want := len(roundTripped.Images), 1; got != want {
		t.Fatalf("image count changed after round trip: got %d want %d", got, want)
	}
}
