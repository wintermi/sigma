// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/wintermi/sigma"
)

// responsesIDs keeps normalization consistent across calls and their results without
// changing persisted messages. Item IDs and call IDs occupy separate namespaces.
type responsesIDs struct {
	calls     map[string]string
	usedCalls map[string]bool
	usedItems map[string]bool
}

func newResponsesIDs(messages []sigma.Message) *responsesIDs {
	ids := &responsesIDs{calls: make(map[string]string), usedCalls: make(map[string]bool), usedItems: make(map[string]bool)}
	var originals []string
	for _, message := range messages {
		if message.Role == sigma.RoleTool {
			originals = append(originals, message.ToolCallID)
		}
		for _, block := range message.Content {
			raw := providerID(block.ProviderMetadata)
			if block.Type == sigma.ContentBlockToolCall {
				call := firstNonEmpty(block.ToolCallID, providerMetadataString(block.ProviderMetadata, "call_id"))
				originals = append(originals, call)
				if _, item, ok := strings.Cut(call, "|"); ok && raw == "" {
					raw = item
				}
			}
			if validResponsesCallID(raw) {
				ids.usedItems[raw] = true
			}
		}
	}
	// Bare valid IDs take priority over compound IDs with the same call portion.
	for _, raw := range originals {
		if validResponsesCallID(raw) {
			ids.calls[raw] = raw
			ids.usedCalls[raw] = true
		}
	}
	for _, raw := range originals {
		ids.callID(raw)
	}
	return ids
}

func validResponsesCallID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, ch := range id {
		switch {
		case ch == '_', ch == '-', ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		default:
			return false
		}
	}
	return true
}

func (ids *responsesIDs) callID(raw string) string {
	if id, ok := ids.calls[raw]; ok {
		return id
	}
	candidate, _, _ := strings.Cut(raw, "|")
	if !validResponsesCallID(candidate) || ids.usedCalls[candidate] {
		sum := sha256.Sum256([]byte(raw))
		prefix := sanitizeResponsesID(candidate)
		if prefix == "" {
			prefix = "call_sigma"
		}
		if len(prefix) > 47 {
			prefix = prefix[:47]
		}
		candidate = prefix + "_" + hex.EncodeToString(sum[:8])
	}
	id := uniqueResponsesID(candidate, ids.usedCalls)
	ids.calls[raw] = id
	return id
}

func uniqueResponsesID(candidate string, used map[string]bool) string {
	id := candidate
	for ordinal := 1; used[id]; ordinal++ {
		suffix := fmt.Sprintf("_%d", ordinal)
		prefix := candidate
		if len(prefix)+len(suffix) > 64 {
			prefix = prefix[:64-len(suffix)]
		}
		id = prefix + suffix
	}
	used[id] = true
	return id
}

func (ids *responsesIDs) itemID(prefix, raw, fallback string) string {
	candidate := responsesBoundedID(prefix, raw, fallback)
	if raw != "" && raw == candidate {
		return candidate
	}
	return uniqueResponsesID(candidate, ids.usedItems)
}

func responsesToolItemID(prefix, raw, fallback string) string {
	id := responsesBoundedID(prefix, raw, fallback)
	if !strings.HasPrefix(id, prefix+"_") {
		id = responsesBoundedID(prefix, prefix+"_"+id, fallback)
	}
	return id
}

func (ids *responsesIDs) toolIDs(block sigma.ContentBlock, prefix, fallback string) (string, string) {
	raw := firstNonEmpty(block.ToolCallID, providerMetadataString(block.ProviderMetadata, "call_id"))
	item := providerID(block.ProviderMetadata)
	if _, compoundItem, ok := strings.Cut(raw, "|"); ok && item == "" {
		item = compoundItem
	}
	itemID := responsesToolItemID(prefix, item, fallback)
	if item == "" || item != itemID {
		itemID = uniqueResponsesID(itemID, ids.usedItems)
	}
	if raw == "" {
		return uniqueResponsesID(fallback+"_call", ids.usedCalls), itemID
	}
	return ids.callID(raw), itemID
}
