// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

// Thinking from another model is replayed as plain text; delimiter tags teach
// Gemini to imitate them in visible output.
func TestGoogleReplaysForeignThinkingWithoutTags(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ID: "gemini-test", Provider: sigma.ProviderGoogle, API: sigma.APIGoogleGenerativeAI, SupportsThinking: true}
	payload, err := generativePayload(model, sigma.Request{Messages: []sigma.Message{
		sigma.UserText("q"),
		{
			Role: sigma.RoleAssistant, Provider: sigma.ProviderAnthropic, API: sigma.APIAnthropicMessages, Model: "claude-test",
			Content: []sigma.ContentBlock{sigma.Thinking("plan the answer", "sig"), sigma.Text("answer")},
		},
		sigma.UserText("again"),
	}}, sigma.Options{})
	if err != nil {
		t.Fatal(err)
	}
	encoded := fmt.Sprint(payload["contents"])
	if strings.Contains(encoded, "<thinking>") {
		t.Fatalf("contents = %s, want foreign thinking without tags", encoded)
	}
	if !strings.Contains(encoded, "plan the answer") {
		t.Fatalf("contents = %s, want foreign thinking kept as text", encoded)
	}
}
