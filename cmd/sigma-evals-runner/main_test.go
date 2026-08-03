// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/evals"
)

func TestParseConfigUsesCLISelectionOverEnvironment(t *testing.T) {
	t.Parallel()

	lookup := func(name string) (string, bool) {
		values := map[string]string{
			evals.ProviderEnvironmentVariable: "environment-provider",
			evals.ModelEnvironmentVariable:    "environment-model",
		}
		value, ok := values[name]
		return value, ok
	}
	config, err := parseConfig([]string{
		"-provider", "openai",
		"-model", "gpt-5.6-sol",
		"-candidate", " opencode-go/kimi-k3 ",
		"-candidate", "fireworks/accounts/fireworks/models/kimi-k3",
		"-repetitions", "3",
		"-run", "factual|multi-turn",
		"-artifact-dir", "artifacts",
		"-timeout", "30s",
		"-case-timeout", "20s",
	}, &bytes.Buffer{}, lookup)
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if config.selection.Provider != "openai" || config.selection.Model != "gpt-5.6-sol" ||
		len(config.candidates) != 2 || config.candidates[0].Provider != "opencode-go" ||
		config.candidates[1].Model != "accounts/fireworks/models/kimi-k3" ||
		config.repetitions != 3 || !config.runPattern.MatchString("factual-recall") ||
		config.artifactDir != "artifacts" || config.timeout != 30*time.Second ||
		config.caseTimeout != 20*time.Second {
		t.Fatalf("config = %#v", config)
	}
}

func TestParseConfigCaseTimeoutDefaultsAndAllowsZero(t *testing.T) {
	t.Parallel()

	lookup := func(string) (string, bool) { return "", false }
	tests := []struct {
		name string
		args []string
		want time.Duration
	}{
		{name: "default", want: defaultCaseTimeout},
		{name: "disabled", args: []string{"-case-timeout", "0"}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := append([]string{"-provider", "openai", "-model", "model"}, tt.args...)
			config, err := parseConfig(args, &bytes.Buffer{}, lookup)
			if err != nil {
				t.Fatalf("parseConfig returned error: %v", err)
			}
			if config.caseTimeout != tt.want {
				t.Fatalf("case timeout = %s, want %s", config.caseTimeout, tt.want)
			}
		})
	}
}

func TestParseConfigRequiresCompleteSelectionAndPositiveTimeout(t *testing.T) {
	t.Parallel()

	lookup := func(string) (string, bool) { return "", false }
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing selection", want: evals.ProviderEnvironmentVariable},
		{name: "partial selection", args: []string{"-provider", "openai"}, want: "requires both"},
		{name: "timeout", args: []string{"-provider", "openai", "-model", "model", "-timeout", "0s"}, want: "positive"},
		{name: "negative case timeout", args: []string{"-provider", "openai", "-model", "model", "-case-timeout", "-1s"}, want: "must not be negative"},
		{name: "repetitions", args: []string{"-provider", "openai", "-model", "model", "-repetitions", "0"}, want: "positive"},
		{name: "repetitions without candidate", args: []string{"-provider", "openai", "-model", "model", "-repetitions", "2"}, want: "candidate"},
		{name: "invalid run expression", args: []string{"-provider", "openai", "-model", "model", "-run", "["}, want: "compile -run"},
		{name: "malformed candidate", args: []string{"-provider", "openai", "-model", "model", "-candidate", "missing-model"}, want: "provider/model"},
		{name: "baseline candidate", args: []string{"-provider", "openai", "-model", "model", "-candidate", "openai/model"}, want: "duplicated"},
		{name: "duplicate candidate", args: []string{"-provider", "openai", "-model", "model", "-candidate", "other/one", "-candidate", "other/one"}, want: "duplicated"},
		{name: "extra argument", args: []string{"-provider", "openai", "-model", "model", "extra"}, want: "unexpected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseConfig(tt.args, &bytes.Buffer{}, lookup)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseConfig error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseModelSelectionRefPreservesSlashContainingModelID(t *testing.T) {
	t.Parallel()

	selection, err := parseModelSelectionRef(" fireworks/accounts/fireworks/models/kimi-k3 ")
	if err != nil {
		t.Fatalf("parseModelSelectionRef returned error: %v", err)
	}
	if selection.Provider != "fireworks" || selection.Model != "accounts/fireworks/models/kimi-k3" {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestFilterSmokeCasesPreservesDeclarationOrder(t *testing.T) {
	t.Parallel()

	selected, err := filterSmokeCases(smokeCases(), regexp.MustCompile("recall"))
	if err != nil {
		t.Fatalf("filterSmokeCases returned error: %v", err)
	}
	if len(selected) != 2 || selected[0].name != "factual-recall" || selected[1].name != "multi-turn-recall" {
		t.Fatalf("selected cases = %#v", selected)
	}
	if _, err := filterSmokeCases(smokeCases(), regexp.MustCompile("not-a-case")); err == nil {
		t.Fatal("filterSmokeCases accepted an expression with no matches")
	}
}

func TestRunRejectsUnsupportedSuiteModelBeforeNetwork(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exitCode := run(
		[]string{"-provider", "anthropic", "-model", "claude-opus-5"},
		&bytes.Buffer{},
		&stderr,
		func(string) (string, bool) { return "", false },
	)
	if exitCode != 2 || !strings.Contains(stderr.String(), "provider \"anthropic\" is unsupported") {
		t.Fatalf("run exit = %d, stderr = %q", exitCode, stderr.String())
	}
}

func TestRunRejectsUnsupportedCandidateBeforeNetwork(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exitCode := run(
		[]string{
			"-provider", "openai",
			"-model", "gpt-5.6-sol",
			"-candidate", "openai/not-a-catalog-model",
		},
		&bytes.Buffer{},
		&stderr,
		func(string) (string, bool) { return "", false },
	)
	if exitCode != 2 || !strings.Contains(stderr.String(), "candidate openai/not-a-catalog-model: model not found") {
		t.Fatalf("run exit = %d, stderr = %q", exitCode, stderr.String())
	}
}

func TestNewSmokeSuiteUsesFullModelReferenceAsHarnessName(t *testing.T) {
	t.Parallel()

	suite, err := newSmokeSuite(evals.ModelSelection{
		Provider: sigma.ProviderOpenAI,
		Model:    "gpt-5.6-sol",
	}, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("newSmokeSuite returned error: %v", err)
	}
	if suite.name() != "openai/gpt-5.6-sol" || suite.harness.HarnessName() != suite.name() {
		t.Fatalf("suite name = %q, harness name = %q", suite.name(), suite.harness.HarnessName())
	}
}

func TestRegisterSmokeProviderSupportsConfiguredRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		provider    sigma.ProviderID
		model       sigma.ModelID
		providerAPI sigma.API
	}{
		{name: "OpenAI Responses", provider: sigma.ProviderOpenAI, model: "gpt-5.6-sol", providerAPI: sigma.APIOpenAIResponses},
		{name: "OpenCode Go", provider: sigma.ProviderOpenCodeGo, model: "kimi-k3", providerAPI: sigma.APIOpenAICompletions},
		{name: "Fireworks", provider: sigma.ProviderFireworks, model: "accounts/fireworks/models/kimi-k3", providerAPI: sigma.APIOpenAICompletions},
		{name: "Fireworks Anthropic", provider: sigma.ProviderFireworksAnthropic, model: "accounts/fireworks/models/deepseek-v4-flash", providerAPI: sigma.APIAnthropicMessages},
		{name: "Google Vertex", provider: sigma.ProviderGoogleVertex, model: "gemini-2.5-flash", providerAPI: sigma.APIGoogleVertex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := sigma.DefaultRegistry()
			model, ok := registry.Model(tt.provider, tt.model)
			if !ok {
				t.Fatalf("catalog model %s/%s not found", tt.provider, tt.model)
			}
			lookup := mapEnvironment(map[string]string{
				"GOOGLE_CLOUD_PROJECT":      "project",
				"GOOGLE_CLOUD_LOCATION":     "global",
				"GOOGLE_CLOUD_ACCESS_TOKEN": "token",
			})
			if _, err := registerSmokeProvider(registry, model, lookup); err != nil {
				t.Fatalf("registerSmokeProvider returned error: %v", err)
			}
			provider, ok := registry.TextProvider(tt.provider)
			if !ok || provider.API() != tt.providerAPI {
				t.Fatalf("registered provider = %#v, want API %q", provider, tt.providerAPI)
			}
		})
	}
}

func TestVertexEvalConfigSupportsOAuthAndAPIKeyEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		env       map[string]string
		wantType  sigma.CredentialType
		wantValue string
	}{
		{
			name: "OAuth access token",
			env: map[string]string{
				"GOOGLE_CLOUD_PROJECT":      "project",
				"GOOGLE_CLOUD_LOCATION":     "global",
				"GOOGLE_CLOUD_ACCESS_TOKEN": "token",
			},
			wantType:  sigma.CredentialTypeOAuthToken,
			wantValue: "token",
		},
		{
			name: "API key fallbacks",
			env: map[string]string{
				"GCLOUD_PROJECT":       "project",
				"GOOGLE_CLOUD_REGION":  "us-central1",
				"GOOGLE_CLOUD_API_KEY": "key",
			},
			wantType:  sigma.CredentialTypeAPIKey,
			wantValue: "key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config, resolver, err := vertexEvalConfig(mapEnvironment(tt.env))
			if err != nil {
				t.Fatalf("vertexEvalConfig returned error: %v", err)
			}
			if config.ProjectID != "project" || config.Location == "" {
				t.Fatalf("Vertex config = %#v", config)
			}
			credential, err := resolver.Resolve(context.Background(), sigma.Model{
				Provider: sigma.ProviderGoogleVertex,
				ID:       "gemini-2.5-flash",
			}, sigma.Options{})
			if err != nil {
				t.Fatalf("resolve credential: %v", err)
			}
			if credential.Type != tt.wantType || credential.Value != tt.wantValue {
				t.Fatalf("credential = %s, want %s", credential, tt.wantType)
			}
		})
	}
}

func TestVertexEvalConfigRequiresRoutingAndAuthentication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "project", env: map[string]string{"GOOGLE_CLOUD_LOCATION": "global", "GOOGLE_API_KEY": "key"}, want: "PROJECT"},
		{name: "location", env: map[string]string{"GOOGLE_CLOUD_PROJECT": "project", "GOOGLE_API_KEY": "key"}, want: "LOCATION"},
		{name: "authentication", env: map[string]string{"GOOGLE_CLOUD_PROJECT": "project", "GOOGLE_CLOUD_LOCATION": "global"}, want: "ACCESS_TOKEN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := vertexEvalConfig(mapEnvironment(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("vertexEvalConfig error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSmokeCasesUseDeterministicLocalJudges(t *testing.T) {
	t.Parallel()

	cases := smokeCases()
	want := []struct {
		name   string
		output string
	}{
		{name: "factual-recall", output: "paris"},
		{name: "arithmetic", output: "391"},
		{name: "exact-formatting", output: "red,green,blue"},
		{name: "json-extraction", output: `{"count":7,"name":"Ada"}`},
		{name: "multi-turn-recall", output: "cedar"},
	}
	if len(cases) != len(want) {
		t.Fatalf("smoke case count = %d, want %d", len(cases), len(want))
	}
	for index, smoke := range cases {
		if smoke.name != want[index].name {
			t.Fatalf("smoke case %d = %#v", index, smoke)
		}
		judgment, err := smoke.judge.Score(context.Background(), evals.JudgmentInput[evals.SigmaInput, string]{
			Input:  smoke.input,
			Result: evals.RunResult[string]{Output: want[index].output},
		})
		if err != nil || judgment.Score != 1 {
			t.Fatalf("smoke case %q judgment = %#v, error = %v", smoke.name, judgment, err)
		}
	}
	if got := len(cases[len(cases)-1].input.Prompts); got != 2 {
		t.Fatalf("multi-turn prompt count = %d, want 2", got)
	}
}

func TestSmokeJudgesRejectMalformedOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		judge  evals.Judge[evals.SigmaInput, string]
		output string
	}{
		{name: "case-sensitive formatting", judge: exactJudge("red,green,blue"), output: "RED,GREEN,BLUE"},
		{name: "invalid JSON", judge: jsonExtractionJudge(), output: `{"name":"Ada","count":7} trailing`},
		{name: "unknown JSON field", judge: jsonExtractionJudge(), output: `{"name":"Ada","count":7,"extra":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			judgment, err := tt.judge.Score(context.Background(), evals.JudgmentInput[evals.SigmaInput, string]{
				Result: evals.RunResult[string]{Output: tt.output},
			})
			if err != nil || judgment.Score != 0 || judgment.Reason == "" {
				t.Fatalf("judgment = %#v, error = %v", judgment, err)
			}
		})
	}
}

func TestFormatSmokeResultReportsOutcomeAndTelemetry(t *testing.T) {
	t.Parallel()

	cost := 0.000123
	score := 1.0
	formatted := formatSmokeResult("baseline", "openai/gpt-5.6-sol", "factual-recall", 2, evals.Execution[string]{
		Result: evals.RunResult[string]{
			Output: "Paris",
			Usage: evals.Usage{
				InputTokens:      10,
				OutputTokens:     2,
				TotalTokens:      12,
				EstimatedCostUSD: &cost,
			},
			Timings: evals.Timings{Total: 1500 * time.Millisecond},
		},
		AverageScore: &score,
	}, nil)
	for _, want := range []string{
		"PASS role=baseline",
		"harness=openai/gpt-5.6-sol",
		"case=factual-recall",
		"repetition=2",
		"score=1.00",
		"tokens=12(in=10,out=2)",
		"latency=1.5s",
		"cost=$0.000123",
		`output="Paris"`,
	} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted result %q missing %q", formatted, want)
		}
	}
}

func TestFormatSmokeResultBoundsFailures(t *testing.T) {
	t.Parallel()

	formatted := formatSmokeResult("candidate", "provider/model", "json-extraction", 1, evals.Execution[string]{
		Result: evals.RunResult[string]{Output: strings.Repeat("x", 200)},
		Err:    errors.New(strings.Repeat("failure", 30)),
	}, nil)
	if !strings.HasPrefix(formatted, "FAIL role=candidate harness=provider/model case=json-extraction repetition=1 score=unavailable") ||
		!strings.Contains(formatted, "output=\"") ||
		!strings.Contains(formatted, "error=\"") ||
		strings.Count(formatted, "…") != 2 {
		t.Fatalf("formatted failure = %q", formatted)
	}
}

func TestExecuteSmokeRunsProducesPairedObservationsWithoutFailingOnLowScores(t *testing.T) {
	t.Parallel()

	runner, err := evals.NewRunner(evals.RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	baseline := fakeSmokeSuite("baseline/model", "expected", nil)
	candidate := fakeSmokeSuite("candidate/model", "wrong", nil)
	cases := []smokeCase{newSmokeCase("comparison", evals.Prompt("prompt"), exactJudge("expected"))}
	var stdout, stderr bytes.Buffer
	summary, err := executeSmokeRuns(
		context.Background(),
		runner,
		&stdout,
		&stderr,
		cases,
		baseline,
		[]smokeSuite{candidate},
		2,
		0,
	)
	if err != nil {
		t.Fatalf("executeSmokeRuns returned error: %v", err)
	}
	if summary.Runs != 4 || summary.Correct != 2 || summary.OperationalFailures != 0 || summary.Failed {
		t.Fatalf("summary = %#v", summary)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 4 ||
		!strings.Contains(lines[0], "role=baseline harness=baseline/model case=comparison repetition=1") ||
		!strings.Contains(lines[1], "role=candidate harness=candidate/model case=comparison repetition=1") ||
		!strings.Contains(lines[2], "role=baseline harness=baseline/model case=comparison repetition=2") ||
		!strings.Contains(lines[3], "role=candidate harness=candidate/model case=comparison repetition=2") {
		t.Fatalf("result lines = %q", lines)
	}
	var report bytes.Buffer
	if err := runner.Close(&report); err != nil {
		t.Fatalf("Runner.Close returned error: %v", err)
	}
	for _, want := range []string{
		"Eval Comparisons",
		"Baseline  baseline/model",
		"Candidate  candidate/model (2/2 pairs)",
		"Pass rate  -100.0 pp",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("comparison report %q missing %q", report.String(), want)
		}
	}
}

func TestExecuteSmokeRunsSingleModelFailsHardAndReportsCleanupErrorsBeforeResult(t *testing.T) {
	t.Parallel()

	runner, err := evals.NewRunner(evals.RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	if err := runner.Close(io.Discard); err != nil {
		t.Fatalf("Runner.Close returned error: %v", err)
	}
	baseline := fakeSmokeSuite("baseline/model", "wrong", nil)
	cases := []smokeCase{newSmokeCase("single", evals.Prompt("prompt"), exactJudge("expected"))}
	var stdout, stderr bytes.Buffer
	summary, err := executeSmokeRuns(
		context.Background(),
		runner,
		&stdout,
		&stderr,
		cases,
		baseline,
		nil,
		1,
		0,
	)
	if err != nil {
		t.Fatalf("executeSmokeRuns returned error: %v", err)
	}
	if summary.Runs != 1 || summary.Correct != 0 || summary.OperationalFailures != 1 || !summary.Failed {
		t.Fatalf("summary = %#v", summary)
	}
	if !strings.Contains(stdout.String(), "FAIL role=baseline") ||
		!strings.Contains(stdout.String(), "error=\"eval artifact recording failed: evals: runner is closed\"") ||
		!strings.Contains(stderr.String(), "eval average score") ||
		!strings.Contains(stderr.String(), "runner is closed") {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestExecuteSmokeRunsComparisonFailsOnHarnessErrors(t *testing.T) {
	t.Parallel()

	runner, err := evals.NewRunner(evals.RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	baseline := fakeSmokeSuite("baseline/model", "expected", nil)
	candidate := fakeSmokeSuite("candidate/model", "partial", errors.New("provider unavailable"))
	cases := []smokeCase{newSmokeCase("comparison", evals.Prompt("prompt"), exactJudge("expected"))}
	var stdout, stderr bytes.Buffer
	summary, err := executeSmokeRuns(
		context.Background(),
		runner,
		&stdout,
		&stderr,
		cases,
		baseline,
		[]smokeSuite{candidate},
		1,
		0,
	)
	if err != nil {
		t.Fatalf("executeSmokeRuns returned error: %v", err)
	}
	if summary.Runs != 2 || summary.Correct != 1 || summary.OperationalFailures != 1 || !summary.Failed {
		t.Fatalf("summary = %#v", summary)
	}
	if !strings.Contains(stdout.String(), "FAIL role=candidate") ||
		!strings.Contains(stdout.String(), `error="evals: harness \"candidate/model\": provider unavailable"`) ||
		!strings.Contains(stderr.String(), "provider unavailable") {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestExecuteSmokeRunsUsesIndependentCaseTimeouts(t *testing.T) {
	t.Parallel()

	artifactDir := t.TempDir()
	runner, err := evals.NewRunner(evals.RunnerConfig{ArtifactDir: artifactDir})
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	baseline := fakeSmokeSuite("baseline/model", "expected", nil)
	candidate := smokeSuite{
		model: sigma.Model{Provider: "candidate", ID: "model"},
		harness: evals.HarnessFunc[evals.SigmaInput, string]{
			Name: "candidate/model",
			Func: func(ctx context.Context, input evals.SigmaInput, run *evals.RunContext) (evals.RunResult[string], error) {
				result := evals.RunResult[string]{
					Output: "expected",
					Usage: evals.Usage{
						Provider:    "candidate",
						Model:       "model",
						InputTokens: 4,
						TotalTokens: 5,
					},
				}
				if input.Prompts[0] != "timeout" {
					return result, nil
				}
				if err := run.Attach(evals.AttachmentFile, "partial.txt", "text/plain", []byte("partial")); err != nil {
					return result, err
				}
				<-ctx.Done()
				return result, ctx.Err()
			},
		},
	}
	cases := []smokeCase{
		newSmokeCase("timeout", evals.Prompt("timeout"), exactJudge("expected")),
		newSmokeCase("following", evals.Prompt("following"), exactJudge("expected")),
	}
	var stdout, stderr bytes.Buffer
	summary, err := executeSmokeRuns(
		context.Background(),
		runner,
		&stdout,
		&stderr,
		cases,
		baseline,
		[]smokeSuite{candidate},
		1,
		20*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("executeSmokeRuns returned error: %v", err)
	}
	if summary.Runs != 4 || summary.Correct != 3 || summary.OperationalFailures != 1 || !summary.Failed {
		t.Fatalf("summary = %#v", summary)
	}
	if !strings.Contains(stdout.String(), "FAIL role=candidate harness=candidate/model case=timeout") ||
		!strings.Contains(stdout.String(), "PASS role=candidate harness=candidate/model case=following") ||
		!strings.Contains(stderr.String(), "context deadline exceeded") {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}

	var report bytes.Buffer
	if err := runner.Close(&report); err != nil {
		t.Fatalf("Runner.Close returned error: %v", err)
	}
	if !strings.Contains(report.String(), "Candidate  candidate/model (1/2 pairs)") ||
		!strings.Contains(report.String(), "errored-observation") {
		t.Fatalf("comparison report = %q", report.String())
	}
	records, err := os.ReadFile(filepath.Join(artifactDir, "runs.jsonl"))
	if err != nil {
		t.Fatalf("read runs.jsonl: %v", err)
	}
	if !strings.Contains(string(records), "partial.txt") ||
		!strings.Contains(string(records), "context deadline exceeded") {
		t.Fatalf("runs.jsonl = %q", records)
	}
}

func TestExecuteSmokeRunsZeroCaseTimeoutUsesParentContext(t *testing.T) {
	t.Parallel()

	runner, err := evals.NewRunner(evals.RunnerConfig{ArtifactDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	baseline := smokeSuite{
		model: sigma.Model{Provider: "baseline", ID: "model"},
		harness: evals.HarnessFunc[evals.SigmaInput, string]{
			Name: "baseline/model",
			Func: func(ctx context.Context, _ evals.SigmaInput, _ *evals.RunContext) (evals.RunResult[string], error) {
				if _, ok := ctx.Deadline(); ok {
					return evals.RunResult[string]{}, errors.New("unexpected per-case deadline")
				}
				return evals.RunResult[string]{
					Output: "expected",
					Usage:  evals.Usage{Provider: "baseline", Model: "model", TotalTokens: 1},
				}, nil
			},
		},
	}
	summary, err := executeSmokeRuns(
		context.Background(),
		runner,
		io.Discard,
		io.Discard,
		[]smokeCase{newSmokeCase("zero-timeout", evals.Prompt("prompt"), exactJudge("expected"))},
		baseline,
		nil,
		1,
		0,
	)
	if err != nil {
		t.Fatalf("executeSmokeRuns returned error: %v", err)
	}
	if summary.Runs != 1 || summary.Correct != 1 || summary.OperationalFailures != 0 || summary.Failed {
		t.Fatalf("summary = %#v", summary)
	}
}

func fakeSmokeSuite(name, output string, runErr error) smokeSuite {
	provider, model, _ := strings.Cut(name, "/")
	return smokeSuite{
		model: sigma.Model{Provider: sigma.ProviderID(provider), ID: sigma.ModelID(model)},
		harness: evals.HarnessFunc[evals.SigmaInput, string]{
			Name: name,
			Func: func(context.Context, evals.SigmaInput, *evals.RunContext) (evals.RunResult[string], error) {
				return evals.RunResult[string]{
					Output: output,
					Usage: evals.Usage{
						Provider:    provider,
						Model:       model,
						InputTokens: 4,
						TotalTokens: 5,
					},
					Timings: evals.Timings{Total: time.Millisecond},
				}, runErr
			},
		},
	}
}

func mapEnvironment(values map[string]string) evals.EnvironmentLookup {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
