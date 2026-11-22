// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
)

// Some providers reject whitespace-only tool content, so it is sent as the
// empty-output placeholder on both Chat Completions and Responses.
func TestWhitespaceToolOutputUsesPlaceholder(t *testing.T) {
	t.Parallel()
	model := sigma.Model{ID: "gpt-test", Provider: sigma.ProviderOpenAI}
	for _, text := range []string{"\r\n", "  \t ", ""} {
		message := sigma.Message{Role: sigma.RoleTool, ToolCallID: "call_1", ToolName: "bash", Content: []sigma.ContentBlock{sigma.Text(text)}}
		chat, err := chatMessage(model, message, "", completionsCompat{}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := chat["content"]; got != "(no tool output)" {
			t.Fatalf("chat content for %q = %#v, want placeholder", text, got)
		}
		output, err := responsesToolOutput(model, message)
		if err != nil {
			t.Fatal(err)
		}
		if output != "(no tool output)" {
			t.Fatalf("responses output for %q = %#v, want placeholder", text, output)
		}
	}
	message := sigma.Message{Role: sigma.RoleTool, ToolCallID: "call_1", ToolName: "bash", Content: []sigma.ContentBlock{sigma.Text(" ok \n")}}
	if output, err := responsesToolOutput(model, message); err != nil || output != " ok \n" {
		t.Fatalf("responses output = %#v, %v; want text unchanged", output, err)
	}
}
