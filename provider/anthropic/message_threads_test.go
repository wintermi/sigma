// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wintermi/sigma"
)

type threadServer struct {
	mu        sync.Mutex
	bodies    []map[string]any
	betas     []string
	responses []func(http.ResponseWriter)
}

func (s *threadServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.betas = append(s.betas, r.Header.Get("Anthropic-Beta"))
		respond := func(w http.ResponseWriter) { writeMessagesSSE(t, w, completedEvent) }
		if len(s.responses) > 0 {
			respond, s.responses = s.responses[0], s.responses[1:]
		}
		s.mu.Unlock()
		respond(w)
	}
}

func rejectThread(status int, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func threadTestModel(providerID sigma.ProviderID, enabled bool) sigma.Model {
	model := anthropicTestModel(providerID)
	if enabled {
		model.AnthropicMessagesCompat = &sigma.AnthropicMessagesCompat{SupportsMessageThreads: sigma.AnthropicCompatSupported}
	}
	return model
}

// runTurns sends two turns of one conversation and returns the server.
func runThreadTurns(t *testing.T, model sigma.Model, server *threadServer, session string, opts ...sigma.Option) {
	t.Helper()
	httpServer := httptest.NewServer(server.handler(t))
	t.Cleanup(httpServer.Close)
	client := anthropicTestClient(t, model.Provider, model, httpServer.URL)
	t.Cleanup(func() { _ = sigma.CleanupSessionResources(session) })
	opts = append([]sigma.Option{sigma.WithSessionID(session), sigma.WithCacheRetention(sigma.CacheRetentionShort)}, opts...)
	req := sigma.Request{SystemPrompt: "Be brief.", Messages: []sigma.Message{sigma.UserText("first")}}
	final, err := client.Complete(context.Background(), model, req, opts...)
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	req.Messages = append(req.Messages, sigma.Message{
		Role: sigma.RoleAssistant, Content: final.Content, Provider: final.Provider, API: model.API,
		Model: final.Model, StopReason: final.StopReason, ProviderThinkingLevel: final.ProviderThinkingLevel,
	}, sigma.UserText("second"))
	if _, err := client.Complete(context.Background(), model, req, opts...); err != nil {
		t.Fatalf("second turn: %v", err)
	}
}

func threadField(body map[string]any) map[string]any {
	thread, _ := body["thread"].(map[string]any)
	return thread
}

// With threads enabled, the second turn continues the first reply and sends
// only the new user message, without the system prompt the thread keeps.
func TestMessageThreadsContinueWithOnlyNewMessages(t *testing.T) {
	t.Parallel()
	server := &threadServer{}
	runThreadTurns(t, threadTestModel("anthropic-threads", true), server, "threads-continue")

	if len(server.bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(server.bodies))
	}
	if thread := threadField(server.bodies[0]); thread["type"] != "create" {
		t.Fatalf("first thread = %#v, want create", thread)
	}
	second := server.bodies[1]
	if thread := threadField(second); thread["type"] != "continue" || thread["previous_message_id"] != "msg_complete" {
		t.Fatalf("second thread = %#v, want continue from msg_complete", thread)
	}
	if _, ok := second["system"]; ok {
		t.Fatal("continue resent the system prompt")
	}
	messages, _ := second["messages"].([]any)
	if len(messages) != 1 || !strings.Contains(mustJSON(t, messages[0]), "second") {
		t.Fatalf("continue messages = %s, want only the new user message", mustJSON(t, messages))
	}
	for _, beta := range server.betas {
		if !strings.Contains(beta, "message-threads-2026-08-12") {
			t.Fatalf("anthropic-beta = %q, want the threads beta", beta)
		}
	}
}

// Threads are opt-in: without the compat flag, without a session, or with
// continuation disabled, requests carry no thread.
func TestMessageThreadsOffUnlessEnabled(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		enabled bool
		opts    []sigma.Option
	}{
		{name: "not opted in"},
		{name: "caching off", enabled: true, opts: []sigma.Option{sigma.WithCacheRetention(sigma.CacheRetentionNone)}},
		{name: "continuation disabled", enabled: true, opts: []sigma.Option{sigma.WithProviderOption("anthropic-threads-off", "disable_continuation", true)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := &threadServer{}
			runThreadTurns(t, threadTestModel("anthropic-threads-off", tt.enabled), server, "threads-off-"+tt.name, tt.opts...)
			for index, body := range server.bodies {
				if thread := threadField(body); thread != nil {
					t.Fatalf("request %d thread = %#v, want none", index, thread)
				}
			}
		})
	}
}

// A continue the server cannot use is resent as a new thread; an endpoint
// that does not know threads gets the full request and threads stay off.
func TestMessageThreadsResendRejectedRequests(t *testing.T) {
	t.Parallel()
	t.Run("expired thread", func(t *testing.T) {
		t.Parallel()
		server := &threadServer{responses: []func(http.ResponseWriter){
			func(w http.ResponseWriter) { writeMessagesSSE(t, w, completedEvent) },
			rejectThread(http.StatusBadRequest, `{"type":"error","error":{"type":"invalid_request_error","message":"thread not found","details":{"error_code":"thread_not_found"}}}`),
		}}
		runThreadTurns(t, threadTestModel("anthropic-threads-expired", true), server, "threads-expired")
		if len(server.bodies) != 3 {
			t.Fatalf("requests = %d, want 3", len(server.bodies))
		}
		if thread := threadField(server.bodies[2]); thread["type"] != "create" || server.bodies[2]["system"] == nil {
			t.Fatalf("resend = %#v, want a full new thread", server.bodies[2])
		}
	})
	t.Run("threads unsupported", func(t *testing.T) {
		t.Parallel()
		server := &threadServer{responses: []func(http.ResponseWriter){
			rejectThread(http.StatusBadRequest, `{"type":"error","error":{"type":"invalid_request_error","message":"thread: Extra inputs are not permitted"}}`),
		}}
		runThreadTurns(t, threadTestModel("anthropic-threads-unsupported", true), server, "threads-unsupported")
		if len(server.bodies) != 3 {
			t.Fatalf("requests = %d, want 3", len(server.bodies))
		}
		for index, body := range server.bodies[1:] {
			if thread := threadField(body); thread != nil {
				t.Fatalf("request %d after rejection thread = %#v, want none", index+1, thread)
			}
		}
	})
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
