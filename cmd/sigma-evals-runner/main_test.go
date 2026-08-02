// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"bytes"
	"context"
	"errors"
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
		"-artifact-dir", "artifacts",
		"-timeout", "30s",
	}, &bytes.Buffer{}, lookup)
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if config.selection.Provider != "openai" || config.selection.Model != "gpt-5.6-sol" ||
		config.artifactDir != "artifacts" || config.timeout != 30*time.Second {
		t.Fatalf("config = %#v", config)
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

	harness := evals.HarnessFunc[evals.SigmaInput, string]{Name: "fake"}
	cases := smokeCases(harness)
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
		if smoke.name != want[index].name || len(smoke.evaluation.Judges) != 1 ||
			smoke.evaluation.JudgeThreshold == nil || *smoke.evaluation.JudgeThreshold != 1 {
			t.Fatalf("smoke case %d = %#v", index, smoke)
		}
		judgment, err := smoke.evaluation.Judges[0].Score(context.Background(), evals.JudgmentInput[evals.SigmaInput, string]{
			Input:  smoke.evaluation.Input,
			Result: evals.RunResult[string]{Output: want[index].output},
		})
		if err != nil || judgment.Score != 1 {
			t.Fatalf("smoke case %q judgment = %#v, error = %v", smoke.name, judgment, err)
		}
	}
	if got := len(cases[len(cases)-1].evaluation.Input.Prompts); got != 2 {
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
	formatted := formatSmokeResult("factual-recall", evals.Execution[string]{
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
	})
	for _, want := range []string{
		"PASS factual-recall",
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

	formatted := formatSmokeResult("json-extraction", evals.Execution[string]{
		Result: evals.RunResult[string]{Output: strings.Repeat("x", 200)},
		Err:    errors.New(strings.Repeat("failure", 30)),
	})
	if !strings.HasPrefix(formatted, "FAIL json-extraction score=unavailable") ||
		!strings.Contains(formatted, "output=\"") ||
		!strings.Contains(formatted, "error=\"") ||
		strings.Count(formatted, "…") != 2 {
		t.Fatalf("formatted failure = %q", formatted)
	}
}

func mapEnvironment(values map[string]string) evals.EnvironmentLookup {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
