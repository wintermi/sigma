// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

type requestResolutionAuth struct {
	result                sigma.AuthResolution
	err                   error
	richCalls, plainCalls int
}

func (a *requestResolutionAuth) Resolve(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
	a.plainCalls++
	return a.result.Credential, a.err
}

func (a *requestResolutionAuth) ResolveAuthResolution(context.Context, sigma.Model, sigma.Options) (sigma.AuthResolution, error) {
	a.richCalls++
	return a.result, a.err
}

func TestRequestAuthResolutionAcrossBuilders(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{"chat", "images", "stream images", "edit", "variation", "embeddings", "codex SSE"} {
		for _, mode := range []string{"base URL", "resolved endpoint", "request override", "credential only", "resolver failure", "caller camel auth snake", "caller snake auth camel", "caller snake auth snake", "caller camel auth camel", "caller both", "auth both", "top-level base URL", "provider headers", "model string headers", "model any headers", "caller camel top-level"} {
			t.Run(surface+"/"+mode, func(t *testing.T) {
				t.Parallel()
				model := sigma.Model{ID: "test", Provider: "test", API: sigma.APIOpenAICompletions}
				resolver := &requestResolutionAuth{result: sigma.AuthResolution{
					Credential: sigma.Credential{Value: "synthetic", Metadata: map[string]any{"accountID": "account"}},
					BaseURL:    "https://resolved.invalid/v1", Headers: map[string]string{"X-Tenant": "resolved", "X-Remove": "remove"},
					ProviderOptions: map[string]any{"organization": "resolved-org"},
				}}
				opts := sigma.Options{AuthResolver: resolver, SuppressedHeaders: []string{"x-remove"}}
				host, tenant, organization := "resolved.invalid", "resolved", "resolved-org"
				switch mode {
				case "resolved endpoint":
					resolver.result.ProviderOptions["endpoint"] = "https://endpoint.invalid/custom"
					host = "endpoint.invalid"
				case "request override":
					resolver.result.ProviderOptions["endpoint"] = "https://ignored.invalid/custom"
					opts.ProviderOptions = map[sigma.ProviderID]map[string]any{"test": {"endpoint": "https://request.invalid/custom", "organization": "request-org"}}
					opts.Headers = map[string]string{"x-tenant": "request"}
					host, tenant, organization = "request.invalid", "request", "request-org"
				case "credential only":
					opts.AuthResolver = sigma.AuthResolverFunc(resolver.Resolve)
					host, tenant, organization = "default.invalid", "provider last", ""
				case "caller camel auth snake", "caller snake auth camel", "caller snake auth snake", "caller camel auth camel", "caller both", "auth both", "top-level base URL", "caller camel top-level":
					resolver.result.BaseURL = ""
					callerKey, authKey := "baseURL", "base_url"
					if mode == "caller snake auth camel" || mode == "caller snake auth snake" {
						callerKey = "base_url"
					}
					if mode == "caller snake auth camel" || mode == "caller camel auth camel" {
						authKey = "baseURL"
					}
					resolver.result.ProviderOptions[authKey] = "https://resolved.invalid/v1"
					opts.ProviderOptions = map[sigma.ProviderID]map[string]any{"test": {callerKey: "https://request.invalid/v1"}}
					host = "request.invalid"
					if mode == "caller camel top-level" {
						resolver.result.BaseURL = "https://top.invalid/v1"
					}
					if mode == "caller both" {
						opts.ProviderOptions["test"]["base_url"] = "https://request.invalid/v1"
						opts.ProviderOptions["test"]["baseURL"] = "https://ignored.invalid/v1"
					}
					if mode == "auth both" || mode == "top-level base URL" {
						opts.ProviderOptions = nil
						resolver.result.ProviderOptions["baseURL"] = "https://ignored.invalid/v1"
						host = "resolved.invalid"
						if mode == "top-level base URL" {
							resolver.result.BaseURL = "https://top.invalid/v1"
							host = "top.invalid"
						}
					}
				case "provider headers", "model string headers", "model any headers":
					delete(resolver.result.Headers, "X-Tenant")
					tenant = "provider last"
					if mode != "provider headers" {
						var headers any = map[string]string{"X-TENANT": "model loser", "x-tenant": "model last", "X-Nonempty": "keep", "x-nonempty": ""}
						if mode == "model any headers" {
							headers = map[string]any{"X-TENANT": "model loser", "x-tenant": "model last", "X-Invalid": 42, "X-Nonempty": "keep", "x-nonempty": ""}
						}
						model.ProviderMetadata = map[string]any{"headers": headers}
						tenant = "model last"
					}
				case "resolver failure":
					resolver.err = errors.New("resolution failed")
				}
				original := maps.Clone(opts.ProviderOptions["test"])
				var originalHeaders any
				switch headers := model.ProviderMetadata["headers"].(type) {
				case map[string]string:
					originalHeaders = maps.Clone(headers)
				case map[string]any:
					originalHeaders = maps.Clone(headers)
				}
				providerOpts := []ProviderOption{WithBaseURL("https://default.invalid/v1"), WithHeaders(map[string]string{"X-Tenant": "provider first"}), WithHeaders(map[string]string{"X-TENANT": "provider loser", "x-tenant": "provider last"})}
				var req *http.Request
				var err error
				switch surface {
				case "chat":
					req, err = NewProvider(providerOpts...).newRequest(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}, opts)
				case "codex SSE":
					model.API = sigma.APIOpenAICodexResponses
					req, err = NewCodexResponsesProvider(providerOpts...).newRequest(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}, opts)
				case "embeddings":
					req, err = NewEmbeddingsProvider(providerOpts...).newRequest(context.Background(), sigma.EmbeddingModel{ID: "test", Provider: "test", API: sigma.EmbeddingAPIOpenAIEmbeddings, ProviderMetadata: model.ProviderMetadata}, sigma.EmbeddingRequest{Inputs: []string{"test"}}, opts)
				default:
					input := sigma.ImageRequest{Prompt: "test"}
					imageModel := sigma.ImageModel{ID: "gpt-image-1", Provider: "test", API: sigma.ImageAPIOpenAIImages, ProviderMetadata: model.ProviderMetadata}
					if surface == "edit" || surface == "variation" {
						input.Inputs = []sigma.ImageInput{{Type: sigma.ImageInputImage, Source: sigma.ImageSourceBase64, MIMEType: "image/png", Data: "aW1hZ2U="}}
						input.Operation = sigma.ImageOperationEdit
					}
					if surface == "variation" {
						input.Prompt = ""
						input.Operation = sigma.ImageOperationVariation
						imageModel.ID = "dall-e-2"
					}
					req, err = NewImagesProvider(providerOpts...).newRequestWithStream(context.Background(), imageModel, input, opts, surface == "stream images")
				}
				if mode == "resolver failure" {
					if !errors.Is(err, resolver.err) || req != nil {
						t.Fatalf("resolver failure lost: req=%v err=%v", req, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				defer req.Body.Close()
				if req.URL.Host != host || req.Header.Get("X-Tenant") != tenant || req.Header.Get("OpenAI-Organization") != organization || req.Header.Get("X-Remove") != "" {
					t.Fatalf("wrong resolved request: %s %v", req.URL, req.Header)
				}
				if req.Header.Get("Authorization") != "Bearer synthetic" {
					t.Fatal("credential not applied")
				}
				if mode == "credential only" {
					if resolver.plainCalls != 1 || resolver.richCalls != 0 {
						t.Fatal("credential resolved more than once")
					}
				} else if resolver.richCalls != 1 || resolver.plainCalls != 0 {
					t.Fatalf("auth resolution calls rich=%d plain=%d", resolver.richCalls, resolver.plainCalls)
				}
				if !reflect.DeepEqual(originalHeaders, model.ProviderMetadata["headers"]) {
					t.Fatal("model headers mutated")
				}
				if originalHeaders != nil && surface == "embeddings" && req.Header.Get("X-Nonempty") != "keep" {
					t.Fatal("blank model header masked a valid case variant")
				}
				if req.Header.Get("X-Invalid") != "" {
					t.Fatal("non-string model header reached request")
				}
				if !reflect.DeepEqual(original, opts.ProviderOptions["test"]) {
					t.Fatal("caller provider options mutated")
				}
				if mode == "request override" && !reflect.DeepEqual(opts.Headers, map[string]string{"x-tenant": "request"}) {
					t.Fatal("request headers mutated")
				}
			})
		}
	}
}
