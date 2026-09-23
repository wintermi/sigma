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
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/openai"
	"github.com/wintermi/sigma/provider/openrouter"
)

type diagnosticBody struct{ err error }

func (b diagnosticBody) Read([]byte) (int, error) { return 0, b.err }
func (diagnosticBody) Close() error               { return nil }

func TestSynchronousAdaptersRedactBodyAndTransportErrors(t *testing.T) {
	t.Parallel()
	type operation struct {
		name string
		call func(sigma.Options) error
	}
	var operations []operation
	for _, tt := range embeddingWireCases() {
		operations = append(operations, operation{tt.name, func(opts sigma.Options) error {
			_, err := tt.provider.Embed(context.Background(), tt.model, sigma.EmbeddingQuery("input"), opts)
			return err
		}})
	}
	for _, tt := range []struct {
		name     string
		provider sigma.ImageProvider
		id       sigma.ProviderID
	}{
		{"openai images", openai.NewImagesProvider(), sigma.ProviderOpenAI},
		{"gemini images", google.NewImagesProvider(), sigma.ProviderGoogle},
		{"vertex images", google.NewVertexImagesProvider(google.WithVertexConfig(google.VertexConfig{ProjectID: "test", Location: "global", CredentialMode: google.VertexCredentialAPIKey})), sigma.ProviderGoogleVertex},
		{"openrouter images", openrouter.NewImagesProvider(), sigma.ProviderOpenRouter},
	} {
		operations = append(operations, operation{tt.name, func(opts sigma.Options) error {
			_, err := tt.provider.Generate(context.Background(), sigma.ImageModel{ID: "test", Provider: tt.id, API: tt.provider.API()}, sigma.ImageRequest{Prompt: "input"}, opts)
			return err
		}})
	}
	deferred := openai.NewResponsesProvider()
	model := sigma.Model{ID: "test", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses}
	handle := sigma.DeferredResponseHandle{ID: "response", Model: model.ID, Provider: model.Provider, API: model.API}
	operations = append(operations,
		operation{"deferred submit", func(opts sigma.Options) error {
			_, err := deferred.SubmitDeferred(context.Background(), model, sigma.Request{}, opts)
			return err
		}},
		operation{"deferred fetch", func(opts sigma.Options) error {
			_, err := deferred.FetchDeferred(context.Background(), model, handle, opts)
			return err
		}},
		operation{"deferred cancel", func(opts sigma.Options) error {
			_, err := deferred.CancelDeferred(context.Background(), model, handle, opts)
			return err
		}},
	)
	for _, op := range operations {
		for _, stage := range []string{"transport", "body", "auth"} {
			if stage == "auth" && (op.name == "titan" || op.name == "nova" || strings.HasPrefix(op.name, "cohere")) {
				continue
			}
			t.Run(op.name+"/"+stage, func(t *testing.T) {
				t.Parallel()
				cause := &url.Error{Op: "read", URL: "https://example.invalid/?access_token=synthetic-secret&X-Amz-Signature=signed-secret", Err: syscall.ECONNRESET}
				requests := 0
				opts := sigma.Options{APIKey: "synthetic", AuthResolver: sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
					return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "synthetic"}, nil
				}), HTTPClient: &http.Client{Transport: ownershipTransport(func(*http.Request) (*http.Response, error) {
					requests++
					if stage == "transport" {
						return nil, cause
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: diagnosticBody{cause}}, nil
				})}}
				if stage == "auth" {
					opts.APIKey = ""
					opts.AuthResolver = sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
						return sigma.Credential{}, cause
					})
				}
				err := op.call(opts)
				assertRedactedCause(t, err, cause)
				if (stage == "auth" && requests != 0) || (stage != "auth" && requests != 1) {
					t.Fatalf("unexpected HTTP calls: %d", requests)
				}
			})
		}
	}
}

func assertRedactedCause(t *testing.T, err, errorCause error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, format := range []string{"%s", "%v", "%+v", "%#v", "%q"} {
		if got := fmt.Sprintf(format, err); strings.Contains(got, "synthetic-secret") || strings.Contains(got, "signed-secret") {
			t.Fatalf("credential leaked: %s", got)
		}
	}
	var network *url.Error
	if !errors.As(err, &network) || !errors.Is(err, errorCause) {
		t.Fatalf("cause lost: %v", err)
	}
	var response *sigma.ProviderError
	if errors.As(err, &response) {
		t.Fatalf("manufactured response error: %v", err)
	}
}

func TestRetryBoundaryRedactsWithoutChangingAttempts(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"request", "transport", "hook"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			cause := &url.Error{Op: "request", URL: "https://example.invalid/?api_key=synthetic-secret", Err: syscall.ECONNRESET}
			calls := 0
			retries := 1
			delay := time.Duration(0)
			client := &http.Client{Transport: ownershipTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if stage == "transport" {
					return nil, cause
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			resp, attempts, err := sigma.DoHTTPWithRetryAttempts(context.Background(), client, sigma.Options{MaxRetries: &retries, MaxRetryDelay: &delay}, func(ctx context.Context) (*http.Request, error) {
				if stage == "request" {
					return nil, cause
				}
				return http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid", nil)
			}, nil, func(*http.Response) error {
				if stage == "hook" {
					return cause
				}
				return nil
			})
			assertRedactedCause(t, err, cause)
			want := 0
			if stage == "transport" {
				want = 2
			}
			if stage == "hook" {
				want = 1
			}
			if resp != nil || calls != want || len(attempts) != want {
				t.Fatalf("calls=%d attempts=%v response=%v", calls, attempts, resp)
			}
		})
	}
}
