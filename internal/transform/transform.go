// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package transform

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/wintermi/sigma"
)

const (
	defaultThinkingStartDelimiter = "<thinking>"
	defaultThinkingEndDelimiter   = "</thinking>"
)

// Input describes a provider-neutral request transformation.
type Input struct {
	TargetModel   sigma.Model
	Request       sigma.Request
	Compatibility Compatibility
	Policy        Policy
}

// Compatibility describes target-provider constraints that are not represented
// by generic model metadata.
type Compatibility struct {
	ThinkingAsText                  bool
	AssistantAfterToolResultRepair  bool
	AssistantAfterToolResultMessage sigma.Message
	RequireToolResultName           bool
	ConvertDeveloperRole            bool
	DropUnansweredToolCalls         bool
	NormalizeToolCallID             func(string) string
}

// Policy controls lossless request normalization choices.
type Policy struct {
	ThinkingStartDelimiter string
	ThinkingEndDelimiter   string
	AllowUnsupportedImages bool
}

// SafeToolCallIDNormalizer returns a stateful normalizer that preserves a
// stable mapping from original tool-call IDs to provider-safe IDs.
func SafeToolCallIDNormalizer(limit int) func(string) string {
	mapped := make(map[string]string)
	used := make(map[string]string)
	return func(id string) string {
		if id == "" {
			return id
		}
		if normalized := mapped[id]; normalized != "" {
			return normalized
		}
		base := safeToolCallID(id, limit)
		for attempt := 0; ; attempt++ {
			candidate := base
			if attempt > 0 {
				suffix := "_" + strconv.Itoa(attempt)
				candidate = strings.TrimRight(base[:min(len(base), max(1, limit-len(suffix)))], "_") + suffix
			}
			if owner := used[candidate]; owner == "" || owner == id {
				mapped[id] = candidate
				used[candidate] = id
				return candidate
			}
		}
	}
}

func safeToolCallID(id string, limit int) string {
	var out strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_',
			r == '-':
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}
		if out.Len() == limit {
			break
		}
	}
	if out.Len() == 0 {
		return "_"
	}
	return out.String()
}

// Transform returns a transformed copy of input.Request for input.TargetModel.
// It never mutates the caller's request, message content, tool schemas, or
// provider metadata.
func Transform(input Input) (sigma.Request, error) {
	if err := sigma.ValidateModelRef(sigma.ModelRef{Provider: input.TargetModel.Provider, ID: input.TargetModel.ID}); err != nil {
		return sigma.Request{}, err
	}

	policy := input.Policy.withDefaults()
	output := sigma.Request{
		SystemPrompt: input.Request.SystemPrompt,
		Tools:        cloneTools(input.Request.Tools),
	}

	toolNamesByID := make(map[string]string)
	for index, message := range input.Request.Messages {
		transformed, err := transformMessage(message, messageContext{
			target:       input.TargetModel,
			compat:       input.Compatibility,
			policy:       policy,
			toolNames:    toolNamesByID,
			messageIndex: index,
		})
		if err != nil {
			return sigma.Request{}, err
		}

		output.Messages = append(output.Messages, transformed)
		recordToolCalls(toolNamesByID, transformed)
	}

	if input.Compatibility.DropUnansweredToolCalls {
		output.Messages = dropUnansweredToolCalls(output.Messages)
	}
	if input.Compatibility.AssistantAfterToolResultRepair {
		output.Messages = insertAssistantAfterToolResults(output.Messages, input.Compatibility.AssistantAfterToolResultMessage)
	}

	return output, nil
}

// DropUnansweredToolCalls returns a copy of req with synthetic error tool
// results inserted for replayed assistant tool calls that have no result.
func DropUnansweredToolCalls(req sigma.Request) sigma.Request {
	return sigma.Request{
		SystemPrompt: req.SystemPrompt,
		Messages:     synthesizeUnansweredToolResults(cloneMessages(req.Messages)),
		Tools:        cloneTools(req.Tools),
	}
}

func dropUnansweredToolCalls(messages []sigma.Message) []sigma.Message {
	return synthesizeUnansweredToolResults(messages)
}

func synthesizeUnansweredToolResults(messages []sigma.Message) []sigma.Message {
	if len(messages) == 0 {
		return nil
	}
	repaired := make([]sigma.Message, 0, len(messages))
	var pending []sigma.ContentBlock
	answered := make(map[string]struct{})
	insertMissing := func() {
		for _, call := range pending {
			if _, ok := answered[call.ToolCallID]; ok {
				continue
			}
			repaired = append(repaired, syntheticToolResult(call))
		}
		pending = nil
		answered = make(map[string]struct{})
	}
	for _, message := range messages {
		switch message.Role {
		case sigma.RoleAssistant:
			insertMissing()
			repaired = append(repaired, message)
			pending = assistantToolCalls(message)
		case sigma.RoleUser, sigma.RoleDeveloper:
			insertMissing()
			repaired = append(repaired, message)
		case sigma.RoleTool:
			if hasPendingToolCall(pending, message.ToolCallID) {
				answered[message.ToolCallID] = struct{}{}
			}
			repaired = append(repaired, message)
		default:
			repaired = append(repaired, message)
		}
	}
	insertMissing()
	return repaired
}

func assistantToolCalls(message sigma.Message) []sigma.ContentBlock {
	var calls []sigma.ContentBlock
	for _, block := range message.Content {
		if isClientToolCall(block) {
			calls = append(calls, block)
		}
	}
	return calls
}

func hasPendingToolCall(calls []sigma.ContentBlock, id string) bool {
	for _, call := range calls {
		if call.ToolCallID == id {
			return true
		}
	}
	return false
}

func syntheticToolResult(call sigma.ContentBlock) sigma.Message {
	return sigma.Message{
		Role:       sigma.RoleTool,
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		IsError:    true,
		Content:    []sigma.ContentBlock{sigma.Text("No result provided")},
	}
}

func insertAssistantAfterToolResults(messages []sigma.Message, message sigma.Message) []sigma.Message {
	if len(messages) == 0 {
		return nil
	}
	repaired := make([]sigma.Message, 0, len(messages)+1)
	for _, current := range messages {
		if (current.Role == sigma.RoleUser || current.Role == sigma.RoleDeveloper) &&
			len(repaired) > 0 &&
			repaired[len(repaired)-1].Role == sigma.RoleTool {
			repaired = append(repaired, repairMessage(message))
		}
		repaired = append(repaired, current)
	}
	return repaired
}

func isClientToolCall(block sigma.ContentBlock) bool {
	if block.Type != sigma.ContentBlockToolCall {
		return false
	}
	metadataType, _ := block.ProviderMetadata["type"].(string)
	return metadataType != "server_tool_use"
}

type messageContext struct {
	target       sigma.Model
	compat       Compatibility
	policy       Policy
	toolNames    map[string]string
	messageIndex int
}

func transformMessage(message sigma.Message, ctx messageContext) (sigma.Message, error) {
	transformed := cloneMessage(message)
	if ctx.compat.ConvertDeveloperRole && transformed.Role == sigma.RoleDeveloper {
		transformed.Role = sigma.RoleUser
	}

	content := make([]sigma.ContentBlock, 0, len(message.Content))
	for _, block := range message.Content {
		if err := validateImageSupport(block, ctx); err != nil {
			return sigma.Message{}, err
		}
		if transformed.Role == sigma.RoleAssistant && block.Type == sigma.ContentBlockThinking && shouldConvertThinking(message, ctx) {
			if strings.TrimSpace(block.ThinkingText) == "" || block.Redacted {
				continue
			}
			content = append(content, sigma.Text(wrapThinking(block.ThinkingText, ctx.policy)))
			continue
		}
		cloned := block.Clone()
		if cloned.Type == sigma.ContentBlockToolCall && ctx.compat.NormalizeToolCallID != nil {
			cloned.ToolCallID = ctx.compat.NormalizeToolCallID(cloned.ToolCallID)
		}
		content = append(content, cloned)
	}
	transformed.Content = content

	if transformed.Role == sigma.RoleTool && ctx.compat.NormalizeToolCallID != nil {
		transformed.ToolCallID = ctx.compat.NormalizeToolCallID(transformed.ToolCallID)
	}
	if transformed.Role == sigma.RoleTool && ctx.compat.RequireToolResultName && transformed.ToolName == "" {
		toolName, ok := ctx.toolNames[transformed.ToolCallID]
		if !ok {
			return sigma.Message{}, transformError(ctx, "tool result requires a tool name but no matching assistant tool call was found")
		}
		transformed.ToolName = toolName
	}

	return transformed, nil
}

func shouldConvertThinking(message sigma.Message, ctx messageContext) bool {
	if ctx.compat.ThinkingAsText {
		return true
	}
	if !ctx.target.SupportsReasoning() {
		return true
	}
	if message.Provider != "" && message.Provider != ctx.target.Provider {
		return true
	}
	if message.API != "" && ctx.target.API != "" && message.API != ctx.target.API {
		return true
	}
	return message.Model != "" && ctx.target.ID != "" && message.Model != ctx.target.ID
}

func validateImageSupport(block sigma.ContentBlock, ctx messageContext) error {
	if block.Type != sigma.ContentBlockImage || ctx.policy.AllowUnsupportedImages || ctx.target.SupportsImages() {
		return nil
	}
	return transformError(ctx, "target model does not support image content")
}

func wrapThinking(text string, policy Policy) string {
	return policy.ThinkingStartDelimiter + "\n" + text + "\n" + policy.ThinkingEndDelimiter
}

func recordToolCalls(toolNamesByID map[string]string, message sigma.Message) {
	if message.Role != sigma.RoleAssistant {
		return
	}
	for _, block := range message.Content {
		if block.Type == sigma.ContentBlockToolCall && block.ToolCallID != "" && block.ToolName != "" {
			toolNamesByID[block.ToolCallID] = block.ToolName
		}
	}
}

func repairMessage(message sigma.Message) sigma.Message {
	if message.Role != "" {
		return cloneMessage(message)
	}
	return sigma.Message{
		Role:    sigma.RoleAssistant,
		Content: []sigma.ContentBlock{sigma.Text("I have processed the tool results.")},
	}
}

func transformError(ctx messageContext, message string) error {
	return &sigma.Error{
		Code:     sigma.ErrorUnsupported,
		Message:  fmt.Sprintf("transform message %d: %s", ctx.messageIndex, message),
		Provider: ctx.target.Provider,
		Model:    ctx.target.ID,
	}
}

func (policy Policy) withDefaults() Policy {
	if policy.ThinkingStartDelimiter == "" {
		policy.ThinkingStartDelimiter = defaultThinkingStartDelimiter
	}
	if policy.ThinkingEndDelimiter == "" {
		policy.ThinkingEndDelimiter = defaultThinkingEndDelimiter
	}
	return policy
}

func cloneMessage(message sigma.Message) sigma.Message {
	message.Content = cloneContentBlocks(message.Content)
	return message
}

func cloneMessages(messages []sigma.Message) []sigma.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]sigma.Message, len(messages))
	for index, message := range messages {
		cloned[index] = cloneMessage(message)
	}
	return cloned
}

func cloneContentBlocks(blocks []sigma.ContentBlock) []sigma.ContentBlock {
	if len(blocks) == 0 {
		return nil
	}
	cloned := make([]sigma.ContentBlock, len(blocks))
	for index, block := range blocks {
		cloned[index] = block.Clone()
	}
	return cloned
}

func cloneTool(tool sigma.Tool) sigma.Tool {
	tool.InputSchema = cloneAny(tool.InputSchema)
	tool.ProviderDefinedOptions = cloneProviderDefinedOptions(tool.ProviderDefinedOptions)
	tool.ProviderMetadata = cloneStringAnyMap(tool.ProviderMetadata)
	return tool
}

func cloneTools(tools []sigma.Tool) []sigma.Tool {
	if len(tools) == 0 {
		return nil
	}
	cloned := make([]sigma.Tool, len(tools))
	for index, tool := range tools {
		cloned[index] = cloneTool(tool)
	}
	return cloned
}

func cloneStringAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneAny(value)
	}
	return cloned
}

func cloneProviderDefinedOptions(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneProviderDefinedOption(value)
	}
	return cloned
}

func cloneProviderDefinedOption(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return cloneProviderDefinedOptions(v)
	case sigma.Schema:
		cloned := make(sigma.Schema, len(v))
		for key, value := range v {
			cloned[key] = cloneProviderDefinedOption(value)
		}
		return cloned
	case []any:
		cloned := make([]any, len(v))
		for index, item := range v {
			cloned[index] = cloneProviderDefinedOption(item)
		}
		return cloned
	case []string:
		return append([]string(nil), v...)
	case []byte:
		return append([]byte(nil), v...)
	case json.RawMessage:
		return append(json.RawMessage(nil), v...)
	default:
		return v
	}
}

func cloneAny(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return cloneStringAnyMap(v)
	case sigma.Schema:
		cloned := make(sigma.Schema, len(v))
		for key, value := range v {
			cloned[key] = cloneAny(value)
		}
		return cloned
	case []any:
		cloned := make([]any, len(v))
		for index, item := range v {
			cloned[index] = cloneAny(item)
		}
		return cloned
	case []string:
		return append([]string(nil), v...)
	case []byte:
		return append([]byte(nil), v...)
	case json.RawMessage:
		return append(json.RawMessage(nil), v...)
	default:
		return v
	}
}
