// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func main() {
	tools := []sigma.Tool{weatherTool()}
	provider := sigmatest.NewFauxProvider(
		sigmatest.Script{
			Final: sigma.AssistantMessage{
				Content:    []sigma.ContentBlock{sigma.ToolCallBlock("call_bad", "weather", map[string]any{"city": json.Number("42")})},
				StopReason: sigma.StopReasonToolCalls,
			},
		},
		sigmatest.Script{
			Final: sigma.AssistantMessage{
				Content:    []sigma.ContentBlock{sigma.ToolCallBlock("call_good", "weather", map[string]any{"city": "Melbourne", "units": "celsius"})},
				StopReason: sigma.StopReasonToolCalls,
			},
		},
		sigmatest.Script{
			Final: sigma.AssistantMessage{
				Content: []sigma.ContentBlock{sigma.Text("Melbourne is 18 C and clear.")},
			},
		},
	)
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		log.Fatal(err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	messages := []sigma.Message{sigma.UserText("What is the weather in Melbourne?")}

	for {
		final, err := client.Complete(context.Background(), sigmatest.TextModel(), sigma.Request{
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			log.Fatal(err)
		}

		messages = append(messages, assistantMessage(final))
		if final.StopReason != sigma.StopReasonToolCalls {
			fmt.Println(text(final))
			return
		}

		for _, call := range toolCalls(final) {
			args, err := sigma.ValidateToolCall(tools, call)
			if err != nil {
				messages = append(messages, sigma.ToolError(call.ID, sigma.ToolErrorMessage(call, err)))
				continue
			}

			result, err := runTool(call.Name, args)
			if err != nil {
				messages = append(messages, sigma.ToolError(call.ID, err.Error()))
				continue
			}
			messages = append(messages, sigma.ToolResult(call.ID, result))
		}
	}
}

func weatherTool() sigma.Tool {
	return sigma.Tool{
		Name:        "weather",
		Description: "Look up current weather for a city.",
		InputSchema: sigma.Schema{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{
					"type":        "string",
					"description": "City name, for example Melbourne.",
				},
				"units": map[string]any{
					"type": "string",
					"enum": []any{"celsius", "fahrenheit"},
				},
			},
			"required":             []any{"city"},
			"additionalProperties": false,
		},
	}
}

func runTool(name string, args map[string]any) (string, error) {
	if name != "weather" {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	city, _ := args["city"].(string)
	units, _ := args["units"].(string)
	if units == "" {
		units = "celsius"
	}

	payload := map[string]any{
		"city":        city,
		"temperature": 18,
		"units":       units,
		"conditions":  "clear",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func assistantMessage(final sigma.AssistantMessage) sigma.Message {
	return sigma.Message{
		Role:       sigma.RoleAssistant,
		Content:    final.Content,
		Provider:   final.Provider,
		Model:      final.Model,
		StopReason: final.StopReason,
	}
}

func toolCalls(final sigma.AssistantMessage) []sigma.ToolCall {
	var calls []sigma.ToolCall
	for _, block := range final.Content {
		if block.Type == sigma.ContentBlockToolCall {
			calls = append(calls, sigma.ToolCall{
				ID:        block.ToolCallID,
				Name:      block.ToolName,
				Arguments: block.ToolArguments,
			})
		}
	}
	return calls
}

func text(final sigma.AssistantMessage) string {
	var out string
	for _, block := range final.Content {
		if block.Type == sigma.ContentBlockText {
			out += block.Text
		}
	}
	return out
}
