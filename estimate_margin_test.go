// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"strings"
	"testing"
)

// Routers such as OpenRouter count input up to 12% higher than the estimate
// and reject input plus max_tokens above the window, so the cap leaves a
// proportional margin while it still allows a useful amount of output.
func TestMaxTokensForContextLeavesRoomForInputOvercount(t *testing.T) {
	t.Parallel()
	request := func(chars int) Request {
		return Request{Messages: []Message{UserText(strings.Repeat("a", chars))}}
	}

	large := Model{ContextWindow: 1_000_000, MaxOutputTokens: 1_000_000}
	req := request(1_750_000)
	input := EstimateRequestTokens(req).Tokens
	got := MaxTokensForContext(large, req, 0)
	if overcounted := input + input*12/100; overcounted+got > large.ContextWindow {
		t.Fatalf("cap %d with input %d does not fit a 12%% overcount in %d", got, input, large.ContextWindow)
	}

	// Near a full window the percentage margin would leave too little output,
	// so only the fixed margin is kept.
	small := Model{ContextWindow: 200_000, MaxOutputTokens: 64_000}
	req = request(630_000)
	input = EstimateRequestTokens(req).Tokens
	if got, want := MaxTokensForContext(small, req, 0), small.ContextWindow-input-4096; got != want {
		t.Fatalf("near-full cap = %d, want %d", got, want)
	}
}
