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

// The first poll waits one interval, slow_down adopts the server's interval,
// and every interval is at least one second.
func TestDeviceFlowWaitsFirstAndUsesServerInterval(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	responses := []string{`{"error":"slow_down","interval":7}`, `{"error":"slow_down","interval":0.3}`, `{"access_token":"token"}`}
	client := &http.Client{Transport: slowDownRoundTripper(func(*http.Request) *http.Response {
		mu.Lock()
		defer mu.Unlock()
		body := responses[0]
		responses = responses[1:]
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
	})}

	var waits []time.Duration
	token, err := pollGitHubCopilotDeviceFlowWithWait(context.Background(), client, "github.com",
		githubCopilotDeviceCodeResponse{DeviceCode: "device", Interval: 0.01, ExpiresIn: 60},
		func(_ context.Context, duration time.Duration) error {
			waits = append(waits, duration)
			return nil
		})
	if err != nil || token != "token" {
		t.Fatalf("token = %q, err = %v; want token", token, err)
	}
	want := []time.Duration{time.Second, 7 * time.Second, time.Second}
	if len(waits) != len(want) || waits[0] != want[0] || waits[1] != want[1] || waits[2] != want[2] {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
}
