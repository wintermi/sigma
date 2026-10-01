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
	"github.com/wintermi/sigma/provider/anthropic"
	"github.com/wintermi/sigma/provider/google"
	"github.com/wintermi/sigma/provider/openai"
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
	for _, surface := range []string{"text", "gemini images", "imagen images", "embeddings", "openrouter images", "vertex text", "vertex gemini images", "vertex imagen images", "vertex embeddings", "vertex openai", "vertex anthropic"} {
		for _, mode := range []string{"resolved", "resolved endpoint", "caller overrides", "caller endpoint", "caller baseURL", "caller base_url", "credential only", "OAuth", "empty credential", "protected credential", "failure", "cancellation", "timeout", "retry refresh", "resolved token mode", "caller credential mode"} {
			t.Run(surface+"/"+mode, func(t *testing.T) {
				t.Parallel()
				isVertex := strings.HasPrefix(surface, "vertex ")
				if !isVertex && (mode == "resolved token mode" || mode == "caller credential mode") {
					t.Skip("Vertex credential modes")
				}
				nativeSurface := strings.TrimPrefix(surface, "vertex ")
				providerID := sigma.ProviderGoogle
				if isVertex {
					providerID = sigma.ProviderGoogleVertex
				}
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
					if isVertex {
						result.ProviderOptions["project_id"] = "resolved-project"
					}
					if mode == "resolved token mode" || mode == "caller credential mode" {
						result.ProviderOptions["credential_mode"] = "token"
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
				if mode == "caller credential mode" {
					opts.ProviderOptions[providerID]["credential_mode"] = "api-key"
				}
				if mode == "credential only" {
					opts.AuthResolver = sigma.AuthResolverFunc(auth.Resolve)
				}
				if mode == "caller overrides" {
					opts.Headers["x-tenant"] = "caller"
					opts.ProviderOptions[providerID] = map[string]any{"base_url": "https://caller.invalid/v1", "extra_body": map[string]any{"auth_marker": "caller"}, "image_size": "caller", "negative_prompt": "caller", "task_type": "caller", "project_id": "caller-project"}
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
					if mode == "credential only" && isVertex {
						host = "aiplatform.googleapis.com"
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
					switch nativeSurface {
					case "gemini images":
						suffix = "/models/gemini-test:generateContent"
					case "imagen images":
						suffix = "/models/imagen-test:predict"
					case "embeddings":
						suffix = "/models/test:batchEmbedContents"
					case "openrouter images":
						suffix = "/chat/completions"
					}
					if isVertex {
						if mode == "credential only" {
							basePath = "/v1"
							if nativeSurface == "openai" {
								basePath = "/v1beta1"
							}
						}
						project := "resolved-project"
						if mode == "credential only" {
							project = "project"
						}
						if mode == "caller overrides" {
							project = "caller-project"
						}
						prefix := "/projects/" + project + "/locations/global"
						switch nativeSurface {
						case "openai":
							suffix = prefix + "/endpoints/openapi/chat/completions"
						case "anthropic":
							suffix = prefix + "/publishers/anthropic/models/test:streamRawPredict"
						case "embeddings":
							suffix = prefix + "/publishers/google/models/test:predict"
						default:
							suffix = prefix + "/publishers/google" + suffix
						}
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
						Instances        []struct {
							TaskType string `json:"task_type"`
						}
					}
					if err := json.Unmarshal(body, &payload); err != nil {
						return nil, err
					}
					gotMarker := payload.AuthMarker
					switch nativeSurface {
					case "gemini images":
						gotMarker = payload.GenerationConfig.ImageConfig.ImageSize
					case "imagen images":
						gotMarker = payload.Parameters.NegativePrompt
					case "embeddings":
						if isVertex {
							payload.Requests = nil
							for _, instance := range payload.Instances {
								payload.Requests = append(payload.Requests, struct{ TaskType string }{instance.TaskType})
							}
						}
						if len(payload.Requests) != 1 {
							return nil, fmt.Errorf("unexpected embedding request count: %d", len(payload.Requests))
						}
						gotMarker = payload.Requests[0].TaskType
					}
					if gotMarker != payloadMarker {
						t.Errorf("resolved payload option = %q, want %q: %s", gotMarker, payloadMarker, body)
					}
					responseBody := `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"ok"}]}}]}`
					switch nativeSurface {
					case "text":
						responseBody = "data: " + responseBody + "\n\n"
					case "embeddings":
						responseBody = `{"embeddings":[{"values":[1]}]}`
						if isVertex {
							responseBody = `{"predictions":[{"embeddings":{"values":[1]}}]}`
						}
					case "imagen images":
						responseBody = `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U="}]}`
					case "openrouter images":
						responseBody = `{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`
					}
					if nativeSurface == "openai" {
						responseBody = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
					}
					if nativeSurface == "anthropic" {
						responseBody = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":0}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
					}
					resp := regressionResponse(req, responseBody)
					if mode == "retry refresh" && attempts == 1 {
						resp.StatusCode = http.StatusServiceUnavailable
					}
					return resp, nil
				})}
				var err error
				switch surface {
				case "vertex text", "vertex openai", "vertex anthropic":
					var provider sigma.TextProvider
					switch nativeSurface {
					case "text":
						provider = google.NewVertexProvider(google.WithVertexConfig(google.VertexConfig{ProjectID: "project", Location: "global"}))
					case "openai":
						provider = openai.NewVertexProvider(openai.WithVertexConfig(openai.VertexConfig{ProjectID: "project", Location: "global"}))
					case "anthropic":
						provider = anthropic.NewVertexProvider(anthropic.WithVertexConfig(anthropic.VertexConfig{ProjectID: "project", Location: "global"}))
					}
					model := sigma.Model{ID: "test", Provider: providerID, API: provider.API()}
					_, err = sigma.Collect(ctx, provider.Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}, opts))
				case "vertex embeddings":
					provider := google.NewVertexEmbeddingsProvider(google.WithVertexConfig(google.VertexConfig{ProjectID: "project", Location: "global"}))
					_, err = provider.Embed(ctx, sigma.EmbeddingModel{ID: "test", Provider: providerID, API: provider.API()}, sigma.EmbeddingRequest{Inputs: []string{"test"}}, opts)
				case "text":
					model := sigma.Model{ID: "test", Provider: providerID, API: sigma.APIGoogleGenerativeAI}
					_, err = sigma.Collect(ctx, google.NewProvider().Stream(ctx, model, sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}, opts))
				case "embeddings":
					_, err = google.NewEmbeddingsProvider().Embed(ctx, sigma.EmbeddingModel{ID: "test", Provider: providerID, API: sigma.EmbeddingAPIGoogleEmbeddings}, sigma.EmbeddingRequest{Inputs: []string{"test"}}, opts)
				default:
					var provider sigma.ImageProvider = google.NewImagesProvider()
					if isVertex {
						provider = google.NewVertexImagesProvider(google.WithVertexConfig(google.VertexConfig{ProjectID: "project", Location: "global"}))
					}
					id := sigma.ModelID("gemini-test")
					if nativeSurface == "imagen images" {
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
				case "resolved token mode":
					if !errors.Is(err, sigma.ErrInvalidOptions) || attempts != 0 {
						t.Fatalf("merged mode not validated: %v attempts=%d", err, attempts)
					}
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
					if mode == "empty credential" && isVertex {
						if !errors.Is(err, sigma.ErrCredentialUnavailable) || attempts != 0 {
							t.Fatalf("expected unavailable credential: %v attempts=%d", err, attempts)
						}
						break
					}
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
