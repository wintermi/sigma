// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestWhitespaceToolOutputIsSentEmpty(t *testing.T) {
	t.Parallel()
	model := sigma.Model{ID: "gemini-2.5-flash", Provider: sigma.ProviderGoogle, API: sigma.APIGoogleGenerativeAI}
	message := sigma.Message{Role: sigma.RoleTool, ToolCallID: "call_1", ToolName: "bash", Content: []sigma.ContentBlock{sigma.Text("\r\n")}}
	contents, err := googleToolResultContents(model, message, newGoogleToolCallIDNormalizer(model))
	if err != nil {
		t.Fatal(err)
	}
	parts := contents[0]["parts"].([]map[string]any)
	response := parts[0]["functionResponse"].(map[string]any)["response"].(map[string]any)
	if got := response["output"]; got != "" {
		t.Fatalf("output = %#v, want empty", got)
	}
}
