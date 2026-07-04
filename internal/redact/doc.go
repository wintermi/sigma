// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package redact contains helpers for removing secrets from diagnostics.
package redact

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

const replacement = "[redacted]"

var (
	authorizationLinePattern = regexp.MustCompile(`(?im)^([ \t]*authorization[ \t]*:[ \t]*)(?:bearer|basic|api-key|apikey)?[ \t]*[^\r\n]+`)
	bearerPattern            = regexp.MustCompile(`(?i)\bbearer[ \t]+[A-Za-z0-9._~+/=-]+`)
	cookieLinePattern        = regexp.MustCompile(`(?im)^([ \t]*(?:cookie|set-cookie)[ \t]*:[ \t]*)[^\r\n]+`)
	jsonSecretPattern        = regexp.MustCompile(`(?i)("(?:(?:api[_-]?key)|(?:access[_-]?token)|(?:refresh[_-]?token)|(?:id[_-]?token)|(?:client[_-]?secret)|(?:device[_-]?code)|(?:user[_-]?code)|(?:secret[_-]?access[_-]?key)|(?:session[_-]?token)|(?:provider[_-]?signature)|authorization|cookie|signature)"[ \t]*:[ \t]*)("[^"\\]*(?:\\.[^"\\]*)*"|null|true|false|-?[0-9]+(?:\.[0-9]+)?)`)
	querySecretPattern       = regexp.MustCompile(`(?i)([?&](?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|device[_-]?code|user[_-]?code|signature|sig|x-amz-signature|x-amz-credential|x-amz-security-token|x-goog-signature|x-goog-credential|x-goog-security-token|awsaccesskeyid)=)[^&#\s]+`)
	formSecretPattern        = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|device[_-]?code|user[_-]?code)=([^&\s]+)`)
	apiKeyPattern            = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{8,}|sk-proj-[A-Za-z0-9_-]{8,}|AIza[0-9A-Za-z_-]{16,})\b`)
)

// Secret returns a stable placeholder for non-empty secret material.
func Secret(value string) string {
	if value == "" {
		return ""
	}
	return replacement
}

// Source returns source information with any inline value removed.
func Source(source string) string {
	if source == "" {
		return ""
	}
	if before, _, ok := strings.Cut(source, "="); ok {
		return strings.TrimSpace(before) + "=" + replacement
	}
	return source
}

// String removes known credential shapes from a diagnostic string.
func String(value string) string {
	if value == "" {
		return ""
	}
	if redacted, ok := redactJSON(value); ok {
		value = redacted
	}
	value = authorizationLinePattern.ReplaceAllString(value, "${1}"+replacement)
	value = cookieLinePattern.ReplaceAllString(value, "${1}"+replacement)
	value = bearerPattern.ReplaceAllString(value, "Bearer "+replacement)
	value = jsonSecretPattern.ReplaceAllString(value, "${1}\""+replacement+"\"")
	value = querySecretPattern.ReplaceAllString(value, "${1}"+replacement)
	value = formSecretPattern.ReplaceAllString(value, "$1="+replacement)
	value = apiKeyPattern.ReplaceAllString(value, replacement)
	return value
}

// Preview redacts value, normalizes invalid UTF-8, and caps it to limit bytes.
func Preview(value string, limit int) string {
	if value == "" {
		return ""
	}
	value = strings.ToValidUTF8(String(value), "\uFFFD")
	if limit <= 0 || len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit] + "..."
}

// Header redacts sensitive HTTP header values.
func Header(name string, value string) string {
	if isSensitiveHeader(name) {
		return replacement
	}
	return String(value)
}

// Headers returns a cloned header map with sensitive values removed.
func Headers(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	redacted := make(map[string][]string, len(headers))
	for name, values := range headers {
		redactedValues := make([]string, len(values))
		for i, value := range values {
			redactedValues[i] = Header(name, value)
		}
		redacted[name] = redactedValues
	}
	return redacted
}

func redactJSON(value string) (string, bool) {
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return "", false
	}
	redactJSONValue(decoded)
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func redactJSONValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if isSensitiveField(key) {
				typed[key] = replacement
				continue
			}
			redactJSONValue(nested)
		}
	case []any:
		for _, nested := range typed {
			redactJSONValue(nested)
		}
	}
}

func isSensitiveHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key", "x-goog-api-key", "cf-aig-authorization":
		return true
	default:
		return false
	}
}

func isSensitiveField(name string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "_").Replace(strings.TrimSpace(name)))
	switch normalized {
	case "api_key", "access_token", "refresh_token", "id_token", "client_secret", "device_code", "user_code", "secret_access_key", "session_token", "provider_signature", "authorization", "cookie", "signature":
		return true
	default:
		return false
	}
}
