// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai_test

import (
	"errors"
	"testing"

	"github.com/wintermi/sigma"
)

// OpenAI documents stream error events with top-level code and message fields.
func TestResponsesTopLevelErrorEventKeepsProviderDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		event    string
		code     string
		class    sigma.ErrorClass
		overflow bool
	}{
		{
			name:     "context overflow",
			event:    `{"type":"error","code":"context_length_exceeded","message":"Your input exceeds the context window of this model.","param":null,"sequence_number":1}`,
			code:     "context_length_exceeded",
			class:    sigma.ErrorClassContextOverflow,
			overflow: true,
		},
		{
			name:  "server error",
			event: `{"type":"error","code":"server_error","message":"The server had an error processing your request.","param":null,"sequence_number":1}`,
			code:  "server_error",
			class: sigma.ErrorClassTransient,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := completeResponsesStream(t, responsesEventStream(
				`{"type":"response.created","response":{"id":"resp_1"}}`,
				tt.event,
			))
			var providerErr *sigma.ProviderError
			if !errors.As(err, &providerErr) {
				t.Fatalf("error = %v, want typed provider error", err)
			}
			if providerErr.ProviderCode != tt.code || providerErr.ProviderMessage == "" {
				t.Fatalf("provider code/message = %q/%q, want %q with the provider message", providerErr.ProviderCode, providerErr.ProviderMessage, tt.code)
			}
			if got := sigma.ClassifyError(err).Class; got != tt.class {
				t.Fatalf("class = %q, want %q", got, tt.class)
			}
			if errors.Is(err, sigma.ErrContextOverflow) != tt.overflow {
				t.Fatalf("errors.Is(ErrContextOverflow) = %v, want %v", !tt.overflow, tt.overflow)
			}
		})
	}
}
