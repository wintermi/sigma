// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCodexWebSocketRequestIDsAreOrderedUUIDv7(t *testing.T) {
	t.Parallel()

	first, err := codexWebSocketRequestID("")
	if err != nil {
		t.Fatalf("first request id returned error: %v", err)
	}
	second, err := codexWebSocketRequestID("")
	if err != nil {
		t.Fatalf("second request id returned error: %v", err)
	}
	for _, requestID := range []string{first, second} {
		if len(requestID) != 36 || requestID[8] != '-' || requestID[13] != '-' || requestID[14] != '7' ||
			requestID[18] != '-' || requestID[23] != '-' || !strings.ContainsRune("89ab", rune(requestID[19])) {
			t.Fatalf("request id = %q, want UUIDv7", requestID)
		}
	}
	if first >= second {
		t.Fatalf("request ids = %q, %q; want increasing order", first, second)
	}
}

func TestCodexWebSocketRejectsOversized64BitFrameBeforePayload(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	connection := &codexWebSocketConnection{conn: client, reader: bufio.NewReader(client)}
	t.Cleanup(connection.Close)
	t.Cleanup(func() { _ = server.Close() })
	writeErr := make(chan error, 1)
	go func() {
		header := []byte{0x81, 127}
		length := make([]byte, 8)
		binary.BigEndian.PutUint64(length, uint64(codexWebSocketMaxFrameBytes)+1)
		_, err := server.Write(append(header, length...))
		writeErr <- err
	}()

	_, err := connection.ReadText(context.Background())
	if err == nil || !strings.Contains(err.Error(), "websocket frame exceeds") {
		t.Fatalf("ReadText error = %v, want oversized frame error", err)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write frame header: %v", err)
	}
}

func TestCodexWebSocketRejectsOversizedFragmentedMessage(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	connection := &codexWebSocketConnection{conn: client, reader: bufio.NewReader(client)}
	t.Cleanup(connection.Close)
	t.Cleanup(func() { _ = server.Close() })
	writeErr := make(chan error, 1)
	go func() {
		frames := append(
			codexTestServerFrame(webSocketOpcodeText, false, []byte("12345")),
			codexTestServerFrame(webSocketOpcodeContinuation, true, []byte("6789"))...,
		)
		_, err := server.Write(frames)
		writeErr <- err
	}()

	_, err := connection.readText(context.Background(), 8)
	if err == nil || !strings.Contains(err.Error(), "websocket message exceeds") {
		t.Fatalf("readText error = %v, want oversized message error", err)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write fragmented frames: %v", err)
	}
}

func TestCodexWebSocketReadsFragmentedMessage(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	connection := &codexWebSocketConnection{conn: client, reader: bufio.NewReader(client)}
	t.Cleanup(connection.Close)
	t.Cleanup(func() { _ = server.Close() })
	writeErr := make(chan error, 1)
	go func() {
		frames := append(
			codexTestServerFrame(webSocketOpcodeText, false, []byte("hello ")),
			codexTestServerFrame(webSocketOpcodeContinuation, true, []byte("world"))...,
		)
		_, err := server.Write(frames)
		writeErr <- err
	}()

	got, err := connection.ReadText(context.Background())
	if err != nil {
		t.Fatalf("ReadText returned error: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("ReadText = %q, want hello world", got)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write fragmented frames: %v", err)
	}
}

func TestCodexWebSocketBlockedWriteStopsOnCancellationAndClose(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	connection := &codexWebSocketConnection{conn: client, reader: bufio.NewReader(client)}
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	writeErr := make(chan error, 1)
	go func() {
		writeErr <- connection.WriteText(ctx, "blocked")
	}()

	select {
	case err := <-writeErr:
		t.Fatalf("WriteText returned before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	closeDone := make(chan struct{})
	go func() {
		connection.Close()
		close(closeDone)
	}()

	select {
	case err := <-writeErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("WriteText error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WriteText did not stop after cancellation")
	}
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close deadlocked with canceled write")
	}
}

func TestWriteCodexWebSocketFrameHandlesPartialWrites(t *testing.T) {
	t.Parallel()

	conn := &codexShortWriteConn{maxWrite: 3}
	want := []byte("complete websocket frame")
	if err := writeCodexWebSocketFrame(conn, want); err != nil {
		t.Fatalf("writeCodexWebSocketFrame returned error: %v", err)
	}
	if !bytes.Equal(conn.Bytes(), want) {
		t.Fatalf("written frame = %q, want %q", conn.Bytes(), want)
	}
}

func codexTestServerFrame(opcode byte, final bool, payload []byte) []byte {
	first := opcode
	if final {
		first |= 0x80
	}
	return append([]byte{first, byte(len(payload))}, payload...)
}

type codexShortWriteConn struct {
	bytes.Buffer
	maxWrite int
}

func (c *codexShortWriteConn) Read([]byte) (int, error) { return 0, io.EOF }

func (c *codexShortWriteConn) Write(p []byte) (int, error) {
	if len(p) > c.maxWrite {
		p = p[:c.maxWrite]
	}
	return c.Buffer.Write(p)
}

func (c *codexShortWriteConn) Close() error                     { return nil }
func (c *codexShortWriteConn) LocalAddr() net.Addr              { return codexTestAddr("local") }
func (c *codexShortWriteConn) RemoteAddr() net.Addr             { return codexTestAddr("remote") }
func (c *codexShortWriteConn) SetDeadline(time.Time) error      { return nil }
func (c *codexShortWriteConn) SetReadDeadline(time.Time) error  { return nil }
func (c *codexShortWriteConn) SetWriteDeadline(time.Time) error { return nil }

type codexTestAddr string

func (a codexTestAddr) Network() string { return "test" }
func (a codexTestAddr) String() string  { return string(a) }

func TestCodexWebSocketProxyURL(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		env       map[string]string
		wantProxy string
		wantErr   string
	}{
		{
			name:   "wss uses https proxy",
			target: "wss://api.openai.com/v1/responses",
			env: map[string]string{
				"HTTPS_PROXY": "http://proxy.example:8080",
			},
			wantProxy: "http://proxy.example:8080",
		},
		{
			name:   "ws uses http proxy",
			target: "ws://api.openai.com/v1/responses",
			env: map[string]string{
				"HTTP_PROXY": "http://proxy.example:8080",
			},
			wantProxy: "http://proxy.example:8080",
		},
		{
			name:   "no proxy suppresses exact host",
			target: "wss://api.openai.com/v1/responses",
			env: map[string]string{
				"HTTPS_PROXY": "http://proxy.example:8080",
				"NO_PROXY":    "api.openai.com",
			},
		},
		{
			name:   "unsupported proxy scheme",
			target: "wss://api.openai.com/v1/responses",
			env: map[string]string{
				"HTTPS_PROXY": "socks5://proxy.example:1080",
			},
			wantErr: "unsupported proxy protocol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearCodexProxyEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			parsed, err := url.Parse(tt.target)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}

			got, err := codexWebSocketProxyURL(parsed)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("proxy error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("codexWebSocketProxyURL returned error: %v", err)
			}
			if tt.wantProxy == "" {
				if got != nil {
					t.Fatalf("proxy URL = %v, want nil", got)
				}
				return
			}
			if got == nil || got.String() != tt.wantProxy {
				t.Fatalf("proxy URL = %v, want %q", got, tt.wantProxy)
			}
		})
	}
}

func clearCodexProxyEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"ALL_PROXY",
		"http_proxy",
		"https_proxy",
		"no_proxy",
		"all_proxy",
	} {
		t.Setenv(key, "")
	}
}
