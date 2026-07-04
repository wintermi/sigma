// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
	// HandoffChangeRepairMessageInserted indicates an assistant bridge message
	// was inserted between a tool result and following user turn.
	HandoffChangeRepairMessageInserted HandoffChangeKind = "repair-message-inserted"
	// HandoffChangeToolResultSynthesized indicates a missing tool result was
	// synthesized for an unanswered assistant tool call.
	HandoffChangeToolResultSynthesized HandoffChangeKind = "tool-result-synthesized"
	// HandoffChangeUnsupportedImageReplaced indicates an image block was
	// replaced with caller-supplied text for a non-vision target.
	HandoffChangeUnsupportedImageReplaced HandoffChangeKind = "unsupported-image-replaced"
)

// HandoffChange describes one source-neutral transformation.
type HandoffChange struct {
	Kind               HandoffChangeKind `json:"kind"`
	MessageIndex       int               `json:"messageIndex"`
	OutputMessageIndex *int              `json:"outputMessageIndex,omitempty"`
	ContentIndex       *int              `json:"contentIndex,omitempty"`
	Detail             string            `json:"detail,omitempty"`
}

// HandoffReport summarizes adaptations made for a target model.
type HandoffReport struct {
	ConvertedThinkingBlocks    int             `json:"convertedThinkingBlocks,omitempty"`
	ConvertedDeveloperMessages int             `json:"convertedDeveloperMessages,omitempty"`
	RepairedToolResultNames    int             `json:"repairedToolResultNames,omitempty"`
	DroppedUnansweredToolCalls int             `json:"droppedUnansweredToolCalls,omitempty"`
	InsertedRepairMessages     int             `json:"insertedRepairMessages,omitempty"`
	SynthesizedToolResults     int             `json:"synthesizedToolResults,omitempty"`
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
	sourceIndexes := make([]int, 0, len(req.Messages))

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
			(transformed.Role == RoleUser || transformed.Role == RoleDeveloper) {
			outputIndex := len(output.Messages)
			output.Messages = append(output.Messages, handoffRepairMessage())
			sourceIndexes = append(sourceIndexes, index)
			report.recordOutput(HandoffChangeRepairMessageInserted, index, outputIndex, nil, "inserted assistant bridge before user message")
		}

		output.Messages = append(output.Messages, transformed)
		sourceIndexes = append(sourceIndexes, index)
		recordHandoffToolCalls(toolNamesByID, transformed)
	}

	if compat.dropUnansweredToolCalls {
		output.Messages, sourceIndexes = dropUnansweredHandoffToolCalls(output.Messages, sourceIndexes, len(req.Messages), &report)
	}
	if compat.assistantAfterToolResultRepair {
		output.Messages, _ = insertHandoffRepairMessages(output.Messages, sourceIndexes, &report)
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
	normalizeToolCallID            func(string) string
}

func handoffCompatibilityForModel(model Model) handoffCompatibility {
	compat := handoffCompatibility{dropUnansweredToolCalls: true}
	if model.API == APIOpenAICompletions && model.OpenAICompletionsCompat != nil {
		compat.requireToolResultName = model.OpenAICompletionsCompat.RequiresToolResultName == OpenAICompatSupported
		compat.assistantAfterToolResultRepair = model.OpenAICompletionsCompat.RequiresAssistantAfterToolResult == OpenAICompatSupported
		compat.convertDeveloperRole = model.OpenAICompletionsCompat.SupportsDeveloperRole == OpenAICompatUnsupported
	}
	if model.API == APIAnthropicMessages || model.API == APIBedrockConverseStream {
		compat.normalizeToolCallID = newHandoffToolCallIDNormalizer(64)
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

	content := make([]ContentBlock, 0, len(message.Content))
	for index, block := range message.Content {
		if block.Type == ContentBlockImage && !ctx.target.SupportsImages() {
			if ctx.config.unsupportedImageReplacement == nil {
				return Message{}, handoffError(ctx, "target model does not support image content")
			}
			content = append(content, Text(*ctx.config.unsupportedImageReplacement))
			ctx.report.record(HandoffChangeUnsupportedImageReplaced, ctx.messageIndex, intPtr(index), "replaced image block with text")
			continue
		}
		if transformed.Role == RoleAssistant && block.Type == ContentBlockThinking && shouldConvertHandoffThinking(message, ctx) {
			if strings.TrimSpace(block.ThinkingText) == "" || block.Redacted {
				continue
			}
			content = append(content, Text(wrapHandoffThinking(block.ThinkingText, ctx.config)))
			ctx.report.record(HandoffChangeThinkingConverted, ctx.messageIndex, intPtr(index), "converted thinking block to text")
			continue
		}
		cloned := cloneHandoffContentBlock(block)
		if cloned.Type == ContentBlockToolCall && ctx.compat.normalizeToolCallID != nil {
			cloned.ToolCallID = ctx.compat.normalizeToolCallID(cloned.ToolCallID)
		}
		content = append(content, cloned)
	}
	transformed.Content = content

	if transformed.Role == RoleTool && ctx.compat.normalizeToolCallID != nil {
		transformed.ToolCallID = ctx.compat.normalizeToolCallID(transformed.ToolCallID)
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

func handoffRepairMessage() Message {
	return Message{
		Role:    RoleAssistant,
		Content: []ContentBlock{Text("I have processed the tool results.")},
	}
}

func dropUnansweredHandoffToolCalls(messages []Message, sourceIndexes []int, endSourceIndex int, report *HandoffReport) ([]Message, []int) {
	if len(messages) == 0 {
		return nil, nil
	}
	repaired := make([]Message, 0, len(messages))
	repairedSourceIndexes := make([]int, 0, len(messages))
	var pending []ContentBlock
	answered := make(map[string]struct{})
	insertMissing := func(sourceIndex int) {
		for _, call := range pending {
			if _, ok := answered[call.ToolCallID]; ok {
				continue
			}
			outputIndex := len(repaired)
			repaired = append(repaired, syntheticHandoffToolResult(call))
			repairedSourceIndexes = append(repairedSourceIndexes, sourceIndex)
			report.recordOutput(HandoffChangeToolResultSynthesized, sourceIndex, outputIndex, nil, "synthesized missing tool result")
		}
		pending = nil
		answered = make(map[string]struct{})
	}
	for index, message := range messages {
		sourceIndex := handoffSourceIndex(sourceIndexes, index, endSourceIndex)
		switch message.Role {
		case RoleAssistant:
			insertMissing(sourceIndex)
			repaired = append(repaired, message)
			repairedSourceIndexes = append(repairedSourceIndexes, sourceIndex)
			pending = handoffAssistantToolCalls(message)
		case RoleUser, RoleDeveloper:
			insertMissing(sourceIndex)
			repaired = append(repaired, message)
			repairedSourceIndexes = append(repairedSourceIndexes, sourceIndex)
		case RoleTool:
			if hasPendingHandoffToolCall(pending, message.ToolCallID) {
				answered[message.ToolCallID] = struct{}{}
			}
			repaired = append(repaired, message)
			repairedSourceIndexes = append(repairedSourceIndexes, sourceIndex)
		default:
			repaired = append(repaired, message)
			repairedSourceIndexes = append(repairedSourceIndexes, sourceIndex)
		}
	}
	insertMissing(endSourceIndex)
	return repaired, repairedSourceIndexes
}

func handoffAssistantToolCalls(message Message) []ContentBlock {
	var calls []ContentBlock
	for _, block := range message.Content {
		if isHandoffClientToolCall(block) {
			calls = append(calls, block)
		}
	}
	return calls
}

func hasPendingHandoffToolCall(calls []ContentBlock, id string) bool {
	for _, call := range calls {
		if call.ToolCallID == id {
			return true
		}
	}
	return false
}

func syntheticHandoffToolResult(call ContentBlock) Message {
	return Message{
		Role:       RoleTool,
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		IsError:    true,
		Content:    []ContentBlock{Text("No result provided")},
	}
}

func insertHandoffRepairMessages(messages []Message, sourceIndexes []int, report *HandoffReport) ([]Message, []int) {
	if len(messages) == 0 {
		return nil, nil
	}
	repaired := make([]Message, 0, len(messages)+1)
	repairedSourceIndexes := make([]int, 0, len(messages)+1)
	for index, message := range messages {
		sourceIndex := handoffSourceIndex(sourceIndexes, index, index)
		if (message.Role == RoleUser || message.Role == RoleDeveloper) &&
			len(repaired) > 0 &&
			repaired[len(repaired)-1].Role == RoleTool {
			outputIndex := len(repaired)
			repaired = append(repaired, handoffRepairMessage())
			repairedSourceIndexes = append(repairedSourceIndexes, sourceIndex)
			report.recordOutput(HandoffChangeRepairMessageInserted, sourceIndex, outputIndex, nil, "inserted assistant bridge before user message")
		}
		repaired = append(repaired, message)
		repairedSourceIndexes = append(repairedSourceIndexes, sourceIndex)
	}
	return repaired, repairedSourceIndexes
}

func handoffSourceIndex(sourceIndexes []int, outputIndex int, fallback int) int {
	if outputIndex >= 0 && outputIndex < len(sourceIndexes) {
		return sourceIndexes[outputIndex]
	}
	return fallback
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
	report.recordChange(kind, messageIndex, nil, contentIndex, detail)
}

func (report *HandoffReport) recordOutput(kind HandoffChangeKind, messageIndex int, outputIndex int, contentIndex *int, detail string) {
	report.recordChange(kind, messageIndex, intPtr(outputIndex), contentIndex, detail)
}

func (report *HandoffReport) recordChange(kind HandoffChangeKind, messageIndex int, outputIndex *int, contentIndex *int, detail string) {
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
	case HandoffChangeToolResultSynthesized:
		report.SynthesizedToolResults++
	case HandoffChangeUnsupportedImageReplaced:
		report.ReplacedUnsupportedImages++
	}
	report.Changes = append(report.Changes, HandoffChange{
		Kind:               kind,
		MessageIndex:       messageIndex,
		OutputMessageIndex: outputIndex,
		ContentIndex:       contentIndex,
		Detail:             detail,
	})
}

func newHandoffToolCallIDNormalizer(limit int) func(string) string {
	mapped := make(map[string]string)
	used := make(map[string]string)
	return func(id string) string {
		if id == "" {
			return id
		}
		if normalized := mapped[id]; normalized != "" {
			return normalized
		}
		base := safeHandoffToolCallID(id, limit)
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

func safeHandoffToolCallID(id string, limit int) string {
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
	block.ExtraFields = cloneHandoffStringAnyMap(block.ExtraFields)
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
