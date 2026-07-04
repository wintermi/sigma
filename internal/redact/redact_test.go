// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package redact

import (
	"strings"
	"testing"
)

func TestStringRedactsCredentialMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		secrets []string
	}{
		{
			name: "authorization header",
			input: strings.Join([]string{
				"HTTP/1.1 401 Unauthorized",
				"Authorization: Bearer sk-proj-authorization123",
			}, "\n"),
			secrets: []string{"sk-proj-authorization123"},
		},
		{
			name:    "api key header line",
			input:   "X-Api-Key: sk-proj-headerapikey123",
			secrets: []string{"sk-proj-headerapikey123"},
		},
		{
			name:    "cookies",
			input:   "Set-Cookie: session=cookie-secret; Path=/",
			secrets: []string{"cookie-secret"},
		},
		{
			name:    "signed url",
			input:   "https://storage.example.test/object?X-Amz-Signature=signed-secret&X-Amz-Credential=credential-secret",
			secrets: []string{"signed-secret", "credential-secret"},
		},
		{
			name:    "query parameters",
			input:   "https://api.example.test/callback?api_key=query-api-key&access_token=query-access-token&refresh_token=query-refresh-token",
			secrets: []string{"query-api-key", "query-access-token", "query-refresh-token"},
		},
		{
			name:    "oauth device form",
			input:   "access_token=form-access-token&refresh_token=form-refresh-token&device_code=form-device-code&user_code=form-user-code",
			secrets: []string{"form-access-token", "form-refresh-token", "form-device-code", "form-user-code"},
		},
		{
			name: "json credentials",
			input: `{
				"api_key": "json-api-key",
				"access_token": "json-access-token",
				"refresh_token": "json-refresh-token",
				"client_secret": "json-client-secret",
				"device_code": "json-device-code",
				"secret_access_key": "json-secret-access-key",
				"nested": {"session_token": "json-session-token"}
			}`,
			secrets: []string{
				"json-api-key",
				"json-access-token",
				"json-refresh-token",
				"json-client-secret",
				"json-device-code",
				"json-secret-access-key",
				"json-session-token",
			},
		},
		{
			name:    "provider error body",
			input:   `{"error":{"message":"invalid credential","api_key":"json-provider-key","access_token":"json-provider-token"}}`,
			secrets: []string{"json-provider-key", "json-provider-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			redacted := String(tt.input)
			assertNoRawSecrets(t, redacted, tt.secrets)
			if !strings.Contains(redacted, replacement) {
				t.Fatalf("String(%q) = %q, want redaction marker", tt.input, redacted)
			}
		})
	}
}

func TestHeadersRedactsSensitiveHeaderValues(t *testing.T) {
	t.Parallel()

	headers := Headers(map[string][]string{
		"Authorization":        {"Bearer sk-proj-authheader123"},
		"Proxy-Authorization":  {"Basic proxy-secret"},
		"X-Api-Key":            {"plain-api-key-secret"},
		"Api-Key":              {"alternate-api-key-secret"},
		"X-Goog-Api-Key":       {"google-debug-secret-without-AIza-pattern"},
		"Cf-Aig-Authorization": {"Bearer cloudflare-gateway-secret"},
		"Cookie":               {"session=cookie-secret"},
		"Set-Cookie":           {"session=response-cookie-secret"},
		"X-Callback":           {"https://example.test/cb?signature=signed-query-secret"},
	})

	for _, name := range []string{"Authorization", "Proxy-Authorization", "X-Api-Key", "Api-Key", "X-Goog-Api-Key", "Cf-Aig-Authorization", "Cookie", "Set-Cookie"} {
		if got := headers[name][0]; got != replacement {
			t.Fatalf("%s = %q, want redacted", name, got)
		}
	}
	assertNoRawSecrets(t, headers["X-Callback"][0], []string{"signed-query-secret"})
}

func TestPreviewRedactsBeforeTruncating(t *testing.T) {
	t.Parallel()

	preview := Preview(`{"message":"safe context","access_token":"preview-access-token"}`, 32)
	assertNoRawSecrets(t, preview, []string{"preview-access-token"})
	if !strings.Contains(preview, replacement) {
		t.Fatalf("Preview did not retain redaction marker: %q", preview)
	}
}

func assertNoRawSecrets(t *testing.T, value string, secrets []string) {
	t.Helper()

	for _, secret := range secrets {
		if strings.Contains(value, secret) {
			t.Fatalf("value leaked %q: %q", secret, value)
		}
	}
}
