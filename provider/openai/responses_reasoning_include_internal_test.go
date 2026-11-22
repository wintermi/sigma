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
