// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6455 requires SHA-1 for Sec-WebSocket-Accept.
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
)

const (
	codexWebSocketIdleTTL               = 5 * time.Minute
	codexWebSocketDefaultConnectTimeout = 15 * time.Second
	codexWebSocketMaxFrameBytes         = 16 << 20
	codexWebSocketMaxMessageBytes       = 16 << 20
	webSocketGUID                       = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	webSocketOpcodeContinuation = 0x0
	webSocketOpcodeText         = 0x1
	webSocketOpcodeClose        = 0x8
	webSocketOpcodePing         = 0x9
	webSocketOpcodePong         = 0xA
)

type codexWebSocketConnection struct {
	conn    net.Conn
	reader  *bufio.Reader
	stateMu sync.Mutex
	writeMu sync.Mutex
	closed  bool
}

type codexWebSocketSessionEntry struct {
	conn         *codexWebSocketConnection
	busy         bool
	idleTimer    *time.Timer
	continuation *codexWebSocketContinuation
}

type codexWebSocketContinuation struct {
	lastRequestBody   map[string]any
	lastResponseID    string
	lastResponseItems []any
}

// CodexResponsesWebSocketStatsSnapshot reports observable behavior from the
// Codex Responses WebSocket session cache.
type CodexResponsesWebSocketStatsSnapshot struct {
	Requests                int
	ConnectionsCreated      int
	ConnectionsReused       int
	CachedContextRequests   int
	FullContextRequests     int
	DeltaContextRequests    int
	LastInputItems          int
	LastDeltaInputItems     int
	LastPreviousResponseID  string
	WebSocketFailures       int
	SSEFallbacks            int
	WebSocketFallbackActive bool
	LastWebSocketError      string
}

var codexWebSocketSessions = struct {
	sync.Mutex
	entries       map[string]*codexWebSocketSessionEntry
	fallbackState map[string]bool
	stats         map[string]*CodexResponsesWebSocketStatsSnapshot
}{
	entries:       make(map[string]*codexWebSocketSessionEntry),
	fallbackState: make(map[string]bool),
	stats:         make(map[string]*CodexResponsesWebSocketStatsSnapshot),
}

func init() {
	sigma.RegisterSessionResourceCleanup(func(sessionID string) error {
		if sessionID == "" {
			CloseCodexResponsesWebSocketSessions()
			return nil
		}
		CloseCodexResponsesWebSocketSession(sessionID)
		return nil
	})
}

// CloseCodexResponsesWebSocketSession closes and forgets a cached Codex
// WebSocket session and clears its SSE fallback marker.
func CloseCodexResponsesWebSocketSession(sessionID string) {
	codexWebSocketSessions.Lock()
	entry := codexWebSocketSessions.entries[sessionID]
	delete(codexWebSocketSessions.entries, sessionID)
	delete(codexWebSocketSessions.fallbackState, sessionID)
	codexWebSocketSessions.Unlock()
	closeCodexWebSocketEntry(entry)
}

// CloseCodexResponsesWebSocketSessions closes all cached Codex WebSocket
// sessions and clears SSE fallback markers.
func CloseCodexResponsesWebSocketSessions() {
	codexWebSocketSessions.Lock()
	entries := make([]*codexWebSocketSessionEntry, 0, len(codexWebSocketSessions.entries))
	for _, entry := range codexWebSocketSessions.entries {
		entries = append(entries, entry)
	}
	codexWebSocketSessions.entries = make(map[string]*codexWebSocketSessionEntry)
	codexWebSocketSessions.fallbackState = make(map[string]bool)
	codexWebSocketSessions.Unlock()
	for _, entry := range entries {
		closeCodexWebSocketEntry(entry)
	}
}

// CodexResponsesWebSocketStats returns a copy of the recorded WebSocket stats
// for sessionID.
func CodexResponsesWebSocketStats(sessionID string) (CodexResponsesWebSocketStatsSnapshot, bool) {
	if sessionID == "" {
		return CodexResponsesWebSocketStatsSnapshot{}, false
	}
	codexWebSocketSessions.Lock()
	stats := codexWebSocketSessions.stats[sessionID]
	if stats == nil {
		codexWebSocketSessions.Unlock()
		return CodexResponsesWebSocketStatsSnapshot{}, false
	}
	out := *stats
	out.WebSocketFallbackActive = codexWebSocketSessions.fallbackState[sessionID]
	codexWebSocketSessions.Unlock()
	return out, true
}

// ResetCodexResponsesWebSocketStats clears recorded WebSocket stats for
// sessionID without closing cached sessions or changing fallback state.
func ResetCodexResponsesWebSocketStats(sessionID string) {
	if sessionID == "" {
		return
	}
	codexWebSocketSessions.Lock()
	delete(codexWebSocketSessions.stats, sessionID)
	codexWebSocketSessions.Unlock()
}

// ResetCodexResponsesWebSocketStatsAll clears all recorded WebSocket stats
// without closing cached sessions or changing fallback state.
func ResetCodexResponsesWebSocketStatsAll() {
	codexWebSocketSessions.Lock()
	codexWebSocketSessions.stats = make(map[string]*CodexResponsesWebSocketStatsSnapshot)
	codexWebSocketSessions.Unlock()
}

func (p *CodexResponsesProvider) runWebSocket(ctx context.Context, writer sigma.StreamWriter, model sigma.Model, req sigma.Request, opts sigma.Options, final sigma.AssistantMessage) {
	if codexWebSocketFallbackActive(opts.SessionID) {
		p.runSSE(ctx, writer, model, req, opts, final)
		return
	}

	parser, err := p.processWebSocket(ctx, writer, model, req, opts)
	if err == nil {
		_ = writer.Done(ctx, parser.finalize(ctx))
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		final.StopReason = sigma.StopReasonAborted
		_ = writer.Error(ctx, contextError(ctx, err), final)
		return
	}
	recordCodexWebSocketFailure(opts.SessionID, err)
	if parser == nil || !parser.started {
		recordCodexWebSocketFallback(opts.SessionID)
		p.runSSE(ctx, writer, model, req, opts, final)
		return
	}
	final = parser.finalize(ctx)
	final.StopReason = sigma.StopReasonError
	_ = writer.Error(ctx, err, final)
}

func (p *CodexResponsesProvider) runSSE(ctx context.Context, writer sigma.StreamWriter, model sigma.Model, req sigma.Request, opts sigma.Options, final sigma.AssistantMessage) {
	sseOpts := opts
	sseOpts.Transport = sigma.TransportSSE
	resp, err := p.do(ctx, model, req, sseOpts)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			final.StopReason = sigma.StopReasonAborted
			_ = writer.Error(ctx, contextError(ctx, err), final)
			return
		}
		_ = writer.Error(ctx, err, final)
		return
	}
	defer resp.Body.Close()
	body := sse.CloseOnContextDone(ctx, resp.Body)
	defer body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		providerErr := codexResponsesResponseError(resp, model)
		final.StopReason = sigma.StopReasonError
		final.Diagnostics = []sigma.Diagnostic{providerErr.Diagnostic()}
		_ = writer.Error(ctx, providerErr, final)
		return
	}

	final, err = parseResponsesStream(ctx, body, writer, model, codexResponsesStreamOptions(opts))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			final.StopReason = sigma.StopReasonAborted
			_ = writer.Error(ctx, contextError(ctx, err), final)
			return
		}
		final.StopReason = sigma.StopReasonError
		_ = writer.Error(ctx, err, final)
		return
	}
	_ = writer.Done(ctx, final)
}

func (p *CodexResponsesProvider) processWebSocket(ctx context.Context, writer sigma.StreamWriter, model sigma.Model, req sigma.Request, opts sigma.Options) (*responsesStreamParser, error) {
	body, err := p.requestBody(model, req, opts)
	if err != nil {
		return nil, err
	}
	requestBody, err := decodeCodexWebSocketBody(body)
	if err != nil {
		return nil, err
	}
	endpoint, err := p.endpoint(model, opts)
	if err != nil {
		return nil, err
	}
	wsURL, err := codexWebSocketURL(endpoint)
	if err != nil {
		return nil, err
	}
	headers, err := p.codexWebSocketHeaders(ctx, model, opts)
	if err != nil {
		return nil, err
	}
	acquired, err := acquireCodexWebSocket(ctx, wsURL, headers, opts.SessionID, codexWebSocketConnectTimeout(opts))
	if err != nil {
		return nil, err
	}
	keepConnection := false
	parser := newResponsesStreamParser(writer, model, codexResponsesStreamOptions(opts))
	defer func() {
		acquired.release(keepConnection)
	}()

	fullBody := cloneJSONMap(requestBody)
	sendBody := requestBody
	if acquired.entry != nil {
		sendBody = cachedCodexWebSocketRequestBody(acquired.entry, requestBody)
	}
	recordCodexWebSocketRequest(opts.SessionID, acquired.reused, sendBody, acquired.entry != nil)
	wireBody := map[string]any{providerToolOptionTypeKey: "response.create"}
	for key, value := range sendBody {
		wireBody[key] = value
	}
	wireData, err := json.Marshal(wireBody)
	if err != nil {
		return parser, fmt.Errorf("openai codex responses: encode websocket request: %w", err)
	}
	if err := sigma.RunTextPayloadDebugHooks(ctx, opts, model.Provider, sigma.APIOpenAICodexResponses, model.ID, wireData, headers); err != nil {
		return parser, err
	}
	if err := acquired.conn.WriteText(ctx, string(wireData)); err != nil {
		return parser, err
	}
	for {
		message, err := acquired.conn.ReadText(ctx)
		if err != nil {
			return parser, err
		}
		completed, err := parser.handleEventData(ctx, "", message)
		if err != nil {
			return parser, err
		}
		if completed {
			break
		}
	}
	final := parser.finalize(ctx)
	if acquired.entry != nil && parser.responseID != "" {
		acquired.entry.continuation = &codexWebSocketContinuation{
			lastRequestBody:   fullBody,
			lastResponseID:    parser.responseID,
			lastResponseItems: codexResponsesAssistantInputItems(model, final),
		}
		keepConnection = true
	}
	return parser, nil
}

type acquiredCodexWebSocket struct {
	conn    *codexWebSocketConnection
	entry   *codexWebSocketSessionEntry
	reused  bool
	release func(keep bool)
}

func acquireCodexWebSocket(ctx context.Context, wsURL string, headers http.Header, sessionID string, connectTimeout time.Duration) (*acquiredCodexWebSocket, error) {
	if sessionID == "" {
		conn, err := dialCodexWebSocket(ctx, wsURL, headers, connectTimeout)
		if err != nil {
			return nil, err
		}
		return &acquiredCodexWebSocket{
			conn: conn,
			release: func(bool) {
				conn.Close()
			},
		}, nil
	}

	codexWebSocketSessions.Lock()
	if entry := codexWebSocketSessions.entries[sessionID]; entry != nil {
		if entry.idleTimer != nil {
			entry.idleTimer.Stop()
			entry.idleTimer = nil
		}
		if !entry.busy && entry.conn.IsOpen() {
			entry.busy = true
			codexWebSocketSessions.Unlock()
			return &acquiredCodexWebSocket{
				conn:   entry.conn,
				entry:  entry,
				reused: true,
				release: func(keep bool) {
					releaseCodexWebSocketSession(sessionID, entry, keep)
				},
			}, nil
		}
		if !entry.busy {
			delete(codexWebSocketSessions.entries, sessionID)
			codexWebSocketSessions.Unlock()
			closeCodexWebSocketEntry(entry)
			return acquireCodexWebSocket(ctx, wsURL, headers, sessionID, connectTimeout)
		}
		codexWebSocketSessions.Unlock()
		conn, err := dialCodexWebSocket(ctx, wsURL, headers, connectTimeout)
		if err != nil {
			return nil, err
		}
		return &acquiredCodexWebSocket{
			conn: conn,
			release: func(bool) {
				conn.Close()
			},
		}, nil
	}
	codexWebSocketSessions.Unlock()

	conn, err := dialCodexWebSocket(ctx, wsURL, headers, connectTimeout)
	if err != nil {
		return nil, err
	}
	entry := &codexWebSocketSessionEntry{conn: conn, busy: true}
	codexWebSocketSessions.Lock()
	codexWebSocketSessions.entries[sessionID] = entry
	codexWebSocketSessions.Unlock()
	return &acquiredCodexWebSocket{
		conn:  conn,
		entry: entry,
		release: func(keep bool) {
			releaseCodexWebSocketSession(sessionID, entry, keep)
		},
	}, nil
}

func releaseCodexWebSocketSession(sessionID string, entry *codexWebSocketSessionEntry, keep bool) {
	codexWebSocketSessions.Lock()
	defer codexWebSocketSessions.Unlock()
	current := codexWebSocketSessions.entries[sessionID]
	if current != entry {
		return
	}
	if !keep || !entry.conn.IsOpen() {
		delete(codexWebSocketSessions.entries, sessionID)
		closeCodexWebSocketEntryLocked(entry)
		return
	}
	entry.busy = false
	entry.idleTimer = time.AfterFunc(codexWebSocketIdleTTL, func() {
		codexWebSocketSessions.Lock()
		if codexWebSocketSessions.entries[sessionID] == entry && !entry.busy {
			delete(codexWebSocketSessions.entries, sessionID)
			closeCodexWebSocketEntryLocked(entry)
		}
		codexWebSocketSessions.Unlock()
	})
}

func closeCodexWebSocketEntry(entry *codexWebSocketSessionEntry) {
	if entry == nil {
		return
	}
	codexWebSocketSessions.Lock()
	closeCodexWebSocketEntryLocked(entry)
	codexWebSocketSessions.Unlock()
}

func closeCodexWebSocketEntryLocked(entry *codexWebSocketSessionEntry) {
	if entry.idleTimer != nil {
		entry.idleTimer.Stop()
		entry.idleTimer = nil
	}
	entry.busy = false
	entry.conn.Close()
}

func codexWebSocketFallbackActive(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	codexWebSocketSessions.Lock()
	active := codexWebSocketSessions.fallbackState[sessionID]
	codexWebSocketSessions.Unlock()
	return active
}

func recordCodexWebSocketFallback(sessionID string) {
	if sessionID == "" {
		return
	}
	codexWebSocketSessions.Lock()
	codexWebSocketSessions.fallbackState[sessionID] = true
	stats := getCodexWebSocketStatsLocked(sessionID)
	stats.SSEFallbacks++
	stats.WebSocketFallbackActive = true
	codexWebSocketSessions.Unlock()
}

func recordCodexWebSocketFailure(sessionID string, err error) {
	if sessionID == "" {
		return
	}
	codexWebSocketSessions.Lock()
	stats := getCodexWebSocketStatsLocked(sessionID)
	stats.WebSocketFailures++
	stats.LastWebSocketError = err.Error()
	stats.WebSocketFallbackActive = codexWebSocketSessions.fallbackState[sessionID]
	codexWebSocketSessions.Unlock()
}

func recordCodexWebSocketRequest(sessionID string, reused bool, body map[string]any, cachedContext bool) {
	if sessionID == "" {
		return
	}
	codexWebSocketSessions.Lock()
	stats := getCodexWebSocketStatsLocked(sessionID)
	stats.Requests++
	if reused {
		stats.ConnectionsReused++
	} else {
		stats.ConnectionsCreated++
	}
	if cachedContext {
		stats.CachedContextRequests++
	}
	stats.LastInputItems = lenAnySlice(body["input"])
	if previousResponseID, _ := body["previous_response_id"].(string); previousResponseID != "" {
		stats.DeltaContextRequests++
		stats.LastDeltaInputItems = lenAnySlice(body["input"])
		stats.LastPreviousResponseID = previousResponseID
	} else {
		stats.FullContextRequests++
		stats.LastDeltaInputItems = 0
		stats.LastPreviousResponseID = ""
	}
	stats.WebSocketFallbackActive = codexWebSocketSessions.fallbackState[sessionID]
	codexWebSocketSessions.Unlock()
}

func getCodexWebSocketStatsLocked(sessionID string) *CodexResponsesWebSocketStatsSnapshot {
	stats := codexWebSocketSessions.stats[sessionID]
	if stats == nil {
		stats = &CodexResponsesWebSocketStatsSnapshot{}
		codexWebSocketSessions.stats[sessionID] = stats
	}
	return stats
}

func cachedCodexWebSocketRequestBody(entry *codexWebSocketSessionEntry, body map[string]any) map[string]any {
	if entry.continuation == nil {
		return body
	}
	delta, ok := codexWebSocketInputDelta(body, entry.continuation)
	if !ok || entry.continuation.lastResponseID == "" {
		entry.continuation = nil
		return body
	}
	cached := cloneJSONMap(body)
	cached["previous_response_id"] = entry.continuation.lastResponseID
	cached["input"] = delta
	return cached
}

func codexWebSocketInputDelta(body map[string]any, continuation *codexWebSocketContinuation) ([]any, bool) {
	if !jsonValuesEqual(codexRequestWithoutInput(body), codexRequestWithoutInput(continuation.lastRequestBody)) {
		return nil, false
	}
	current, ok := anySlice(body["input"])
	if !ok {
		return nil, false
	}
	last, ok := anySlice(continuation.lastRequestBody["input"])
	if !ok {
		return nil, false
	}
	baseline := append([]any{}, last...)
	baseline = append(baseline, continuation.lastResponseItems...)
	if len(current) < len(baseline) {
		return nil, false
	}
	if !jsonValuesEqual(current[:len(baseline)], baseline) {
		return nil, false
	}
	return current[len(baseline):], true
}

func codexRequestWithoutInput(body map[string]any) map[string]any {
	copied := cloneJSONMap(body)
	delete(copied, "input")
	delete(copied, "previous_response_id")
	return copied
}

func codexResponsesAssistantInputItems(model sigma.Model, final sigma.AssistantMessage) []any {
	message := sigma.Message{
		Role:     sigma.RoleAssistant,
		Content:  final.Content,
		Provider: final.Provider,
		API:      model.API,
		Model:    final.Model,
	}
	items, err := responsesAssistantItems(model, message, 0)
	if err != nil {
		return nil
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		if item["type"] == "function_call_output" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (p *CodexResponsesProvider) codexWebSocketHeaders(ctx context.Context, model sigma.Model, opts sigma.Options) (http.Header, error) {
	opts, credential, hasCredential, err := p.resolveRequestAuth(ctx, model, opts)
	if err != nil {
		return nil, err
	}
	endpoint, err := p.endpoint(model, opts)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("OpenAI-Beta", codexResponsesWebSocketBeta)
	req.Header.Set("originator", "sigma")
	req.Header.Set("User-Agent", "sigma/openai-codex-responses")
	if err := p.addAuthHeader(ctx, req, model, opts, credential, hasCredential); err != nil {
		return nil, err
	}
	p.addProviderHeaders(req, model.Provider, opts)
	for key, value := range p.base.headers {
		req.Header.Set(key, value)
	}
	addOpenAICompatibleModelHeaders(req, model)
	for key, value := range opts.Headers {
		req.Header.Set(key, value)
	}
	sigma.ApplySuppressedHeaders(req.Header, opts)
	return req.Header, nil
}

func dialCodexWebSocket(ctx context.Context, rawURL string, headers http.Header, connectTimeout time.Duration) (*codexWebSocketConnection, error) {
	connectCtx, cancel := codexWebSocketConnectContext(ctx, connectTimeout)
	defer cancel()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("openai codex responses: websocket URL: %w", err)
	}
	if parsed.Scheme != "wss" && parsed.Scheme != "ws" {
		return nil, fmt.Errorf("openai codex responses: unsupported websocket scheme %q", parsed.Scheme)
	}
	host := codexWebSocketHostPort(parsed)
	conn, err := dialCodexWebSocketConn(connectCtx, parsed, host)
	if err != nil {
		return nil, codexWebSocketConnectError(ctx, connectCtx, connectTimeout, err)
	}
	clearDeadline := codexWebSocketConnDeadline(connectCtx, conn)
	defer clearDeadline()

	if parsed.Scheme == "wss" {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName: parsed.Hostname(),
			MinVersion: tls.VersionTLS12,
		})
		if err := tlsConn.HandshakeContext(connectCtx); err != nil {
			_ = conn.Close()
			err = fmt.Errorf("openai codex responses: websocket tls handshake: %w", err)
			return nil, codexWebSocketConnectError(ctx, connectCtx, connectTimeout, err)
		}
		conn = tlsConn
	}
	ws := &codexWebSocketConnection{conn: conn, reader: bufio.NewReader(conn)}
	if err := ws.handshake(connectCtx, parsed, headers); err != nil {
		ws.Close()
		return nil, codexWebSocketConnectError(ctx, connectCtx, connectTimeout, err)
	}
	return ws, nil
}

func codexWebSocketConnectTimeout(opts sigma.Options) time.Duration {
	if opts.OpenAIOptions == nil || opts.OpenAIOptions.CodexWebSocketConnectTimeout == nil {
		return codexWebSocketDefaultConnectTimeout
	}
	return *opts.OpenAIOptions.CodexWebSocketConnectTimeout
}

func codexWebSocketConnectContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout == 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func codexWebSocketConnectError(parent context.Context, connectCtx context.Context, timeout time.Duration, err error) error {
	var netErr net.Error
	if timeout > 0 && parent.Err() == nil && (connectCtx.Err() != nil || errors.As(err, &netErr) && netErr.Timeout()) {
		return fmt.Errorf("openai codex responses: websocket connect timeout after %s", timeout)
	}
	return err
}

func codexWebSocketConnDeadline(ctx context.Context, conn net.Conn) func() {
	deadline, ok := ctx.Deadline()
	if !ok {
		return func() {}
	}
	_ = conn.SetDeadline(deadline)
	return func() {
		_ = conn.SetDeadline(time.Time{})
	}
}

func dialCodexWebSocketConn(ctx context.Context, parsed *url.URL, host string) (net.Conn, error) {
	proxyURL, err := codexWebSocketProxyURL(parsed)
	if err != nil {
		return nil, fmt.Errorf("openai codex responses: websocket proxy: %w", err)
	}
	if proxyURL != nil {
		return dialCodexWebSocketProxy(ctx, proxyURL, host)
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("openai codex responses: websocket dial: %w", err)
	}
	return conn, nil
}

func dialCodexWebSocketProxy(ctx context.Context, proxyURL *url.URL, targetHost string) (net.Conn, error) {
	proxyHost := codexProxyHostPort(proxyURL)
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", proxyHost)
	if err != nil {
		return nil, fmt.Errorf("openai codex responses: websocket proxy dial: %w", err)
	}
	clearDeadline := codexWebSocketConnDeadline(ctx, conn)
	defer clearDeadline()

	if proxyURL.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName: proxyURL.Hostname(),
			MinVersion: tls.VersionTLS12,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("openai codex responses: websocket proxy tls handshake: %w", err)
		}
		conn = tlsConn
	}
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Scheme: "http", Host: targetHost},
		Host:   targetHost,
		Header: make(http.Header),
	}
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		token := base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username() + ":" + password))
		req.Header.Set("Proxy-Authorization", "Basic "+token)
	}
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("openai codex responses: websocket proxy connect request: %w", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("openai codex responses: websocket proxy connect response: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = conn.Close()
		return nil, fmt.Errorf("openai codex responses: websocket proxy connect status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return conn, nil
}

func codexWebSocketProxyURL(parsed *url.URL) (*url.URL, error) {
	target := codexWebSocketProxyTarget(parsed)
	if !codexShouldProxy(target) {
		return nil, nil
	}
	proxy := codexProxyEnv(strings.ToUpper(target.Scheme)+"_PROXY", strings.ToLower(target.Scheme)+"_proxy")
	if proxy == "" {
		proxy = codexProxyEnv("ALL_PROXY", "all_proxy")
	}
	if proxy == "" {
		return nil, nil
	}
	if !strings.Contains(proxy, "://") {
		proxy = "http://" + proxy
	}
	proxyURL, err := url.Parse(proxy)
	if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
		if err == nil {
			err = fmt.Errorf("missing scheme or host")
		}
		return nil, fmt.Errorf("invalid proxy URL %q: %w", proxy, err)
	}
	if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported proxy protocol %q", proxyURL.Scheme)
	}
	return proxyURL, nil
}

func codexWebSocketProxyTarget(parsed *url.URL) *url.URL {
	target := *parsed
	if target.Scheme == "wss" {
		target.Scheme = "https"
	} else {
		target.Scheme = "http"
	}
	return &target
}

func codexShouldProxy(target *url.URL) bool {
	noProxy := strings.ToLower(codexProxyEnv("NO_PROXY", "no_proxy"))
	if noProxy == "" {
		return true
	}
	if strings.TrimSpace(noProxy) == "*" {
		return false
	}
	host := strings.ToLower(target.Hostname())
	port := codexURLPort(target)
	for _, token := range strings.FieldsFunc(noProxy, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		if codexNoProxyMatch(host, port, strings.TrimSpace(token)) {
			return false
		}
	}
	return true
}

func codexNoProxyMatch(host, port, token string) bool {
	if token == "" {
		return false
	}
	tokenHost, tokenPort := codexSplitNoProxyToken(token)
	if tokenPort != "" && tokenPort != port {
		return false
	}
	tokenHost = strings.TrimPrefix(tokenHost, "*")
	if strings.HasPrefix(tokenHost, ".") {
		return host == strings.TrimPrefix(tokenHost, ".") || strings.HasSuffix(host, tokenHost)
	}
	return host == tokenHost
}

func codexSplitNoProxyToken(token string) (string, string) {
	token = strings.ToLower(token)
	if strings.Count(token, ":") == 1 {
		host, port, ok := strings.Cut(token, ":")
		if ok && port != "" {
			return host, port
		}
	}
	return token, ""
}

func codexProxyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func codexWebSocketHostPort(parsed *url.URL) string {
	return net.JoinHostPort(parsed.Hostname(), codexWebSocketPort(parsed))
}

func codexWebSocketPort(parsed *url.URL) string {
	if port := parsed.Port(); port != "" {
		return port
	}
	if parsed.Scheme == "wss" {
		return "443"
	}
	return "80"
}

func codexProxyHostPort(proxyURL *url.URL) string {
	port := proxyURL.Port()
	if port == "" {
		if proxyURL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(proxyURL.Hostname(), port)
}

func codexURLPort(parsed *url.URL) string {
	if port := parsed.Port(); port != "" {
		return port
	}
	if parsed.Scheme == "https" || parsed.Scheme == "wss" {
		return "443"
	}
	return "80"
}

func (c *codexWebSocketConnection) handshake(ctx context.Context, parsed *url.URL, headers http.Header) error {
	key, err := randomWebSocketKey()
	if err != nil {
		return err
	}
	requestURL := *parsed
	if parsed.Scheme == "wss" {
		requestURL.Scheme = "https"
	} else {
		requestURL.Scheme = "http"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return err
	}
	req.Host = parsed.Host
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)
	if err := req.Write(c.conn); err != nil {
		return fmt.Errorf("openai codex responses: websocket handshake request: %w", err)
	}
	resp, err := http.ReadResponse(c.reader, req)
	if err != nil {
		return fmt.Errorf("openai codex responses: websocket handshake response: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("openai codex responses: websocket handshake status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		return fmt.Errorf("openai codex responses: websocket handshake missing upgrade")
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Connection")), "upgrade") {
		return fmt.Errorf("openai codex responses: websocket handshake missing connection upgrade")
	}
	if got, want := resp.Header.Get("Sec-WebSocket-Accept"), webSocketAccept(key); got != want {
		return fmt.Errorf("openai codex responses: websocket accept mismatch")
	}
	return nil
}

func (c *codexWebSocketConnection) WriteText(ctx context.Context, text string) error {
	return c.writeFrame(ctx, webSocketOpcodeText, []byte(text))
}

func (c *codexWebSocketConnection) ReadText(ctx context.Context) (string, error) {
	return c.readText(ctx, codexWebSocketMaxMessageBytes)
}

func (c *codexWebSocketConnection) readText(ctx context.Context, maxMessageBytes int) (string, error) {
	var message bytes.Buffer
	for {
		opcode, final, payload, err := c.readFrame(ctx)
		if err != nil {
			return "", err
		}
		switch opcode {
		case webSocketOpcodeText, webSocketOpcodeContinuation:
			remaining := maxMessageBytes - message.Len()
			if remaining < 0 || len(payload) > remaining {
				return "", fmt.Errorf("openai codex responses: websocket message exceeds %d bytes", maxMessageBytes)
			}
			message.Write(payload)
			if final {
				return message.String(), nil
			}
		case webSocketOpcodePing:
			if err := c.writeFrame(ctx, webSocketOpcodePong, payload); err != nil {
				return "", err
			}
		case webSocketOpcodePong:
			continue
		case webSocketOpcodeClose:
			c.Close()
			return "", io.EOF
		default:
			return "", fmt.Errorf("openai codex responses: unsupported websocket frame opcode %d", opcode)
		}
	}
}

func (c *codexWebSocketConnection) readFrame(ctx context.Context) (byte, bool, []byte, error) {
	if err := ctx.Err(); err != nil {
		c.Close()
		return 0, false, nil, err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	defer close(done)

	header := make([]byte, 2)
	if _, err := io.ReadFull(c.reader, header); err != nil {
		return 0, false, nil, fmt.Errorf("openai codex responses: websocket read: %w", err)
	}
	final := header[0]&0x80 != 0
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var extended uint16
		if err := binary.Read(c.reader, binary.BigEndian, &extended); err != nil {
			return 0, false, nil, err
		}
		length = uint64(extended)
	case 127:
		if err := binary.Read(c.reader, binary.BigEndian, &length); err != nil {
			return 0, false, nil, err
		}
	}
	if length > codexWebSocketMaxFrameBytes {
		return 0, false, nil, fmt.Errorf("openai codex responses: websocket frame exceeds %d bytes", codexWebSocketMaxFrameBytes)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.reader, mask[:]); err != nil {
			return 0, false, nil, fmt.Errorf("openai codex responses: websocket read mask: %w", err)
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return 0, false, nil, fmt.Errorf("openai codex responses: websocket read payload: %w", err)
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, final, payload, nil
}

func (c *codexWebSocketConnection) writeFrame(ctx context.Context, opcode byte, payload []byte) error {
	if err := ctx.Err(); err != nil {
		c.Close()
		return err
	}
	stopCancel := context.AfterFunc(ctx, c.Close)
	defer stopCancel()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !c.IsOpen() {
		return io.ErrClosedPipe
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return fmt.Errorf("openai codex responses: websocket mask: %w", err)
	}
	var frame bytes.Buffer
	frame.WriteByte(0x80 | opcode)
	switch length := len(payload); {
	case length < 126:
		frame.WriteByte(0x80 | byte(length))
	case length <= 0xffff:
		frame.WriteByte(0x80 | 126)
		_ = binary.Write(&frame, binary.BigEndian, uint16(length))
	default:
		frame.WriteByte(0x80 | 127)
		_ = binary.Write(&frame, binary.BigEndian, uint64(length))
	}
	frame.Write(mask[:])
	for i, b := range payload {
		frame.WriteByte(b ^ mask[i%4])
	}
	if err := writeCodexWebSocketFrame(c.conn, frame.Bytes()); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("openai codex responses: websocket write: %w", err)
	}
	return nil
}

func writeCodexWebSocketFrame(conn net.Conn, frame []byte) error {
	for len(frame) > 0 {
		written, err := conn.Write(frame)
		if err != nil {
			return fmt.Errorf("write websocket frame: %w", err)
		}
		if written == 0 {
			return io.ErrUnexpectedEOF
		}
		frame = frame[written:]
	}
	return nil
}

func (c *codexWebSocketConnection) IsOpen() bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return !c.closed
}

func (c *codexWebSocketConnection) Close() {
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return
	}
	c.closed = true
	conn := c.conn
	c.stateMu.Unlock()
	_ = conn.Close()
}

func codexWebSocketURL(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("openai codex responses: parse websocket endpoint: %w", err)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("unsupported endpoint scheme %q", parsed.Scheme)
	}
	return parsed.String(), nil
}

func randomWebSocketKey() (string, error) {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", fmt.Errorf("openai codex responses: websocket key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key[:]), nil
}

func webSocketAccept(key string) string {
	// #nosec G401 -- RFC 6455 requires SHA-1 for Sec-WebSocket-Accept.
	sum := sha1.Sum([]byte(key + webSocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func decodeCodexWebSocketBody(body []byte) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("openai codex responses: decode websocket request: %w", err)
	}
	return out, nil
}

func cloneJSONMap(value map[string]any) map[string]any {
	data, _ := json.Marshal(value)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

func anySlice(value any) ([]any, bool) {
	items, ok := value.([]any)
	return items, ok
}

func lenAnySlice(value any) int {
	items, ok := anySlice(value)
	if !ok {
		return 0
	}
	return len(items)
}

func jsonValuesEqual(a any, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}
