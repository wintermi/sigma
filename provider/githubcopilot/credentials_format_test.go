// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package githubcopilot

import (
	"fmt"
	"strings"
	"testing"
)

func TestCredentialFormattingRedactsSecrets(t *testing.T) {
	t.Parallel()

	for _, value := range []any{
		GitHubCopilotOAuthCredentials{AccessToken: "secret-access", RefreshToken: "secret-refresh"},
	} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			if got := fmt.Sprintf(verb, value); strings.Contains(got, "secret-") {
				t.Fatalf("%T formatted with %s leaked a secret: %s", value, verb, got)
			}
		}
	}
}
