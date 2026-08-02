// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"errors"
	"os"
	"strings"

	"github.com/wintermi/sigma"
)

const (
	// ProviderEnvironmentVariable selects the default evaluation provider.
	ProviderEnvironmentVariable = "SIGMA_EVAL_PROVIDER"
	// ModelEnvironmentVariable selects the default evaluation model.
	ModelEnvironmentVariable = "SIGMA_EVAL_MODEL"
	// ArtifactDirectoryEnvironmentVariable overrides the run artifact directory.
	ArtifactDirectoryEnvironmentVariable = "SIGMA_EVAL_ARTIFACT_DIR"
)

// ModelSelection identifies one provider-specific evaluation model.
type ModelSelection struct {
	Provider sigma.ProviderID `json:"provider"`
	Model    sigma.ModelID    `json:"model"`
}

// EnvironmentLookup reads one environment variable.
type EnvironmentLookup func(string) (string, bool)

// ResolveModelSelection applies explicit-over-environment selection precedence.
// A false boolean means no default was configured.
func ResolveModelSelection(explicit *ModelSelection, lookup EnvironmentLookup) (ModelSelection, bool, error) {
	var selection ModelSelection
	if explicit != nil {
		selection = *explicit
	} else {
		if lookup == nil {
			lookup = os.LookupEnv
		}
		provider, _ := lookup(ProviderEnvironmentVariable)
		model, _ := lookup(ModelEnvironmentVariable)
		selection = ModelSelection{Provider: sigma.ProviderID(provider), Model: sigma.ModelID(model)}
	}

	selection.Provider = sigma.ProviderID(strings.TrimSpace(string(selection.Provider)))
	selection.Model = sigma.ModelID(strings.TrimSpace(string(selection.Model)))
	if selection.Provider == "" && selection.Model == "" {
		return ModelSelection{}, false, nil
	}
	if selection.Provider == "" || selection.Model == "" {
		return ModelSelection{}, false, errors.New("evals: model selection requires both provider and model")
	}
	if err := sigma.ValidateModelRef(sigma.ModelRef{Provider: selection.Provider, ID: selection.Model}); err != nil {
		return ModelSelection{}, false, err
	}
	return selection, true, nil
}

// RequireModelSelection returns an error when no default model was selected.
func RequireModelSelection(explicit *ModelSelection, lookup EnvironmentLookup) (ModelSelection, error) {
	selection, ok, err := ResolveModelSelection(explicit, lookup)
	if err != nil {
		return ModelSelection{}, err
	}
	if !ok {
		return ModelSelection{}, errors.New("evals: select a harness model explicitly or set both SIGMA_EVAL_PROVIDER and SIGMA_EVAL_MODEL")
	}
	return selection, nil
}
