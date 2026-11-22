// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
)

// GPT-5.5 and older put dynamically loaded tools in a namespace and reject
// replayed calls without it, so history from another model fails there. They
// declare every tool at the top level; GPT-5.6 and later load them dynamically.
func TestOpenAIToolLoadingByModelGeneration(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		id      sigma.ModelID
		dynamic bool
	}{
		{id: "gpt-5.4"},
		{id: "gpt-5.4-mini"},
		{id: "gpt-5.4-pro"},
		{id: "gpt-5.5"},
		{id: "gpt-5.6-luna", dynamic: true},
	} {
		t.Run(string(tt.id), func(t *testing.T) {
			t.Parallel()
			requests := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captureRequest(t, requests, r)
				writeResponsesSSE(t, w, responsesCompletedEvent)
			}))
			t.Cleanup(server.Close)
			model, ok := sigma.DefaultRegistry().Model(sigma.ProviderOpenAI, tt.id)
			if !ok {
				t.Fatal("missing model")
			}
			model.ProviderMetadata["baseURL"] = server.URL
			client := responsesTestClient(t, sigma.ProviderOpenAI, model, server.URL)
			if _, err := client.Complete(context.Background(), model, deferredToolsRequest()); err != nil {
				t.Fatal(err)
			}
			body := receiveRequest(t, requests).Body
			if tt.dynamic {
				assertAdditionalToolsPayload(t, body)
			} else {
				assertDeferredToolsRemainEager(t, body)
			}
		})
	}
}
