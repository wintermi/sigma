// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

func TestReasoningReplayUsesTargetModel(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, modelID, modelName, signature string
		wantSignature                       string
		wantText, incompatible              bool
	}{
		{name: "non-Claude unsigned", modelID: "qwen.qwen3-32b-v1:0"},
		{name: "non-Claude signed", modelID: "qwen.qwen3-32b-v1:0", signature: "opaque"},
		{name: "Claude signed", modelID: "anthropic.claude-sonnet-4-5", signature: "opaque", wantSignature: "opaque"},
		{name: "Claude unsigned", modelID: "anthropic.claude-sonnet-4-5", wantText: true},
		{name: "Claude whitespace signature", modelID: "anthropic.claude-sonnet-4-5", signature: " \t", wantText: true},
		{name: "Claude profile", modelID: "application-profile", modelName: "Claude Sonnet 4.5", wantText: true},
		{name: "incompatible provenance", modelID: "anthropic.claude-sonnet-4-5", signature: "opaque", incompatible: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := bedrockTestModel(sigma.ProviderAmazonBedrock)
			model.ID, model.Name = sigma.ModelID(tt.modelID), tt.modelName
			message := sigma.Message{
				Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID,
				Content: []sigma.ContentBlock{sigma.Text("before"), sigma.Thinking("reasoning", tt.signature), sigma.Text("after")},
			}
			if tt.incompatible {
				message.Model = "another-model"
			}
			req := sigma.Request{Messages: []sigma.Message{message, sigma.UserText("continue")}}
			before, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := conversePayload(model, req, sigma.Options{}, Config{})
			if err != nil {
				t.Fatal(err)
			}
			wire, err := awsContentBlocks(payload.Messages[0].Content)
			if err != nil {
				t.Fatal(err)
			}
			want := []map[string]any{{"text": "before"}}
			if tt.incompatible {
				want = append(want, map[string]any{"text": "<thinking>\nreasoning\n</thinking>"})
			} else {
				middle := map[string]any{"text": "reasoning"}
				if !tt.wantText {
					reasoning := map[string]any{"text": "reasoning"}
					if tt.wantSignature != "" {
						reasoning["signature"] = tt.wantSignature
					}
					middle = map[string]any{"reasoningContent": map[string]any{"reasoningText": reasoning}}
				}
				want = append(want, middle)
			}
			want = append(want, map[string]any{"text": "after"})
			if !reflect.DeepEqual(wire, want) {
				t.Fatalf("wire = %#v, want %#v", wire, want)
			}
			after, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("replay changed caller history")
			}
		})
	}
}

func TestManualThinkingRespectsOutputCap(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                string
		cap, budget         *int
		modelLimit          int
		interleaved, tools  bool
		wantBudget, wantCap int
	}{
		{name: "high reasoning clamps", cap: intPtr(2048), wantBudget: 1024, wantCap: 2048},
		{name: "too little output", cap: intPtr(2047), wantCap: 2047},
		{name: "valid budget", cap: intPtr(4096), budget: intPtr(2048), wantBudget: 2048, wantCap: 4096},
		{name: "zero budget", cap: intPtr(4096), budget: intPtr(0), wantCap: 4096},
		{name: "subminimum budget", cap: intPtr(4096), budget: intPtr(1023), wantCap: 4096},
		{name: "model default cap", modelLimit: 4096, wantBudget: 3072, wantCap: 4096},
		{name: "unknown model cap disables"},
		{name: "interleaved without tools clamps", cap: intPtr(2048), interleaved: true, wantBudget: 1024, wantCap: 2048},
		{name: "interleaved tools exception", cap: intPtr(2048), interleaved: true, tools: true, wantBudget: 16384, wantCap: 2048},
		{name: "interleaved tools default cap", modelLimit: 4096, interleaved: true, tools: true, wantBudget: 16384, wantCap: 4096},
		{name: "interleaved tools fallback cap", interleaved: true, tools: true, wantBudget: 16384, wantCap: 1024},
		{name: "interleaved tools minimum", cap: intPtr(2048), budget: intPtr(1023), interleaved: true, tools: true, wantCap: 2048},
		{name: "tools without interleaving clamp", cap: intPtr(2048), tools: true, wantBudget: 1024, wantCap: 2048},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := bedrockTestModel(sigma.ProviderAmazonBedrock)
			model.ID = "anthropic.claude-sonnet-4-5"
			model.MaxOutputTokens = tt.modelLimit
			req := sigma.Request{Messages: []sigma.Message{sigma.UserText("solve this")}}
			if tt.tools {
				req.Tools = []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}}
			}
			opts := sigma.Options{
				MaxTokens: tt.cap, ReasoningLevel: sigma.ThinkingLevelHigh, ThinkingBudgetTokens: tt.budget,
				BedrockOptions: &sigma.BedrockOptions{InterleavedThinking: &tt.interleaved},
			}
			payload, err := conversePayload(model, req, opts, Config{})
			if err != nil {
				t.Fatal(err)
			}
			thinking := payload.AdditionalModelRequestFields["thinking"].(map[string]any)
			if tt.wantBudget == 0 {
				if !reflect.DeepEqual(thinking, map[string]any{"type": "disabled"}) {
					t.Fatalf("thinking = %#v, want disabled", thinking)
				}
				if _, ok := payload.AdditionalModelRequestFields["anthropic_beta"]; ok {
					t.Fatal("disabled thinking added interleaved beta")
				}
			} else if thinking["type"] != "enabled" || thinking["budget_tokens"] != tt.wantBudget {
				t.Fatalf("thinking = %#v, want budget %d", thinking, tt.wantBudget)
			}
			gotCap := 0
			if payload.InferenceConfig != nil && payload.InferenceConfig.MaxTokens != nil {
				gotCap = *payload.InferenceConfig.MaxTokens
			}
			if gotCap != tt.wantCap {
				t.Fatalf("max tokens = %d, want %d", gotCap, tt.wantCap)
			}
			if tt.cap != nil && *tt.cap != tt.wantCap {
				t.Fatal("caller output cap changed")
			}
		})
	}
}

func TestManualThinkingUsesFinalTools(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"response format", "replay", "suppressed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			model := bedrockTestModel(sigma.ProviderAmazonBedrock)
			model.ID = "anthropic.claude-sonnet-4-5"
			req := sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}
			opts := sigma.Options{ReasoningLevel: sigma.ThinkingLevelHigh, MaxTokens: intPtr(2048)}
			switch mode {
			case "response format":
				opts.BedrockOptions = &sigma.BedrockOptions{ResponseFormat: sigma.Schema{"type": "object"}}
			case "replay":
				req.Messages = append(req.Messages, sigma.Message{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock("call", "lookup", map[string]any{})}},
					sigma.Message{Role: sigma.RoleTool, ToolCallID: "call", ToolName: "lookup", Content: []sigma.ContentBlock{sigma.Text("done")}})
			case "suppressed":
				req.Tools = []sigma.Tool{{Name: "lookup", InputSchema: sigma.Schema{"type": "object"}}}
				opts.ToolChoice = sigma.ToolChoiceNone
			}
			payload, err := conversePayload(model, req, opts, Config{})
			if err != nil {
				t.Fatal(err)
			}
			want := 16384
			if mode == "suppressed" {
				want = 1024
			}
			if got := payload.AdditionalModelRequestFields["thinking"].(map[string]any)["budget_tokens"]; got != want {
				t.Fatalf("budget = %v, want %d", got, want)
			}
		})
	}
}

func TestManualThinkingUsesContextAdjustedCap(t *testing.T) {
	t.Parallel()
	model := bedrockTestModel(sigma.ProviderAmazonBedrock)
	model.ID = "anthropic.claude-sonnet-4-5"
	model.ContextWindow, model.MaxOutputTokens = 8192, 4096
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}
	fake := &fakeConverseClient{}
	client := bedrockTestClient(t, model.Provider, model, fake, fakeCredentialDetector{})
	stream := client.Stream(context.Background(), model, req, sigma.WithReasoningLevel(sigma.ThinkingLevelHigh), sigma.WithAutomaticMaxTokensForContext(true))
	collectEvents(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	wantCap := sigma.MaxTokensForContext(model, req, 0)
	if fake.request.InferenceConfig == nil || fake.request.InferenceConfig.MaxTokens == nil || *fake.request.InferenceConfig.MaxTokens != wantCap {
		t.Fatalf("inference config = %#v, want cap %d", fake.request.InferenceConfig, wantCap)
	}
	if got := fake.request.AdditionalModelRequestFields["thinking"].(map[string]any)["budget_tokens"]; got != wantCap-1024 {
		t.Fatalf("budget = %v, want %d", got, wantCap-1024)
	}
}
