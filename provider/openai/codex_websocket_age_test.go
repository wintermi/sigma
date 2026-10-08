// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"bufio"
	"context"
	"crypto/sha256"
	"net"
	"testing"
	"time"
)

// The Codex backend closes WebSocket connections after 60 minutes, so a cached
// connection must be retired before then rather than reused.
func TestCodexWebSocketRetiresConnectionsNearBackendLimit(t *testing.T) {
	CloseCodexResponsesWebSocketSessions()
	t.Cleanup(CloseCodexResponsesWebSocketSessions)

	for _, tt := range []struct {
		name   string
		age    time.Duration
		reused bool
	}{
		{name: "fresh", age: time.Minute, reused: true},
		{name: "near limit", age: 56 * time.Minute, reused: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, server := net.Pipe()
			t.Cleanup(func() { _ = server.Close() })
			conn := &codexWebSocketConnection{conn: client, reader: bufio.NewReader(client)}
			sessionID := "age-" + tt.name
			var fingerprint [sha256.Size]byte
			entry := &codexWebSocketSessionEntry{conn: conn, fingerprint: fingerprint, createdAt: time.Now().Add(-tt.age)}
			codexWebSocketSessions.Lock()
			codexWebSocketSessions.entries[sessionID] = map[string]*codexWebSocketSessionEntry{"account": entry}
			codexWebSocketSessions.Unlock()

			// An unreachable URL makes any fresh dial fail, so success means reuse.
			acquired, err := acquireCodexWebSocket(context.Background(), "ws://127.0.0.1:1/responses", nil, sessionID, "account", time.Second, fingerprint)
			reused := err == nil && acquired.reused
			if err == nil {
				acquired.release(false)
			}
			if reused != tt.reused {
				t.Fatalf("reused = %v (err %v), want %v", reused, err, tt.reused)
			}
			if !tt.reused && conn.IsOpen() {
				t.Fatal("retired connection was not closed")
			}
		})
	}
}
