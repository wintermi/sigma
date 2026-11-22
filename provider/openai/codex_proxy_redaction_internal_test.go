// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"net/url"
	"strings"
	"testing"
)

// A malformed proxy URL can still carry a password, which the error must not
// echo. Not parallel: the proxy comes from the environment.
func TestCodexInvalidProxyURLErrorOmitsPassword(t *testing.T) {
	for _, key := range []string{"NO_PROXY", "no_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(key, "")
	}
	t.Setenv("HTTPS_PROXY", "http://user:s3cr3t-pass@[bad-host")
	target, _ := url.Parse("wss://chatgpt.com/backend-api/codex/responses")
	_, err := codexWebSocketProxyURL(target)
	if err == nil {
		t.Fatal("codexWebSocketProxyURL accepted a malformed proxy URL")
	}
	if strings.Contains(err.Error(), "s3cr3t-pass") {
		t.Fatalf("error leaked the proxy password: %v", err)
	}
}
