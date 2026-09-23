// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func TestRegressionResponsesMultipart(t *testing.T) {
	t.Parallel()
	var response responsesResponse
	if err := json.Unmarshal([]byte(`{"id":"resp_audit","status":"completed","output":[{"type":"message","id":"msg_audit","role":"assistant","content":[{"type":"output_text","text":"first "},{"type":"output_text","text":"second"}]}]}`), &response); err != nil {
		t.Fatal(err)
	}
	final, err := parseResponsesObject(context.Background(), response, sigma.Model{ID: "audit", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses}, responsesStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, block := range final.Content {
		text += block.Text
	}
	if text != "first second" {
		t.Fatalf("multipart content lost: got %q, want %q", text, "first second")
	}
}

func TestRegressionEmbeddingMalformedVectors(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"data":[{"index":0}]}`, `{"data":[{"index":0,"embedding":[]}]}`, `{"data":[{"index":0,"embedding":[null,1]}]}`} {
		result, err := decodeEmbeddingsResponse([]byte(body), sigma.EmbeddingModel{ID: "audit"}, 1)
		if err == nil {
			t.Errorf("accepted malformed success %s: vectors=%v", body, result.Vectors)
		}
	}
}

type auditTransport func(*http.Request) (*http.Response, error)

func (f auditTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRegressionEmbeddingTransportErrorRedaction(t *testing.T) {
	t.Parallel()
	model := sigma.EmbeddingModel{ID: "audit", Provider: sigma.ProviderOpenAI, API: sigma.EmbeddingAPIOpenAIEmbeddings}
	registry := sigma.NewRegistry()
	if err := registry.RegisterEmbeddingProvider(model.Provider, NewEmbeddingsProvider()); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterEmbeddingModel(model); err != nil {
		t.Fatal(err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	_, err := client.Embed(context.Background(), model, sigma.EmbeddingQuery("hello"),
		sigma.WithEmbeddingAPIKey("synthetic-key"),
		sigma.WithEmbeddingProviderOption(model.Provider, "endpoint", "https://example.invalid/embeddings?api_key=audit-secret-123"),
		sigma.WithEmbeddingHTTPClient(&http.Client{Transport: auditTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic connection failure") })}),
	)
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "audit-secret-123") {
		t.Fatalf("transport error exposes query credential: %v", err)
	}
}

func TestRegressionOAuthCanceledWaiter(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	provider := NewCodexOAuthTokenProvider(CodexOAuthCredentials{AccessToken: "expired", RefreshToken: "synthetic", AccountID: "audit", Expiry: time.Now().Add(-time.Hour)}, CodexOAuthTokenProviderOptions{
		HTTPClient: &http.Client{Transport: auditTransport(func(*http.Request) (*http.Response, error) {
			startOnce.Do(func() { close(started) })
			<-release
			return nil, errors.New("synthetic refresh failure")
		})},
	})
	ownerDone := make(chan struct{})
	go func() {
		defer close(ownerDone)
		_, _ = provider.Token(context.Background(), sigma.Model{}, sigma.Options{})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	waiterDone := make(chan error, 1)
	go func() { _, err := provider.Token(ctx, sigma.Model{}, sigma.Options{}); waiterDone <- err }()
	blocked := false
	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter error=%v", err)
		}
	case <-time.After(5 * time.Second):
		blocked = true
	}
	unblock()
	<-ownerDone
	if blocked {
		<-waiterDone
		t.Fatal("already-canceled caller remained blocked behind another request's OAuth refresh")
	}
}

type auditWriter struct{}

func (auditWriter) Emit(context.Context, sigma.Event) error                    { return nil }
func (auditWriter) Done(context.Context, sigma.AssistantMessage) error         { return nil }
func (auditWriter) Error(context.Context, error, sigma.AssistantMessage) error { return nil }
func (auditWriter) Close()                                                     {}

func TestRegressionResponsesStreamingMultipart(t *testing.T) {
	t.Parallel()
	wire := "data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"first \"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":1,\"delta\":\"second\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"first \"},{\"type\":\"output_text\",\"text\":\"second\"}]}]}}\n\n"
	final, err := parseResponsesStream(context.Background(), strings.NewReader(wire), auditWriter{}, sigma.Model{ID: "audit", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses}, responsesStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, block := range final.Content {
		text += block.Text
	}
	if text != "first second" {
		t.Fatalf("streamed multipart content lost: got %q", text)
	}
}
