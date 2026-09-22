// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCodexWebSocketFailureDiagnosticsRedact(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"json", `{"access_token":"synthetic-secret"}`},
		{"truncated_json", `{"refresh_token":"synthetic-secret`},
		{"bearer", `upstream rejected Bearer synthetic-secret`},
		{"signed_url", `https://example.invalid/?X-Amz-Signature=synthetic-secret&status=failed`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Exercise both response-body error constructors before the stats boundary.
			for _, proxy := range []bool{false, true} {
				var err error
				if proxy {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodConnect {
							t.Errorf("method = %s, want CONNECT", r.Method)
						}
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = io.WriteString(w, tc.body)
					}))
					endpoint, parseErr := url.Parse(server.URL)
					if parseErr != nil {
						t.Fatal(parseErr)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					_, err = dialCodexWebSocketProxy(ctx, endpoint, "target.invalid:80")
					cancel()
					server.Close()
				} else {
					client, server := net.Pipe()
					_ = client.SetDeadline(time.Now().Add(2 * time.Second))
					done := make(chan error, 1)
					go func() {
						defer server.Close()
						if _, readErr := http.ReadRequest(bufio.NewReader(server)); readErr != nil {
							done <- readErr
							return
						}
						_, writeErr := fmt.Fprintf(server, "HTTP/1.1 401 Unauthorized\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(tc.body), tc.body)
						done <- writeErr
					}()
					conn := &codexWebSocketConnection{conn: client, reader: bufio.NewReader(client)}
					err = conn.handshake(context.Background(), &url.URL{Scheme: "ws", Host: "test.invalid", Path: "/responses"}, nil)
					_ = client.Close()
					if serverErr := <-done; serverErr != nil {
						t.Fatal(serverErr)
					}
				}
				if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "synthetic-secret") {
					t.Fatalf("proxy=%v: unsafe or missing status error: %v", proxy, err)
				}
				session := t.Name()
				recordCodexWebSocketFailure(session, err)
				stats, ok := CodexResponsesWebSocketStats(session)
				CloseCodexResponsesWebSocketSession(session)
				if !ok || strings.Contains(stats.LastWebSocketError, "synthetic-secret") || !strings.Contains(stats.LastWebSocketError, "401") {
					t.Fatalf("unsafe or missing diagnostic: %+v", stats)
				}
			}
			// A caller-supplied transport error must also be sanitized at storage.
			session := t.Name()
			recordCodexWebSocketFailure(session, errors.New("status 401: "+tc.body))
			stats, _ := CodexResponsesWebSocketStats(session)
			CloseCodexResponsesWebSocketSession(session)
			if strings.Contains(stats.LastWebSocketError, "synthetic-secret") || !strings.Contains(stats.LastWebSocketError, "[redacted]") {
				t.Fatalf("storage bypassed redaction: %s", stats.LastWebSocketError)
			}
		})
	}
}

func TestCodexWebSocketFailureDiagnosticPreviewBound(t *testing.T) {
	session := t.Name()
	defer CloseCodexResponsesWebSocketSession(session)
	recordCodexWebSocketFailure(session, errors.New("status 401: Bearer synthetic-secret "+strings.Repeat("界", 1000)+"\xff"))
	stats, ok := CodexResponsesWebSocketStats(session)
	if !ok || len(stats.LastWebSocketError) > 2051 || !utf8.ValidString(stats.LastWebSocketError) || !strings.HasSuffix(stats.LastWebSocketError, "...") || strings.Contains(stats.LastWebSocketError, "synthetic-secret") {
		t.Fatalf("unsafe preview: %q", stats.LastWebSocketError)
	}
}

func TestCodexWebSocketPreservesExactRequestNumbers(t *testing.T) {
	t.Parallel()
	const payload = `{"tools":[{"type":"function","name":"tool","parameters":{"type":"object","properties":{"id":{"type":"integer","const":9007199254740993,"enum":[9007199254740993],"minimum":9007199254740993,"maximum":9007199254740995}}}}],"input":[]}`
	body, err := decodeCodexWebSocketBody([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	copy := cloneJSONMap(body)
	for name, value := range map[string]map[string]any{"initial": body, "cached_clone": copy} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(encoded), "9007199254740993") != 3 || !strings.Contains(string(encoded), "9007199254740995") {
			t.Fatalf("%s rounded schema: %s", name, encoded)
		}
	}
	entry := &codexWebSocketSessionEntry{continuation: &codexWebSocketContinuation{lastRequestBody: copy, lastResponseID: "previous"}}
	cached := cachedCodexWebSocketRequestBody(entry, body)
	if cached["previous_response_id"] != "previous" {
		t.Fatal("unchanged exact schema failed to reuse continuation")
	}
	cached["tools"].([]any)[0].(map[string]any)["name"] = "changed"
	if body["tools"].([]any)[0].(map[string]any)["name"] != "tool" || copy["tools"].([]any)[0].(map[string]any)["name"] != "tool" {
		t.Fatal("cached body aliases request or history")
	}
	changed, err := decodeCodexWebSocketBody([]byte(strings.ReplaceAll(payload, "9007199254740993", "9007199254740992")))
	if err != nil {
		t.Fatal(err)
	}
	full := cachedCodexWebSocketRequestBody(entry, changed)
	if _, ok := full["previous_response_id"]; ok || entry.continuation != nil {
		t.Fatal("different exact schema incorrectly reused continuation")
	}
	if _, err := decodeCodexWebSocketBody([]byte(payload + `{}`)); err == nil {
		t.Fatal("accepted trailing JSON")
	}
}
