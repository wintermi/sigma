// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"fmt"
	"strings"
	"testing"
)

// Options and auth resolutions are commonly logged; no fmt verb may print the
// API key, authorization headers, or provider environment values.
func TestOptionsAndAuthResolutionRedactSecrets(t *testing.T) {
	t.Parallel()
	const secret = "sk-test-secret-value-1234567890"
	opts := Options{APIKey: secret, Headers: map[string]string{"Authorization": "Bearer " + secret}, SessionID: "session-1"}
	resolution := AuthResolution{
		Credential:  Credential{Type: CredentialTypeAPIKey, Value: secret},
		ProviderEnv: map[string]string{"OPENAI_API_KEY": secret},
		Headers:     map[string]string{"x-api-key": secret},
		BaseURL:     "https://api.example.test",
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, value := range []any{opts, &opts, resolution, &resolution} {
			out := fmt.Sprintf(verb, value)
			if strings.Contains(out, secret) {
				t.Fatalf("%s of %T printed the secret: %s", verb, value, out)
			}
		}
	}
	if out := fmt.Sprintf("%+v", opts); !strings.Contains(out, "session-1") {
		t.Fatalf("%%+v dropped other fields: %s", out)
	}
	if out := fmt.Sprintf("%+v", resolution); !strings.Contains(out, "https://api.example.test") {
		t.Fatalf("%%+v dropped other fields: %s", out)
	}
}
