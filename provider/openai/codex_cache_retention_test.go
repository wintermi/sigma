// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

// CacheRetentionNone opts out of Codex session affinity: no session headers,
// prompt cache key, or reused WebSocket continuation for the session.
func TestCodexCacheRetentionNoneDropsSessionAffinity(t *testing.T) {
	t.Run("sse", func(t *testing.T) {
		requests := make(chan capturedRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captureRequest(t, requests, r)
			writeResponsesSSE(t, w, responsesCompletedEvent)
		}))
		t.Cleanup(server.Close)
		providerID := sigma.ProviderID("codex-cache-none-sse")
		model := codexResponsesTestModel(providerID)
		client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))
		if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			sigma.WithSessionID("session-none"), sigma.WithCacheRetention(sigma.CacheRetentionNone)); err != nil {
			t.Fatal(err)
		}
		request := receiveRequest(t, requests)
		for _, header := range []string{"session-id", "x-client-request-id"} {
			if got := request.Headers.Get(header); got != "" {
				t.Fatalf("%s = %q, want omitted", header, got)
			}
		}
		if key, ok := decodeResponsesPayload(t, request.Body)["prompt_cache_key"]; ok {
			t.Fatalf("prompt_cache_key = %v, want omitted", key)
		}
	})

	t.Run("websocket", func(t *testing.T) {
		openai.CloseCodexResponsesWebSocketSessions()
		t.Cleanup(openai.CloseCodexResponsesWebSocketSessions)
		server := newCodexWebSocketTestServer(t, func(_ *http.Request, ws *codexWebSocketTestConn) {
			if _, err := ws.readJSONError(); err != nil {
				return
			}
			writeCodexWebSocketTextResponse(t, ws, "resp_none", "msg_none", "txt_none", "ok")
		})
		defer server.Close()
		providerID := sigma.ProviderID("codex-cache-none-ws")
		model := codexResponsesTestModel(providerID)
		client := codexResponsesTestClient(t, providerID, model, server.URL, codexTokenProvider("codex-oauth-token"))
		if _, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
			sigma.WithTransport(sigma.TransportWebSocket), sigma.WithSessionID("session-none-ws"), sigma.WithCacheRetention(sigma.CacheRetentionNone)); err != nil {
			t.Fatal(err)
		}
		if stats, ok := openai.CodexResponsesWebSocketStats("session-none-ws"); ok {
			t.Fatalf("session state recorded despite CacheRetentionNone: %+v", stats)
		}
	})
}
