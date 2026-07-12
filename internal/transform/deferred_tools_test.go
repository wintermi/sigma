// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package transform

import (
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestPlanDeferredTools(t *testing.T) {
	t.Parallel()

	base := sigma.Tool{Name: "base"}
	late := sigma.Tool{Name: "late"}
	hosted := sigma.Tool{Name: "web_search", ProviderDefinedType: "web_search_20250305"}
	req := sigma.Request{
		Tools: []sigma.Tool{base, late, hosted},
		Messages: []sigma.Message{
			{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_base", "base", nil)}},
			{Role: sigma.RoleTool, ToolCallID: "call_base", AddedToolNames: []string{"late", "missing", "late"}},
		},
	}

	plan := PlanDeferredTools(req, true, nil)
	if got, want := toolNames(plan.Immediate), []string{"base", "web_search"}; !sameStrings(got, want) {
		t.Fatalf("immediate tools = %#v, want %#v", got, want)
	}
	if got, ok := plan.Deferred["late"]; !ok || got.Name != "late" {
		t.Fatalf("deferred tools = %#v, want late", plan.Deferred)
	}

	unchanged := PlanDeferredTools(req, false, nil)
	if got, want := toolNames(unchanged.Immediate), []string{"base", "late", "web_search"}; !sameStrings(got, want) {
		t.Fatalf("disabled immediate tools = %#v, want %#v", got, want)
	}
	if len(unchanged.Deferred) != 0 {
		t.Fatalf("disabled deferred tools = %#v, want none", unchanged.Deferred)
	}
}

func TestPlanDeferredToolsKeepsPreviouslyUsedToolImmediate(t *testing.T) {
	t.Parallel()

	req := sigma.Request{
		Tools: []sigma.Tool{{Name: "late"}},
		Messages: []sigma.Message{
			{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_late", "late", nil)}},
			{Role: sigma.RoleTool, ToolCallID: "call_late", AddedToolNames: []string{"late"}},
		},
	}

	plan := PlanDeferredTools(req, true, strings.ToUpper)
	if got, want := toolNames(plan.Immediate), []string{"late"}; !sameStrings(got, want) {
		t.Fatalf("immediate tools = %#v, want %#v", got, want)
	}
	if len(plan.Deferred) != 0 {
		t.Fatalf("deferred tools = %#v, want none", plan.Deferred)
	}
}

func toolNames(tools []sigma.Tool) []string {
	names := make([]string, len(tools))
	for index, tool := range tools {
		names[index] = tool.Name
	}
	return names
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
