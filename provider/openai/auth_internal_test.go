// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"errors"
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
		for _, mode := range []string{"base URL", "resolved endpoint", "request override", "credential only", "resolver failure"} {
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
					host, tenant, organization = "default.invalid", "", ""
				case "resolver failure":
					resolver.err = errors.New("resolution failed")
				}
				var req *http.Request
				var err error
				switch surface {
				case "chat":
					req, err = NewProvider(WithBaseURL("https://default.invalid/v1")).newRequest(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}, opts)
				case "codex SSE":
					model.API = sigma.APIOpenAICodexResponses
					req, err = NewCodexResponsesProvider(WithBaseURL("https://default.invalid/v1")).newRequest(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("test")}}, opts)
				case "embeddings":
					req, err = NewEmbeddingsProvider(WithBaseURL("https://default.invalid/v1")).newRequest(context.Background(), sigma.EmbeddingModel{ID: "test", Provider: "test", API: sigma.EmbeddingAPIOpenAIEmbeddings}, sigma.EmbeddingRequest{Inputs: []string{"test"}}, opts)
				default:
					input := sigma.ImageRequest{Prompt: "test"}
					imageModel := sigma.ImageModel{ID: "gpt-image-1", Provider: "test", API: sigma.ImageAPIOpenAIImages}
					if surface == "edit" || surface == "variation" {
						input.Inputs = []sigma.ImageInput{{Type: sigma.ImageInputImage, Source: sigma.ImageSourceBase64, MIMEType: "image/png", Data: "aW1hZ2U="}}
						input.Operation = sigma.ImageOperationEdit
					}
					if surface == "variation" {
						input.Prompt = ""
						input.Operation = sigma.ImageOperationVariation
						imageModel.ID = "dall-e-2"
					}
					req, err = NewImagesProvider(WithBaseURL("https://default.invalid/v1")).newRequestWithStream(context.Background(), imageModel, input, opts, surface == "stream images")
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
				if mode == "request override" && !reflect.DeepEqual(opts.Headers, map[string]string{"x-tenant": "request"}) {
					t.Fatal("request headers mutated")
				}
			})
		}
	}
}
