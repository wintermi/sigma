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
	"strings"
	"sync"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
)

const (
	codexWebSocketIdleTTL = 5 * time.Minute
	webSocketGUID         = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	webSocketOpcodeContinuation = 0x0
	webSocketOpcodeText         = 0x1
	webSocketOpcodeClose        = 0x8
	webSocketOpcodePing         = 0x9
	webSocketOpcodePong         = 0xA
)

type codexWebSocketConnection struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
	closed bool
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

var codexWebSocketSessions = struct {
	sync.Mutex
	entries       map[string]*codexWebSocketSessionEntry
	fallbackState map[string]bool
}{
	entries:       make(map[string]*codexWebSocketSessionEntry),
	fallbackState: make(map[string]bool),
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
	acquired, err := acquireCodexWebSocket(ctx, wsURL, headers, opts.SessionID)
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
	release func(keep bool)
}

func acquireCodexWebSocket(ctx context.Context, wsURL string, headers http.Header, sessionID string) (*acquiredCodexWebSocket, error) {
	if sessionID == "" {
		conn, err := dialCodexWebSocket(ctx, wsURL, headers)
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
				conn:  entry.conn,
				entry: entry,
				release: func(keep bool) {
					releaseCodexWebSocketSession(sessionID, entry, keep)
				},
			}, nil
		}
		if !entry.busy {
			delete(codexWebSocketSessions.entries, sessionID)
			codexWebSocketSessions.Unlock()
			closeCodexWebSocketEntry(entry)
			return acquireCodexWebSocket(ctx, wsURL, headers, sessionID)
		}
		codexWebSocketSessions.Unlock()
		conn, err := dialCodexWebSocket(ctx, wsURL, headers)
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

	conn, err := dialCodexWebSocket(ctx, wsURL, headers)
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
	codexWebSocketSessions.Unlock()
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
	if err := p.addAuthHeader(ctx, req, model, opts); err != nil {
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
	return req.Header, nil
}

func dialCodexWebSocket(ctx context.Context, rawURL string, headers http.Header) (*codexWebSocketConnection, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("openai codex responses: websocket URL: %w", err)
	}
	host := parsed.Host
	if !strings.Contains(host, ":") {
		if parsed.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("openai codex responses: websocket dial: %w", err)
	}
	if parsed.Scheme == "wss" {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName: parsed.Hostname(),
			MinVersion: tls.VersionTLS12,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("openai codex responses: websocket tls handshake: %w", err)
		}
		conn = tlsConn
	} else if parsed.Scheme != "ws" {
		_ = conn.Close()
		return nil, fmt.Errorf("openai codex responses: unsupported websocket scheme %q", parsed.Scheme)
	}
	ws := &codexWebSocketConnection{conn: conn, reader: bufio.NewReader(conn)}
	if err := ws.handshake(ctx, parsed, headers); err != nil {
		ws.Close()
		return nil, err
	}
	return ws, nil
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
	var message bytes.Buffer
	for {
		opcode, final, payload, err := c.readFrame(ctx)
		if err != nil {
			return "", err
		}
		switch opcode {
		case webSocketOpcodeText, webSocketOpcodeContinuation:
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
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
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
	if _, err := c.conn.Write(frame.Bytes()); err != nil {
		return fmt.Errorf("openai codex responses: websocket write: %w", err)
	}
	return nil
}

func (c *codexWebSocketConnection) IsOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

func (c *codexWebSocketConnection) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	conn := c.conn
	c.mu.Unlock()
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

func jsonValuesEqual(a any, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}
