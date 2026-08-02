//go:build evals

// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/evals"
)

var (
	liveProvider    = flag.String("provider", "", "default provider id")
	liveModel       = flag.String("model", "", "default model id")
	liveArtifactDir = flag.String("artifact-dir", "", "exact private artifact directory")
	liveRunner      *evals.Runner
)

func TestMain(m *testing.M) {
	flag.Parse()

	runner, err := evals.NewRunner(evals.RunnerConfig{ArtifactDir: strings.TrimSpace(*liveArtifactDir)})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "sigma-evals-runner: create runner: %v\n", err)
		os.Exit(2)
	}
	liveRunner = runner
	code := m.Run()
	if err := runner.Close(os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "sigma-evals-runner: close runner: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func TestProviderFactualSmoke(t *testing.T) {
	selection, err := liveModelSelection()
	if err != nil {
		t.Fatal(err)
	}
	suite, err := newSmokeSuite(selection, os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}

	for _, smoke := range smokeCases(suite.harness) {
		t.Run(smoke.name, func(t *testing.T) {
			execution := evals.Run(t.Context(), liveRunner, t, smoke.evaluation)
			validateSmokeExecution(t, suite.model, execution)
		})
	}
}

func liveModelSelection() (evals.ModelSelection, error) {
	var explicit *evals.ModelSelection
	flag.Visit(func(item *flag.Flag) {
		if item.Name == "provider" || item.Name == "model" {
			explicit = &evals.ModelSelection{
				Provider: sigma.ProviderID(*liveProvider),
				Model:    sigma.ModelID(*liveModel),
			}
		}
	})
	selection, err := evals.RequireModelSelection(explicit, os.LookupEnv)
	if err != nil {
		return evals.ModelSelection{}, fmt.Errorf("resolve model selection: %w", err)
	}
	return selection, nil
}
