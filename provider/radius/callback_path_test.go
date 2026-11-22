// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package radius

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// Paths that collide with or break ServeMux patterns must not panic the
// process after the listener is open; the callback still completes.
func TestRadiusCallbackServerAcceptsAnyValidRedirectPath(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/", "/call back", "/{code}"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()
			redirect := (&url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), Path: path}).String()
			validated, err := validRadiusOAuthRedirectURI(redirect)
			if err != nil {
				t.Fatalf("redirect %q rejected: %v", redirect, err)
			}
			server, err := startRadiusBrowserCallbackServer(validated, "state-value")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(server.close)
			callback, _ := url.Parse(validated)
			callback.RawQuery = url.Values{"code": {"code-value"}, "state": {"state-value"}}.Encode()
			response, err := http.Get(callback.String())
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			select {
			case result := <-server.done:
				if result.err != nil || result.code != "code-value" {
					t.Fatalf("result = %+v, want code-value", result)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("callback did not complete")
			}
		})
	}
}
