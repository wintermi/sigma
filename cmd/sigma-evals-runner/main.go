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
	"regexp"
	"strings"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/evals"
)

type runnerConfig struct {
	selection   evals.ModelSelection
	candidates  []evals.ModelSelection
	repetitions int
	runPattern  *regexp.Regexp
	artifactDir string
	timeout     time.Duration
	caseTimeout time.Duration
}

const defaultCaseTimeout = time.Minute

type candidateSelections []evals.ModelSelection

func (s *candidateSelections) String() string {
	if s == nil {
		return ""
	}
	refs := make([]string, len(*s))
	for i, selection := range *s {
		refs[i] = modelSelectionName(selection)
	}
	return strings.Join(refs, ",")
}

func (s *candidateSelections) Set(value string) error {
	selection, err := parseModelSelectionRef(value)
	if err != nil {
		return err
	}
	*s = append(*s, selection)
	return nil
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

	cases, err := filterSmokeCases(smokeCases(), config.runPattern)
	if err != nil {
		printTof(stderr, "sigma-evals-runner: %v\n", err)
		return 2
	}
	baseline, candidates, err := newSmokeSuites(config.selection, config.candidates, lookup)
	if err != nil {
		printTof(stderr, "sigma-evals-runner: %v\n", err)
		return 2
	}
	runner, err := evals.NewRunner(evals.RunnerConfig{ArtifactDir: config.artifactDir})
	if err != nil {
		printTof(stderr, "sigma-evals-runner: create runner: %v\n", err)
		return 2
	}
	printTof(stderr, "[eval] baseline=%s\n", baseline.name())
	for _, candidate := range candidates {
		printTof(stderr, "[eval] candidate=%s\n", candidate.name())
	}
	printTof(stderr, "[eval] artifacts=%s\n", runner.ArtifactDir())

	ctx, cancel := context.WithTimeout(context.Background(), config.timeout)
	summary, runErr := executeSmokeRuns(
		ctx,
		runner,
		stdout,
		stderr,
		cases,
		baseline,
		candidates,
		config.repetitions,
		config.caseTimeout,
	)
	cancel()
	if runErr != nil {
		printTof(stderr, "sigma-evals-runner: plan smoke runs: %v\n", runErr)
		return 2
	}
	printTof(stdout, "%s\n", summary.String())
	if err := runner.Close(stdout); err != nil {
		printTof(stderr, "[eval] error=close runner: %v\n", err)
		return 1
	}
	if summary.Failed {
		return 1
	}
	return 0
}

func parseConfig(args []string, stderr io.Writer, lookup evals.EnvironmentLookup) (runnerConfig, error) {
	flags := flag.NewFlagSet("sigma-evals-runner", flag.ContinueOnError)
	flags.SetOutput(stderr)
	provider := flags.String("provider", "", "baseline provider id")
	model := flags.String("model", "", "baseline model id")
	var candidates candidateSelections
	flags.Var(&candidates, "candidate", "candidate provider/model reference; may be repeated")
	repetitions := flags.Int("repetitions", 1, "comparison repetitions")
	runPattern := flags.String("run", "", "regular expression selecting smoke case names")
	artifactDirectory := flags.String("artifact-dir", "", "exact private artifact directory")
	timeout := flags.Duration("timeout", 5*time.Minute, "overall evaluation timeout")
	caseTimeout := flags.Duration(
		"case-timeout",
		defaultCaseTimeout,
		"maximum duration for one evaluation; 0 uses only the overall timeout",
	)
	if err := flags.Parse(args); err != nil {
		return runnerConfig{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return runnerConfig{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *timeout <= 0 {
		return runnerConfig{}, errors.New("timeout must be positive")
	}
	if *caseTimeout < 0 {
		return runnerConfig{}, errors.New("case timeout must not be negative")
	}
	if *repetitions <= 0 {
		return runnerConfig{}, errors.New("repetitions must be positive")
	}
	if len(candidates) == 0 && *repetitions != 1 {
		return runnerConfig{}, errors.New("repetitions require at least one candidate")
	}
	compiledRunPattern, err := regexp.Compile(*runPattern)
	if err != nil {
		return runnerConfig{}, fmt.Errorf("compile -run expression: %w", err)
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
	if err := validateCandidateSelections(selection, candidates); err != nil {
		return runnerConfig{}, err
	}
	return runnerConfig{
		selection:   selection,
		candidates:  append([]evals.ModelSelection(nil), candidates...),
		repetitions: *repetitions,
		runPattern:  compiledRunPattern,
		artifactDir: strings.TrimSpace(*artifactDirectory),
		timeout:     *timeout,
		caseTimeout: *caseTimeout,
	}, nil
}

func parseModelSelectionRef(value string) (evals.ModelSelection, error) {
	value = strings.TrimSpace(value)
	provider, model, ok := strings.Cut(value, "/")
	selection := evals.ModelSelection{
		Provider: sigma.ProviderID(strings.TrimSpace(provider)),
		Model:    sigma.ModelID(strings.TrimSpace(model)),
	}
	if !ok || selection.Provider == "" || selection.Model == "" {
		return evals.ModelSelection{}, fmt.Errorf("candidate %q must be a provider/model reference", value)
	}
	if err := sigma.ValidateModelRef(sigma.ModelRef{Provider: selection.Provider, ID: selection.Model}); err != nil {
		return evals.ModelSelection{}, fmt.Errorf("candidate %q: %w", value, err)
	}
	return selection, nil
}

func validateCandidateSelections(baseline evals.ModelSelection, candidates []evals.ModelSelection) error {
	seen := map[string]struct{}{modelSelectionName(baseline): {}}
	for _, candidate := range candidates {
		name := modelSelectionName(candidate)
		if _, ok := seen[name]; ok {
			return fmt.Errorf("model selection %q is duplicated", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func modelSelectionName(selection evals.ModelSelection) string {
	return string(selection.Provider) + "/" + string(selection.Model)
}

func printTof(output io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(output, format, args...)
}
