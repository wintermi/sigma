// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestVertexCatalogMaaSRequests(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		id     sigma.ModelID
		level  sigma.ThinkingLevel
		effort string
	}{
		{id: "meta/llama-3.3-70b-instruct-maas"},
		{id: "meta/llama-4-maverick-17b-128e-instruct-maas"},
		{id: "openai/gpt-oss-120b-maas", level: sigma.ThinkingLevelHigh, effort: "high"},
		{id: "xai/grok-4.20-non-reasoning"},
		{id: "xai/grok-4.20-reasoning"},
		{id: "xai/grok-4.3"},
		{id: "xai/grok-4.6"},
		{id: "zai-org/glm-5.2-maas"},
	} {
		t.Run(string(tt.id)+"/"+string(tt.level), func(t *testing.T) {
			t.Parallel()
			model, ok := sigma.GetModel(sigma.ProviderGoogleVertexOpenAI, tt.id)
			if !ok {
				t.Fatal("missing MaaS model")
			}
			opts := sigma.Options{ReasoningLevel: tt.level, AuthResolver: sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
				return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "fixture"}, nil
			})}
			provider := NewVertexProvider(WithVertexConfig(VertexConfig{ProjectID: "fixture-project", Location: "global"}))
			req, err := provider.newRequest(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer req.Body.Close()
			if !strings.HasSuffix(req.URL.Path, "/endpoints/openapi/chat/completions") {
				t.Fatalf("wrong route: %s", req.URL)
			}
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["model"] != string(tt.id) {
				t.Fatalf("model ID = %v", payload["model"])
			}
			effort, _ := payload["reasoning_effort"].(string)
			if effort != tt.effort {
				t.Fatalf("reasoning effort = %q, want %q", effort, tt.effort)
			}
			thinking, _ := payload["thinking"].(map[string]any)
			if thinking != nil {
				t.Fatalf("thinking = %#v", thinking)
			}
		})
	}
}
