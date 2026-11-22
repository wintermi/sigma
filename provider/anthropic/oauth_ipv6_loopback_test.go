// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"
)

func listenIPv6LoopbackOrSkip(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func TestAnthropicCallbackServerRefusesPortHeldOnIPv6Loopback(t *testing.T) {
	t.Parallel()

	held := listenIPv6LoopbackOrSkip(t)
	_, port, err := net.SplitHostPort(held.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server, err := startAnthropicBrowserCallbackServer(net.JoinHostPort("127.0.0.1", port), "state")
	if err == nil {
		server.close()
		t.Fatal("callback server started although another process holds the port on [::1]")
	}
}

func TestAnthropicCallbackServerAcceptsIPv6LoopbackCallback(t *testing.T) {
	t.Parallel()

	_ = listenIPv6LoopbackOrSkip(t)
	server, err := startAnthropicBrowserCallbackServer("127.0.0.1:0", "state-value")
	if err != nil {
		t.Fatalf("startAnthropicBrowserCallbackServer returned error: %v", err)
	}
	t.Cleanup(server.close)
	redirect, err := url.Parse(server.redirectURI)
	if err != nil {
		t.Fatal(err)
	}
	callback := "http://" + net.JoinHostPort("::1", redirect.Port()) + anthropicOAuthCallbackPath + "?code=ipv6-code&state=state-value"
	resp, err := http.Get(callback)
	if err != nil {
		t.Fatalf("IPv6 callback request failed: %v", err)
	}
	_ = resp.Body.Close()
	select {
	case result := <-server.done:
		if result.err != nil || result.code != "ipv6-code" {
			t.Fatalf("callback result = %+v, want code ipv6-code", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback server did not receive the IPv6 callback")
	}
}

// Only a missing IPv6 loopback lets login continue on IPv4 alone; any other
// bind failure, including Windows' WSAEADDRINUSE (10048), stops it.
func TestIPv6LoopbackUnavailableOnlyForMissingIPv6(t *testing.T) {
	t.Parallel()
	bindError := func(err error) error {
		return &net.OpError{Op: "listen", Net: "tcp", Err: os.NewSyscallError("bind", err)}
	}
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "address not available", err: bindError(syscall.EADDRNOTAVAIL), want: true},
		{name: "address family unsupported", err: bindError(syscall.EAFNOSUPPORT), want: true},
		{name: "windows address not available", err: bindError(syscall.Errno(10049)), want: true},
		{name: "windows address family unsupported", err: bindError(syscall.Errno(10047)), want: true},
		{name: "address in use", err: bindError(syscall.EADDRINUSE)},
		{name: "windows address in use", err: bindError(syscall.Errno(10048))},
		{name: "windows access denied", err: bindError(syscall.Errno(10013))},
		{name: "other failure", err: errors.New("listen failed")},
	} {
		if got := ipv6LoopbackUnavailable(tt.err); got != tt.want {
			t.Errorf("%s: unavailable = %v, want %v", tt.name, got, tt.want)
		}
	}
}
