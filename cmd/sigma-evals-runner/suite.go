// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/evals"
	"github.com/wintermi/sigma/provider/fireworks"
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/provider/opencode"
)

type smokeSuite struct {
	model       sigma.Model
	textHarness evals.Harness[evals.SigmaInput, string]
	toolHarness evals.Harness[evals.SigmaInput, string]
}

func (s smokeSuite) name() string {
	return string(s.model.Provider) + "/" + string(s.model.ID)
}

type smokeHarnessKind string

const (
	smokeHarnessText smokeHarnessKind = "text"
	smokeHarnessTool smokeHarnessKind = "tool"
)

func (s smokeSuite) harness(kind smokeHarnessKind) evals.Harness[evals.SigmaInput, string] {
	if kind == smokeHarnessTool {
		return s.toolHarness
	}
	return s.textHarness
}

type smokeCase struct {
	name        string
	harnessKind smokeHarnessKind
	input       evals.SigmaInput
	judge       evals.Judge[evals.SigmaInput, string]
}

type smokeRunSummary struct {
	Runs                int
	Correct             int
	OperationalFailures int
	Failed              bool
}

func (s smokeRunSummary) String() string {
	return fmt.Sprintf(
		"[eval] summary runs=%d correct=%d incorrect=%d operational-failures=%d",
		s.Runs,
		s.Correct,
		s.Runs-s.Correct,
		s.OperationalFailures,
	)
}

type smokeTest interface {
	Errorf(string, ...any)
}

type extractedRecord struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

const (
	smokeLookupToolName = "lookup_sigma_eval_value"
	smokeLookupKey      = "round-trip"
	smokeLookupValue    = "violet-echo-731"
)

func newSmokeSuite(selection evals.ModelSelection, lookup evals.EnvironmentLookup) (smokeSuite, error) {
	registry := sigma.DefaultRegistry()
	model, ok := registry.Model(selection.Provider, selection.Model)
	if !ok {
		return smokeSuite{}, fmt.Errorf("model not found: %s/%s", selection.Provider, selection.Model)
	}
	if err := validateSmokeModel(model); err != nil {
		return smokeSuite{}, err
	}
	resolver, err := registerSmokeProvider(registry, model, lookup)
	if err != nil {
		return smokeSuite{}, err
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(resolver),
	)
	name := string(model.Provider) + "/" + string(model.ID)
	textHarness, err := evals.NewSigmaTextHarness(evals.SigmaHarnessConfig{
		Name:   string(model.Provider) + "/" + string(model.ID),
		Client: client,
		Model:  model,
	})
	if err != nil {
		return smokeSuite{}, fmt.Errorf("create text harness: %w", err)
	}
	tool := smokeLookupTool()
	toolHarness, err := evals.NewSigmaTextHarness(evals.SigmaHarnessConfig{
		Name:         name,
		Client:       client,
		Model:        model,
		BaseRequest:  sigma.Request{Tools: []sigma.Tool{tool}},
		ToolExecutor: smokeLookupExecutor(tool),
	})
	if err != nil {
		return smokeSuite{}, fmt.Errorf("create tool harness: %w", err)
	}
	return smokeSuite{model: model, textHarness: textHarness, toolHarness: toolHarness}, nil
}

func validateSmokeModel(model sigma.Model) error {
	if !model.SupportsTools {
		return fmt.Errorf("model %s/%s does not support tools required by the evaluation suite", model.Provider, model.ID)
	}
	return nil
}

func newSmokeSuites(
	baseline evals.ModelSelection,
	candidateSelections []evals.ModelSelection,
	lookup evals.EnvironmentLookup,
) (smokeSuite, []smokeSuite, error) {
	baselineSuite, err := newSmokeSuite(baseline, lookup)
	if err != nil {
		return smokeSuite{}, nil, fmt.Errorf("baseline: %w", err)
	}
	candidates := make([]smokeSuite, 0, len(candidateSelections))
	for _, selection := range candidateSelections {
		candidate, candidateErr := newSmokeSuite(selection, lookup)
		if candidateErr != nil {
			return smokeSuite{}, nil, fmt.Errorf("candidate %s: %w", modelSelectionName(selection), candidateErr)
		}
		candidates = append(candidates, candidate)
	}
	return baselineSuite, candidates, nil
}

func registerSmokeProvider(
	registry *sigma.Registry,
	model sigma.Model,
	lookup evals.EnvironmentLookup,
) (sigma.AuthResolver, error) {
	if registry == nil {
		return nil, errors.New("registry is required")
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}
	var resolver sigma.AuthResolver = sigma.EnvironmentAuthResolver{LookupEnv: lookup}
	var err error
	switch model.Provider {
	case sigma.ProviderOpenAI:
		if model.API != sigma.APIOpenAIResponses {
			return nil, unsupportedModelAPI(model, sigma.APIOpenAIResponses)
		}
		err = openai.RegisterResponses(registry, sigma.ProviderOpenAI)
	case sigma.ProviderOpenCodeGo:
		err = opencode.RegisterGo(registry)
	case sigma.ProviderFireworks:
		if model.API != sigma.APIOpenAICompletions {
			return nil, unsupportedModelAPI(model, sigma.APIOpenAICompletions)
		}
		err = fireworks.Register(registry)
	case sigma.ProviderFireworksAnthropic:
		if model.API != sigma.APIAnthropicMessages {
			return nil, unsupportedModelAPI(model, sigma.APIAnthropicMessages)
		}
		err = fireworks.RegisterAnthropic(registry)
	case sigma.ProviderGoogleVertex:
		config, authResolver, configErr := vertexEvalConfig(lookup)
		if configErr != nil {
			return nil, configErr
		}
		if model.API != sigma.APIGoogleVertex {
			return nil, unsupportedModelAPI(model, sigma.APIGoogleVertex)
		}
		resolver = authResolver
		err = google.RegisterVertex(
			registry,
			sigma.ProviderGoogleVertex,
			google.WithVertexConfig(config),
		)
	default:
		return nil, fmt.Errorf(
			"provider %q is unsupported; supported providers are openai, opencode-go, fireworks, fireworks-anthropic, and google-vertex",
			model.Provider,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("register %s provider: %w", model.Provider, err)
	}
	return resolver, nil
}

func vertexEvalConfig(lookup evals.EnvironmentLookup) (google.VertexConfig, sigma.AuthResolver, error) {
	projectID := firstEnvironmentValue(lookup, "GOOGLE_CLOUD_PROJECT", "GCLOUD_PROJECT")
	if projectID == "" {
		return google.VertexConfig{}, nil, errors.New("GOOGLE_CLOUD_PROJECT or GCLOUD_PROJECT is required for google-vertex evals")
	}
	location := firstEnvironmentValue(lookup, "GOOGLE_CLOUD_LOCATION", "GOOGLE_CLOUD_REGION")
	if location == "" {
		return google.VertexConfig{}, nil, errors.New("GOOGLE_CLOUD_LOCATION or GOOGLE_CLOUD_REGION is required for google-vertex evals")
	}
	resolver := sigma.AuthResolver(sigma.EnvironmentAuthResolver{LookupEnv: lookup})
	if accessToken := environmentValue(lookup, "GOOGLE_CLOUD_ACCESS_TOKEN"); accessToken != "" {
		resolver = sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{
				Type:   sigma.CredentialTypeOAuthToken,
				Value:  accessToken,
				Source: "env:GOOGLE_CLOUD_ACCESS_TOKEN",
			}, nil
		})
	} else if firstEnvironmentValue(lookup, "GOOGLE_CLOUD_API_KEY", "GOOGLE_API_KEY") == "" {
		return google.VertexConfig{}, nil, errors.New(
			"GOOGLE_CLOUD_ACCESS_TOKEN, GOOGLE_CLOUD_API_KEY, or GOOGLE_API_KEY is required for google-vertex evals",
		)
	}
	return google.VertexConfig{ProjectID: projectID, Location: location}, resolver, nil
}

func unsupportedModelAPI(model sigma.Model, want sigma.API) error {
	return fmt.Errorf("model %s/%s uses API %q; provider evals require %q", model.Provider, model.ID, model.API, want)
}

func firstEnvironmentValue(lookup evals.EnvironmentLookup, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(environmentValue(lookup, name)); value != "" {
			return value
		}
	}
	return ""
}

func environmentValue(lookup evals.EnvironmentLookup, name string) string {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, _ := lookup(name)
	return value
}

func smokeCases() []smokeCase {
	return []smokeCase{
		newSmokeCase(
			"factual-recall",
			evals.Prompt("What is the capital of France? Respond with only the city name."),
			caseInsensitiveExactJudge("Paris"),
		),
		newSmokeCase(
			"arithmetic",
			evals.Prompt("Calculate 17 multiplied by 23. Respond with only the integer result."),
			exactJudge("391"),
		),
		newSmokeCase(
			"exact-formatting",
			evals.Prompt("Respond with exactly these three lowercase words separated by commas and no spaces: red,green,blue"),
			exactJudge("red,green,blue"),
		),
		newSmokeCase(
			"json-extraction",
			evals.Prompt(
				"Extract the name and count from this record: name is Ada and count is 7. "+
					"Respond with only a JSON object containing string field name and integer field count.",
			),
			jsonExtractionJudge(),
		),
		newSmokeCase(
			"multi-turn-recall",
			evals.Conversation(
				"Remember the codeword cedar. Respond with only: acknowledged",
				"What was the codeword? Respond with only the codeword.",
			),
			exactJudge("cedar"),
		),
		newToolSmokeCase(
			"tool-call-round-trip",
			evals.Prompt(
				"Call the lookup_sigma_eval_value tool with key round-trip. "+
					"After receiving the tool result, respond with only the returned value.",
			),
			toolRoundTripJudge(),
		),
	}
}

func newSmokeCase(
	name string,
	input evals.SigmaInput,
	judge evals.Judge[evals.SigmaInput, string],
) smokeCase {
	return smokeCase{
		name:        name,
		harnessKind: smokeHarnessText,
		input:       input,
		judge:       judge,
	}
}

func newToolSmokeCase(
	name string,
	input evals.SigmaInput,
	judge evals.Judge[evals.SigmaInput, string],
) smokeCase {
	return smokeCase{
		name:        name,
		harnessKind: smokeHarnessTool,
		input:       input,
		judge:       judge,
	}
}

func filterSmokeCases(cases []smokeCase, pattern *regexp.Regexp) ([]smokeCase, error) {
	if pattern == nil {
		return append([]smokeCase(nil), cases...), nil
	}
	selected := make([]smokeCase, 0, len(cases))
	for _, smoke := range cases {
		if pattern.MatchString(smoke.name) {
			selected = append(selected, smoke)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no smoke cases match -run %q", pattern.String())
	}
	return selected, nil
}

func executeSmokeRuns(
	ctx context.Context,
	runner *evals.Runner,
	stdout io.Writer,
	stderr io.Writer,
	cases []smokeCase,
	baseline smokeSuite,
	candidates []smokeSuite,
	repetitions int,
	caseTimeout time.Duration,
) (smokeRunSummary, error) {
	comparative := len(candidates) > 0

	suites := map[string]smokeSuite{baseline.name(): baseline}
	for _, candidate := range candidates {
		suites[candidate.name()] = candidate
	}
	prefix := "Provider smoke/"
	if comparative {
		prefix = "Provider comparison/"
	}

	var summary smokeRunSummary
	for _, smoke := range cases {
		rows, err := smokeHarnessRows(smoke.harnessKind, baseline, candidates, repetitions)
		if err != nil {
			return smokeRunSummary{}, fmt.Errorf("create %s harness rows: %w", smoke.name, err)
		}
		for _, row := range rows {
			suite := suites[row.Name]
			role := "candidate"
			if row.Name == baseline.name() {
				role = "baseline"
			}
			var threshold *float64
			if !comparative {
				value := 1.0
				threshold = &value
			}
			test := &commandTest{name: prefix + smoke.name}
			runContext := ctx
			var cancel context.CancelFunc
			if caseTimeout > 0 {
				runContext, cancel = context.WithTimeout(ctx, caseTimeout)
			}
			execution := evals.Run(runContext, runner, test, evals.Case[evals.SigmaInput, string]{
				EvalSet:        "Sigma text smoke",
				Input:          smoke.input,
				Harness:        row.Harness,
				Judges:         []evals.Judge[evals.SigmaInput, string]{smoke.judge},
				JudgeThreshold: threshold,
			})
			if cancel != nil {
				cancel()
			}
			afterRun := len(test.errors)
			validateSmokeExecution(test, suite.model, execution)
			validationErrors := append([]string(nil), test.errors[afterRun:]...)
			beforeCleanup := len(test.errors)
			test.finish()
			cleanupErrors := append([]string(nil), test.errors[beforeCleanup:]...)

			operationalMessages := append([]string(nil), validationErrors...)
			operationalMessages = append(operationalMessages, cleanupErrors...)
			operationalFailure := execution.Err != nil || len(operationalMessages) > 0
			correct := execution.AverageScore != nil && *execution.AverageScore >= 1
			summary.Runs++
			if correct {
				summary.Correct++
			}
			if operationalFailure {
				summary.OperationalFailures++
			}
			if operationalFailure || (!comparative && !correct) {
				summary.Failed = true
			}
			for _, message := range test.errors {
				printTof(stderr, "[eval] test=%s harness=%s error=%s\n", test.name, row.Name, message)
			}
			printTof(stdout, "%s\n", formatSmokeResult(
				role,
				row.Name,
				smoke.name,
				row.Repetition,
				execution,
				operationalMessages,
			))
		}
	}
	return summary, nil
}

func smokeHarnessRows(
	kind smokeHarnessKind,
	baseline smokeSuite,
	candidates []smokeSuite,
	repetitions int,
) ([]evals.HarnessTableRow[evals.SigmaInput, string], error) {
	baselineHarness := baseline.harness(kind)
	if len(candidates) == 0 {
		return []evals.HarnessTableRow[evals.SigmaInput, string]{
			{Harness: baselineHarness, Name: baseline.name(), Repetition: 1},
		}, nil
	}
	candidateHarnesses := make([]evals.Harness[evals.SigmaInput, string], len(candidates))
	for i, candidate := range candidates {
		candidateHarnesses[i] = candidate.harness(kind)
	}
	rows, err := evals.NewHarnessTable(
		"Sigma text smoke",
		baselineHarness,
		candidateHarnesses,
		repetitions,
	)
	if err != nil {
		return nil, fmt.Errorf("create comparison table: %w", err)
	}
	return rows, nil
}

func exactJudge(want string) evals.Judge[evals.SigmaInput, string] {
	return evals.Judge[evals.SigmaInput, string]{
		Name: "exact output",
		Score: func(_ context.Context, input evals.JudgmentInput[evals.SigmaInput, string]) (evals.JudgeResult, error) {
			if strings.TrimSpace(input.Result.Output) == want {
				return evals.JudgeResult{Score: 1}, nil
			}
			return evals.JudgeResult{
				Score:  0,
				Reason: fmt.Sprintf("response was not exactly %q", want),
			}, nil
		},
	}
}

func caseInsensitiveExactJudge(want string) evals.Judge[evals.SigmaInput, string] {
	judge := exactJudge(want)
	judge.Score = func(_ context.Context, input evals.JudgmentInput[evals.SigmaInput, string]) (evals.JudgeResult, error) {
		if strings.EqualFold(strings.TrimSpace(input.Result.Output), want) {
			return evals.JudgeResult{Score: 1}, nil
		}
		return evals.JudgeResult{
			Score:  0,
			Reason: fmt.Sprintf("response was not exactly %q", want),
		}, nil
	}
	return judge
}

func jsonExtractionJudge() evals.Judge[evals.SigmaInput, string] {
	return evals.Judge[evals.SigmaInput, string]{
		Name: "JSON extraction",
		Score: func(_ context.Context, input evals.JudgmentInput[evals.SigmaInput, string]) (evals.JudgeResult, error) {
			return scoreJSONExtraction(input.Result.Output), nil
		},
	}
}

func scoreJSONExtraction(output string) evals.JudgeResult {
	var record extractedRecord
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return evals.JudgeResult{Score: 0, Reason: "response was not the required JSON object"}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return evals.JudgeResult{Score: 0, Reason: "response contained content after the JSON object"}
	}
	if record.Name != "Ada" || record.Count != 7 {
		return evals.JudgeResult{Score: 0, Reason: "JSON fields did not match the source record"}
	}
	return evals.JudgeResult{Score: 1}
}

func smokeLookupTool() sigma.Tool {
	return sigma.Tool{
		Name:        smokeLookupToolName,
		Description: "Return the stored evaluation value for a key.",
		InputSchema: sigma.Schema{
			"type": "object",
			"properties": map[string]any{
				"key": map[string]any{"type": "string"},
			},
			"required":             []any{"key"},
			"additionalProperties": false,
		},
	}
}

func smokeLookupExecutor(tool sigma.Tool) evals.SigmaToolExecutor {
	return func(_ context.Context, call sigma.ToolCall) (evals.SigmaToolOutput, error) {
		arguments, err := sigma.ValidateToolCall([]sigma.Tool{tool}, call)
		if err != nil {
			return evals.SigmaToolOutput{Text: sigma.ToolErrorMessage(call, err), IsError: true}, nil
		}
		key, _ := arguments["key"].(string)
		if key != smokeLookupKey {
			return evals.SigmaToolOutput{
				Text:    fmt.Sprintf("key %q was not found", key),
				IsError: true,
			}, nil
		}
		return evals.SigmaToolOutput{Text: smokeLookupValue}, nil
	}
}

func toolRoundTripJudge() evals.Judge[evals.SigmaInput, string] {
	return evals.Judge[evals.SigmaInput, string]{
		Name: "tool call round trip",
		Score: func(_ context.Context, input evals.JudgmentInput[evals.SigmaInput, string]) (evals.JudgeResult, error) {
			if strings.TrimSpace(input.Result.Output) != smokeLookupValue {
				return evals.JudgeResult{Score: 0, Reason: "response did not match the successful tool result"}, nil
			}
			matchingCalls := make(map[string]struct{})
			for _, event := range input.Result.Events {
				if event.Type == "tool_call" && event.Name == smokeLookupToolName &&
					event.Arguments["key"] == smokeLookupKey {
					matchingCalls[event.ID] = struct{}{}
				}
			}
			for _, event := range input.Result.Events {
				_, matched := matchingCalls[event.ToolCallID]
				if event.Type == "tool_result" && matched && event.Name == smokeLookupToolName &&
					event.Error == "" && event.Content == smokeLookupValue {
					return evals.JudgeResult{Score: 1}, nil
				}
			}
			return evals.JudgeResult{Score: 0, Reason: "no successful matching tool call and result were recorded"}, nil
		},
	}
}

func validateSmokeExecution(
	test smokeTest,
	model sigma.Model,
	execution evals.Execution[string],
) {
	if execution.Err != nil {
		return
	}
	if execution.Result.Usage.Provider != string(model.Provider) ||
		execution.Result.Usage.Model != string(model.ID) {
		test.Errorf(
			"usage model identity = %s/%s, want %s/%s",
			execution.Result.Usage.Provider,
			execution.Result.Usage.Model,
			model.Provider,
			model.ID,
		)
	}
	if execution.Result.Usage.TotalTokens <= 0 {
		test.Errorf("total token usage = %d, want positive", execution.Result.Usage.TotalTokens)
	}
}

func formatSmokeResult(
	role string,
	harness string,
	name string,
	repetition int,
	execution evals.Execution[string],
	operationalErrors []string,
) string {
	status := "PASS"
	if execution.Err != nil || len(operationalErrors) > 0 ||
		execution.AverageScore == nil || *execution.AverageScore < 1 {
		status = "FAIL"
	}
	parts := []string{
		status,
		"role=" + role,
		"harness=" + harness,
		"case=" + name,
		fmt.Sprintf("repetition=%d", repetition),
	}
	if execution.AverageScore != nil {
		parts = append(parts, fmt.Sprintf("score=%.2f", *execution.AverageScore))
	} else {
		parts = append(parts, "score=unavailable")
	}
	parts = append(parts,
		fmt.Sprintf(
			"tokens=%d(in=%d,out=%d)",
			execution.Result.Usage.TotalTokens,
			execution.Result.Usage.InputTokens,
			execution.Result.Usage.OutputTokens,
		),
		"latency="+execution.Result.Timings.Total.Round(time.Millisecond).String(),
	)
	if execution.Result.Usage.EstimatedCostUSD != nil {
		parts = append(parts, fmt.Sprintf("cost=$%.6f", *execution.Result.Usage.EstimatedCostUSD))
	}
	parts = append(parts, fmt.Sprintf("output=%q", boundedResultText(execution.Result.Output)))
	for _, judgment := range execution.Judgments {
		if reason := strings.TrimSpace(judgment.Reason); reason != "" {
			parts = append(parts, fmt.Sprintf("reason=%q", boundedResultText(reason)))
			break
		}
	}
	if execution.Err != nil {
		parts = append(parts, fmt.Sprintf("error=%q", boundedResultText(execution.Err.Error())))
	} else if len(operationalErrors) > 0 {
		parts = append(parts, fmt.Sprintf("error=%q", boundedResultText(strings.Join(operationalErrors, "; "))))
	}
	return strings.Join(parts, " ")
}

func boundedResultText(value string) string {
	const maxRunes = 160

	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}
