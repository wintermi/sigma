// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"errors"
	"fmt"
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

func factualSmokeCase(harness evals.Harness[evals.SigmaInput, string]) evals.Case[evals.SigmaInput, string] {
	threshold := 1.0
	return evals.Case[evals.SigmaInput, string]{
		EvalSet: "Sigma text smoke",
		Input:   evals.Prompt("What is the capital of France? Respond with only the city name."),
		Harness: harness,
		Judges: []evals.Judge[evals.SigmaInput, string]{
			{
				Name: "exact answer",
				Score: func(_ context.Context, input evals.JudgmentInput[evals.SigmaInput, string]) (evals.JudgeResult, error) {
					if strings.EqualFold(strings.TrimSpace(input.Result.Output), "Paris") {
						return evals.JudgeResult{Score: 1}, nil
					}
					return evals.JudgeResult{Score: 0, Reason: "response was not exactly Paris"}, nil
				},
			},
		},
		JudgeThreshold: &threshold,
	}
}
