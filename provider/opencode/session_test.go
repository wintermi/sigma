// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package opencode_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/opencode"
)

func TestOpenCodeSessionHeaders(t *testing.T) {
	t.Parallel()
	for _, wire := range []struct {
		api      sigma.API
		response string
	}{
		{sigma.APIOpenAICompletions, chatCompletedEvent},
		{sigma.APIOpenAIResponses, responsesCompletedEvent},
		{sigma.APIAnthropicMessages, anthropicCompletedEvent},
		{sigma.APIGoogleGenerativeAI, googleCompletedEvent},
	} {
		t.Run(string(wire.api), func(t *testing.T) {
			t.Parallel()
			for _, tt := range []struct {
				name       string
				session    string
				provider   map[string]string
				model      map[string]any
				request    map[string]string
				suppressed []string
				want       string
			}{
				{name: "session without cache", session: "conversation-123", want: "conversation-123"},
				{name: "no session"},
				{name: "provider override", session: "generated", provider: map[string]string{"X-OpenCode-Session": "provider"}, want: "provider"},
				{name: "model override", session: "generated", model: map[string]any{"headers": map[string]any{"X-OpenCode-Session": "model"}}, want: "model"},
				{name: "request override", session: "generated", request: map[string]string{"X-OpenCode-Session": "request"}, want: "request"},
				{name: "suppression", session: "generated", suppressed: []string{"X-OpenCode-Session"}},
			} {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					requests := make(chan capturedRequest, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						captureRequest(t, requests, r)
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, wire.response)
					}))
					t.Cleanup(server.Close)
					provider := opencode.NewProvider(opencode.WithBaseURL(server.URL), opencode.WithHeaders(tt.provider))
					model := sigma.Model{Provider: sigma.ProviderOpenCodeGo, ID: "test-model", API: wire.api, ProviderMetadata: tt.model}
					headers := map[string]string{"X-Keep": "unchanged"}
					for key, value := range tt.request {
						headers[key] = value
					}
					opts := sigma.Options{APIKey: "test-key", SessionID: tt.session, CacheRetention: sigma.CacheRetentionNone, Headers: headers, SuppressedHeaders: tt.suppressed}
					opts.AuthResolver = sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
						return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "test-key"}, nil
					})
					_, err := sigma.Collect(context.Background(), provider.Stream(context.Background(), model,
						sigma.Request{Messages: []sigma.Message{sigma.UserText("Reply with ok.")}}, opts))
					if err != nil {
						t.Fatalf("Collect: %v", err)
					}
					request := receiveRequest(t, requests)
					if got := request.Headers.Get("x-opencode-session"); got != tt.want {
						t.Fatalf("session header = %q, want %q", got, tt.want)
					}
					wantHeaders := map[string]string{"X-Keep": "unchanged"}
					for key, value := range tt.request {
						wantHeaders[key] = value
					}
					if !reflect.DeepEqual(headers, wantHeaders) {
						t.Fatalf("caller headers mutated: %v", headers)
					}
				})
			}
		})
	}
}
