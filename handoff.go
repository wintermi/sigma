// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"fmt"
)

const (
	defaultHandoffThinkingStartDelimiter = "<thinking>"
	defaultHandoffThinkingEndDelimiter   = "</thinking>"
)

// HandoffChangeKind identifies one request adaptation made during handoff.
type HandoffChangeKind string

const (
	// HandoffChangeThinkingConverted indicates a thinking block was converted
	// to text for a target that cannot safely replay it natively.
	HandoffChangeThinkingConverted HandoffChangeKind = "thinking-converted"
	// HandoffChangeDeveloperRoleConverted indicates a developer message was
	// converted to a user message for target compatibility.
	HandoffChangeDeveloperRoleConverted HandoffChangeKind = "developer-role-converted"
	// HandoffChangeToolResultNameRepaired indicates a missing tool result name
	// was filled from a prior assistant tool call.
	HandoffChangeToolResultNameRepaired HandoffChangeKind = "tool-result-name-repaired"
	// HandoffChangeUnansweredToolCallDropped indicates an assistant tool call
	// without a matching tool result before the next user/developer turn was
	// removed.
	HandoffChangeUnansweredToolCallDropped HandoffChangeKind = "unanswered-tool-call-dropped"
	// HandoffChangeRepairMessageInserted indicates a small user continuation
	// message was inserted between a tool result and following assistant turn.
	HandoffChangeRepairMessageInserted HandoffChangeKind = "repair-message-inserted"
	// HandoffChangeUnsupportedImageReplaced indicates an image block was
	// replaced with caller-supplied text for a non-vision target.
	HandoffChangeUnsupportedImageReplaced HandoffChangeKind = "unsupported-image-replaced"
)

// HandoffChange describes one source-neutral transformation.
type HandoffChange struct {
	Kind         HandoffChangeKind `json:"kind"`
	MessageIndex int               `json:"messageIndex"`
	ContentIndex *int              `json:"contentIndex,omitempty"`
	Detail       string            `json:"detail,omitempty"`
}

// HandoffReport summarizes adaptations made for a target model.
type HandoffReport struct {
	ConvertedThinkingBlocks    int             `json:"convertedThinkingBlocks,omitempty"`
	ConvertedDeveloperMessages int             `json:"convertedDeveloperMessages,omitempty"`
	RepairedToolResultNames    int             `json:"repairedToolResultNames,omitempty"`
	DroppedUnansweredToolCalls int             `json:"droppedUnansweredToolCalls,omitempty"`
	InsertedRepairMessages     int             `json:"insertedRepairMessages,omitempty"`
	ReplacedUnsupportedImages  int             `json:"replacedUnsupportedImages,omitempty"`
	Changes                    []HandoffChange `json:"changes,omitempty"`
}

// HandoffResult is a transformed request and its adaptation report.
type HandoffResult struct {
	Request Request       `json:"request"`
	Report  HandoffReport `json:"report,omitempty"`
}

// HandoffMessagesResult is a transformed message list and its adaptation report.
type HandoffMessagesResult struct {
	Messages []Message     `json:"messages,omitempty"`
	Report   HandoffReport `json:"report,omitempty"`
}

// HandoffOption configures public cross-provider request adaptation.
type HandoffOption func(*handoffConfig)

// WithHandoffThinkingDelimiters configures the text wrappers used when
// provider-native thinking blocks are converted to text.
func WithHandoffThinkingDelimiters(start string, end string) HandoffOption {
	return func(config *handoffConfig) {
		config.thinkingStartDelimiter = start
		config.thinkingEndDelimiter = end
	}
}

// WithHandoffUnsupportedImageReplacement replaces unsupported image blocks
// with the supplied text instead of returning an unsupported-content error.
func WithHandoffUnsupportedImageReplacement(text string) HandoffOption {
	return func(config *handoffConfig) {
		config.unsupportedImageReplacement = &text
	}
}

// TransformRequestForModel adapts a request for replay against target. The
// helper is opt-in and does not mutate the caller's request.
func TransformRequestForModel(target Model, req Request, opts ...HandoffOption) (HandoffResult, error) {
	if err := ValidateModelRef(ModelRef{Provider: target.Provider, ID: target.ID}); err != nil {
		return HandoffResult{}, err
	}

	config := newHandoffConfig(opts...)
	compat := handoffCompatibilityForModel(target)
	report := HandoffReport{}
	output := Request{
		SystemPrompt: req.SystemPrompt,
		Tools:        cloneHandoffTools(req.Tools),
	}

	toolNamesByID := make(map[string]string)
	for index, message := range req.Messages {
		transformed, err := transformHandoffMessage(message, handoffMessageContext{
			target:       target,
			compat:       compat,
			config:       config,
			toolNames:    toolNamesByID,
			messageIndex: index,
			report:       &report,
		})
		if err != nil {
			return HandoffResult{}, err
		}

		if compat.assistantAfterToolResultRepair &&
			len(output.Messages) > 0 &&
			output.Messages[len(output.Messages)-1].Role == RoleTool &&
			transformed.Role == RoleAssistant {
			output.Messages = append(output.Messages, UserText("Continue."))
			report.record(HandoffChangeRepairMessageInserted, index, nil, "inserted continuation before assistant message")
		}

		output.Messages = append(output.Messages, transformed)
		recordHandoffToolCalls(toolNamesByID, transformed)
	}

	if compat.dropUnansweredToolCalls {
		output.Messages = dropUnansweredHandoffToolCalls(output.Messages, &report)
	}

	return HandoffResult{Request: output, Report: report}, nil
}

// TransformMessagesForModel adapts a message list for replay against target.
// It is equivalent to TransformRequestForModel with only Request.Messages set.
func TransformMessagesForModel(target Model, messages []Message, opts ...HandoffOption) (HandoffMessagesResult, error) {
	result, err := TransformRequestForModel(target, Request{Messages: messages}, opts...)
	if err != nil {
		return HandoffMessagesResult{}, err
	}
	return HandoffMessagesResult{Messages: result.Request.Messages, Report: result.Report}, nil
}

type handoffConfig struct {
	thinkingStartDelimiter      string
	thinkingEndDelimiter        string
	unsupportedImageReplacement *string
}

func newHandoffConfig(opts ...HandoffOption) handoffConfig {
	config := handoffConfig{
		thinkingStartDelimiter: defaultHandoffThinkingStartDelimiter,
		thinkingEndDelimiter:   defaultHandoffThinkingEndDelimiter,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}
	if config.thinkingStartDelimiter == "" {
		config.thinkingStartDelimiter = defaultHandoffThinkingStartDelimiter
	}
	if config.thinkingEndDelimiter == "" {
		config.thinkingEndDelimiter = defaultHandoffThinkingEndDelimiter
	}
	return config
}

type handoffCompatibility struct {
	assistantAfterToolResultRepair bool
	requireToolResultName          bool
	convertDeveloperRole           bool
	dropUnansweredToolCalls        bool
}

func handoffCompatibilityForModel(model Model) handoffCompatibility {
	compat := handoffCompatibility{dropUnansweredToolCalls: true}
	if model.API == APIOpenAICompletions && model.OpenAICompletionsCompat != nil {
		compat.requireToolResultName = model.OpenAICompletionsCompat.RequiresToolResultName == OpenAICompatSupported
		compat.assistantAfterToolResultRepair = model.OpenAICompletionsCompat.RequiresAssistantAfterToolResult == OpenAICompatSupported
		compat.convertDeveloperRole = model.OpenAICompletionsCompat.SupportsDeveloperRole == OpenAICompatUnsupported
	}
	return compat
}

type handoffMessageContext struct {
	target       Model
	compat       handoffCompatibility
	config       handoffConfig
	toolNames    map[string]string
	messageIndex int
	report       *HandoffReport
}

func transformHandoffMessage(message Message, ctx handoffMessageContext) (Message, error) {
	transformed := cloneHandoffMessage(message)
	if ctx.compat.convertDeveloperRole && transformed.Role == RoleDeveloper {
		transformed.Role = RoleUser
		ctx.report.record(HandoffChangeDeveloperRoleConverted, ctx.messageIndex, nil, "converted developer message to user message")
	}

	for index, block := range message.Content {
		if block.Type == ContentBlockImage && !ctx.target.SupportsImages() {
			if ctx.config.unsupportedImageReplacement == nil {
				return Message{}, handoffError(ctx, "target model does not support image content")
			}
			transformed.Content[index] = Text(*ctx.config.unsupportedImageReplacement)
			ctx.report.record(HandoffChangeUnsupportedImageReplaced, ctx.messageIndex, intPtr(index), "replaced image block with text")
			continue
		}
		if transformed.Role == RoleAssistant && block.Type == ContentBlockThinking && shouldConvertHandoffThinking(message, ctx) {
			transformed.Content[index] = Text(wrapHandoffThinking(block.ThinkingText, ctx.config))
			ctx.report.record(HandoffChangeThinkingConverted, ctx.messageIndex, intPtr(index), "converted thinking block to text")
			continue
		}
		transformed.Content[index] = cloneHandoffContentBlock(block)
	}

	if transformed.Role == RoleTool && ctx.compat.requireToolResultName && transformed.ToolName == "" {
		toolName, ok := ctx.toolNames[transformed.ToolCallID]
		if !ok {
			return Message{}, handoffError(ctx, "tool result requires a tool name but no matching assistant tool call was found")
		}
		transformed.ToolName = toolName
		ctx.report.record(HandoffChangeToolResultNameRepaired, ctx.messageIndex, nil, "filled tool result name from assistant tool call")
	}

	return transformed, nil
}

func shouldConvertHandoffThinking(message Message, ctx handoffMessageContext) bool {
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

func wrapHandoffThinking(text string, config handoffConfig) string {
	return config.thinkingStartDelimiter + "\n" + text + "\n" + config.thinkingEndDelimiter
}

func recordHandoffToolCalls(toolNamesByID map[string]string, message Message) {
	if message.Role != RoleAssistant {
		return
	}
	for _, block := range message.Content {
		if block.Type == ContentBlockToolCall && block.ToolCallID != "" && block.ToolName != "" {
			toolNamesByID[block.ToolCallID] = block.ToolName
		}
	}
}

func dropUnansweredHandoffToolCalls(messages []Message, report *HandoffReport) []Message {
	if len(messages) == 0 {
		return nil
	}
	cleaned := make([]Message, 0, len(messages))
	for index, message := range messages {
		if message.Role != RoleAssistant || len(message.Content) == 0 {
			cleaned = append(cleaned, message)
			continue
		}
		answered := answeredHandoffToolCallsBeforeUserTurn(messages, index)
		if answered == nil {
			cleaned = append(cleaned, message)
			continue
		}
		message.Content = dropUnansweredHandoffToolCallBlocks(message.Content, answered, index, report)
		if len(message.Content) == 0 {
			continue
		}
		cleaned = append(cleaned, message)
	}
	return cleaned
}

func answeredHandoffToolCallsBeforeUserTurn(messages []Message, assistantIndex int) map[string]struct{} {
	callIDs := handoffAssistantToolCallIDs(messages[assistantIndex])
	if len(callIDs) == 0 {
		return nil
	}
	answered := make(map[string]struct{})
	for index := assistantIndex + 1; index < len(messages); index++ {
		message := messages[index]
		switch message.Role {
		case RoleTool:
			if _, ok := callIDs[message.ToolCallID]; ok {
				answered[message.ToolCallID] = struct{}{}
			}
		case RoleUser, RoleDeveloper:
			return answered
		case RoleAssistant:
			return nil
		}
	}
	return nil
}

func handoffAssistantToolCallIDs(message Message) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, block := range message.Content {
		if isHandoffClientToolCall(block) {
			ids[block.ToolCallID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func dropUnansweredHandoffToolCallBlocks(blocks []ContentBlock, answered map[string]struct{}, messageIndex int, report *HandoffReport) []ContentBlock {
	cleaned := make([]ContentBlock, 0, len(blocks))
	for index, block := range blocks {
		if isHandoffClientToolCall(block) {
			if _, ok := answered[block.ToolCallID]; !ok {
				report.record(HandoffChangeUnansweredToolCallDropped, messageIndex, intPtr(index), "dropped unanswered assistant tool call")
				continue
			}
		}
		cleaned = append(cleaned, block)
	}
	return cleaned
}

func isHandoffClientToolCall(block ContentBlock) bool {
	if block.Type != ContentBlockToolCall {
		return false
	}
	metadataType, _ := block.ProviderMetadata["type"].(string)
	return metadataType != "server_tool_use"
}

func handoffError(ctx handoffMessageContext, message string) error {
	return &Error{
		Code:     ErrorUnsupported,
		Message:  fmt.Sprintf("handoff message %d: %s", ctx.messageIndex, message),
		Provider: ctx.target.Provider,
		Model:    ctx.target.ID,
	}
}

func (report *HandoffReport) record(kind HandoffChangeKind, messageIndex int, contentIndex *int, detail string) {
	switch kind {
	case HandoffChangeThinkingConverted:
		report.ConvertedThinkingBlocks++
	case HandoffChangeDeveloperRoleConverted:
		report.ConvertedDeveloperMessages++
	case HandoffChangeToolResultNameRepaired:
		report.RepairedToolResultNames++
	case HandoffChangeUnansweredToolCallDropped:
		report.DroppedUnansweredToolCalls++
	case HandoffChangeRepairMessageInserted:
		report.InsertedRepairMessages++
	case HandoffChangeUnsupportedImageReplaced:
		report.ReplacedUnsupportedImages++
	}
	report.Changes = append(report.Changes, HandoffChange{
		Kind:         kind,
		MessageIndex: messageIndex,
		ContentIndex: contentIndex,
		Detail:       detail,
	})
}

func cloneHandoffMessage(message Message) Message {
	message.Content = cloneHandoffContentBlocks(message.Content)
	return message
}

func cloneHandoffContentBlocks(blocks []ContentBlock) []ContentBlock {
	if len(blocks) == 0 {
		return nil
	}
	cloned := make([]ContentBlock, len(blocks))
	for index, block := range blocks {
		cloned[index] = cloneHandoffContentBlock(block)
	}
	return cloned
}

func cloneHandoffContentBlock(block ContentBlock) ContentBlock {
	block.ToolArguments = cloneHandoffAny(block.ToolArguments)
	block.ProviderMetadata = cloneHandoffStringAnyMap(block.ProviderMetadata)
	return block
}

func cloneHandoffTool(tool Tool) Tool {
	tool.InputSchema = cloneHandoffAny(tool.InputSchema)
	tool.ProviderDefinedOptions = cloneHandoffProviderDefinedOptions(tool.ProviderDefinedOptions)
	tool.ProviderMetadata = cloneHandoffStringAnyMap(tool.ProviderMetadata)
	return tool
}

func cloneHandoffTools(tools []Tool) []Tool {
	if len(tools) == 0 {
		return nil
	}
	cloned := make([]Tool, len(tools))
	for index, tool := range tools {
		cloned[index] = cloneHandoffTool(tool)
	}
	return cloned
}

func cloneHandoffStringAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneHandoffAny(value)
	}
	return cloned
}

func cloneHandoffProviderDefinedOptions(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneHandoffProviderDefinedOption(value)
	}
	return cloned
}

func cloneHandoffProviderDefinedOption(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return cloneHandoffProviderDefinedOptions(v)
	case Schema:
		cloned := make(Schema, len(v))
		for key, value := range v {
			cloned[key] = cloneHandoffProviderDefinedOption(value)
		}
		return cloned
	case []any:
		cloned := make([]any, len(v))
		for index, item := range v {
			cloned[index] = cloneHandoffProviderDefinedOption(item)
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

func cloneHandoffAny(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return cloneHandoffStringAnyMap(v)
	case Schema:
		cloned := make(Schema, len(v))
		for key, value := range v {
			cloned[key] = cloneHandoffAny(value)
		}
		return cloned
	case []any:
		cloned := make([]any, len(v))
		for index, item := range v {
			cloned[index] = cloneHandoffAny(item)
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
