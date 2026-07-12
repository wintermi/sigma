// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/transform"
)

const (
	providerOptionStore          = "store"
	providerOptionPreviousID     = "previous_response_id"
	providerOptionPreviousIDGo   = "previousResponseID"
	providerOptionInclude        = "include"
	providerOptionText           = "text"
	providerOptionToolChoice     = "tool_choice"
	providerOptionToolChoiceGo   = "toolChoice"
	providerOptionTruncation     = "truncation"
	providerOptionPromptCacheKey = "prompt_cache_key"
)

func responsesPayload(model sigma.Model, req sigma.Request, opts sigma.Options) (map[string]any, error) {
	cleaned := transform.DropUnansweredToolCalls(req)
	deferredTools := transform.PlanDeferredTools(cleaned, supportsResponsesToolSearch(model), nil)
	input, err := responsesInput(model, cleaned, deferredTools.Deferred)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"model":  string(model.ID),
		"input":  input,
		"store":  false,
		"stream": true,
	}
	if cleaned.SystemPrompt != "" {
		payload["instructions"] = providerText(cleaned.SystemPrompt)
	}
	if opts.Temperature != nil {
		payload["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		payload["max_output_tokens"] = *opts.MaxTokens
	}
	if len(opts.Metadata) > 0 {
		payload["metadata"] = copyAnyMap(opts.Metadata)
	}
	addOpenAIPromptCache(payload, opts)
	if opts.OpenAIOptions != nil && opts.OpenAIOptions.ServiceTier != "" {
		payload["service_tier"] = opts.OpenAIOptions.ServiceTier
	}
	if err := addResponsesOpenAIOptions(payload, model, opts); err != nil {
		return nil, err
	}
	addResponsesReasoning(payload, model, opts)
	addResponsesReasoningInclude(payload, model, opts)
	if len(deferredTools.Immediate) > 0 {
		tools, err := responsesTools(deferredTools.Immediate, false)
		if err != nil {
			return nil, err
		}
		payload["tools"] = tools
	}
	addResponsesPreviousResponseID(payload, model.Provider, opts)
	addResponsesProviderOptions(payload, model.Provider, opts)
	return payload, nil
}

func supportsResponsesToolSearch(model sigma.Model) bool {
	if model.OpenAICodexResponses != nil {
		return model.OpenAICodexResponses.SupportsToolSearch
	}
	return model.OpenAIResponsesCompat != nil && model.OpenAIResponsesCompat.SupportsToolSearch
}

func responsesInput(model sigma.Model, req sigma.Request, deferredTools map[string]sigma.Tool) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(req.Messages)+1)
	loadedToolNames := make(map[string]struct{})
	for index, message := range req.Messages {
		converted, err := responsesMessage(model, message, index, deferredTools, loadedToolNames)
		if err != nil {
			return nil, err
		}
		items = append(items, converted...)
	}
	return items, nil
}

func responsesMessage(model sigma.Model, message sigma.Message, messageIndex int, deferredTools map[string]sigma.Tool, loadedToolNames map[string]struct{}) ([]map[string]any, error) {
	switch message.Role {
	case sigma.RoleUser, sigma.RoleDeveloper:
		content, err := responsesInputContent(model, message)
		if err != nil {
			return nil, err
		}
		return []map[string]any{{
			"role":    string(message.Role),
			"content": content,
		}}, nil
	case sigma.RoleAssistant:
		return responsesAssistantItems(model, message, messageIndex)
	case sigma.RoleTool:
		output, err := responsesToolOutput(model, message)
		if err != nil {
			return nil, err
		}
		searchItems, err := responsesToolSearchItems(message, deferredTools, loadedToolNames)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 1, 1+len(searchItems))
		items[0] = map[string]any{
			"type":    "function_call_output", //nolint:goconst
			"call_id": responsesCallID(message.ToolCallID),
			"output":  output,
		}
		return append(items, searchItems...), nil
	default:
		return nil, fmt.Errorf("openai responses: unsupported message role %q", message.Role)
	}
}

func responsesInputContent(model sigma.Model, message sigma.Message) ([]map[string]any, error) {
	if len(message.Content) == 0 {
		return []map[string]any{{"type": "input_text", providerOptionText: ""}}, nil
	}
	parts := make([]map[string]any, 0, len(message.Content))
	for _, block := range message.Content {
		switch block.Type {
		case sigma.ContentBlockText:
			parts = append(parts, map[string]any{
				"type":             "input_text",
				providerOptionText: providerText(block.Text),
			})
		case sigma.ContentBlockImage:
			if message.Role != sigma.RoleUser {
				return nil, fmt.Errorf("openai responses: image content is only supported for user messages")
			}
			url, err := imageURL(block)
			if err != nil {
				return nil, err
			}
			parts = append(parts, map[string]any{
				"detail":    "auto",
				"type":      "input_image",
				"image_url": url,
			})
		case sigma.ContentBlockDocument:
			if !model.SupportsDocuments() {
				return nil, unsupportedDocumentInputError(model, "openai responses")
			}
			file, err := responsesInputFile(block)
			if err != nil {
				return nil, err
			}
			parts = append(parts, file)
		default:
			return nil, fmt.Errorf("openai responses: unsupported input content block %q", block.Type)
		}
	}
	return parts, nil
}

func responsesInputFile(block sigma.ContentBlock) (map[string]any, error) {
	file, err := openAIFile(block)
	if err != nil {
		return nil, fmt.Errorf("openai responses: %w", err)
	}
	file["type"] = "input_file"
	return file, nil
}

func responsesAssistantItems(model sigma.Model, message sigma.Message, messageIndex int) ([]map[string]any, error) {
	var items []map[string]any
	var content []map[string]any
	var messageID string
	messageOrdinal := 0
	contentOrdinal := 0
	flushMessage := func() {
		if len(content) == 0 {
			return
		}
		item := map[string]any{
			"type":    "message",
			"role":    "assistant",
			"content": content,
		}
		item["id"] = responsesBoundedID("msg", messageID, fmt.Sprintf("msg_sigma_%d_%d", messageIndex, messageOrdinal))
		items = append(items, item)
		content = nil
		messageID = ""
		messageOrdinal++
	}

	omitToolItemID := sameProviderDifferentModel(model, message)
	for _, block := range message.Content {
		switch block.Type {
		case sigma.ContentBlockText:
			messageID = firstNonEmpty(messageID, providerID(block.ProviderMetadata))
			part := map[string]any{
				"type":             "output_text",
				providerOptionText: providerText(block.Text),
			}
			part["id"] = responsesBoundedID(
				providerOptionText,
				providerContentID(block.ProviderMetadata),
				fmt.Sprintf("text_sigma_%d_%d", messageIndex, contentOrdinal),
			)
			if block.Signature != "" {
				part["signature"] = block.Signature
			}
			content = append(content, part)
			contentOrdinal++
		case sigma.ContentBlockThinking:
			flushMessage()
			item := map[string]any{
				"type": "reasoning",
				"summary": []map[string]any{{
					"type":             "summary_text",
					providerOptionText: providerText(block.ThinkingText),
				}},
			}
			item["id"] = responsesBoundedID("rs", providerID(block.ProviderMetadata), fmt.Sprintf("rs_sigma_%d", messageIndex))
			if block.Signature != "" {
				item["signature"] = block.Signature
			}
			if block.ProviderSignature != "" {
				item["encrypted_content"] = block.ProviderSignature
			}
			items = append(items, item)
		case sigma.ContentBlockToolCall:
			flushMessage()
			arguments, err := toolArgumentsString(block.ToolArguments)
			if err != nil {
				return nil, err
			}
			callID, itemID := responsesToolCallIDs(block, fmt.Sprintf("fc_sigma_%d", messageIndex))
			item := map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      block.ToolName,
				"arguments": arguments,
			}
			if !omitToolItemID {
				item["id"] = itemID
			}
			if block.ProviderSignature != "" {
				item["encrypted_content"] = block.ProviderSignature
			}
			items = append(items, item)
		default:
			return nil, fmt.Errorf("openai responses: unsupported assistant content block %q", block.Type)
		}
	}
	flushMessage()
	return items, nil
}

func sameProviderDifferentModel(model sigma.Model, message sigma.Message) bool {
	return message.Provider == model.Provider &&
		message.API == model.API &&
		message.Model != "" &&
		message.Model != model.ID
}

func responsesTools(tools []sigma.Tool, deferLoading bool) ([]map[string]any, error) {
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if tool.ProviderDefinedType != "" {
			convertedTool := map[string]any{
				"type": tool.ProviderDefinedType,
			}
			for key, value := range tool.ProviderDefinedOptions {
				convertedTool[key] = value
			}
			converted = append(converted, convertedTool)
			continue
		}
		parameters, err := jsonValue(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("openai responses: tool %q schema: %w", tool.Name, err)
		}
		if parameters == nil {
			parameters = map[string]any{"type": "object"}
		}
		convertedTool := map[string]any{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  parameters,
		}
		if strict, ok := tool.ProviderMetadata["strict"].(bool); ok {
			convertedTool["strict"] = strict
		}
		if deferLoading {
			convertedTool["defer_loading"] = true
		}
		converted = append(converted, convertedTool)
	}
	return converted, nil
}

func responsesToolSearchItems(message sigma.Message, deferredTools map[string]sigma.Tool, loadedToolNames map[string]struct{}) ([]map[string]any, error) {
	if len(deferredTools) == 0 {
		return nil, nil
	}
	tools := make([]sigma.Tool, 0, len(message.AddedToolNames))
	names := make([]string, 0, len(message.AddedToolNames))
	for _, name := range message.AddedToolNames {
		tool, ok := deferredTools[name]
		if !ok {
			continue
		}
		if _, loaded := loadedToolNames[name]; loaded {
			continue
		}
		loadedToolNames[name] = struct{}{}
		tools = append(tools, tool)
		names = append(names, tool.Name)
	}
	if len(tools) == 0 {
		return nil, nil
	}
	converted, err := responsesTools(tools, true)
	if err != nil {
		return nil, err
	}
	callID := responsesToolSearchCallID(message.ToolCallID, names)
	return []map[string]any{
		{
			"type":      "tool_search_call",
			"call_id":   callID,
			"execution": "client",
			"status":    "completed",
			"arguments": map[string]any{"query": strings.Join(names, " "), "limit": len(names)},
		},
		{
			"type":      "tool_search_output",
			"call_id":   callID,
			"execution": "client",
			"status":    "completed",
			"tools":     converted,
		},
	}, nil
}

func responsesToolSearchCallID(toolCallID string, names []string) string {
	sum := sha256.Sum256([]byte(toolCallID + ":" + strings.Join(names, ",")))
	return "fc_sigma_tool_load_" + hex.EncodeToString(sum[:])[:16]
}

func addResponsesReasoning(payload map[string]any, model sigma.Model, opts sigma.Options) {
	reasoning := make(map[string]any)
	if effort := reasoningEffort(model, opts); effort != "" {
		reasoning["effort"] = effort
	}
	if opts.ThinkingBudgetTokens != nil {
		reasoning["budget_tokens"] = *opts.ThinkingBudgetTokens
	}
	if opts.OpenAIOptions != nil && opts.OpenAIOptions.ReasoningSummary != "" {
		reasoning["summary"] = opts.OpenAIOptions.ReasoningSummary
	} else if len(reasoning) > 0 {
		reasoning["summary"] = "auto"
	}
	if len(reasoning) > 0 {
		payload["reasoning"] = reasoning
	}
}

func addResponsesReasoningInclude(payload map[string]any, model sigma.Model, opts sigma.Options) {
	if !responsesReasoningEnabled(model, opts) || responsesIncludeConfigured(model.Provider, opts) {
		return
	}
	payload["include"] = []string{"reasoning.encrypted_content"}
}

func responsesReasoningEnabled(model sigma.Model, opts sigma.Options) bool {
	if reasoningEffort(model, opts) != "" {
		return true
	}
	return opts.ThinkingBudgetTokens != nil
}

func responsesIncludeConfigured(provider sigma.ProviderID, opts sigma.Options) bool {
	options := providerOptions(opts, provider)
	_, ok := options[providerOptionInclude]
	return ok
}

func addResponsesOpenAIOptions(payload map[string]any, model sigma.Model, opts sigma.Options) error {
	if opts.OpenAIOptions == nil {
		return nil
	}
	if opts.OpenAIOptions.TopLogprobs > 0 {
		return &sigma.Error{
			Code:     sigma.ErrorInvalidOptions,
			Message:  "openai logprobs are only supported by openai-completions",
			Provider: model.Provider,
			Model:    model.ID,
		}
	}
	if opts.OpenAIOptions.ToolChoice != nil {
		setResponsesToolChoice(payload, opts.OpenAIOptions.ToolChoice)
	}
	if opts.OpenAIOptions.ResponseFormat != nil {
		text, _ := payload[providerOptionText].(map[string]any)
		if text == nil {
			text = make(map[string]any)
			payload[providerOptionText] = text
		}
		text["format"] = responsesTextFormat(opts.OpenAIOptions.ResponseFormat)
	}
	if opts.OpenAIOptions.PromptCacheRetention != "" {
		payload["prompt_cache_retention"] = opts.OpenAIOptions.PromptCacheRetention
	}
	if opts.OpenAIOptions.ParallelToolCalls != nil {
		payload["parallel_tool_calls"] = *opts.OpenAIOptions.ParallelToolCalls
	}
	if opts.OpenAIOptions.TextVerbosity != "" {
		text, _ := payload[providerOptionText].(map[string]any)
		if text == nil {
			text = make(map[string]any)
			payload[providerOptionText] = text
		}
		text["verbosity"] = opts.OpenAIOptions.TextVerbosity
	}
	return nil
}

func responsesTextFormat(value any) any {
	responseFormat := anyMap(value)
	if responseFormat == nil {
		return value
	}
	if formatType, _ := responseFormat["type"].(string); formatType != "json_schema" {
		return copyAnyMap(responseFormat)
	}
	jsonSchema := anyMap(responseFormat["json_schema"])
	if jsonSchema == nil {
		return copyAnyMap(responseFormat)
	}
	textFormat := map[string]any{"type": "json_schema"}
	for key, value := range jsonSchema {
		textFormat[key] = value
	}
	return textFormat
}

func anyMap(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case sigma.Schema:
		return map[string]any(v)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func addResponsesPreviousResponseID(payload map[string]any, provider sigma.ProviderID, opts sigma.Options) {
	options := providerOptions(opts, provider)
	if previous, ok := stringOption(options, providerOptionPreviousID); ok {
		payload["previous_response_id"] = previous
		return
	}
	if previous, ok := stringOption(options, providerOptionPreviousIDGo); ok {
		payload["previous_response_id"] = previous
		return
	}
}

func addResponsesProviderOptions(payload map[string]any, provider sigma.ProviderID, opts sigma.Options) {
	options := providerOptions(opts, provider)
	if value, ok := boolOption(options, providerOptionStore); ok {
		payload["store"] = value
	}
	if value, ok := options[providerOptionInclude]; ok {
		payload["include"] = value
	}
	if value, ok := options[providerOptionText]; ok {
		setResponsesText(payload, value)
	}
	if value, ok := options[providerOptionToolChoice]; ok {
		setResponsesToolChoice(payload, value)
	} else if value, ok := options[providerOptionToolChoiceGo]; ok {
		setResponsesToolChoice(payload, value)
	}
	if value, ok := stringOption(options, providerOptionTruncation); ok {
		payload["truncation"] = value
	}
	if value, ok := stringOption(options, providerOptionPromptCacheKey); ok {
		payload["prompt_cache_key"] = value
	}
	for key, value := range extraBody(opts, provider) {
		payload[key] = value
	}
}

func setResponsesText(payload map[string]any, value any) {
	text, ok := value.(map[string]any)
	if !ok {
		payload[providerOptionText] = value
		return
	}
	current, _ := payload[providerOptionText].(map[string]any)
	if current == nil {
		payload[providerOptionText] = text
		return
	}
	for key, nested := range text {
		current[key] = nested
	}
}

func setResponsesToolChoice(payload map[string]any, value any) {
	choice, ok := value.(map[string]any)
	if !ok {
		payload["tool_choice"] = value
		return
	}
	if choiceType, _ := choice["type"].(string); choiceType != "function" {
		payload["tool_choice"] = value
		return
	}
	if _, ok := choice["name"]; ok {
		payload["tool_choice"] = value
		return
	}
	function, _ := choice["function"].(map[string]any)
	name, _ := function["name"].(string)
	if name == "" {
		payload["tool_choice"] = value
		return
	}
	normalized := make(map[string]any, len(choice))
	for key, nested := range choice {
		if key == "function" {
			continue
		}
		normalized[key] = nested
	}
	normalized["name"] = name
	payload["tool_choice"] = normalized
}

func responsesToolOutput(model sigma.Model, message sigma.Message) (any, error) {
	var parts []map[string]any
	var text strings.Builder
	var hasImage bool
	for _, block := range message.Content {
		switch block.Type {
		case sigma.ContentBlockText:
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(providerText(block.Text))
		case sigma.ContentBlockImage:
			hasImage = true
			if !model.SupportsImages() {
				continue
			}
			url, err := imageURL(block)
			if err != nil {
				return nil, err
			}
			parts = append(parts, map[string]any{
				"detail":    "auto",
				"type":      "input_image",
				"image_url": url,
			})
		case sigma.ContentBlockDocument:
			if !model.SupportsDocuments() {
				return nil, unsupportedDocumentInputError(model, "openai responses")
			}
			file, err := responsesInputFile(block)
			if err != nil {
				return nil, err
			}
			parts = append(parts, file)
		default:
			return nil, fmt.Errorf("openai responses: unsupported tool result content block %q", block.Type)
		}
	}
	if len(parts) == 0 {
		if text.Len() > 0 {
			return text.String(), nil
		}
		if hasImage {
			return "(see attached image)", nil
		}
		return "", nil
	}
	if text.Len() > 0 {
		parts = append([]map[string]any{{
			"type":             "input_text",
			providerOptionText: text.String(),
		}}, parts...)
	}
	return parts, nil
}

func responsesToolCallIDs(block sigma.ContentBlock, fallbackItemID string) (string, string) {
	callID := firstNonEmpty(block.ToolCallID, providerMetadataString(block.ProviderMetadata, "call_id"))
	itemID := providerID(block.ProviderMetadata)
	if before, after, ok := strings.Cut(callID, "|"); ok {
		callID = before
		if itemID == "" {
			itemID = after
		}
	}
	callID = responsesBoundedID("call", callID, fallbackItemID+"_call")
	itemID = responsesBoundedID("fc", itemID, fallbackItemID)
	if !strings.HasPrefix(itemID, "fc_") {
		itemID = responsesBoundedID("fc", "fc_"+itemID, fallbackItemID)
	}
	return callID, itemID
}

func responsesCallID(raw string) string {
	callID, _, _ := strings.Cut(raw, "|")
	return responsesBoundedID("call", callID, "call_sigma")
}

func providerID(metadata map[string]any) string {
	if id := providerMetadataString(metadata, "id"); id != "" {
		return id
	}
	return providerMetadataString(metadata, "item_id")
}

func providerContentID(metadata map[string]any) string {
	if id, ok := stringOption(metadata, "content_id"); ok {
		return id
	}
	return ""
}

func providerMetadataString(metadata map[string]any, key string) string {
	value, _ := stringOption(metadata, key)
	return value
}

func responsesBoundedID(prefix string, raw string, fallback string) string {
	id := sanitizeResponsesID(firstNonEmpty(raw, fallback))
	if id == "" {
		id = sanitizeResponsesID(fallback)
	}
	if id == "" {
		id = prefix + "_sigma"
	}
	if len(id) <= 64 {
		return id
	}
	hash := sha256.Sum256([]byte(id))
	suffix := hex.EncodeToString(hash[:])[:16]
	trimmed := strings.TrimRight(id, "_-")
	maxPrefix := 64 - len(suffix) - 1
	if len(trimmed) > maxPrefix {
		trimmed = trimmed[:maxPrefix]
	}
	if trimmed == "" {
		trimmed = prefix
	}
	return trimmed + "_" + suffix
}

func sanitizeResponsesID(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_-")
}
