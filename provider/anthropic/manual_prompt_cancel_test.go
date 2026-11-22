// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"context"
	"testing"
	"time"
)

// When the browser callback completes login, the manual prompt's context is
// cancelled so a caller reading pasted input stops instead of swallowing the
// next line typed.
func TestAnthropicBrowserCallbackCancelsManualPrompt(t *testing.T) {
	t.Parallel()
	server := &anthropicBrowserCallbackServer{done: make(chan anthropicBrowserCallbackResult, 1)}
	server.done <- anthropicBrowserCallbackResult{code: "browser-code"}
	cancelled := make(chan struct{})
	manual := func(ctx context.Context, _ AnthropicBrowserManualPrompt) (string, error) {
		<-ctx.Done()
		close(cancelled)
		return "", ctx.Err()
	}
	code, err := waitAnthropicBrowserAuthorizationCode(context.Background(), "state", server, manual)
	if err != nil || code != "browser-code" {
		t.Fatalf("code = %q, err = %v; want browser-code", code, err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("manual prompt context was not cancelled")
	}
}
