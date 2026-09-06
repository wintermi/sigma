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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func TestSecurityDebugHooksReceiveRedactedCopies(t *testing.T) {
	t.Parallel()

	headers := http.Header{
		"Authorization": {"Bearer sk-proj-debugauth123"},
		"X-Api-Key":     {"debug-api-key-secret"},
		"Cookie":        {"session=debug-cookie-secret"},
		"X-Callback":    {"https://example.test/cb?access_token=debug-query-token&X-Amz-Signature=debug-signature"},
	}
	payload := []byte(`{
		"prompt": "use sk-proj-debugprompt123",
		"api_key": "debug-json-api-key",
		"access_token": "debug-access-token",
		"refresh_token": "debug-refresh-token",
		"device_code": "debug-device-code"
	}`)

	var firstPayload string
	var firstPayloadPreview string
	var firstAuthorization string
	var firstAPIKey string
	var firstCookie string
	var firstCallback string
	var second sigma.TextPayloadDebug
	err := sigma.RunTextPayloadDebugHooks(
		context.Background(),
		sigma.Options{
			TextPayloadDebugHooks: []sigma.TextPayloadDebugHook{
				func(_ context.Context, debug sigma.TextPayloadDebug) error {
					firstPayload = string(debug.Payload)
					firstPayloadPreview = debug.PayloadPreview
					firstAuthorization = debug.Headers.Get("Authorization")
					firstAPIKey = debug.Headers.Get("X-Api-Key")
					firstCookie = debug.Headers.Get("Cookie")
					firstCallback = debug.Headers.Get("X-Callback")
					debug.Headers.Set("Authorization", "mutated")
					debug.Payload[0] = '['
					return nil
				},
				func(_ context.Context, debug sigma.TextPayloadDebug) error {
					second = debug
					return nil
				},
			},
		},
		sigma.ProviderOpenAI,
		sigma.APIOpenAICompletions,
		"security-model",
		payload,
		headers,
	)
	if err != nil {
		t.Fatalf("RunTextPayloadDebugHooks returned error: %v", err)
	}

	assertSecurityNoSecrets(t, firstPayloadPreview)
	assertSecurityNoSecrets(t, firstPayload)
	assertSecurityNoSecrets(t, firstCallback)
	if got := firstAuthorization; got != "[redacted]" {
		t.Fatalf("authorization header = %q, want redacted", got)
	}
	if got := firstAPIKey; got != "[redacted]" {
		t.Fatalf("api key header = %q, want redacted", got)
	}
	if got := firstCookie; got != "[redacted]" {
		t.Fatalf("cookie header = %q, want redacted", got)
	}
	if strings.Contains(string(second.Payload), "mutated") || second.Headers.Get("Authorization") == "mutated" {
		t.Fatalf("second hook observed first hook mutation: headers=%v payload=%s", second.Headers, second.Payload)
	}
	if !strings.Contains(string(payload), "sk-proj-debugprompt123") {
		t.Fatalf("source payload was mutated: %s", payload)
	}
	if got := headers.Get("Authorization"); got != "Bearer sk-proj-debugauth123" {
		t.Fatalf("source headers were mutated: Authorization=%q", got)
	}
}

func TestSecurityPersistenceDoesNotSerializeCredentials(t *testing.T) {
	t.Parallel()

	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		Final: sigma.AssistantMessage{Content: []sigma.ContentBlock{sigma.Text("ok")}},
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		t.Fatalf("sigmatest.Registry returned error: %v", err)
	}

	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithDefaultHeaders(map[string]string{
			"Authorization": "Bearer sk-proj-persistheader123",
			"Cookie":        "session=persist-cookie-secret",
		}),
		sigma.WithAuthResolver(sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "sk-proj-persistresolver123"}, nil
		})),
	)
	req := sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}}
	if _, err := client.Complete(context.Background(), sigmatest.TextModel(), req, sigma.WithAPIKey("sk-proj-persistrequest123")); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	capture, ok := provider.LastRequest()
	if !ok {
		t.Fatal("faux provider did not capture request")
	}
	if capture.Options.APIKey != "sk-proj-persistrequest123" {
		t.Fatalf("captured request API key = %q, want request-scoped secret", capture.Options.APIKey)
	}

	data, err := sigma.MarshalRequest(capture.Request)
	if err != nil {
		t.Fatalf("MarshalRequest returned error: %v", err)
	}
	assertSecurityNoSecrets(t, string(data))
	if _, err := sigma.UnmarshalRequest(data); err != nil {
		t.Fatalf("UnmarshalRequest returned error: %v", err)
	}
}

func TestSecurityProviderErrorsRedactPreviewsAndHeaders(t *testing.T) {
	var requestAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session=provider-cookie-secret")
		w.Header().Set("X-Request-ID", "req sk-proj-providerrequestid123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{
			"error": {
				"message": "bad credential",
				"api_key": "provider-json-key",
				"access_token": "provider-access-token",
				"refresh_token": "provider-refresh-token",
				"device_code": "provider-device-code",
				"url": "https://example.test/callback?signature=provider-signature&access_token=provider-query-token"
			}
		}`)
	}))
	t.Cleanup(server.Close)

	client, model := debugTextClient(t, server.URL)
	var requestDebug sigma.TextPayloadDebug
	var responseDebug sigma.TextResponseDebug
	_, err := client.Complete(context.Background(), model, sigma.Request{
		Messages: []sigma.Message{sigma.UserText("prompt sk-proj-providerprompt123")},
	},
		sigma.WithTextPayloadDebugHook(func(_ context.Context, debug sigma.TextPayloadDebug) error {
			requestDebug = debug
			return nil
		}),
		sigma.WithTextResponseDebugHook(func(_ context.Context, debug sigma.TextResponseDebug) error {
			responseDebug = debug
			return nil
		}),
	)
	if err == nil {
		t.Fatal("Complete returned nil error, want provider error")
	}
	if requestAuthorization != "Bearer sk-proj-requestsecret" {
		t.Fatalf("server authorization header = %q, want raw credential sent to provider", requestAuthorization)
	}

	assertSecurityNoSecrets(t, requestDebug.PayloadPreview)
	assertSecurityNoSecrets(t, string(requestDebug.Payload))
	if got := requestDebug.Headers.Get("Authorization"); got != "[redacted]" {
		t.Fatalf("request debug Authorization = %q, want redacted", got)
	}
	if got := responseDebug.Headers.Get("Set-Cookie"); got != "[redacted]" {
		t.Fatalf("response debug Set-Cookie = %q, want redacted", got)
	}
	assertSecurityNoSecrets(t, responseDebug.RequestID)
	assertSecurityNoSecrets(t, err.Error())

	var providerErr *sigma.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error type = %T, want ProviderError", err)
	}
	assertSecurityNoSecrets(t, providerErr.BodyPreview)
	diagnostic := providerErr.Diagnostic()
	assertSecurityNoSecrets(t, diagnostic.BodyPreview)
	assertSecurityNoSecrets(t, diagnostic.RequestID)
	if !errors.Is(err, sigma.ErrProviderResponse) {
		t.Fatalf("error = %v, want ErrProviderResponse", err)
	}
}

func TestSecurityCustomHeadersAreCopiedPerCall(t *testing.T) {
	t.Parallel()

	provider := &mutatingHeaderProvider{}
	registry := sigma.NewRegistry()
	if err := registry.RegisterTextProvider(sigmatest.ProviderID, provider); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(sigmatest.TextModel()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	defaultHeaders := map[string]string{"X-Default": "default"}
	callHeaders := map[string]string{"X-Call": "call"}
	client := sigma.NewClient(
		sigma.WithRegistry(registry),
		sigma.WithDefaultHeaders(defaultHeaders),
	)
	defaultHeaders["X-Default"] = "changed-before-call"

	if _, err := client.Complete(context.Background(), sigmatest.TextModel(), sigma.Request{
		Messages: []sigma.Message{sigma.UserText("hello")},
	}, sigma.WithHeaders(callHeaders)); err != nil {
		t.Fatalf("first Complete returned error: %v", err)
	}
	callHeaders["X-Call"] = "changed-after-call"
	if _, err := client.Complete(context.Background(), sigmatest.TextModel(), sigma.Request{
		Messages: []sigma.Message{sigma.UserText("hello again")},
	}); err != nil {
		t.Fatalf("second Complete returned error: %v", err)
	}

	requests := provider.requests()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	if got := requests[0]["X-Default"]; got != "default" {
		t.Fatalf("first default header = %q, want copied default", got)
	}
	if got := requests[0]["X-Call"]; got != "call" {
		t.Fatalf("first call header = %q, want copied call header", got)
	}
	if got := requests[1]["X-Default"]; got != "default" {
		t.Fatalf("second default header = %q, want original default after provider mutation", got)
	}
	if _, ok := requests[1]["X-Call"]; ok {
		t.Fatalf("second request inherited call-scoped header: %#v", requests[1])
	}
}

func TestSecurityStringAndErrorMethodsRedactSecrets(t *testing.T) {
	t.Parallel()

	values := []string{
		fmt.Sprint(sigma.Credential{
			Type:   sigma.CredentialTypeAPIKey,
			Value:  "sk-proj-credentialstring123",
			Source: "env:OPENAI_API_KEY=sk-proj-credentialsource123",
			Metadata: map[string]any{
				"secret_name": "sk-proj-credentialmetadata123",
			},
		}),
		(&sigma.CredentialUnavailableError{
			Provider: sigma.ProviderOpenAI,
			Model:    "security-model",
			Sources:  []string{"env:OPENAI_API_KEY=sk-proj-unavailablesource123"},
		}).Error(),
		(&sigma.Error{Message: "failed with Authorization: Bearer sk-proj-sigmaerror123"}).Error(),
		sigma.NewProviderError(
			sigma.ProviderOpenAI,
			sigma.APIOpenAIResponses,
			"security-model",
			http.StatusUnauthorized,
			"req sk-proj-providerrequestid123",
			time.Second,
			[]byte(`{"api_key":"provider-error-key","access_token":"provider-error-token"}`),
			errors.New("upstream said bearer sk-proj-providercause123"),
		).String(),
		(&sigma.GenerationError{Err: errors.New("wrapped sk-proj-generationerror123")}).Error(),
	}

	for _, value := range values {
		assertSecurityNoSecrets(t, value)
	}
}

func TestSecurityExamplesAndGoldenFilesDoNotContainCredentials(t *testing.T) {
	t.Parallel()

	credentialPattern := regexp.MustCompile(`(?i)(sk-(?:live|proj)-[A-Za-z0-9_-]{8,}|AIza[0-9A-Za-z_-]{16,}|bearer[ \t]+[A-Za-z0-9._~+/=-]{16,})`)
	for _, root := range []string{"examples", filepath.Join("testdata", "golden")} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if match := credentialPattern.Find(data); len(match) > 0 {
				t.Fatalf("%s contains credential-looking value %q", path, string(match))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir(%s) returned error: %v", root, err)
		}
	}
}

type mutatingHeaderProvider struct {
	mu       sync.Mutex
	captures []map[string]string
}

func (p *mutatingHeaderProvider) API() sigma.API {
	return sigmatest.TextAPI
}

func (p *mutatingHeaderProvider) Stream(ctx context.Context, model sigma.Model, _ sigma.Request, opts sigma.Options) *sigma.Stream {
	p.mu.Lock()
	p.captures = append(p.captures, cloneStringMap(opts.Headers))
	if opts.Headers != nil {
		opts.Headers["X-Default"] = "mutated-by-provider"
		opts.Headers["X-Injected"] = "mutated-by-provider"
	}
	p.mu.Unlock()

	stream, writer := sigma.NewStream(ctx)
	go func() {
		_ = writer.Done(ctx, sigma.AssistantMessage{
			Model:      model.ID,
			Provider:   model.Provider,
			Content:    []sigma.ContentBlock{sigma.Text("ok")},
			StopReason: sigma.StopReasonEndTurn,
		})
	}()
	return stream
}

func (p *mutatingHeaderProvider) requests() []map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()

	captures := make([]map[string]string, len(p.captures))
	for i, capture := range p.captures {
		captures[i] = cloneStringMap(capture)
	}
	return captures
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func assertSecurityNoSecrets(t *testing.T, value string) {
	t.Helper()

	secrets := []string{
		"sk-proj-debugauth123",
		"debug-api-key-secret",
		"debug-cookie-secret",
		"debug-query-token",
		"debug-signature",
		"sk-proj-debugprompt123",
		"debug-json-api-key",
		"debug-access-token",
		"debug-refresh-token",
		"debug-device-code",
		"sk-proj-persistheader123",
		"persist-cookie-secret",
		"sk-proj-persistresolver123",
		"sk-proj-persistrequest123",
		"sk-proj-providerrequestid123",
		"provider-cookie-secret",
		"provider-json-key",
		"provider-access-token",
		"provider-refresh-token",
		"provider-device-code",
		"provider-signature",
		"provider-query-token",
		"sk-proj-providerprompt123",
		"sk-proj-credentialstring123",
		"sk-proj-credentialsource123",
		"sk-proj-credentialmetadata123",
		"sk-proj-unavailablesource123",
		"sk-proj-sigmaerror123",
		"provider-error-key",
		"provider-error-token",
		"sk-proj-providercause123",
		"sk-proj-generationerror123",
	}
	for _, secret := range secrets {
		if strings.Contains(value, secret) {
			t.Fatalf("value leaked %q: %q", secret, value)
		}
	}
}

func TestSecurityProviderErrorRedactsEveryCredentialCutoff(t *testing.T) {
	t.Parallel()
	const value = `synthetic\"credential\\tail`
	const prefix = `{"safe":"context","access_token":"`
	for cut := 0; cut <= len(value); cut++ {
		err := sigma.NewProviderError("test", sigma.APIOpenAICompletions, "test", 401, "", 0, []byte(prefix+value[:cut]), nil)
		for _, output := range []string{err.Error(), err.BodyPreview, err.String()} {
			if !strings.Contains(output, `"access_token":"[redacted]"`) || strings.Contains(output, "synthetic") {
				t.Fatalf("cutoff %d not redacted: %q", cut, output)
			}
		}
	}
}

func TestSecurityProviderErrorKeepsBoundedUTF8Preview(t *testing.T) {
	t.Parallel()
	body := `{"nested":{"access_token":"synthetic\"token"},"safe":"` + strings.Repeat("界", 1000) + `"}`
	providerErr := sigma.NewProviderError("test", sigma.APIOpenAICompletions, "test", 401, "", 0, []byte(body), nil)
	if !utf8.ValidString(providerErr.BodyPreview) || len(providerErr.BodyPreview) > 2051 || !strings.Contains(providerErr.BodyPreview, "界") {
		t.Fatalf("invalid bounded preview: bytes=%d", len(providerErr.BodyPreview))
	}
	for _, output := range []string{providerErr.BodyPreview, providerErr.Error(), providerErr.String()} {
		if strings.Contains(output, "synthetic") || !strings.Contains(output, "[redacted]") {
			t.Fatalf("credential leaked: %q", output)
		}
	}
}
