// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

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
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/openrouter"
)

type regressionAuth struct {
	resolve               func(context.Context, sigma.Model, sigma.Options) (sigma.AuthResolution, error)
	richCalls, plainCalls int
}

func (a *regressionAuth) Resolve(ctx context.Context, model sigma.Model, opts sigma.Options) (sigma.Credential, error) {
	a.plainCalls++
	result, err := a.resolve(ctx, model, opts)
	return result.Credential, err
}

func (a *regressionAuth) ResolveAuthResolution(ctx context.Context, model sigma.Model, opts sigma.Options) (sigma.AuthResolution, error) {
	a.richCalls++
	return a.resolve(ctx, model, opts)
}

func TestScopedRequestAuthResolution(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{"text", "gemini images", "imagen images", "embeddings", "openrouter images"} {
		for _, mode := range []string{"resolved", "resolved endpoint", "caller overrides", "caller endpoint", "caller baseURL", "caller base_url", "credential only", "OAuth", "empty credential", "protected credential", "failure", "cancellation", "timeout", "retry refresh"} {
			t.Run(surface+"/"+mode, func(t *testing.T) {
				t.Parallel()
				providerID := sigma.ProviderGoogle
				if surface == "openrouter images" {
					providerID = sigma.ProviderOpenRouter
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				failure := errors.New("auth unavailable")
				resolutions, attempts := 0, 0
				resolverDone := make(chan struct{})
				auth := &regressionAuth{}
				auth.resolve = func(ctx context.Context, model sigma.Model, opts sigma.Options) (sigma.AuthResolution, error) {
					resolutions++
					if mode == "cancellation" || mode == "timeout" {
						defer close(resolverDone)
					}
					if model.Provider != providerID {
						t.Errorf("wrong auth model: %#v", model)
					}
					if mode == "failure" {
						return sigma.AuthResolution{}, failure
					}
					if mode == "cancellation" {
						cancel()
						return sigma.AuthResolution{}, ctx.Err()
					}
					if mode == "timeout" {
						<-ctx.Done()
						return sigma.AuthResolution{}, ctx.Err()
					}
					if mode == "retry refresh" && (opts.Headers["X-Tenant"] != "" || opts.ProviderOptions[providerID]["base_url"] != nil) {
						t.Error("retry received stale auth defaults")
					}
					marker := fmt.Sprintf("resolved-%d", resolutions)
					result := sigma.AuthResolution{
						Credential: sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "synthetic-key"},
						BaseURL:    "https://" + marker + ".invalid/v1", Headers: map[string]string{"X-Tenant": marker, "X-Remove": "remove"},
						ProviderOptions: map[string]any{"extra_body": map[string]any{"auth_marker": marker}, "image_size": marker, "negative_prompt": marker, "task_type": marker},
					}
					if mode == "OAuth" {
						result.Credential.Type = sigma.CredentialTypeOAuthToken
					}
					if mode == "empty credential" {
						result.Credential.Value = ""
					}
					if mode == "resolved endpoint" || mode == "caller endpoint" {
						result.ProviderOptions["endpoint"] = "https://endpoint.invalid/custom"
					}
					if mode == "caller baseURL" || mode == "caller base_url" {
						result.BaseURL = ""
						key := "base_url"
						if mode == "caller base_url" {
							key = "baseURL"
						}
						result.ProviderOptions[key] = "https://ignored.invalid/v1"
					}
					return result, nil
				}
				opts := sigma.Options{AuthResolver: auth, Headers: map[string]string{"X-Caller": "keep"}, SuppressedHeaders: []string{"x-remove"}, ProviderOptions: map[sigma.ProviderID]map[string]any{providerID: {}}}
				if mode == "credential only" {
					opts.AuthResolver = sigma.AuthResolverFunc(auth.Resolve)
				}
				if mode == "caller overrides" {
					opts.Headers["x-tenant"] = "caller"
					opts.ProviderOptions[providerID] = map[string]any{"base_url": "https://caller.invalid/v1", "extra_body": map[string]any{"auth_marker": "caller"}, "image_size": "caller", "negative_prompt": "caller", "task_type": "caller"}
				}
				if mode == "caller baseURL" || mode == "caller base_url" {
					opts.ProviderOptions[providerID][strings.TrimPrefix(mode, "caller ")] = "https://caller.invalid/v1"
				}
				if mode == "caller endpoint" {
					opts.ProviderOptions[providerID]["endpoint"] = "https://caller.invalid/exact"
				}
				if mode == "protected credential" {
					opts.SuppressedHeaders = append(opts.SuppressedHeaders, "authorization", "x-goog-api-key")
				}
				if mode == "timeout" {
					timeout := 10 * time.Millisecond
					opts.Timeout = &timeout
				}
				if mode == "retry refresh" {
					retries := 1
					delay := time.Duration(0)
					opts.MaxRetries = &retries
					opts.MaxRetryDelay = &delay
				}
				snapshot := func() string {
					encoded, err := json.Marshal([]any{opts.Headers, opts.ProviderOptions})
					if err != nil {
						t.Fatal(err)
					}
					return string(encoded)
				}
				original := snapshot()
				var debugHeaders http.Header
				captureDebug := func(headers http.Header) {
					debugHeaders = headers.Clone()
					if strings.Contains(fmt.Sprint(headers), "synthetic-key") {
						t.Error("debug hook exposed credential")
					}
					headers.Set("X-Caller", "debug mutation")
				}
				opts.TextPayloadDebugHooks = []sigma.TextPayloadDebugHook{func(_ context.Context, debug sigma.TextPayloadDebug) error { captureDebug(debug.Headers); return nil }}
				opts.ImagePayloadDebugHooks = []sigma.ImagePayloadDebugHook{func(_ context.Context, debug sigma.ImagePayloadDebug) error { captureDebug(debug.Headers); return nil }}
				opts.EmbeddingPayloadDebugHooks = []sigma.EmbeddingPayloadDebugHook{func(_ context.Context, debug sigma.EmbeddingPayloadDebug) error {
					captureDebug(debug.Headers)
					return nil
				}}
				opts.HTTPClient = &http.Client{Transport: regressionTransport(func(req *http.Request) (*http.Response, error) {
					attempts++
					marker := fmt.Sprintf("resolved-%d", attempts)
					host, tenant, payloadMarker := marker+".invalid", marker, marker
					if mode == "credential only" {
						host, tenant, payloadMarker = "generativelanguage.googleapis.com", "", ""
						if surface == "openrouter images" {
							host = "openrouter.ai"
						}
					}
					if mode == "caller overrides" {
						host, tenant, payloadMarker = "caller.invalid", "caller", "caller"
					}
					if mode == "caller baseURL" || mode == "caller base_url" {
						host = "caller.invalid"
					}
					basePath := "/v1"
					if mode == "credential only" {
						basePath = "/v1beta"
						if surface == "openrouter images" {
							basePath = "/api/v1"
						}
					}
					suffix := "/models/test:streamGenerateContent"
					switch surface {
					case "gemini images":
						suffix = "/models/gemini-test:generateContent"
					case "imagen images":
						suffix = "/models/imagen-test:predict"
					case "embeddings":
						suffix = "/models/test:batchEmbedContents"
					case "openrouter images":
						suffix = "/chat/completions"
					}
					path := basePath + suffix
					if mode == "resolved endpoint" {
						host, path = "endpoint.invalid", "/custom"
					}
					if mode == "caller endpoint" {
						host, path = "caller.invalid", "/exact"
					}
					if req.URL.Path != path || req.URL.Scheme != "https" {
						t.Errorf("wrong resolved URL: %s", req.URL)
					}
					if req.URL.Host != host || req.Header.Get("X-Tenant") != tenant || req.Header.Get("X-Caller") != "keep" || req.Header.Get("X-Remove") != "" {
						t.Errorf("wrong resolved request: %s %v", req.URL, req.Header)
					}
					if debugHeaders.Get("X-Tenant") != tenant || debugHeaders.Get("X-Remove") != "" {
						t.Errorf("debug hooks did not see final resolved headers: %v", debugHeaders)
					}
					key, token := "synthetic-key", ""
					if mode == "OAuth" || surface == "openrouter images" {
						key, token = "", "Bearer synthetic-key"
					}
					if mode == "empty credential" {
						key, token = "", ""
					}
					if req.Header.Get("X-Goog-Api-Key") != key || req.Header.Get("Authorization") != token {
						t.Errorf("wrong credential headers: %v", req.Header)
					}
					body, err := io.ReadAll(req.Body)
					if err != nil {
						return nil, err
					}
					var payload struct {
						AuthMarker       string `json:"auth_marker"`
						GenerationConfig struct{ ImageConfig struct{ ImageSize string } }
						Parameters       struct{ NegativePrompt string }
						Requests         []struct{ TaskType string }
					}
					if err := json.Unmarshal(body, &payload); err != nil {
						return nil, err
					}
					gotMarker := payload.AuthMarker
					switch surface {
					case "gemini images":
						gotMarker = payload.GenerationConfig.ImageConfig.ImageSize
					case "imagen images":
						gotMarker = payload.Parameters.NegativePrompt
					case "embeddings":
						if len(payload.Requests) != 1 {
							return nil, fmt.Errorf("unexpected embedding request count: %d", len(payload.Requests))
						}
						gotMarker = payload.Requests[0].TaskType
					}
					if gotMarker != payloadMarker {
						t.Errorf("resolved payload option = %q, want %q: %s", gotMarker, payloadMarker, body)
					}
					responseBody := `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"ok"}]}}]}`
					switch surface {
					case "text":
						responseBody = "data: " + responseBody + "\n\n"
					case "embeddings":
						responseBody = `{"embeddings":[{"values":[1]}]}`
					case "imagen images":
						responseBody = `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U="}]}`
					case "openrouter images":
						responseBody = `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`
					}
					resp := regressionResponse(req, responseBody)
					if mode == "retry refresh" && attempts == 1 {
						resp.StatusCode = http.StatusServiceUnavailable
					}
					return resp, nil
				})}
				var err error
				switch surface {
				case "text":
					model := sigma.Model{ID: "test", Provider: providerID, API: sigma.APIGoogleGenerativeAI}
					_, err = sigma.Collect(ctx, google.NewProvider().Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}, opts))
				case "embeddings":
					_, err = google.NewEmbeddingsProvider().Embed(ctx, sigma.EmbeddingModel{ID: "test", Provider: providerID, API: sigma.EmbeddingAPIGoogleEmbeddings}, sigma.EmbeddingRequest{Inputs: []string{"test"}}, opts)
				default:
					var provider sigma.ImageProvider = google.NewImagesProvider()
					id := sigma.ModelID("gemini-test")
					if surface == "imagen images" {
						id = "imagen-test"
					}
					if surface == "openrouter images" {
						provider = openrouter.NewImagesProvider()
					}
					_, err = provider.Generate(ctx, sigma.ImageModel{ID: id, Provider: providerID, API: provider.API()}, sigma.ImageRequest{Prompt: "test"}, opts)
				}
				if mode == "cancellation" || mode == "timeout" {
					select {
					case <-resolverDone:
					case <-time.After(5 * time.Second):
						t.Fatal("auth resolver did not finish after cancellation")
					}
				}
				wantCalls := 1
				switch mode {
				case "failure":
					if !errors.Is(err, failure) || attempts != 0 {
						t.Fatalf("resolver failure lost: %v attempts=%d", err, attempts)
					}
				case "cancellation", "timeout":
					want := context.Canceled
					if mode == "timeout" {
						want = context.DeadlineExceeded
					}
					if !errors.Is(err, want) || attempts != 0 {
						t.Fatalf("auth cancellation lost: %v attempts=%d", err, attempts)
					}
				default:
					if err != nil {
						t.Fatal(err)
					}
					if mode == "retry refresh" {
						wantCalls = 2
					}
					if attempts != wantCalls {
						t.Fatalf("attempts=%d want=%d", attempts, wantCalls)
					}
				}
				if resolutions != wantCalls {
					t.Errorf("resolutions=%d want=%d", resolutions, wantCalls)
				}
				if mode == "credential only" {
					if auth.plainCalls != wantCalls || auth.richCalls != 0 {
						t.Error("wrong plain resolution count")
					}
				} else if auth.richCalls != wantCalls || auth.plainCalls != 0 {
					t.Errorf("rich=%d plain=%d", auth.richCalls, auth.plainCalls)
				}
				if original != snapshot() {
					t.Error("caller maps mutated")
				}
			})
		}
	}
}
