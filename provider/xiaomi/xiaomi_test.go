// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package xiaomi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/xiaomi"
)

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

func TestRegistersReportOpenAICompletionsAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider sigma.ProviderID
		register func(*sigma.Registry) error
	}{
		{name: "api billing", provider: sigma.ProviderXiaomi, register: func(registry *sigma.Registry) error {
			return xiaomi.Register(registry)
		}},
		{name: "token plan cn", provider: sigma.ProviderXiaomiTokenPlanCN, register: func(registry *sigma.Registry) error {
			return xiaomi.RegisterTokenPlanCN(registry)
		}},
		{name: "token plan ams", provider: sigma.ProviderXiaomiTokenPlanAMS, register: func(registry *sigma.Registry) error {
			return xiaomi.RegisterTokenPlanAMS(registry)
		}},
		{name: "token plan sgp", provider: sigma.ProviderXiaomiTokenPlanSGP, register: func(registry *sigma.Registry) error {
			return xiaomi.RegisterTokenPlanSGP(registry)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := sigma.NewRegistry()
			if err := tt.register(registry); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			if err := registry.RegisterModel(xiaomiTestModel(tt.provider)); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}

			providers := registry.ListProviders()
			if got, want := providers[0].ID, tt.provider; got != want {
				t.Fatalf("provider ID = %q, want %q", got, want)
			}
			if got, want := providers[0].TextAPI, sigma.APIOpenAICompletions; got != want {
				t.Fatalf("provider API = %q, want %q", got, want)
			}
		})
	}
}

func TestCompleteUsesConfiguredOpenAICompatibleBaseURL(t *testing.T) {
	t.Parallel()

	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(t, requests, r)
		writeCompleted(t, w)
	}))
	t.Cleanup(server.Close)

	model := xiaomiTestModel(sigma.ProviderXiaomiTokenPlanAMS)
	registry := sigma.NewRegistry()
	if err := xiaomi.RegisterTokenPlanAMS(registry, xiaomi.WithBaseURL(server.URL+"/v1")); err != nil {
		t.Fatalf("RegisterTokenPlanAMS returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))

	final, err := client.Complete(
		context.Background(),
		model,
		sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}},
		sigma.WithAPIKey("request-key"),
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Content[0].Text, "ok"; got != want {
		t.Fatalf("final text = %q, want %q", got, want)
	}

	request := receiveRequest(t, requests)
	if got, want := request.Method, http.MethodPost; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := request.Path, "/v1/chat/completions"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := request.Headers.Get("Authorization"), "Bearer request-key"; got != want {
		t.Fatalf("Authorization header = %q, want %q", got, want)
	}
}

func TestRegistersCatalogModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider sigma.ProviderID
		register func(*sigma.Registry) error
	}{
		{name: "api billing", provider: sigma.ProviderXiaomi, register: func(registry *sigma.Registry) error {
			return xiaomi.Register(registry)
		}},
		{name: "cn", provider: sigma.ProviderXiaomiTokenPlanCN, register: func(registry *sigma.Registry) error {
			return xiaomi.RegisterTokenPlanCN(registry)
		}},
		{name: "ams", provider: sigma.ProviderXiaomiTokenPlanAMS, register: func(registry *sigma.Registry) error {
			return xiaomi.RegisterTokenPlanAMS(registry)
		}},
		{name: "sgp", provider: sigma.ProviderXiaomiTokenPlanSGP, register: func(registry *sigma.Registry) error {
			return xiaomi.RegisterTokenPlanSGP(registry)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model, ok := sigma.DefaultRegistry().Model(tt.provider, "mimo-v2.5-pro")
			if !ok {
				t.Fatalf("default registry missing %s mimo-v2.5-pro", tt.provider)
			}
			registry := sigma.NewRegistry()
			if err := tt.register(registry); err != nil {
				t.Fatalf("register returned error: %v", err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}
		})
	}
}

// MiMo documents only the thinking on/off toggle and rejects reasoning_effort
// values such as "minimal", so catalog models expose only off and high and
// never send reasoning_effort.
func TestCatalogModelsSendOnlyThinkingToggle(t *testing.T) {
	t.Parallel()

	providers := []sigma.ProviderID{
		sigma.ProviderXiaomi,
		sigma.ProviderXiaomiTokenPlanCN,
		sigma.ProviderXiaomiTokenPlanAMS,
		sigma.ProviderXiaomiTokenPlanSGP,
	}
	for _, provider := range providers {
		t.Run(string(provider), func(t *testing.T) {
			t.Parallel()

			requests := make(chan capturedRequest, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeCompleted(t, w)
			}))
			t.Cleanup(server.Close)

			model, ok := sigma.DefaultRegistry().Model(provider, "mimo-v2.5-pro")
			if !ok {
				t.Fatalf("default registry missing %s mimo-v2.5-pro", provider)
			}
			// Catalog rows carry their endpoint, which overrides the provider's.
			model.ProviderMetadata["baseURL"] = server.URL + "/v1"
			registry := sigma.NewRegistry()
			if err := registry.RegisterTextProvider(provider, xiaomi.NewProvider(xiaomi.WithBaseURL(server.URL+"/v1"))); err != nil {
				t.Fatalf("RegisterTextProvider returned error: %v", err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatalf("RegisterModel returned error: %v", err)
			}
			client := sigma.NewClient(sigma.WithRegistry(registry))
			request := sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}

			for level, wantThinking := range map[sigma.ThinkingLevel]string{
				sigma.ThinkingLevelHigh: "enabled",
				sigma.ThinkingLevelOff:  "disabled",
			} {
				if _, err := client.Complete(context.Background(), model, request, sigma.WithAPIKey("key"), sigma.WithReasoningLevel(level)); err != nil {
					t.Fatalf("Complete(%s) returned error: %v", level, err)
				}
				var payload map[string]any
				if err := json.Unmarshal(receiveRequest(t, requests).Body, &payload); err != nil {
					t.Fatalf("Unmarshal request body returned error: %v", err)
				}
				if got := payload["thinking"]; !jsonEqual(got, map[string]any{"type": wantThinking}) {
					t.Fatalf("thinking for %s = %#v, want type %q", level, got, wantThinking)
				}
				if effort, ok := payload["reasoning_effort"]; ok {
					t.Fatalf("reasoning_effort for %s = %#v, want absent", level, effort)
				}
			}

			for _, level := range []sigma.ThinkingLevel{sigma.ThinkingLevelMinimal, sigma.ThinkingLevelMedium, sigma.ThinkingLevelXHigh} {
				_, err := client.Complete(context.Background(), model, request, sigma.WithAPIKey("key"), sigma.WithReasoningLevel(level))
				var sigmaErr *sigma.Error
				if !errors.As(err, &sigmaErr) || sigmaErr.Code != sigma.ErrorInvalidOptions {
					t.Fatalf("Complete(%s) error = %v, want invalid options", level, err)
				}
			}
		})
	}
}

func jsonEqual(got any, want map[string]any) bool {
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	return string(gotJSON) == string(wantJSON)
}

func xiaomiTestModel(provider sigma.ProviderID) sigma.Model {
	return sigma.Model{
		ID:              "mimo-v2.5-pro",
		Provider:        provider,
		API:             sigma.APIOpenAICompletions,
		Name:            "MiMo-V2.5-Pro",
		SupportedInputs: []sigma.ContentBlockType{sigma.ContentBlockText},
		SupportsTools:   true,
	}
}

func captureRequest(t *testing.T, ch chan<- capturedRequest, r *http.Request) {
	t.Helper()

	body, _ := io.ReadAll(r.Body)
	ch <- capturedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
		Body:    body,
	}
}

func receiveRequest(t *testing.T, ch <-chan capturedRequest) capturedRequest {
	t.Helper()

	select {
	case request := <-ch:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request")
		return capturedRequest{}
	}
}

func writeCompleted(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"id":"chatcmpl_xiaomi","model":"mimo-v2.5-pro","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`+"\n\n")
	_, _ = io.WriteString(w, `data: {"id":"chatcmpl_xiaomi","model":"mimo-v2.5-pro","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`+"\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}
