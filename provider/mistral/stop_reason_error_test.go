// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
)

func TestConversationStopReasonClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason    string
		class     sigma.ErrorClass
		retryable bool
		code      string
	}{
		// Mistral reports transient server failures as a terminal "error" stop reason.
		{reason: "error", class: sigma.ErrorClassTransient, retryable: true, code: "server_error"},
		{reason: "mystery", class: sigma.ErrorClassUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeMistralSSE(t, w, "event: conversation.response.started\ndata: {\"type\":\"conversation.response.started\"}\n\n"+
					"event: conversation.response.done\ndata: {\"type\":\"conversation.response.done\",\"stop_reason\":\""+tt.reason+"\"}\n\n")
			}))
			t.Cleanup(server.Close)

			providerID := sigma.ProviderID("mistral-stop-reason-test")
			model := mistralTestModel(providerID)
			client := mistralTestClient(t, providerID, model, server.URL)
			_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
			if err == nil {
				t.Fatalf("stop reason %q completed without error", tt.reason)
			}
			got := sigma.ClassifyError(err)
			if got.Class != tt.class || got.RetryHint.Retryable != tt.retryable || got.ProviderCode != tt.code {
				t.Fatalf("classification = %s retryable=%v code=%q, want %s retryable=%v code=%q",
					got.Class, got.RetryHint.Retryable, got.ProviderCode, tt.class, tt.retryable, tt.code)
			}
		})
	}
}
