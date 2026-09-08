// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/wintermi/sigma"
)

func TestChatAlternativesValidateFinalOverrides(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAICompletions, ID: "test", ProviderMetadata: map[string]any{sigma.MetadataOpenAISamplingParameters: map[string]any{"n": 2}}}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}
	for _, value := range []any{1, json.Number("1.0"), 2, 0, nil, "1", true} {
		opts := sigma.Options{OpenAIOptions: &sigma.OpenAIOptions{SamplingParameters: map[string]any{"n": 3}}, ProviderOptions: map[sigma.ProviderID]map[string]any{model.Provider: {"extra_body": map[string]any{"n": value}}}}
		_, err := chatCompletionsPayload(model, req, opts, completionsCompat{})
		valid := value == 1 || value == json.Number("1.0")
		if valid && err != nil || !valid && !errors.Is(err, sigma.ErrInvalidOptions) {
			t.Errorf("n=%#v: %v", value, err)
		}
	}
	if _, err := chatCompletionsPayload(model, req, sigma.Options{}, completionsCompat{}); !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("model default n=2: %v", err)
	}
	model.ProviderMetadata = nil
	if _, err := chatCompletionsPayload(model, req, sigma.Options{}, completionsCompat{}); err != nil {
		t.Fatal(err)
	}
}
