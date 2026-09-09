// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestXAIJSONObjectDiagnosticControls(t *testing.T) {
	t.Parallel()
	route := routes["xai"]
	primary := findProbeCase(t, route.Cases(route, route.Model(route, "grok-4.6")), "json_object")
	variants := repairVariants(route, primary)
	if len(variants) != 7 {
		t.Fatalf("variants=%d; want availability, three format controls, and three value controls", len(variants))
	}
	for i, value := range []struct{ name, object string }{
		{"json_object_string_value", `{"ok":"ready"}`},
		{"json_object_number_value", `{"ok":1}`},
		{"json_object_false_value", `{"ok":false}`},
	} {
		variant := variants[i+4]
		if variant.Name != value.name || !reflect.DeepEqual(variant.Request, basicRequest("Return JSON exactly "+value.object+".")) {
			t.Fatalf("unexpected value control: %+v", variant)
		}
		if !reflect.DeepEqual(applyProbeOptions(variant.Options), applyProbeOptions(primary.Options)) {
			t.Fatalf("value control changed request options: %s", variant.Name)
		}
		if repairPreservesCapability(primary.Name, variant.Name) {
			t.Fatalf("value control claimed repair: %s", variant.Name)
		}
	}
	for i, name := range []string{"json_object_explicit_instruction", "json_object_schema_control", "json_object_text_control"} {
		variant := variants[i+1]
		if variant.Name != name || !reflect.DeepEqual(variant.Request.Messages, primary.Request.Messages) || len(variant.Request.Tools) != 0 {
			t.Fatalf("diagnostic changed user task: %+v", variant)
		}
		options := applyProbeOptions(variant.Options)
		if options.MaxTokens == nil || *options.MaxTokens != 256 {
			t.Fatalf("diagnostic changed output budget: %+v", options.MaxTokens)
		}
		body := options.ProviderOptions[route.Provider]["extra_body"].(map[string]any)
		format := body["response_format"].(map[string]any)
		wantType := []string{"json_object", "json_schema", "text"}[i]
		if format["type"] != wantType {
			t.Fatalf("format=%v; want %s", format, wantType)
		}
		if (variant.Request.SystemPrompt != "") != (i == 0) {
			t.Fatalf("unexpected system prompt in %s", name)
		}
		if repairPreservesCapability(primary.Name, name) {
			t.Fatalf("diagnostic %s must not claim a repair", name)
		}
	}
	if primary.Request.SystemPrompt != "" {
		t.Fatal("primary mutated")
	}
	if len(repairVariants(routes["zen"], primary)) != 1 {
		t.Fatal("xAI diagnostics escaped their route")
	}
}

func TestXAIJSONObjectControlsKeepSafetyFailure(t *testing.T) {
	t.Parallel()
	safetyErr := sigma.NewProviderError(sigma.ProviderXAI, sigma.APIOpenAICompletions, "grok-4.6", http.StatusForbidden, "safety-request", 0,
		[]byte(`{"error":"Content violates usage guidelines. Failed check: SAFETY_CHECK_TYPE_BIO"}`), nil)
	for _, allFail := range []bool{false, true} {
		t.Run(map[bool]string{false: "controls succeed", true: "controls fail"}[allFail], func(t *testing.T) {
			t.Parallel()
			control := sigmatest.Script{}
			if allFail {
				control.Err = safetyErr
			}
			primary := findProbeCase(t, routes["xai"].Cases(routes["xai"], sigma.Model{}), "json_object")
			route := openAICompatibleSigmatestProbeRoute(t, []probeCase{primary}, sigmatest.Script{Err: safetyErr}, sigmatest.Script{}, control, control, control, control, control, control)
			// The fake provider remains registered under its own ID; the route name
			// selects the real xAI diagnostic plan without accessing credentials.
			route.Name = "xai"
			results := collectProbeModel(context.Background(), route, "grok-4.6", routeCredential{apiKey: "fake"}, config{repair: true})
			if len(results) != 1 {
				t.Fatalf("results=%+v", results)
			}
			got := results[0]
			if got.Outcome != "inconclusive" || got.Error != safetyErr.Error() || !got.AvailabilityOKAfterFailure {
				t.Fatalf("lost original failure: %+v", got)
			}
			if _, ok := recommendationFor(got); ok || got.Hint != "" {
				t.Fatalf("diagnostics claimed repair: %+v", got)
			}
			if allFail {
				if len(got.FailedAttempts) != 7 || len(got.SuccessfulControls) != 0 {
					t.Fatalf("failed controls lost: %+v", got)
				}
			} else if !reflect.DeepEqual(got.SuccessfulControls, []string{"json_object_explicit_instruction", "json_object_schema_control", "json_object_text_control", "json_object_string_value", "json_object_number_value", "json_object_false_value"}) {
				t.Fatalf("successful controls=%v", got.SuccessfulControls)
			}
		})
	}
}
