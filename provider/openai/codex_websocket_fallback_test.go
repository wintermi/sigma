// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

// Only a WebSocket transport failure justifies resending a request over SSE;
// an API error would fail the same way and must not make SSE sticky.
func TestCodexWebSocketFallsBackToSSEOnlyForTransportFailures(t *testing.T) {
	openai.CloseCodexResponsesWebSocketSessions()
	t.Cleanup(openai.CloseCodexResponsesWebSocketSessions)

	tests := []struct {
		name         string
		websocket    func(*testing.T, *codexWebSocketTestConn)
		wantFallback bool
	}{
		{
			name: "api error frame",
			websocket: func(t *testing.T, ws *codexWebSocketTestConn) {
				ws.writeJSON(t, map[string]any{"type": "error", "status": 400, "error": map[string]any{"code": "context_length_exceeded", "message": "too long"}})
			},
		},
		{
			name: "response failed",
			websocket: func(t *testing.T, ws *codexWebSocketTestConn) {
				ws.writeJSON(t, map[string]any{"type": "response.failed", "response": map[string]any{"status": "failed", "error": map[string]any{"code": "invalid_prompt", "message": "rejected"}}})
			},
		},
		{
			name:         "connection dropped before output",
			websocket:    func(*testing.T, *codexWebSocketTestConn) {},
			wantFallback: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
					posts.Add(1)
					writeResponsesSSE(t, w, responsesCompletedEvent)
					return
				}
				ws := acceptCodexWebSocket(t, w, r)
				defer ws.conn.Close()
				_ = ws.readJSON(t)
				tt.websocket(t, ws)
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("codex-ws-fallback-test")
			model := codexResponsesTestModel(providerID)
			client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))
			sessionID := "codex-ws-fallback-" + strings.ReplaceAll(tt.name, " ", "-")
			_, err := client.Complete(context.Background(), model,
				sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
				sigma.WithTransport(sigma.TransportWebSocket),
				sigma.WithSessionID(sessionID),
			)

			stats, _ := openai.CodexResponsesWebSocketStats(sessionID)
			if tt.wantFallback {
				if err != nil || posts.Load() != 1 || !stats.WebSocketFallbackActive {
					t.Fatalf("err=%v posts=%d fallback=%v, want SSE fallback", err, posts.Load(), stats.WebSocketFallbackActive)
				}
				return
			}
			var providerErr *sigma.ProviderError
			if !errors.As(err, &providerErr) {
				t.Fatalf("error = %v, want the WebSocket provider error", err)
			}
			if posts.Load() != 0 || stats.WebSocketFallbackActive {
				t.Fatalf("posts=%d fallback=%v, want no SSE resend", posts.Load(), stats.WebSocketFallbackActive)
			}
		})
	}
}
