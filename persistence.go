// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

const (
	// ErrorInvalidRequest indicates a persisted request cannot be replayed safely.
	ErrorInvalidRequest ErrorCode = "invalid-request"
)

// MarshalRequest serializes req as the public Request JSON shape.
func MarshalRequest(req Request) ([]byte, error) {
	if err := ValidateRequest(req); err != nil {
		return nil, err
	}
	return json.Marshal(req)
}

// UnmarshalRequest decodes Request JSON and validates it for replay.
//
// Unknown struct fields are rejected. ProviderMetadata, ToolArguments, and tool
// schemas remain open JSON maps because providers may need opaque continuation
// data that sigma does not interpret.
func UnmarshalRequest(data []byte) (Request, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()

	var req Request
	if err := decoder.Decode(&req); err != nil {
		return Request{}, invalidRequestError("decode request: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value")
		}
		return Request{}, invalidRequestError("decode request: %v", err)
	}
	if err := ValidateRequest(req); err != nil {
		return Request{}, err
	}
	return req, nil
}

// ValidateRequest checks that req is structurally safe to persist and replay.
func ValidateRequest(req Request) error {
	toolCalls := make(map[string]string)
	for messageIndex, message := range req.Messages {
		if err := validateMessage(message, messageIndex, toolCalls); err != nil {
			return err
		}
	}
	return nil
}

func validateMessage(message Message, index int, toolCalls map[string]string) error {
	switch message.Role {
	case RoleUser, RoleDeveloper, RoleAssistant, RoleTool:
	default:
		if message.Role == "" {
			return invalidRequestError("message %d: role is required", index)
		}
		return invalidRequestError("message %d: unsupported role %q", index, message.Role)
	}

	if message.Role != RoleTool && (message.ToolCallID != "" || message.ToolName != "" || message.IsError) {
		return invalidRequestError("message %d: tool result fields require role %q", index, RoleTool)
	}
	if message.Role != RoleAssistant && (message.Provider != "" || message.API != "" || message.Model != "" || message.StopReason != "") {
		return invalidRequestError("message %d: assistant metadata requires role %q", index, RoleAssistant)
	}
	if message.Role != RoleAssistant && message.Usage != nil {
		return invalidRequestError("message %d: assistant usage requires role %q", index, RoleAssistant)
	}
	if message.Role == RoleAssistant {
		if err := validateAssistantMetadata(message, index); err != nil {
			return err
		}
	}
	if message.Role == RoleTool {
		if message.ToolCallID == "" {
			return invalidRequestError("message %d: tool result is missing tool call id", index)
		}
		if _, ok := toolCalls[message.ToolCallID]; !ok {
			return invalidRequestError("message %d: tool result %q has no preceding assistant tool call", index, message.ToolCallID)
		}
	}

	hasToolCall := false
	for contentIndex, block := range message.Content {
		if err := validateContentBlock(message.Role, block, index, contentIndex); err != nil {
			return err
		}
		if block.Type != ContentBlockToolCall {
			continue
		}
		hasToolCall = true
		if _, exists := toolCalls[block.ToolCallID]; exists {
			return invalidRequestError("message %d content %d: duplicate tool call id %q", index, contentIndex, block.ToolCallID)
		}
		toolCalls[block.ToolCallID] = block.ToolName
	}
	if message.Role == RoleAssistant && message.StopReason == StopReasonToolCalls && !hasToolCall {
		return invalidRequestError("message %d: stop reason %q requires a tool call", index, StopReasonToolCalls)
	}
	return nil
}

func validateAssistantMetadata(message Message, index int) error {
	if (message.API != "" || message.Model != "") && message.Provider == "" {
		return invalidRequestError("message %d: assistant api or model metadata requires provider metadata", index)
	}
	switch message.StopReason {
	case "", StopReasonEndTurn, StopReasonMaxTokens, StopReasonStopSequence, StopReasonToolCalls, StopReasonContentFilter, StopReasonError, StopReasonUnknown, StopReasonAborted:
		return nil
	default:
		return invalidRequestError("message %d: unsupported assistant stop reason %q", index, message.StopReason)
	}
}

func validateContentBlock(role Role, block ContentBlock, messageIndex int, contentIndex int) error {
	if !roleAllowsContentBlock(role, block.Type) {
		if block.Type == "" {
			return invalidRequestError("message %d content %d: content block type is required", messageIndex, contentIndex)
		}
		return invalidRequestError("message %d content %d: role %q cannot contain %q blocks", messageIndex, contentIndex, role, block.Type)
	}
	switch block.Type {
	case ContentBlockText:
		if block.Text == "" {
			return invalidRequestError("message %d content %d: text block is empty", messageIndex, contentIndex)
		}
	case ContentBlockThinking:
		if block.ThinkingText == "" && block.Signature == "" && block.ProviderSignature == "" && !block.Redacted {
			return invalidRequestError("message %d content %d: thinking block has no thinking text or signature", messageIndex, contentIndex)
		}
	case ContentBlockImage:
		if err := validateImageBlock(block); err != nil {
			return invalidRequestError("message %d content %d: %v", messageIndex, contentIndex, err)
		}
	case ContentBlockDocument:
		if err := validateDocumentBlock(block); err != nil {
			return invalidRequestError("message %d content %d: %v", messageIndex, contentIndex, err)
		}
	case ContentBlockToolCall:
		if block.ToolCallID == "" {
			return invalidRequestError("message %d content %d: tool call id is required", messageIndex, contentIndex)
		}
		if block.ToolName == "" {
			return invalidRequestError("message %d content %d: tool call name is required", messageIndex, contentIndex)
		}
		if _, err := json.Marshal(block.ToolArguments); err != nil {
			return invalidRequestError("message %d content %d: tool arguments are not JSON serializable: %v", messageIndex, contentIndex, err)
		}
	default:
		return invalidRequestError("message %d content %d: unsupported content block type %q", messageIndex, contentIndex, block.Type)
	}
	return nil
}

func roleAllowsContentBlock(role Role, blockType ContentBlockType) bool {
	switch role {
	case RoleUser:
		return blockType == ContentBlockText || blockType == ContentBlockImage || blockType == ContentBlockDocument
	case RoleDeveloper:
		return blockType == ContentBlockText
	case RoleAssistant:
		return blockType == ContentBlockText || blockType == ContentBlockThinking || blockType == ContentBlockToolCall
	case RoleTool:
		return blockType == ContentBlockText || blockType == ContentBlockImage || blockType == ContentBlockDocument
	default:
		return false
	}
}

func validateImageBlock(block ContentBlock) error {
	if block.MIMEType == "" {
		return fmt.Errorf("image MIME type is required")
	}
	switch block.ImageSource {
	case "base64":
		if block.Data == "" {
			return fmt.Errorf("base64 image data is required")
		}
		if _, err := base64.StdEncoding.DecodeString(block.Data); err != nil {
			return fmt.Errorf("base64 image data is invalid: %w", err)
		}
	case "url":
		if block.URL == "" {
			return fmt.Errorf("image URL is required")
		}
	default:
		if block.ImageSource == "" {
			return fmt.Errorf("image source is required")
		}
		return fmt.Errorf("unsupported image source %q", block.ImageSource)
	}
	return nil
}

func validateDocumentBlock(block ContentBlock) error {
	if block.MIMEType == "" {
		return fmt.Errorf("document MIME type is required")
	}
	if block.Filename == "" {
		return fmt.Errorf("document filename is required")
	}
	switch block.DocumentSource {
	case "base64":
		if block.Data == "" {
			return fmt.Errorf("base64 document data is required")
		}
		if _, err := base64.StdEncoding.DecodeString(block.Data); err != nil {
			return fmt.Errorf("base64 document data is invalid: %w", err)
		}
	case "url":
		if block.URL == "" {
			return fmt.Errorf("document URL is required")
		}
	case "file_id":
		if block.FileID == "" {
			return fmt.Errorf("document file id is required")
		}
	default:
		if block.DocumentSource == "" {
			return fmt.Errorf("document source is required")
		}
		return fmt.Errorf("unsupported document source %q", block.DocumentSource)
	}
	return nil
}

func invalidRequestError(format string, args ...any) error {
	return &Error{
		Code:    ErrorInvalidRequest,
		Message: fmt.Sprintf(format, args...),
	}
}
