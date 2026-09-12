// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestSigmaHarnessMissingTurnUsageStaysUnavailable(t *testing.T) {
	t.Parallel()
	for _, toolLoop := range []bool{false, true} {
		for _, missing := range []int{-1, 0, 1, 2} {
			t.Run(fmt.Sprintf("tool=%v/missing=%d", toolLoop, missing), func(t *testing.T) {
				t.Parallel()
				model := sigmatest.TextModel()
				model.InputCostPerMillion = 2
				scripts := make([]sigmatest.Script, 3)
				for i := range scripts {
					final := sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("answer")}}
					if i != missing {
						final.Usage = &sigma.Usage{InputTokens: 10}
					}
					if toolLoop && i < 2 {
						final.Content = []sigma.ContentBlock{sigma.ToolCallBlock(fmt.Sprint(i), "lookup", map[string]any{})}
						final.StopReason = sigma.StopReasonToolCalls
					}
					scripts[i].Final = final
				}
				provider := sigmatest.NewFauxProvider(scripts...)
				config := SigmaHarnessConfig{Client: sigmaHarnessTestClient(t, provider, model), Model: model}
				input := Conversation("first", "second", "third")
				if toolLoop {
					input = Prompt("lookup")
					config.ToolExecutor = func(context.Context, sigma.ToolCall) (SigmaToolOutput, error) {
						return SigmaToolOutput{Text: "value"}, nil
					}
				}
				harness, err := NewSigmaTextHarness(config)
				if err != nil {
					t.Fatal(err)
				}
				run := newRunContext("usage")
				result, err := harness.Run(t.Context(), input, run)
				if err != nil {
					t.Fatal(err)
				}
				if len(provider.Requests()) != 3 {
					t.Fatal("did not execute all turns")
				}
				if missing < 0 {
					if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 30 || result.Usage.EstimatedCostUSD == nil {
						t.Fatalf("complete usage = %+v", result.Usage)
					}
				} else if result.Usage.TotalTokens != nil || result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil || result.Usage.EstimatedCostUSD != nil {
					t.Fatalf("partial usage published as complete: %+v", result.Usage)
				}
				_, attachments := run.snapshot()
				if len(attachments) != 1 || !strings.Contains(string(attachments[0].Body), `"inputTokens":10`) {
					t.Fatal("per-turn usage was discarded")
				}
			})
		}
	}
}

func TestSigmaUsageTracksCostAndTokensIndependently(t *testing.T) {
	t.Parallel()
	model := sigmatest.TextModel()
	model.InputCostPerMillion = 2
	for _, tt := range []struct {
		name       string
		turns      []sigma.AssistantMessage
		wantTokens *int
		wantCost   *float64
	}{
		{name: "measured zero", turns: []sigma.AssistantMessage{{Usage: &sigma.Usage{}}}, wantTokens: intPointer(0), wantCost: floatPointer(0)},
		{name: "all absent", turns: []sigma.AssistantMessage{{}, {}}},
		{name: "cost without tokens", turns: []sigma.AssistantMessage{{Cost: &sigma.Cost{Currency: "USD", TotalCost: 1}}}, wantCost: floatPointer(1)},
		{name: "non-USD then USD", turns: []sigma.AssistantMessage{
			{Usage: &sigma.Usage{InputTokens: 10}, Cost: &sigma.Cost{Currency: "EUR", TotalCost: 1}},
			{Usage: &sigma.Usage{InputTokens: 10}, Cost: &sigma.Cost{Currency: "USD", TotalCost: 1}},
		}, wantTokens: intPointer(20)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			accumulator := sigmaUsageAccumulator{tokensComplete: true, costComplete: true}
			var usage Usage
			for _, final := range tt.turns {
				accumulator.accumulate(&usage, model, final)
			}
			if (usage.TotalTokens == nil) != (tt.wantTokens == nil) || (usage.TotalTokens != nil && *usage.TotalTokens != *tt.wantTokens) ||
				(usage.EstimatedCostUSD == nil) != (tt.wantCost == nil) || (usage.EstimatedCostUSD != nil && *usage.EstimatedCostUSD != *tt.wantCost) {
				t.Fatalf("usage = %+v", usage)
			}
		})
	}
}

func TestSigmaHarnessCapturesConfigurationWithoutReapplyingOptions(t *testing.T) {
	t.Parallel()
	model := sigmatest.TextModel()
	provider := sigmatest.NewFauxProvider(
		sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("first")}}},
		sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("last")}}},
	)
	tool := sigma.Tool{Name: "lookup", InputSchema: sigma.Schema{"type": "object", "description": "original schema"}}
	registry, err := sigmatest.Registry(provider, model)
	if err != nil {
		t.Fatal(err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry), sigma.WithDefaultOptions(sigma.WithTemperature(0.25), sigma.WithMaxTokens(900)))
	applications := 0
	harness, err := NewSigmaHarness(SigmaHarnessConfig{
		Client: client, Model: model,
		BaseRequest:           sigma.Request{SystemPrompt: "base", Tools: []sigma.Tool{tool}},
		TransformSystemPrompt: func(value string) (string, error) { return value + " transformed", nil },
		Options: []sigma.Option{func(options *sigma.Options) {
			applications++
			options.MaxTokens = intPointer(applications * 10)
			options.APIKey = "synthetic-api-secret"
			options.Headers = map[string]string{"X-Secret": "synthetic-header-secret"}
			options.Metadata = map[string]any{"secret": "synthetic-metadata-secret"}
			options.ProviderOptions = map[sigma.ProviderID]map[string]any{model.Provider: {"secret": "synthetic-provider-secret"}}
		}},
	}, func(_ context.Context, run SigmaRun) (string, error) {
		// Mutating the output-transform view must not rewrite captured configuration.
		run.Request.Tools[0].InputSchema.(map[string]any)["description"] = "mutation"
		return run.Response, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	run := newRunContext("configuration")
	_, err = harness.Run(t.Context(), Conversation("first prompt", "last prompt"), run)
	if err != nil {
		t.Fatal(err)
	}
	requests := provider.Requests()
	if applications != 2 || len(requests) != 2 || *requests[0].Options.MaxTokens != 10 || *requests[1].Options.MaxTokens != 20 {
		t.Fatalf("option applications=%d requests=%+v", applications, requests)
	}
	_, attachments := run.snapshot()
	body := string(attachments[0].Body)
	if strings.Contains(body, "synthetic-") || strings.Contains(body, "mutation") {
		t.Fatalf("unsafe or mutable capture: %s", body)
	}
	var configuration sigmaRunConfiguration
	var controls []sigmaRunControls
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		var record sigmaTranscriptRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record.Configuration != nil {
			if err := json.Unmarshal(record.Configuration, &configuration); err != nil {
				t.Fatal(err)
			}
		}
		if record.Controls != nil {
			if record.Stage != "merged-options-before-request-adjustments" {
				t.Fatalf("stage = %q", record.Stage)
			}
			var captured sigmaRunControls
			if err := json.Unmarshal(record.Controls, &captured); err != nil {
				t.Fatal(err)
			}
			controls = append(controls, captured)
		}
	}
	if configuration.SystemPrompt != requests[0].Request.SystemPrompt || configuration.SystemPrompt != "base transformed" ||
		len(configuration.Tools) != 1 || configuration.Tools[0].Name != "lookup" || len(controls) != 2 ||
		*controls[0].MaxTokens != 10 || *controls[1].MaxTokens != 20 || controls[0].Temperature == nil || *controls[0].Temperature != 0.25 {
		t.Fatalf("configuration=%+v controls=%+v", configuration, controls)
	}
}
