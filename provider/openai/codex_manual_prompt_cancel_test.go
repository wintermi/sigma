// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"testing"
	"time"
)

// When the browser callback completes login, the manual prompt's context is
// cancelled so a caller reading pasted input stops instead of swallowing the
// next line typed.
func TestCodexBrowserCallbackCancelsManualPrompt(t *testing.T) {
	t.Parallel()
	server := &codexBrowserCallbackServer{done: make(chan codexBrowserCallbackResult, 1)}
	server.done <- codexBrowserCallbackResult{code: "browser-code"}
	cancelled := make(chan struct{})
	manual := func(ctx context.Context, _ CodexBrowserManualPrompt) (string, error) {
		<-ctx.Done()
		close(cancelled)
		return "", ctx.Err()
	}
	code, err := waitOpenAICodexBrowserAuthorizationCode(context.Background(), "state", server, manual)
	if err != nil || code != "browser-code" {
		t.Fatalf("code = %q, err = %v; want browser-code", code, err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("manual prompt context was not cancelled")
	}
}
