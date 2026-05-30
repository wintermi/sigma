// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/anthropic"
	"github.com/wintermi/sigma/provider/fireworks"
	"github.com/wintermi/sigma/provider/opencode"
	"github.com/wintermi/sigma/provider/xai"
)

type routeSpec struct {
	Name             string
	Provider         sigma.ProviderID
	BaseURL          string
	ModelBaseURL     string
	APIKeyEnv        string
	RegisterProvider func(*sigma.Registry, routeSpec) error
	Model            func(routeSpec, string) sigma.Model
	Cases            func(routeSpec) []probeCase
}

type probeCase struct {
	Name        string
	Description string
	Request     sigma.Request
	Options     []sigma.Option
}

type probeResult struct {
	Route   string `json:"route"`
	Model   string `json:"model"`
	Case    string `json:"case"`
	Attempt string `json:"attempt"`
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
}

type summary struct {
	Total                   int `json:"total"`
	OK                      int `json:"ok"`
	Skipped                 int `json:"skipped"`
	SigmaRequestShape       int `json:"sigmaRequestShape"`
	ProviderCapabilityLimit int `json:"providerCapabilityLimit"`
	UpstreamAvailability    int `json:"upstreamAvailability"`
	NoWorkingAttempt        int `json:"noWorkingAttempt"`
	FixedByRepairVariant    int `json:"fixedByRepairVariant"`
}

type config struct {
	routes             []string
	models             map[string]bool
	repair             bool
	includeUnavailable bool
	timeout            time.Duration
}

var routes = map[string]routeSpec{
	"zen": {
		Name:             "zen",
		Provider:         sigma.ProviderOpenCode,
		BaseURL:          opencode.ZenBaseURL,
		APIKeyEnv:        "OPENCODE_API_KEY",
		RegisterProvider: registerOpenCodeProvider,
		Model:            discoveredOpenCodeModel,
		Cases:            openAICompatibleProbeCases,
	},
	"go": {
		Name:             "go",
		Provider:         sigma.ProviderOpenCodeGo,
		BaseURL:          opencode.GoBaseURL,
		APIKeyEnv:        "OPENCODE_API_KEY",
		RegisterProvider: registerOpenCodeProvider,
		Model:            discoveredOpenCodeModel,
		Cases:            openAICompatibleProbeCases,
	},
	"fireworks-openai": {
		Name:             "fireworks-openai",
		Provider:         sigma.ProviderFireworks,
		BaseURL:          fireworks.DefaultBaseURL,
		APIKeyEnv:        "FIREWORKS_API_KEY",
		RegisterProvider: registerFireworksOpenAIProvider,
		Model:            discoveredFireworksOpenAIModel,
		Cases:            openAICompatibleProbeCases,
	},
	"fireworks-anthropic": {
		Name:             "fireworks-anthropic",
		Provider:         sigma.ProviderFireworks,
		BaseURL:          "https://api.fireworks.ai/inference",
		ModelBaseURL:     fireworks.DefaultBaseURL,
		APIKeyEnv:        "FIREWORKS_API_KEY",
		RegisterProvider: registerFireworksAnthropicProvider,
		Model:            discoveredFireworksAnthropicModel,
		Cases:            anthropicCompatibleProbeCases,
	},
	"xai": {
		Name:             "xai",
		Provider:         sigma.ProviderXAI,
		BaseURL:          xai.DefaultBaseURL,
		APIKeyEnv:        "XAI_API_KEY",
		RegisterProvider: registerXAIProvider,
		Model:            discoveredXAIModel,
		Cases:            openAICompatibleProbeCases,
	},
}

const jsonTypeKey = "type"

func main() {
	cfg := parseConfig()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	writer := bufio.NewWriter(os.Stdout)
	defer func() {
		_ = writer.Flush()
	}()

	var totals summary
	for _, routeName := range cfg.routes {
		route, ok := routes[routeName]
		if !ok {
			fatalf("unknown route %q", routeName)
		}
		apiKey := os.Getenv(route.APIKeyEnv)
		if apiKey == "" {
			fatalf("%s is required for live %s probing", route.APIKeyEnv, route.Name)
		}
		models, err := modelsForRoute(ctx, route, apiKey, cfg.models)
		if err != nil {
			fatalf("discover %s models: %v", route.Name, err)
		}
		for _, modelID := range models {
			if len(cfg.models) > 0 && !cfg.models[modelID] {
				continue
			}
			for _, result := range probeModel(ctx, route, modelID, apiKey, cfg) {
				totals.add(result)
				writeResult(writer, result)
			}
		}
	}
	writeSummary(writer, totals)
}

func parseConfig() config {
	var routeList string
	var modelList string
	var timeout time.Duration
	var repair bool
	var includeUnavailable bool
	flag.StringVar(&routeList, "routes", "zen,go", "comma-separated routes: zen,go,fireworks-openai,fireworks-anthropic,xai")
	flag.StringVar(&modelList, "models", "", "comma-separated model IDs to probe")
	flag.BoolVar(&repair, "repair", false, "try targeted repair variants after a failing case")
	flag.BoolVar(&includeUnavailable, "include-unavailable", false, "run known unavailable advertised models instead of skipping them")
	flag.DurationVar(&timeout, "timeout", 10*time.Minute, "overall probe timeout")
	flag.Parse()

	return config{
		routes:             splitCSV(routeList),
		models:             setFromCSV(modelList),
		repair:             repair,
		includeUnavailable: includeUnavailable,
		timeout:            timeout,
	}
}

func modelsForRoute(ctx context.Context, route routeSpec, apiKey string, selected map[string]bool) ([]string, error) {
	if len(selected) > 0 {
		models := make([]string, 0, len(selected))
		for modelID := range selected {
			models = append(models, modelID)
		}
		sort.Strings(models)
		return models, nil
	}
	return discoverModels(ctx, route, apiKey)
}

func discoverModels(ctx context.Context, route routeSpec, apiKey string) ([]string, error) {
	baseURL := route.BaseURL
	if route.ModelBaseURL != "" {
		baseURL = route.ModelBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("X-Goog-Api-Key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GET /models returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseModelIDs(body)
}

func parseModelIDs(body []byte) ([]string, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if id, _ := v["id"].(string); id != "" {
				ids = append(ids, id)
			}
			if data, ok := v["data"]; ok {
				walk(data)
			}
			if models, ok := v["models"]; ok {
				walk(models)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(decoded)
	if len(ids) == 0 {
		return nil, fmt.Errorf("no model IDs in /models response")
	}
	sort.Strings(ids)
	return ids, nil
}

func probeModel(ctx context.Context, route routeSpec, modelID string, apiKey string, cfg config) []probeResult {
	if !cfg.includeUnavailable && knownUnavailable(route.Name, modelID) {
		return []probeResult{{
			Route:   route.Name,
			Model:   modelID,
			Case:    "all",
			Attempt: "skip_known_unavailable",
			Outcome: "skipped",
		}}
	}

	client := probeClient(route, modelID)
	model := route.Model(route, modelID)
	cases := route.Cases(route)
	results := make([]probeResult, 0, len(cases))
	for _, testCase := range cases {
		result := runCase(ctx, route, client, model, testCase, apiKey, testCase.Name)
		if result.Outcome == "ok" || !cfg.repair {
			results = append(results, result)
			continue
		}
		repaired := result
		for _, variant := range repairVariants(route, testCase) {
			attempt := runCase(ctx, route, client, model, variant, apiKey, variant.Name)
			if attempt.Outcome == "ok" {
				attempt.Outcome = "fixed_by_repair_variant"
				repaired = attempt
				break
			}
		}
		results = append(results, repaired)
	}
	return results
}

func probeClient(route routeSpec, modelID string) *sigma.Client {
	registry := sigma.NewRegistry()
	_ = route.RegisterProvider(registry, route)
	_ = registry.RegisterModel(route.Model(route, modelID))
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func registerOpenCodeProvider(registry *sigma.Registry, route routeSpec) error {
	switch route.Name {
	case "go":
		if err := opencode.RegisterGo(registry); err != nil {
			return fmt.Errorf("register opencode go provider: %w", err)
		}
	default:
		if err := opencode.RegisterZen(registry); err != nil {
			return fmt.Errorf("register opencode zen provider: %w", err)
		}
	}
	return nil
}

func registerFireworksOpenAIProvider(registry *sigma.Registry, route routeSpec) error {
	if err := fireworks.Register(registry, fireworks.WithBaseURL(route.BaseURL)); err != nil {
		return fmt.Errorf("register fireworks openai-compatible provider: %w", err)
	}
	return nil
}

func registerFireworksAnthropicProvider(registry *sigma.Registry, route routeSpec) error {
	if err := anthropic.Register(registry, sigma.ProviderFireworks, anthropic.WithBaseURL(route.BaseURL)); err != nil {
		return fmt.Errorf("register fireworks anthropic-compatible provider: %w", err)
	}
	return nil
}

func registerXAIProvider(registry *sigma.Registry, route routeSpec) error {
	if err := xai.Register(registry, xai.WithBaseURL(route.BaseURL)); err != nil {
		return fmt.Errorf("register xai provider: %w", err)
	}
	return nil
}

func discoveredOpenCodeModel(route routeSpec, id string) sigma.Model {
	return sigma.Model{
		ID:               sigma.ModelID(id),
		Provider:         route.Provider,
		API:              sigma.APIOpenAICompletions,
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:    true,
		SupportsThinking: true,
		ProviderMetadata: map[string]any{
			"baseURL":         route.BaseURL,
			"apiKeyEnvVars":   []string{"OPENCODE_API_KEY"},
			"modelFamily":     modelFamily(id),
			"opencodeAPI":     string(openCodeRouteAPI(route.Name, id)),
			"probeDiscovered": true,
		},
	}
}

func discoveredXAIModel(route routeSpec, id string) sigma.Model {
	return sigma.Model{
		ID:               sigma.ModelID(id),
		Provider:         route.Provider,
		API:              sigma.APIOpenAICompletions,
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:    true,
		SupportsThinking: true,
		ProviderMetadata: map[string]any{
			"baseURL":         route.BaseURL,
			"apiKeyEnvVars":   []string{route.APIKeyEnv},
			"modelFamily":     modelFamily(id),
			"probeDiscovered": true,
			"probeRoute":      route.Name,
			"probeSurface":    "openai-completions",
		},
	}
}

func discoveredFireworksOpenAIModel(route routeSpec, id string) sigma.Model {
	return sigma.Model{
		ID:               sigma.ModelID(id),
		Provider:         route.Provider,
		API:              sigma.APIOpenAICompletions,
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:    true,
		SupportsThinking: true,
		ThinkingLevels:   []sigma.ThinkingLevel{sigma.ThinkingLevelLow, sigma.ThinkingLevelMedium, sigma.ThinkingLevelHigh},
		ContextWindow:    256000,
		MaxOutputTokens:  256000,
		OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
			ReasoningFormat:        sigma.OpenAICompletionsReasoningFireworks,
			SupportsStreamingUsage: sigma.OpenAICompatSupported,
			SupportsStrictTools:    sigma.OpenAICompatSupported,
			MaxTokensField:         sigma.OpenAICompletionsMaxTokens,
		},
		ProviderMetadata: map[string]any{
			"baseURL":          route.BaseURL,
			"apiKeyEnvVars":    []string{route.APIKeyEnv},
			"modelFamily":      modelFamily(id),
			"probeDiscovered":  true,
			"probeRoute":       route.Name,
			"probeSurface":     "openai-completions",
			"fireworksSurface": "openai",
		},
	}
}

func discoveredFireworksAnthropicModel(route routeSpec, id string) sigma.Model {
	return sigma.Model{
		ID:               sigma.ModelID(id),
		Provider:         route.Provider,
		API:              sigma.APIAnthropicMessages,
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:    true,
		SupportsThinking: true,
		ThinkingLevels:   []sigma.ThinkingLevel{sigma.ThinkingLevelLow, sigma.ThinkingLevelMedium, sigma.ThinkingLevelHigh},
		ContextWindow:    262000,
		MaxOutputTokens:  262000,
		AnthropicMessagesCompat: &sigma.AnthropicMessagesCompat{
			SupportsEagerToolInputStreaming: sigma.AnthropicCompatUnsupported,
			SupportsLongCacheRetention:      sigma.AnthropicCompatUnsupported,
			SupportsSessionAffinity:         sigma.AnthropicCompatSupported,
			SupportsCacheControlOnTools:     sigma.AnthropicCompatUnsupported,
			ThinkingFormat:                  sigma.AnthropicThinkingBudget,
		},
		ProviderMetadata: map[string]any{
			"baseURL":          route.BaseURL,
			"apiKeyEnvVars":    []string{route.APIKeyEnv},
			"modelFamily":      modelFamily(id),
			"probeDiscovered":  true,
			"probeRoute":       route.Name,
			"probeSurface":     "anthropic-messages",
			"fireworksSurface": "anthropic",
		},
	}
}

func runCase(ctx context.Context, route routeSpec, client *sigma.Client, model sigma.Model, testCase probeCase, apiKey string, attempt string) probeResult {
	options := append([]sigma.Option{sigma.WithAPIKey(apiKey)}, testCase.Options...)
	_, err := client.Complete(ctx, model, testCase.Request, options...)
	if err == nil {
		return probeResult{
			Route:   route.Name,
			Model:   string(model.ID),
			Case:    testCase.Name,
			Attempt: attempt,
			Outcome: "ok",
		}
	}
	return probeResult{
		Route:   route.Name,
		Model:   string(model.ID),
		Case:    testCase.Name,
		Attempt: attempt,
		Outcome: classifyFailure(route, model, err),
		Error:   err.Error(),
	}
}

func openAICompatibleProbeCases(route routeSpec) []probeCase {
	return []probeCase{
		singleTurnCase("basic_text", "plain streaming text", basicRequest("Reply with exactly: sigma-ok."), []sigma.Option{sigma.WithMaxTokens(128)}),
		singleTurnCase("developer_instruction", "developer instruction handling", sigma.Request{
			SystemPrompt: "Reply tersely.",
			Messages:     []sigma.Message{sigma.UserText("Reply with exactly: dev-ok.")},
		}, []sigma.Option{sigma.WithMaxTokens(128)}),
		singleTurnCase("json_object", "JSON object mode", basicRequest("Return JSON exactly {\"ok\":true}."), []sigma.Option{
			sigma.WithProviderOption(route.Provider, "extra_body", map[string]any{"response_format": map[string]any{jsonTypeKey: "json_object"}}),
			sigma.WithMaxTokens(256),
		}),
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("Return JSON exactly {\"answer\":\"ok\"}."), []sigma.Option{
			sigma.WithProviderOption(route.Provider, "extra_body", jsonSchemaBody()),
			sigma.WithMaxTokens(256),
		}),
		singleTurnCase("logprobs", "logprobs and top_logprobs", basicRequest("Reply with exactly: yes."), []sigma.Option{
			sigma.WithProviderOption(route.Provider, "extra_body", map[string]any{"logprobs": true, "top_logprobs": 2}),
			sigma.WithMaxTokens(16),
		}),
		singleTurnCase("cache_ephemeral", "prompt cache marker", basicRequest("Reply with exactly: cache-ok."), []sigma.Option{
			sigma.WithCacheRetention(sigma.CacheRetentionEphemeral),
			sigma.WithMaxTokens(128),
		}),
		singleTurnCase("image_input", "text plus image input", imageRequest(), []sigma.Option{sigma.WithMaxTokens(512)}),
		singleTurnCase("thinking_string_none", "raw thinking string none", basicRequest("Reply with exactly: 5."), rawBodyOptions(route, map[string]any{"thinking": "none"})),
		singleTurnCase("thinking_object_disabled", "raw thinking object disabled", basicRequest("Reply with exactly: 5."), rawBodyOptions(route, map[string]any{"thinking": map[string]any{jsonTypeKey: "disabled"}})),
		singleTurnCase("thinking_bool_false", "raw thinking false", basicRequest("Reply with exactly: 5."), rawBodyOptions(route, map[string]any{"thinking": false})),
		singleTurnCase("enable_thinking_false", "raw enable_thinking false", basicRequest("Reply with exactly: 5."), rawBodyOptions(route, map[string]any{"enable_thinking": false})),
		singleTurnCase("reasoning_effort_low", "raw reasoning effort low", basicRequest("Reply with exactly: 5."), rawBodyOptions(route, map[string]any{"reasoning_effort": "low"})),
		singleTurnCase("reasoning_effort_medium", "raw reasoning effort medium", basicRequest("Reply with exactly: 5."), rawBodyOptions(route, map[string]any{"reasoning_effort": "medium"})),
		singleTurnCase("reasoning_effort_high", "raw reasoning effort high", basicRequest("Reply with exactly: 5."), rawBodyOptions(route, map[string]any{"reasoning_effort": "high"})),
		toolCase("tool_auto_file_read", "auto read-file tool", "auto"),
		toolCase("tool_required_file_read", "required read-file tool", "required"),
		toolCase("strict_tool_required_write", "required strict write-file tool", "required"),
		toolCase("three_turn_file_update", "multi-turn file update", "auto"),
	}
}

func anthropicCompatibleProbeCases(_ routeSpec) []probeCase {
	return []probeCase{
		singleTurnCase("basic_text", "plain streaming text", basicRequest("Reply with exactly: sigma-ok."), []sigma.Option{sigma.WithMaxTokens(128)}),
		singleTurnCase("developer_instruction", "system instruction handling", sigma.Request{
			SystemPrompt: "Reply tersely.",
			Messages:     []sigma.Message{sigma.UserText("Reply with exactly: dev-ok.")},
		}, []sigma.Option{sigma.WithMaxTokens(128)}),
		singleTurnCase("cache_ephemeral", "prompt cache marker", basicRequest("Reply with exactly: cache-ok."), []sigma.Option{
			sigma.WithCacheRetention(sigma.CacheRetentionEphemeral),
			sigma.WithSessionID("sigma-fireworks-probe"),
			sigma.WithMaxTokens(128),
		}),
		singleTurnCase("image_input", "text plus image input", imageRequest(), []sigma.Option{sigma.WithMaxTokens(512)}),
		singleTurnCase("reasoning_level_low", "typed reasoning low", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithReasoningLevel(sigma.ThinkingLevelLow), sigma.WithMaxTokens(512)}),
		singleTurnCase("reasoning_level_medium", "typed reasoning medium", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithReasoningLevel(sigma.ThinkingLevelMedium), sigma.WithMaxTokens(512)}),
		singleTurnCase("reasoning_level_high", "typed reasoning high", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithReasoningLevel(sigma.ThinkingLevelHigh), sigma.WithMaxTokens(512)}),
		toolCase("tool_auto_file_read", "auto read-file tool", "auto"),
		toolCase("tool_required_file_read", "required read-file tool", "required"),
	}
}

func singleTurnCase(name string, description string, req sigma.Request, opts []sigma.Option) probeCase {
	return probeCase{Name: name, Description: description, Request: req, Options: opts}
}

func basicRequest(prompt string) sigma.Request {
	return sigma.Request{Messages: []sigma.Message{sigma.UserText(prompt)}}
}

func imageRequest() sigma.Request {
	return sigma.Request{Messages: []sigma.Message{sigma.UserContent(
		sigma.Text("Answer with one short colour word."),
		sigma.ImageBase64("image/png", "iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAAKUlEQVR4nO3NMQEAAAgDINc/9K3hHBQg9E1m7zMAAAAAAAAAAIBrA5slAWED4YWrAAAAAElFTkSuQmCC"),
	)}}
}

func toolCase(name string, description string, choice any) probeCase {
	return probeCase{
		Name:        name,
		Description: description,
		Request: sigma.Request{
			Messages: []sigma.Message{sigma.UserText("Use the available tool and answer with the result.")},
			Tools: []sigma.Tool{{
				Name:        "read_file",
				Description: "Read a file",
				InputSchema: sigma.Schema{
					jsonTypeKey:  "object",
					"properties": map[string]any{"path": map[string]any{jsonTypeKey: "string"}},
					"required":   []any{"path"},
				},
			}},
		},
		Options: []sigma.Option{
			sigma.WithOpenAIOptions(sigma.OpenAIOptions{ToolChoice: choice}),
			sigma.WithMaxTokens(512),
		},
	}
}

func repairVariants(route routeSpec, failure probeCase) []probeCase {
	variants := []probeCase{
		singleTurnCase("minimal_basic_text", "minimal availability check", basicRequest("Reply with exactly: sigma-ok."), []sigma.Option{sigma.WithMaxTokens(512)}),
	}
	switch failure.Name {
	case "basic_text":
		variants = append(variants, singleTurnCase("basic_text_more_tokens", "larger output cap", failure.Request, []sigma.Option{sigma.WithMaxTokens(512)}))
	case "cache_ephemeral":
		variants = append(variants,
			singleTurnCase("cache_none", "without cache marker", basicRequest("Reply with exactly: cache-ok."), []sigma.Option{sigma.WithMaxTokens(512)}),
			singleTurnCase("cache_none_more_tokens", "without cache marker and larger cap", basicRequest("Reply with exactly: cache-ok."), []sigma.Option{sigma.WithMaxTokens(1024)}),
		)
	case "image_input":
		variants = append(variants,
			singleTurnCase("image_more_tokens", "image with larger output cap", imageRequest(), []sigma.Option{sigma.WithMaxTokens(2048)}),
			singleTurnCase("text_only_fallback", "same task without image input", basicRequest("Answer with one short colour word: red."), []sigma.Option{sigma.WithMaxTokens(64)}),
		)
	case "thinking_string_none", "thinking_bool_false", "thinking_object_disabled", "enable_thinking_false":
		variants = append(variants,
			singleTurnCase("thinking_object_disabled_repair", "object disabled thinking", basicRequest("Reply with exactly: 5."), rawBodyOptions(route, map[string]any{"thinking": map[string]any{jsonTypeKey: "disabled"}})),
			singleTurnCase("no_thinking_control", "omit thinking control", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithMaxTokens(256)}),
			singleTurnCase("no_thinking_control_more_tokens", "omit thinking control and larger cap", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithMaxTokens(1024)}),
		)
	case "reasoning_effort_low", "reasoning_effort_medium", "reasoning_effort_high":
		variants = append(variants,
			singleTurnCase("typed_reasoning_effort_low", "typed reasoning low", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithOpenAIOptions(sigma.OpenAIOptions{ReasoningEffort: sigma.ThinkingLevelLow}), sigma.WithMaxTokens(512)}),
			singleTurnCase("typed_reasoning_effort_medium", "typed reasoning medium", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithOpenAIOptions(sigma.OpenAIOptions{ReasoningEffort: sigma.ThinkingLevelMedium}), sigma.WithMaxTokens(512)}),
			singleTurnCase("typed_reasoning_effort_high", "typed reasoning high", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithOpenAIOptions(sigma.OpenAIOptions{ReasoningEffort: sigma.ThinkingLevelHigh}), sigma.WithMaxTokens(512)}),
			singleTurnCase("no_reasoning_control", "omit reasoning control", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithMaxTokens(512)}),
		)
	case "json_schema":
		variants = append(variants,
			singleTurnCase("json_schema_more_tokens", "strict schema with larger cap", failure.Request, []sigma.Option{
				sigma.WithProviderOption(route.Provider, "extra_body", jsonSchemaBody()),
				sigma.WithMaxTokens(1024),
			}),
			singleTurnCase("json_object_fallback", "JSON object fallback", basicRequest("Return JSON exactly {\"answer\":\"ok\"}."), []sigma.Option{
				sigma.WithProviderOption(route.Provider, "extra_body", map[string]any{"response_format": map[string]any{jsonTypeKey: "json_object"}}),
				sigma.WithMaxTokens(512),
			}),
			singleTurnCase("manual_json", "prompt-level JSON only", basicRequest("Return JSON exactly {\"answer\":\"ok\"}."), []sigma.Option{sigma.WithMaxTokens(512)}),
		)
	case "logprobs":
		variants = append(variants,
			singleTurnCase("no_logprobs_more_tokens", "omit logprobs and larger cap", basicRequest("Reply with exactly: yes."), []sigma.Option{sigma.WithMaxTokens(512)}),
		)
	case "tool_auto_file_read", "tool_required_file_read", "strict_tool_required_write", "three_turn_file_update":
		variants = append(variants,
			toolCase("tool_auto_more_turns", "auto tool choice with larger cap", "auto"),
			singleTurnCase("three_turn_more_tokens", "larger multi-turn budget", failure.Request, []sigma.Option{sigma.WithMaxTokens(1024)}),
			toolCase("one_turn_auto_write", "simpler one-turn auto write", "auto"),
		)
	}
	return uniqueCases(variants)
}

func rawBodyOptions(route routeSpec, body map[string]any) []sigma.Option {
	return []sigma.Option{
		sigma.WithProviderOption(route.Provider, "extra_body", body),
		sigma.WithMaxTokens(256),
	}
}

func jsonSchemaBody() map[string]any {
	return map[string]any{"response_format": map[string]any{
		jsonTypeKey: "json_schema",
		"json_schema": map[string]any{
			"name":   "answer",
			"strict": true,
			"schema": map[string]any{
				jsonTypeKey:            "object",
				"properties":           map[string]any{"answer": map[string]any{jsonTypeKey: "string"}},
				"required":             []any{"answer"},
				"additionalProperties": false,
			},
		},
	}}
}

func openCodeRouteAPI(route string, id string) sigma.API {
	switch route {
	case "zen":
		return zenRouteAPI(id)
	case "go":
		return goRouteAPI(id)
	default:
		return sigma.APIOpenAICompletions
	}
}

func zenRouteAPI(id string) sigma.API {
	switch {
	case strings.HasPrefix(id, "gemini-"):
		return sigma.APIGoogleGenerativeAI
	case strings.HasPrefix(id, "claude-") || strings.HasPrefix(id, "qwen3."):
		return sigma.APIAnthropicMessages
	case strings.HasPrefix(id, "gpt-"):
		return sigma.APIOpenAIResponses
	default:
		return sigma.APIOpenAICompletions
	}
}

func goRouteAPI(id string) sigma.API {
	switch id {
	case "qwen3.7-max", "minimax-m2.5":
		return sigma.APIAnthropicMessages
	default:
		return sigma.APIOpenAICompletions
	}
}

func knownUnavailable(route string, id string) bool {
	if route != "zen" {
		return false
	}
	switch id {
	case "claude-opus-4-6", "minimax-m2.5-free", "qwen3.6-plus-free", "gpt-5.3-codex-spark":
		return true
	default:
		return false
	}
}

func classifyFailure(route routeSpec, model sigma.Model, err error) string {
	if knownUnavailable(route.Name, string(model.ID)) {
		return "upstream_availability"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "image") && strings.Contains(message, "support"):
		return "provider_capability_limit"
	case strings.Contains(message, "free promotion has ended"),
		strings.Contains(message, "model_not_found"),
		strings.Contains(message, "no provider available"):
		return "upstream_availability"
	case strings.Contains(message, "unknown parameter"),
		strings.Contains(message, "missing required parameter"),
		strings.Contains(message, "not supported for format oa-compat"),
		strings.Contains(message, "integer below minimum"):
		return "sigma_request_shape"
	default:
		return "no_working_attempt"
	}
}

func modelFamily(id string) string {
	for _, separator := range []string{"-", "."} {
		if before, _, ok := strings.Cut(id, separator); ok {
			return before
		}
	}
	return id
}

func uniqueCases(cases []probeCase) []probeCase {
	seen := make(map[string]bool, len(cases))
	unique := cases[:0]
	for _, testCase := range cases {
		if seen[testCase.Name] {
			continue
		}
		seen[testCase.Name] = true
		unique = append(unique, testCase)
	}
	return unique
}

func (s *summary) add(result probeResult) {
	s.Total++
	switch result.Outcome {
	case "ok":
		s.OK++
	case "skipped":
		s.Skipped++
	case "sigma_request_shape":
		s.SigmaRequestShape++
	case "provider_capability_limit":
		s.ProviderCapabilityLimit++
	case "upstream_availability":
		s.UpstreamAvailability++
	case "fixed_by_repair_variant":
		s.FixedByRepairVariant++
	default:
		s.NoWorkingAttempt++
	}
}

func writeResult(writer *bufio.Writer, result probeResult) {
	encoded, _ := json.Marshal(result)
	_, _ = writer.Write(encoded)
	_ = writer.WriteByte('\n')
}

func writeSummary(writer *bufio.Writer, totals summary) {
	encoded, _ := json.Marshal(map[string]any{"summary": totals})
	_, _ = writer.Write(encoded)
	_ = writer.WriteByte('\n')
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func setFromCSV(value string) map[string]bool {
	parts := splitCSV(value)
	if len(parts) == 0 {
		return nil
	}
	set := make(map[string]bool, len(parts))
	for _, part := range parts {
		set[part] = true
	}
	return set
}

func fatalf(format string, args ...any) {
	var b bytes.Buffer
	_, _ = fmt.Fprintf(&b, format, args...)
	_, _ = fmt.Fprintln(os.Stderr, b.String())
	os.Exit(1)
}
