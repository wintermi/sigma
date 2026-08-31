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
	"errors"
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
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/moonshot"
	"github.com/wintermi/sigma/provider/nvidia"
	"github.com/wintermi/sigma/provider/openai"
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
	Cases            func(routeSpec, sigma.Model) []probeCase
}

type probeCase struct {
	Name        string
	Description string
	Request     sigma.Request
	Options     []sigma.Option
}

type imageRouteSpec struct {
	Name             string
	Provider         sigma.ProviderID
	BaseURL          string
	APIKeyEnv        string
	RegisterProvider func(*sigma.Registry, imageRouteSpec) error
	Model            func(imageRouteSpec, string) sigma.ImageModel
	Cases            func(imageRouteSpec) []imageProbeCase
}

type imageProbeCase struct {
	Name              string
	Description       string
	ModelID           string
	Request           sigma.ImageRequest
	Options           []sigma.ImageOption
	Stream            bool
	ResponsesTool     bool
	ResponsesModelID  string
	ResponsesRequest  sigma.Request
	ResponsesOptions  []sigma.Option
	SuccessHint       string
	RequireImage      bool
	RequirePartial    bool
	RequireToolOutput bool
}

type probeResult struct {
	Route                      string          `json:"route"`
	Model                      string          `json:"model"`
	Case                       string          `json:"case"`
	Attempt                    string          `json:"attempt"`
	SourceRoute                string          `json:"sourceRoute,omitempty"`
	SourceModel                string          `json:"sourceModel,omitempty"`
	Outcome                    string          `json:"outcome"`
	Error                      string          `json:"error,omitempty"`
	OriginalError              string          `json:"originalError,omitempty"`
	FailedAttempts             []failedAttempt `json:"failedAttempts,omitempty"`
	Hint                       string          `json:"hint,omitempty"`
	AvailabilityOKAfterFailure bool            `json:"availabilityOKAfterFailure,omitempty"`
}

type failedAttempt struct {
	Attempt string `json:"attempt"`
	Error   string `json:"error"`
}

type probeRecommendation struct {
	Route    string `json:"route"`
	Model    string `json:"model"`
	Case     string `json:"case"`
	Hint     string `json:"hint"`
	Evidence string `json:"evidence"`
}

type probeReport struct {
	Summary         summary               `json:"summary"`
	Recommendations []probeRecommendation `json:"recommendations,omitempty"`
}

type summary struct {
	Total                      int `json:"total"`
	OK                         int `json:"ok"`
	Skipped                    int `json:"skipped"`
	SigmaRequestShape          int `json:"sigmaRequestShape"`
	ProviderCapabilityLimit    int `json:"providerCapabilityLimit"`
	UpstreamAvailability       int `json:"upstreamAvailability"`
	NoWorkingAttempt           int `json:"noWorkingAttempt"`
	FixedByRepairVariant       int `json:"fixedByRepairVariant"`
	AvailabilityOKAfterFailure int `json:"availabilityOKAfterFailure"`
}

type config struct {
	routes             []string
	models             map[string]bool
	repair             bool
	includeUnavailable bool
	timeout            time.Duration
	caseTimeout        time.Duration
	codexOAuth         bool
	codexOAuthBrowser  bool
	handoff            bool
	structuredOutput   bool
	images             bool
}

type routeCredential struct {
	apiKey      string
	accessToken string
	projectID   string
	location    string
	codex       *openai.CodexOAuthCredentials
}

type handoffSource struct {
	Route      routeSpec
	Model      sigma.Model
	Credential routeCredential
	Messages   []sigma.Message
}

var routes = map[string]routeSpec{
	"openai": {
		Name:             "openai",
		Provider:         sigma.ProviderOpenAI,
		BaseURL:          openai.DefaultBaseURL,
		APIKeyEnv:        "OPENAI_API_KEY",
		RegisterProvider: registerOpenAIResponsesProvider,
		Model:            discoveredOpenAIResponsesModel,
		Cases:            openAIResponsesProbeCases,
	},
	"openai-codex": {
		Name:             "openai-codex",
		Provider:         sigma.ProviderOpenAI,
		BaseURL:          "https://chatgpt.com/backend-api/codex",
		APIKeyEnv:        "OPENAI_CODEX_ACCESS_TOKEN",
		RegisterProvider: registerOpenAICodexProvider,
		Model:            discoveredOpenAICodexModel,
		Cases:            openAICodexProbeCases,
	},
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
	routeGoogleVertex: {
		Name:             routeGoogleVertex,
		Provider:         sigma.ProviderGoogleVertex,
		APIKeyEnv:        "GOOGLE_CLOUD_API_KEY",
		RegisterProvider: registerGoogleVertexProvider,
		Model:            googleVertexProbeModel,
		Cases:            googleVertexProbeCases,
	},
	"google-vertex-anthropic": {
		Name:             "google-vertex-anthropic",
		Provider:         sigma.ProviderGoogleVertexAnthropic,
		APIKeyEnv:        "GOOGLE_CLOUD_API_KEY",
		RegisterProvider: registerGoogleVertexAnthropicProvider,
		Model:            googleVertexAnthropicProbeModel,
		Cases:            anthropicCompatibleProbeCases,
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
		Provider:         sigma.ProviderFireworksAnthropic,
		BaseURL:          fireworks.DefaultBaseURL,
		ModelBaseURL:     fireworks.DefaultBaseURL,
		APIKeyEnv:        "FIREWORKS_API_KEY",
		RegisterProvider: registerFireworksAnthropicProvider,
		Model:            discoveredFireworksAnthropicModel,
		Cases:            anthropicCompatibleProbeCases,
	},
	"moonshot": {
		Name:             "moonshot",
		Provider:         sigma.ProviderMoonshotAI,
		BaseURL:          moonshot.DefaultBaseURL,
		APIKeyEnv:        "MOONSHOT_API_KEY",
		RegisterProvider: registerMoonshotProvider,
		Model:            discoveredMoonshotModel,
		Cases:            openAICompatibleProbeCases,
	},
	"moonshot-cn": {
		Name:             "moonshot-cn",
		Provider:         sigma.ProviderMoonshotAICN,
		BaseURL:          moonshot.DefaultCNBaseURL,
		APIKeyEnv:        "MOONSHOT_API_KEY",
		RegisterProvider: registerMoonshotProvider,
		Model:            discoveredMoonshotModel,
		Cases:            openAICompatibleProbeCases,
	},
	"nvidia": {
		Name:             "nvidia",
		Provider:         sigma.ProviderNVIDIA,
		BaseURL:          nvidia.DefaultBaseURL,
		APIKeyEnv:        "NVIDIA_API_KEY",
		RegisterProvider: registerNVIDIAProvider,
		Model:            discoveredNVIDIAModel,
		Cases:            openAICompatibleProbeCases,
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

var imageRoutes = map[string]imageRouteSpec{
	"google": {
		Name:             "google",
		Provider:         sigma.ProviderGoogle,
		BaseURL:          google.DefaultBaseURL,
		APIKeyEnv:        "GOOGLE_API_KEY",
		RegisterProvider: registerGoogleImagesProvider,
		Model:            googleImageProbeModel,
		Cases:            googleImageProbeCases,
	},
	routeGoogleVertex: {
		Name:             routeGoogleVertex,
		Provider:         sigma.ProviderGoogleVertex,
		APIKeyEnv:        "GOOGLE_CLOUD_API_KEY",
		RegisterProvider: registerGoogleVertexImagesProvider,
		Model:            googleImageProbeModel,
		Cases:            googleImageProbeCases,
	},
	"openai": {
		Name:             "openai",
		Provider:         sigma.ProviderOpenAI,
		BaseURL:          openai.DefaultBaseURL,
		APIKeyEnv:        "OPENAI_API_KEY",
		RegisterProvider: registerOpenAIImagesProvider,
		Model:            openAIImageProbeModel,
		Cases:            openAIImageProbeCases,
	},
}

const (
	jsonTypeKey                          = "type"
	routeGoogleVertex                    = "google-vertex"
	outcomeSkipped                       = "skipped"
	defaultRouteList                     = "zen,go"
	defaultOpenAICodexProbeModel         = "gpt-5.5"
	defaultGoogleVertexAnthropicModel    = "claude-sonnet-4-6"
	defaultNVIDIAProbeModel              = "nvidia/nemotron-3-super-120b-a12b"
	defaultGoogleGeminiImageProbeModel   = "gemini-2.5-flash-image"
	defaultGoogleCurrentImageProbeModel  = "gemini-3.1-flash-image"
	defaultOpenAIImageProbeModel         = "gpt-image-1"
	defaultOpenAIImageVariationModel     = "dall-e-2"
	defaultOpenAIResponsesToolProbeModel = "gpt-5.5"
	defaultCaseTimeout                   = time.Minute
)

func main() {
	cfg := parseConfig()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	writer := bufio.NewWriter(os.Stdout)
	defer func() {
		_ = writer.Flush()
	}()

	var totals summary
	var recommendations []probeRecommendation
	if cfg.handoff {
		runHandoffProbes(ctx, cfg, func(result probeResult) {
			totals.add(result)
			if recommendation, ok := recommendationFor(result); ok {
				recommendations = append(recommendations, recommendation)
			}
			writeResult(writer, result)
			_ = writer.Flush()
		})
		writeSummary(writer, totals, recommendations)
		_ = writer.Flush()
		return
	}
	if cfg.images {
		runImageProbes(ctx, cfg, func(result probeResult) {
			totals.add(result)
			if recommendation, ok := recommendationFor(result); ok {
				recommendations = append(recommendations, recommendation)
			}
			writeResult(writer, result)
			_ = writer.Flush()
		})
		writeSummary(writer, totals, recommendations)
		_ = writer.Flush()
		return
	}

	for _, routeName := range cfg.routes {
		route, ok := routes[routeName]
		if !ok {
			fatalf("unknown route %q", routeName)
		}
		credential, err := credentialForRoute(ctx, route, cfg)
		if err != nil {
			fatalf("%v", err)
		}
		models, err := modelsForRoute(ctx, route, credential, cfg.models)
		if err != nil {
			fatalf("discover %s models: %v", route.Name, err)
		}
		for _, modelID := range models {
			if len(cfg.models) > 0 && !cfg.models[modelID] {
				continue
			}
			probeModelEach(ctx, route, modelID, credential, cfg, func(result probeResult) {
				totals.add(result)
				if recommendation, ok := recommendationFor(result); ok {
					recommendations = append(recommendations, recommendation)
				}
				writeResult(writer, result)
				_ = writer.Flush()
			})
		}
	}
	writeSummary(writer, totals, recommendations)
	_ = writer.Flush()
}

func parseConfig() config {
	var routeList string
	var modelList string
	var timeout time.Duration
	var caseTimeout time.Duration
	var repair bool
	var includeUnavailable bool
	var codexOAuth bool
	var codexOAuthBrowser bool
	var handoff bool
	var structuredOutput bool
	var images bool
	flag.StringVar(&routeList, "routes", defaultRouteList, "comma-separated routes: openai,openai-codex,zen,go,google-vertex,google-vertex-anthropic,fireworks-openai,fireworks-anthropic,moonshot,moonshot-cn,nvidia,xai")
	flag.StringVar(&modelList, "models", "", "comma-separated model IDs to probe")
	flag.BoolVar(&repair, "repair", false, "try targeted repair variants after a failing case")
	flag.BoolVar(&includeUnavailable, "include-unavailable", false, "run known unavailable advertised models instead of skipping them")
	flag.BoolVar(&codexOAuth, "codex-oauth", false, "run OpenAI Codex device-code OAuth for the openai-codex route")
	flag.BoolVar(&codexOAuthBrowser, "codex-oauth-browser", false, "run OpenAI Codex browser callback OAuth for the openai-codex route")
	flag.BoolVar(&handoff, "handoff", false, "run cross-provider replay handoff diagnostics instead of per-route surface cases")
	flag.BoolVar(&structuredOutput, "structured-output", false, "run focused OpenAI-compatible JSON object and JSON Schema capability probes")
	flag.BoolVar(&images, "images", false, "run focused image-generation surface probes")
	flag.DurationVar(&timeout, "timeout", 10*time.Minute, "overall probe timeout")
	flag.DurationVar(&caseTimeout, "case-timeout", defaultCaseTimeout, "maximum duration for one probe case or repair attempt; 0 uses only the overall timeout")
	flag.Parse()
	if images && routeList == defaultRouteList {
		routeList = "openai"
	}

	return config{
		routes:             splitCSV(routeList),
		models:             setFromCSV(modelList),
		repair:             repair,
		includeUnavailable: includeUnavailable,
		timeout:            timeout,
		caseTimeout:        caseTimeout,
		codexOAuth:         codexOAuth,
		codexOAuthBrowser:  codexOAuthBrowser,
		handoff:            handoff,
		structuredOutput:   structuredOutput,
		images:             images,
	}
}

func credentialForRoute(ctx context.Context, route routeSpec, cfg config) (routeCredential, error) {
	if route.Name == "openai-codex" {
		return openAICodexCredential(ctx, cfg)
	}
	if isGoogleVertexRoute(route) {
		return googleVertexCredential()
	}
	apiKey := os.Getenv(route.APIKeyEnv)
	if apiKey == "" {
		return routeCredential{}, fmt.Errorf("%s is required for live %s probing", route.APIKeyEnv, route.Name)
	}
	return routeCredential{apiKey: apiKey}, nil
}

func googleVertexCredential() (routeCredential, error) {
	projectID := firstEnvironmentValue("GOOGLE_CLOUD_PROJECT", "GCLOUD_PROJECT")
	if projectID == "" {
		return routeCredential{}, fmt.Errorf("GOOGLE_CLOUD_PROJECT or GCLOUD_PROJECT is required for live google-vertex probing")
	}
	location := firstEnvironmentValue("GOOGLE_CLOUD_LOCATION", "GOOGLE_CLOUD_REGION")
	if location == "" {
		return routeCredential{}, fmt.Errorf("GOOGLE_CLOUD_LOCATION or GOOGLE_CLOUD_REGION is required for live google-vertex probing")
	}

	credential := routeCredential{projectID: projectID, location: location}
	if credential.accessToken = os.Getenv("GOOGLE_CLOUD_ACCESS_TOKEN"); credential.accessToken != "" {
		return credential, nil
	}
	credential.apiKey = firstEnvironmentValue("GOOGLE_CLOUD_API_KEY", "GOOGLE_API_KEY")
	if credential.apiKey == "" {
		return routeCredential{}, fmt.Errorf("GOOGLE_CLOUD_ACCESS_TOKEN, GOOGLE_CLOUD_API_KEY, or GOOGLE_API_KEY is required for live google-vertex probing")
	}
	return credential, nil
}

func firstEnvironmentValue(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func openAICodexCredential(ctx context.Context, cfg config) (routeCredential, error) {
	if cfg.codexOAuth && cfg.codexOAuthBrowser {
		return routeCredential{}, fmt.Errorf("use only one of -codex-oauth or -codex-oauth-browser")
	}
	if cfg.codexOAuthBrowser {
		credentials, err := openai.LoginOpenAICodexBrowser(ctx, openai.CodexBrowserLoginOptions{
			OnAuth: func(info openai.CodexBrowserAuthInfo) {
				_, _ = fmt.Fprintf(os.Stderr, "%s\n%s\n", info.Instructions, info.URL)
			},
			OnManualCode: func(ctx context.Context, prompt openai.CodexBrowserManualPrompt) (string, error) {
				_, _ = fmt.Fprintln(os.Stderr, prompt.Message)
				return readLineContext(ctx, os.Stdin)
			},
		})
		if err != nil {
			return routeCredential{}, fmt.Errorf("openai codex browser oauth: %w", err)
		}
		return routeCredential{apiKey: credentials.AccessToken, codex: &credentials}, nil
	}
	if cfg.codexOAuth {
		credentials, err := openai.LoginOpenAICodexDeviceCode(ctx, openai.CodexDeviceCodeLoginOptions{
			OnDeviceCode: func(info openai.CodexDeviceCodeInfo) {
				_, _ = fmt.Fprintf(os.Stderr, "Open %s and enter code %s\n", info.VerificationURI, info.UserCode)
			},
		})
		if err != nil {
			return routeCredential{}, fmt.Errorf("openai codex oauth: %w", err)
		}
		return routeCredential{apiKey: credentials.AccessToken, codex: &credentials}, nil
	}
	accessToken := os.Getenv("OPENAI_CODEX_ACCESS_TOKEN")
	if accessToken != "" {
		return routeCredential{
			apiKey: accessToken,
			codex:  &openai.CodexOAuthCredentials{AccessToken: accessToken},
		}, nil
	}
	refreshToken := os.Getenv("OPENAI_CODEX_REFRESH_TOKEN")
	if refreshToken != "" {
		credentials, err := openai.RefreshOpenAICodexToken(ctx, refreshToken, openai.CodexOAuthTokenProviderOptions{})
		if err != nil {
			return routeCredential{}, fmt.Errorf("openai codex oauth refresh: %w", err)
		}
		return routeCredential{apiKey: credentials.AccessToken, codex: &credentials}, nil
	}
	return routeCredential{}, fmt.Errorf("OPENAI_CODEX_ACCESS_TOKEN, OPENAI_CODEX_REFRESH_TOKEN, -codex-oauth, or -codex-oauth-browser is required for live openai-codex probing")
}

func readLineContext(ctx context.Context, reader io.Reader) (string, error) {
	type result struct {
		value string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(reader).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			done <- result{err: err}
			return
		}
		done <- result{value: strings.TrimSpace(line)}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-done:
		return result.value, result.err
	}
}

func modelsForRoute(ctx context.Context, route routeSpec, credential routeCredential, selected map[string]bool) ([]string, error) {
	if len(selected) > 0 {
		models := make([]string, 0, len(selected))
		for modelID := range selected {
			switch route.Name {
			case routeGoogleVertex:
				model, ok := sigma.GetModel(route.Provider, sigma.ModelID(modelID))
				if !ok || !isBuiltInGoogleVertexGeminiModel(model) {
					return nil, fmt.Errorf("model %q is not a built-in google-vertex text model", modelID)
				}
			case "google-vertex-anthropic":
				model, ok := sigma.GetModel(route.Provider, sigma.ModelID(modelID))
				if !ok || !isBuiltInGoogleVertexAnthropicModel(model) {
					return nil, fmt.Errorf("model %q is not a built-in google-vertex-anthropic text model", modelID)
				}
			}
			models = append(models, modelID)
		}
		sort.Strings(models)
		return models, nil
	}
	if route.Name == "openai-codex" {
		return []string{defaultOpenAICodexProbeModel}, nil
	}
	if route.Name == "nvidia" {
		return []string{defaultNVIDIAProbeModel}, nil
	}
	if route.Name == "google-vertex-anthropic" {
		return []string{defaultGoogleVertexAnthropicModel}, nil
	}
	if route.Name == routeGoogleVertex {
		models := make([]string, 0)
		for _, model := range sigma.Models() {
			if isBuiltInGoogleVertexGeminiModel(model) {
				models = append(models, string(model.ID))
			}
		}
		sort.Strings(models)
		if len(models) == 0 {
			return nil, fmt.Errorf("no built-in google-vertex text models are registered")
		}
		return models, nil
	}
	return discoverModels(ctx, route, credential.apiKey)
}

func isBuiltInGoogleVertexGeminiModel(model sigma.Model) bool {
	family, _ := model.ProviderMetadata["modelFamily"].(string)
	return model.Provider == sigma.ProviderGoogleVertex && model.API == sigma.APIGoogleVertex && family == "gemini"
}

func isBuiltInGoogleVertexAnthropicModel(model sigma.Model) bool {
	family, _ := model.ProviderMetadata["modelFamily"].(string)
	publisher, _ := model.ProviderMetadata["vertexPublisher"].(string)
	return model.Provider == sigma.ProviderGoogleVertexAnthropic &&
		model.API == sigma.APIAnthropicMessages &&
		family == "claude" &&
		publisher == "anthropic"
}

func isGoogleVertexRoute(route routeSpec) bool {
	return route.Name == routeGoogleVertex || route.Name == "google-vertex-anthropic"
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

func probeModelEach(ctx context.Context, route routeSpec, modelID string, credential routeCredential, cfg config, emit func(probeResult)) {
	if !cfg.includeUnavailable && knownUnavailable(route.Name, modelID) {
		emit(probeResult{
			Route:   route.Name,
			Model:   modelID,
			Case:    "all",
			Attempt: "skip_known_unavailable",
			Outcome: outcomeSkipped,
		})
		return
	}

	model := route.Model(route, modelID)
	if cfg.structuredOutput && model.API != sigma.APIOpenAICompletions {
		emit(probeResult{
			Route:   route.Name,
			Model:   modelID,
			Case:    "structured_output",
			Attempt: "unsupported_api",
			Outcome: outcomeSkipped,
		})
		return
	}

	client := probeClient(route, modelID)
	cases := route.Cases(route, model)
	if cfg.structuredOutput {
		cases = structuredOutputProbeCases(cases)
	}
	for _, testCase := range cases {
		result := runCaseWithTimeout(ctx, cfg.caseTimeout, route, client, model, testCase, credential, testCase.Name)
		if result.Outcome == "ok" {
			emit(annotateStructuredOutputResult(cfg, result))
			continue
		}
		if result.Outcome == "upstream_availability" {
			emit(result)
			continue
		}
		if !cfg.repair && !cfg.structuredOutput {
			emit(result)
			continue
		}
		repaired := result
		repairedByVariant := false
		availability := probeResult{}
		failedAttempts := []failedAttempt{{Attempt: result.Attempt, Error: result.Error}}
		for _, variant := range repairVariants(route, testCase) {
			attempt := runCaseWithTimeout(ctx, cfg.caseTimeout, route, client, model, variant, credential, variant.Name)
			if attempt.Outcome == "ok" {
				attempt.Case = testCase.Name
				attempt.OriginalError = result.Error
				attempt.FailedAttempts = append([]failedAttempt(nil), failedAttempts...)
				attempt.Hint = repairHint(testCase.Name, attempt.Attempt)
				if attempt.Attempt == "minimal_basic_text" {
					attempt.AvailabilityOKAfterFailure = true
					availability = attempt
					continue
				}
				attempt.Outcome = "fixed_by_repair_variant"
				repaired = attempt
				repairedByVariant = true
				break
			}
			failedAttempts = append(failedAttempts, failedAttempt{Attempt: attempt.Attempt, Error: attempt.Error})
			if availability.Outcome != "" {
				availability.FailedAttempts = append([]failedAttempt(nil), failedAttempts...)
			}
		}
		if !repairedByVariant && availability.Outcome != "" {
			repaired.AvailabilityOKAfterFailure = true
			repaired.FailedAttempts = append([]failedAttempt(nil), availability.FailedAttempts...)
			repaired.Hint = availability.Hint
		}
		repaired = annotateStructuredOutputResult(cfg, repaired)
		emit(repaired)
	}
}

func structuredOutputProbeCases(cases []probeCase) []probeCase {
	out := make([]probeCase, 0, 2)
	for _, testCase := range cases {
		switch testCase.Name {
		case "json_object", "json_schema":
			out = append(out, testCase)
		}
	}
	return out
}

func annotateStructuredOutputResult(cfg config, result probeResult) probeResult {
	if !cfg.structuredOutput || result.Hint != "" || result.Outcome != "ok" {
		return result
	}
	switch result.Case {
	case "json_object":
		result.Hint = "json_object_supported"
	case "json_schema":
		result.Hint = "json_schema_supported"
	}
	return result
}

func runHandoffProbes(ctx context.Context, cfg config, emit func(probeResult)) {
	sources := make([]handoffSource, 0)
	for _, routeName := range cfg.routes {
		route, ok := routes[routeName]
		if !ok {
			emit(probeResult{
				Route:   routeName,
				Case:    "handoff_source",
				Attempt: "route",
				Outcome: outcomeSkipped,
				Error:   fmt.Sprintf("unknown route %q", routeName),
			})
			continue
		}
		credential, err := credentialForRoute(ctx, route, cfg)
		if err != nil {
			emit(probeResult{
				Route:   route.Name,
				Case:    "handoff_source",
				Attempt: "credential",
				Outcome: outcomeSkipped,
				Error:   err.Error(),
			})
			continue
		}
		models, err := modelsForRoute(ctx, route, credential, cfg.models)
		if err != nil {
			emit(probeResult{
				Route:   route.Name,
				Case:    "handoff_source",
				Attempt: "model_discovery",
				Outcome: "no_working_attempt",
				Error:   err.Error(),
			})
			continue
		}
		for _, modelID := range models {
			source, result := generateHandoffSource(ctx, route, modelID, credential, cfg)
			emit(result)
			if result.Outcome == "ok" {
				sources = append(sources, source)
			}
		}
	}

	for _, source := range sources {
		for _, target := range sources {
			if source.Route.Name == target.Route.Name && source.Model.ID == target.Model.ID {
				continue
			}
			emit(runHandoffTarget(ctx, source, target))
		}
	}
}

func runImageProbes(ctx context.Context, cfg config, emit func(probeResult)) {
	for _, routeName := range cfg.routes {
		route, ok := imageRoutes[routeName]
		if !ok {
			emit(probeResult{
				Route:   routeName,
				Case:    "images",
				Attempt: "route",
				Outcome: outcomeSkipped,
				Error:   fmt.Sprintf("unknown image route %q", routeName),
			})
			continue
		}
		cases, skipped := selectedImageProbeCases(route, cfg.models)
		for _, result := range skipped {
			emit(result)
		}
		if len(cases) == 0 {
			continue
		}

		credential, err := credentialForImageRoute(route)
		if err != nil {
			emit(probeResult{
				Route:   route.Name,
				Case:    "images",
				Attempt: "credential",
				Outcome: outcomeSkipped,
				Error:   err.Error(),
			})
			continue
		}
		for _, testCase := range cases {
			emit(runImageCaseWithTimeout(ctx, cfg.caseTimeout, route, testCase, credential))
		}
	}
}

func credentialForImageRoute(route imageRouteSpec) (routeCredential, error) {
	switch route.Name {
	case "google":
		apiKey := firstEnvironmentValue("GOOGLE_API_KEY", "GOOGLE_CLOUD_API_KEY")
		if apiKey == "" {
			return routeCredential{}, fmt.Errorf("GOOGLE_API_KEY or GOOGLE_CLOUD_API_KEY is required for live google image probing")
		}
		return routeCredential{apiKey: apiKey}, nil
	case routeGoogleVertex:
		return googleVertexCredential()
	}

	apiKey := os.Getenv(route.APIKeyEnv)
	if apiKey == "" {
		return routeCredential{}, fmt.Errorf("%s is required for live %s image probing", route.APIKeyEnv, route.Name)
	}
	return routeCredential{apiKey: apiKey}, nil
}

func selectedImageProbeCases(route imageRouteSpec, selected map[string]bool) ([]imageProbeCase, []probeResult) {
	cases := route.Cases(route)
	if len(selected) == 0 {
		return cases, nil
	}

	selectedCases := make([]imageProbeCase, 0, len(cases))
	for _, testCase := range cases {
		if imageProbeCaseSelected(testCase, selected) {
			selectedCases = append(selectedCases, testCase)
		}
	}
	if route.Name != "google" && route.Name != routeGoogleVertex {
		return selectedCases, nil
	}

	invalid := make([]string, 0)
	for modelID := range selected {
		if !googleImageModelMatchesRoute(route, modelID) {
			invalid = append(invalid, modelID)
		}
	}
	sort.Strings(invalid)
	skipped := make([]probeResult, 0, len(invalid))
	for _, modelID := range invalid {
		skipped = append(skipped, probeResult{
			Route:   route.Name,
			Model:   modelID,
			Case:    "images",
			Attempt: "model_selection",
			Outcome: outcomeSkipped,
			Error:   fmt.Sprintf("model %q is not a built-in %s image model", modelID, route.Name),
		})
	}
	return selectedCases, skipped
}

func imageProbeCaseSelected(testCase imageProbeCase, selected map[string]bool) bool {
	if len(selected) == 0 {
		return true
	}
	return selected[testCase.ModelID] || selected[testCase.ResponsesModelID]
}

func runImageCase(ctx context.Context, route imageRouteSpec, testCase imageProbeCase, credential routeCredential) probeResult {
	if testCase.ResponsesTool {
		return runResponsesImageToolCase(ctx, route, testCase, credential)
	}

	model := route.Model(route, testCase.ModelID)
	result := probeResult{
		Route:   route.Name,
		Model:   string(model.ID),
		Case:    testCase.Name,
		Attempt: testCase.Name,
	}
	client := imageProbeClient(route, model.ID)
	options := append(imageAuthOptions(route, credential), testCase.Options...)
	var images sigma.AssistantImages
	var err error
	partialSeen := false
	if testCase.Stream {
		images, partialSeen, err = collectImageProbeStream(ctx, client.StreamImages(ctx, model, testCase.Request, options...))
	} else {
		images, err = client.GenerateImages(ctx, model, testCase.Request, options...)
	}
	if err != nil {
		result.Outcome = classifyImageFailure(route, model, err)
		result.Error = err.Error()
		return result
	}
	if testCase.RequireImage && !hasGeneratedImage(images) {
		result.Outcome = "no_working_attempt"
		result.Error = "image response did not include a non-empty base64 or URL image"
		return result
	}
	if testCase.RequirePartial && !partialSeen {
		result.Outcome = "no_working_attempt"
		result.Error = "image stream did not include a partial image"
		return result
	}
	result.Outcome = "ok"
	result.Hint = testCase.SuccessHint
	return result
}

func runImageCaseWithTimeout(ctx context.Context, timeout time.Duration, route imageRouteSpec, testCase imageProbeCase, credential routeCredential) probeResult {
	if timeout <= 0 {
		return runImageCase(ctx, route, testCase, credential)
	}

	caseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return runImageCase(caseCtx, route, testCase, credential)
}

func runResponsesImageToolCase(ctx context.Context, imageRoute imageRouteSpec, testCase imageProbeCase, credential routeCredential) probeResult {
	textRoute := routes["openai"]
	textRoute.BaseURL = imageRoute.BaseURL
	model := textRoute.Model(textRoute, testCase.ResponsesModelID)
	result := probeResult{
		Route:   imageRoute.Name,
		Model:   string(model.ID),
		Case:    testCase.Name,
		Attempt: testCase.Name,
	}
	client := probeClient(textRoute, string(model.ID))
	options := append(authOptions(textRoute, credential), testCase.ResponsesOptions...)
	final, err := client.Complete(ctx, model, testCase.ResponsesRequest, options...)
	if err != nil {
		result.Outcome = classifyFailure(textRoute, model, err)
		result.Error = err.Error()
		return result
	}
	if testCase.RequireToolOutput && !hasImageOutputBlock(final) {
		result.Outcome = "no_working_attempt"
		result.Error = "response did not include image generation output"
		return result
	}
	result.Outcome = "ok"
	result.Hint = testCase.SuccessHint
	return result
}

func collectImageProbeStream(ctx context.Context, stream *sigma.ImageStream) (sigma.AssistantImages, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	partialSeen := false
	events := stream.Events()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				final, _ := stream.Final()
				if err := stream.Err(); err != nil {
					return final, partialSeen, fmt.Errorf("collect image stream: %w", err)
				}
				return final, partialSeen, nil
			}
			if event.Kind == sigma.ImageEventKindPartial && event.PartialImage != nil {
				partialSeen = true
			}
			switch event.Kind { //nolint:exhaustive
			case sigma.ImageEventKindDone:
				final, _ := stream.Final()
				if event.FinalImages != nil {
					final = *event.FinalImages
				}
				return final, partialSeen, nil
			case sigma.ImageEventKindError:
				final, _ := stream.Final()
				if event.FinalImages != nil {
					final = *event.FinalImages
				}
				err := stream.Err()
				if err == nil && event.Error != "" {
					err = errors.New(event.Error)
				}
				if err != nil {
					err = fmt.Errorf("collect image stream: %w", err)
				}
				return final, partialSeen, err
			}
		case <-ctx.Done():
			stream.Close()
			final, _ := stream.Final()
			return final, partialSeen, ctx.Err()
		}
	}
}

func hasImageOutputBlock(final sigma.AssistantMessage) bool {
	for _, block := range final.Content {
		if block.Type == sigma.ContentBlockImage && (block.Data != "" || block.URL != "") {
			return true
		}
	}
	return false
}

func hasGeneratedImage(images sigma.AssistantImages) bool {
	for _, image := range images.Images {
		if image.Type != sigma.ImageInputImage {
			continue
		}
		if image.Source == sigma.ImageSourceBase64 && image.Data != "" {
			return true
		}
		if image.Source == sigma.ImageSourceURL && image.URL != "" {
			return true
		}
	}
	return false
}

func imageProbeClient(route imageRouteSpec, modelID sigma.ModelID) *sigma.Client {
	registry := sigma.NewRegistry()
	_ = route.RegisterProvider(registry, route)
	_ = registry.RegisterImageModel(route.Model(route, string(modelID)))
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func generateHandoffSource(ctx context.Context, route routeSpec, modelID string, credential routeCredential, cfg config) (handoffSource, probeResult) {
	model := route.Model(route, modelID)
	result := probeResult{
		Route:   route.Name,
		Model:   string(model.ID),
		Case:    "handoff_source",
		Attempt: "source_context",
	}
	if !cfg.includeUnavailable && knownUnavailable(route.Name, modelID) {
		result.Attempt = "skip_known_unavailable"
		result.Outcome = outcomeSkipped
		return handoffSource{}, result
	}

	client := probeClient(route, modelID)
	user := sigma.UserText("Use the double_number tool with value 21, then wait for the tool result.")
	req := sigma.Request{
		SystemPrompt: "You are a handoff probe source. Use tools when requested.",
		Messages:     []sigma.Message{user},
		Tools:        handoffTools(),
	}
	options := append(authOptions(route, credential), sigma.WithMaxTokens(512))
	first, err := client.Complete(ctx, model, req, options...)
	if err != nil {
		result.Outcome = classifyFailure(route, model, err)
		result.Error = err.Error()
		return handoffSource{}, result
	}
	toolCall, ok := firstToolCall(first)
	if !ok {
		result.Outcome = outcomeSkipped
		result.Error = "source response did not emit a tool call"
		return handoffSource{}, result
	}

	assistant := assistantMessage(model, first)
	toolResult := sigma.Message{
		Role:       sigma.RoleTool,
		ToolCallID: toolCall.ToolCallID,
		ToolName:   toolCall.ToolName,
		Content:    []sigma.ContentBlock{sigma.Text("42")},
	}
	messages := make([]sigma.Message, 0, 4)
	messages = append(messages, user, assistant, toolResult)
	final, err := client.Complete(ctx, model, sigma.Request{
		SystemPrompt: "You are a handoff probe source. Answer after tool results.",
		Messages:     messages,
		Tools:        handoffTools(),
	}, options...)
	if err != nil {
		result.Attempt = "source_final"
		result.Outcome = classifyFailure(route, model, err)
		result.Error = err.Error()
		return handoffSource{}, result
	}

	messages = append(messages, assistantMessage(model, final))
	result.Outcome = "ok"
	return handoffSource{
		Route:      route,
		Model:      model,
		Credential: credential,
		Messages:   messages,
	}, result
}

func runHandoffTarget(ctx context.Context, source handoffSource, target handoffSource) probeResult {
	client := probeClient(target.Route, string(target.Model.ID))
	messages := append([]sigma.Message(nil), source.Messages...)
	messages = append(messages, sigma.UserText("Great, thanks. Reply with exactly: Hello, handoff successful."))
	result := probeResult{
		Route:       target.Route.Name,
		Model:       string(target.Model.ID),
		Case:        "handoff_replay",
		Attempt:     "target_replay",
		SourceRoute: source.Route.Name,
		SourceModel: string(source.Model.ID),
	}
	options := append(authOptions(target.Route, target.Credential), sigma.WithMaxTokens(512))
	_, err := client.Complete(ctx, target.Model, sigma.Request{
		Messages: messages,
		Tools:    handoffTools(),
	}, options...)
	if err != nil {
		result.Outcome = classifyFailure(target.Route, target.Model, err)
		result.Error = err.Error()
		return result
	}
	result.Outcome = "ok"
	return result
}

func handoffTools() []sigma.Tool {
	return []sigma.Tool{{
		Name:        "double_number",
		Description: "Doubles a number and returns the result.",
		InputSchema: sigma.Schema{
			jsonTypeKey: "object",
			"properties": map[string]any{
				"value": map[string]any{jsonTypeKey: "number"},
			},
			"required": []any{"value"},
		},
	}}
}

func firstToolCall(message sigma.AssistantMessage) (sigma.ContentBlock, bool) {
	for _, block := range message.Content {
		if block.Type == sigma.ContentBlockToolCall && block.ToolCallID != "" && block.ToolName != "" {
			return block, true
		}
	}
	return sigma.ContentBlock{}, false
}

func assistantMessage(model sigma.Model, final sigma.AssistantMessage) sigma.Message {
	provider := final.Provider
	if provider == "" {
		provider = model.Provider
	}
	modelID := final.Model
	if modelID == "" {
		modelID = model.ID
	}
	return sigma.Message{
		Role:       sigma.RoleAssistant,
		Content:    append([]sigma.ContentBlock(nil), final.Content...),
		Provider:   provider,
		API:        model.API,
		Model:      modelID,
		StopReason: final.StopReason,
	}
}

func probeClient(route routeSpec, modelID string) *sigma.Client {
	registry := sigma.NewRegistry()
	_ = route.RegisterProvider(registry, route)
	_ = registry.RegisterModel(route.Model(route, modelID))
	return sigma.NewClient(sigma.WithRegistry(registry))
}

func registerOpenAIResponsesProvider(registry *sigma.Registry, route routeSpec) error {
	if err := openai.RegisterResponses(registry, route.Provider, openai.WithBaseURL(route.BaseURL)); err != nil {
		return fmt.Errorf("register openai responses provider: %w", err)
	}
	return nil
}

func registerOpenAIImagesProvider(registry *sigma.Registry, route imageRouteSpec) error {
	if err := openai.RegisterImages(registry, route.Provider, openai.WithBaseURL(route.BaseURL)); err != nil {
		return fmt.Errorf("register openai images provider: %w", err)
	}
	return nil
}

func registerGoogleImagesProvider(registry *sigma.Registry, route imageRouteSpec) error {
	options := make([]google.ProviderOption, 0, 1)
	if route.BaseURL != "" {
		options = append(options, google.WithBaseURL(route.BaseURL))
	}
	if err := google.RegisterImages(registry, route.Provider, options...); err != nil {
		return fmt.Errorf("register google images provider: %w", err)
	}
	return nil
}

func registerGoogleVertexImagesProvider(registry *sigma.Registry, route imageRouteSpec) error {
	options := make([]google.VertexProviderOption, 0, 1)
	if route.BaseURL != "" {
		options = append(options, google.WithVertexBaseURL(route.BaseURL))
	}
	if err := google.RegisterVertexImages(registry, route.Provider, options...); err != nil {
		return fmt.Errorf("register google vertex images provider: %w", err)
	}
	return nil
}

func registerOpenAICodexProvider(registry *sigma.Registry, route routeSpec) error {
	if err := openai.RegisterCodexResponses(registry, route.Provider, openai.WithBaseURL(route.BaseURL)); err != nil {
		return fmt.Errorf("register openai codex responses provider: %w", err)
	}
	return nil
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

func registerGoogleVertexProvider(registry *sigma.Registry, route routeSpec) error {
	options := make([]google.VertexProviderOption, 0, 1)
	if route.BaseURL != "" {
		options = append(options, google.WithVertexBaseURL(route.BaseURL))
	}
	if err := google.RegisterVertex(registry, route.Provider, options...); err != nil {
		return fmt.Errorf("register google vertex provider: %w", err)
	}
	return nil
}

func registerGoogleVertexAnthropicProvider(registry *sigma.Registry, route routeSpec) error {
	options := make([]anthropic.VertexProviderOption, 0, 1)
	if route.BaseURL != "" {
		options = append(options, anthropic.WithVertexBaseURL(route.BaseURL))
	}
	if err := anthropic.RegisterVertex(registry, route.Provider, options...); err != nil {
		return fmt.Errorf("register google vertex anthropic provider: %w", err)
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
	if err := fireworks.RegisterAnthropic(registry, fireworks.WithAnthropicBaseURL(route.BaseURL)); err != nil {
		return fmt.Errorf("register fireworks anthropic-compatible provider: %w", err)
	}
	return nil
}

func registerMoonshotProvider(registry *sigma.Registry, route routeSpec) error {
	switch route.Provider {
	case sigma.ProviderMoonshotAICN:
		if err := moonshot.RegisterCN(registry, moonshot.WithBaseURL(route.BaseURL)); err != nil {
			return fmt.Errorf("register moonshot cn provider: %w", err)
		}
	default:
		if err := moonshot.Register(registry, moonshot.WithBaseURL(route.BaseURL)); err != nil {
			return fmt.Errorf("register moonshot provider: %w", err)
		}
	}
	return nil
}

func registerNVIDIAProvider(registry *sigma.Registry, route routeSpec) error {
	if err := nvidia.Register(registry, nvidia.WithBaseURL(route.BaseURL)); err != nil {
		return fmt.Errorf("register nvidia provider: %w", err)
	}
	return nil
}

func registerXAIProvider(registry *sigma.Registry, route routeSpec) error {
	if err := xai.Register(registry, xai.WithBaseURL(route.BaseURL)); err != nil {
		return fmt.Errorf("register xai provider: %w", err)
	}
	return nil
}

func openAIImageProbeModel(route imageRouteSpec, id string) sigma.ImageModel {
	return sigma.ImageModel{
		ID:               sigma.ModelID(id),
		Provider:         route.Provider,
		API:              sigma.ImageAPIOpenAIImages,
		Name:             id,
		SupportedSizes:   []string{string(sigma.ImageSize1024x1024)},
		SupportedFormats: []string{"image/png"},
		ProviderMetadata: map[string]any{
			"baseURL":         route.BaseURL,
			"apiKeyEnvVars":   []string{route.APIKeyEnv},
			"modelFamily":     imageModelFamily(id),
			"probeDiscovered": true,
			"probeRoute":      route.Name,
			"probeSurface":    "openai-images",
		},
	}
}

func googleImageProbeModel(route imageRouteSpec, id string) sigma.ImageModel {
	model, ok := sigma.GetImageModel(route.Provider, sigma.ModelID(id))
	if !ok || !googleImageModelMatchesRoute(route, id) {
		return sigma.ImageModel{
			ID:               sigma.ModelID(id),
			Provider:         route.Provider,
			API:              googleImageAPI(route),
			Name:             id,
			SupportedSizes:   []string{"1:1"},
			SupportedFormats: []string{"image/png"},
		}
	}
	if route.BaseURL != "" {
		metadata := make(map[string]any, len(model.ProviderMetadata))
		for key, value := range model.ProviderMetadata {
			metadata[key] = value
		}
		metadata["baseURL"] = route.BaseURL
		model.ProviderMetadata = metadata
	}
	return model
}

func googleImageModelMatchesRoute(route imageRouteSpec, id string) bool {
	model, ok := sigma.GetImageModel(route.Provider, sigma.ModelID(id))
	return ok && model.API == googleImageAPI(route)
}

func googleImageAPI(route imageRouteSpec) sigma.ImageAPI {
	if route.Name == routeGoogleVertex {
		return sigma.ImageAPIGoogleVertexImages
	}
	return sigma.ImageAPIGoogleImages
}

func discoveredOpenAIResponsesModel(route routeSpec, id string) sigma.Model {
	return sigma.Model{
		ID:               sigma.ModelID(id),
		Provider:         route.Provider,
		API:              sigma.APIOpenAIResponses,
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:    true,
		SupportsThinking: true,
		ProviderMetadata: map[string]any{
			"baseURL":         route.BaseURL,
			"apiKeyEnvVars":   []string{route.APIKeyEnv},
			"modelFamily":     modelFamily(id),
			"probeDiscovered": true,
			"probeRoute":      route.Name,
			"probeSurface":    "openai-responses",
		},
	}
}

func discoveredOpenAICodexModel(route routeSpec, id string) sigma.Model {
	return sigma.Model{
		ID:               sigma.ModelID(id),
		Provider:         route.Provider,
		API:              sigma.APIOpenAICodexResponses,
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:    true,
		SupportsThinking: true,
		ThinkingLevelMap: map[sigma.ThinkingLevel]string{
			sigma.ThinkingLevelLow:    "low",
			sigma.ThinkingLevelMedium: "medium",
			sigma.ThinkingLevelHigh:   "high",
			"off":                     "",
		},
		ContextWindow:        400000,
		MaxOutputTokens:      128000,
		DefaultTransport:     sigma.TransportSSE,
		OpenAICodexResponses: &sigma.OpenAICodexResponsesConfig{},
		ProviderMetadata: map[string]any{
			"baseURL":         route.BaseURL,
			"apiKeyEnvVars":   []string{route.APIKeyEnv, "OPENAI_CODEX_REFRESH_TOKEN"},
			"modelFamily":     modelFamily(id),
			"probeDiscovered": true,
			"probeRoute":      route.Name,
			"probeSurface":    "openai-codex-responses",
			"requiresOAuth":   true,
		},
	}
}

func googleVertexProbeModel(route routeSpec, id string) sigma.Model {
	if model, ok := sigma.GetModel(route.Provider, sigma.ModelID(id)); ok && isBuiltInGoogleVertexGeminiModel(model) {
		return model
	}
	return sigma.Model{
		ID:              sigma.ModelID(id),
		Provider:        route.Provider,
		API:             sigma.APIGoogleVertex,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
	}
}

func googleVertexAnthropicProbeModel(route routeSpec, id string) sigma.Model {
	if model, ok := sigma.GetModel(route.Provider, sigma.ModelID(id)); ok && isBuiltInGoogleVertexAnthropicModel(model) {
		return model
	}
	return sigma.Model{
		ID:              sigma.ModelID(id),
		Provider:        route.Provider,
		API:             sigma.APIAnthropicMessages,
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
	}
}

func discoveredOpenCodeModel(route routeSpec, id string) sigma.Model {
	model := sigma.Model{
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
	applyDiscoveredOpenCodeCompat(&model, route.Name, id)
	return model
}

func applyDiscoveredOpenCodeCompat(model *sigma.Model, routeName string, id string) {
	if routeName != "go" {
		return
	}
	switch id {
	case "kimi-k2.6", "kimi-k3":
		model.OpenAICompletionsCompat = &sigma.OpenAICompletionsCompat{
			ReasoningFormat: sigma.OpenAICompletionsReasoningEffort,
		}
	case "kimi-k2.7-code":
		model.OpenAICompletionsCompat = &sigma.OpenAICompletionsCompat{
			ReasoningFormat:            sigma.OpenAICompletionsReasoningEffort,
			SupportsRequiredToolChoice: sigma.OpenAICompatUnsupported,
		}
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

func discoveredMoonshotModel(route routeSpec, id string) sigma.Model {
	if model, ok := sigma.DefaultRegistry().Model(route.Provider, sigma.ModelID(id)); ok {
		return model
	}
	model := sigma.Model{
		ID:               sigma.ModelID(id),
		Provider:         route.Provider,
		API:              sigma.APIOpenAICompletions,
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:    true,
		SupportsThinking: true,
		ContextWindow:    262144,
		MaxOutputTokens:  262144,
		OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
			ReasoningFormat:         sigma.OpenAICompletionsReasoningDeepSeek,
			SupportsReasoningEffort: sigma.OpenAICompatUnsupported,
			SupportsStreamingUsage:  sigma.OpenAICompatSupported,
			SupportsStrictTools:     sigma.OpenAICompatUnsupported,
			MaxTokensField:          sigma.OpenAICompletionsMaxTokens,
		},
		ProviderMetadata: map[string]any{
			"baseURL":         route.BaseURL,
			"apiKeyEnvVars":   []string{route.APIKeyEnv},
			"modelFamily":     modelFamily(id),
			"probeDiscovered": true,
			"probeRoute":      route.Name,
			"probeSurface":    "openai-completions",
		},
	}
	if strings.Contains(id, "kimi-k2.7-code") {
		model.UnsupportedThinkingLevels = []sigma.ThinkingLevel{sigma.ThinkingLevelOff}
	}
	return model
}

func discoveredNVIDIAModel(route routeSpec, id string) sigma.Model {
	if model, ok := sigma.DefaultRegistry().Model(route.Provider, sigma.ModelID(id)); ok {
		return model
	}
	return sigma.Model{
		ID:               sigma.ModelID(id),
		Provider:         route.Provider,
		API:              sigma.APIOpenAICompletions,
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText, sigma.ContentBlockImage},
		SupportsTools:    true,
		SupportsThinking: true,
		OpenAICompletionsCompat: &sigma.OpenAICompletionsCompat{
			SupportsStore:           sigma.OpenAICompatUnsupported,
			SupportsDeveloperRole:   sigma.OpenAICompatUnsupported,
			SupportsReasoningEffort: sigma.OpenAICompatUnsupported,
			SupportsStreamingUsage:  sigma.OpenAICompatSupported,
			SupportsStrictTools:     sigma.OpenAICompatUnsupported,
			MaxTokensField:          sigma.OpenAICompletionsMaxTokens,
		},
		ProviderMetadata: map[string]any{
			"baseURL":         route.BaseURL,
			"apiKeyEnvVars":   []string{route.APIKeyEnv},
			"headers":         map[string]string{"NVCF-POLL-SECONDS": "3600"},
			"modelFamily":     modelFamily(id),
			"probeDiscovered": true,
			"probeRoute":      route.Name,
			"probeSurface":    "openai-completions",
		},
	}
}

func discoveredFireworksOpenAIModel(route routeSpec, id string) sigma.Model {
	if id == "accounts/fireworks/models/kimi-k3" {
		if model, ok := sigma.DefaultRegistry().Model(route.Provider, sigma.ModelID(id)); ok {
			return model
		}
	}
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

func runCaseWithTimeout(ctx context.Context, timeout time.Duration, route routeSpec, client *sigma.Client, model sigma.Model, testCase probeCase, credential routeCredential, attempt string) probeResult {
	if timeout <= 0 {
		return runCase(ctx, route, client, model, testCase, credential, attempt)
	}

	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return runCase(attemptCtx, route, client, model, testCase, credential, attempt)
}

func runCase(ctx context.Context, route routeSpec, client *sigma.Client, model sigma.Model, testCase probeCase, credential routeCredential, attempt string) probeResult {
	options := append(authOptions(route, credential), testCase.Options...)
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

func authOptions(route routeSpec, credential routeCredential) []sigma.Option {
	if route.Name == "openai-codex" && credential.codex != nil {
		return []sigma.Option{
			openai.WithCodexResponsesOAuthTokenProvider(
				route.Provider,
				openai.NewCodexOAuthTokenProvider(*credential.codex, openai.CodexOAuthTokenProviderOptions{}),
			),
		}
	}
	if isGoogleVertexRoute(route) {
		options := []sigma.Option{sigma.WithProviderOptions(route.Provider, map[string]any{
			"projectID": credential.projectID,
			"location":  credential.location,
		})}
		if credential.accessToken != "" {
			resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
				return sigma.Credential{
					Type:   sigma.CredentialTypeOAuthToken,
					Value:  credential.accessToken,
					Source: "env:GOOGLE_CLOUD_ACCESS_TOKEN",
				}, nil
			})
			return append(options, sigma.WithProviderAuthResolver(route.Provider, resolver))
		}
		return append(options, sigma.WithAPIKey(credential.apiKey))
	}
	return []sigma.Option{sigma.WithAPIKey(credential.apiKey)}
}

func imageAuthOptions(route imageRouteSpec, credential routeCredential) []sigma.ImageOption {
	if route.Name == routeGoogleVertex {
		options := []sigma.ImageOption{sigma.WithImageProviderOptions(route.Provider, map[string]any{
			"projectID": credential.projectID,
			"location":  credential.location,
		})}
		if credential.accessToken != "" {
			resolver := sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
				return sigma.Credential{
					Type:   sigma.CredentialTypeOAuthToken,
					Value:  credential.accessToken,
					Source: "env:GOOGLE_CLOUD_ACCESS_TOKEN",
				}, nil
			})
			return append(options, sigma.WithImageProviderAuthResolver(route.Provider, resolver))
		}
		return append(options, sigma.WithImageAPIKey(credential.apiKey))
	}
	return []sigma.ImageOption{sigma.WithImageAPIKey(credential.apiKey)}
}

func googleImageProbeCases(route imageRouteSpec) []imageProbeCase {
	cases := []imageProbeCase{
		{
			Name:        "generate_gemini_3_1",
			Description: "Gemini 3.1 image generation",
			ModelID:     defaultGoogleCurrentImageProbeModel,
			Request: sigma.ImageRequest{
				Prompt:   "Create a simple square icon with the word sigma.",
				Size:     "1:1",
				MIMEType: "image/png",
				Count:    1,
			},
			SuccessHint:  "image_generated",
			RequireImage: true,
		},
	}
	if route.Name == routeGoogleVertex {
		return cases
	}
	return append([]imageProbeCase{
		{
			Name:        "generate_gemini",
			Description: "Gemini image generation",
			ModelID:     defaultGoogleGeminiImageProbeModel,
			Request: sigma.ImageRequest{
				Prompt:   "Create a simple square icon with the word sigma.",
				Size:     "1:1",
				MIMEType: "image/png",
				Count:    1,
			},
			SuccessHint:  "image_generated",
			RequireImage: true,
		},
	}, cases...)
}

func openAIImageProbeCases(_ imageRouteSpec) []imageProbeCase {
	basePNG := probeImageInput()
	return []imageProbeCase{
		{
			Name:        "generate",
			Description: "text-to-image generation",
			ModelID:     defaultOpenAIImageProbeModel,
			Request: sigma.ImageRequest{
				Prompt:   "Create a simple square icon with the word sigma.",
				Size:     string(sigma.ImageSize1024x1024),
				Quality:  string(sigma.ImageQualityLow),
				MIMEType: "image/png",
				Count:    1,
			},
			SuccessHint:  "image_generated",
			RequireImage: true,
		},
		{
			Name:        "edit_multipart",
			Description: "multipart image edit",
			ModelID:     defaultOpenAIImageProbeModel,
			Request: sigma.ImageRequest{
				Operation: sigma.ImageOperationEdit,
				Prompt:    "Add a thin blue border.",
				Inputs:    []sigma.ImageInput{basePNG},
				Size:      string(sigma.ImageSize1024x1024),
				Quality:   string(sigma.ImageQualityLow),
				MIMEType:  "image/png",
				Count:     1,
			},
			SuccessHint:  "image_generated",
			RequireImage: true,
		},
		{
			Name:        "edit_reference_json",
			Description: "JSON reference image edit",
			ModelID:     defaultOpenAIImageProbeModel,
			Request: sigma.ImageRequest{
				Operation: sigma.ImageOperationEdit,
				Prompt:    "Make the image look like a simple app icon.",
				Inputs:    []sigma.ImageInput{sigma.ImageOutputURL("", "https://upload.wikimedia.org/wikipedia/commons/7/70/Example.png")},
				Size:      string(sigma.ImageSize1024x1024),
				Quality:   string(sigma.ImageQualityLow),
				MIMEType:  "image/png",
				Count:     1,
			},
			SuccessHint:  "image_generated",
			RequireImage: true,
		},
		{
			Name:        "variation",
			Description: "DALL-E 2 variation",
			ModelID:     defaultOpenAIImageVariationModel,
			Request: sigma.ImageRequest{
				Operation: sigma.ImageOperationVariation,
				Inputs:    []sigma.ImageInput{basePNG},
				Size:      string(sigma.ImageSize1024x1024),
				Count:     1,
			},
			SuccessHint:  "image_generated",
			RequireImage: true,
		},
		{
			Name:        "stream_partial",
			Description: "streaming partial image generation",
			ModelID:     defaultOpenAIImageProbeModel,
			Request: sigma.ImageRequest{
				Prompt:   "Create a simple square icon with one diagonal line.",
				Size:     string(sigma.ImageSize1024x1024),
				Quality:  string(sigma.ImageQualityLow),
				MIMEType: "image/png",
				Count:    1,
			},
			Options:        []sigma.ImageOption{sigma.WithImageProviderOption(sigma.ProviderOpenAI, "partial_images", 1)},
			Stream:         true,
			SuccessHint:    "partial_image_seen",
			RequireImage:   true,
			RequirePartial: true,
		},
		{
			Name:             "responses_image_tool",
			Description:      "OpenAI Responses image-generation tool output",
			ResponsesTool:    true,
			ResponsesModelID: defaultOpenAIResponsesToolProbeModel,
			ResponsesRequest: sigma.Request{
				Messages: []sigma.Message{sigma.UserText("Use the image generation tool to create a tiny square icon.")},
				Tools: []sigma.Tool{openai.Tools.ImageGeneration(
					openai.WithImageModel(defaultOpenAIImageProbeModel),
					openai.WithImageSize(string(sigma.ImageSize1024x1024)),
					openai.WithImageQuality(string(sigma.ImageQualityLow)),
					openai.WithOutputFormat("png"),
					openai.WithPartialImages(1),
				)},
			},
			ResponsesOptions:  []sigma.Option{sigma.WithMaxTokens(1024)},
			SuccessHint:       "image_tool_output_seen",
			RequireToolOutput: true,
		},
	}
}

func openAIResponsesProbeCases(_ routeSpec, _ sigma.Model) []probeCase {
	return []probeCase{
		singleTurnCase("basic_text", "plain streaming text", basicRequest("Reply with exactly: sigma-ok."), []sigma.Option{sigma.WithMaxTokens(128)}),
		singleTurnCase("developer_instruction", "developer instruction handling", sigma.Request{
			SystemPrompt: "Reply tersely.",
			Messages:     []sigma.Message{sigma.UserText("Reply with exactly: dev-ok.")},
		}, []sigma.Option{sigma.WithMaxTokens(128)}),
		singleTurnCase("json_schema", "strict structured output", basicRequest("Return JSON exactly {\"answer\":\"ok\"}."), []sigma.Option{
			sigma.WithOpenAIOptions(sigma.OpenAIOptions{ResponseFormat: jsonSchemaTextFormat()}),
			sigma.WithMaxTokens(512),
		}),
		singleTurnCase("cache_ephemeral", "prompt cache marker", basicRequest("Reply with exactly: cache-ok."), []sigma.Option{
			sigma.WithCacheRetention(sigma.CacheRetentionEphemeral),
			sigma.WithSessionID("sigma-openai-probe"),
			sigma.WithMaxTokens(128),
		}),
		singleTurnCase("image_input", "text plus image input", imageRequest(), []sigma.Option{sigma.WithMaxTokens(512)}),
		singleTurnCase("reasoning_level_low", "typed reasoning low", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithOpenAIOptions(sigma.OpenAIOptions{ReasoningEffort: sigma.ThinkingLevelLow}), sigma.WithMaxTokens(512)}),
		singleTurnCase("reasoning_level_medium", "typed reasoning medium", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithOpenAIOptions(sigma.OpenAIOptions{ReasoningEffort: sigma.ThinkingLevelMedium}), sigma.WithMaxTokens(512)}),
		singleTurnCase("reasoning_level_high", "typed reasoning high", basicRequest("Reply with exactly: 5."), []sigma.Option{sigma.WithOpenAIOptions(sigma.OpenAIOptions{ReasoningEffort: sigma.ThinkingLevelHigh}), sigma.WithMaxTokens(512)}),
		toolCase("tool_auto_file_read", "auto read-file tool", "auto"),
		toolCase("tool_required_file_read", "required read-file tool", "required"),
	}
}

func openAICodexProbeCases(route routeSpec, model sigma.Model) []probeCase {
	cases := openAIResponsesProbeCases(route, model)
	for index := range cases {
		if cases[index].Name == "image_input" {
			cases[index].Description = "text plus URL image input"
			cases[index].Request = imageURLRequest()
		}
	}
	return append(cases,
		singleTurnCase("text_verbosity_low", "Codex text verbosity low", basicRequest("Reply with exactly: terse-ok."), []sigma.Option{
			sigma.WithOpenAIOptions(sigma.OpenAIOptions{TextVerbosity: "low"}),
			sigma.WithMaxTokens(128),
		}),
	)
}

func googleVertexProbeCases(_ routeSpec, model sigma.Model) []probeCase {
	cases := []probeCase{
		singleTurnCase("basic_text", "plain streaming text", basicRequest("Reply with exactly: sigma-ok."), []sigma.Option{sigma.WithMaxTokens(128)}),
		singleTurnCase("developer_instruction", "system instruction handling", sigma.Request{
			SystemPrompt: "Reply tersely.",
			Messages:     []sigma.Message{sigma.UserText("Reply with exactly: dev-ok.")},
		}, []sigma.Option{sigma.WithMaxTokens(128)}),
	}
	if model.SupportsImages() {
		cases = append(cases, singleTurnCase("image_input", "text plus image input", imageRequest(), []sigma.Option{sigma.WithMaxTokens(512)}))
	}
	if model.SupportsReasoning() {
		if model.SupportsThinkingLevel(sigma.ThinkingLevelOff) {
			disableThinking := true
			cases = append(cases, singleTurnCase("thinking_disabled", "typed disabled thinking", basicRequest("Reply with exactly: 5."), []sigma.Option{
				sigma.WithGoogleOptions(sigma.GoogleOptions{DisableThinking: &disableThinking}),
				sigma.WithMaxTokens(512),
			}))
		}
		for _, level := range []sigma.ThinkingLevel{sigma.ThinkingLevelLow, sigma.ThinkingLevelMedium, sigma.ThinkingLevelHigh} {
			if model.SupportsThinkingLevel(level) {
				cases = append(cases, singleTurnCase("reasoning_level_"+string(level), "typed reasoning "+string(level), basicRequest("Reply with exactly: 5."), []sigma.Option{
					sigma.WithReasoningLevel(level),
					sigma.WithMaxTokens(512),
				}))
			}
		}
	}
	if model.SupportsTools {
		cases = append(cases,
			googleToolCase("tool_auto_file_read", "auto read-file tool", "auto"),
			googleToolCase("tool_any_file_read", "forced-any read-file tool", "any"),
		)
	}
	return cases
}

func openAICompatibleProbeCases(route routeSpec, model sigma.Model) []probeCase {
	cases := []probeCase{
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
	if route.Provider != sigma.ProviderFireworks && !isOpenCodeGoReasoningEffortKimi(model) && !modelUnsupportedThinkingOff(model) {
		return cases
	}
	filtered := cases[:0]
	for _, testCase := range cases {
		if skipOpenAICompatibleProbeCase(route, model, testCase.Name) {
			continue
		}
		filtered = append(filtered, testCase)
	}
	return filtered
}

func skipOpenAICompatibleProbeCase(route routeSpec, model sigma.Model, name string) bool {
	switch {
	case route.Provider == sigma.ProviderFireworks:
		switch name {
		case "thinking_string_none", "thinking_bool_false", "enable_thinking_false":
			return true
		default:
			return false
		}
	case isOpenCodeGoReasoningEffortKimi(model):
		switch name {
		case "logprobs":
			return model.ID == "kimi-k3"
		case "thinking_string_none", "thinking_object_disabled", "thinking_bool_false", "enable_thinking_false":
			return true
		case "tool_required_file_read", "strict_tool_required_write":
			return !openAICompletionsRequiredToolChoiceSupported(model)
		default:
			return false
		}
	case modelUnsupportedThinkingOff(model):
		switch name {
		case "thinking_string_none", "thinking_object_disabled", "thinking_bool_false", "enable_thinking_false":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func modelUnsupportedThinkingOff(model sigma.Model) bool {
	return !model.SupportsThinkingLevel(sigma.ThinkingLevelOff)
}

func openAICompletionsRequiredToolChoiceSupported(model sigma.Model) bool {
	if model.OpenAICompletionsCompat == nil {
		return true
	}
	return model.OpenAICompletionsCompat.SupportsRequiredToolChoice != sigma.OpenAICompatUnsupported
}

func isOpenCodeGoReasoningEffortKimi(model sigma.Model) bool {
	return model.Provider == sigma.ProviderOpenCodeGo &&
		model.OpenAICompletionsCompat != nil &&
		model.OpenAICompletionsCompat.ReasoningFormat == sigma.OpenAICompletionsReasoningEffort &&
		modelFamily(string(model.ID)) == "kimi"
}

func anthropicCompatibleProbeCases(route routeSpec, model sigma.Model) []probeCase {
	cases := []probeCase{
		singleTurnCase("basic_text", "plain streaming text", basicRequest("Reply with exactly: sigma-ok."), []sigma.Option{sigma.WithMaxTokens(128)}),
		singleTurnCase("developer_instruction", "system instruction handling", sigma.Request{
			SystemPrompt: "Reply tersely.",
			Messages:     []sigma.Message{sigma.UserText("Reply with exactly: dev-ok.")},
		}, []sigma.Option{sigma.WithMaxTokens(128)}),
		singleTurnCase("cache_ephemeral", "prompt cache marker", basicRequest("Reply with exactly: cache-ok."), []sigma.Option{
			sigma.WithCacheRetention(sigma.CacheRetentionEphemeral),
			sigma.WithSessionID("sigma-" + route.Name + "-probe"),
			sigma.WithMaxTokens(128),
		}),
	}
	if model.SupportsImages() {
		cases = append(cases, singleTurnCase("image_input", "text plus image input", imageRequest(), []sigma.Option{sigma.WithMaxTokens(512)}))
	}
	if model.SupportsReasoning() {
		for _, level := range []sigma.ThinkingLevel{sigma.ThinkingLevelLow, sigma.ThinkingLevelMedium, sigma.ThinkingLevelHigh} {
			if model.SupportsThinkingLevel(level) {
				cases = append(cases, singleTurnCase("reasoning_level_"+string(level), "typed reasoning "+string(level), basicRequest("Reply with exactly: 5."), []sigma.Option{
					sigma.WithReasoningLevel(level),
					sigma.WithMaxTokens(512),
				}))
			}
		}
	}
	if model.SupportsTools {
		cases = append(cases,
			anthropicToolCase("tool_auto_file_read", "auto read-file tool", sigma.AnthropicToolChoiceAuto),
			anthropicToolCase("tool_required_file_read", "required read-file tool", sigma.AnthropicToolChoiceAny),
		)
	}
	return cases
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
		sigma.ImageBase64("image/png", "iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAAJ0lEQVR42u3NsQkAAAjAsP7/tF7hIASyp6lTCQQCgUAgEAgEgi/BAjLD/C5w/SM9AAAAAElFTkSuQmCC"),
	)}}
}

func imageURLRequest() sigma.Request {
	return sigma.Request{Messages: []sigma.Message{sigma.UserContent(
		sigma.Text("Answer with one short colour word."),
		sigma.ImageURL("image/png", "https://upload.wikimedia.org/wikipedia/commons/7/70/Example.png"),
	)}}
}

func probeImageInput() sigma.ImageInput {
	return sigma.ImageData("image/png", "iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAAJ0lEQVR42u3NsQkAAAjAsP7/tF7hIASyp6lTCQQCgUAgEAgEgi/BAjLD/C5w/SM9AAAAAElFTkSuQmCC")
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

func anthropicToolCase(name string, description string, choice sigma.AnthropicToolChoiceType) probeCase {
	options := []sigma.Option{sigma.WithMaxTokens(512)}
	if choice == sigma.AnthropicToolChoiceAuto {
		options = append(options, sigma.WithToolChoice(sigma.ToolChoiceAuto))
	} else {
		options = append(options, sigma.WithAnthropicOptions(sigma.AnthropicOptions{
			ToolChoice: &sigma.AnthropicToolChoice{Type: choice},
		}))
	}
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
		Options: options,
	}
}

func googleToolCase(name string, description string, choice string) probeCase {
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
			sigma.WithGoogleOptions(sigma.GoogleOptions{ToolChoice: choice}),
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
			singleTurnCase("image_url_fallback", "same image task with an HTTPS image URL", imageURLRequest(), []sigma.Option{sigma.WithMaxTokens(512)}),
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
			singleTurnCase("logprobs_more_tokens", "logprobs with a larger cap", basicRequest("Reply with exactly: yes."), []sigma.Option{
				sigma.WithProviderOption(route.Provider, "extra_body", map[string]any{"logprobs": true, "top_logprobs": 2}),
				sigma.WithMaxTokens(512),
			}),
			singleTurnCase("no_logprobs", "omit logprobs", basicRequest("Reply with exactly: yes."), []sigma.Option{sigma.WithMaxTokens(16)}),
			singleTurnCase("no_logprobs_more_tokens", "omit logprobs and use a larger cap", basicRequest("Reply with exactly: yes."), []sigma.Option{sigma.WithMaxTokens(512)}),
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

func jsonSchemaTextFormat() map[string]any {
	return map[string]any{
		jsonTypeKey: "json_schema",
		"name":      "answer",
		"strict":    true,
		"schema": map[string]any{
			jsonTypeKey:            "object",
			"properties":           map[string]any{"answer": map[string]any{jsonTypeKey: "string"}},
			"required":             []any{"answer"},
			"additionalProperties": false,
		},
	}
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
	if errors.Is(err, context.DeadlineExceeded) {
		return "upstream_availability"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "image") && strings.Contains(message, "support"):
		return "provider_capability_limit"
	case strings.Contains(message, "free promotion has ended"),
		strings.Contains(message, "model_not_found"),
		strings.Contains(message, "path not found"),
		strings.Contains(message, "status=404"),
		strings.Contains(message, "no provider available"),
		strings.Contains(message, "status=429"),
		strings.Contains(message, "rate limit"),
		strings.Contains(message, "not supported when using codex with a chatgpt account"):
		return "upstream_availability"
	case strings.Contains(message, "unknown parameter"),
		strings.Contains(message, "missing required parameter"),
		strings.Contains(message, "unsupported parameter"),
		strings.Contains(message, "thinking_level is not supported"),
		strings.Contains(message, "instructions are required"),
		strings.Contains(message, "store must be set to false"),
		strings.Contains(message, "not supported for format oa-compat"),
		strings.Contains(message, "integer below minimum"):
		return "sigma_request_shape"
	default:
		return "no_working_attempt"
	}
}

func classifyImageFailure(route imageRouteSpec, model sigma.ImageModel, err error) string {
	return classifyFailure(routeSpec{Name: route.Name}, sigma.Model{
		ID:       model.ID,
		Provider: model.Provider,
	}, err)
}

func modelFamily(id string) string {
	for _, separator := range []string{"-", "."} {
		if before, _, ok := strings.Cut(id, separator); ok {
			return before
		}
	}
	return id
}

func imageModelFamily(id string) string {
	switch {
	case strings.HasPrefix(id, "gpt-image"):
		return "gpt-image"
	case strings.HasPrefix(id, "dall-e"):
		return "dall-e"
	default:
		return modelFamily(id)
	}
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

func repairHint(caseName string, attempt string) string {
	switch {
	case attempt == "minimal_basic_text":
		return "minimal_text_available_after_failure"
	case caseName == "image_input" && attempt == "image_url_fallback":
		return "base64_image_rejected_url_image_ok"
	case caseName == "image_input" && attempt == "text_only_fallback":
		return "image_input_rejected_text_only_ok"
	case caseName == "image_input" && attempt == "image_more_tokens":
		return "image_input_needs_larger_output_budget"
	case strings.HasPrefix(caseName, "thinking_") && attempt == "thinking_object_disabled_repair":
		return "use_thinking_object_disabled"
	case caseName == "enable_thinking_false" && attempt == "thinking_object_disabled_repair":
		return "use_thinking_object_disabled"
	case strings.HasPrefix(caseName, "reasoning_effort_") && strings.HasPrefix(attempt, "typed_reasoning_effort_"):
		return "use_typed_reasoning_effort_option"
	case strings.HasPrefix(caseName, "reasoning_effort_") && attempt == "no_reasoning_control":
		return "omit_reasoning_control"
	case caseName == "cache_ephemeral" && strings.HasPrefix(attempt, "cache_none"):
		return "cache_marker_rejected"
	case caseName == "json_schema" && attempt == "json_object_fallback":
		return "json_schema_rejected_json_object_ok"
	case caseName == "json_schema" && attempt == "manual_json":
		return "structured_output_rejected_prompt_json_ok"
	case caseName == "json_schema" && attempt == "json_schema_more_tokens":
		return "json_schema_needs_larger_output_budget"
	case caseName == "logprobs" && attempt == "logprobs_more_tokens":
		return "logprobs_needs_larger_output_budget"
	case caseName == "logprobs" && attempt == "no_logprobs":
		return "logprobs_rejected"
	case caseName == "logprobs" && attempt == "no_logprobs_more_tokens":
		return "logprobs_output_budget_interaction"
	case strings.HasPrefix(caseName, "tool_") && attempt == "tool_auto_more_turns":
		return "auto_tool_choice_with_larger_budget_ok"
	case caseName == "strict_tool_required_write" && attempt == "tool_auto_more_turns":
		return "auto_tool_choice_with_larger_budget_ok"
	case caseName == "three_turn_file_update" && attempt == "three_turn_more_tokens":
		return "multi_turn_tool_flow_needs_larger_output_budget"
	case caseName == "basic_text" && attempt == "basic_text_more_tokens":
		return "basic_text_needs_larger_output_budget"
	default:
		return ""
	}
}

func recommendationFor(result probeResult) (probeRecommendation, bool) {
	if result.Hint == "" {
		return probeRecommendation{}, false
	}
	evidence := fmt.Sprintf("%s repaired by %s", result.Case, result.Attempt)
	if result.AvailabilityOKAfterFailure {
		evidence = fmt.Sprintf("%s failed; minimal text remained available", result.Case)
	} else if result.Outcome == "ok" {
		evidence = fmt.Sprintf("%s supported by %s", result.Case, result.Attempt)
	}
	return probeRecommendation{
		Route:    result.Route,
		Model:    result.Model,
		Case:     result.Case,
		Hint:     result.Hint,
		Evidence: evidence,
	}, true
}

func (s *summary) add(result probeResult) {
	s.Total++
	if result.AvailabilityOKAfterFailure {
		s.AvailabilityOKAfterFailure++
	}
	switch result.Outcome {
	case "ok":
		s.OK++
	case outcomeSkipped:
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

func writeSummary(writer *bufio.Writer, totals summary, recommendations []probeRecommendation) {
	encoded, _ := json.Marshal(probeReport{Summary: totals, Recommendations: recommendations})
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
