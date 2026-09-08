// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

type regressionTransport func(*http.Request) (*http.Response, error)

func (f regressionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type regressionAuth struct{}

func (regressionAuth) Resolve(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
	return sigma.Credential{Value: "synthetic-key"}, nil
}

func (regressionAuth) ResolveAuthResolution(context.Context, sigma.Model, sigma.Options) (sigma.AuthResolution, error) {
	return sigma.AuthResolution{Credential: sigma.Credential{Value: "synthetic-key"}, BaseURL: "https://auth-route.invalid/v1", ProviderOptions: map[string]any{"extra_body": map[string]any{"audit_marker": true}}}, nil
}

func TestRegressionDeferredRequestContracts(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"submit", "fetch", "cancel"} {
		t.Run(op, func(t *testing.T) {
			var gotURL string
			var deadline bool
			var marker any
			transport := regressionTransport(func(r *http.Request) (*http.Response, error) {
				gotURL = r.URL.String()
				_, deadline = r.Context().Deadline()
				if r.Body != nil {
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					marker = body["audit_marker"]
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"resp_audit","status":"queued"}`)), Request: r}, nil
			})
			model := responsesTestModel(sigma.ProviderOpenAI)
			registry := sigma.NewRegistry()
			if err := openai.RegisterResponses(registry, model.Provider); err != nil {
				t.Fatal(err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatal(err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry), sigma.WithHTTPClient(&http.Client{Transport: transport}), sigma.WithAuthResolver(regressionAuth{}))
			opts := []sigma.Option{sigma.WithTimeout(time.Second)}
			handle := sigma.DeferredResponseHandle{Provider: model.Provider, Model: model.ID, API: model.API, ID: "resp_audit"}
			var err error
			switch op {
			case "submit":
				_, err = client.SubmitDeferred(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}, opts...)
			case "fetch":
				_, err = client.FetchDeferred(context.Background(), handle, opts...)
			case "cancel":
				_, err = client.CancelDeferred(context.Background(), handle, opts...)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("url=%s deadline=%v auth_payload_marker=%v", gotURL, deadline, marker)
			if !deadline {
				t.Error("configured timeout did not reach HTTP request")
			}
			if !strings.HasPrefix(gotURL, "https://auth-route.invalid/v1/") {
				t.Error("auth-derived route ignored")
			}
			if op == "submit" && marker != true {
				t.Error("auth-derived extra_body ignored")
			}
		})
	}
}

type deferredResolutionFunc func(context.Context) (sigma.AuthResolution, error)

func (f deferredResolutionFunc) Resolve(ctx context.Context, _ sigma.Model, _ sigma.Options) (sigma.Credential, error) {
	r, err := f(ctx)
	return r.Credential, err
}

func (f deferredResolutionFunc) ResolveAuthResolution(ctx context.Context, _ sigma.Model, _ sigma.Options) (sigma.AuthResolution, error) {
	return f(ctx)
}

type deferredBlockingBody struct {
	ctx    context.Context
	closed bool
}

func (b *deferredBlockingBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (b *deferredBlockingBody) Close() error             { b.closed = true; return nil }

func TestDeferredTimeoutCoversEntireOperation(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"submit", "fetch", "cancel"} {
		for _, stage := range []string{"auth", "headers", "body", "retry"} {
			t.Run(op+"/"+stage, func(t *testing.T) {
				t.Parallel()
				model := responsesTestModel(sigma.ProviderOpenAI)
				provider := openai.NewResponsesProvider()
				timeout := 20 * time.Millisecond
				retries := 1
				var body *deferredBlockingBody
				var authContext context.Context
				resolver := deferredResolutionFunc(func(ctx context.Context) (sigma.AuthResolution, error) {
					authContext = ctx
					if stage == "auth" {
						<-ctx.Done()
						return sigma.AuthResolution{}, ctx.Err()
					}
					return sigma.AuthResolution{Credential: sigma.Credential{Value: "synthetic"}}, nil
				})
				transport := regressionTransport(func(r *http.Request) (*http.Response, error) {
					switch stage {
					case "headers":
						<-r.Context().Done()
						return nil, r.Context().Err()
					case "body":
						body = &deferredBlockingBody{ctx: r.Context()}
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
					default:
						return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"1"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"retry"}}`))}, nil
					}
				})
				opts := sigma.Options{Timeout: &timeout, MaxRetries: &retries, AuthResolver: resolver, HTTPClient: &http.Client{Transport: transport}}
				handle := sigma.DeferredResponseHandle{Provider: model.Provider, Model: model.ID, API: model.API, ID: "resp_test"}
				var err error
				switch op {
				case "submit":
					_, err = provider.SubmitDeferred(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}, opts)
				case "fetch":
					_, err = provider.FetchDeferred(context.Background(), model, handle, opts)
				case "cancel":
					_, err = provider.CancelDeferred(context.Background(), model, handle, opts)
				}
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("timeout error = %v", err)
				}
				if authContext == nil || authContext.Err() == nil {
					t.Fatal("operation context was not canceled")
				}
				if body != nil && !body.closed {
					t.Fatal("response body was not closed")
				}
			})
		}
	}
}

func TestDeferredRetriesRebuildResolvedSubmission(t *testing.T) {
	t.Parallel()
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprint(override), func(t *testing.T) {
			t.Parallel()
			resolves, calls := 0, 0
			resolver := deferredResolutionFunc(func(context.Context) (sigma.AuthResolution, error) {
				resolves++
				return sigma.AuthResolution{Credential: sigma.Credential{Value: "synthetic"}, BaseURL: fmt.Sprintf("https://route-%d.invalid/v1", resolves), Headers: map[string]string{"X-Auth": "default", "X-Suppressed": "secret"}, ProviderOptions: map[string]any{"extra_body": map[string]any{"marker": resolves, "service_tier": fmt.Sprintf("tier-%d", resolves)}}}, nil
			})
			transport := regressionTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				wantHost := fmt.Sprintf("route-%d.invalid", calls)
				if override {
					wantHost = "explicit.invalid"
				}
				if r.URL.Host != wantHost {
					t.Errorf("host = %s, want %s", r.URL.Host, wantHost)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				wantMarker := float64(calls)
				if override {
					wantMarker = 99
				}
				if payload["marker"] != wantMarker {
					t.Errorf("payload marker = %v", payload["marker"])
				}
				if r.Header.Get("X-Auth") != "caller" || r.Header.Get("X-Suppressed") != "" {
					t.Errorf("header precedence: %v", r.Header)
				}
				status, body := 200, `{"id":"resp_test","status":"queued"}`
				if calls == 1 {
					status = 503
					body = `{"error":{"message":"retry"}}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			model := responsesTestModel(sigma.ProviderOpenAI)
			retries := 1
			delay := time.Millisecond
			opts := sigma.Options{AuthResolver: resolver, HTTPClient: &http.Client{Transport: transport}, MaxRetries: &retries, MaxRetryDelay: &delay, Headers: map[string]string{"X-Auth": "caller"}}
			sigma.WithSuppressedHeader("X-Suppressed")(&opts)
			if override {
				sigma.WithProviderOptions(model.Provider, map[string]any{"base_url": "https://explicit.invalid/v1", "extra_body": map[string]any{"marker": 99, "service_tier": "explicit"}})(&opts)
			}
			result, err := openai.NewResponsesProvider().SubmitDeferred(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}, opts)
			if err != nil {
				t.Fatal(err)
			}
			if resolves != 2 || calls != 2 {
				t.Fatalf("resolutions %d, calls %d", resolves, calls)
			}
			wantTier := "tier-2"
			if override {
				wantTier = "explicit"
			}
			if result.Handle.ProviderMetadata["request_service_tier"] != wantTier {
				t.Fatalf("successful-attempt metadata = %#v", result.Handle.ProviderMetadata)
			}
			data, err := json.Marshal(result.Handle)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "synthetic") || strings.Contains(string(data), "invalid") {
				t.Fatal("handle retained auth identity")
			}
		})
	}
}
