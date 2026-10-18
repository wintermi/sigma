// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package transform

import (
	"fmt"

	"github.com/wintermi/sigma"
)

// ErrorToolImagePlaceholder stands in for the images of an error tool result
// that are sent after the turn's tool results.
const ErrorToolImagePlaceholder = "(image attached after the tool results)"

// ErrorToolResultSplit is an error tool result split into text-only content and
// the images to send after the turn's tool results.
type ErrorToolResultSplit struct {
	// Content replaces the tool result content.
	Content []sigma.ContentBlock
	// Caption names the tool call and precedes Images.
	Caption string
	Images  []sigma.ContentBlock
}

// SplitErrorToolResultImages splits the images out of an error tool result for
// APIs that reject images in error results (Anthropic, and Claude on Bedrock).
// Send Content as the tool result, then Caption and Images after all tool
// results of the turn in the same user message. ok is false for results that
// need no split.
func SplitErrorToolResultImages(message sigma.Message) (split ErrorToolResultSplit, ok bool) {
	if !message.IsError {
		return ErrorToolResultSplit{}, false
	}
	previousWasPlaceholder := false
	for _, block := range message.Content {
		if block.Type == sigma.ContentBlockImage {
			split.Images = append(split.Images, block)
			if !previousWasPlaceholder {
				split.Content = append(split.Content, sigma.Text(ErrorToolImagePlaceholder))
			}
			previousWasPlaceholder = true
			continue
		}
		split.Content = append(split.Content, block)
		previousWasPlaceholder = block.Type == sigma.ContentBlockText && block.Text == ErrorToolImagePlaceholder
	}
	if len(split.Images) == 0 {
		return ErrorToolResultSplit{}, false
	}
	split.Caption = fmt.Sprintf("Images from the error result of tool call %s (%s):", message.ToolCallID, message.ToolName)
	return split, true
}
