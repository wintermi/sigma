// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"sort"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/providertext"
	"github.com/wintermi/sigma/internal/transform"
)

const (
	providerOptionRegion                       = "region"
	providerOptionModelID                      = "model_id"
	providerOptionModelIDGo                    = "modelID"
	providerOptionInferenceProfileARN          = "inference_profile_arn"
	providerOptionInferenceProfileARNGo        = "inferenceProfileARN"
	providerOptionEndpoint                     = "endpoint"
	providerOptionCredentialSource             = "credential_source"
	providerOptionCredentialSourceGo           = "credentialSource"
	providerOptionAdditionalModelRequestFields = "additional_model_request_fields"
	providerOptionAdditionalModelRequestGo     = "additionalModelRequestFields"
	providerOptionAdditionalResponsePaths      = "additional_model_response_field_paths"
	providerOptionAdditionalResponsePathsGo    = "additionalModelResponseFieldPaths"
	providerOptionRequestMetadata              = "request_metadata"
	providerOptionRequestMetadataGo            = "requestMetadata"
	providerOptionStopSequences                = "stop_sequences"
	providerOptionStopSequencesGo              = "stopSequences"
	providerOptionTopP                         = "top_p"
	providerOptionTopPGo                       = "topP"
	providerOptionToolChoice                   = "tool_choice"
	providerOptionToolChoiceGo                 = "toolChoice"
	providerOptionThinkingDisplay              = "thinking_display"
	providerOptionThinkingDisplayGo            = "thinkingDisplay"
	providerOptionInterleavedThinking          = "interleaved_thinking"
	providerOptionInterleavedThinkingGo        = "interleavedThinking"
	bedrockResponseFormatToolName              = "__sigma_json_response"
)

const (
	converseBlockText       = "text"
	converseBlockImage      = "image"
	converseBlockToolUse    = "tool_use"
	converseBlockToolResult = "tool_result"
	converseBlockReasoning  = "reasoning"
	converseBlockCachePoint = "cache_point"
)

// ConverseStreamClient is the narrow fakeable Bedrock runtime seam.
type ConverseStreamClient interface {
	ConverseStream(context.Context, ConverseRequest) (ConverseStream, error)
}

// ConverseRequest is the provider-owned request shape sent through the client seam.
type ConverseRequest struct {
	ModelID                           string                   `json:"modelId"`
	System                            []ConverseContentBlock   `json:"system,omitempty"`
	Messages                          []ConverseMessage        `json:"messages,omitempty"`
	InferenceConfig                   *ConverseInferenceConfig `json:"inferenceConfig,omitempty"`
	Tools                             []ConverseTool           `json:"tools,omitempty"`
	ToolChoice                        *sigma.BedrockToolChoice `json:"toolChoice,omitempty"`
	AdditionalModelRequestFields      map[string]any           `json:"additionalModelRequestFields,omitempty"`
	AdditionalModelResponseFieldPaths []string                 `json:"additionalModelResponseFieldPaths,omitempty"`
	RequestMetadata                   map[string]string        `json:"requestMetadata,omitempty"`
}

// ConverseMessage is a Bedrock Converse message without AWS SDK types.
type ConverseMessage struct {
	Role    string                 `json:"role"`
	Content []ConverseContentBlock `json:"content"`
}

// ConverseContentBlock is a provider-owned Converse content block.
type ConverseContentBlock struct {
	Type       string                   `json:"type"`
	Text       string                   `json:"text,omitempty"`
	Image      *ConverseImageBlock      `json:"image,omitempty"`
	ToolUse    *ConverseToolUseBlock    `json:"toolUse,omitempty"`
	ToolResult *ConverseToolResultBlock `json:"toolResult,omitempty"`
	Reasoning  *ConverseReasoningBlock  `json:"reasoning,omitempty"`
	CachePoint *ConverseCachePointBlock `json:"cachePoint,omitempty"`
}

// ConverseImageBlock carries inline image bytes as base64 text.
type ConverseImageBlock struct {
	Format string `json:"format"`
	Data   string `json:"data"`
}

// ConverseToolUseBlock carries an assistant tool request.
type ConverseToolUseBlock struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input any    `json:"input,omitempty"`
}

// ConverseToolResultBlock carries a user tool result.
type ConverseToolResultBlock struct {
	ToolUseID string                      `json:"toolUseId"`
	Content   []ConverseToolResultContent `json:"content,omitempty"`
	Status    string                      `json:"status,omitempty"`
}

// ConverseToolResultContent carries one tool-result content item.
type ConverseToolResultContent struct {
	Type  string              `json:"type"`
	Text  string              `json:"text,omitempty"`
	Image *ConverseImageBlock `json:"image,omitempty"`
}

// ConverseReasoningBlock carries replayed reasoning content.
type ConverseReasoningBlock struct {
	Text              string `json:"text,omitempty"`
	Signature         string `json:"signature,omitempty"`
	ProviderSignature string `json:"providerSignature,omitempty"`
	Redacted          bool   `json:"redacted,omitempty"`
}

// ConverseCachePointBlock carries Bedrock prompt cache marker options.
type ConverseCachePointBlock struct {
	TTL string `json:"ttl,omitempty"`
}

// ConverseInferenceConfig carries portable Converse inference parameters.
type ConverseInferenceConfig struct {
	MaxTokens     *int     `json:"maxTokens,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"topP,omitempty"`
	StopSequences []string `json:"stopSequences,omitempty"`
}

// ConverseTool carries a function tool spec.
type ConverseTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

func conversePayload(model sigma.Model, req sigma.Request, opts sigma.Options, config Config) (ConverseRequest, error) {
	if err := validateCapabilities(model, req, opts); err != nil {
		return ConverseRequest{}, err
	}

	transformed, err := transform.Transform(transform.Input{
		TargetModel: model,
		Request:     req,
		Compatibility: transform.Compatibility{
			ConvertDeveloperRole:    true,
			DropUnansweredToolCalls: true,
		},
	})
	if err != nil {
		return ConverseRequest{}, err
	}

	messages, err := converseMessages(transformed)
	if err != nil {
		return ConverseRequest{}, err
	}
	payload := ConverseRequest{
		ModelID:                           bedrockModelID(config, model),
		Messages:                          messages,
		InferenceConfig:                   inferenceConfig(opts, model.Provider),
		AdditionalModelRequestFields:      additionalModelRequestFields(opts, model.Provider),
		RequestMetadata:                   requestMetadata(opts, model.Provider),
		AdditionalModelResponseFieldPaths: responseFieldPaths(opts, model.Provider),
	}
	if payload.ModelID == "" {
		return ConverseRequest{}, unsupportedError(model, "bedrock converse stream: model id is required")
	}
	if transformed.SystemPrompt != "" {
		payload.System = append(payload.System, ConverseContentBlock{Type: converseBlockText, Text: providertext.Clean(transformed.SystemPrompt)})
	}
	if cachePointsEnabled(model, opts.CacheRetention) {
		cachePoint := cachePointBlock(opts.CacheRetention)
		if len(payload.System) > 0 {
			payload.System = append(payload.System, cachePoint)
		}
		if len(payload.Messages) > 0 {
			last := len(payload.Messages) - 1
			if payload.Messages[last].Role == "user" {
				payload.Messages[last].Content = append(payload.Messages[last].Content, cachePoint)
			}
		}
	}
	if thinkingFields := bedrockThinkingFields(model, opts, config); len(thinkingFields) > 0 {
		if payload.AdditionalModelRequestFields == nil {
			payload.AdditionalModelRequestFields = make(map[string]any)
		}
		for key, value := range thinkingFields {
			payload.AdditionalModelRequestFields[key] = value
		}
	}
	tools := transformed.Tools
	toolChoice := bedrockToolChoice(opts, model.Provider)
	tools, toolChoice, err = addBedrockResponseFormatTool(model, tools, toolChoice, opts)
	if err != nil {
		return ConverseRequest{}, err
	}
	if len(tools) > 0 && (toolChoice == nil || toolChoice.Type != sigma.BedrockToolChoiceNone) {
		tools, err := converseTools(model, tools)
		if err != nil {
			return ConverseRequest{}, err
		}
		payload.Tools = tools
		payload.ToolChoice = toolChoice
	} else if toolChoice == nil || toolChoice.Type != sigma.BedrockToolChoiceNone {
		payload.Tools = replayToolSpecs(transformed.Messages)
	}
	return payload, nil
}

func validateCapabilities(model sigma.Model, req sigma.Request, opts sigma.Options) error {
	toolChoice := bedrockToolChoice(opts, model.Provider)
	responseFormatRequested := opts.BedrockOptions != nil && opts.BedrockOptions.ResponseFormat != nil
	if (len(req.Tools) > 0 || responseFormatRequested) && !model.SupportsTools && (toolChoice == nil || toolChoice.Type != sigma.BedrockToolChoiceNone) {
		return unsupportedError(model, "target model does not support tools")
	}
	if opts.ThinkingBudgetTokens != nil && !model.SupportsThinking {
		return unsupportedError(model, "target model does not support thinking options")
	}
	if opts.ReasoningLevel != "" && opts.ReasoningLevel != sigma.ThinkingLevelOff && !model.SupportsThinking {
		return unsupportedError(model, "target model does not support thinking options")
	}
	for messageIndex, message := range req.Messages {
		for _, block := range message.Content {
			if block.Type == sigma.ContentBlockImage && !supportsInput(model, sigma.ContentBlockImage) {
				return unsupportedError(model, fmt.Sprintf("message %d: target model does not declare image input support", messageIndex))
			}
		}
	}
	return nil
}

func addBedrockResponseFormatTool(model sigma.Model, tools []sigma.Tool, toolChoice *sigma.BedrockToolChoice, opts sigma.Options) ([]sigma.Tool, *sigma.BedrockToolChoice, error) {
	if opts.BedrockOptions == nil || opts.BedrockOptions.ResponseFormat == nil {
		return tools, toolChoice, nil
	}
	for _, tool := range tools {
		if tool.Name == bedrockResponseFormatToolName {
			return nil, nil, &sigma.Error{
				Code:     sigma.ErrorInvalidOptions,
				Message:  "bedrock response format synthetic tool name is already in use",
				Provider: model.Provider,
				Model:    model.ID,
				Err:      sigma.ErrInvalidOptions,
			}
		}
	}
	if toolChoice != nil && (toolChoice.Type != sigma.BedrockToolChoiceTool || toolChoice.Name != bedrockResponseFormatToolName) {
		return nil, nil, &sigma.Error{
			Code:     sigma.ErrorInvalidOptions,
			Message:  "bedrock response format cannot be combined with another tool choice",
			Provider: model.Provider,
			Model:    model.ID,
			Err:      sigma.ErrInvalidOptions,
		}
	}
	schema, err := bedrockResponseFormatSchema(opts.BedrockOptions.ResponseFormat)
	if err != nil {
		return nil, nil, &sigma.Error{
			Code:     sigma.ErrorInvalidOptions,
			Message:  fmt.Sprintf("bedrock response format schema is invalid: %v", err),
			Provider: model.Provider,
			Model:    model.ID,
			Err:      sigma.ErrInvalidOptions,
		}
	}
	synthetic := sigma.Tool{
		Name:        bedrockResponseFormatToolName,
		Description: "Return structured JSON response",
		InputSchema: schema,
	}
	out := make([]sigma.Tool, 0, len(tools)+1)
	out = append(out, synthetic)
	out = append(out, tools...)
	return out, &sigma.BedrockToolChoice{Type: sigma.BedrockToolChoiceTool, Name: bedrockResponseFormatToolName}, nil
}

func bedrockResponseFormatSchema(value any) (any, error) {
	converted, err := jsonValue(value)
	if err != nil {
		return nil, err
	}
	format, ok := converted.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object")
	}
	if nested, ok := format["json_schema"].(map[string]any); ok {
		if schema, ok := nested["schema"]; ok {
			return schema, nil
		}
		return map[string]any{"type": "object"}, nil
	}
	if kind, _ := format["type"].(string); kind == "json_schema" {
		if schema, ok := format["schema"]; ok {
			return schema, nil
		}
		return map[string]any{"type": "object"}, nil
	}
	return format, nil
}

func unsupportedError(model sigma.Model, message string) error {
	return &sigma.Error{
		Code:     sigma.ErrorUnsupported,
		Message:  message,
		Provider: model.Provider,
		Model:    model.ID,
	}
}

func converseMessages(req sigma.Request) ([]ConverseMessage, error) {
	messages := make([]ConverseMessage, 0, len(req.Messages))
	for index := 0; index < len(req.Messages); index++ {
		message := req.Messages[index]
		if message.Role == sigma.RoleTool {
			converted, next, err := converseToolResults(req.Messages, index)
			if err != nil {
				return nil, err
			}
			messages = append(messages, converted)
			index = next - 1
			continue
		}
		converted, err := converseMessage(message)
		if err != nil {
			return nil, err
		}
		// Bedrock rejects messages with empty content arrays, so drop replayed
		// assistant turns whose blocks were all blank (for example aborted
		// requests).
		if message.Role == sigma.RoleAssistant && len(converted.Content) == 0 {
			continue
		}
		messages = append(messages, converted)
	}
	return messages, nil
}

func converseMessage(message sigma.Message) (ConverseMessage, error) {
	switch message.Role {
	case sigma.RoleUser, sigma.RoleDeveloper:
		content, err := converseInputContent(message.Content)
		return ConverseMessage{Role: "user", Content: content}, err
	case sigma.RoleAssistant:
		content, err := converseAssistantContent(message.Content)
		return ConverseMessage{Role: "assistant", Content: content}, err
	case sigma.RoleTool:
		converted, _, err := converseToolResults([]sigma.Message{message}, 0)
		return converted, err
	default:
		return ConverseMessage{}, fmt.Errorf("bedrock converse stream: unsupported message role %q", message.Role)
	}
}

func converseToolResults(messages []sigma.Message, start int) (ConverseMessage, int, error) {
	content := []ConverseContentBlock{}
	index := start
	for index < len(messages) && messages[index].Role == sigma.RoleTool {
		block, err := converseToolResult(messages[index])
		if err != nil {
			return ConverseMessage{}, index, err
		}
		content = append(content, block)
		index++
	}
	return ConverseMessage{Role: "user", Content: content}, index, nil
}

func converseToolResult(message sigma.Message) (ConverseContentBlock, error) {
	content, err := converseToolResultContent(message.Content)
	if err != nil {
		return ConverseContentBlock{}, err
	}
	return ConverseContentBlock{
		Type: converseBlockToolResult,
		ToolResult: &ConverseToolResultBlock{
			ToolUseID: message.ToolCallID,
			Content:   content,
			Status:    toolResultStatus(message.IsError),
		},
	}, nil
}

func converseToolResultContent(blocks []sigma.ContentBlock) ([]ConverseToolResultContent, error) {
	content := make([]ConverseToolResultContent, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case sigma.ContentBlockText:
			// Bedrock rejects blank text blocks, so skip them here and fall
			// back to the placeholder below when nothing else remains.
			text := providertext.Clean(block.Text)
			if strings.TrimSpace(text) == "" {
				continue
			}
			content = append(content, ConverseToolResultContent{Type: converseBlockText, Text: text})
		case sigma.ContentBlockImage:
			image, err := converseImage(block)
			if err != nil {
				return nil, err
			}
			content = append(content, ConverseToolResultContent{Type: converseBlockImage, Image: image})
		default:
			return nil, fmt.Errorf("bedrock converse stream: unsupported tool result content block %q", block.Type)
		}
	}
	if len(content) == 0 {
		return []ConverseToolResultContent{{Type: converseBlockText, Text: emptyTextPlaceholder}}, nil
	}
	return content, nil
}

// emptyTextPlaceholder replaces required Bedrock text content whose replayed
// blocks were all blank, because Converse rejects blank text blocks.
const emptyTextPlaceholder = "<empty>"

func converseInputContent(blocks []sigma.ContentBlock) ([]ConverseContentBlock, error) {
	content := make([]ConverseContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case sigma.ContentBlockText:
			// Bedrock rejects blank text blocks, so skip them here and fall
			// back to the placeholder below when nothing else remains.
			text := providertext.Clean(block.Text)
			if strings.TrimSpace(text) == "" {
				continue
			}
			content = append(content, ConverseContentBlock{Type: converseBlockText, Text: text})
		case sigma.ContentBlockImage:
			image, err := converseImage(block)
			if err != nil {
				return nil, err
			}
			content = append(content, ConverseContentBlock{Type: converseBlockImage, Image: image})
		default:
			return nil, fmt.Errorf("bedrock converse stream: unsupported user content block %q", block.Type)
		}
	}
	if len(content) == 0 {
		return []ConverseContentBlock{{Type: converseBlockText, Text: emptyTextPlaceholder}}, nil
	}
	return content, nil
}

func converseAssistantContent(blocks []sigma.ContentBlock) ([]ConverseContentBlock, error) {
	content := make([]ConverseContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case sigma.ContentBlockText:
			// Bedrock rejects blank text blocks, so skip blank replay text.
			text := providertext.Clean(block.Text)
			if strings.TrimSpace(text) == "" {
				continue
			}
			content = append(content, ConverseContentBlock{Type: converseBlockText, Text: text})
		case sigma.ContentBlockThinking:
			thinkingText := providertext.Clean(block.ThinkingText)
			if strings.TrimSpace(thinkingText) == "" && !block.Redacted {
				continue
			}
			reasoning := &ConverseReasoningBlock{
				Text:              thinkingText,
				Signature:         block.Signature,
				ProviderSignature: firstNonEmpty(block.ProviderSignature, block.Signature),
				Redacted:          block.Redacted,
			}
			content = append(content, ConverseContentBlock{Type: converseBlockReasoning, Reasoning: reasoning})
		case sigma.ContentBlockToolCall:
			input, err := jsonValue(block.ToolArguments)
			if err != nil {
				return nil, fmt.Errorf("bedrock converse stream: tool %q input: %w", block.ToolName, err)
			}
			if input == nil {
				input = map[string]any{}
			}
			content = append(content, ConverseContentBlock{
				Type: converseBlockToolUse,
				ToolUse: &ConverseToolUseBlock{
					ID:    block.ToolCallID,
					Name:  block.ToolName,
					Input: input,
				},
			})
		default:
			return nil, fmt.Errorf("bedrock converse stream: unsupported assistant content block %q", block.Type)
		}
	}
	return content, nil
}

func converseImage(block sigma.ContentBlock) (*ConverseImageBlock, error) {
	if block.ImageSource != "base64" {
		return nil, fmt.Errorf("bedrock converse stream: unsupported image source %q", block.ImageSource)
	}
	if block.Data == "" {
		return nil, fmt.Errorf("bedrock converse stream: image data is required")
	}
	if _, err := base64.StdEncoding.DecodeString(block.Data); err != nil {
		return nil, fmt.Errorf("bedrock converse stream: image data must be base64: %w", err)
	}
	format := imageFormat(block.MIMEType)
	if format == "" {
		return nil, fmt.Errorf("bedrock converse stream: unsupported image MIME type %q", block.MIMEType)
	}
	return &ConverseImageBlock{Format: format, Data: block.Data}, nil
}

func converseTools(model sigma.Model, tools []sigma.Tool) ([]ConverseTool, error) {
	converted := make([]ConverseTool, 0, len(tools))
	for _, tool := range tools {
		if tool.ProviderDefinedType != "" {
			return nil, unsupportedError(model, fmt.Sprintf("provider-defined tool %q is not supported by bedrock converse stream", tool.ProviderDefinedType))
		}
		schema, err := jsonValue(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("bedrock converse stream: tool %q schema: %w", tool.Name, err)
		}
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		converted = append(converted, ConverseTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		})
	}
	return converted, nil
}

func replayToolSpecs(messages []sigma.Message) []ConverseTool {
	names := make(map[string]struct{})
	for _, message := range messages {
		switch message.Role {
		case sigma.RoleAssistant:
			for _, block := range message.Content {
				if block.Type == sigma.ContentBlockToolCall && block.ToolName != "" {
					names[block.ToolName] = struct{}{}
				}
			}
		case sigma.RoleTool:
			if message.ToolName != "" {
				names[message.ToolName] = struct{}{}
			}
		case sigma.RoleUser, sigma.RoleDeveloper:
			continue
		}
	}
	if len(names) == 0 {
		return nil
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	tools := make([]ConverseTool, 0, len(sorted))
	for _, name := range sorted {
		tools = append(tools, ConverseTool{
			Name:        name,
			Description: name,
			InputSchema: map[string]any{"type": "object"},
		})
	}
	return tools
}

func bedrockModelID(config Config, model sigma.Model) string {
	if config.InferenceProfileARN != "" {
		return config.InferenceProfileARN
	}
	if config.ModelID != "" {
		return config.ModelID
	}
	return string(model.ID)
}

func inferenceConfig(opts sigma.Options, provider sigma.ProviderID) *ConverseInferenceConfig {
	config := &ConverseInferenceConfig{}
	if opts.MaxTokens != nil {
		config.MaxTokens = opts.MaxTokens
	}
	if opts.Temperature != nil {
		config.Temperature = opts.Temperature
	}
	if opts.BedrockOptions != nil && opts.BedrockOptions.TopP != nil {
		config.TopP = opts.BedrockOptions.TopP
	}
	options := providerOptions(opts, provider)
	if values, ok := stringSliceOption(options, providerOptionStopSequences); ok {
		config.StopSequences = values
	} else if values, ok := stringSliceOption(options, providerOptionStopSequencesGo); ok {
		config.StopSequences = values
	}
	if topP, ok := float64Option(options, providerOptionTopP); ok && config.TopP == nil {
		config.TopP = &topP
	} else if topP, ok := float64Option(options, providerOptionTopPGo); ok && config.TopP == nil {
		config.TopP = &topP
	}
	if opts.BedrockOptions != nil && len(opts.BedrockOptions.StopSequences) > 0 {
		config.StopSequences = append([]string(nil), opts.BedrockOptions.StopSequences...)
	}
	if config.MaxTokens == nil && config.Temperature == nil && config.TopP == nil && len(config.StopSequences) == 0 {
		return nil
	}
	return config
}

func additionalModelRequestFields(opts sigma.Options, provider sigma.ProviderID) map[string]any {
	options := providerOptions(opts, provider)
	fields := mapOption(options, providerOptionAdditionalModelRequestFields)
	if len(fields) == 0 {
		fields = mapOption(options, providerOptionAdditionalModelRequestGo)
	}
	copied := copyAnyMap(fields)
	if opts.BedrockOptions != nil && len(opts.BedrockOptions.AdditionalModelRequestFields) > 0 {
		if copied == nil {
			copied = make(map[string]any, len(opts.BedrockOptions.AdditionalModelRequestFields))
		}
		for key, value := range opts.BedrockOptions.AdditionalModelRequestFields {
			copied[key] = value
		}
	}
	if len(copied) == 0 {
		return nil
	}
	return copied
}

func responseFieldPaths(opts sigma.Options, provider sigma.ProviderID) []string {
	if opts.BedrockOptions != nil && len(opts.BedrockOptions.AdditionalModelResponseFieldPaths) > 0 {
		return append([]string(nil), opts.BedrockOptions.AdditionalModelResponseFieldPaths...)
	}
	options := providerOptions(opts, provider)
	if values := stringSlice(options[providerOptionAdditionalResponsePaths]); len(values) > 0 {
		return values
	}
	return stringSlice(options[providerOptionAdditionalResponsePathsGo])
}

func requestMetadata(opts sigma.Options, provider sigma.ProviderID) map[string]string {
	converted := make(map[string]string)
	for key, value := range opts.Metadata {
		if text, ok := value.(string); ok {
			converted[key] = text
		}
	}
	options := providerOptions(opts, provider)
	for key, value := range stringMapOption(options, providerOptionRequestMetadata) {
		converted[key] = value
	}
	for key, value := range stringMapOption(options, providerOptionRequestMetadataGo) {
		converted[key] = value
	}
	if opts.BedrockOptions != nil {
		for key, value := range opts.BedrockOptions.RequestMetadata {
			converted[key] = value
		}
	}
	if len(converted) == 0 {
		return nil
	}
	return converted
}

func bedrockToolChoice(opts sigma.Options, provider sigma.ProviderID) *sigma.BedrockToolChoice {
	if opts.BedrockOptions != nil && opts.BedrockOptions.ToolChoice != nil {
		toolChoice := *opts.BedrockOptions.ToolChoice
		return &toolChoice
	}
	options := providerOptions(opts, provider)
	if choice, ok := stringOption(options, providerOptionToolChoice); ok {
		return stringToolChoice(choice)
	}
	if choice, ok := stringOption(options, providerOptionToolChoiceGo); ok {
		return stringToolChoice(choice)
	}
	if choice := mapToolChoice(mapOption(options, providerOptionToolChoice)); choice != nil {
		return choice
	}
	return mapToolChoice(mapOption(options, providerOptionToolChoiceGo))
}

func stringToolChoice(choice string) *sigma.BedrockToolChoice {
	return &sigma.BedrockToolChoice{Type: sigma.BedrockToolChoiceType(choice)}
}

func mapToolChoice(values map[string]any) *sigma.BedrockToolChoice {
	if len(values) == 0 {
		return nil
	}
	choice := sigma.BedrockToolChoice{}
	if text, ok := values["type"].(string); ok {
		choice.Type = sigma.BedrockToolChoiceType(text)
	}
	if text, ok := values["name"].(string); ok {
		choice.Name = text
	}
	if choice.Type == "" && choice.Name == "" {
		return nil
	}
	return &choice
}

func bedrockThinkingFields(model sigma.Model, opts sigma.Options, config Config) map[string]any {
	if opts.ThinkingBudgetTokens == nil && (opts.ReasoningLevel == "" || opts.ReasoningLevel == sigma.ThinkingLevelOff) {
		return nil
	}
	if !isClaudeBedrockModel(model) {
		if opts.ThinkingBudgetTokens == nil {
			return nil
		}
		return map[string]any{
			"thinking": map[string]any{
				"type":          "enabled",
				"budget_tokens": *opts.ThinkingBudgetTokens,
			},
		}
	}

	display := bedrockThinkingDisplay(model, opts, config)
	if supportsAdaptiveThinking(model) && opts.ThinkingBudgetTokens == nil {
		thinking := map[string]any{"type": "adaptive"}
		if display != "" {
			thinking["display"] = display
		}
		return map[string]any{
			"thinking":      thinking,
			"output_config": map[string]any{"effort": bedrockThinkingEffort(model, opts.ReasoningLevel)},
		}
	}

	budget := bedrockThinkingBudget(opts)
	thinking := map[string]any{
		"type":          "enabled",
		"budget_tokens": budget,
	}
	if display != "" {
		thinking["display"] = display
	}
	fields := map[string]any{"thinking": thinking}
	if bedrockInterleavedThinking(opts, model.Provider) {
		fields["anthropic_beta"] = []string{"interleaved-thinking-2025-05-14"}
	}
	return fields
}

func bedrockThinkingDisplay(model sigma.Model, opts sigma.Options, config Config) string {
	if isGovCloudBedrockTarget(model, config) {
		return ""
	}
	if opts.BedrockOptions != nil && opts.BedrockOptions.ThinkingDisplay != "" {
		return string(opts.BedrockOptions.ThinkingDisplay)
	}
	options := providerOptions(opts, model.Provider)
	if value, ok := stringOption(options, providerOptionThinkingDisplay); ok {
		return value
	}
	if value, ok := stringOption(options, providerOptionThinkingDisplayGo); ok {
		return value
	}
	return string(sigma.BedrockThinkingDisplaySummarized)
}

func bedrockThinkingEffort(model sigma.Model, level sigma.ThinkingLevel) string {
	if level == sigma.ThinkingLevelXHigh && supportsNativeXHighEffort(model) {
		return "xhigh"
	}
	if level != "" {
		if value, ok := model.ProviderThinkingLevel(level); ok {
			return value
		}
	}
	switch level {
	case sigma.ThinkingLevelMinimal, sigma.ThinkingLevelLow:
		return "low"
	case sigma.ThinkingLevelMedium:
		return "medium"
	case sigma.ThinkingLevelHigh, sigma.ThinkingLevelXHigh:
		return "high"
	default:
		return "high"
	}
}

func bedrockThinkingBudget(opts sigma.Options) int {
	if opts.ThinkingBudgetTokens != nil {
		return *opts.ThinkingBudgetTokens
	}
	switch opts.ReasoningLevel {
	case sigma.ThinkingLevelMinimal:
		return 1024
	case sigma.ThinkingLevelLow:
		return 2048
	case sigma.ThinkingLevelMedium:
		return 8192
	case sigma.ThinkingLevelHigh, sigma.ThinkingLevelXHigh:
		return 16384
	default:
		return 1024
	}
}

func bedrockInterleavedThinking(opts sigma.Options, provider sigma.ProviderID) bool {
	if opts.BedrockOptions != nil && opts.BedrockOptions.InterleavedThinking != nil {
		return *opts.BedrockOptions.InterleavedThinking
	}
	options := providerOptions(opts, provider)
	if value, ok := boolOption(options, providerOptionInterleavedThinking); ok {
		return value
	}
	if value, ok := boolOption(options, providerOptionInterleavedThinkingGo); ok {
		return value
	}
	return true
}

func cachePointsEnabled(model sigma.Model, retention sigma.CacheRetention) bool {
	if retention == "" || retention == sigma.CacheRetentionNone {
		return false
	}
	return supportsPromptCaching(model)
}

func cachePointBlock(retention sigma.CacheRetention) ConverseContentBlock {
	block := ConverseContentBlock{
		Type:       converseBlockCachePoint,
		CachePoint: &ConverseCachePointBlock{},
	}
	if retention == sigma.CacheRetentionLong || retention == sigma.CacheRetentionPersistent {
		block.CachePoint.TTL = "ONE_HOUR"
	}
	return block
}

func isClaudeBedrockModel(model sigma.Model) bool {
	for _, candidate := range modelMatchCandidates(model) {
		if strings.Contains(candidate, "anthropic.claude") ||
			strings.Contains(candidate, "anthropic/claude") ||
			strings.Contains(candidate, "claude") {
			return true
		}
	}
	return false
}

func supportsPromptCaching(model sigma.Model) bool {
	candidates := modelMatchCandidates(model)
	hasClaude := false
	for _, candidate := range candidates {
		if strings.Contains(candidate, "claude") {
			hasClaude = true
			break
		}
	}
	if !hasClaude {
		return false
	}
	for _, candidate := range candidates {
		if strings.Contains(candidate, "-4-") ||
			strings.Contains(candidate, "claude-3-7-sonnet") ||
			strings.Contains(candidate, "claude-3-5-haiku") {
			return true
		}
	}
	return false
}

func supportsAdaptiveThinking(model sigma.Model) bool {
	for _, candidate := range modelMatchCandidates(model) {
		if strings.Contains(candidate, "opus-4-6") ||
			strings.Contains(candidate, "opus-4-7") ||
			strings.Contains(candidate, "opus-4-8") ||
			strings.Contains(candidate, "sonnet-4-6") {
			return true
		}
	}
	return false
}

func supportsNativeXHighEffort(model sigma.Model) bool {
	for _, candidate := range modelMatchCandidates(model) {
		if strings.Contains(candidate, "opus-4-7") || strings.Contains(candidate, "opus-4-8") {
			return true
		}
	}
	return false
}

func isGovCloudBedrockTarget(model sigma.Model, config Config) bool {
	if strings.HasPrefix(strings.ToLower(config.Region), "us-gov-") {
		return true
	}
	id := strings.ToLower(string(model.ID))
	return strings.HasPrefix(id, "us-gov.") || strings.HasPrefix(id, "arn:aws-us-gov:")
}

func modelMatchCandidates(model sigma.Model) []string {
	values := []string{string(model.ID)}
	if model.Name != "" {
		values = append(values, model.Name)
	}
	candidates := make([]string, 0, len(values)*2)
	for _, value := range values {
		lower := strings.ToLower(value)
		candidates = append(candidates, lower, normalizeModelCandidate(lower))
	}
	return candidates
}

func normalizeModelCandidate(value string) string {
	replacer := strings.NewReplacer(" ", "-", "_", "-", ".", "-", ":", "-")
	return replacer.Replace(value)
}

func stringMapOption(options map[string]any, key string) map[string]string {
	if len(options) == 0 {
		return nil
	}
	switch values := options[key].(type) {
	case map[string]string:
		return copyStringMap(values)
	case map[string]any:
		converted := make(map[string]string, len(values))
		for key, value := range values {
			if text, ok := value.(string); ok {
				converted[key] = text
			}
		}
		return converted
	default:
		return nil
	}
}

func providerOptions(opts sigma.Options, provider sigma.ProviderID) map[string]any {
	if len(opts.ProviderOptions) == 0 {
		return nil
	}
	if values := opts.ProviderOptions[provider]; len(values) > 0 {
		return values
	}
	return opts.ProviderOptions[sigma.ProviderAmazonBedrock]
}

func toolResultStatus(isError bool) string {
	if isError {
		return "error"
	}
	return "success"
}

func imageFormat(mimeType string) string {
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		mediaType = mimeType
	}
	switch strings.ToLower(mediaType) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpeg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return ""
	}
}

func supportsInput(model sigma.Model, blockType sigma.ContentBlockType) bool {
	if len(model.SupportedInputs) == 0 {
		return true
	}
	for _, supported := range model.SupportedInputs {
		if supported == blockType {
			return true
		}
	}
	return false
}

func jsonValue(value any) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		var out any
		if err := json.Unmarshal(v, &out); err != nil {
			return nil, err
		}
		return out, nil
	case []byte:
		var out any
		if err := json.Unmarshal(v, &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var out any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

func mapOption(options map[string]any, key string) map[string]any {
	if len(options) == 0 {
		return nil
	}
	value, _ := options[key].(map[string]any)
	return value
}

func stringOption(options map[string]any, key string) (string, bool) {
	if len(options) == 0 {
		return "", false
	}
	value, ok := options[key].(string)
	return value, ok && value != ""
}

func boolOption(options map[string]any, key string) (bool, bool) {
	if len(options) == 0 {
		return false, false
	}
	value, ok := options[key].(bool)
	return value, ok
}

func float64Option(options map[string]any, key string) (float64, bool) {
	if len(options) == 0 {
		return 0, false
	}
	switch value := options[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	default:
		return 0, false
	}
}

func stringSliceOption(options map[string]any, key string) ([]string, bool) {
	if len(options) == 0 {
		return nil, false
	}
	values := stringSlice(options[key])
	return values, len(values) > 0
}

func stringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func copyAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
