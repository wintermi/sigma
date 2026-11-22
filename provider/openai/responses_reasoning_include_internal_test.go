// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"testing"

	"github.com/wintermi/sigma"
)

func TestResponsesNonReasoningModelOmitsReasoningInclude(t *testing.T) {
	t.Parallel()
	model := sigma.Model{ID: "gpt-test", Provider: "responses-defaults-test", API: sigma.APIOpenAIResponses}
	payload, err := responsesPayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, sigma.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if include, ok := payload["include"]; ok {
		t.Fatalf("include = %#v, want absent for a model without reasoning", include)
	}
}

// Azure does not document prompt_cache_retention, so it is sent only when the
// deployment's metadata opts in.
func TestAzureLongCacheRetentionRequiresOptIn(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		compat *sigma.OpenAIResponsesCompat
		want   any
	}{
		{name: "unspecified", want: nil},
		{name: "opted in", compat: &sigma.OpenAIResponsesCompat{SupportsLongCacheRetention: sigma.OpenAICompatSupported}, want: "24h"},
	} {
		model := sigma.Model{ID: "gpt-test", Provider: sigma.ProviderAzureOpenAIResponses, API: sigma.APIAzureOpenAIResponses, OpenAIResponsesCompat: tt.compat}
		payload, err := responsesPayload(model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, sigma.Options{CacheRetention: sigma.CacheRetentionLong})
		if err != nil {
			t.Fatal(err)
		}
		if got := payload["prompt_cache_retention"]; got != tt.want {
			t.Errorf("%s: prompt_cache_retention = %#v, want %#v", tt.name, got, tt.want)
		}
	}
}
