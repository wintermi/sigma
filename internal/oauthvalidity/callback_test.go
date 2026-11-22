// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package oauthvalidity

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// A login completes from inside the callback handler and then closes the
// server; the browser must still receive the whole success page.
func TestCloseCallbackServerFinishesInFlightResponse(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	inside := make(chan struct{})
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inside)
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(w, "Authentication successful")
	})}
	go func() { _ = server.Serve(listener) }()

	type result struct {
		body string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String() + "/callback")
		if err != nil {
			done <- result{err: err}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		done <- result{body: string(body), err: err}
	}()
	<-inside
	CloseCallbackServer(server)
	if got := <-done; got.err != nil || got.body != "Authentication successful" {
		t.Fatalf("response = %q, %v; want the full success page", got.body, got.err)
	}
}
