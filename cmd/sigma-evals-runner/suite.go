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
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/evals"
	"github.com/wintermi/sigma/provider/fireworks"
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/provider/opencode"
)

type smokeSuite struct {
	model   sigma.Model
	harness evals.Harness[evals.SigmaInput, string]
}

type namedSmokeCase struct {
	name       string
	evaluation evals.Case[evals.SigmaInput, string]
}

type smokeTest interface {
	Errorf(string, ...any)
}

type extractedRecord struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func newSmokeSuite(selection evals.ModelSelection, lookup evals.EnvironmentLookup) (smokeSuite, error) {
	registry := sigma.DefaultRegistry()
	model, ok := registry.Model(selection.Provider, selection.Model)
	if !ok {
		return smokeSuite{}, fmt.Errorf("model not found: %s/%s", selection.Provider, selection.Model)
	}
	resolver, err := registerSmokeProvider(registry, model, lookup)
	if err != nil {
		return smokeSuite{}, err
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(resolver),
	)
	harness, err := evals.NewSigmaTextHarness(evals.SigmaHarnessConfig{
		Name:   string(model.Provider),
		Client: client,
		Model:  model,
	})
	if err != nil {
		return smokeSuite{}, fmt.Errorf("create harness: %w", err)
	}
	return smokeSuite{model: model, harness: harness}, nil
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

func smokeCases(harness evals.Harness[evals.SigmaInput, string]) []namedSmokeCase {
	return []namedSmokeCase{
		newSmokeCase(
			"factual-recall",
			evals.Prompt("What is the capital of France? Respond with only the city name."),
			harness,
			caseInsensitiveExactJudge("Paris"),
		),
		newSmokeCase(
			"arithmetic",
			evals.Prompt("Calculate 17 multiplied by 23. Respond with only the integer result."),
			harness,
			exactJudge("391"),
		),
		newSmokeCase(
			"exact-formatting",
			evals.Prompt("Respond with exactly these three lowercase words separated by commas and no spaces: red,green,blue"),
			harness,
			exactJudge("red,green,blue"),
		),
		newSmokeCase(
			"json-extraction",
			evals.Prompt(
				"Extract the name and count from this record: name is Ada and count is 7. "+
					"Respond with only a JSON object containing string field name and integer field count.",
			),
			harness,
			jsonExtractionJudge(),
		),
		newSmokeCase(
			"multi-turn-recall",
			evals.Conversation(
				"Remember the codeword cedar. Respond with only: acknowledged",
				"What was the codeword? Respond with only the codeword.",
			),
			harness,
			exactJudge("cedar"),
		),
	}
}

func newSmokeCase(
	name string,
	input evals.SigmaInput,
	harness evals.Harness[evals.SigmaInput, string],
	judge evals.Judge[evals.SigmaInput, string],
) namedSmokeCase {
	threshold := 1.0
	return namedSmokeCase{
		name: name,
		evaluation: evals.Case[evals.SigmaInput, string]{
			EvalSet:        "Sigma text smoke",
			Input:          input,
			Harness:        harness,
			Judges:         []evals.Judge[evals.SigmaInput, string]{judge},
			JudgeThreshold: &threshold,
		},
	}
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
