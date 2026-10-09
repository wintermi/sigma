// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestGenerativePayloadKeepsParallelFunctionResponsesTogether(t *testing.T) {
	t.Parallel()

	model, ok := sigma.GetModel(sigma.ProviderGoogle, "gemini-2.5-flash")
	if !ok {
		t.Fatal("gemini-2.5-flash not in catalog")
	}
	req := sigma.Request{Messages: []sigma.Message{
		sigma.UserText("look at both"),
		{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, Content: []sigma.ContentBlock{
			sigma.ToolCallBlock("call_1", "screenshot", map[string]any{}),
			sigma.ToolCallBlock("call_2", "read", map[string]any{}),
		}},
		{Role: sigma.RoleTool, ToolCallID: "call_1", ToolName: "screenshot", Content: []sigma.ContentBlock{sigma.ImageBase64("image/png", "aGk=")}},
		{Role: sigma.RoleTool, ToolCallID: "call_2", ToolName: "read", Content: []sigma.ContentBlock{sigma.Text("file text")}},
		sigma.UserText("next"),
	}}

	payload, err := generativePayload(model, req, sigma.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var turns []string
	for _, content := range payload["contents"].([]map[string]any) {
		var keys []string
		for _, part := range content["parts"].([]map[string]any) {
			for key := range part {
				keys = append(keys, key)
			}
		}
		turns = append(turns, content["role"].(string)+":"+strings.Join(keys, ","))
	}
	want := []string{
		"user:text",
		"model:functionCall,functionCall",
		"user:functionResponse,functionResponse",
		"user:text,inlineData",
		"user:text",
	}
	if strings.Join(turns, " | ") != strings.Join(want, " | ") {
		t.Fatalf("turns = %q, want %q", turns, want)
	}
}
