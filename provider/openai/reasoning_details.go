// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"encoding/json"
	"fmt"
)

const orderedReasoningDetailsMetadataKey = "openai_reasoning_details"

func parseReasoningDetails(raw json.RawMessage) ([]any, error) {
	var details []any
	if err := json.Unmarshal(raw, &details); err != nil {
		return nil, fmt.Errorf("openai completions: decode reasoning_details: %w", err)
	}
	return filterReasoningDetails(details), nil
}

func validatedReasoningDetails(value any) []any {
	switch details := value.(type) {
	case []any:
		return filterReasoningDetails(details)
	case []map[string]any:
		items := make([]any, len(details))
		for index, detail := range details {
			items[index] = detail
		}
		return filterReasoningDetails(items)
	default:
		return nil
	}
}

func filterReasoningDetails(details []any) []any {
	valid := make([]any, 0, len(details))
	for _, detail := range details {
		raw, err := json.Marshal(detail)
		if err != nil {
			continue
		}
		var candidate map[string]any
		if err := json.Unmarshal(raw, &candidate); err != nil || !validReasoningDetail(candidate) {
			continue
		}
		valid = append(valid, candidate)
	}
	if len(valid) == 0 {
		return nil
	}
	return valid
}

func appendReasoningDetail(details []any, detail any) []any {
	next, ok := detail.(map[string]any)
	if !ok || len(details) == 0 {
		return append(details, detail)
	}
	current, ok := details[len(details)-1].(map[string]any)
	if !ok || current["type"] != next["type"] {
		return append(details, detail)
	}

	switch next["type"] {
	case "reasoning.text":
		currentText, currentOK := current["text"].(string)
		nextText, nextOK := next["text"].(string)
		if !currentOK || !nextOK {
			return append(details, detail)
		}
		current["text"] = currentText + nextText
		fillMissingReasoningDetailString(current, next, "signature")
	case "reasoning.summary":
		currentSummary, currentOK := current["summary"].(string)
		nextSummary, nextOK := next["summary"].(string)
		if !currentOK || !nextOK {
			return append(details, detail)
		}
		current["summary"] = currentSummary + nextSummary
	default:
		return append(details, detail)
	}
	fillMissingReasoningDetailString(current, next, "id")
	fillMissingReasoningDetailString(current, next, "format")
	fillMissingReasoningDetailValue(current, next, "index")
	return details
}

func fillMissingReasoningDetailString(target, source map[string]any, key string) {
	if value, _ := target[key].(string); value != "" {
		return
	}
	if value, _ := source[key].(string); value != "" {
		target[key] = value
	}
}

func fillMissingReasoningDetailValue(target, source map[string]any, key string) {
	if value, ok := target[key]; ok && value != nil {
		return
	}
	if value, ok := source[key]; ok && value != nil {
		target[key] = value
	}
}

func validReasoningDetail(detail map[string]any) bool {
	if !optionalNilOrString(detail, "id") || !optionalString(detail, "format") || !optionalNumber(detail, "index") {
		return false
	}
	detailType, _ := detail["type"].(string)
	switch detailType {
	case "reasoning.summary":
		_, ok := detail["summary"].(string)
		return ok
	case "reasoning.encrypted":
		_, ok := detail["data"].(string)
		return ok
	case "reasoning.text":
		if _, ok := detail["text"].(string); !ok {
			return false
		}
		return optionalNilOrString(detail, "signature")
	default:
		return false
	}
}

func optionalNilOrString(values map[string]any, key string) bool {
	value, ok := values[key]
	if !ok || value == nil {
		return true
	}
	_, ok = value.(string)
	return ok
}

func optionalString(values map[string]any, key string) bool {
	value, ok := values[key]
	if !ok {
		return true
	}
	_, ok = value.(string)
	return ok
}

func optionalNumber(values map[string]any, key string) bool {
	value, ok := values[key]
	if !ok {
		return true
	}
	_, ok = value.(float64)
	return ok
}
