// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"strings"

	"github.com/wintermi/sigma"
)

// Anthropic OAuth access tokens are only accepted when requests identify as
// Claude Code: the request must carry the Claude Code beta headers, start the
// system prompt with the Claude Code identity block, and use Claude Code's
// canonical tool-name casing.
const (
	claudeCodeIdentityPrompt = "You are Claude Code, Anthropic's official CLI for Claude."
	claudeCodeBetaHeader     = "claude-code-20250219"
	claudeCodeOAuthBeta      = "oauth-2025-04-20"
	claudeCodeVersion        = "2.1.75"
	claudeCodeUserAgent      = "claude-cli/" + claudeCodeVersion
	anthropicOAuthTokenMark  = "sk-ant-oat"
)

// claudeCodeToolNames lists Claude Code's canonical tool-name casing so caller
// tools that differ only by case can be replayed with the expected names.
var claudeCodeToolNames = []string{
	"Read",
	"Write",
	"Edit",
	"Bash",
	"Grep",
	"Glob",
	"AskUserQuestion",
	"EnterPlanMode",
	"ExitPlanMode",
	"KillShell",
	"NotebookEdit",
	"Skill",
	"Task",
	"TaskOutput",
	"TodoWrite",
	"WebFetch",
	"WebSearch",
}

var claudeCodeToolLookup = func() map[string]string {
	lookup := make(map[string]string, len(claudeCodeToolNames))
	for _, name := range claudeCodeToolNames {
		lookup[strings.ToLower(name)] = name
	}
	return lookup
}()

// isAnthropicOAuthCredential reports whether a resolved credential should use
// Claude Code identity mode: OAuth-typed credentials and Anthropic OAuth
// access tokens passed as plain API keys both qualify.
func isAnthropicOAuthCredential(credential sigma.Credential) bool {
	if credential.Value == "" {
		return false
	}
	if credential.Type == sigma.CredentialTypeOAuthToken {
		return true
	}
	return strings.Contains(credential.Value, anthropicOAuthTokenMark)
}

// toClaudeCodeToolName maps a caller tool name onto Claude Code's canonical
// casing when it matches case-insensitively, and returns it unchanged
// otherwise.
func toClaudeCodeToolName(name string) string {
	if canonical, ok := claudeCodeToolLookup[strings.ToLower(name)]; ok {
		return canonical
	}
	return name
}

// restoreCallerToolName maps a streamed tool name back to the caller's
// original tool name when one matches case-insensitively.
func restoreCallerToolName(name string, tools []sigma.Tool) string {
	if name == "" || len(tools) == 0 {
		return name
	}
	lower := strings.ToLower(name)
	for _, tool := range tools {
		if strings.ToLower(tool.Name) == lower {
			return tool.Name
		}
	}
	return name
}
