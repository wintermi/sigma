// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/jsonutil"
)

const (
	hostedReplayKey   = "anthropic_hosted_replay"
	serverToolUseType = "server_tool_use"
)

type hostedReplay struct {
	Provider   sigma.ProviderID `json:"provider"`
	API        sigma.API        `json:"api"`
	Model      sigma.ModelID    `json:"model"`
	ServerCall map[string]any   `json:"server_tool_use,omitempty"`
	Results    []map[string]any `json:"results_after,omitempty"`
}

func supportedHostedResult(kind string) bool {
	switch kind {
	case "web_search_tool_result", "web_fetch_tool_result", "code_execution_tool_result",
		"bash_code_execution_tool_result", "text_editor_code_execution_tool_result":
		return true
	default:
		return false
	}
}

// UnmarshalJSON keeps the complete provider block alongside the represented
// fields. Hosted results and server calls contain opaque replay data.
func (c *streamContent) UnmarshalJSON(data []byte) error {
	type plain streamContent
	var value plain
	if err := jsonutil.Decode(data, &value); err != nil {
		return err
	}
	*c = streamContent(value)
	if c.Type == serverToolUseType || supportedHostedResult(c.Type) {
		return jsonutil.Decode(data, &c.Raw)
	}
	return nil
}

func (p *streamParser) captureHostedResult(content streamContent) error {
	id, _ := content.Raw["tool_use_id"].(string)
	found := false
	for _, call := range p.toolCalls {
		if call.ID() == id && providerMetadataString(call.ProviderMetadata, "type") == serverToolUseType {
			found = id != ""
		}
	}
	if !found || p.nextBlock == 0 {
		return sigma.NewProviderError(p.model.Provider, p.model.API, p.model.ID, http.StatusOK, p.responseID, 0,
			[]byte(`{"error":{"message":"hosted result has no preceding server tool call"}}`), sigma.ErrProviderResponse)
	}
	if p.hostedResults == nil {
		p.hostedResults = make(map[int][]map[string]any)
	}
	anchor := p.nextBlock - 1
	p.hostedResults[anchor] = append(p.hostedResults[anchor], content.Raw)
	return nil
}

func invalidRequestErrorf(format string, args ...any) error {
	return &sigma.Error{Code: sigma.ErrorInvalidRequest, Message: fmt.Sprintf("anthropic messages: "+format, args...)}
}

func decodeHostedReplay(value any) (hostedReplay, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return hostedReplay{}, invalidRequestErrorf("invalid hosted replay: %v", err)
	}
	var replay hostedReplay
	var fields map[string]json.RawMessage
	if err := jsonutil.Decode(data, &fields); err != nil {
		return replay, invalidRequestErrorf("invalid hosted replay: %v", err)
	}
	for _, key := range []string{serverToolUseType, "results_after"} {
		if raw, ok := fields[key]; ok && string(raw) == "null" {
			return replay, invalidRequestErrorf("null hosted replay field %q", key)
		}
	}
	if string(data) == "null" {
		return replay, invalidRequestErrorf("null hosted replay")
	}
	if err := jsonutil.Decode(data, &replay); err != nil {
		return replay, invalidRequestErrorf("invalid hosted replay: %v", err)
	}
	return replay, nil
}

func anthropicReplayContent(message sigma.Message, compat messagesCompat) ([]map[string]any, error) {
	content := make([]map[string]any, 0, len(message.Content))
	serverCalls := make(map[string]bool)
	for _, block := range message.Content {
		converted, err := anthropicAssistantContent([]sigma.ContentBlock{block}, compat)
		if err != nil {
			return nil, err
		}
		server := block.Type == sigma.ContentBlockToolCall && providerMetadataString(block.ProviderMetadata, "type") == serverToolUseType
		if server {
			if block.ToolCallID == "" || serverCalls[block.ToolCallID] {
				return nil, invalidRequestErrorf("missing or duplicate server call id")
			}
			serverCalls[block.ToolCallID] = true
		}
		value, ok := block.ProviderMetadata[hostedReplayKey]
		if !ok {
			content = append(content, converted...)
			continue
		}
		replay, err := decodeHostedReplay(value)
		if err != nil {
			return nil, err
		}
		if replay.ServerCall != nil {
			if !server || len(converted) != 1 || replay.ServerCall["type"] != serverToolUseType || replay.ServerCall["id"] != block.ToolCallID {
				return nil, invalidRequestErrorf("server call does not match replay anchor")
			}
			for key, value := range converted[0] {
				replay.ServerCall[key] = value
			}
			converted[0] = replay.ServerCall
		}
		for _, result := range replay.Results {
			kind, _ := result["type"].(string)
			id, _ := result["tool_use_id"].(string)
			if !supportedHostedResult(kind) || id == "" || !serverCalls[id] {
				return nil, invalidRequestErrorf("hosted result has no matching preceding server call")
			}
		}
		converted = append(converted, replay.Results...)
		content = append(content, converted...)
	}
	return content, nil
}

func hostedReplayMetadata(model sigma.Model) map[string]any {
	return map[string]any{"provider": string(model.Provider), "api": string(model.API), "model": string(model.ID)}
}
