// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestVertexCatalogClaudeRequests(t *testing.T) {
	t.Parallel()
	for _, id := range []sigma.ModelID{"claude-haiku-4-5", "claude-fable-5", "claude-fable-5-1", "claude-opus-5", "claude-opus-5-5", "claude-sonnet-5", "claude-sonnet-5-5"} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()
			model, ok := sigma.GetModel(sigma.ProviderGoogleVertexAnthropic, id)
			if !ok {
				t.Fatal("missing Claude model")
			}
			opts := sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh, AuthResolver: sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
				return sigma.Credential{Type: sigma.CredentialTypeOAuthToken, Value: "fixture"}, nil
			})}
			provider := NewVertexProvider(WithVertexConfig(VertexConfig{ProjectID: "fixture-project", Location: "global"}))
			req, err := provider.newRequest(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer req.Body.Close()
			if !strings.HasSuffix(req.URL.Path, "/publishers/anthropic/models/"+string(id)+":streamRawPredict") {
				t.Fatalf("wrong route: %s", req.URL)
			}
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if _, ok := payload["model"]; ok {
				t.Fatal("Vertex model leaked into Messages body")
			}
			thinking, _ := payload["thinking"].(map[string]any)
			if id == "claude-haiku-4-5" {
				if thinking["type"] != "enabled" || thinking["budget_tokens"] == nil {
					t.Fatalf("budget thinking = %#v", thinking)
				}
			} else {
				config, _ := payload["output_config"].(map[string]any)
				if thinking["type"] != "adaptive" || config["effort"] != "high" {
					t.Fatalf("adaptive thinking = %#v/%#v", thinking, config)
				}
			}
		})
	}
}
