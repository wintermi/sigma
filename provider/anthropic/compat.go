// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"net/url"
	"strings"

	"github.com/wintermi/sigma"
)

// MessagesCompat describes Anthropic Messages compatibility differences for
// Anthropic-compatible endpoints. Leave fields at their zero value to use
// provider/base-URL detection, or set them with WithMessagesCompat for tests or
// custom routers.
type MessagesCompat struct {
	EagerToolInputStreaming bool
	LongCacheRetention      bool
	SessionAffinityHeaders  bool
	CacheControlOnTools     bool
	AdaptiveThinking        bool
}

type messagesCompat struct {
	eagerToolInputStreaming  bool
	longCacheRetention       bool
	sessionAffinityHeaders   bool
	cacheControlOnTools      bool
	adaptiveThinking         bool
	emptyThinkingSignature   bool
	supportsTemperature      bool
	supportsDisabledThinking bool
	supportsToolReferences   bool
	thinkingFormat           sigma.AnthropicThinkingFormat
	// claudeCodeIdentity is request-scoped: it is set when the resolved
	// credential is an Anthropic OAuth token, which Anthropic only accepts
	// from requests that identify as Claude Code.
	claudeCodeIdentity bool
}

func anthropicMessagesCompat(model sigma.Model, baseURL string, override *MessagesCompat) messagesCompat {
	compat := detectedMessagesCompat(model.Provider, baseURL)
	compat = applyModelMessagesCompat(compat, model.AnthropicMessagesCompat)
	if override == nil {
		return compat
	}
	compat.eagerToolInputStreaming = override.EagerToolInputStreaming
	compat.longCacheRetention = override.LongCacheRetention
	compat.sessionAffinityHeaders = override.SessionAffinityHeaders
	compat.cacheControlOnTools = override.CacheControlOnTools
	compat.adaptiveThinking = override.AdaptiveThinking
	return compat
}

func detectedMessagesCompat(provider sigma.ProviderID, baseURL string) messagesCompat {
	host := baseURLHost(baseURL)
	providerText := strings.ToLower(string(provider))

	switch {
	case provider == sigma.ProviderAnthropic || host == "api.anthropic.com":
		return messagesCompat{
			eagerToolInputStreaming:  true,
			longCacheRetention:       true,
			cacheControlOnTools:      true,
			supportsTemperature:      true,
			supportsDisabledThinking: true,
			thinkingFormat:           sigma.AnthropicThinkingBudget,
		}
	case provider == sigma.ProviderFireworks ||
		provider == sigma.ProviderFireworksAnthropic ||
		strings.Contains(host, "fireworks.ai"):
		return messagesCompat{
			sessionAffinityHeaders:   true,
			adaptiveThinking:         true,
			supportsTemperature:      true,
			supportsDisabledThinking: true,
			thinkingFormat:           sigma.AnthropicThinkingBudget,
		}
	case provider == sigma.ProviderKimi ||
		provider == sigma.ProviderKimiCoding ||
		strings.Contains(providerText, "kimi") ||
		strings.Contains(host, "moonshot") ||
		strings.Contains(host, "kimi"):
		return messagesCompat{
			sessionAffinityHeaders:   true,
			adaptiveThinking:         true,
			supportsTemperature:      true,
			supportsDisabledThinking: true,
			thinkingFormat:           sigma.AnthropicThinkingBudget,
		}
	case provider == sigma.ProviderXiaomi || strings.Contains(host, "xiaomi"):
		return messagesCompat{
			sessionAffinityHeaders:   true,
			adaptiveThinking:         true,
			supportsTemperature:      true,
			supportsDisabledThinking: true,
			thinkingFormat:           sigma.AnthropicThinkingBudget,
		}
	default:
		return messagesCompat{
			supportsTemperature:      true,
			supportsDisabledThinking: true,
			thinkingFormat:           sigma.AnthropicThinkingBudget,
		}
	}
}

func applyModelMessagesCompat(compat messagesCompat, override *sigma.AnthropicMessagesCompat) messagesCompat {
	if override == nil {
		return compat
	}
	if value, ok := anthropicCompatBool(override.SupportsEagerToolInputStreaming); ok {
		compat.eagerToolInputStreaming = value
	}
	if value, ok := anthropicCompatBool(override.SupportsLongCacheRetention); ok {
		compat.longCacheRetention = value
	}
	if value, ok := anthropicCompatBool(override.SupportsSessionAffinity); ok {
		compat.sessionAffinityHeaders = value
	}
	if value, ok := anthropicCompatBool(override.SupportsCacheControlOnTools); ok {
		compat.cacheControlOnTools = value
	}
	if value, ok := anthropicCompatBool(override.SupportsEmptyThinkingSignature); ok {
		compat.emptyThinkingSignature = value
	}
	if value, ok := anthropicCompatBool(override.SupportsTemperature); ok {
		compat.supportsTemperature = value
	}
	if value, ok := anthropicCompatBool(override.SupportsDisabledThinking); ok {
		compat.supportsDisabledThinking = value
	}
	if value, ok := anthropicCompatBool(override.SupportsToolReferences); ok {
		compat.supportsToolReferences = value
	}
	if override.ThinkingFormat != "" {
		compat.thinkingFormat = override.ThinkingFormat
		compat.adaptiveThinking = override.ThinkingFormat == sigma.AnthropicThinkingAdaptive
	}
	return compat
}

func anthropicCompatBool(value sigma.AnthropicCompatSupport) (bool, bool) {
	switch value {
	case sigma.AnthropicCompatSupported:
		return true, true
	case sigma.AnthropicCompatUnsupported:
		return false, true
	default:
		return false, false
	}
}

func baseURLHost(baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
