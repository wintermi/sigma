// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/providertext"
	"github.com/wintermi/sigma/internal/transform"
)

const (
	providerOptionBaseURL                 = "base_url"
	providerOptionBaseURLCamel            = "baseURL"
	providerOptionEndpoint                = "endpoint"
	providerOptionExtraBody               = "extra_body"
	providerOptionExtraBodyGo             = "extraBody"
	providerOptionToolConfig              = "tool_config"
	providerOptionToolConfigGo            = "toolConfig"
	providerOptionFunctionCallingConfig   = "function_calling_config"
	providerOptionFunctionCallingConfigGo = "functionCallingConfig"
	providerOptionIncludeThoughts         = "include_thoughts"
	providerOptionIncludeThoughtsGo       = "includeThoughts"
	providerOptionSafetySettings          = "safety_settings"
	providerOptionSafetySettingsGo        = "safetySettings"
	providerOptionResponseMIMEType        = "response_mime_type"
	providerOptionResponseMIMETypeGo      = "responseMimeType"
	providerOptionResponseSchema          = "response_schema"
	providerOptionResponseSchemaGo        = "responseSchema"
	providerOptionCandidateCount          = "candidate_count"
	providerOptionCandidateCountGo        = "candidateCount"
	providerOptionTopP                    = "top_p"
	providerOptionTopPGo                  = "topP"
	providerOptionTopK                    = "top_k"
	providerOptionTopKGo                  = "topK"
	providerOptionToolSchemaFormat        = "tool_schema_format"
	providerOptionToolSchemaFormatGo      = "toolSchemaFormat"
)

func generativePayload(model sigma.Model, req sigma.Request, opts sigma.Options) (map[string]any, error) {
	transformed, err := transform.Transform(transform.Input{
		TargetModel: model,
		Request:     req,
		Compatibility: transform.Compatibility{
			ConvertDeveloperRole:    true,
			RequireToolResultName:   true,
			DropUnansweredToolCalls: true,
		},
	})
	if err != nil {
		return nil, err
	}

	contents, err := googleContents(model, transformed)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"contents": contents,
	}
	if transformed.SystemPrompt != "" {
		payload["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": providertext.Clean(transformed.SystemPrompt)}},
		}
	}

	generationConfig, err := googleGenerationConfig(model, opts)
	if err != nil {
		return nil, err
	}
	if len(generationConfig) > 0 {
		payload["generationConfig"] = generationConfig
	}
	if len(transformed.Tools) > 0 {
		tools, err := googleTools(model.Provider, transformed.Tools, opts)
		if err != nil {
			return nil, err
		}
		payload["tools"] = tools
	}
	addGoogleProviderOptions(payload, model.Provider, opts, len(transformed.Tools) > 0)
	return payload, nil
}

func googleContents(model sigma.Model, req sigma.Request) ([]map[string]any, error) {
	contents := make([]map[string]any, 0, len(req.Messages))
	ids := newGoogleToolCallIDNormalizer(model)
	for _, message := range req.Messages {
		converted, err := googleContent(model, message, ids)
		if err != nil {
			return nil, err
		}
		if len(converted) > 0 && len(contents) > 0 && mergeFunctionResponseContent(contents[len(contents)-1], converted[0]) {
			converted = converted[1:]
		}
		contents = append(contents, converted...)
	}
	return contents, nil
}

func googleContent(model sigma.Model, message sigma.Message, ids *googleToolCallIDNormalizer) ([]map[string]any, error) {
	switch message.Role {
	case sigma.RoleUser, sigma.RoleDeveloper:
		parts, err := googleInputParts(message.Content)
		if err != nil {
			return nil, err
		}
		return []map[string]any{{"role": "user", "parts": parts}}, nil
	case sigma.RoleAssistant:
		parts, err := googleAssistantParts(model, message, ids)
		if err != nil {
			return nil, err
		}
		if len(parts) == 0 {
			return nil, nil
		}
		return []map[string]any{{"role": "model", "parts": parts}}, nil
	case sigma.RoleTool:
		return googleToolResultContents(model, message, ids)
	default:
		return nil, fmt.Errorf("google generative ai: unsupported message role %q", message.Role)
	}
}

func isFunctionResponseContent(content map[string]any) bool {
	if content["role"] != "user" {
		return false
	}
	parts, ok := content["parts"].([]map[string]any)
	if !ok || len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if _, ok := part["functionResponse"]; !ok {
			return false
		}
	}
	return true
}

func mergeFunctionResponseContent(target map[string]any, source map[string]any) bool {
	if !isFunctionResponseContent(target) || !isFunctionResponseContent(source) {
		return false
	}
	targetParts, ok := target["parts"].([]map[string]any)
	if !ok {
		return false
	}
	sourceParts, ok := source["parts"].([]map[string]any)
	if !ok {
		return false
	}
	target["parts"] = append(targetParts, sourceParts...)
	return true
}

func googleInputParts(blocks []sigma.ContentBlock) ([]map[string]any, error) {
	if len(blocks) == 0 {
		return []map[string]any{{"text": ""}}, nil
	}
	parts := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case sigma.ContentBlockText:
			parts = append(parts, map[string]any{"text": providertext.Clean(block.Text)})
		case sigma.ContentBlockImage:
			image, err := googleImage(block)
			if err != nil {
				return nil, err
			}
			parts = append(parts, image)
		default:
			return nil, fmt.Errorf("google generative ai: unsupported user content block %q", block.Type)
		}
	}
	return parts, nil
}

func googleAssistantParts(model sigma.Model, message sigma.Message, ids *googleToolCallIDNormalizer) ([]map[string]any, error) {
	parts := make([]map[string]any, 0, len(message.Content))
	for _, block := range message.Content {
		switch block.Type {
		case sigma.ContentBlockText:
			text := providertext.Clean(block.Text)
			if text == "" && block.ProviderSignature == "" {
				continue
			}
			part := map[string]any{"text": text}
			addThoughtSignature(part, replayThoughtSignature(model, message, block.ProviderSignature))
			parts = append(parts, part)
		case sigma.ContentBlockThinking:
			thinking := providertext.Clean(block.ThinkingText)
			if thinking == "" && block.ProviderSignature == "" {
				continue
			}
			part := map[string]any{
				"text":    thinking,
				"thought": true,
			}
			addThoughtSignature(part, replayThoughtSignature(model, message, block.ProviderSignature))
			parts = append(parts, part)
		case sigma.ContentBlockToolCall:
			args, err := jsonValue(block.ToolArguments)
			if err != nil {
				return nil, fmt.Errorf("google generative ai: tool %q args: %w", block.ToolName, err)
			}
			if args == nil {
				args = map[string]any{}
			}
			call := map[string]any{
				"name": block.ToolName,
				"args": args,
			}
			if id := ids.normalize(block.ToolCallID); id != "" {
				call["id"] = id
			}
			part := map[string]any{"functionCall": call}
			addThoughtSignature(part, replayThoughtSignature(model, message, block.ProviderSignature))
			parts = append(parts, part)
		default:
			return nil, fmt.Errorf("google generative ai: unsupported assistant content block %q", block.Type)
		}
	}
	return parts, nil
}

func googleToolResultContents(model sigma.Model, message sigma.Message, ids *googleToolCallIDNormalizer) ([]map[string]any, error) {
	text, images, err := googleToolResultContent(message.Content)
	if err != nil {
		return nil, err
	}
	response := map[string]any{"output": text}
	if message.IsError {
		response = map[string]any{"error": text}
	}
	if text == "" && len(images) > 0 {
		if message.IsError {
			response["error"] = "(see attached image)"
		} else {
			response["output"] = "(see attached image)"
		}
	}
	functionResponse := map[string]any{
		"name":     message.ToolName,
		"response": response,
	}
	if id := ids.normalize(message.ToolCallID); id != "" {
		functionResponse["id"] = id
	}
	if len(images) > 0 && supportsGoogleMultimodalFunctionResponse(model.ID) {
		functionResponse["parts"] = images
	}
	part := map[string]any{"functionResponse": functionResponse}
	contents := []map[string]any{{"role": "user", "parts": []map[string]any{part}}}
	if len(images) > 0 && !supportsGoogleMultimodalFunctionResponse(model.ID) {
		sidecar := make([]map[string]any, 0, 1+len(images))
		sidecar = append(sidecar, map[string]any{"text": "Tool result image:"})
		sidecar = append(sidecar, images...)
		contents = append(contents, map[string]any{"role": "user", "parts": sidecar})
	}
	return contents, nil
}

func googleToolResultContent(blocks []sigma.ContentBlock) (string, []map[string]any, error) {
	var text string
	var images []map[string]any
	for _, block := range blocks {
		switch block.Type {
		case sigma.ContentBlockText:
			if text != "" {
				text += "\n"
			}
			text += providertext.Clean(block.Text)
		case sigma.ContentBlockImage:
			image, err := googleImage(block)
			if err != nil {
				return "", nil, err
			}
			images = append(images, image)
		default:
			return "", nil, fmt.Errorf("google generative ai: unsupported tool result content block %q", block.Type)
		}
	}
	return text, images, nil
}

func googleImage(block sigma.ContentBlock) (map[string]any, error) {
	switch block.ImageSource {
	case "base64":
		if block.Data == "" {
			return nil, fmt.Errorf("google generative ai: image data is required")
		}
		if _, err := base64.StdEncoding.DecodeString(block.Data); err != nil {
			return nil, fmt.Errorf("google generative ai: image data must be base64: %w", err)
		}
		mimeType := block.MIMEType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		return map[string]any{
			"inlineData": map[string]any{
				"mimeType": mimeType,
				"data":     block.Data,
			},
		}, nil
	case "url":
		if block.URL == "" {
			return nil, fmt.Errorf("google generative ai: image URL is required")
		}
		fileData := map[string]any{
			"fileUri": block.URL,
		}
		part := map[string]any{
			"fileData": fileData,
		}
		if block.MIMEType != "" {
			fileData["mimeType"] = block.MIMEType
		}
		return part, nil
	default:
		return nil, fmt.Errorf("google generative ai: unsupported image source %q", block.ImageSource)
	}
}

func googleTools(provider sigma.ProviderID, tools []sigma.Tool, opts sigma.Options) ([]map[string]any, error) {
	declarations := make([]map[string]any, 0, len(tools))
	providerTools := make([]map[string]any, 0, len(tools))
	useParameters, err := googleUseLegacyToolParameters(provider, opts)
	if err != nil {
		return nil, err
	}
	for _, tool := range tools {
		if tool.ProviderDefinedType != "" {
			providerTools = append(providerTools, googleProviderTool(tool))
			continue
		}
		parameters, err := jsonValue(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("google generative ai: tool %q schema: %w", tool.Name, err)
		}
		if parameters == nil {
			parameters = map[string]any{"type": "object"}
		}
		declaration := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
		}
		if useParameters {
			declaration["parameters"] = sanitizeForOpenAPI(parameters)
		} else {
			declaration["parametersJsonSchema"] = parameters
		}
		declarations = append(declarations, declaration)
	}
	converted := make([]map[string]any, 0, 1+len(providerTools))
	if len(declarations) > 0 {
		converted = append(converted, map[string]any{"functionDeclarations": declarations})
	}
	converted = append(converted, providerTools...)
	return converted, nil
}

func googleUseLegacyToolParameters(provider sigma.ProviderID, opts sigma.Options) (bool, error) {
	options := providerOptions(opts, provider)
	value, _ := stringOption(options, providerOptionToolSchemaFormat)
	if value == "" {
		value, _ = stringOption(options, providerOptionToolSchemaFormatGo)
	}
	switch value {
	case "", "parameters_json_schema", "parametersJsonSchema":
		return false, nil
	case "parameters":
		return true, nil
	default:
		return false, &sigma.Error{
			Code:     sigma.ErrorInvalidOptions,
			Message:  fmt.Sprintf("google tool schema format %q is not supported", value),
			Provider: provider,
			Err:      sigma.ErrInvalidOptions,
		}
	}
}

func googleProviderTool(tool sigma.Tool) map[string]any {
	toolType := strings.TrimPrefix(tool.ProviderDefinedType, "google.")
	key := snakeToCamel(toolType)
	converted := make(map[string]any, len(tool.ProviderDefinedOptions))
	for optionKey, value := range tool.ProviderDefinedOptions {
		converted[optionKey] = value
	}
	return map[string]any{key: converted}
}

func snakeToCamel(value string) string {
	parts := strings.Split(value, "_")
	for index := 1; index < len(parts); index++ {
		if parts[index] == "" {
			continue
		}
		parts[index] = strings.ToUpper(parts[index][:1]) + parts[index][1:]
	}
	return strings.Join(parts, "")
}

func googleGenerationConfig(model sigma.Model, opts sigma.Options) (map[string]any, error) {
	config := make(map[string]any)
	if opts.Temperature != nil {
		config["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		config["maxOutputTokens"] = *opts.MaxTokens
	}
	addGenerationConfigProviderOptions(config, model.Provider, opts)
	thinking, err := googleThinkingConfig(model, opts)
	if err != nil {
		return nil, err
	}
	if len(thinking) > 0 {
		config["thinkingConfig"] = thinking
	}
	return config, nil
}

func googleThinkingConfig(model sigma.Model, opts sigma.Options) (map[string]any, error) {
	thinking := make(map[string]any)
	options := providerOptions(opts, model.Provider)

	if include, ok := boolOption(options, providerOptionIncludeThoughts); ok {
		thinking["includeThoughts"] = include
	} else if include, ok := boolOption(options, providerOptionIncludeThoughtsGo); ok {
		thinking["includeThoughts"] = include
	}

	if opts.GoogleOptions != nil && opts.GoogleOptions.ThinkingBudgetTokens != nil {
		thinking["thinkingBudget"] = *opts.GoogleOptions.ThinkingBudgetTokens
		if _, ok := thinking["includeThoughts"]; !ok {
			thinking["includeThoughts"] = true
		}
		return thinking, nil
	}
	if googleThinkingDisabled(opts) {
		return googleDisabledThinkingConfig(model), nil
	}
	if opts.ThinkingBudgetTokens != nil {
		thinking["thinkingBudget"] = *opts.ThinkingBudgetTokens
		if _, ok := thinking["includeThoughts"]; !ok {
			thinking["includeThoughts"] = true
		}
		return thinking, nil
	}
	if opts.ReasoningLevel == "" || opts.ReasoningLevel == sigma.ThinkingLevelOff {
		return thinking, nil
	}
	value, ok := model.ProviderThinkingLevel(opts.ReasoningLevel)
	if !ok {
		return nil, &sigma.Error{
			Code:     sigma.ErrorInvalidOptions,
			Message:  fmt.Sprintf("thinking level %q is not supported by model metadata", opts.ReasoningLevel),
			Provider: model.Provider,
			Model:    model.ID,
			Err:      sigma.ErrInvalidOptions,
		}
	}
	if tokens, err := strconv.Atoi(value); err == nil {
		thinking["thinkingBudget"] = tokens
	} else {
		level, ok := googleThinkingLevel(value)
		if !ok {
			return nil, &sigma.Error{
				Code:     sigma.ErrorInvalidOptions,
				Message:  fmt.Sprintf("google thinking level %q is not supported", value),
				Provider: model.Provider,
				Model:    model.ID,
				Err:      sigma.ErrInvalidOptions,
			}
		}
		thinking["thinkingLevel"] = level
	}
	if _, ok := thinking["includeThoughts"]; !ok {
		thinking["includeThoughts"] = true
	}
	return thinking, nil
}

func googleThinkingDisabled(opts sigma.Options) bool {
	if opts.GoogleOptions != nil && opts.GoogleOptions.DisableThinking != nil {
		return *opts.GoogleOptions.DisableThinking
	}
	return opts.ReasoningLevel == sigma.ThinkingLevelOff
}

func googleDisabledThinkingConfig(model sigma.Model) map[string]any {
	switch {
	case isGemini3ProModel(model.ID):
		return map[string]any{"thinkingLevel": "LOW"}
	case isGemini3FlashModel(model.ID), isGemma4Model(model.ID):
		return map[string]any{"thinkingLevel": "MINIMAL"}
	default:
		return map[string]any{"thinkingBudget": 0}
	}
}

func googleThinkingLevel(value string) (string, bool) {
	switch strings.ToUpper(strings.ReplaceAll(value, "-", "_")) {
	case "MINIMAL":
		return "MINIMAL", true
	case "LOW":
		return "LOW", true
	case "MEDIUM":
		return "MEDIUM", true
	case "HIGH":
		return "HIGH", true
	default:
		return "", false
	}
}

func addGenerationConfigProviderOptions(config map[string]any, provider sigma.ProviderID, opts sigma.Options) {
	options := providerOptions(opts, provider)
	copyOption(config, options, providerOptionResponseMIMEType, "responseMimeType")
	copyOption(config, options, providerOptionResponseMIMETypeGo, "responseMimeType")
	copyOption(config, options, providerOptionResponseSchema, "responseSchema")
	copyOption(config, options, providerOptionResponseSchemaGo, "responseSchema")
	copyOption(config, options, providerOptionCandidateCount, "candidateCount")
	copyOption(config, options, providerOptionCandidateCountGo, "candidateCount")
	copyOption(config, options, providerOptionTopP, "topP")
	copyOption(config, options, providerOptionTopPGo, "topP")
	copyOption(config, options, providerOptionTopK, "topK")
	copyOption(config, options, providerOptionTopKGo, "topK")
}

func addGoogleProviderOptions(payload map[string]any, provider sigma.ProviderID, opts sigma.Options, hasTools bool) {
	options := providerOptions(opts, provider)
	if value, ok := googleToolConfig(options, opts, hasTools); ok {
		payload["toolConfig"] = value
	}
	if value, ok := options[providerOptionSafetySettings]; ok {
		payload["safetySettings"] = value
	} else if value, ok := options[providerOptionSafetySettingsGo]; ok {
		payload["safetySettings"] = value
	}
	for key, value := range extraBody(opts, provider) {
		payload[key] = value
	}
}

func googleToolConfig(options map[string]any, opts sigma.Options, hasTools bool) (any, bool) {
	if value, ok := options[providerOptionToolConfig]; ok {
		return value, true
	}
	if value, ok := options[providerOptionToolConfigGo]; ok {
		return value, true
	}
	if value, ok := options[providerOptionFunctionCallingConfig]; ok {
		return map[string]any{"functionCallingConfig": value}, true
	}
	if value, ok := options[providerOptionFunctionCallingConfigGo]; ok {
		return map[string]any{"functionCallingConfig": value}, true
	}
	if !hasTools || opts.GoogleOptions == nil || opts.GoogleOptions.ToolChoice == "" {
		return nil, false
	}
	mode, err := googleToolChoiceMode(opts.GoogleOptions.ToolChoice)
	if err != nil {
		return nil, false
	}
	return map[string]any{"functionCallingConfig": map[string]any{"mode": mode}}, true
}

func googleToolChoiceMode(choice string) (string, error) {
	switch strings.ToLower(choice) {
	case "auto":
		return "AUTO", nil
	case "none":
		return "NONE", nil
	case "any":
		return "ANY", nil
	default:
		return "", &sigma.Error{
			Code:    sigma.ErrorInvalidOptions,
			Message: fmt.Sprintf("google tool choice %q is not supported", choice),
			Err:     sigma.ErrInvalidOptions,
		}
	}
}

func addThoughtSignature(part map[string]any, signature string) {
	if signature != "" {
		part["thoughtSignature"] = signature
	}
}

func replayThoughtSignature(model sigma.Model, message sigma.Message, signature string) string {
	if message.Provider != model.Provider || message.API != model.API || message.Model != model.ID {
		return ""
	}
	if !validThoughtSignature(signature) {
		return ""
	}
	return signature
}

func validThoughtSignature(signature string) bool {
	if signature == "" || len(signature)%4 != 0 {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(signature)
	return err == nil
}

func sanitizeForOpenAPI(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, nested := range v {
			if isJSONSchemaMetaDeclaration(key) {
				continue
			}
			out[key] = sanitizeForOpenAPI(nested)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for index, nested := range v {
			out[index] = sanitizeForOpenAPI(nested)
		}
		return out
	default:
		return value
	}
}

func isJSONSchemaMetaDeclaration(key string) bool {
	switch key {
	case "$schema", "$id", "$anchor", "$dynamicAnchor", "$vocabulary", "$comment", "$defs", "definitions":
		return true
	default:
		return false
	}
}

func supportsGoogleMultimodalFunctionResponse(modelID sigma.ModelID) bool {
	major, ok := geminiMajorVersion(modelID)
	if !ok {
		return true
	}
	return major >= 3
}

type googleToolCallIDNormalizer struct {
	emit     bool
	required bool
	ids      map[string]string
	used     map[string]string
}

func newGoogleToolCallIDNormalizer(model sigma.Model) *googleToolCallIDNormalizer {
	return &googleToolCallIDNormalizer{
		emit:     model.API != sigma.APIGoogleVertex,
		required: requiresGoogleToolCallID(model),
		ids:      make(map[string]string),
		used:     make(map[string]string),
	}
}

func (n *googleToolCallIDNormalizer) normalize(id string) string {
	if !n.emit {
		return ""
	}
	if id == "" {
		return ""
	}
	if !n.required {
		return id
	}
	if normalized := n.ids[id]; normalized != "" {
		return normalized
	}
	base := googleSafeToolCallID(id)
	for attempt := 0; ; attempt++ {
		candidate := base
		if attempt > 0 {
			suffix := "_" + strconv.Itoa(attempt)
			candidate = strings.TrimRight(base[:min(len(base), max(1, 64-len(suffix)))], "_") + suffix
		}
		if owner := n.used[candidate]; owner == "" || owner == id {
			n.ids[id] = candidate
			n.used[candidate] = id
			return candidate
		}
	}
}

func googleSafeToolCallID(id string) string {
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
		if out.Len() == 64 {
			break
		}
	}
	if out.Len() == 0 {
		return "_"
	}
	return out.String()
}

func requiresGoogleToolCallID(model sigma.Model) bool {
	if model.API == sigma.APIGoogleVertex {
		return false
	}
	id := strings.ToLower(string(model.ID))
	return strings.HasPrefix(id, "claude-") || strings.HasPrefix(id, "gpt-oss-")
}

func geminiMajorVersion(modelID sigma.ModelID) (int, bool) {
	id := strings.ToLower(string(modelID))
	found := false
	for _, prefix := range []string{"gemini-live-", "gemini-"} {
		if strings.HasPrefix(id, prefix) {
			id = strings.TrimPrefix(id, prefix)
			found = true
			break
		}
	}
	if !found {
		return 0, false
	}
	dot := strings.IndexAny(id, ".-")
	if dot >= 0 {
		id = id[:dot]
	}
	major, err := strconv.Atoi(id)
	if err != nil {
		return 0, false
	}
	return major, true
}

func isGemini3ProModel(modelID sigma.ModelID) bool {
	id := strings.ToLower(string(modelID))
	return strings.HasPrefix(id, "gemini-3") && strings.Contains(id, "-pro")
}

func isGemini3FlashModel(modelID sigma.ModelID) bool {
	id := strings.ToLower(string(modelID))
	return strings.HasPrefix(id, "gemini-3") && strings.Contains(id, "-flash")
}

func isGemma4Model(modelID sigma.ModelID) bool {
	id := strings.ToLower(string(modelID))
	return strings.Contains(id, "gemma-4") || strings.Contains(id, "gemma4")
}

func providerOptions(opts sigma.Options, provider sigma.ProviderID) map[string]any {
	if len(opts.ProviderOptions) == 0 {
		return nil
	}
	if values := opts.ProviderOptions[provider]; len(values) > 0 {
		return values
	}
	return opts.ProviderOptions[sigma.ProviderGoogle]
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

func copyOption(target map[string]any, source map[string]any, sourceKey string, targetKey string) {
	if value, ok := source[sourceKey]; ok {
		target[targetKey] = value
	}
}
