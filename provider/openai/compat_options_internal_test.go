// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
)

// Some OpenAI-compatible servers reject an assistant turn whose content is
// missing; RequiresAssistantContent sends an empty string instead.
func TestRequiresAssistantContentSendsEmptyString(t *testing.T) {
	t.Parallel()
	message := sigma.Message{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_1", "lookup", map[string]any{})}}
	for _, tt := range []struct {
		support sigma.OpenAICompatSupport
		want    bool
	}{
		{support: sigma.OpenAICompatSupported, want: true},
		{support: "", want: false},
	} {
		model := sigma.Model{ID: "m", Provider: sigma.ProviderCustom, OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{RequiresAssistantContent: tt.support}}
		converted, err := chatMessage(model, message, "", openAICompletionsCompat(model, "https://llm.example.test/v1"), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		content, present := converted["content"]
		if present != tt.want || (present && content != "") {
			t.Errorf("support %q: content = %#v (present %v), want present %v", tt.support, content, present, tt.want)
		}
	}
}

// Proxies that reject prompt_cache_key can turn it off on both APIs.
func TestPromptCacheKeyCanBeDisabled(t *testing.T) {
	t.Parallel()
	opts := sigma.Options{SessionID: "session-1", CacheRetention: sigma.CacheRetentionShort}
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}

	chatModel := sigma.Model{ID: "m", Provider: sigma.ProviderCustom, OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{SupportsPromptCacheKey: sigma.OpenAICompatUnsupported}}
	chat, err := chatCompletionsPayload(chatModel, req, opts, openAICompletionsCompat(chatModel, "https://llm.example.test/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if key, ok := chat["prompt_cache_key"]; ok {
		t.Fatalf("chat prompt_cache_key = %#v, want absent", key)
	}

	responsesModel := sigma.Model{ID: "m", Provider: sigma.ProviderCustom, API: sigma.APIOpenAIResponses, OpenAIResponsesCompat: &sigma.OpenAIResponsesCompat{SupportsPromptCacheKey: sigma.OpenAICompatUnsupported}}
	responses, err := responsesPayload(responsesModel, req, opts)
	if err != nil {
		t.Fatal(err)
	}
	if key, ok := responses["prompt_cache_key"]; ok {
		t.Fatalf("responses prompt_cache_key = %#v, want absent", key)
	}
	responsesModel.OpenAIResponsesCompat = nil
	if responses, _ = responsesPayload(responsesModel, req, opts); responses["prompt_cache_key"] != "session-1" {
		t.Fatalf("default prompt_cache_key = %#v, want session-1", responses["prompt_cache_key"])
	}
}
