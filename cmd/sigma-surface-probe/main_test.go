// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/sigmatest"
)

func collectProbeModel(ctx context.Context, route routeSpec, modelID string, credential routeCredential, cfg config) []probeResult {
	model := route.Model(route, modelID)
	results := make([]probeResult, 0, len(route.Cases(route, model)))
	probeModelEach(ctx, route, modelID, credential, cfg, func(result probeResult) {
		results = append(results, result)
	})
	return results
}

func TestOpenCodeRouteAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		route string
		id    string
		want  sigma.API
	}{
		{route: "zen", id: "gemini-3-flash", want: sigma.APIGoogleGenerativeAI},
		{route: "zen", id: "claude-opus-4-7", want: sigma.APIAnthropicMessages},
		{route: "zen", id: "qwen3.6-plus", want: sigma.APIAnthropicMessages},
		{route: "zen", id: "gpt-5.1-codex", want: sigma.APIOpenAIResponses},
		{route: "zen", id: "kimi-k2.6", want: sigma.APIOpenAICompletions},
		{route: "go", id: "qwen3.7-max", want: sigma.APIAnthropicMessages},
		{route: "go", id: "minimax-m2.5", want: sigma.APIAnthropicMessages},
		{route: "go", id: "kimi-k2.6", want: sigma.APIOpenAICompletions},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.route+"/"+tt.id, func(t *testing.T) {
			t.Parallel()

			if got := openCodeRouteAPI(tt.route, tt.id); got != tt.want {
				t.Fatalf("openCodeRouteAPI = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKnownUnavailable(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"claude-opus-4-6", "minimax-m2.5-free", "qwen3.6-plus-free", "gpt-5.3-codex-spark"} {
		if !knownUnavailable("zen", id) {
			t.Fatalf("%s was not classified as known unavailable", id)
		}
	}
	if knownUnavailable("go", "qwen3.7-max") {
		t.Fatal("go qwen3.7-max should not be skipped")
	}
}

func TestFireworksRoutesBuildExpectedModels(t *testing.T) {
	t.Parallel()

	openAI := routes["fireworks-openai"].Model(routes["fireworks-openai"], "accounts/fireworks/routers/kimi-k2p6-turbo")
	if openAI.Provider != sigma.ProviderFireworks || openAI.API != sigma.APIOpenAICompletions {
		t.Fatalf("fireworks-openai model provider/API = %q/%q", openAI.Provider, openAI.API)
	}
	if openAI.OpenAICompletionsCompat == nil ||
		openAI.OpenAICompletionsCompat.ReasoningFormat != sigma.OpenAICompletionsReasoningFireworks ||
		openAI.OpenAICompletionsCompat.MaxTokensField != sigma.OpenAICompletionsMaxTokens {
		t.Fatalf("fireworks-openai compat = %#v, want Fireworks OpenAI completions compat", openAI.OpenAICompletionsCompat)
	}
	assertMetadataString(t, openAI.ProviderMetadata, "baseURL", "https://api.fireworks.ai/inference/v1")
	assertMetadataStrings(t, openAI.ProviderMetadata, "apiKeyEnvVars", []string{"FIREWORKS_API_KEY"})

	anthropic := routes["fireworks-anthropic"].Model(routes["fireworks-anthropic"], "accounts/fireworks/models/kimi-k2p6")
	if anthropic.Provider != sigma.ProviderFireworksAnthropic || anthropic.API != sigma.APIAnthropicMessages {
		t.Fatalf("fireworks-anthropic model provider/API = %q/%q", anthropic.Provider, anthropic.API)
	}
	if anthropic.AnthropicMessagesCompat == nil ||
		anthropic.AnthropicMessagesCompat.SupportsSessionAffinity != sigma.AnthropicCompatSupported ||
		anthropic.AnthropicMessagesCompat.SupportsEagerToolInputStreaming != sigma.AnthropicCompatUnsupported ||
		anthropic.AnthropicMessagesCompat.SupportsLongCacheRetention != sigma.AnthropicCompatUnsupported ||
		anthropic.AnthropicMessagesCompat.SupportsCacheControlOnTools != sigma.AnthropicCompatUnsupported {
		t.Fatalf("fireworks-anthropic compat = %#v, want Fireworks Anthropic compat", anthropic.AnthropicMessagesCompat)
	}
	assertMetadataString(t, anthropic.ProviderMetadata, "baseURL", "https://api.fireworks.ai/inference/v1")
	assertMetadataStrings(t, anthropic.ProviderMetadata, "apiKeyEnvVars", []string{"FIREWORKS_API_KEY"})
}

func TestOpenAIRoutesBuildExpectedModels(t *testing.T) {
	t.Parallel()

	openAIRoute := routes["openai"]
	openAIModel := openAIRoute.Model(openAIRoute, "gpt-5.1")
	if openAIModel.Provider != sigma.ProviderOpenAI || openAIModel.API != sigma.APIOpenAIResponses {
		t.Fatalf("openai model provider/API = %q/%q", openAIModel.Provider, openAIModel.API)
	}
	assertMetadataString(t, openAIModel.ProviderMetadata, "baseURL", "https://api.openai.com/v1")
	assertMetadataString(t, openAIModel.ProviderMetadata, "probeSurface", "openai-responses")
	assertMetadataStrings(t, openAIModel.ProviderMetadata, "apiKeyEnvVars", []string{"OPENAI_API_KEY"})

	codexRoute := routes["openai-codex"]
	codexModel := codexRoute.Model(codexRoute, "gpt-5.1-codex")
	if codexModel.Provider != sigma.ProviderOpenAI || codexModel.API != sigma.APIOpenAICodexResponses {
		t.Fatalf("openai-codex model provider/API = %q/%q", codexModel.Provider, codexModel.API)
	}
	if codexModel.OpenAICodexResponses == nil {
		t.Fatal("openai-codex model missing OpenAICodexResponses config")
	}
	assertMetadataString(t, codexModel.ProviderMetadata, "baseURL", "https://chatgpt.com/backend-api/codex")
	assertMetadataString(t, codexModel.ProviderMetadata, "probeSurface", "openai-codex-responses")
	assertMetadataStrings(t, codexModel.ProviderMetadata, "apiKeyEnvVars", []string{"OPENAI_CODEX_ACCESS_TOKEN", "OPENAI_CODEX_REFRESH_TOKEN"})
}

func TestXAIRouteBuildsExpectedModel(t *testing.T) {
	t.Parallel()

	route := routes["xai"]
	model := route.Model(route, "grok-4.3")
	if model.Provider != sigma.ProviderXAI || model.API != sigma.APIOpenAICompletions {
		t.Fatalf("xai model provider/API = %q/%q", model.Provider, model.API)
	}
	if !model.SupportsTools || !model.SupportsImages() || !model.SupportsReasoning() {
		t.Fatalf("xai probe model did not enable optimistic probe capabilities: %+v", model)
	}
	assertMetadataString(t, model.ProviderMetadata, "baseURL", "https://api.x.ai/v1")
	assertMetadataString(t, model.ProviderMetadata, "modelFamily", "grok")
	assertMetadataString(t, model.ProviderMetadata, "probeRoute", "xai")
	assertMetadataString(t, model.ProviderMetadata, "probeSurface", "openai-completions")
	assertMetadataStrings(t, model.ProviderMetadata, "apiKeyEnvVars", []string{"XAI_API_KEY"})
}

func TestMoonshotRoutesBuildExpectedModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider sigma.ProviderID
		baseURL  string
	}{
		{name: "moonshot", provider: sigma.ProviderMoonshotAI, baseURL: "https://api.moonshot.ai/v1"},
		{name: "moonshot-cn", provider: sigma.ProviderMoonshotAICN, baseURL: "https://api.moonshot.cn/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			route := routes[tt.name]
			if route.RegisterProvider == nil {
				t.Fatalf("route %q missing provider registration", tt.name)
			}
			if got, want := route.Provider, tt.provider; got != want {
				t.Fatalf("provider = %q, want %q", got, want)
			}
			if got, want := route.BaseURL, tt.baseURL; got != want {
				t.Fatalf("base URL = %q, want %q", got, want)
			}
			if got, want := route.APIKeyEnv, "MOONSHOT_API_KEY"; got != want {
				t.Fatalf("api key env = %q, want %q", got, want)
			}

			model := route.Model(route, "kimi-k2.7-code")
			if model.Provider != tt.provider || model.API != sigma.APIOpenAICompletions {
				t.Fatalf("model provider/API = %q/%q", model.Provider, model.API)
			}
			if model.OpenAICompletionsCompat == nil ||
				model.OpenAICompletionsCompat.ReasoningFormat != sigma.OpenAICompletionsReasoningDeepSeek ||
				model.OpenAICompletionsCompat.SupportsReasoningEffort != sigma.OpenAICompatUnsupported {
				t.Fatalf("Moonshot probe compat = %#v, want DeepSeek format without reasoning effort", model.OpenAICompletionsCompat)
			}
		})
	}
}

func TestNVIDIARouteBuildsExpectedModels(t *testing.T) {
	t.Parallel()

	route := routes["nvidia"]
	if route.RegisterProvider == nil {
		t.Fatal("nvidia route missing provider registration")
	}
	if got, want := route.Provider, sigma.ProviderNVIDIA; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if got, want := route.BaseURL, "https://integrate.api.nvidia.com/v1"; got != want {
		t.Fatalf("base URL = %q, want %q", got, want)
	}
	if got, want := route.APIKeyEnv, "NVIDIA_API_KEY"; got != want {
		t.Fatalf("api key env = %q, want %q", got, want)
	}

	generated := route.Model(route, defaultNVIDIAProbeModel)
	if generated.Provider != sigma.ProviderNVIDIA || generated.API != sigma.APIOpenAICompletions {
		t.Fatalf("generated NVIDIA model provider/API = %q/%q", generated.Provider, generated.API)
	}
	if generated.OpenAICompletionsCompat == nil ||
		generated.OpenAICompletionsCompat.SupportsReasoningEffort != sigma.OpenAICompatUnsupported ||
		generated.OpenAICompletionsCompat.SupportsStreamingUsage != sigma.OpenAICompatSupported ||
		generated.OpenAICompletionsCompat.SupportsStrictTools != sigma.OpenAICompatUnsupported ||
		generated.OpenAICompletionsCompat.MaxTokensField != sigma.OpenAICompletionsMaxTokens {
		t.Fatalf("generated NVIDIA compat = %#v, want NVIDIA OpenAI-compatible defaults", generated.OpenAICompletionsCompat)
	}
	assertMetadataString(t, generated.ProviderMetadata, "baseURL", "https://integrate.api.nvidia.com/v1")
	assertMetadataStrings(t, generated.ProviderMetadata, "apiKeyEnvVars", []string{"NVIDIA_API_KEY"})

	discovered := route.Model(route, "custom/nim")
	if discovered.Provider != sigma.ProviderNVIDIA || discovered.API != sigma.APIOpenAICompletions {
		t.Fatalf("discovered NVIDIA model provider/API = %q/%q", discovered.Provider, discovered.API)
	}
	if discovered.OpenAICompletionsCompat == nil ||
		discovered.OpenAICompletionsCompat.SupportsStore != sigma.OpenAICompatUnsupported ||
		discovered.OpenAICompletionsCompat.SupportsDeveloperRole != sigma.OpenAICompatUnsupported ||
		discovered.OpenAICompletionsCompat.SupportsReasoningEffort != sigma.OpenAICompatUnsupported ||
		discovered.OpenAICompletionsCompat.SupportsStreamingUsage != sigma.OpenAICompatSupported ||
		discovered.OpenAICompletionsCompat.SupportsStrictTools != sigma.OpenAICompatUnsupported ||
		discovered.OpenAICompletionsCompat.MaxTokensField != sigma.OpenAICompletionsMaxTokens {
		t.Fatalf("discovered NVIDIA compat = %#v, want NVIDIA OpenAI-compatible defaults", discovered.OpenAICompletionsCompat)
	}
	assertMetadataString(t, discovered.ProviderMetadata, "probeRoute", "nvidia")
	assertMetadataString(t, discovered.ProviderMetadata, "probeSurface", "openai-completions")
	assertMetadataStrings(t, discovered.ProviderMetadata, "apiKeyEnvVars", []string{"NVIDIA_API_KEY"})
}

func TestModelsForRouteUsesSelectedModelsWithoutDiscovery(t *testing.T) {
	t.Parallel()

	models, err := modelsForRoute(context.Background(), routes["fireworks-anthropic"], routeCredential{apiKey: "key"}, map[string]bool{
		"z": true,
		"a": true,
	})
	if err != nil {
		t.Fatalf("modelsForRoute returned error: %v", err)
	}
	if !reflect.DeepEqual(models, []string{"a", "z"}) {
		t.Fatalf("models = %v, want sorted selected models", models)
	}
}

func TestModelsForRouteDefaultsOpenAICodexWithoutDiscovery(t *testing.T) {
	t.Parallel()

	if got, want := defaultOpenAICodexProbeModel, "gpt-5.5"; got != want {
		t.Fatalf("defaultOpenAICodexProbeModel = %q, want %q", got, want)
	}
	models, err := modelsForRoute(context.Background(), routes["openai-codex"], routeCredential{apiKey: "token"}, nil)
	if err != nil {
		t.Fatalf("modelsForRoute returned error: %v", err)
	}
	if !reflect.DeepEqual(models, []string{defaultOpenAICodexProbeModel}) {
		t.Fatalf("models = %v, want default Codex model", models)
	}
}

func TestModelsForRouteDefaultsNVIDIAWithoutDiscovery(t *testing.T) {
	t.Parallel()

	if got, want := defaultNVIDIAProbeModel, "nvidia/nemotron-3-super-120b-a12b"; got != want {
		t.Fatalf("defaultNVIDIAProbeModel = %q, want %q", got, want)
	}
	models, err := modelsForRoute(context.Background(), routes["nvidia"], routeCredential{apiKey: "token"}, nil)
	if err != nil {
		t.Fatalf("modelsForRoute returned error: %v", err)
	}
	if !reflect.DeepEqual(models, []string{defaultNVIDIAProbeModel}) {
		t.Fatalf("models = %v, want default NVIDIA model", models)
	}
}

func TestOpenAICompatibleProbeCasesUseRouteProviderOptions(t *testing.T) {
	t.Parallel()

	testCase := findProbeCase(t, openAICompatibleProbeCases(routes["fireworks-openai"], sigma.Model{}), "json_object")
	options := applyProbeOptions(testCase.Options)
	if _, ok := options.ProviderOptions[sigma.ProviderFireworks]["extra_body"]; !ok {
		t.Fatalf("fireworks provider options = %#v, want extra_body", options.ProviderOptions[sigma.ProviderFireworks])
	}
	if _, ok := options.ProviderOptions[sigma.ProviderOpenCode]; ok {
		t.Fatalf("unexpected OpenCode provider options: %#v", options.ProviderOptions[sigma.ProviderOpenCode])
	}
}

func TestStructuredOutputProbeCasesSelectsJSONOnly(t *testing.T) {
	t.Parallel()

	cases := structuredOutputProbeCases(openAICompatibleProbeCases(routes["xai"], sigma.Model{}))
	if len(cases) != 2 {
		t.Fatalf("cases length = %d, want 2", len(cases))
	}
	if got, want := cases[0].Name, "json_object"; got != want {
		t.Fatalf("first case = %q, want %q", got, want)
	}
	if got, want := cases[1].Name, "json_schema"; got != want {
		t.Fatalf("second case = %q, want %q", got, want)
	}
}

func TestFireworksOpenAIProbeCasesSkipScalarThinkingControls(t *testing.T) {
	t.Parallel()

	cases := openAICompatibleProbeCases(routes["fireworks-openai"], sigma.Model{})
	if hasRepairVariant(cases, "thinking_string_none") {
		t.Fatal("fireworks-openai should not probe scalar thinking string controls")
	}
	if hasRepairVariant(cases, "thinking_bool_false") {
		t.Fatal("fireworks-openai should not probe scalar thinking bool controls")
	}
	if hasRepairVariant(cases, "enable_thinking_false") {
		t.Fatal("fireworks-openai should not probe unsupported enable_thinking controls")
	}
	if !hasRepairVariant(cases, "thinking_object_disabled") {
		t.Fatal("fireworks-openai should still probe object disabled thinking")
	}
	if !hasRepairVariant(openAICompatibleProbeCases(routes["xai"], sigma.Model{}), "thinking_string_none") {
		t.Fatal("non-Fireworks OpenAI-compatible routes should keep scalar thinking probes")
	}
}

func TestOpenCodeGoKimiProbeCasesMatchReasoningFormat(t *testing.T) {
	t.Parallel()

	route := routes["go"]
	kimiCode := discoveredOpenCodeModel(route, "kimi-k2.7-code")
	codeCases := openAICompatibleProbeCases(route, kimiCode)
	for _, name := range []string{"thinking_string_none", "thinking_object_disabled", "thinking_bool_false", "enable_thinking_false"} {
		if hasRepairVariant(codeCases, name) {
			t.Fatalf("OpenCode Go Kimi K2.7 Code should not probe raw thinking control %q", name)
		}
	}
	if !hasRepairVariant(codeCases, "reasoning_effort_high") {
		t.Fatal("OpenCode Go Kimi K2.7 Code should probe reasoning_effort")
	}
	if hasRepairVariant(codeCases, "tool_required_file_read") {
		t.Fatal("OpenCode Go Kimi K2.7 Code should not probe unsupported required tool choice")
	}
	if hasRepairVariant(codeCases, "strict_tool_required_write") {
		t.Fatal("OpenCode Go Kimi K2.7 Code should not probe unsupported strict required tool choice")
	}

	kimi26 := discoveredOpenCodeModel(route, "kimi-k2.6")
	kimi26Cases := openAICompatibleProbeCases(route, kimi26)
	for _, name := range []string{"thinking_string_none", "thinking_object_disabled", "thinking_bool_false", "enable_thinking_false"} {
		if hasRepairVariant(kimi26Cases, name) {
			t.Fatalf("OpenCode Go Kimi K2.6 should not probe raw thinking control %q", name)
		}
	}
	if !hasRepairVariant(kimi26Cases, "reasoning_effort_high") {
		t.Fatal("OpenCode Go Kimi K2.6 should probe reasoning_effort")
	}

	kimiK3 := discoveredOpenCodeModel(route, "kimi-k3")
	k3Cases := openAICompatibleProbeCases(route, kimiK3)
	for _, name := range []string{"thinking_string_none", "thinking_object_disabled", "thinking_bool_false", "enable_thinking_false"} {
		if hasRepairVariant(k3Cases, name) {
			t.Fatalf("OpenCode Go Kimi K3 should not probe raw thinking control %q", name)
		}
	}
	if !hasRepairVariant(k3Cases, "reasoning_effort_high") {
		t.Fatal("OpenCode Go Kimi K3 should probe reasoning_effort")
	}
}

func TestMoonshotK27ProbeCasesSkipDisabledThinkingControls(t *testing.T) {
	t.Parallel()

	route := routes["moonshot"]
	kimiCode := route.Model(route, "kimi-k2.7-code")
	codeCases := openAICompatibleProbeCases(route, kimiCode)
	for _, name := range []string{"thinking_string_none", "thinking_object_disabled", "thinking_bool_false", "enable_thinking_false"} {
		if hasRepairVariant(codeCases, name) {
			t.Fatalf("Moonshot Kimi K2.7 Code should not probe disabled thinking control %q", name)
		}
	}
	if !hasRepairVariant(codeCases, "reasoning_effort_high") {
		t.Fatal("Moonshot Kimi K2.7 Code should keep non-disabled reasoning probes")
	}

	kimi26 := route.Model(route, "kimi-k2.6")
	kimi26Cases := openAICompatibleProbeCases(route, kimi26)
	if !hasRepairVariant(kimi26Cases, "thinking_object_disabled") {
		t.Fatal("Moonshot Kimi K2.6 should still probe disabled thinking controls")
	}
}

func TestXAIProbeCasesUseRouteProviderOptions(t *testing.T) {
	t.Parallel()

	testCase := findProbeCase(t, openAICompatibleProbeCases(routes["xai"], sigma.Model{}), "json_object")
	options := applyProbeOptions(testCase.Options)
	if _, ok := options.ProviderOptions[sigma.ProviderXAI]["extra_body"]; !ok {
		t.Fatalf("xai provider options = %#v, want extra_body", options.ProviderOptions[sigma.ProviderXAI])
	}
	if _, ok := options.ProviderOptions[sigma.ProviderOpenCode]; ok {
		t.Fatalf("unexpected OpenCode provider options: %#v", options.ProviderOptions[sigma.ProviderOpenCode])
	}
	if _, ok := options.ProviderOptions[sigma.ProviderFireworks]; ok {
		t.Fatalf("unexpected Fireworks provider options: %#v", options.ProviderOptions[sigma.ProviderFireworks])
	}
}

func TestOpenAIResponsesProbeCasesUseTypedResponseFormat(t *testing.T) {
	t.Parallel()

	testCase := findProbeCase(t, openAIResponsesProbeCases(routes["openai"], sigma.Model{}), "json_schema")
	options := applyProbeOptions(testCase.Options)
	if options.OpenAIOptions == nil || options.OpenAIOptions.ResponseFormat == nil {
		t.Fatalf("OpenAIOptions.ResponseFormat = %#v, want typed response format", options.OpenAIOptions)
	}
	if _, ok := options.ProviderOptions[sigma.ProviderOpenAI]["extra_body"]; ok {
		t.Fatalf("unexpected extra_body for OpenAI Responses: %#v", options.ProviderOptions[sigma.ProviderOpenAI])
	}
}

func TestOpenAICodexAuthOptionsUseOAuthTokenProvider(t *testing.T) {
	t.Parallel()

	route := routes["openai-codex"]
	options := applyProbeOptions(authOptions(route, routeCredential{
		codex: openAIProbeTestCredentials(),
	}))
	providerOptions := options.ProviderOptions[route.Provider]
	if providerOptions == nil {
		t.Fatal("missing provider options")
	}
	provider, ok := providerOptions["oauthTokenProvider"].(sigma.OAuthTokenProvider)
	if !ok {
		t.Fatalf("oauthTokenProvider type = %T, want sigma.OAuthTokenProvider", providerOptions["oauthTokenProvider"])
	}
	credential, err := provider.Token(context.Background(), route.Model(route, "gpt-5.1-codex"), sigma.Options{})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if credential.Value == "" {
		t.Fatal("credential value was empty")
	}
	if got, want := credential.Metadata["accountID"], "acct_probe"; got != want {
		t.Fatalf("accountID metadata = %v, want %q", got, want)
	}
}

func TestParseConfigEnablesOpenAICodexBrowserOAuth(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe", "-routes=openai-codex", "-codex-oauth-browser"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if !cfg.codexOAuthBrowser {
		t.Fatal("codexOAuthBrowser = false, want true")
	}
	if cfg.codexOAuth {
		t.Fatal("codexOAuth = true, want false")
	}
	if !reflect.DeepEqual(cfg.routes, []string{"openai-codex"}) {
		t.Fatalf("routes = %v, want openai-codex", cfg.routes)
	}
}

func TestParseConfigHandoffDefaultOff(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if cfg.handoff {
		t.Fatal("handoff = true, want default false")
	}
}

func TestParseConfigEnablesHandoff(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe", "-handoff", "-routes=sigmatest"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if !cfg.handoff {
		t.Fatal("handoff = false, want true")
	}
	if !reflect.DeepEqual(cfg.routes, []string{"sigmatest"}) {
		t.Fatalf("routes = %v, want sigmatest", cfg.routes)
	}
}

func TestParseConfigEnablesStructuredOutput(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe", "-structured-output", "-routes=zen"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if !cfg.structuredOutput {
		t.Fatal("structuredOutput = false, want true")
	}
	if !reflect.DeepEqual(cfg.routes, []string{"zen"}) {
		t.Fatalf("routes = %v, want zen", cfg.routes)
	}
}

func TestParseConfigEnablesImagesWithOpenAIDefaultRoute(t *testing.T) {
	oldCommandLine := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("sigma-surface-probe-test", flag.ContinueOnError)
	os.Args = []string{"sigma-surface-probe", "-images"}
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
		os.Args = oldArgs
	})

	cfg := parseConfig()
	if !cfg.images {
		t.Fatal("images = false, want true")
	}
	if !reflect.DeepEqual(cfg.routes, []string{"openai"}) {
		t.Fatalf("routes = %v, want openai image default", cfg.routes)
	}
}

func TestOpenAICodexCredentialRejectsMultipleOAuthModes(t *testing.T) {
	t.Parallel()

	_, err := openAICodexCredential(context.Background(), config{
		codexOAuth:        true,
		codexOAuthBrowser: true,
	})
	if err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("error = %v, want mutually exclusive OAuth mode error", err)
	}
}

func TestOpenAICodexProbeCasesUseURLImageInput(t *testing.T) {
	t.Parallel()

	testCase := findProbeCase(t, openAICodexProbeCases(routes["openai-codex"], sigma.Model{}), "image_input")
	image := testCase.Request.Messages[0].Content[1]
	if got, want := image.ImageSource, "url"; got != want {
		t.Fatalf("image source = %q, want %q", got, want)
	}
	if image.URL == "" {
		t.Fatal("image URL was empty")
	}
}

func TestImageRequestEmbedsValidVisiblePNG(t *testing.T) {
	t.Parallel()

	request := imageRequest()
	image := request.Messages[0].Content[1]
	if image.MIMEType != "image/png" || image.ImageSource != "base64" {
		t.Fatalf("image block = %#v, want base64 PNG", image)
	}
	data, err := base64.StdEncoding.DecodeString(image.Data)
	if err != nil {
		t.Fatalf("decode base64 image: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode PNG image: %v", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != 32 || bounds.Dy() != 32 {
		t.Fatalf("image dimensions = %dx%d, want 32x32", bounds.Dx(), bounds.Dy())
	}
	r, g, b, a := decoded.At(bounds.Min.X+16, bounds.Min.Y+16).RGBA()
	if a == 0 || r <= g || r <= b {
		t.Fatalf("center pixel rgba = %x/%x/%x/%x, want visible red pixel", r, g, b, a)
	}
}

func TestOpenAIImageRouteBuildsExpectedModel(t *testing.T) {
	t.Parallel()

	route := imageRoutes["openai"]
	if route.RegisterProvider == nil {
		t.Fatal("openai image route missing provider registration")
	}
	if got, want := route.Provider, sigma.ProviderOpenAI; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	model := route.Model(route, defaultOpenAIImageProbeModel)
	if model.Provider != sigma.ProviderOpenAI || model.API != sigma.ImageAPIOpenAIImages {
		t.Fatalf("image model provider/API = %q/%q", model.Provider, model.API)
	}
	assertMetadataString(t, model.ProviderMetadata, "baseURL", openai.DefaultBaseURL)
	assertMetadataString(t, model.ProviderMetadata, "probeSurface", "openai-images")
	assertMetadataStrings(t, model.ProviderMetadata, "apiKeyEnvVars", []string{"OPENAI_API_KEY"})
}

func TestOpenAIImageProbeCasesUseExpectedModels(t *testing.T) {
	t.Parallel()

	cases := openAIImageProbeCases(imageRoutes["openai"])
	if got, want := len(cases), 6; got != want {
		t.Fatalf("cases = %d, want %d", got, want)
	}
	if findImageProbeCase(t, cases, "variation").ModelID != defaultOpenAIImageVariationModel {
		t.Fatal("variation case did not use DALL-E 2 model")
	}
	if !findImageProbeCase(t, cases, "stream_partial").Stream {
		t.Fatal("stream_partial case did not enable streaming")
	}
	if !findImageProbeCase(t, cases, "responses_image_tool").ResponsesTool {
		t.Fatal("responses_image_tool case did not use Responses tool path")
	}
}

func TestRunOpenAIImageCasesUseExpectedRequestShapes(t *testing.T) {
	t.Parallel()

	var recordsMu sync.Mutex
	var records []imageProbeRequestRecord
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := captureImageProbeRequest(t, r)
		recordsMu.Lock()
		records = append(records, record)
		recordsMu.Unlock()
		if record.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"type":"image_generation.partial_image","partial_image_index":0,"b64_json":"cGFydGlhbA=="}`+"\n\n")
			_, _ = io.WriteString(w, `data: {"type":"image_generation.completed","data":[{"b64_json":"ZmluYWw="}]}`+"\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":"ZmluYWw="}]}`)
	}))
	t.Cleanup(server.Close)

	route := imageRoutes["openai"]
	route.BaseURL = server.URL
	for _, name := range []string{"generate", "edit_multipart", "edit_reference_json", "variation", "stream_partial"} {
		testCase := findImageProbeCase(t, route.Cases(route), name)
		result := runImageCase(context.Background(), route, testCase, routeCredential{apiKey: "key"})
		if result.Outcome != "ok" {
			t.Fatalf("%s result = %+v, want ok", name, result)
		}
		if result.Hint == "" {
			t.Fatalf("%s hint was empty", name)
		}
	}
	recordsMu.Lock()
	gotRecords := append([]imageProbeRequestRecord(nil), records...)
	recordsMu.Unlock()
	if got, want := len(gotRecords), 5; got != want {
		t.Fatalf("requests = %d, want %d: %#v", got, want, gotRecords)
	}
	assertImageProbeRecord(t, gotRecords[0], "/images/generations", defaultOpenAIImageProbeModel, false, false)
	assertImageProbeRecord(t, gotRecords[1], "/images/edits", defaultOpenAIImageProbeModel, true, false)
	assertImageProbeRecord(t, gotRecords[2], "/images/edits", defaultOpenAIImageProbeModel, true, false)
	assertImageProbeRecord(t, gotRecords[3], "/images/variations", defaultOpenAIImageVariationModel, true, false)
	assertImageProbeRecord(t, gotRecords[4], "/images/generations", defaultOpenAIImageProbeModel, false, true)
}

func TestRunOpenAIResponsesImageToolCaseDetectsImageOutput(t *testing.T) {
	t.Parallel()

	var sawImageToolMu sync.Mutex
	var sawImageTool bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/responses"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		tools, _ := payload["tools"].([]any)
		for _, tool := range tools {
			toolMap, _ := tool.(map[string]any)
			if toolMap["type"] == "image_generation" {
				sawImageToolMu.Lock()
				sawImageTool = true
				sawImageToolMu.Unlock()
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.created","response":{"id":"resp_image","status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_item.added","response_id":"resp_image","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","status":"in_progress"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.image_generation_call.partial_image","response_id":"resp_image","item_id":"ig_1","output_index":0,"partial_image_b64":"cGFydGlhbA=="}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_item.done","response_id":"resp_image","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","status":"completed","result":"ZmluYWw="}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"id":"resp_image","status":"completed","output":[{"type":"image_generation_call","id":"ig_1","status":"completed","result":"ZmluYWw="}]}}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	route := imageRoutes["openai"]
	route.BaseURL = server.URL
	testCase := findImageProbeCase(t, route.Cases(route), "responses_image_tool")
	result := runImageCase(context.Background(), route, testCase, routeCredential{apiKey: "key"})
	if result.Outcome != "ok" {
		t.Fatalf("result = %+v, want ok", result)
	}
	if got, want := result.Hint, "image_tool_output_seen"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	sawImageToolMu.Lock()
	gotSawImageTool := sawImageTool
	sawImageToolMu.Unlock()
	if !gotSawImageTool {
		t.Fatal("request did not include image_generation tool")
	}
}

func openAIProbeTestCredentials() *openai.CodexOAuthCredentials {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]string{
			"chatgpt_account_id": "acct_probe",
		},
	})
	token := header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	return &openai.CodexOAuthCredentials{AccessToken: token}
}

func TestAnthropicProbeCasesDoNotSendRawOpenAIExtraBody(t *testing.T) {
	t.Parallel()

	for _, testCase := range anthropicCompatibleProbeCases(routes["fireworks-anthropic"], sigma.Model{}) {
		options := applyProbeOptions(testCase.Options)
		if providerOptions := options.ProviderOptions[sigma.ProviderFireworks]; providerOptions != nil {
			if _, ok := providerOptions["extra_body"]; ok {
				t.Fatalf("%s set raw extra_body for Anthropic route: %#v", testCase.Name, providerOptions)
			}
		}
	}
}

func TestXAIRouteRegistrationBuildsClient(t *testing.T) {
	t.Parallel()

	route := routes["xai"]
	registry := sigma.NewRegistry()
	if err := route.RegisterProvider(registry, route); err != nil {
		t.Fatalf("RegisterProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(route.Model(route, "grok-code-fast-1")); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestNVIDIARouteRegistrationBuildsClient(t *testing.T) {
	t.Parallel()

	route := routes["nvidia"]
	registry := sigma.NewRegistry()
	if err := route.RegisterProvider(registry, route); err != nil {
		t.Fatalf("RegisterProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(route.Model(route, defaultNVIDIAProbeModel)); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestRunCaseKeepsDistinctRouteNames(t *testing.T) {
	t.Parallel()

	model := sigma.Model{ID: "same-provider-model", Provider: sigma.ProviderFireworks, API: sigmatest.TextAPI}
	for _, routeName := range []string{"fireworks-openai", "fireworks-anthropic"} {
		route := routes[routeName]
		provider := sigmatest.NewFauxProvider()
		registry := sigma.NewRegistry()
		if err := registry.RegisterTextProvider(sigma.ProviderFireworks, provider); err != nil {
			t.Fatalf("RegisterTextProvider returned error: %v", err)
		}
		if err := registry.RegisterModel(model); err != nil {
			t.Fatalf("RegisterModel returned error: %v", err)
		}

		client := sigma.NewClient(sigma.WithRegistry(registry))
		result := runCase(context.Background(), route, client, model, singleTurnCase("basic", "", basicRequest("hi"), nil), routeCredential{apiKey: "key"}, "basic")
		if result.Route != routeName {
			t.Fatalf("route = %q, want %q", result.Route, routeName)
		}
	}
}

func TestGenerateHandoffSourceBuildsToolContext(t *testing.T) {
	t.Parallel()

	route := handoffProbeRoute(t, "handoff-source",
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content:    []sigma.ContentBlock{sigma.ToolCallBlock("call_double", "double_number", map[string]any{"value": 21})},
			StopReason: sigma.StopReasonToolCalls,
		}},
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("42")},
		}},
	)

	source, result := generateHandoffSource(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{})
	if result.Outcome != "ok" {
		t.Fatalf("result = %+v, want ok", result)
	}
	if len(source.Messages) != 4 {
		t.Fatalf("source messages = %d, want 4", len(source.Messages))
	}
	if got, want := source.Messages[1].Content[0].ToolCallID, "call_double"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
	if got, want := source.Messages[2].Role, sigma.RoleTool; got != want {
		t.Fatalf("tool result role = %q, want %q", got, want)
	}
	if got, want := source.Messages[2].ToolName, "double_number"; got != want {
		t.Fatalf("tool result name = %q, want %q", got, want)
	}
	if got, want := source.Messages[2].Content[0].Text, "42"; got != want {
		t.Fatalf("tool result text = %q, want %q", got, want)
	}
}

func TestGenerateHandoffSourceSkipsWithoutToolCall(t *testing.T) {
	t.Parallel()

	route := handoffProbeRoute(t, "handoff-no-tool",
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("no tool")},
		}},
	)

	_, result := generateHandoffSource(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{})
	if got, want := result.Outcome, "skipped"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if !strings.Contains(result.Error, "did not emit a tool call") {
		t.Fatalf("error = %q, want no-tool-call diagnostic", result.Error)
	}
}

func TestRunHandoffTargetEmitsSourceMetadata(t *testing.T) {
	t.Parallel()

	sourceRoute := handoffProbeRoute(t, "handoff-source")
	targetRoute := handoffProbeRoute(t, "handoff-target",
		sigmatest.Script{Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("Hello, handoff successful.")},
		}},
	)
	source := handoffSource{
		Route: sourceRoute,
		Model: sourceRoute.Model(sourceRoute, "source-model"),
		Messages: []sigma.Message{
			sigma.UserText("Use the tool."),
			{
				Role:    sigma.RoleAssistant,
				Content: []sigma.ContentBlock{sigma.ToolCallBlock("call_double", "double_number", map[string]any{"value": 21})},
			},
			{Role: sigma.RoleTool, ToolCallID: "call_double", ToolName: "double_number", Content: []sigma.ContentBlock{sigma.Text("42")}},
			{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.Text("42")}},
		},
	}
	target := handoffSource{
		Route:      targetRoute,
		Model:      targetRoute.Model(targetRoute, "target-model"),
		Credential: routeCredential{apiKey: "key"},
	}

	result := runHandoffTarget(context.Background(), source, target)
	if result.Outcome != "ok" {
		t.Fatalf("result = %+v, want ok", result)
	}
	if got, want := result.SourceRoute, "handoff-source"; got != want {
		t.Fatalf("source route = %q, want %q", got, want)
	}
	if got, want := result.SourceModel, "source-model"; got != want {
		t.Fatalf("source model = %q, want %q", got, want)
	}
}

func TestRunHandoffProbesEmitsPairwiseResults(t *testing.T) {
	oldRoutes := routes
	t.Cleanup(func() {
		routes = oldRoutes
	})

	routes = map[string]routeSpec{
		"handoff-a": handoffProbeRoute(t, "handoff-a",
			sigmatest.Script{Final: sigma.AssistantMessage{
				Content:    []sigma.ContentBlock{sigma.ToolCallBlock("call_a", "double_number", map[string]any{"value": 21})},
				StopReason: sigma.StopReasonToolCalls,
			}},
			sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("42")}}},
			sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("Hello, handoff successful.")}}},
		),
		"handoff-b": handoffProbeRoute(t, "handoff-b",
			sigmatest.Script{Final: sigma.AssistantMessage{
				Content:    []sigma.ContentBlock{sigma.ToolCallBlock("call_b", "double_number", map[string]any{"value": 21})},
				StopReason: sigma.StopReasonToolCalls,
			}},
			sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("42")}}},
			sigmatest.Script{Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("Hello, handoff successful.")}}},
		),
	}
	t.Setenv("SIGMATEST_API_KEY", "key")

	var emitted []probeResult
	runHandoffProbes(context.Background(), config{
		routes: []string{"handoff-a", "handoff-b"},
		models: map[string]bool{"model": true},
	}, func(result probeResult) {
		emitted = append(emitted, result)
	})

	if len(emitted) != 4 {
		t.Fatalf("emitted results = %d, want 4: %#v", len(emitted), emitted)
	}
	if emitted[0].Case != "handoff_source" || emitted[0].Outcome != "ok" {
		t.Fatalf("first source result = %+v, want ok handoff_source", emitted[0])
	}
	if emitted[1].Case != "handoff_source" || emitted[1].Outcome != "ok" {
		t.Fatalf("second source result = %+v, want ok handoff_source", emitted[1])
	}
	if emitted[2].Case != "handoff_replay" || emitted[2].SourceRoute != "handoff-a" || emitted[2].Route != "handoff-b" {
		t.Fatalf("first pairwise result = %+v, want handoff-a -> handoff-b", emitted[2])
	}
	if emitted[3].Case != "handoff_replay" || emitted[3].SourceRoute != "handoff-b" || emitted[3].Route != "handoff-a" {
		t.Fatalf("second pairwise result = %+v, want handoff-b -> handoff-a", emitted[3])
	}
}

func TestProbeModelEachEmitsEachCompletedCase(t *testing.T) {
	t.Parallel()

	route := sigmatestProbeRouteWithCases(t, []probeCase{
		singleTurnCase("first", "first case", basicRequest("first"), nil),
		singleTurnCase("second", "second case", basicRequest("second"), nil),
	}, sigmatest.Script{}, sigmatest.Script{})

	var emitted []probeResult
	probeModelEach(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{}, func(result probeResult) {
		emitted = append(emitted, result)
	})
	if len(emitted) != 2 {
		t.Fatalf("emitted length = %d, want 2", len(emitted))
	}
	if got, want := emitted[0].Case, "first"; got != want {
		t.Fatalf("first emitted case = %q, want %q", got, want)
	}
	if got, want := emitted[1].Case, "second"; got != want {
		t.Fatalf("second emitted case = %q, want %q", got, want)
	}
}

func TestStructuredOutputProbeModelEachRunsOnlyStructuredCases(t *testing.T) {
	t.Parallel()

	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{
		singleTurnCase("basic_text", "basic case", basicRequest("basic"), nil),
		singleTurnCase("json_object", "JSON object mode", basicRequest("json object"), nil),
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("json schema"), nil),
	}, sigmatest.Script{}, sigmatest.Script{})

	var emitted []probeResult
	probeModelEach(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{structuredOutput: true}, func(result probeResult) {
		emitted = append(emitted, result)
	})
	if len(emitted) != 2 {
		t.Fatalf("emitted length = %d, want 2", len(emitted))
	}
	if got, want := emitted[0].Case, "json_object"; got != want {
		t.Fatalf("first case = %q, want %q", got, want)
	}
	if got, want := emitted[0].Hint, "json_object_supported"; got != want {
		t.Fatalf("first hint = %q, want %q", got, want)
	}
	if got, want := emitted[1].Case, "json_schema"; got != want {
		t.Fatalf("second case = %q, want %q", got, want)
	}
	if got, want := emitted[1].Hint, "json_schema_supported"; got != want {
		t.Fatalf("second hint = %q, want %q", got, want)
	}
	recommendation, ok := recommendationFor(emitted[1])
	if !ok {
		t.Fatal("recommendationFor returned false")
	}
	if got, want := recommendation.Evidence, "json_schema supported by json_schema"; got != want {
		t.Fatalf("evidence = %q, want %q", got, want)
	}
}

func TestStructuredOutputProbeModelEachSkipsNonOpenAICompatibleModels(t *testing.T) {
	t.Parallel()

	route := sigmatestProbeRouteWithCases(t, []probeCase{
		singleTurnCase("json_object", "JSON object mode", basicRequest("json object"), nil),
	}, sigmatest.Script{})

	var emitted []probeResult
	probeModelEach(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{structuredOutput: true}, func(result probeResult) {
		emitted = append(emitted, result)
	})
	if len(emitted) != 1 {
		t.Fatalf("emitted length = %d, want 1", len(emitted))
	}
	if got, want := emitted[0].Case, "structured_output"; got != want {
		t.Fatalf("case = %q, want %q", got, want)
	}
	if got, want := emitted[0].Attempt, "unsupported_api"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if got, want := emitted[0].Outcome, "skipped"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if _, ok := recommendationFor(emitted[0]); ok {
		t.Fatal("recommendationFor returned true for incompatible API skip")
	}
}

func TestProbeModelPrefersTargetedRepairOverAvailabilityCheck(t *testing.T) {
	t.Parallel()

	route := sigmatestProbeRoute(
		t,
		sigmatest.Script{Err: errors.New("strict schema failed")},
		sigmatest.Script{},
		sigmatest.Script{},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{repair: true})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if got, want := results[0].Case, "json_schema"; got != want {
		t.Fatalf("case = %q, want %q", got, want)
	}
	if got, want := results[0].Attempt, "json_schema_more_tokens"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if got, want := results[0].Outcome, "fixed_by_repair_variant"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if got, want := results[0].OriginalError, "strict schema failed"; got != want {
		t.Fatalf("original error = %q, want %q", got, want)
	}
	if got, want := results[0].Hint, "json_schema_needs_larger_output_budget"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	assertFailedAttempts(t, results[0].FailedAttempts, []failedAttempt{
		{Attempt: "json_schema", Error: "strict schema failed"},
	})
	recommendation, ok := recommendationFor(results[0])
	if !ok {
		t.Fatal("recommendationFor returned false")
	}
	if recommendation.Route != "sigmatest" || recommendation.Model != "model" ||
		recommendation.Case != "json_schema" || recommendation.Hint != "json_schema_needs_larger_output_budget" ||
		recommendation.Evidence != "json_schema repaired by json_schema_more_tokens" {
		t.Fatalf("recommendation = %+v", recommendation)
	}
}

func TestStructuredOutputProbeReportsJSONSchemaFallbackToJSONObject(t *testing.T) {
	t.Parallel()

	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("json schema"), nil),
	},
		sigmatest.Script{Err: errors.New("strict schema failed")},
		sigmatest.Script{},
		sigmatest.Script{Err: errors.New("larger schema failed")},
		sigmatest.Script{},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{structuredOutput: true})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if got, want := results[0].Outcome, "fixed_by_repair_variant"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if got, want := results[0].Attempt, "json_object_fallback"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if got, want := results[0].Hint, "json_schema_rejected_json_object_ok"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	assertFailedAttempts(t, results[0].FailedAttempts, []failedAttempt{
		{Attempt: "json_schema", Error: "strict schema failed"},
		{Attempt: "json_schema_more_tokens", Error: "larger schema failed"},
	})
}

func TestStructuredOutputProbeReportsPromptJSONFallback(t *testing.T) {
	t.Parallel()

	route := openAICompatibleSigmatestProbeRoute(t, []probeCase{
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("json schema"), nil),
	},
		sigmatest.Script{Err: errors.New("strict schema failed")},
		sigmatest.Script{},
		sigmatest.Script{Err: errors.New("larger schema failed")},
		sigmatest.Script{Err: errors.New("json object failed")},
		sigmatest.Script{},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{structuredOutput: true})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if got, want := results[0].Outcome, "fixed_by_repair_variant"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if got, want := results[0].Attempt, "manual_json"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if got, want := results[0].Hint, "structured_output_rejected_prompt_json_ok"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	assertFailedAttempts(t, results[0].FailedAttempts, []failedAttempt{
		{Attempt: "json_schema", Error: "strict schema failed"},
		{Attempt: "json_schema_more_tokens", Error: "larger schema failed"},
		{Attempt: "json_object_fallback", Error: "json object failed"},
	})
}

func TestProbeModelReportsAvailabilityCheckSeparately(t *testing.T) {
	t.Parallel()

	route := sigmatestProbeRoute(
		t,
		sigmatest.Script{Err: errors.New("strict schema failed")},
		sigmatest.Script{},
		sigmatest.Script{Err: errors.New("larger schema failed")},
		sigmatest.Script{Err: errors.New("json object failed")},
		sigmatest.Script{Err: errors.New("manual json failed")},
	)
	results := collectProbeModel(context.Background(), route, "model", routeCredential{apiKey: "key"}, config{repair: true})
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if got, want := results[0].Case, "json_schema"; got != want {
		t.Fatalf("case = %q, want %q", got, want)
	}
	if got, want := results[0].Attempt, "minimal_basic_text"; got != want {
		t.Fatalf("attempt = %q, want %q", got, want)
	}
	if got, want := results[0].Outcome, "availability_ok_after_failure"; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if got, want := results[0].OriginalError, "strict schema failed"; got != want {
		t.Fatalf("original error = %q, want %q", got, want)
	}
	if got, want := results[0].Hint, "minimal_text_available_after_failure"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	assertFailedAttempts(t, results[0].FailedAttempts, []failedAttempt{
		{Attempt: "json_schema", Error: "strict schema failed"},
		{Attempt: "json_schema_more_tokens", Error: "larger schema failed"},
		{Attempt: "json_object_fallback", Error: "json object failed"},
		{Attempt: "manual_json", Error: "manual json failed"},
	})
}

func TestRepairVariantsCoverTargetedFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "basic_text", want: "basic_text_more_tokens"},
		{name: "cache_ephemeral", want: "cache_none_more_tokens"},
		{name: "image_input", want: "text_only_fallback"},
		{name: "thinking_string_none", want: "thinking_object_disabled_repair"},
		{name: "reasoning_effort_high", want: "typed_reasoning_effort_high"},
		{name: "json_schema", want: "json_object_fallback"},
		{name: "logprobs", want: "no_logprobs_more_tokens"},
		{name: "tool_required_file_read", want: "tool_auto_more_turns"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !hasRepairVariant(repairVariants(routes["zen"], probeCase{Name: tt.name}), tt.want) {
				t.Fatalf("repairVariants(%q) missing %q", tt.name, tt.want)
			}
		})
	}
}

func TestImageRepairVariantsTryURLBeforeTextOnly(t *testing.T) {
	t.Parallel()

	var imageURLIndex int
	var textOnlyIndex int
	for i, variant := range repairVariants(routes["xai"], probeCase{Name: "image_input"}) {
		switch variant.Name {
		case "image_url_fallback":
			imageURLIndex = i
		case "text_only_fallback":
			textOnlyIndex = i
		}
	}
	if imageURLIndex == 0 {
		t.Fatal("image_url_fallback missing from image repair variants")
	}
	if textOnlyIndex == 0 {
		t.Fatal("text_only_fallback missing from image repair variants")
	}
	if imageURLIndex > textOnlyIndex {
		t.Fatalf("image_url_fallback index = %d, text_only_fallback index = %d; want URL fallback first", imageURLIndex, textOnlyIndex)
	}
}

func TestClassifyFailure(t *testing.T) {
	t.Parallel()

	route := routes["zen"]
	model := sigma.Model{Provider: sigma.ProviderOpenCode, ID: "gpt-5.1-codex"}
	if got := classifyFailure(route, model, errors.New("unknown parameter: 'thinking'")); got != "sigma_request_shape" {
		t.Fatalf("unknown parameter classification = %q", got)
	}
	if got := classifyFailure(routes["openai-codex"], model, errors.New("status=400 body={\"detail\":\"Unsupported parameter: max_output_tokens\"}")); got != "sigma_request_shape" {
		t.Fatalf("unsupported-parameter classification = %q", got)
	}
	if got := classifyFailure(routes["openai-codex"], model, errors.New("status=400 body={\"detail\":\"Instructions are required\"}")); got != "sigma_request_shape" {
		t.Fatalf("instructions-required classification = %q", got)
	}
	if got := classifyFailure(routes["openai-codex"], model, errors.New("status=400 body={\"detail\":\"Store must be set to false\"}")); got != "sigma_request_shape" {
		t.Fatalf("store-false classification = %q", got)
	}
	if got := classifyFailure(route, model, errors.New("model does not support image input")); got != "provider_capability_limit" {
		t.Fatalf("image classification = %q", got)
	}
	model.ID = "claude-opus-4-6"
	if got := classifyFailure(route, model, errors.New("No provider available")); got != "upstream_availability" {
		t.Fatalf("availability classification = %q", got)
	}
	model.ID = "accounts/fireworks/routers/kimi-k2p6-turbo"
	if got := classifyFailure(routes["fireworks-anthropic"], model, errors.New("status=404 body={\"error\":{\"code\":\"NOT_FOUND\",\"message\":\"Path not found: /messages\"}}")); got != "upstream_availability" {
		t.Fatalf("path-not-found classification = %q", got)
	}
	model.ID = "gpt-5.1-codex"
	if got := classifyFailure(routes["openai-codex"], model, errors.New("status=400 body={\"detail\":\"The 'gpt-5.1-codex' model is not supported when using Codex with a ChatGPT account.\"}")); got != "upstream_availability" {
		t.Fatalf("chatgpt-account-unsupported classification = %q", got)
	}
}

func TestSummaryCounts(t *testing.T) {
	t.Parallel()

	var totals summary
	for _, outcome := range []string{
		"ok",
		"skipped",
		"sigma_request_shape",
		"provider_capability_limit",
		"upstream_availability",
		"fixed_by_repair_variant",
		"availability_ok_after_failure",
		"other",
	} {
		totals.add(probeResult{Outcome: outcome})
	}
	if totals.Total != 8 || totals.OK != 1 || totals.Skipped != 1 ||
		totals.SigmaRequestShape != 1 || totals.ProviderCapabilityLimit != 1 ||
		totals.UpstreamAvailability != 1 || totals.FixedByRepairVariant != 1 ||
		totals.AvailabilityOKAfterFailure != 1 ||
		totals.NoWorkingAttempt != 1 {
		t.Fatalf("summary = %+v", totals)
	}
}

func TestParseModelIDs(t *testing.T) {
	t.Parallel()

	ids, err := parseModelIDs([]byte(`{"data":[{"id":"b"},{"id":"a"}]}`))
	if err != nil {
		t.Fatalf("parseModelIDs returned error: %v", err)
	}
	if got, want := ids[0], "a"; got != want {
		t.Fatalf("first id = %q, want %q", got, want)
	}
	if got, want := ids[1], "b"; got != want {
		t.Fatalf("second id = %q, want %q", got, want)
	}
}

func hasRepairVariant(variants []probeCase, name string) bool {
	for _, variant := range variants {
		if variant.Name == name {
			return true
		}
	}
	return false
}

func findProbeCase(t *testing.T, cases []probeCase, name string) probeCase {
	t.Helper()

	for _, testCase := range cases {
		if testCase.Name == name {
			return testCase
		}
	}
	t.Fatalf("probe case %q not found", name)
	return probeCase{}
}

func findImageProbeCase(t *testing.T, cases []imageProbeCase, name string) imageProbeCase {
	t.Helper()

	for _, testCase := range cases {
		if testCase.Name == name {
			return testCase
		}
	}
	t.Fatalf("image probe case %q not found", name)
	return imageProbeCase{}
}

type imageProbeRequestRecord struct {
	Path     string
	Model    string
	HasImage bool
	Stream   bool
}

func captureImageProbeRequest(t *testing.T, r *http.Request) imageProbeRequestRecord {
	t.Helper()

	record := imageProbeRequestRecord{Path: r.URL.Path}
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		record.Model = r.FormValue("model")
		record.HasImage = len(r.MultipartForm.File["image"]) > 0
		record.Stream = r.FormValue("stream") == "true"
		return record
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	record.Model, _ = payload["model"].(string)
	record.Stream, _ = payload["stream"].(bool)
	_, record.HasImage = payload["images"]
	return record
}

func assertImageProbeRecord(t *testing.T, record imageProbeRequestRecord, path string, model string, hasImage bool, stream bool) {
	t.Helper()

	if record.Path != path {
		t.Fatalf("path = %q, want %q (record %#v)", record.Path, path, record)
	}
	if record.Model != model {
		t.Fatalf("model = %q, want %q (record %#v)", record.Model, model, record)
	}
	if record.HasImage != hasImage {
		t.Fatalf("hasImage = %v, want %v (record %#v)", record.HasImage, hasImage, record)
	}
	if record.Stream != stream {
		t.Fatalf("stream = %v, want %v (record %#v)", record.Stream, stream, record)
	}
}

func applyProbeOptions(opts []sigma.Option) sigma.Options {
	var options sigma.Options
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

func assertFailedAttempts(t *testing.T, got []failedAttempt, want []failedAttempt) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("failed attempts = %#v, want %#v", got, want)
	}
}

func sigmatestProbeRoute(t *testing.T, scripts ...sigmatest.Script) routeSpec {
	t.Helper()

	return sigmatestProbeRouteWithCases(t, []probeCase{
		singleTurnCase("json_schema", "strict JSON schema", basicRequest("Return JSON exactly {\"answer\":\"ok\"}."), nil),
	}, scripts...)
}

func openAICompatibleSigmatestProbeRoute(t *testing.T, cases []probeCase, scripts ...sigmatest.Script) routeSpec {
	t.Helper()

	provider := openAICompatibleFauxProvider{sigmatest.NewFauxProvider(scripts...)}
	return routeSpec{
		Name:      "sigmatest-openai",
		Provider:  sigmatest.ProviderID,
		BaseURL:   "https://example.test",
		APIKeyEnv: "SIGMATEST_API_KEY",
		RegisterProvider: func(registry *sigma.Registry, _ routeSpec) error {
			return registry.RegisterTextProvider(sigmatest.ProviderID, provider)
		},
		Model: func(_ routeSpec, id string) sigma.Model {
			model := sigmatest.TextModel()
			model.ID = sigma.ModelID(id)
			model.API = sigma.APIOpenAICompletions
			return model
		},
		Cases: func(routeSpec, sigma.Model) []probeCase {
			return cases
		},
	}
}

type openAICompatibleFauxProvider struct {
	*sigmatest.FauxProvider
}

func (p openAICompatibleFauxProvider) API() sigma.API {
	return sigma.APIOpenAICompletions
}

func sigmatestProbeRouteWithCases(t *testing.T, cases []probeCase, scripts ...sigmatest.Script) routeSpec {
	t.Helper()

	provider := sigmatest.NewFauxProvider(scripts...)
	return routeSpec{
		Name:      "sigmatest",
		Provider:  sigmatest.ProviderID,
		BaseURL:   "https://example.test",
		APIKeyEnv: "SIGMATEST_API_KEY",
		RegisterProvider: func(registry *sigma.Registry, _ routeSpec) error {
			return registry.RegisterTextProvider(sigmatest.ProviderID, provider)
		},
		Model: func(_ routeSpec, id string) sigma.Model {
			model := sigmatest.TextModel()
			model.ID = sigma.ModelID(id)
			return model
		},
		Cases: func(routeSpec, sigma.Model) []probeCase {
			return cases
		},
	}
}

func handoffProbeRoute(t *testing.T, name string, scripts ...sigmatest.Script) routeSpec {
	t.Helper()

	providerID := sigma.ProviderID(name)
	provider := sigmatest.NewFauxProvider(scripts...)
	return routeSpec{
		Name:      name,
		Provider:  providerID,
		BaseURL:   "https://example.test",
		APIKeyEnv: "SIGMATEST_API_KEY",
		RegisterProvider: func(registry *sigma.Registry, _ routeSpec) error {
			return registry.RegisterTextProvider(providerID, provider)
		},
		Model: func(route routeSpec, id string) sigma.Model {
			model := sigmatest.TextModel()
			model.ID = sigma.ModelID(id)
			model.Provider = route.Provider
			return model
		},
		Cases: func(routeSpec, sigma.Model) []probeCase {
			return nil
		},
	}
}

func assertMetadataString(t *testing.T, metadata map[string]any, key string, want string) {
	t.Helper()

	if got, ok := metadata[key].(string); !ok || got != want {
		t.Fatalf("metadata[%q] = %#v, want %q", key, metadata[key], want)
	}
}

func assertMetadataStrings(t *testing.T, metadata map[string]any, key string, want []string) {
	t.Helper()

	got, ok := metadata[key].([]string)
	if !ok {
		t.Fatalf("metadata[%q] type = %T, want []string", key, metadata[key])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata[%q] = %v, want %v", key, got, want)
	}
}
