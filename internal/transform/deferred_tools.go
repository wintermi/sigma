// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package transform

import "github.com/wintermi/sigma"

// DeferredToolPlan partitions current client-defined tools into definitions
// available at request start and definitions loaded from prior tool-result
// markers. Provider-defined tools always remain available at request start.
type DeferredToolPlan struct {
	Immediate []sigma.Tool
	Deferred  map[string]sigma.Tool
}

// PlanDeferredTools returns the eager and deferred client tool definitions for
// req. Deferred loading is inactive unless enabled. normalize maps caller tool
// names to provider wire names and may be nil for identity behavior.
func PlanDeferredTools(req sigma.Request, enabled bool, normalize func(string) string) DeferredToolPlan {
	if !enabled {
		return DeferredToolPlan{Immediate: append([]sigma.Tool(nil), req.Tools...)}
	}
	if normalize == nil {
		normalize = func(name string) string { return name }
	}

	definitions := make(map[string]sigma.Tool)
	for _, tool := range req.Tools {
		if tool.ProviderDefinedType != "" {
			continue
		}
		name := normalize(tool.Name)
		if name == "" {
			continue
		}
		definitions[name] = tool
	}

	used := make(map[string]struct{})
	deferredNames := make(map[string]struct{})
	for _, message := range req.Messages {
		if message.Role == sigma.RoleAssistant {
			for _, block := range message.Content {
				if block.Type == sigma.ContentBlockToolCall {
					used[normalize(block.ToolName)] = struct{}{}
				}
			}
			continue
		}
		if message.Role != sigma.RoleTool {
			continue
		}
		for _, name := range message.AddedToolNames {
			name = normalize(name)
			if name == "" {
				continue
			}
			if _, exists := definitions[name]; !exists {
				continue
			}
			if _, exists := used[name]; exists {
				continue
			}
			deferredNames[name] = struct{}{}
		}
	}
	if len(deferredNames) == 0 {
		return DeferredToolPlan{Immediate: append([]sigma.Tool(nil), req.Tools...)}
	}

	plan := DeferredToolPlan{Deferred: make(map[string]sigma.Tool, len(deferredNames))}
	seenDefinitions := make(map[string]struct{}, len(definitions))
	for _, tool := range req.Tools {
		if tool.ProviderDefinedType != "" {
			plan.Immediate = append(plan.Immediate, tool)
			continue
		}
		name := normalize(tool.Name)
		if name == "" {
			plan.Immediate = append(plan.Immediate, tool)
			continue
		}
		if _, seen := seenDefinitions[name]; seen {
			continue
		}
		seenDefinitions[name] = struct{}{}
		definition := definitions[name]
		if _, deferred := deferredNames[name]; deferred {
			plan.Deferred[name] = definition
			continue
		}
		plan.Immediate = append(plan.Immediate, definition)
	}
	return plan
}
