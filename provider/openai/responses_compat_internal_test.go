// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

func TestResponsesLongCacheCompatibility(t *testing.T) {
	t.Parallel()
	for _, capable := range []bool{false, true} {
		for _, tt := range []struct {
			name                  string
			retention             sigma.CacheRetention
			unsupported           bool
			typed                 string
			sampling, extra, want map[string]any
		}{
			{name: "unset"},
			{name: "short", retention: sigma.CacheRetentionShort},
			{name: "none", retention: sigma.CacheRetentionNone},
			{name: "long", retention: sigma.CacheRetentionLong},
			{name: "unsupported", retention: sigma.CacheRetentionLong, unsupported: true},
			{name: "typed", retention: sigma.CacheRetentionLong, typed: "24h", want: map[string]any{"prompt_cache_retention": "24h"}},
			{name: "unsupported typed", retention: sigma.CacheRetentionLong, typed: "24h", unsupported: true},
			{
				name:      "sampling retention",
				retention: sigma.CacheRetentionLong,
				sampling:  map[string]any{"prompt_cache_retention": "caller"},
				want:      map[string]any{"prompt_cache_retention": "caller"},
			},
			{
				name:      "sampling options",
				retention: sigma.CacheRetentionLong,
				sampling:  map[string]any{"prompt_cache_options": map[string]any{"mode": "caller"}},
				want:      map[string]any{"prompt_cache_options": map[string]any{"mode": "caller"}},
			},
			{name: "sampling null", retention: sigma.CacheRetentionLong, sampling: map[string]any{"prompt_cache_retention": nil}, want: map[string]any{"prompt_cache_retention": nil}},
			{
				name:      "raw retention",
				retention: sigma.CacheRetentionLong,
				extra:     map[string]any{"prompt_cache_retention": "caller"},
				want:      map[string]any{"prompt_cache_retention": "caller"},
			},
			{
				name:      "raw options",
				retention: sigma.CacheRetentionLong,
				extra:     map[string]any{"prompt_cache_options": map[string]any{"mode": "caller"}},
				want:      map[string]any{"prompt_cache_options": map[string]any{"mode": "caller"}},
			},
			{name: "raw null", retention: sigma.CacheRetentionLong, extra: map[string]any{"prompt_cache_options": nil}, want: map[string]any{"prompt_cache_options": nil}},
			{
				name:      "raw replaces sampling object",
				retention: sigma.CacheRetentionLong,
				sampling:  map[string]any{"prompt_cache_options": map[string]any{"ttl": "caller"}},
				extra:     map[string]any{"prompt_cache_options": map[string]any{"mode": "raw"}},
				want:      map[string]any{"prompt_cache_options": map[string]any{"mode": "raw"}},
			},
			{
				name:      "caller conflict",
				retention: sigma.CacheRetentionLong,
				sampling:  map[string]any{"prompt_cache_retention": "caller"},
				extra:     map[string]any{"prompt_cache_options": nil},
				want:      map[string]any{"prompt_cache_retention": "caller", "prompt_cache_options": nil},
			},
			{
				name:        "unsupported raw",
				retention:   sigma.CacheRetentionLong,
				unsupported: true,
				extra:       map[string]any{"prompt_cache_options": nil},
				want:        map[string]any{"prompt_cache_options": nil},
			},
		} {
			name := "legacy/" + tt.name
			if capable {
				name = "cache options/" + tt.name
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				model := sigma.Model{
					ID: "custom", Provider: sigma.ProviderCustom, API: sigma.APIOpenAIResponses,
					OpenAIResponsesCompat: &sigma.OpenAIResponsesCompat{SupportsExplicitPromptCacheMode: capable},
				}
				if tt.unsupported {
					model.OpenAIResponsesCompat.SupportsLongCacheRetention = sigma.OpenAICompatUnsupported
				}
				opts := sigma.Options{
					CacheRetention: tt.retention, SessionID: "session",
					OpenAIOptions:   &sigma.OpenAIOptions{PromptCacheRetention: tt.typed, SamplingParameters: tt.sampling},
					ProviderOptions: map[sigma.ProviderID]map[string]any{sigma.ProviderCustom: {"extra_body": tt.extra}},
				}
				payload, err := responsesPayload(model, sigma.Request{}, opts)
				if err != nil {
					t.Fatal(err)
				}
				got := map[string]any{}
				for _, key := range []string{"prompt_cache_retention", "prompt_cache_options"} {
					if value, ok := payload[key]; ok {
						got[key] = value
					}
				}
				want := tt.want
				if want == nil {
					want = map[string]any{}
					if tt.name == "long" {
						if capable {
							want["prompt_cache_options"] = map[string]any{"ttl": "30m"}
						} else {
							want["prompt_cache_retention"] = "24h"
						}
					} else if tt.retention == sigma.CacheRetentionNone && capable {
						want["prompt_cache_options"] = map[string]any{"mode": "explicit"}
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("cache payload = %#v, want %#v", got, want)
				}
				if tt.retention == sigma.CacheRetentionLong || tt.retention == sigma.CacheRetentionShort {
					if payload["prompt_cache_key"] != "session" {
						t.Fatalf("cache key = %#v", payload["prompt_cache_key"])
					}
				}
			})
		}
	}
}

func TestResponsesMaxOutputTokensCompatibility(t *testing.T) {
	t.Parallel()
	for _, support := range []sigma.OpenAICompatSupport{"", sigma.OpenAICompatSupported, sigma.OpenAICompatUnsupported} {
		name := string(support)
		if name == "" {
			name = "unspecified"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, tt := range []struct {
				name  string
				limit *int
				want  any
			}{
				{name: "absent"},
				{name: "zero", limit: intPtr(0), want: 16},
				{name: "subminimum", limit: intPtr(1), want: 16},
				{name: "minimum", limit: intPtr(16), want: 16},
				{name: "normal", limit: intPtr(128), want: 128},
			} {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					model := sigma.Model{
						ID: "custom", Provider: sigma.ProviderCustom, API: sigma.APIOpenAIResponses,
						OpenAIResponsesCompat: &sigma.OpenAIResponsesCompat{SupportsMaxOutputTokens: support},
					}
					payload, err := responsesPayload(model, sigma.Request{}, sigma.Options{MaxTokens: tt.limit})
					if err != nil {
						t.Fatal(err)
					}
					want := tt.want
					if support == sigma.OpenAICompatUnsupported {
						want = nil
					}
					got, present := payload["max_output_tokens"]
					if got != want || present != (want != nil) {
						t.Fatalf("max_output_tokens = %v, present %t; want %v", got, present, want)
					}
				})
			}
		})
	}
}

func TestResponsesUnsupportedMaxOutputTokensPreservesExplicitFields(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                      string
		defaults, sampling, extra map[string]any
		want                      any
	}{
		{name: "model defaults", defaults: map[string]any{"max_output_tokens": 32}, want: 32},
		{name: "request sampling", defaults: map[string]any{"max_output_tokens": 32}, sampling: map[string]any{"max_output_tokens": 8}, want: 8},
		{name: "raw body", sampling: map[string]any{"max_output_tokens": 8}, extra: map[string]any{"max_output_tokens": 4}, want: 4},
		{name: "raw null", extra: map[string]any{"max_output_tokens": nil}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := sigma.Model{
				ID: "custom", Provider: sigma.ProviderCustom, API: sigma.APIOpenAIResponses,
				OpenAIResponsesCompat: &sigma.OpenAIResponsesCompat{SupportsMaxOutputTokens: sigma.OpenAICompatUnsupported},
				ProviderMetadata:      map[string]any{sigma.MetadataOpenAISamplingParameters: tt.defaults},
			}
			payload, err := responsesPayload(model, sigma.Request{}, sigma.Options{
				MaxTokens:       intPtr(128),
				OpenAIOptions:   &sigma.OpenAIOptions{SamplingParameters: tt.sampling},
				ProviderOptions: map[sigma.ProviderID]map[string]any{sigma.ProviderCustom: {"extra_body": tt.extra}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got, present := payload["max_output_tokens"]; !present || got != tt.want {
				t.Fatalf("max_output_tokens = %v, present %t; want %v", got, present, tt.want)
			}
			normalizeCodexResponsesPayload(payload)
			if _, present := payload["max_output_tokens"]; present {
				t.Fatal("Codex retained max_output_tokens")
			}
		})
	}
}
