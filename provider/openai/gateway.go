// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/wintermi/sigma"
)

var cloudflareBaseURLVariable = regexp.MustCompile(`\{([A-Z_][A-Z0-9_]*)\}`)

func addCopilotDynamicHeaders(req *http.Request, model sigma.Model, request sigma.Request) {
	if model.Provider != sigma.ProviderGitHubCopilot {
		return
	}
	req.Header.Set("X-Initiator", copilotInitiator(request.Messages))
	req.Header.Set("Openai-Intent", "conversation-edits")
	if hasCopilotVisionInput(request.Messages) {
		req.Header.Set("Copilot-Vision-Request", "true")
	}
}

func copilotInitiator(messages []sigma.Message) string {
	if len(messages) == 0 || messages[len(messages)-1].Role == sigma.RoleUser {
		return "user"
	}
	return "agent"
}

func hasCopilotVisionInput(messages []sigma.Message) bool {
	for _, message := range messages {
		if message.Role != sigma.RoleUser && message.Role != sigma.RoleTool {
			continue
		}
		if hasImageContent(message.Content) {
			return true
		}
	}
	return false
}

func resolveCloudflareBaseURL(provider sigma.ProviderID, baseURL string) (string, error) {
	if !isCloudflareRoute(provider, baseURL) {
		return baseURL, nil
	}
	var missing string
	resolved := cloudflareBaseURLVariable.ReplaceAllStringFunc(baseURL, func(match string) string {
		if missing != "" {
			return match
		}
		name := strings.Trim(match, "{}")
		value := os.Getenv(name)
		if value == "" {
			missing = name
			return match
		}
		return value
	})
	if missing != "" {
		return "", fmt.Errorf("openai: %s is required for Cloudflare base URL", missing)
	}
	return resolved, nil
}

func isCloudflareRoute(provider sigma.ProviderID, baseURL string) bool {
	providerText := strings.ToLower(string(provider))
	if strings.Contains(providerText, "cloudflare") {
		return true
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return strings.Contains(strings.ToLower(baseURL), "cloudflare")
	}
	host := strings.ToLower(parsed.Host)
	return strings.Contains(host, "cloudflare.com")
}

func addCloudflareAuthHeader(req *http.Request, model sigma.Model, credential sigma.Credential) bool {
	if !isCloudflareRoute(model.Provider, req.URL.String()) {
		return false
	}
	if credential.Value != "" && req.Header.Get("cf-aig-authorization") == "" {
		req.Header.Set("cf-aig-authorization", "Bearer "+credential.Value)
	}
	return true
}
