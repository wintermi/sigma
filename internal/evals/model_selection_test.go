// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestResolveModelSelectionPrecedenceAndValidation(t *testing.T) {
	t.Parallel()

	lookup := func(name string) (string, bool) {
		values := map[string]string{
			ProviderEnvironmentVariable: " environment-provider ",
			ModelEnvironmentVariable:    " environment-model ",
		}
		value, ok := values[name]
		return value, ok
	}
	explicit := &ModelSelection{Provider: " explicit-provider ", Model: " explicit-model "}
	selection, ok, err := ResolveModelSelection(explicit, lookup)
	if err != nil {
		t.Fatalf("ResolveModelSelection returned error: %v", err)
	}
	if !ok || selection.Provider != "explicit-provider" || selection.Model != "explicit-model" {
		t.Fatalf("selection = %#v, %v", selection, ok)
	}

	selection, ok, err = ResolveModelSelection(nil, lookup)
	if err != nil {
		t.Fatalf("ResolveModelSelection environment returned error: %v", err)
	}
	if !ok || selection.Provider != "environment-provider" || selection.Model != "environment-model" {
		t.Fatalf("environment selection = %#v, %v", selection, ok)
	}

	_, _, err = ResolveModelSelection(&ModelSelection{Provider: sigma.ProviderOpenAI}, lookup)
	if err == nil || !strings.Contains(err.Error(), "requires both provider and model") {
		t.Fatalf("incomplete explicit selection error = %v", err)
	}
}

func TestRequireModelSelectionRejectsMissingDefaults(t *testing.T) {
	t.Parallel()

	selection, ok, err := ResolveModelSelection(nil, func(string) (string, bool) { return "", false })
	if err != nil || ok || selection != (ModelSelection{}) {
		t.Fatalf("optional selection = %#v, %v, %v", selection, ok, err)
	}
	_, err = RequireModelSelection(nil, func(string) (string, bool) { return "", false })
	if err == nil || !strings.Contains(err.Error(), ProviderEnvironmentVariable) {
		t.Fatalf("RequireModelSelection error = %v", err)
	}
}
