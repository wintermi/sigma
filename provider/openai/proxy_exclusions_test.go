// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"net/url"
	"testing"
)

func TestCodexProxyExclusions(t *testing.T) {
	for _, tt := range []struct {
		name, target, exclusion string
		bypass                  bool
	}{
		{"root", "wss://example.com", "example.com", true},
		{"descendant", "wss://api.example.com", "example.com", true},
		{"lookalike", "wss://badexample.com", "example.com", false},
		{"suffix lookalike", "wss://example.com.evil", "example.com", false},
		{"dot root", "wss://example.com", ".example.com", true},
		{"dot child", "wss://api.example.com", ".example.com", true},
		{"wildcard root", "wss://example.com", "*.example.com", true},
		{"wildcard child", "wss://a.b.example.com", "*.example.com", true},
		{"all", "wss://elsewhere.test", "*", true},
		{"all in list", "wss://elsewhere.test", "example.com, *, other.test", true},
		{"case and whitespace", "wss://API.Example.COM", " other.test\n EXAMPLE.COM\t", true},
		{"port matches", "wss://api.example.com", "example.com:443", true},
		{"port differs", "wss://api.example.com:8443", "example.com:443", false},
		{"no port", "wss://api.example.com:8443", "example.com", true},
		{"ws default port", "ws://example.com", "example.com:80", true},
		{"ipv4", "wss://192.0.2.1", "192.0.2.1", true},
		{"ipv4 port", "wss://192.0.2.1:8443", "192.0.2.1:8443", true},
		{"ipv4 differs", "wss://192.0.2.2", "192.0.2.1", false},
		{"ipv4 is not domain", "wss://sub.192.0.2.1", "192.0.2.1", false},
		{"ipv6", "wss://[2001:db8::1]", "2001:db8::1", true},
		{"ipv6 bracketed", "wss://[2001:db8::1]", "[2001:db8::1]", true},
		{"ipv6 canonical", "wss://[2001:db8::1]", "2001:0db8:0:0:0:0:0:1", true},
		{"ipv6 port", "wss://[2001:db8::1]:8443", "[2001:db8::1]:8443", true},
		{"ipv6 default port", "wss://[2001:db8::1]", "[2001:db8::1]:443", true},
		{"ipv6 wrong port", "wss://[2001:db8::1]", "[2001:db8::1]:8443", false},
		{"ipv6 differs", "wss://[2001:db8::2]", "[2001:db8::1]", false},
		{"invalid port", "wss://example.com", "example.com:invalid", false},
		{"empty port", "wss://example.com", "example.com:", false},
		{"malformed wildcard", "wss://example.com", "*example.com", false},
		{"empty wildcard label", "wss://example.com", "*..example.com", false},
		{"bracketed domain", "wss://example.com", "[example.com]:443", false},
		{"out of range port", "wss://example.com", "example.com:65536", false},
		{"empty suffix", "wss://example.com", ".", false},
		{"malformed brackets", "wss://[2001:db8::1]", "[2001:db8::1", false},
		{"CIDR not supported", "wss://192.0.2.1", "192.0.2.0/24", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearCodexProxyEnv(t)
			t.Setenv("HTTPS_PROXY", "http://proxy.example:8080")
			t.Setenv("HTTP_PROXY", "http://proxy.example:8080")
			t.Setenv("NO_PROXY", tt.exclusion)
			target, err := url.Parse(tt.target)
			if err != nil {
				t.Fatal(err)
			}
			proxy, err := codexWebSocketProxyURL(target)
			if err != nil {
				t.Fatal(err)
			}
			if (proxy == nil) != tt.bypass {
				t.Fatalf("target=%s NO_PROXY=%q proxy=%v, want bypass=%v", tt.target, tt.exclusion, proxy, tt.bypass)
			}
		})
	}
}
