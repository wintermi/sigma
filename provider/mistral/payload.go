// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/providertext"
	"github.com/wintermi/sigma/internal/transform"
)

const (
	providerOptionBaseURL             = "base_url"
	providerOptionBaseURLCamel        = "baseURL"
	providerOptionEndpoint            = "endpoint"
	providerOptionExtraBody           = "extra_body"
	providerOptionExtraBodyGo         = "extraBody"
	providerOptionCompletionArgs      = "completion_args"
	providerOptionCompletionArgsGo    = "completionArgs"
	providerOptionStore               = "store"
	providerOptionHandoffExecution    = "handoff_execution"
	providerOptionHandoffExecutionGo  = "handoffExecution"
	providerOptionToolChoice          = "tool_choice"
	providerOptionToolChoiceGo        = "toolChoice"
	providerOptionReasoningMode       = "mistral_reasoning_mode"
	providerOptionReasoningModeGo     = "mistralReasoningMode"
	providerOptionReasoningModeEffort = "reasoning_effort"
	providerOptionReasoningModePrompt = "prompt_mode"
	payloadKeyType                    = "type"
	payloadValueText                  = "text"
)

func conversationPayload(model sigma.Model, req sigma.Request, opts sigma.Options) (map[string]any, error) {
	if err := validateCapabilities(model, req, opts); err != nil {
		return nil, err
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
		return nil, err
	}

	inputs, err := conversationInputs(model, transformed)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"model":  string(model.ID),
		"inputs": inputs,
		"stream": true,
	}
	if transformed.SystemPrompt != "" {
		payload["instructions"] = providertext.Clean(transformed.SystemPrompt)
	}
	if len(opts.Metadata) > 0 {
		payload["metadata"] = copyAnyMap(opts.Metadata)
	}
	if usePromptCaching(opts) {
		payload["prompt_cache_key"] = opts.SessionID
	}
	if promptMode := mistralPromptMode(model, opts); promptMode != "" {
		payload["prompt_mode"] = promptMode
	}
	completionArgs := completionArgs(model, opts)
	if len(completionArgs) > 0 {
		payload["completion_args"] = completionArgs
	}
	if len(transformed.Tools) > 0 {
		tools, err := conversationTools(model, transformed.Tools)
		if err != nil {
			return nil, err
		}
		payload["tools"] = tools
	}
	addProviderOptions(payload, model.Provider, opts)
	return payload, nil
}

func usePromptCaching(opts sigma.Options) bool {
	return opts.SessionID != "" && opts.CacheRetention != sigma.CacheRetentionNone
}

func validateCapabilities(model sigma.Model, req sigma.Request, opts sigma.Options) error {
	if len(req.Tools) > 0 && !model.SupportsTools {
		return unsupportedError(model, "target model does not support tools")
	}
	if opts.ReasoningLevel != "" && opts.ReasoningLevel != sigma.ThinkingLevelOff {
		if !model.SupportsThinking {
			return unsupportedError(model, "target model does not support thinking options")
		}
		if !model.SupportsThinkingLevel(opts.ReasoningLevel) {
			return unsupportedError(model, fmt.Sprintf("target model does not support thinking level %q", opts.ReasoningLevel))
		}
		if _, ok := mistralReasoningMode(model); !ok {
			return unsupportedError(model, "mistral conversations does not support thinking options for this model")
		}
	}
	if opts.ThinkingBudgetTokens != nil {
		return unsupportedError(model, "mistral conversations does not support thinking options")
	}
	for messageIndex, message := range req.Messages {
		for _, block := range message.Content {
			switch block.Type { //nolint:exhaustive
			case sigma.ContentBlockImage:
				if !model.SupportsImages() {
					return unsupportedError(model, fmt.Sprintf("message %d: target model does not declare image input support", messageIndex))
				}
			case sigma.ContentBlockThinking:
				if !model.SupportsThinking {
					return unsupportedError(model, fmt.Sprintf("message %d: target model does not support thinking content", messageIndex))
				}
			}
		}
	}
	return nil
}

func unsupportedError(model sigma.Model, message string) error {
	return &sigma.Error{
		Code:     sigma.ErrorUnsupported,
		Message:  message,
		Provider: model.Provider,
		Model:    model.ID,
	}
}

func conversationInputs(model sigma.Model, req sigma.Request) ([]map[string]any, error) {
	inputs := make([]map[string]any, 0, len(req.Messages))
	ids := newToolCallIDNormalizer()
	for _, message := range req.Messages {
		converted, err := conversationInput(model, message, ids)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, converted...)
	}
	return inputs, nil
}

func conversationInput(model sigma.Model, message sigma.Message, ids *toolCallIDNormalizer) ([]map[string]any, error) {
	switch message.Role {
	case sigma.RoleUser, sigma.RoleDeveloper:
		content, err := conversationMessageContent(model, message.Content)
		if err != nil {
			return nil, err
		}
		return []map[string]any{{
			"object":       "entry",
			payloadKeyType: "message.input",
			"role":         "user",
			"content":      content,
		}}, nil
	case sigma.RoleAssistant:
		return assistantEntries(message.Content, ids)
	case sigma.RoleTool:
		result, err := conversationToolResult(model, message.Content)
		if err != nil {
			return nil, err
		}
		entry := map[string]any{
			"object":       "entry",
			payloadKeyType: "function.result",
			"tool_call_id": ids.normalize(message.ToolCallID),
			"result":       result,
		}
		return []map[string]any{entry}, nil
	default:
		return nil, fmt.Errorf("mistral conversations: unsupported message role %q", message.Role)
	}
}

func assistantEntries(blocks []sigma.ContentBlock, ids *toolCallIDNormalizer) ([]map[string]any, error) {
	var entries []map[string]any
	var output assistantOutput
	flushOutput := func() {
		if output.empty() {
			return
		}
		entries = append(entries, map[string]any{
			"object":       "entry",
			payloadKeyType: "message.output",
			"role":         "assistant",
			"content":      output.content(),
		})
		output.reset()
	}

	for _, block := range blocks {
		switch block.Type {
		case sigma.ContentBlockText:
			output.addText(block.Text)
		case sigma.ContentBlockThinking:
			output.addThinking(block.ThinkingText)
		case sigma.ContentBlockToolCall:
			flushOutput()
			arguments, err := toolArgumentsString(block.ToolArguments)
			if err != nil {
				return nil, err
			}
			entries = append(entries, map[string]any{
				"object":       "entry",
				payloadKeyType: "function.call",
				"tool_call_id": ids.normalize(block.ToolCallID),
				"name":         block.ToolName,
				"arguments":    arguments,
			})
		default:
			return nil, fmt.Errorf("mistral conversations: unsupported assistant content block %q", block.Type)
		}
	}
	flushOutput()
	if len(entries) == 0 {
		entries = append(entries, map[string]any{
			"object":       "entry",
			payloadKeyType: "message.output",
			"role":         "assistant",
			"content":      "",
		})
	}
	return entries, nil
}

type assistantOutput struct {
	text   strings.Builder
	chunks []map[string]any
	typed  bool
}

func (o *assistantOutput) addText(text string) {
	if text == "" {
		return
	}
	text = providertext.Clean(text)
	if o.typed {
		o.chunks = append(o.chunks, map[string]any{
			payloadKeyType:   payloadValueText,
			payloadValueText: text,
		})
		return
	}
	o.text.WriteString(text)
}

func (o *assistantOutput) addThinking(text string) {
	if !o.typed {
		if existing := o.text.String(); existing != "" {
			o.chunks = append(o.chunks, map[string]any{
				payloadKeyType:   payloadValueText,
				payloadValueText: existing,
			})
			o.text.Reset()
		}
		o.typed = true
	}
	o.chunks = append(o.chunks, map[string]any{
		payloadKeyType: "thinking",
		"thinking": []map[string]any{{
			payloadKeyType:   payloadValueText,
			payloadValueText: providertext.Clean(text),
		}},
	})
}

func (o *assistantOutput) empty() bool {
	return !o.typed && o.text.Len() == 0
}

func (o *assistantOutput) content() any {
	if o.typed {
		return o.chunks
	}
	return o.text.String()
}

func (o *assistantOutput) reset() {
	o.text.Reset()
	o.chunks = nil
	o.typed = false
}

func conversationTools(model sigma.Model, tools []sigma.Tool) ([]map[string]any, error) {
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if tool.ProviderDefinedType != "" {
			return nil, unsupportedError(model, fmt.Sprintf("provider-defined tool %q is not supported by mistral conversations", tool.ProviderDefinedType))
		}
		parameters, err := jsonValue(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("mistral conversations: tool %q schema: %w", tool.Name, err)
		}
		if parameters == nil {
			parameters = map[string]any{"type": "object"}
		}
		converted = append(converted, map[string]any{
			payloadKeyType: "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  parameters,
			},
		})
	}
	return converted, nil
}

func completionArgs(model sigma.Model, opts sigma.Options) map[string]any {
	args := copyAnyMap(completionArgsOption(opts, model.Provider))
	if args == nil {
		args = make(map[string]any)
	}
	if opts.Temperature != nil {
		args["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		args["max_tokens"] = *opts.MaxTokens
	}
	addMistralReasoningArgs(args, model, opts)
	options := providerOptions(opts, model.Provider)
	if value, ok := mistralToolChoice(opts); ok {
		args["tool_choice"] = value
	} else if value, ok := options[providerOptionToolChoice]; ok {
		args["tool_choice"] = value
	} else if value, ok := options[providerOptionToolChoiceGo]; ok {
		args["tool_choice"] = value
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

func addMistralReasoningArgs(args map[string]any, model sigma.Model, opts sigma.Options) {
	if opts.ReasoningLevel == "" || opts.ReasoningLevel == sigma.ThinkingLevelOff {
		return
	}
	mode, ok := mistralReasoningMode(model)
	if !ok {
		return
	}
	if mode != providerOptionReasoningModeEffort {
		return
	}
	value, ok := model.ProviderThinkingLevel(opts.ReasoningLevel)
	if !ok || value == "" {
		value = "high"
	}
	args["reasoning_effort"] = value
}

func mistralPromptMode(model sigma.Model, opts sigma.Options) string {
	if opts.ReasoningLevel == "" || opts.ReasoningLevel == sigma.ThinkingLevelOff {
		return ""
	}
	mode, ok := mistralReasoningMode(model)
	if !ok || mode != providerOptionReasoningModePrompt {
		return ""
	}
	return "reasoning"
}

func mistralToolChoice(opts sigma.Options) (any, bool) {
	if opts.MistralOptions == nil || opts.MistralOptions.ToolChoice == nil {
		return nil, false
	}
	choice := opts.MistralOptions.ToolChoice
	return string(choice.Type), true
}

func mistralReasoningMode(model sigma.Model) (string, bool) {
	if value, ok := stringMetadata(model.ProviderMetadata, providerOptionReasoningMode); ok {
		return value, true
	}
	if value, ok := stringMetadata(model.ProviderMetadata, providerOptionReasoningModeGo); ok {
		return value, true
	}
	modelID := string(model.ID)
	switch {
	case strings.HasPrefix(modelID, "magistral-"):
		return providerOptionReasoningModePrompt, true
	case modelID == "mistral-small-2603",
		modelID == "mistral-small-latest",
		modelID == "mistral-medium-3.5",
		modelID == "mistral-medium-2604":
		return providerOptionReasoningModeEffort, true
	default:
		return "", false
	}
}

func addProviderOptions(payload map[string]any, provider sigma.ProviderID, opts sigma.Options) {
	options := providerOptions(opts, provider)
	if value, ok := options[providerOptionStore]; ok {
		payload["store"] = value
	}
	if value, ok := options[providerOptionHandoffExecution]; ok {
		payload["handoff_execution"] = value
	} else if value, ok := options[providerOptionHandoffExecutionGo]; ok {
		payload["handoff_execution"] = value
	}
	for key, value := range extraBody(opts, provider) {
		payload[key] = value
	}
}

func conversationMessageContent(model sigma.Model, blocks []sigma.ContentBlock) (any, error) {
	chunks, hasImage, err := conversationContentChunks(model, blocks)
	if err != nil {
		return nil, err
	}
	if !hasImage {
		return textContent(blocks), nil
	}
	return chunks, nil
}

func conversationToolResult(model sigma.Model, blocks []sigma.ContentBlock) (any, error) {
	return conversationToolResultText(model, blocks)
}

func conversationToolResultText(model sigma.Model, blocks []sigma.ContentBlock) (string, error) {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case sigma.ContentBlockText:
			if text := providertext.Clean(block.Text); text != "" {
				parts = append(parts, text)
			}
		case sigma.ContentBlockImage:
			if !model.SupportsImages() {
				return "", unsupportedError(model, "target model does not declare image input support")
			}
			imageURL, err := conversationImageURL(block)
			if err != nil {
				return "", err
			}
			parts = append(parts, "Image: "+imageURL)
		default:
			return "", fmt.Errorf("mistral conversations: unsupported tool result content block %q", block.Type)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func conversationContentChunks(model sigma.Model, blocks []sigma.ContentBlock) ([]map[string]any, bool, error) {
	chunks := make([]map[string]any, 0, len(blocks))
	hasImage := false
	for _, block := range blocks {
		switch block.Type {
		case sigma.ContentBlockText:
			if block.Text == "" {
				continue
			}
			chunks = append(chunks, map[string]any{
				payloadKeyType:   payloadValueText,
				payloadValueText: providertext.Clean(block.Text),
			})
		case sigma.ContentBlockImage:
			hasImage = true
			image, err := conversationImage(model, block)
			if err != nil {
				return nil, false, err
			}
			chunks = append(chunks, image)
		default:
			return nil, false, fmt.Errorf("mistral conversations: unsupported input content block %q", block.Type)
		}
	}
	return chunks, hasImage, nil
}

func conversationImage(model sigma.Model, block sigma.ContentBlock) (map[string]any, error) {
	if !model.SupportsImages() {
		return nil, unsupportedError(model, "target model does not declare image input support")
	}
	imageURL, err := conversationImageURL(block)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		payloadKeyType: "image_url",
		"image_url":    imageURL,
	}, nil
}

func conversationImageURL(block sigma.ContentBlock) (string, error) {
	switch block.ImageSource {
	case "base64":
		if block.Data == "" {
			return "", fmt.Errorf("mistral conversations: image data is required")
		}
		if _, err := base64.StdEncoding.DecodeString(block.Data); err != nil {
			return "", fmt.Errorf("mistral conversations: image data must be base64: %w", err)
		}
		mimeType := block.MIMEType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		return "data:" + mimeType + ";base64," + block.Data, nil
	case "url":
		if block.URL == "" {
			return "", fmt.Errorf("mistral conversations: image URL is required")
		}
		return block.URL, nil
	default:
		return "", fmt.Errorf("mistral conversations: unsupported image source %q", block.ImageSource)
	}
}

func textContent(blocks []sigma.ContentBlock) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == sigma.ContentBlockText {
			text.WriteString(providertext.Clean(block.Text))
		}
	}
	return text.String()
}

func toolArgumentsString(arguments any) (string, error) {
	if arguments == nil {
		return "{}", nil
	}
	if text, ok := arguments.(string); ok {
		return text, nil
	}
	data, err := json.Marshal(arguments)
	if err != nil {
		return "", fmt.Errorf("mistral conversations: tool arguments: %w", err)
	}
	return string(data), nil
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

func providerOptions(opts sigma.Options, provider sigma.ProviderID) map[string]any {
	if len(opts.ProviderOptions) == 0 {
		return nil
	}
	if values := opts.ProviderOptions[provider]; len(values) > 0 {
		return values
	}
	return opts.ProviderOptions[sigma.ProviderMistral]
}

func extraBody(opts sigma.Options, provider sigma.ProviderID) map[string]any {
	options := providerOptions(opts, provider)
	if value, ok := mapOption(options, providerOptionExtraBody); ok {
		return value
	}
	if value, ok := mapOption(options, providerOptionExtraBodyGo); ok {
		return value
	}
	return nil
}

func completionArgsOption(opts sigma.Options, provider sigma.ProviderID) map[string]any {
	options := providerOptions(opts, provider)
	if value, ok := mapOption(options, providerOptionCompletionArgs); ok {
		return value
	}
	if value, ok := mapOption(options, providerOptionCompletionArgsGo); ok {
		return value
	}
	return nil
}

func stringOption(options map[string]any, key string) (string, bool) {
	value, ok := options[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok && text != ""
}

func mapOption(options map[string]any, key string) (map[string]any, bool) {
	value, ok := options[key]
	if !ok {
		return nil, false
	}
	values, ok := value.(map[string]any)
	return values, ok
}

func stringMetadata(values map[string]any, key string) (string, bool) {
	value, ok := values[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok && text != ""
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

const mistralToolCallIDLength = 9

type toolCallIDNormalizer struct {
	ids     map[string]string
	reverse map[string]string
}

func newToolCallIDNormalizer() *toolCallIDNormalizer {
	return &toolCallIDNormalizer{
		ids:     make(map[string]string),
		reverse: make(map[string]string),
	}
}

func (n *toolCallIDNormalizer) normalize(id string) string {
	if id == "" {
		return ""
	}
	if existing := n.ids[id]; existing != "" {
		return existing
	}
	for attempt := 0; ; attempt++ {
		candidate := deriveMistralToolCallID(id, attempt)
		if owner := n.reverse[candidate]; owner == "" || owner == id {
			n.ids[id] = candidate
			n.reverse[candidate] = id
			return candidate
		}
	}
}

func deriveMistralToolCallID(id string, attempt int) string {
	normalized := alphanumeric(id)
	if attempt == 0 && len(normalized) == mistralToolCallIDLength {
		return normalized
	}
	seed := normalized
	if seed == "" {
		seed = id
	}
	if attempt > 0 {
		seed = fmt.Sprintf("%s:%d", seed, attempt)
	}
	hash := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(hash[:])[:mistralToolCallIDLength]
}

func alphanumeric(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		}
	}
	return out.String()
}
