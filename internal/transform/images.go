// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package transform

import "github.com/wintermi/sigma"

const (
	nonVisionUserImagePlaceholder = "(image omitted: model does not support images)"
	nonVisionToolImagePlaceholder = "(tool image omitted: model does not support images)"
)

// ReplaceUnsupportedImages replaces each run of image blocks in user,
// developer, and tool messages with placeholder text when model does not
// accept image input. A conversation that once included a screenshot can then
// continue on a text-only model, and the model still learns an image was there.
// Requests without images, or for vision models, are returned unchanged.
func ReplaceUnsupportedImages(model sigma.Model, req sigma.Request) sigma.Request {
	if model.SupportsImages() || !requestHasImages(req) {
		return req
	}
	messages := make([]sigma.Message, len(req.Messages))
	for index, message := range req.Messages {
		messages[index] = message
		placeholder := nonVisionUserImagePlaceholder
		switch message.Role {
		case sigma.RoleTool:
			placeholder = nonVisionToolImagePlaceholder
		case sigma.RoleUser, sigma.RoleDeveloper:
		default:
			continue
		}
		if !messageHasImages(message) {
			continue
		}
		content := make([]sigma.ContentBlock, 0, len(message.Content))
		previousWasPlaceholder := false
		for _, block := range message.Content {
			if block.Type == sigma.ContentBlockImage {
				if !previousWasPlaceholder {
					content = append(content, sigma.Text(placeholder))
				}
				previousWasPlaceholder = true
				continue
			}
			content = append(content, block)
			previousWasPlaceholder = false
		}
		messages[index].Content = content
	}
	req.Messages = messages
	return req
}

func requestHasImages(req sigma.Request) bool {
	for _, message := range req.Messages {
		if messageHasImages(message) {
			return true
		}
	}
	return false
}

func messageHasImages(message sigma.Message) bool {
	for _, block := range message.Content {
		if block.Type == sigma.ContentBlockImage {
			return true
		}
	}
	return false
}
