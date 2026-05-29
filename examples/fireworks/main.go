// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/fireworks"
)

const firepassModelID = sigma.ModelID("accounts/fireworks/routers/kimi-k2p6-turbo")

func main() {
	if os.Getenv("FIREWORKS_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "set FIREWORKS_API_KEY to run the live Fireworks Firepass demo")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, model, err := firepassDemoClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
		os.Exit(1)
	}

	tools := []sigma.Tool{demoProjectTool(), demoRuntimeTool()}
	question := "Call get_demo_project with include_modules=true and get_demo_runtime with component=\"fireworks-demo\", then explain what this Sigma package helps Go applications do in one short sentence."
	messages := []sigma.Message{sigma.UserText(question)}
	fmt.Printf("Question: %s\n", question)

	for turn := range 4 {
		final, err := client.Complete(ctx, model, sigma.Request{
			SystemPrompt: "You are demonstrating github.com/wintermi/sigma, a Go package for provider-neutral model calls across text, streaming, tools, persistence, and custom OpenAI-compatible endpoints. Use the provided tools before answering. Do not describe Sigma security rules, SIEM, or log detection. Answer directly in one concise sentence.",
			Messages:     messages,
			Tools:        tools,
		},
			sigma.WithReasoningLevel(sigma.ThinkingLevelLow),
			sigma.WithMaxTokens(model.MaxOutputTokens),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Complete returned error: %v\n", err)
			os.Exit(1)
		}

		messages = append(messages, assistantMessage(final))
		if final.StopReason != sigma.StopReasonToolCalls {
			response := textContent(final)
			if response == "" {
				fmt.Fprintf(os.Stderr, "response contained no text blocks: %#v\n", final.Content)
				os.Exit(1)
			}
			fmt.Printf("Response: %s\n", response)
			return
		}

		calls := toolCalls(final)
		if len(calls) == 0 {
			fmt.Fprintln(os.Stderr, "model stopped for tool calls without returning any tool calls")
			os.Exit(1)
		}

		for _, call := range calls {
			args, err := sigma.ValidateToolCall(tools, call)
			if err != nil {
				message := sigma.ToolErrorMessage(call, err)
				fmt.Printf("Tool: %s args=<invalid> error=%s\n", call.Name, message)
				messages = append(messages, sigma.ToolError(call.ID, message))
				continue
			}

			result, err := runTool(call.Name, args)
			if err != nil {
				fmt.Printf("Tool: %s args=%s error=%s\n", call.Name, mustJSON(args), err)
				messages = append(messages, sigma.ToolError(call.ID, err.Error()))
				continue
			}
			fmt.Printf("Tool: %s args=%s result=%s\n", call.Name, mustJSON(args), result)
			messages = append(messages, sigma.ToolResult(call.ID, result))
		}

		if turn == 3 {
			fmt.Fprintln(os.Stderr, "model did not produce a final response after tool results")
			os.Exit(1)
		}
	}
}

func firepassDemoClient() (*sigma.Client, sigma.Model, error) {
	registry := sigma.DefaultRegistry()
	if err := fireworks.Register(registry); err != nil {
		return nil, sigma.Model{}, fmt.Errorf("register Fireworks provider: %w", err)
	}
	model, ok := registry.Model(sigma.ProviderFireworks, firepassModelID)
	if !ok {
		return nil, sigma.Model{}, fmt.Errorf("firepass model %q was not registered", firepassModelID)
	}
	if model.Provider != sigma.ProviderFireworks {
		return nil, sigma.Model{}, fmt.Errorf("model provider = %q, want %q", model.Provider, sigma.ProviderFireworks)
	}
	if model.API != sigma.APIOpenAICompletions {
		return nil, sigma.Model{}, fmt.Errorf("model API = %q, want %q", model.API, sigma.APIOpenAICompletions)
	}
	return sigma.NewClient(sigma.WithRegistry(registry)), model, nil
}

func demoProjectTool() sigma.Tool {
	return sigma.Tool{
		Name:        "get_demo_project",
		Description: "Return fixed facts about the Sigma demo project.",
		InputSchema: sigma.Schema{
			"type": "object",
			"properties": map[string]any{
				"include_modules": map[string]any{
					"type":        "boolean",
					"description": "Whether to include a few example capability areas.",
				},
			},
			"required":             []any{"include_modules"},
			"additionalProperties": false,
		},
	}
}

func demoRuntimeTool() sigma.Tool {
	return sigma.Tool{
		Name:        "get_demo_runtime",
		Description: "Return fixed runtime details for this live Fireworks example.",
		InputSchema: sigma.Schema{
			"type": "object",
			"properties": map[string]any{
				"component": map[string]any{
					"type":        "string",
					"description": "Demo component name, for example fireworks-demo.",
				},
			},
			"required":             []any{"component"},
			"additionalProperties": false,
		},
	}
}

func runTool(name string, args map[string]any) (string, error) {
	var payload map[string]any
	switch name {
	case "get_demo_project":
		payload = map[string]any{
			"name":        "Sigma",
			"language":    "Go",
			"description": "Provider-neutral model calls for Go applications.",
		}
		if includeModules, _ := args["include_modules"].(bool); includeModules {
			payload["modules"] = []string{"text", "streaming", "tools", "persistence", "custom OpenAI-compatible endpoints"}
		}
	case "get_demo_runtime":
		component, _ := args["component"].(string)
		payload = map[string]any{
			"component": component,
			"provider":  "fireworks",
			"status":    "demo-ready",
		}
	default:
		return "", fmt.Errorf("unknown tool %q", name)
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

func textContent(message sigma.AssistantMessage) string {
	var builder strings.Builder
	for _, block := range message.Content {
		if block.Type == sigma.ContentBlockText {
			builder.WriteString(block.Text)
		}
	}
	return builder.String()
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(data)
}
