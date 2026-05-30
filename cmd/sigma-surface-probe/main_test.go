// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"errors"
	"testing"

	"github.com/wintermi/sigma"
)

func TestOpenCodeRouteAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		route string
		id    string
		want  sigma.API
	}{
		{route: "zen", id: "gemini-3-flash", want: sigma.APIGoogleGenerativeAI},
		{route: "zen", id: "claude-opus-4-7", want: sigma.APIAnthropicMessages},
		{route: "zen", id: "qwen3.6-plus", want: sigma.APIAnthropicMessages},
		{route: "zen", id: "gpt-5.1-codex", want: sigma.APIOpenAIResponses},
		{route: "zen", id: "kimi-k2.6", want: sigma.APIOpenAICompletions},
		{route: "go", id: "qwen3.7-max", want: sigma.APIAnthropicMessages},
		{route: "go", id: "minimax-m2.5", want: sigma.APIAnthropicMessages},
		{route: "go", id: "kimi-k2.6", want: sigma.APIOpenAICompletions},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.route+"/"+tt.id, func(t *testing.T) {
			t.Parallel()

			if got := openCodeRouteAPI(tt.route, tt.id); got != tt.want {
				t.Fatalf("openCodeRouteAPI = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKnownUnavailable(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"claude-opus-4-6", "minimax-m2.5-free", "qwen3.6-plus-free", "gpt-5.3-codex-spark"} {
		if !knownUnavailable("zen", id) {
			t.Fatalf("%s was not classified as known unavailable", id)
		}
	}
	if knownUnavailable("go", "qwen3.7-max") {
		t.Fatal("go qwen3.7-max should not be skipped")
	}
}

func TestRepairVariantsCoverTargetedFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "basic_text", want: "basic_text_more_tokens"},
		{name: "cache_ephemeral", want: "cache_none_more_tokens"},
		{name: "image_input", want: "text_only_fallback"},
		{name: "thinking_string_none", want: "thinking_object_disabled_repair"},
		{name: "reasoning_effort_high", want: "typed_reasoning_effort_high"},
		{name: "json_schema", want: "json_object_fallback"},
		{name: "logprobs", want: "no_logprobs_more_tokens"},
		{name: "tool_required_file_read", want: "tool_auto_more_turns"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !hasRepairVariant(repairVariants(probeCase{Name: tt.name}), tt.want) {
				t.Fatalf("repairVariants(%q) missing %q", tt.name, tt.want)
			}
		})
	}
}

func TestClassifyFailure(t *testing.T) {
	t.Parallel()

	model := sigma.Model{Provider: sigma.ProviderOpenCode, ID: "gpt-5.1-codex"}
	if got := classifyFailure(model, errors.New("unknown parameter: 'thinking'")); got != "sigma_request_shape" {
		t.Fatalf("unknown parameter classification = %q", got)
	}
	if got := classifyFailure(model, errors.New("model does not support image input")); got != "provider_capability_limit" {
		t.Fatalf("image classification = %q", got)
	}
	model.ID = "claude-opus-4-6"
	if got := classifyFailure(model, errors.New("No provider available")); got != "upstream_availability" {
		t.Fatalf("availability classification = %q", got)
	}
}

func TestSummaryCounts(t *testing.T) {
	t.Parallel()

	var totals summary
	for _, outcome := range []string{
		"ok",
		"skipped",
		"sigma_request_shape",
		"provider_capability_limit",
		"upstream_availability",
		"fixed_by_repair_variant",
		"other",
	} {
		totals.add(probeResult{Outcome: outcome})
	}
	if totals.Total != 7 || totals.OK != 1 || totals.Skipped != 1 ||
		totals.SigmaRequestShape != 1 || totals.ProviderCapabilityLimit != 1 ||
		totals.UpstreamAvailability != 1 || totals.FixedByRepairVariant != 1 ||
		totals.NoWorkingAttempt != 1 {
		t.Fatalf("summary = %+v", totals)
	}
}

func TestParseModelIDs(t *testing.T) {
	t.Parallel()

	ids, err := parseModelIDs([]byte(`{"data":[{"id":"b"},{"id":"a"}]}`))
	if err != nil {
		t.Fatalf("parseModelIDs returned error: %v", err)
	}
	if got, want := ids[0], "a"; got != want {
		t.Fatalf("first id = %q, want %q", got, want)
	}
	if got, want := ids[1], "b"; got != want {
		t.Fatalf("second id = %q, want %q", got, want)
	}
}

func hasRepairVariant(variants []probeCase, name string) bool {
	for _, variant := range variants {
		if variant.Name == name {
			return true
		}
	}
	return false
}
