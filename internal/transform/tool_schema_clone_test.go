// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package transform

import (
	"encoding/json"
	"testing"

	"github.com/wintermi/sigma"
)

func TestTransformToolsKeepEmptySchemaObjects(t *testing.T) {
	t.Parallel()

	tools := []sigma.Tool{{
		Name: "noargs",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
			"additionalProperties": map[string]any{
				"type":  "array",
				"items": map[string]any{},
			},
		},
	}}
	want := `{"additionalProperties":{"items":{},"type":"array"},"properties":{},"type":"object"}`

	for name, got := range map[string][]sigma.Tool{
		"Transform":               mustTransformTools(t, tools),
		"DropUnansweredToolCalls": DropUnansweredToolCalls(sigma.Request{Tools: tools}).Tools,
	} {
		encoded, err := json.Marshal(got[0].InputSchema)
		if err != nil {
			t.Fatalf("%s: marshal schema: %v", name, err)
		}
		if string(encoded) != want {
			t.Fatalf("%s: schema = %s, want %s", name, encoded, want)
		}
	}
}

func TestTransformToolsIsolateTypedSchemaContainers(t *testing.T) {
	t.Parallel()

	properties := map[string]sigma.Schema{"path": {"type": "string"}}
	tools := []sigma.Tool{{
		Name:        "read",
		InputSchema: sigma.Schema{"type": "object", "properties": properties},
	}}

	got := mustTransformTools(t, tools)
	got[0].InputSchema.(sigma.Schema)["properties"].(map[string]sigma.Schema)["path"]["type"] = "number"

	if properties["path"]["type"] != "string" {
		t.Fatalf("caller schema mutated through transformed tool: %v", properties["path"])
	}
}

func mustTransformTools(t *testing.T, tools []sigma.Tool) []sigma.Tool {
	t.Helper()
	transformed, err := Transform(Input{
		TargetModel: sigma.Model{ID: "target", Provider: sigma.ProviderAnthropic, API: sigma.APIAnthropicMessages},
		Request:     sigma.Request{Tools: tools},
	})
	if err != nil {
		t.Fatalf("Transform returned error: %v", err)
	}
	return transformed.Tools
}
