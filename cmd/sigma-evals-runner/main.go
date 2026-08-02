// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/evals"
)

type runnerConfig struct {
	selection   evals.ModelSelection
	artifactDir string
	timeout     time.Duration
}

type commandTest struct {
	name     string
	failed   bool
	errors   []string
	cleanups []func()
}

func (t *commandTest) Cleanup(cleanup func()) {
	t.cleanups = append(t.cleanups, cleanup)
}

func (t *commandTest) Errorf(format string, args ...any) {
	t.failed = true
	t.errors = append(t.errors, fmt.Sprintf(format, args...))
}

func (t *commandTest) Failed() bool {
	return t.failed
}

func (*commandTest) Helper() {}

func (t *commandTest) Name() string {
	return t.name
}

func (*commandTest) Skipped() bool {
	return false
}

func (t *commandTest) finish() {
	for i := len(t.cleanups) - 1; i >= 0; i-- {
		t.cleanups[i]()
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.LookupEnv))
}

func run(args []string, stdout, stderr io.Writer, lookup evals.EnvironmentLookup) int {
	config, err := parseConfig(args, stderr, lookup)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		printTof(stderr, "sigma-evals-runner: %v\n", err)
		return 2
	}

	suite, err := newSmokeSuite(config.selection, lookup)
	if err != nil {
		printTof(stderr, "sigma-evals-runner: %v\n", err)
		return 2
	}
	runner, err := evals.NewRunner(evals.RunnerConfig{ArtifactDir: config.artifactDir})
	if err != nil {
		printTof(stderr, "sigma-evals-runner: create runner: %v\n", err)
		return 2
	}
	printTof(stderr, "[eval] model=%s/%s\n", suite.model.Provider, suite.model.ID)
	printTof(stderr, "[eval] artifacts=%s\n", runner.ArtifactDir())

	ctx, cancel := context.WithTimeout(context.Background(), config.timeout)
	var tests []*commandTest
	for _, smoke := range smokeCases(suite.harness) {
		test := &commandTest{name: "Provider smoke/" + smoke.name}
		execution := evals.Run(ctx, runner, test, smoke.evaluation)
		validateSmokeExecution(test, suite.model, execution)
		test.finish()
		tests = append(tests, test)
	}
	cancel()
	if err := runner.Close(stdout); err != nil {
		printTof(stderr, "[eval] error=close runner: %v\n", err)
		return 1
	}
	failed := false
	for _, test := range tests {
		for _, message := range test.errors {
			printTof(stderr, "[eval] test=%s error=%s\n", test.name, message)
		}
		failed = failed || test.Failed()
	}
	if failed {
		return 1
	}
	return 0
}

func parseConfig(args []string, stderr io.Writer, lookup evals.EnvironmentLookup) (runnerConfig, error) {
	flags := flag.NewFlagSet("sigma-evals-runner", flag.ContinueOnError)
	flags.SetOutput(stderr)
	provider := flags.String("provider", "", "default provider id")
	model := flags.String("model", "", "default model id")
	artifactDirectory := flags.String("artifact-dir", "", "exact private artifact directory")
	timeout := flags.Duration("timeout", 5*time.Minute, "overall evaluation timeout")
	if err := flags.Parse(args); err != nil {
		return runnerConfig{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return runnerConfig{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *timeout <= 0 {
		return runnerConfig{}, errors.New("timeout must be positive")
	}

	var explicit *evals.ModelSelection
	flags.Visit(func(item *flag.Flag) {
		if item.Name == "provider" || item.Name == "model" {
			explicit = &evals.ModelSelection{
				Provider: sigma.ProviderID(*provider),
				Model:    sigma.ModelID(*model),
			}
		}
	})
	selection, err := evals.RequireModelSelection(explicit, lookup)
	if err != nil {
		return runnerConfig{}, fmt.Errorf("resolve model selection: %w", err)
	}
	return runnerConfig{
		selection:   selection,
		artifactDir: strings.TrimSpace(*artifactDirectory),
		timeout:     *timeout,
	}, nil
}

func printTof(output io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(output, format, args...)
}
