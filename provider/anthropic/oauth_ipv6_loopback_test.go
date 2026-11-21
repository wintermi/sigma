// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"net"
	"net/http"
	"net/url"
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
