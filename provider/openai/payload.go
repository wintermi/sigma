// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wintermi/sigma"
)

const (
	openAIPromptCacheKeyMaxLength = 64

	providerOptionBaseURL         = "base_url"
	providerOptionBaseURLCamel    = "baseURL"
	providerOptionEndpoint        = "endpoint"
	providerOptionIncludeUsage    = "include_usage"
	providerOptionIncludeUsageGo  = "includeUsage"
	providerOptionSessionHeader   = "session_id_header"
	providerOptionSessionHeaderGo = "sessionIDHeader"
	providerOptionExtraBody       = "extra_body"
	providerOptionExtraBodyGo     = "extraBody"
	providerOptionOrganization    = "organization"
	providerOptionProject         = "project"
)

func chatCompletionsPayload(model sigma.Model, req sigma.Request, opts sigma.Options, compat completionsCompat) (map[string]any, error) {
	messages, err := chatMessages(model, req, opts.CacheRetention, compat)
	if err != nil {
		return nil, err
	}
	if err := validateReasoningLevel(model, opts); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"model":    string(model.ID),
		"messages": messages,
		"stream":   true,
	}
	if includeUsage(opts, model.Provider) {
		if compat.supportsStreamingUsage {
			payload["stream_options"] = map[string]any{"include_usage": true}
		}
	}
	if opts.Temperature != nil {
		payload["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		payload[string(compat.maxTokensField)] = *opts.MaxTokens
	}
	if len(opts.Metadata) > 0 {
		payload["metadata"] = copyAnyMap(opts.Metadata)
	}
	addChatPromptCache(payload, opts, compat)
	addReasoning(payload, model, opts, compat)
	addChatOpenAIOptions(payload, opts, compat)
	if len(req.Tools) > 0 {
		tools, err := chatTools(model, req.Tools, compat)
		if err != nil {
			return nil, err
		}
		payload["tools"] = tools
		if compat.supportsToolStream {
			payload["tool_stream"] = true
		}
	} else if compat.requiresToolsForToolHistory && hasToolHistory(req.Messages) {
		payload["tools"] = []map[string]any{}
	}

	for key, value := range extraBody(opts, model.Provider) {
		if key == "store" && !compat.supportsStore {
			continue
		}
		payload[key] = value
	}
	if cacheControl := anthropicCacheControl(opts.CacheRetention, compat.cacheControlFormat); cacheControl != nil {
		addAnthropicCacheControl(payload, cacheControl)
	}
	addRouting(payload, opts, model.Provider, compat)
	return payload, nil
}

func validateReasoningLevel(model sigma.Model, opts sigma.Options) error {
	if opts.ReasoningLevel == "" {
		return nil
	}
	if model.SupportsThinkingLevel(opts.ReasoningLevel) {
		return nil
	}
	return &sigma.Error{
		Code:     sigma.ErrorInvalidOptions,
		Message:  fmt.Sprintf("thinking level %q is not supported by model metadata", opts.ReasoningLevel),
		Provider: model.Provider,
		Model:    model.ID,
		Err:      sigma.ErrInvalidOptions,
	}
}

func addChatPromptCache(payload map[string]any, opts sigma.Options, compat completionsCompat) {
	if compat.cacheControlFormat == sigma.OpenAICompletionsCacheControlAnthropic {
		return
	}
	addOpenAIPromptCache(payload, opts)
}

func addOpenAIPromptCache(payload map[string]any, opts sigma.Options) {
	if key := openAIPromptCacheKey(opts); key != "" {
		payload["prompt_cache_key"] = key
	}
	if opts.CacheRetention.CacheLongLived() && (opts.OpenAIOptions == nil || opts.OpenAIOptions.PromptCacheRetention == "") {
		payload["prompt_cache_retention"] = "24h"
	}
}

func openAIPromptCacheKey(opts sigma.Options) string {
	if opts.SessionID == "" || !opts.CacheRetention.CacheEnabled() {
		return ""
	}
	runes := []rune(opts.SessionID)
	if len(runes) > openAIPromptCacheKeyMaxLength {
		runes = runes[:openAIPromptCacheKeyMaxLength]
	}
	return string(runes)
}

func addChatOpenAIOptions(payload map[string]any, opts sigma.Options, compat completionsCompat) {
	if opts.OpenAIOptions == nil {
		return
	}
	if opts.OpenAIOptions.ToolChoice != nil {
		payload["tool_choice"] = opts.OpenAIOptions.ToolChoice
	}
	if opts.OpenAIOptions.ResponseFormat != nil {
		payload["response_format"] = chatResponseFormat(opts.OpenAIOptions.ResponseFormat, compat)
	}
	if opts.OpenAIOptions.TopLogprobs > 0 {
		payload["logprobs"] = true
		payload["top_logprobs"] = opts.OpenAIOptions.TopLogprobs
	}
	if opts.OpenAIOptions.ReasoningSummary != "" {
		addReasoningSummary(payload, opts.OpenAIOptions.ReasoningSummary, compat)
	}
	if opts.OpenAIOptions.ServiceTier != "" {
		payload["service_tier"] = opts.OpenAIOptions.ServiceTier
	}
	if opts.OpenAIOptions.PromptCacheRetention != "" {
		payload["prompt_cache_retention"] = opts.OpenAIOptions.PromptCacheRetention
	}
}

func chatResponseFormat(value any, compat completionsCompat) any {
	if compat.supportsJSONSchemaResponseFormat {
		return value
	}
	responseFormat, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if formatType, _ := responseFormat[providerToolOptionTypeKey].(string); formatType != "json_schema" {
		return value
	}
	return map[string]any{providerToolOptionTypeKey: "json_object"}
}

func chatMessages(model sigma.Model, req sigma.Request, retention sigma.CacheRetention, compat completionsCompat) ([]map[string]any, error) {
	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		message := map[string]any{
			"role":    "system",
			"content": providerText(req.SystemPrompt),
		}
		addCacheControl(message, retention, compat.cacheControlFormat)
		messages = append(messages, message)
	}
	toolNames := make(map[string]string)
	for index := 0; index < len(req.Messages); index++ {
		message := req.Messages[index]
		if message.Role == sigma.RoleTool {
			nextIndex, err := appendToolRunMessages(&messages, req.Messages, index, model, retention, compat, toolNames)
			if err != nil {
				return nil, err
			}
			index = nextIndex
			continue
		}
		converted, err := chatMessage(message, retention, compat, toolNames)
		if err != nil {
			return nil, err
		}
		if converted == nil {
			continue
		}
		messages = append(messages, converted)
		if message.Role == sigma.RoleAssistant {
			recordToolNames(toolNames, message.Content)
		}
	}
	return repairMessages(messages, compat), nil
}

func appendToolRunMessages(messages *[]map[string]any, input []sigma.Message, start int, model sigma.Model, retention sigma.CacheRetention, compat completionsCompat, toolNames map[string]string) (int, error) {
	var imageParts []map[string]any
	index := start
	for ; index < len(input) && input[index].Role == sigma.RoleTool; index++ {
		toolMessage := input[index]
		converted, err := chatMessage(toolMessage, retention, compat, toolNames)
		if err != nil {
			return start, err
		}
		*messages = append(*messages, converted)
		if !model.SupportsImages() {
			continue
		}
		parts, err := toolResultImageParts(toolMessage)
		if err != nil {
			return start, err
		}
		imageParts = append(imageParts, parts...)
	}
	if len(imageParts) > 0 {
		*messages = append(*messages, toolResultImageMessage(imageParts))
	}
	return index - 1, nil
}

func chatMessage(message sigma.Message, retention sigma.CacheRetention, compat completionsCompat, toolNames map[string]string) (map[string]any, error) {
	switch message.Role {
	case sigma.RoleUser, sigma.RoleDeveloper:
		content, err := inputContent(message)
		if err != nil {
			return nil, err
		}
		role := string(message.Role)
		if message.Role == sigma.RoleDeveloper && !compat.supportsDeveloperRole {
			role = "system"
		}
		converted := map[string]any{
			"role":    role,
			"content": content,
		}
		addCacheControl(converted, retention, compat.cacheControlFormat)
		return converted, nil
	case sigma.RoleAssistant:
		converted := map[string]any{"role": "assistant"}
		text, reasoningContent, toolCalls, err := assistantContent(message.Content, compat)
		if err != nil {
			return nil, err
		}
		if text != "" {
			converted["content"] = text
		}
		if compat.requiresReasoningContentOnAssistantMessages {
			converted["reasoning_content"] = reasoningContent
		}
		if len(toolCalls) > 0 {
			converted["tool_calls"] = toolCalls
		}
		if !assistantHasContent(converted) {
			return nil, nil
		}
		return converted, nil
	case sigma.RoleTool:
		content := textContent(message.Content)
		if content == "" && hasImageContent(message.Content) {
			content = "(see attached image)"
		}
		converted := map[string]any{
			"role":         "tool",
			"tool_call_id": chatToolCallID(message.ToolCallID),
			"content":      content,
		}
		if compat.requiresToolResultName {
			name := message.ToolName
			if name == "" {
				name = toolNames[message.ToolCallID]
			}
			if name != "" {
				converted["name"] = name
			}
		}
		return converted, nil
	default:
		return nil, fmt.Errorf("openai completions: unsupported message role %q", message.Role)
	}
}

func assistantHasContent(message map[string]any) bool {
	if content, ok := message["content"].(string); ok && content != "" {
		return true
	}
	if reasoning, ok := message["reasoning_content"].(string); ok && reasoning != "" {
		return true
	}
	if toolCalls, ok := message["tool_calls"].([]map[string]any); ok && len(toolCalls) > 0 {
		return true
	}
	return false
}

func hasToolHistory(messages []sigma.Message) bool {
	for _, message := range messages {
		if message.Role == sigma.RoleTool {
			return true
		}
		if message.Role != sigma.RoleAssistant {
			continue
		}
		for _, block := range message.Content {
			if block.Type == sigma.ContentBlockToolCall {
				return true
			}
		}
	}
	return false
}

func inputContent(message sigma.Message) (any, error) {
	if len(message.Content) == 0 {
		return "", nil
	}
	if len(message.Content) == 1 && message.Content[0].Type == sigma.ContentBlockText {
		return providerText(message.Content[0].Text), nil
	}

	parts := make([]map[string]any, 0, len(message.Content))
	for _, block := range message.Content {
		switch block.Type {
		case sigma.ContentBlockText:
			parts = append(parts, map[string]any{
				providerToolOptionTypeKey: "text",
				"text":                    providerText(block.Text),
			})
		case sigma.ContentBlockImage:
			if message.Role != sigma.RoleUser {
				return nil, fmt.Errorf("openai completions: image content is only supported for user messages")
			}
			url, err := imageURL(block)
			if err != nil {
				return nil, err
			}
			parts = append(parts, map[string]any{
				providerToolOptionTypeKey: "image_url",
				"image_url": map[string]any{
					"url": url,
				},
			})
		default:
			return nil, fmt.Errorf("openai completions: unsupported input content block %q", block.Type)
		}
	}
	return parts, nil
}

func imageURL(block sigma.ContentBlock) (string, error) {
	switch block.ImageSource {
	case "url":
		if block.URL == "" {
			return "", fmt.Errorf("openai completions: image URL is required")
		}
		return block.URL, nil
	case "base64":
		if block.Data == "" {
			return "", fmt.Errorf("openai completions: image data is required")
		}
		mimeType := block.MIMEType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		if _, err := base64.StdEncoding.DecodeString(block.Data); err != nil {
			return "", fmt.Errorf("openai completions: image data must be base64: %w", err)
		}
		return "data:" + mimeType + ";base64," + block.Data, nil
	default:
		return "", fmt.Errorf("openai completions: unsupported image source %q", block.ImageSource)
	}
}

func assistantContent(blocks []sigma.ContentBlock, compat completionsCompat) (string, string, []map[string]any, error) {
	var text strings.Builder
	var reasoningContent strings.Builder
	var toolCalls []map[string]any
	for _, block := range blocks {
		switch block.Type {
		case sigma.ContentBlockText:
			text.WriteString(providerText(block.Text))
		case sigma.ContentBlockThinking:
			if block.ThinkingText == "" {
				continue
			}
			if compat.requiresReasoningContentOnAssistantMessages {
				appendContent(&reasoningContent, providerText(block.ThinkingText))
			} else {
				appendContent(&text, providerText(block.ThinkingText))
			}
		case sigma.ContentBlockToolCall:
			arguments, err := toolArgumentsString(block.ToolArguments)
			if err != nil {
				return "", "", nil, err
			}
			toolCalls = append(toolCalls, map[string]any{
				"id":                      chatToolCallID(block.ToolCallID),
				providerToolOptionTypeKey: "function",
				"function": map[string]any{
					"name":      block.ToolName,
					"arguments": arguments,
				},
			})
		default:
			return "", "", nil, fmt.Errorf("openai completions: unsupported assistant content block %q", block.Type)
		}
	}
	return text.String(), reasoningContent.String(), toolCalls, nil
}

func chatToolCallID(raw string) string {
	callID, _, _ := strings.Cut(raw, "|")
	var out strings.Builder
	for _, r := range callID {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '_' || r == '-':
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}
	}
	id := strings.Trim(out.String(), "_-")
	if len(id) > 40 {
		id = id[:40]
	}
	return id
}

func appendContent(builder *strings.Builder, text string) {
	text = providerText(text)
	if builder.Len() > 0 {
		builder.WriteByte('\n')
	}
	builder.WriteString(text)
}

func textContent(blocks []sigma.ContentBlock) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == sigma.ContentBlockText {
			text.WriteString(providerText(block.Text))
		}
	}
	return text.String()
}

func hasImageContent(blocks []sigma.ContentBlock) bool {
	for _, block := range blocks {
		if block.Type == sigma.ContentBlockImage {
			return true
		}
	}
	return false
}

func toolResultImageParts(message sigma.Message) ([]map[string]any, error) {
	var parts []map[string]any
	for _, block := range message.Content {
		if block.Type != sigma.ContentBlockImage {
			continue
		}
		url, err := imageURL(block)
		if err != nil {
			return nil, err
		}
		parts = append(parts, map[string]any{
			providerToolOptionTypeKey: "image_url",
			"image_url": map[string]any{
				"url": url,
			},
		})
	}
	return parts, nil
}

func toolResultImageMessage(imageParts []map[string]any) map[string]any {
	parts := make([]map[string]any, 0, 1+len(imageParts))
	parts = append(parts, map[string]any{
		providerToolOptionTypeKey: "text",
		"text":                    "Attached image(s) from tool result:",
	})
	parts = append(parts, imageParts...)
	return map[string]any{
		"role":    "user",
		"content": parts,
	}
}

func chatTools(model sigma.Model, tools []sigma.Tool, compat completionsCompat) ([]map[string]any, error) {
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if tool.ProviderDefinedType != "" {
			return nil, unsupportedProviderToolError(model, tool)
		}
		parameters, err := jsonValue(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("openai completions: tool %q schema: %w", tool.Name, err)
		}
		if parameters == nil {
			parameters = map[string]any{providerToolOptionTypeKey: "object"}
		}
		function := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  parameters,
		}
		if compat.supportsStrictTools {
			if strict, ok := tool.ProviderMetadata["strict"].(bool); ok {
				function["strict"] = strict
			}
		}
		converted = append(converted, map[string]any{
			providerToolOptionTypeKey: "function",
			"function":                function,
		})
	}
	return converted, nil
}

func unsupportedProviderToolError(model sigma.Model, tool sigma.Tool) error {
	return &sigma.Error{
		Code:     sigma.ErrorUnsupported,
		Message:  fmt.Sprintf("provider-defined tool %q is not supported by api %s", tool.ProviderDefinedType, model.API),
		Provider: model.Provider,
		Model:    model.ID,
	}
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
		return "", fmt.Errorf("openai completions: tool arguments: %w", err)
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

func reasoningEffort(model sigma.Model, opts sigma.Options) string {
	level := opts.ReasoningLevel
	if opts.OpenAIOptions != nil && opts.OpenAIOptions.ReasoningEffort != "" {
		level = opts.OpenAIOptions.ReasoningEffort
	}
	if level == "" || level == sigma.ThinkingLevelOff {
		return ""
	}
	if value, ok := model.ProviderThinkingLevel(level); ok {
		return value
	}
	if model.SupportsReasoning() {
		return string(level)
	}
	return ""
}

func addReasoning(payload map[string]any, model sigma.Model, opts sigma.Options, compat completionsCompat) {
	if compat.reasoningFormat == sigma.OpenAICompletionsReasoningFireworks {
		addFireworksReasoning(payload, model, opts, compat)
		return
	}
	if compat.reasoningFormat == sigma.OpenAICompletionsReasoningDeepSeek {
		addDeepSeekReasoning(payload, model, opts, compat)
		return
	}
	if compat.reasoningFormat == sigma.OpenAICompletionsReasoningStringThinking {
		addStringThinkingReasoning(payload, model, opts)
		return
	}
	if compat.reasoningFormat == sigma.OpenAICompletionsReasoningTogether {
		addTogetherReasoning(payload, model, opts, compat)
		return
	}
	if compat.reasoningFormat == sigma.OpenAICompletionsReasoningQwen ||
		compat.reasoningFormat == sigma.OpenAICompletionsReasoningZAI {
		addToggleReasoning(payload, model, opts)
		return
	}
	if compat.reasoningFormat == sigma.OpenAICompletionsReasoningAntLing {
		addAntLingReasoning(payload, model, opts)
		return
	}

	effort := reasoningEffort(model, opts)
	switch compat.reasoningFormat { //nolint:exhaustive
	case sigma.OpenAICompletionsReasoningEffort:
		if effort == "" {
			return
		}
		if compat.supportsReasoningEffort {
			payload["reasoning_effort"] = effort
		}
	case sigma.OpenAICompletionsReasoningObject:
		if compat.supportsReasoningEffort {
			if effort == "" {
				if off, ok := model.ThinkingLevelMap[sigma.ThinkingLevelOff]; ok {
					effort = off
				}
			}
			if effort == "" {
				return
			}
			payload["reasoning"] = map[string]any{"effort": effort}
		}
	}
}

func addTogetherReasoning(payload map[string]any, model sigma.Model, opts sigma.Options, compat completionsCompat) {
	if !model.SupportsReasoning() {
		return
	}
	effort := reasoningEffort(model, opts)
	payload["reasoning"] = map[string]any{"enabled": effort != ""}
	if effort != "" && compat.supportsReasoningEffort {
		payload["reasoning_effort"] = effort
	}
}

func addToggleReasoning(payload map[string]any, model sigma.Model, opts sigma.Options) {
	if !model.SupportsReasoning() {
		return
	}
	payload["enable_thinking"] = reasoningEffort(model, opts) != ""
}

func addAntLingReasoning(payload map[string]any, model sigma.Model, opts sigma.Options) {
	if !model.SupportsReasoning() {
		return
	}
	if effort := reasoningEffort(model, opts); effort != "" {
		payload["reasoning"] = map[string]any{"effort": effort}
	}
}

func addDeepSeekReasoning(payload map[string]any, model sigma.Model, opts sigma.Options, compat completionsCompat) {
	if !model.SupportsReasoning() {
		return
	}
	effort := reasoningEffort(model, opts)
	if effort == "" {
		payload["thinking"] = map[string]any{providerToolOptionTypeKey: "disabled"}
		return
	}
	payload["thinking"] = map[string]any{providerToolOptionTypeKey: "enabled"}
	if compat.supportsReasoningEffort {
		payload["reasoning_effort"] = effort
	}
}

func addStringThinkingReasoning(payload map[string]any, model sigma.Model, opts sigma.Options) {
	if !model.SupportsReasoning() {
		return
	}
	level := opts.ReasoningLevel
	if opts.OpenAIOptions != nil && opts.OpenAIOptions.ReasoningEffort != "" {
		level = opts.OpenAIOptions.ReasoningEffort
	}
	if level != "" && level != sigma.ThinkingLevelOff {
		if effort, ok := model.ProviderThinkingLevel(level); ok {
			payload["thinking"] = effort
			return
		}
		payload["thinking"] = string(level)
		return
	}
	if off, ok := model.ThinkingLevelMap[sigma.ThinkingLevelOff]; ok {
		payload["thinking"] = off
	}
}

func addFireworksReasoning(payload map[string]any, model sigma.Model, opts sigma.Options, compat completionsCompat) {
	if opts.ThinkingBudgetTokens != nil {
		payload["thinking"] = map[string]any{
			providerToolOptionTypeKey: "enabled",
			"budget_tokens":           *opts.ThinkingBudgetTokens,
		}
		return
	}
	if effort := reasoningEffort(model, opts); effort != "" {
		if compat.supportsReasoningEffort {
			payload["reasoning_effort"] = effort
		}
	}
}

func addReasoningSummary(payload map[string]any, summary string, compat completionsCompat) {
	if summary == "" {
		return
	}
	if compat.reasoningFormat == sigma.OpenAICompletionsReasoningObject {
		reasoning, ok := payload["reasoning"].(map[string]any)
		if !ok {
			reasoning = make(map[string]any)
			payload["reasoning"] = reasoning
		}
		reasoning["summary"] = summary
		return
	}
	if compat.reasoningFormat == sigma.OpenAICompletionsReasoningEffort {
		payload["reasoning_summary"] = summary
	}
}

func includeUsage(opts sigma.Options, provider sigma.ProviderID) bool {
	options := providerOptions(opts, provider)
	if value, ok := boolOption(options, providerOptionIncludeUsage); ok {
		return value
	}
	if value, ok := boolOption(options, providerOptionIncludeUsageGo); ok {
		return value
	}
	return true
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

func providerOptions(opts sigma.Options, provider sigma.ProviderID) map[string]any {
	if len(opts.ProviderOptions) == 0 {
		return nil
	}
	if values := opts.ProviderOptions[provider]; len(values) > 0 {
		return values
	}
	return opts.ProviderOptions[sigma.ProviderOpenAI]
}

func boolOption(options map[string]any, key string) (bool, bool) {
	value, ok := options[key]
	if !ok {
		return false, false
	}
	boolean, ok := value.(bool)
	return boolean, ok
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

func addCacheControl(message map[string]any, retention sigma.CacheRetention, format sigma.OpenAICompletionsCacheControlFormat) {
	if !retention.CacheEnabled() {
		return
	}
	if format == sigma.OpenAICompletionsCacheControlUnsupported {
		return
	}
	if format == sigma.OpenAICompletionsCacheControlAnthropic {
		return
	}
	cacheType := "ephemeral"
	if retention.CacheLongLived() {
		cacheType = "persistent"
	}
	cacheControl := map[string]any{providerToolOptionTypeKey: cacheType}
	if format == sigma.OpenAICompletionsCacheControlContentPart {
		addContentPartCacheControl(message, cacheControl)
		return
	}
	message["cache_control"] = cacheControl
}

func anthropicCacheControl(retention sigma.CacheRetention, format sigma.OpenAICompletionsCacheControlFormat) map[string]any {
	if !retention.CacheEnabled() || format != sigma.OpenAICompletionsCacheControlAnthropic {
		return nil
	}
	cacheControl := map[string]any{providerToolOptionTypeKey: "ephemeral"}
	if retention.CacheLongLived() {
		cacheControl["ttl"] = "1h"
	}
	return cacheControl
}

func addAnthropicCacheControl(payload map[string]any, cacheControl map[string]any) {
	messages, _ := payload["messages"].([]map[string]any)
	for _, message := range messages {
		role, _ := message["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		if addContentPartCacheControl(message, cacheControl) {
			break
		}
	}
	if tools, _ := payload["tools"].([]map[string]any); len(tools) > 0 {
		tools[len(tools)-1]["cache_control"] = cacheControl
	}
	for index := len(messages) - 1; index >= 0; index-- {
		role, _ := messages[index]["role"].(string)
		if role != "user" && role != "assistant" {
			continue
		}
		if addContentPartCacheControl(messages[index], cacheControl) {
			return
		}
	}
}

func addContentPartCacheControl(message map[string]any, cacheControl map[string]any) bool {
	switch content := message["content"].(type) {
	case string:
		if content == "" {
			return false
		}
		message["content"] = []map[string]any{{
			providerToolOptionTypeKey: providerOptionText,
			providerOptionText:        content,
			"cache_control":           cacheControl,
		}}
		return true
	case []map[string]any:
		if len(content) == 0 {
			return false
		}
		for index := len(content) - 1; index >= 0; index-- {
			if content[index][providerToolOptionTypeKey] != providerOptionText {
				continue
			}
			content[index]["cache_control"] = cacheControl
			return true
		}
	}
	return false
}

func recordToolNames(names map[string]string, blocks []sigma.ContentBlock) {
	for _, block := range blocks {
		if block.Type == sigma.ContentBlockToolCall && block.ToolCallID != "" && block.ToolName != "" {
			names[block.ToolCallID] = block.ToolName
		}
	}
}

func repairMessages(messages []map[string]any, compat completionsCompat) []map[string]any {
	if !compat.requiresAssistantAfterToolResult {
		return messages
	}
	repaired := make([]map[string]any, 0, len(messages)+1)
	for i, message := range messages {
		repaired = append(repaired, message)
		if message["role"] != "tool" {
			continue
		}
		if i+1 < len(messages) && messages[i+1]["role"] == "tool" {
			continue
		}
		if i+1 < len(messages) && messages[i+1]["role"] == "assistant" {
			continue
		}
		repaired = append(repaired, map[string]any{
			"role":    "assistant",
			"content": "",
		})
	}
	return repaired
}

func addRouting(payload map[string]any, opts sigma.Options, provider sigma.ProviderID, compat completionsCompat) {
	if routing := mergedRoutingMap(routingMap(compat.openRouterRouting), requestOpenRouterRouting(opts, provider)); len(routing) > 0 {
		payload["provider"] = routing
	}
	if routing := routingMap(compat.vercelAIGatewayRouting); len(routing) > 0 {
		payload["providerOptions"] = map[string]any{"gateway": routing}
	}
}

func mergedRoutingMap(base map[string]any, override map[string]any) map[string]any {
	if len(base) == 0 {
		return override
	}
	merged := copyAnyMap(base)
	for key, value := range override {
		merged[key] = value
	}
	return merged
}

func requestOpenRouterRouting(opts sigma.Options, provider sigma.ProviderID) map[string]any {
	routing := make(map[string]any)
	mergeProviderRouting(routing, providerOptions(opts, provider))
	if provider != sigma.ProviderOpenRouter {
		mergeProviderRouting(routing, providerOptions(opts, sigma.ProviderOpenRouter))
	}
	if len(routing) == 0 {
		return nil
	}
	return routing
}

func mergeProviderRouting(out map[string]any, options map[string]any) {
	if value, ok := mapOption(options, "routing"); ok {
		for key, item := range value {
			out[key] = item
		}
	}
	if value, ok := mapOption(options, "provider"); ok {
		for key, item := range value {
			out[key] = item
		}
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
