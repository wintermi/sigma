// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package githubcopilot

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type slowDownRoundTripper func(*http.Request) *http.Response

func (f slowDownRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r), nil
}

func TestDeviceFlowSlowDownUsesServerInterval(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var polls []time.Time
	client := &http.Client{Transport: slowDownRoundTripper(func(*http.Request) *http.Response {
		mu.Lock()
		defer mu.Unlock()
		polls = append(polls, time.Now())
		body := `{"error":"slow_down","interval":0.3}`
		if len(polls) > 1 {
			body = `{"access_token":"token"}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
	})}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	token, err := pollGitHubCopilotDeviceFlow(ctx, client, "github.com", githubCopilotDeviceCodeResponse{DeviceCode: "device", Interval: 0.01, ExpiresIn: 60})
	if err != nil {
		t.Fatalf("pollGitHubCopilotDeviceFlow returned error: %v", err)
	}
	if token != "token" {
		t.Fatalf("token = %q, want token", token)
	}
	if gap := polls[1].Sub(polls[0]); gap < 300*time.Millisecond || gap > 2*time.Second {
		t.Fatalf("poll gap after slow_down = %v, want the server's 300ms interval", gap)
	}
}
