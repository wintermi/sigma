// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wintermi/sigma"
)

func TestGoogleCandidateCountValidationAfterHooks(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderGoogle, API: sigma.APIGoogleGenerativeAI, ID: "test"}
	for _, value := range []any{1, json.Number("1e0"), 2, 0, nil, "1", true} {
		err := validateSingleCandidate(model, map[string]any{"generationConfig": map[string]any{"candidateCount": value}})
		valid := value == 1 || value == json.Number("1e0")
		if valid && err != nil || !valid && !errors.Is(err, sigma.ErrInvalidOptions) {
			t.Errorf("count=%#v: %v", value, err)
		}
	}
	hook := func(_ context.Context, _ sigma.Model, _ sigma.Request, _ sigma.Options, payload map[string]any) error {
		payload["generationConfig"] = map[string]any{"candidateCount": 2}
		return nil
	}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}
	opts := sigma.Options{ProviderOptions: map[sigma.ProviderID]map[string]any{model.Provider: {"candidateCount": 1}}}
	if _, err := NewProvider(WithPayloadHook(hook)).newRequest(context.Background(), model, req, opts); !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("Gemini hook: %v", err)
	}
	model.Provider = sigma.ProviderGoogleVertex
	model.API = sigma.APIGoogleVertex
	if _, err := NewVertexProvider(WithVertexPayloadHook(hook)).newRequest(context.Background(), model, req, opts); !errors.Is(err, sigma.ErrInvalidOptions) {
		t.Fatalf("Vertex hook: %v", err)
	}
}
