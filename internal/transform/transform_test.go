// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package transform

import (
	"errors"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

func TestTransformPreservesSameProviderAssistantBlocks(t *testing.T) {
	t.Parallel()

	request := sigma.Request{
		Messages: []sigma.Message{
			{
				Role:     sigma.RoleAssistant,
				Provider: sigma.ProviderAnthropic,
				API:      sigma.APIAnthropicMessages,
				Model:    "claude-sonnet",
				Content: []sigma.ContentBlock{
					{
						Type:         sigma.ContentBlockThinking,
						ThinkingText: "compare the options",
						Signature:    "sig",
						ProviderMetadata: map[string]any{
							"nested": map[string]any{"key": "value"},
						},
					},
					sigma.Text("Use the smaller option."),
					sigma.ToolCallBlock("call_1", "lookup", map[string]any{"query": "weather"}),
				},
			},
		},
		Tools: []sigma.Tool{
			{
				Name:        "lookup",
				InputSchema: sigma.Schema{"type": "object"},
			},
		},
	}

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:               "claude-sonnet",
			Provider:         sigma.ProviderAnthropic,
			API:              sigma.APIAnthropicMessages,
			SupportsThinking: true,
		},
		Request: request,
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	thinking := transformed.Messages[0].Content[0]
	if got, want := thinking.Type, sigma.ContentBlockThinking; got != want {
		t.Fatalf("thinking block type = %q, want %q", got, want)
	}
	if got, want := thinking.ThinkingText, "compare the options"; got != want {
		t.Fatalf("thinking text = %q, want %q", got, want)
	}
	if got, want := thinking.Signature, "sig"; got != want {
		t.Fatalf("thinking signature = %q, want %q", got, want)
	}

	nested := transformed.Messages[0].Content[0].ProviderMetadata["nested"].(map[string]any)
	nested["key"] = "changed"
	originalNested := request.Messages[0].Content[0].ProviderMetadata["nested"].(map[string]any)
	if got, want := originalNested["key"], "value"; got != want {
		t.Fatalf("original provider metadata was mutated: got %q want %q", got, want)
	}

	transformed.Tools[0].InputSchema.(sigma.Schema)["type"] = "array"
	if got, want := request.Tools[0].InputSchema.(sigma.Schema)["type"], "object"; got != want {
		t.Fatalf("original tool schema was mutated: got %q want %q", got, want)
	}
}

func TestTransformPreservesToolResultUsage(t *testing.T) {
	t.Parallel()

	usage := &sigma.Usage{
		OutputTokens: 3,
		Provider:     sigma.ProviderOpenAI,
		Model:        "tool-model",
		Raw:          map[string]any{"source": "tool"},
	}
	toolResult := sigma.ToolResult("call_1", "result")
	toolResult.Usage = usage
	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:            "gpt-test",
			Provider:      sigma.ProviderOpenAI,
			API:           sigma.APIOpenAICompletions,
			SupportsTools: true,
		},
		Request: sigma.Request{Messages: []sigma.Message{
			{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_1", "tool", map[string]any{})},
			},
			toolResult,
		}},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	if got := transformed.Messages[1].Usage; !reflect.DeepEqual(got, usage) {
		t.Fatalf("tool usage = %#v, want %#v", got, usage)
	}
}

func TestTransformClonesProviderDefinedToolOptions(t *testing.T) {
	t.Parallel()

	request := sigma.Request{
		Messages: []sigma.Message{sigma.UserText("Search for current docs.")},
		Tools: []sigma.Tool{{
			Name:                "web_search",
			ProviderDefinedType: "web_search",
			ProviderDefinedOptions: map[string]any{
				"user_location": map[string]any{"country": "AU"},
				"searchTypes":   map[string]any{"webSearch": map[string]any{}},
			},
		}},
	}

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "gpt-4.1",
			Provider: sigma.ProviderOpenAI,
			API:      sigma.APIOpenAIResponses,
		},
		Request: request,
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	location := transformed.Tools[0].ProviderDefinedOptions["user_location"].(map[string]any)
	location["country"] = "US"
	originalLocation := request.Tools[0].ProviderDefinedOptions["user_location"].(map[string]any)
	if got, want := originalLocation["country"], "AU"; got != want {
		t.Fatalf("original provider-defined options were mutated: got %q want %q", got, want)
	}
	searchTypes := transformed.Tools[0].ProviderDefinedOptions["searchTypes"].(map[string]any)
	webSearch := searchTypes["webSearch"].(map[string]any)
	if len(webSearch) != 0 {
		t.Fatalf("empty provider-defined option map = %#v, want empty map", webSearch)
	}
}

func TestTransformConvertsAnthropicThinkingForOpenAIHandoff(t *testing.T) {
	t.Parallel()

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "gpt-4.1",
			Provider: sigma.ProviderOpenAI,
			API:      sigma.APIOpenAIResponses,
		},
		Request: sigma.Request{
			Messages: []sigma.Message{
				{
					Role:     sigma.RoleAssistant,
					Provider: sigma.ProviderAnthropic,
					API:      sigma.APIAnthropicMessages,
					Content: []sigma.ContentBlock{
						sigma.Thinking("checking constraints", "sig"),
						sigma.Text("I will call the tool."),
						sigma.ToolCallBlock("call_weather", "weather", map[string]any{"city": "Melbourne"}),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	blocks := transformed.Messages[0].Content
	if got, want := blocks[0].Type, sigma.ContentBlockText; got != want {
		t.Fatalf("converted thinking type = %q, want %q", got, want)
	}
	if got, want := blocks[0].Text, "<thinking>\nchecking constraints\n</thinking>"; got != want {
		t.Fatalf("converted thinking text = %q, want %q", got, want)
	}
	if got, want := blocks[1].Text, "I will call the tool."; got != want {
		t.Fatalf("assistant text = %q, want %q", got, want)
	}
	toolCall := blocks[2]
	if got, want := toolCall.ToolCallID, "call_weather"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	if got, want := toolCall.ToolName, "weather"; got != want {
		t.Fatalf("tool call name = %q, want %q", got, want)
	}
	if got, want := toolCall.ToolArguments.(map[string]any)["city"], "Melbourne"; got != want {
		t.Fatalf("tool call city argument = %q, want %q", got, want)
	}
}

func TestTransformConvertsOpenAIThinkingForGoogleHandoff(t *testing.T) {
	t.Parallel()

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:               "gemini-2.5-pro",
			Provider:         sigma.ProviderGoogle,
			API:              sigma.APIGoogleGenerativeAI,
			SupportsThinking: true,
		},
		Request: sigma.Request{
			Messages: []sigma.Message{
				{
					Role:     sigma.RoleAssistant,
					Provider: sigma.ProviderOpenAI,
					API:      sigma.APIOpenAIResponses,
					Content: []sigma.ContentBlock{
						sigma.Text("Known facts."),
						sigma.Thinking("private reasoning", ""),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	blocks := transformed.Messages[0].Content
	if got, want := blocks[0].Text, "Known facts."; got != want {
		t.Fatalf("assistant text = %q, want %q", got, want)
	}
	if got, want := blocks[1].Type, sigma.ContentBlockText; got != want {
		t.Fatalf("foreign thinking type = %q, want %q", got, want)
	}
	if got, want := blocks[1].Text, "<thinking>\nprivate reasoning\n</thinking>"; got != want {
		t.Fatalf("foreign thinking text = %q, want %q", got, want)
	}
}

func TestTransformRepairsToolCallToolResultSequences(t *testing.T) {
	t.Parallel()

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "gpt-4.1",
			Provider: sigma.ProviderOpenAI,
			API:      sigma.APIOpenAIResponses,
		},
		Compatibility: Compatibility{
			RequireToolResultName:          true,
			AssistantAfterToolResultRepair: true,
		},
		Request: sigma.Request{
			Messages: []sigma.Message{
				{
					Role:    sigma.RoleAssistant,
					Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_1", "weather", map[string]any{"city": "Melbourne"})},
				},
				{
					Role:       sigma.RoleTool,
					ToolCallID: "call_1",
					Content:    []sigma.ContentBlock{sigma.Text("18 C")},
				},
				{
					Role:    sigma.RoleAssistant,
					Content: []sigma.ContentBlock{sigma.Text("It is 18 C.")},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	if got, want := transformed.Messages[1].ToolName, "weather"; got != want {
		t.Fatalf("tool result name = %q, want %q", got, want)
	}
	if got, want := transformed.Messages[1].ToolCallID, "call_1"; got != want {
		t.Fatalf("tool result id = %q, want %q", got, want)
	}
	if got, want := len(transformed.Messages), 3; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	if got, want := transformed.Messages[2].Role, sigma.RoleAssistant; got != want {
		t.Fatalf("assistant role = %q, want %q", got, want)
	}
	if got, want := transformed.Messages[2].Content[0].Text, "It is 18 C."; got != want {
		t.Fatalf("assistant text = %q, want %q", got, want)
	}
}

func TestTransformBridgesToolResultBeforeUserTurn(t *testing.T) {
	t.Parallel()

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "gpt-4.1",
			Provider: sigma.ProviderOpenAI,
			API:      sigma.APIOpenAIResponses,
		},
		Compatibility: Compatibility{
			RequireToolResultName:          true,
			AssistantAfterToolResultRepair: true,
		},
		Request: sigma.Request{
			Messages: []sigma.Message{
				{
					Role:    sigma.RoleAssistant,
					Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_1", "weather", map[string]any{"city": "Melbourne"})},
				},
				{
					Role:       sigma.RoleTool,
					ToolCallID: "call_1",
					Content:    []sigma.ContentBlock{sigma.Text("18 C")},
				},
				sigma.UserText("Thanks."),
			},
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	if got, want := len(transformed.Messages), 4; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	if got, want := transformed.Messages[2].Role, sigma.RoleAssistant; got != want {
		t.Fatalf("repair role = %q, want %q", got, want)
	}
	if got, want := transformed.Messages[2].Content[0].Text, "I have processed the tool results."; got != want {
		t.Fatalf("repair text = %q, want %q", got, want)
	}
	if got, want := transformed.Messages[3].Role, sigma.RoleUser; got != want {
		t.Fatalf("user role = %q, want %q", got, want)
	}
}

func TestTransformSynthesizesUnansweredToolCallsBeforeUserTurn(t *testing.T) {
	t.Parallel()

	request := sigma.Request{
		Messages: []sigma.Message{
			{
				Role: sigma.RoleAssistant,
				Content: []sigma.ContentBlock{
					sigma.ToolCallBlock("call_unanswered", "lookup", map[string]any{"query": "weather"}),
				},
			},
			sigma.UserText("Never mind, answer directly."),
		},
	}

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "gpt-4.1",
			Provider: sigma.ProviderOpenAI,
			API:      sigma.APIOpenAIResponses,
		},
		Compatibility: Compatibility{
			DropUnansweredToolCalls: true,
		},
		Request: request,
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	if got, want := len(transformed.Messages), 3; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	synthetic := transformed.Messages[1]
	if got, want := synthetic.Role, sigma.RoleTool; got != want {
		t.Fatalf("synthetic role = %q, want %q", got, want)
	}
	if got, want := synthetic.ToolCallID, "call_unanswered"; got != want {
		t.Fatalf("synthetic tool call id = %q, want %q", got, want)
	}
	if got, want := synthetic.ToolName, "lookup"; got != want {
		t.Fatalf("synthetic tool name = %q, want %q", got, want)
	}
	if !synthetic.IsError {
		t.Fatal("synthetic tool result IsError = false, want true")
	}
	if got, want := synthetic.Content[0].Text, "No result provided"; got != want {
		t.Fatalf("synthetic text = %q, want %q", got, want)
	}
	if got, want := transformed.Messages[2].Role, sigma.RoleUser; got != want {
		t.Fatalf("remaining message role = %q, want %q", got, want)
	}
	if got, want := len(request.Messages[0].Content), 1; got != want {
		t.Fatalf("original assistant content count = %d, want %d", got, want)
	}
}

func TestTransformPreservesAnsweredToolCallsBeforeUserTurn(t *testing.T) {
	t.Parallel()

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "gpt-4.1",
			Provider: sigma.ProviderOpenAI,
			API:      sigma.APIOpenAIResponses,
		},
		Compatibility: Compatibility{
			DropUnansweredToolCalls: true,
		},
		Request: sigma.Request{
			Messages: []sigma.Message{
				{
					Role: sigma.RoleAssistant,
					Content: []sigma.ContentBlock{
						sigma.ToolCallBlock("call_answered", "lookup", map[string]any{"query": "weather"}),
					},
				},
				sigma.ToolResult("call_answered", "18 C"),
				sigma.UserText("Thanks."),
			},
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	if got, want := len(transformed.Messages), 3; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	toolCall := transformed.Messages[0].Content[0]
	if got, want := toolCall.ToolCallID, "call_answered"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
}

func TestTransformSynthesizesTrailingUnansweredToolCalls(t *testing.T) {
	t.Parallel()

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "gpt-4.1",
			Provider: sigma.ProviderOpenAI,
			API:      sigma.APIOpenAIResponses,
		},
		Compatibility: Compatibility{
			DropUnansweredToolCalls: true,
		},
		Request: sigma.Request{
			Messages: []sigma.Message{
				{
					Role: sigma.RoleAssistant,
					Content: []sigma.ContentBlock{
						sigma.Text("I can look that up."),
						sigma.ToolCallBlock("call_unanswered", "lookup", map[string]any{"query": "weather"}),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	if got, want := len(transformed.Messages), 2; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	blocks := transformed.Messages[0].Content
	if got, want := len(blocks), 2; got != want {
		t.Fatalf("assistant content count = %d, want %d", got, want)
	}
	if got, want := blocks[0].Text, "I can look that up."; got != want {
		t.Fatalf("assistant text = %q, want %q", got, want)
	}
	synthetic := transformed.Messages[1]
	if got, want := synthetic.ToolCallID, "call_unanswered"; got != want {
		t.Fatalf("synthetic tool call id = %q, want %q", got, want)
	}
	if !synthetic.IsError {
		t.Fatal("synthetic tool result IsError = false, want true")
	}
}

func TestTransformConvertsSameProviderDifferentModelThinking(t *testing.T) {
	t.Parallel()

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:               "claude-opus",
			Provider:         sigma.ProviderAnthropic,
			API:              sigma.APIAnthropicMessages,
			SupportsThinking: true,
		},
		Request: sigma.Request{
			Messages: []sigma.Message{{
				Role:     sigma.RoleAssistant,
				Provider: sigma.ProviderAnthropic,
				API:      sigma.APIAnthropicMessages,
				Model:    "claude-sonnet",
				Content:  []sigma.ContentBlock{sigma.Thinking("private model state", "sig")},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	block := transformed.Messages[0].Content[0]
	if got, want := block.Type, sigma.ContentBlockText; got != want {
		t.Fatalf("converted thinking type = %q, want %q", got, want)
	}
	if got, want := block.Text, "<thinking>\nprivate model state\n</thinking>"; got != want {
		t.Fatalf("converted thinking text = %q, want %q", got, want)
	}
}

func TestTransformNormalizesToolCallIDsWithResults(t *testing.T) {
	t.Parallel()

	normalize := SafeToolCallIDNormalizer(64)
	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "claude-sonnet",
			Provider: sigma.ProviderAnthropic,
			API:      sigma.APIAnthropicMessages,
		},
		Compatibility: Compatibility{
			NormalizeToolCallID: normalize,
		},
		Request: sigma.Request{
			Messages: []sigma.Message{
				{
					Role: sigma.RoleAssistant,
					Content: []sigma.ContentBlock{
						sigma.ToolCallBlock("call:prev/with spaces|foreign+item", "lookup", map[string]any{"query": "weather"}),
						sigma.ToolCallBlock("call:prev/with spaces|foreign-item", "lookup", map[string]any{"query": "time"}),
					},
				},
				{
					Role:       sigma.RoleTool,
					ToolCallID: "call:prev/with spaces|foreign+item",
					Content:    []sigma.ContentBlock{sigma.Text("18 C")},
				},
				{
					Role:       sigma.RoleTool,
					ToolCallID: "call:prev/with spaces|foreign-item",
					Content:    []sigma.ContentBlock{sigma.Text("10 AM")},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	first := transformed.Messages[0].Content[0].ToolCallID
	second := transformed.Messages[0].Content[1].ToolCallID
	if first == second {
		t.Fatalf("normalized ids collided: %q", first)
	}
	if got, want := transformed.Messages[1].ToolCallID, first; got != want {
		t.Fatalf("first tool result id = %q, want %q", got, want)
	}
	if got, want := transformed.Messages[2].ToolCallID, second; got != want {
		t.Fatalf("second tool result id = %q, want %q", got, want)
	}
}

func TestTransformConvertsDeveloperRoleWhenRequired(t *testing.T) {
	t.Parallel()

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "claude-sonnet",
			Provider: sigma.ProviderAnthropic,
			API:      sigma.APIAnthropicMessages,
		},
		Compatibility: Compatibility{
			ConvertDeveloperRole: true,
		},
		Request: sigma.Request{
			Messages: []sigma.Message{
				{
					Role:    sigma.RoleDeveloper,
					Content: []sigma.ContentBlock{sigma.Text("Prefer concise answers.")},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	if got, want := transformed.Messages[0].Role, sigma.RoleUser; got != want {
		t.Fatalf("developer role conversion = %q, want %q", got, want)
	}
	if got, want := transformed.Messages[0].Content[0].Text, "Prefer concise answers."; got != want {
		t.Fatalf("developer content = %q, want %q", got, want)
	}
}

func TestTransformPreservesOrRejectsImageToolResults(t *testing.T) {
	t.Parallel()

	request := sigma.Request{
		Messages: []sigma.Message{
			{
				Role:       sigma.RoleTool,
				ToolCallID: "call_image",
				ToolName:   "screenshot",
				Content:    []sigma.ContentBlock{sigma.ImageBase64("image/png", "aW1hZ2U=")},
			},
		},
	}

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "gemini-2.5-pro",
			Provider: sigma.ProviderGoogle,
			API:      sigma.APIGoogleGenerativeAI,
			SupportedInputs: []sigma.ContentBlockType{
				sigma.ContentBlockText,
				sigma.ContentBlockImage,
			},
		},
		Request: request,
	})
	if err != nil {
		t.Fatalf("Transform returned error for image-capable model: %v", err)
	}
	image := transformed.Messages[0].Content[0]
	if got, want := image.Type, sigma.ContentBlockImage; got != want {
		t.Fatalf("image type = %q, want %q", got, want)
	}
	if got, want := image.Data, "aW1hZ2U="; got != want {
		t.Fatalf("image data = %q, want %q", got, want)
	}

	_, err = Transform(Input{
		TargetModel: sigma.Model{
			ID:       "gpt-4.1",
			Provider: sigma.ProviderOpenAI,
			API:      sigma.APIOpenAIResponses,
		},
		Request: request,
	})
	if err == nil {
		t.Fatal("Transform succeeded for image content on text-only model")
	}
	var transformErr *sigma.Error
	if !errors.As(err, &transformErr) {
		t.Fatalf("error type = %T, want *sigma.Error", err)
	}
	if got, want := transformErr.Code, sigma.ErrorUnsupported; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
}

func TestTransformKeepsAbortedAssistantPartialContent(t *testing.T) {
	t.Parallel()

	transformed, err := Transform(Input{
		TargetModel: sigma.Model{
			ID:       "gpt-4.1",
			Provider: sigma.ProviderOpenAI,
			API:      sigma.APIOpenAIResponses,
		},
		Request: sigma.Request{
			Messages: []sigma.Message{
				{
					Role:       sigma.RoleAssistant,
					Provider:   sigma.ProviderAnthropic,
					API:        sigma.APIAnthropicMessages,
					StopReason: sigma.StopReasonAborted,
					Content: []sigma.ContentBlock{
						sigma.Thinking("partial plan", ""),
						sigma.Text("Partial answer"),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}

	message := transformed.Messages[0]
	if got, want := message.StopReason, sigma.StopReasonAborted; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := message.Content[0].Text, "<thinking>\npartial plan\n</thinking>"; got != want {
		t.Fatalf("partial thinking text = %q, want %q", got, want)
	}
	if got, want := message.Content[1].Text, "Partial answer"; got != want {
		t.Fatalf("partial assistant text = %q, want %q", got, want)
	}
}
