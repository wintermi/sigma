// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/provider/openrouter"
)

func TestTextDebugHooksRunBeforeSendWithRedactedCopies(t *testing.T) {
	var (
		mu    sync.Mutex
		order []string
	)
	appendOrder := func(value string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, value)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appendOrder("server")
		if got, want := r.Header.Get("Authorization"), "Bearer sk-proj-requestsecret"; got != want {
			t.Errorf("authorization header sent to server = %q, want %q", got, want)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll request body returned error: %v", err)
		}
		if strings.Contains(string(body), "mutated") {
			t.Errorf("request body was mutated by debug hook: %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Set-Cookie", "session=secret")
		w.Header().Set("X-Request-ID", "req sk-proj-secret123")
		writeOpenAIStream(t, w)
	}))
	t.Cleanup(server.Close)

	client, model := debugTextClient(t, server.URL)
	var firstPayload sigma.TextPayloadDebug
	var firstPayloadAuthorization string
	var firstPayloadPreview string
	var firstPayloadBody string
	var secondPayload sigma.TextPayloadDebug
	var responseDebug sigma.TextResponseDebug

	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("debug sk-proj-promptsecret")},
	},
		sigma.WithTextPayloadDebugHook(func(_ context.Context, debug sigma.TextPayloadDebug) error {
			appendOrder("payload")
			firstPayload = debug
			firstPayloadAuthorization = debug.Headers.Get("Authorization")
			firstPayloadPreview = debug.PayloadPreview
			firstPayloadBody = string(debug.Payload)
			debug.Payload = []byte(`{"mutated":true}`)
			debug.Headers.Set("Authorization", "mutated")
			return nil
		}),
		sigma.WithTextPayloadDebugHook(func(_ context.Context, debug sigma.TextPayloadDebug) error {
			secondPayload = debug
			return nil
		}),
		sigma.WithTextResponseDebugHook(func(_ context.Context, debug sigma.TextResponseDebug) error {
			appendOrder("response")
			responseDebug = debug
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if got, want := order, []string{"payload", "server", "response"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %#v, want %#v", got, want)
	}
	if firstPayload.Provider != model.Provider || firstPayload.API != sigma.APIOpenAICompletions || firstPayload.Model != model.ID {
		t.Fatalf("payload metadata = %#v, want provider/api/model from request", firstPayload)
	}
	if got := firstPayloadAuthorization; got != "[redacted]" {
		t.Fatalf("payload authorization header = %q, want redacted", got)
	}
	if strings.Contains(firstPayloadPreview, "sk-proj-promptsecret") || strings.Contains(firstPayloadBody, "sk-proj-promptsecret") {
		t.Fatalf("payload debug view leaked prompt secret: preview=%q payload=%s", firstPayloadPreview, firstPayloadBody)
	}
	if got := secondPayload.Headers.Get("Authorization"); got != "[redacted]" {
		t.Fatalf("second payload authorization header = %q, want redacted copy", got)
	}
	if strings.Contains(string(secondPayload.Payload), "mutated") {
		t.Fatalf("second payload hook observed mutation from first hook: %s", secondPayload.Payload)
	}
	if responseDebug.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want 200", responseDebug.StatusCode)
	}
	if got := responseDebug.Headers.Get("Set-Cookie"); got != "[redacted]" {
		t.Fatalf("response Set-Cookie = %q, want redacted", got)
	}
	if strings.Contains(responseDebug.RequestID, "sk-proj-secret123") {
		t.Fatalf("response request id leaked secret: %q", responseDebug.RequestID)
	}
}

func TestTextPayloadHookErrorStopsBeforeNetworkSend(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		atomic.AddInt32(&requests, 1)
	}))
	t.Cleanup(server.Close)

	client, model := debugTextClient(t, server.URL)
	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("hello")},
	}, sigma.WithTextPayloadDebugHook(func(context.Context, sigma.TextPayloadDebug) error {
		return errors.New("debug hook saw sk-proj-hooksecret")
	}))
	if err == nil {
		t.Fatal("Complete returned nil error, want debug hook error")
	}
	if atomic.LoadInt32(&requests) != 0 {
		t.Fatalf("server requests = %d, want 0", requests)
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorDebugHook || !errors.Is(err, sigma.ErrDebugHook) {
		t.Fatalf("error = %#v, want sigma debug hook error", err)
	}
	if strings.Contains(err.Error(), "sk-proj-hooksecret") {
		t.Fatalf("error leaked hook secret: %v", err)
	}
}

func TestTextResponseHookErrorIsTypedAfterResponse(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIStream(t, w)
	}))
	t.Cleanup(server.Close)

	client, model := debugTextClient(t, server.URL)
	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("hello")},
	}, sigma.WithTextResponseDebugHook(func(context.Context, sigma.TextResponseDebug) error {
		return errors.New("response hook saw sk-proj-responsehook")
	}))
	if err == nil {
		t.Fatal("Complete returned nil error, want debug hook error")
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("server requests = %d, want 1", got)
	}
	var sigmaErr *sigma.Error
	if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorDebugHook || !errors.Is(err, sigma.ErrDebugHook) {
		t.Fatalf("error = %#v, want sigma debug hook error", err)
	}
	if strings.Contains(err.Error(), "sk-proj-responsehook") {
		t.Fatalf("error leaked hook secret: %v", err)
	}
}

func TestTextDebugHooksRunOncePerHTTPAttempt(t *testing.T) {
	var (
		mu       sync.Mutex
		statuses []int
		requests int32
		payloads int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := atomic.AddInt32(&requests, 1)
		if attempt == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"retry"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIStream(t, w)
	}))
	t.Cleanup(server.Close)

	client, model := debugTextClient(t, server.URL)
	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("hello")},
	},
		sigma.WithMaxRetries(1),
		sigma.WithMaxRetryDelay(0),
		sigma.WithTextPayloadDebugHook(func(context.Context, sigma.TextPayloadDebug) error {
			atomic.AddInt32(&payloads, 1)
			return nil
		}),
		sigma.WithTextResponseDebugHook(func(_ context.Context, debug sigma.TextResponseDebug) error {
			mu.Lock()
			defer mu.Unlock()
			statuses = append(statuses, debug.StatusCode)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := atomic.LoadInt32(&requests), int32(2); got != want {
		t.Fatalf("server requests = %d, want %d", got, want)
	}
	if got, want := atomic.LoadInt32(&payloads), int32(2); got != want {
		t.Fatalf("payload hook calls = %d, want %d", got, want)
	}
	if got, want := statuses, []int{http.StatusTooManyRequests, http.StatusOK}; !reflect.DeepEqual(got, want) {
		t.Fatalf("response statuses = %#v, want %#v", got, want)
	}
}

func TestImageDebugHooksAreSeparateFromTextHooks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer sk-proj-imagekey"; got != want {
			t.Errorf("authorization header sent to server = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "image=secret")
		_, _ = io.WriteString(w, `{
			"id": "gen_debug",
			"choices": [{
				"finish_reason": "stop",
				"message": {
					"images": [{
						"type": "image_url",
						"image_url": {"url": "data:image/png;base64,aW1hZ2U="}
					}]
				}
			}]
		}`)
	}))
	t.Cleanup(server.Close)

	var textCalls int32
	var imagePayload sigma.ImagePayloadDebug
	var imageResponse sigma.ImageResponseDebug
	client, model := debugImageClient(t, server.URL,
		sigma.WithDefaultOptions(sigma.WithTextPayloadDebugHook(func(context.Context, sigma.TextPayloadDebug) error {
			atomic.AddInt32(&textCalls, 1)
			return nil
		})),
	)

	_, err := client.GenerateImages(context.Background(), model, sigma.ImageRequest{
		Prompt: "image sk-proj-promptsecret",
	}, sigma.WithImageAPIKey("sk-proj-imagekey"),
		sigma.WithImagePayloadDebugHook(func(_ context.Context, debug sigma.ImagePayloadDebug) error {
			imagePayload = debug
			return nil
		}),
		sigma.WithImageResponseDebugHook(func(_ context.Context, debug sigma.ImageResponseDebug) error {
			imageResponse = debug
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	if got := atomic.LoadInt32(&textCalls); got != 0 {
		t.Fatalf("text hook calls during image request = %d, want 0", got)
	}
	if imagePayload.Provider != sigma.ProviderOpenRouter || imagePayload.API != sigma.ImageAPIOpenRouterImages || imagePayload.Model != model.ID {
		t.Fatalf("image payload metadata = %#v, want openrouter image metadata", imagePayload)
	}
	if got := imagePayload.Headers.Get("Authorization"); got != "[redacted]" {
		t.Fatalf("image authorization header = %q, want redacted", got)
	}
	if strings.Contains(imagePayload.PayloadPreview, "sk-proj-promptsecret") {
		t.Fatalf("image payload preview leaked secret: %q", imagePayload.PayloadPreview)
	}
	if imageResponse.StatusCode != http.StatusOK {
		t.Fatalf("image response status = %d, want 200", imageResponse.StatusCode)
	}
	if got := imageResponse.Headers.Get("Set-Cookie"); got != "[redacted]" {
		t.Fatalf("image response Set-Cookie = %q, want redacted", got)
	}
}

func ExampleWithTextPayloadDebugHook() {
	_ = sigma.WithTextPayloadDebugHook(func(_ context.Context, debug sigma.TextPayloadDebug) error {
		_ = debug.PayloadPreview
		return nil
	})
}

func debugTextClient(t *testing.T, baseURL string) (*sigma.Client, sigma.Model) {
	t.Helper()

	model := sigma.Model{
		Provider: sigma.ProviderOpenAI,
		ID:       "debug-text-model",
		API:      sigma.APIOpenAICompletions,
	}
	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(model.Provider, openai.NewProvider(openai.WithBaseURL(baseURL))); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithAuthResolver(sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "sk-proj-requestsecret"}, nil
		})),
	)
	return client, model
}

func debugImageClient(t *testing.T, baseURL string, opts ...sigma.ClientOption) (*sigma.Client, sigma.ImageModel) {
	t.Helper()

	model := sigma.ImageModel{
		Provider: sigma.ProviderOpenRouter,
		ID:       "debug-image-model",
		API:      sigma.ImageAPIOpenRouterImages,
	}
	registry := sigma.NewRegistry()
	if err := registry.RegisterImageProvider(model.Provider, openrouter.NewImagesProvider(openrouter.WithImagesBaseURL(baseURL))); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	if err := registry.RegisterImageModel(model); err != nil {
		t.Fatalf("RegisterImageModel returned error: %v", err)
	}
	clientOpts := append([]sigma.ClientOption{sigma.WithRegistry(registry)}, opts...)
	return sigma.NewClient(clientOpts...), model
}

func writeOpenAIStream(t *testing.T, w io.Writer) {
	t.Helper()
	_, err := fmt.Fprint(w, ""+
		"data: {\"id\":\"chatcmpl_debug\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"+
		"data: {\"id\":\"chatcmpl_debug\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
		"data: [DONE]\n\n")
	if err != nil {
		t.Fatalf("write stream returned error: %v", err)
	}
}
