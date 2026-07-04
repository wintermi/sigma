// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

func TestTransformRequestForModelPreservesSameModelThinking(t *testing.T) {
	t.Parallel()

	request := sigma.Request{Messages: []sigma.Message{{
		Role:     sigma.RoleAssistant,
		Provider: sigma.ProviderAnthropic,
		API:      sigma.APIAnthropicMessages,
		Model:    "claude-sonnet",
		Content:  []sigma.ContentBlock{sigma.Thinking("compare options", "sig")},
	}}}
	target := sigma.Model{
		ID:               "claude-sonnet",
		Provider:         sigma.ProviderAnthropic,
		API:              sigma.APIAnthropicMessages,
		SupportsThinking: true,
	}

	result, err := sigma.TransformRequestForModel(target, request)
	if err != nil {
		t.Fatalf("TransformRequestForModel returned error: %v", err)
	}

	block := result.Request.Messages[0].Content[0]
	if got, want := block.Type, sigma.ContentBlockThinking; got != want {
		t.Fatalf("thinking block type = %q, want %q", got, want)
	}
	if got := result.Report.ConvertedThinkingBlocks; got != 0 {
		t.Fatalf("converted thinking blocks = %d, want 0", got)
	}
}

func TestTransformRequestForModelConvertsForeignThinkingWithReport(t *testing.T) {
	t.Parallel()

	request := sigma.Request{Messages: []sigma.Message{{
		Role:     sigma.RoleAssistant,
		Provider: sigma.ProviderAnthropic,
		API:      sigma.APIAnthropicMessages,
		Model:    "claude-sonnet",
		Content:  []sigma.ContentBlock{sigma.Thinking("check constraints", "sig")},
	}}}
	target := sigma.Model{
		ID:               "gpt-5.1",
		Provider:         sigma.ProviderOpenAI,
		API:              sigma.APIOpenAIResponses,
		SupportsThinking: true,
	}

	result, err := sigma.TransformRequestForModel(
		target,
		request,
		sigma.WithHandoffThinkingDelimiters("[thinking]", "[/thinking]"),
	)
	if err != nil {
		t.Fatalf("TransformRequestForModel returned error: %v", err)
	}

	block := result.Request.Messages[0].Content[0]
	if got, want := block.Type, sigma.ContentBlockText; got != want {
		t.Fatalf("converted block type = %q, want %q", got, want)
	}
	if got, want := block.Text, "[thinking]\ncheck constraints\n[/thinking]"; got != want {
		t.Fatalf("converted thinking text = %q, want %q", got, want)
	}
	if got := result.Report.ConvertedThinkingBlocks; got != 1 {
		t.Fatalf("converted thinking blocks = %d, want 1", got)
	}
	assertHandoffChange(t, result.Report, sigma.HandoffChangeThinkingConverted, 0, 0)
}

func TestTransformRequestForModelConvertsSameProviderDifferentModelThinking(t *testing.T) {
	t.Parallel()

	request := sigma.Request{Messages: []sigma.Message{{
		Role:     sigma.RoleAssistant,
		Provider: sigma.ProviderAnthropic,
		API:      sigma.APIAnthropicMessages,
		Model:    "claude-sonnet",
		Content:  []sigma.ContentBlock{sigma.Thinking("model-private state", "sig")},
	}}}
	target := sigma.Model{
		ID:               "claude-opus",
		Provider:         sigma.ProviderAnthropic,
		API:              sigma.APIAnthropicMessages,
		SupportsThinking: true,
	}

	result, err := sigma.TransformRequestForModel(target, request)
	if err != nil {
		t.Fatalf("TransformRequestForModel returned error: %v", err)
	}

	block := result.Request.Messages[0].Content[0]
	if got, want := block.Type, sigma.ContentBlockText; got != want {
		t.Fatalf("converted block type = %q, want %q", got, want)
	}
	if got, want := block.Text, "<thinking>\nmodel-private state\n</thinking>"; got != want {
		t.Fatalf("converted thinking text = %q, want %q", got, want)
	}
	if got := result.Report.ConvertedThinkingBlocks; got != 1 {
		t.Fatalf("converted thinking blocks = %d, want 1", got)
	}
}

func TestTransformRequestForModelRejectsOrReplacesUnsupportedImages(t *testing.T) {
	t.Parallel()

	request := sigma.Request{Messages: []sigma.Message{
		sigma.UserContent(
			sigma.Text("Inspect this."),
			sigma.ImageBase64("image/png", "aGVsbG8="),
		),
	}}
	target := sigma.Model{
		ID:               "claude-sonnet",
		Provider:         sigma.ProviderAnthropic,
		API:              sigma.APIAnthropicMessages,
		SupportsThinking: true,
	}

	_, err := sigma.TransformRequestForModel(target, request)
	if err == nil {
		t.Fatal("TransformRequestForModel succeeded for unsupported image")
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorUnsupported {
		t.Fatalf("error = %T %[1]v, want unsupported sigma error", err)
	}

	result, err := sigma.TransformRequestForModel(
		target,
		request,
		sigma.WithHandoffUnsupportedImageReplacement("[image omitted]"),
	)
	if err != nil {
		t.Fatalf("TransformRequestForModel with replacement returned error: %v", err)
	}
	blocks := result.Request.Messages[0].Content
	if got, want := blocks[1].Type, sigma.ContentBlockText; got != want {
		t.Fatalf("replacement block type = %q, want %q", got, want)
	}
	if got, want := blocks[1].Text, "[image omitted]"; got != want {
		t.Fatalf("replacement text = %q, want %q", got, want)
	}
	if got := result.Report.ReplacedUnsupportedImages; got != 1 {
		t.Fatalf("replaced unsupported images = %d, want 1", got)
	}
	assertHandoffChange(t, result.Report, sigma.HandoffChangeUnsupportedImageReplaced, 0, 1)
}

func TestTransformRequestForModelRepairsToolsAndSynthesizesUnansweredCalls(t *testing.T) {
	t.Parallel()

	request := sigma.Request{Messages: []sigma.Message{
		{
			Role:    sigma.RoleAssistant,
			Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_weather", "weather", map[string]any{"city": "Melbourne"})},
		},
		sigma.ToolResult("call_weather", "sunny"),
		{
			Role:    sigma.RoleAssistant,
			Content: []sigma.ContentBlock{sigma.Text("It is sunny.")},
		},
		{
			Role: sigma.RoleAssistant,
			Content: []sigma.ContentBlock{
				sigma.Text("I can check again."),
				sigma.ToolCallBlock("call_unanswered", "weather", map[string]any{"city": "Sydney"}),
			},
		},
		sigma.UserText("Skip that."),
	}}
	target := sigma.Model{
		ID:       "compat-model",
		Provider: sigma.ProviderCustom,
		API:      sigma.APIOpenAICompletions,
		OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
			RequiresToolResultName:           sigma.OpenAICompatSupported,
			RequiresAssistantAfterToolResult: sigma.OpenAICompatSupported,
		},
	}

	result, err := sigma.TransformRequestForModel(target, request)
	if err != nil {
		t.Fatalf("TransformRequestForModel returned error: %v", err)
	}

	if got, want := result.Request.Messages[1].ToolName, "weather"; got != want {
		t.Fatalf("tool result name = %q, want %q", got, want)
	}
	if got, want := result.Request.Messages[2].Role, sigma.RoleAssistant; got != want {
		t.Fatalf("following assistant role = %q, want %q", got, want)
	}
	assistant := result.Request.Messages[3]
	if got, want := len(assistant.Content), 2; got != want {
		t.Fatalf("assistant content count after synthesis = %d, want %d", got, want)
	}
	if got, want := assistant.Content[0].Text, "I can check again."; got != want {
		t.Fatalf("remaining assistant text = %q, want %q", got, want)
	}
	synthetic := result.Request.Messages[4]
	if got, want := synthetic.Role, sigma.RoleTool; got != want {
		t.Fatalf("synthetic role = %q, want %q", got, want)
	}
	if got, want := synthetic.ToolCallID, "call_unanswered"; got != want {
		t.Fatalf("synthetic tool call id = %q, want %q", got, want)
	}
	if got, want := synthetic.ToolName, "weather"; got != want {
		t.Fatalf("synthetic tool name = %q, want %q", got, want)
	}
	if !synthetic.IsError {
		t.Fatal("synthetic tool result IsError = false, want true")
	}
	if got, want := synthetic.Content[0].Text, "No result provided"; got != want {
		t.Fatalf("synthetic text = %q, want %q", got, want)
	}
	if got, want := result.Request.Messages[5].Role, sigma.RoleAssistant; got != want {
		t.Fatalf("inserted repair role = %q, want %q", got, want)
	}
	if got, want := result.Request.Messages[5].Content[0].Text, "I have processed the tool results."; got != want {
		t.Fatalf("inserted repair text = %q, want %q", got, want)
	}
	if got, want := result.Request.Messages[6].Role, sigma.RoleUser; got != want {
		t.Fatalf("final user role = %q, want %q", got, want)
	}
	if got := result.Report.RepairedToolResultNames; got != 1 {
		t.Fatalf("repaired tool result names = %d, want 1", got)
	}
	if got := result.Report.InsertedRepairMessages; got != 1 {
		t.Fatalf("inserted repair messages = %d, want 1", got)
	}
	if got := result.Report.SynthesizedToolResults; got != 1 {
		t.Fatalf("synthesized tool results = %d, want 1", got)
	}
	if got := result.Report.DroppedUnansweredToolCalls; got != 0 {
		t.Fatalf("dropped unanswered tool calls = %d, want 0", got)
	}
	assertHandoffChange(t, result.Report, sigma.HandoffChangeToolResultNameRepaired, 1, -1)
	assertHandoffChange(t, result.Report, sigma.HandoffChangeToolResultSynthesized, 4, -1)
	assertHandoffChange(t, result.Report, sigma.HandoffChangeRepairMessageInserted, 4, -1)
	assertHandoffOutputChange(t, result.Report, sigma.HandoffChangeToolResultSynthesized, 4, 4)
	assertHandoffOutputChange(t, result.Report, sigma.HandoffChangeRepairMessageInserted, 4, 5)
}

func TestTransformRequestForModelConvertsUnsupportedDeveloperRole(t *testing.T) {
	t.Parallel()

	target := sigma.Model{
		ID:       "compat-model",
		Provider: sigma.ProviderCustom,
		API:      sigma.APIOpenAICompletions,
		OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
			SupportsDeveloperRole: sigma.OpenAICompatUnsupported,
		},
	}

	result, err := sigma.TransformRequestForModel(target, sigma.Request{Messages: []sigma.Message{{
		Role:    sigma.RoleDeveloper,
		Content: []sigma.ContentBlock{sigma.Text("Prefer concise answers.")},
	}}})
	if err != nil {
		t.Fatalf("TransformRequestForModel returned error: %v", err)
	}

	if got, want := result.Request.Messages[0].Role, sigma.RoleUser; got != want {
		t.Fatalf("converted role = %q, want %q", got, want)
	}
	if got := result.Report.ConvertedDeveloperMessages; got != 1 {
		t.Fatalf("converted developer messages = %d, want 1", got)
	}
	assertHandoffChange(t, result.Report, sigma.HandoffChangeDeveloperRoleConverted, 0, -1)
}

func TestTransformRequestForModelDoesNotMutateInputs(t *testing.T) {
	t.Parallel()

	request := sigma.Request{
		Messages: []sigma.Message{{
			Role:     sigma.RoleAssistant,
			Provider: sigma.ProviderAnthropic,
			API:      sigma.APIAnthropicMessages,
			Model:    "claude-sonnet",
			Content: []sigma.ContentBlock{{
				Type:         sigma.ContentBlockThinking,
				ThinkingText: "compare options",
				ProviderMetadata: map[string]any{
					"nested": map[string]any{"key": "value"},
				},
			}},
		}},
		Tools: []sigma.Tool{{
			Name:        "lookup",
			InputSchema: sigma.Schema{"type": "object"},
			ProviderDefinedOptions: map[string]any{
				"config": map[string]any{"country": "AU"},
			},
		}},
	}
	target := sigma.Model{
		ID:               "claude-sonnet",
		Provider:         sigma.ProviderAnthropic,
		API:              sigma.APIAnthropicMessages,
		SupportsThinking: true,
	}

	result, err := sigma.TransformRequestForModel(target, request)
	if err != nil {
		t.Fatalf("TransformRequestForModel returned error: %v", err)
	}

	result.Request.Messages[0].Content[0].ProviderMetadata["nested"].(map[string]any)["key"] = "changed"
	result.Request.Tools[0].InputSchema.(sigma.Schema)["type"] = "array"
	result.Request.Tools[0].ProviderDefinedOptions["config"].(map[string]any)["country"] = "US"

	metadata := request.Messages[0].Content[0].ProviderMetadata["nested"].(map[string]any)
	if got, want := metadata["key"], "value"; got != want {
		t.Fatalf("original provider metadata = %q, want %q", got, want)
	}
	if got, want := request.Tools[0].InputSchema.(sigma.Schema)["type"], "object"; got != want {
		t.Fatalf("original tool schema = %q, want %q", got, want)
	}
	options := request.Tools[0].ProviderDefinedOptions["config"].(map[string]any)
	if got, want := options["country"], "AU"; got != want {
		t.Fatalf("original provider-defined option = %q, want %q", got, want)
	}
}

func TestTransformMessagesForModelMatchesRequestHelper(t *testing.T) {
	t.Parallel()

	messages := []sigma.Message{{
		Role:     sigma.RoleAssistant,
		Provider: sigma.ProviderAnthropic,
		API:      sigma.APIAnthropicMessages,
		Model:    "claude-sonnet",
		Content:  []sigma.ContentBlock{sigma.Thinking("check constraints", "sig")},
	}}
	target := sigma.Model{
		ID:       "gpt-text",
		Provider: sigma.ProviderOpenAI,
		API:      sigma.APIOpenAIResponses,
	}

	requestResult, err := sigma.TransformRequestForModel(target, sigma.Request{Messages: messages})
	if err != nil {
		t.Fatalf("TransformRequestForModel returned error: %v", err)
	}
	messageResult, err := sigma.TransformMessagesForModel(target, messages)
	if err != nil {
		t.Fatalf("TransformMessagesForModel returned error: %v", err)
	}

	if !reflect.DeepEqual(messageResult.Messages, requestResult.Request.Messages) {
		t.Fatalf("message helper messages = %#v, want %#v", messageResult.Messages, requestResult.Request.Messages)
	}
	if !reflect.DeepEqual(messageResult.Report, requestResult.Report) {
		t.Fatalf("message helper report = %#v, want %#v", messageResult.Report, requestResult.Report)
	}
}

func assertHandoffChange(t *testing.T, report sigma.HandoffReport, kind sigma.HandoffChangeKind, messageIndex int, contentIndex int) {
	t.Helper()

	for _, change := range report.Changes {
		if change.Kind != kind || change.MessageIndex != messageIndex {
			continue
		}
		if contentIndex < 0 {
			if change.ContentIndex != nil {
				t.Fatalf("change %q content index = %d, want nil", kind, *change.ContentIndex)
			}
			return
		}
		if change.ContentIndex == nil {
			t.Fatalf("change %q content index = nil, want %d", kind, contentIndex)
		}
		if *change.ContentIndex != contentIndex {
			t.Fatalf("change %q content index = %d, want %d", kind, *change.ContentIndex, contentIndex)
		}
		return
	}
	t.Fatalf("missing handoff change kind=%q messageIndex=%d contentIndex=%d in %#v", kind, messageIndex, contentIndex, report.Changes)
}

func assertHandoffOutputChange(t *testing.T, report sigma.HandoffReport, kind sigma.HandoffChangeKind, messageIndex int, outputIndex int) {
	t.Helper()

	for _, change := range report.Changes {
		if change.Kind != kind || change.MessageIndex != messageIndex {
			continue
		}
		if change.OutputMessageIndex == nil {
			t.Fatalf("change %q output index = nil, want %d", kind, outputIndex)
		}
		if *change.OutputMessageIndex != outputIndex {
			t.Fatalf("change %q output index = %d, want %d", kind, *change.OutputMessageIndex, outputIndex)
		}
		return
	}
	t.Fatalf("missing handoff output change kind=%q messageIndex=%d outputIndex=%d in %#v", kind, messageIndex, outputIndex, report.Changes)
}
