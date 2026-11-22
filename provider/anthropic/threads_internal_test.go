// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"testing"

	"github.com/wintermi/sigma"
)

// A continue builds on the messages and settings the server already holds,
// so any earlier change starts a new thread instead. Cache markers move
// between requests and do not count as a change.
func TestPrepareThreadRequestContinuesOnlyAnUnchangedPrefix(t *testing.T) {
	t.Parallel()
	user := func(text string, cached bool) any {
		block := map[string]any{"type": "text", "text": text}
		if cached {
			block["cache_control"] = map[string]any{"type": "ephemeral"}
		}
		return map[string]any{"role": "user", "content": []any{block}}
	}
	reply := map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "ok"}}}
	request := func(system string, messages ...any) map[string]any {
		return map[string]any{"model": "claude-test", "system": system, "messages": messages}
	}
	first := request("Be brief.", user("first", true))
	for _, tt := range []struct {
		name         string
		next         map[string]any
		wantContinue bool
	}{
		{name: "cache marker moved", next: request("Be brief.", user("first", false), reply, user("second", true)), wantContinue: true},
		{name: "earlier message edited", next: request("Be brief.", user("edited", false), reply, user("second", true))},
		{name: "system prompt changed", next: request("Be thorough.", user("first", false), reply, user("second", true))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			session := "prefix-" + tt.name
			t.Cleanup(func() { _ = sigma.CleanupSessionResources(session) })
			firstSettings := threadSettings("https://api.anthropic.com/v1/messages", "", first)
			thread := prepareThreadRequest(session, firstSettings, first)
			recordThreadReply(thread, firstSettings, "msg_1", []any{user("first", true), reply})

			settings := threadSettings("https://api.anthropic.com/v1/messages", "", tt.next)
			next := prepareThreadRequest(session, settings, tt.next)
			gotContinue := next.body["thread"].(map[string]any)["type"] == "continue"
			if gotContinue != tt.wantContinue {
				t.Fatalf("thread = %#v, want continue %v", next.body["thread"], tt.wantContinue)
			}
		})
	}
}
