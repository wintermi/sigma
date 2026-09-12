// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package transform

import (
	"encoding/json"
	"strings"

	"github.com/wintermi/sigma"
)

// PrepareReplay removes incomplete tool exchanges before normalization or tool
// planning. Visible text from interrupted turns remains useful conversation
// context. The original history is never changed.
func PrepareReplay(model sigma.Model, req sigma.Request) sigma.Request {
	discarded := make(map[string]bool)
	messages := make([]sigma.Message, 0, len(req.Messages))
	for _, message := range req.Messages {
		if message.Role != sigma.RoleAssistant {
			messages = append(messages, message)
			continue
		}
		failed := message.StopReason == sigma.StopReasonError || message.StopReason == sigma.StopReasonAborted
		compatible := message.Provider == model.Provider && message.API == model.API && message.Model == model.ID &&
			model.API == sigma.APIAnthropicMessages && hostedProvenanceMatches(model, message)
		content := make([]sigma.ContentBlock, 0, len(message.Content))
		for _, block := range message.Content {
			hosted := block.Type == sigma.ContentBlockToolCall && !isClientToolCall(block)
			if failed || (hosted && !compatible) {
				if block.Type == sigma.ContentBlockToolCall {
					discarded[block.ToolCallID] = true
				}
				if failed && block.Type == sigma.ContentBlockText && strings.TrimSpace(block.Text) != "" {
					content = append(content, sigma.Text(block.Text))
				}
				continue
			}
			if !compatible && block.ProviderMetadata != nil {
				block = block.Clone()
				delete(block.ProviderMetadata, "anthropic_hosted_replay")
			}
			content = append(content, block)
		}
		if len(content) == 0 && (failed || len(message.Content) > 0) {
			continue
		}
		message.Content = content
		if failed {
			message.ProviderThinkingLevel = ""
		}
		messages = append(messages, message)
	}
	req.Messages = make([]sigma.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role != sigma.RoleTool || !discarded[message.ToolCallID] {
			req.Messages = append(req.Messages, message)
		}
	}
	return req
}

// Malformed envelopes with compatible containing-message provenance are left
// for the Anthropic serializer to reject with a local invalid-request error.
func hostedProvenanceMatches(model sigma.Model, message sigma.Message) bool {
	for _, block := range message.Content {
		value, ok := block.ProviderMetadata["anthropic_hosted_replay"]
		if !ok {
			continue
		}
		data, err := json.Marshal(value)
		if err != nil {
			continue
		}
		var source struct {
			Provider sigma.ProviderID
			API      sigma.API
			Model    sigma.ModelID
		}
		if err := json.Unmarshal(data, &source); err != nil || string(data) == "null" {
			continue
		}
		if source.Provider != model.Provider || source.API != model.API || source.Model != model.ID {
			return false
		}
	}
	return true
}
