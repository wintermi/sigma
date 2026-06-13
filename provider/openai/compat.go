// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/wintermi/sigma"
)

type completionsCompat struct {
	supportsStore                               bool
	supportsDeveloperRole                       bool
	reasoningFormat                             sigma.OpenAICompletionsReasoningFormat
	supportsReasoningEffort                     bool
	supportsStreamingUsage                      bool
	supportsStrictTools                         bool
	supportsToolStream                          bool
	supportsJSONSchemaResponseFormat            bool
	maxTokensField                              sigma.OpenAICompletionsMaxTokensField
	cacheControlFormat                          sigma.OpenAICompletionsCacheControlFormat
	supportsSessionAffinity                     bool
	requiresToolResultName                      bool
	requiresAssistantAfterToolResult            bool
	requiresToolsForToolHistory                 bool
	requiresReasoningContentOnAssistantMessages bool
	openRouterRouting                           *sigma.OpenRouterRoutingPreference
	vercelAIGatewayRouting                      *sigma.VercelAIGatewayRoutingPreference
}

func openAICompletionsCompat(model sigma.Model, baseURL string) completionsCompat {
	compat := detectedCompletionsCompat(model, baseURL)
	if model.OpenAICompletionsCompat == nil {
		return compat
	}

	override := model.OpenAICompletionsCompat
	compat.supportsStore = supportOverride(compat.supportsStore, override.SupportsStore)
	compat.supportsDeveloperRole = supportOverride(compat.supportsDeveloperRole, override.SupportsDeveloperRole)
	compat.supportsReasoningEffort = supportOverride(compat.supportsReasoningEffort, override.SupportsReasoningEffort)
	compat.supportsStreamingUsage = supportOverride(compat.supportsStreamingUsage, override.SupportsStreamingUsage)
	compat.supportsStrictTools = supportOverride(compat.supportsStrictTools, override.SupportsStrictTools)
	compat.supportsToolStream = supportOverride(compat.supportsToolStream, override.SupportsToolStream)
	compat.supportsJSONSchemaResponseFormat = supportOverride(
		compat.supportsJSONSchemaResponseFormat,
		override.SupportsJSONSchemaResponseFormat,
	)
	compat.supportsSessionAffinity = supportOverride(compat.supportsSessionAffinity, override.SupportsSessionAffinity)
	compat.requiresToolResultName = supportOverride(compat.requiresToolResultName, override.RequiresToolResultName)
	compat.requiresAssistantAfterToolResult = supportOverride(compat.requiresAssistantAfterToolResult, override.RequiresAssistantAfterToolResult)
	compat.requiresToolsForToolHistory = supportOverride(compat.requiresToolsForToolHistory, override.RequiresToolsForToolHistory)
	compat.requiresReasoningContentOnAssistantMessages = supportOverride(
		compat.requiresReasoningContentOnAssistantMessages,
		override.RequiresReasoningContentOnAssistantMessages,
	)
	if override.ReasoningFormat != sigma.OpenAICompletionsReasoningDefault {
		compat.reasoningFormat = override.ReasoningFormat
	}
	if override.MaxTokensField != sigma.OpenAICompletionsMaxTokensDefault {
		compat.maxTokensField = override.MaxTokensField
	}
	if override.CacheControlFormat != sigma.OpenAICompletionsCacheControlDefault {
		compat.cacheControlFormat = override.CacheControlFormat
	}
	if override.OpenRouterRouting != nil {
		compat.openRouterRouting = override.OpenRouterRouting
	}
	if override.VercelAIGatewayRouting != nil {
		compat.vercelAIGatewayRouting = override.VercelAIGatewayRouting
	}
	return compat
}

func detectedCompletionsCompat(model sigma.Model, baseURL string) completionsCompat {
	provider := model.Provider
	compat := conservativeCompletionsCompat()
	host := baseURLHost(baseURL)
	providerText := strings.ToLower(string(provider))

	switch {
	case provider == sigma.ProviderOpenAI || host == "api.openai.com":
		return completionsCompat{
			supportsStore:                    true,
			supportsDeveloperRole:            true,
			reasoningFormat:                  sigma.OpenAICompletionsReasoningEffort,
			supportsReasoningEffort:          true,
			supportsStreamingUsage:           true,
			supportsStrictTools:              true,
			supportsJSONSchemaResponseFormat: true,
			maxTokensField:                   sigma.OpenAICompletionsMaxTokens,
			cacheControlFormat:               sigma.OpenAICompletionsCacheControlMessage,
		}
	case provider == sigma.ProviderOpenRouter || strings.Contains(host, "openrouter.ai"):
		compat.supportsStreamingUsage = true
		compat.cacheControlFormat = sigma.OpenAICompletionsCacheControlMessage
		compat.reasoningFormat = sigma.OpenAICompletionsReasoningObject
		if strings.HasPrefix(string(model.ID), "anthropic/") {
			compat.cacheControlFormat = sigma.OpenAICompletionsCacheControlAnthropic
		}
		compat.openRouterRouting = &sigma.OpenRouterRoutingPreference{}
	case provider == sigma.ProviderDeepSeek || strings.Contains(host, "deepseek.com"):
		compat.reasoningFormat = sigma.OpenAICompletionsReasoningUnsupported
	case provider == sigma.ProviderFireworks || strings.Contains(host, "fireworks.ai"):
		compat.reasoningFormat = sigma.OpenAICompletionsReasoningFireworks
		compat.supportsStreamingUsage = true
		compat.supportsStrictTools = true
	case provider == sigma.ProviderMoonshotAI || provider == sigma.ProviderMoonshotAICN || strings.Contains(host, "api.moonshot."):
		compat.reasoningFormat = sigma.OpenAICompletionsReasoningDeepSeek
		compat.supportsReasoningEffort = false
		compat.supportsStreamingUsage = true
		compat.maxTokensField = sigma.OpenAICompletionsMaxTokens
	case provider == sigma.ProviderOpenCode || provider == sigma.ProviderOpenCodeGo || strings.Contains(host, "opencode.ai"):
		compat.reasoningFormat = sigma.OpenAICompletionsReasoningEffort
		compat.supportsStreamingUsage = true
		compat.supportsStrictTools = true
		compat.maxTokensField = sigma.OpenAICompletionsMaxTokens
	case provider == sigma.ProviderTogether || strings.Contains(host, "together.ai"):
	case provider == sigma.ProviderCerebras || strings.Contains(host, "cerebras.ai"):
	case provider == sigma.ProviderXAI || strings.Contains(host, "x.ai"):
		compat.supportsReasoningEffort = false
		compat.supportsStreamingUsage = true
		compat.supportsStrictTools = true
		compat.maxTokensField = sigma.OpenAICompletionsMaxCompletionTokens
	case providerText == "z.ai" || providerText == "zai" || strings.Contains(host, "z.ai"):
	case providerText == "cloudflare" || strings.Contains(host, "workers-ai") || strings.Contains(host, "cloudflare.com"):
	case strings.Contains(host, "ai-gateway.vercel.sh") || strings.Contains(host, "gateway.ai.vercel.com"):
		compat.supportsStreamingUsage = true
		compat.vercelAIGatewayRouting = &sigma.VercelAIGatewayRoutingPreference{}
	case isLocalHost(host):
		return compat
	}
	return compat
}

func conservativeCompletionsCompat() completionsCompat {
	return completionsCompat{
		reasoningFormat:                  sigma.OpenAICompletionsReasoningUnsupported,
		supportsReasoningEffort:          true,
		supportsJSONSchemaResponseFormat: true,
		maxTokensField:                   sigma.OpenAICompletionsMaxTokens,
		cacheControlFormat:               sigma.OpenAICompletionsCacheControlUnsupported,
	}
}

func supportOverride(defaultValue bool, override sigma.OpenAICompatSupport) bool {
	switch override {
	case sigma.OpenAICompatSupported:
		return true
	case sigma.OpenAICompatUnsupported:
		return false
	default:
		return defaultValue
	}
}

func baseURLHost(baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func isLocalHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	default:
		return strings.HasSuffix(host, ".local")
	}
}

func openAICompatibleBaseURL(model sigma.Model, fallback string) string {
	baseURL := fallback
	if value, ok := model.ProviderMetadata[sigma.MetadataOpenAICompatibleBaseURL].(string); ok && strings.TrimSpace(value) != "" {
		baseURL = value
	} else if value, ok := model.ProviderMetadata["baseURL"].(string); ok && strings.TrimSpace(value) != "" {
		baseURL = value
	}
	return strings.TrimRight(baseURL, "/")
}

func addOpenAICompatibleModelHeaders(req *http.Request, model sigma.Model) {
	for key, value := range openAICompatibleModelHeaders(model) {
		if unsafeCredentialHeader(key) {
			continue
		}
		req.Header.Set(key, value)
	}
}

func openAICompatibleModelHeaders(model sigma.Model) map[string]string {
	raw, ok := model.ProviderMetadata[sigma.MetadataOpenAICompatibleHeaders]
	if !ok {
		raw = model.ProviderMetadata["headers"]
	}
	switch headers := raw.(type) {
	case map[string]string:
		return headers
	case map[string]any:
		copied := make(map[string]string, len(headers))
		for key, value := range headers {
			text, ok := value.(string)
			if !ok {
				continue
			}
			copied[key] = text
		}
		return copied
	default:
		return nil
	}
}

func unsafeCredentialHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization":
		return true
	default:
		return false
	}
}

func routingMap(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
