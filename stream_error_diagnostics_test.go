// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"testing"

	"github.com/wintermi/sigma"
)

// Adapters report mid-stream provider errors without attaching diagnostics;
// callers that inspect the final message must still see the overflow.
func TestStreamErrorAttachesProviderDiagnostic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stream, writer := sigma.NewStream(ctx)
	body := []byte(`{"error":{"code":"context_length_exceeded","message":"Your input exceeds the context window of this model."}}`)
	go func() {
		_ = writer.Error(ctx, sigma.NewProviderError("openai", sigma.APIOpenAIResponses, "gpt", 0, "", 0, body, sigma.ErrContextOverflow), sigma.AssistantMessage{StopReason: sigma.StopReasonError})
	}()

	final, err := sigma.Collect(ctx, stream)
	if err == nil {
		t.Fatal("Collect returned nil error")
	}
	if len(final.Diagnostics) != 1 || final.Diagnostics[0].ProviderCode != "context_length_exceeded" {
		t.Fatalf("diagnostics = %+v, want the provider diagnostic", final.Diagnostics)
	}
	if !sigma.IsContextOverflow(final, 0) {
		t.Fatal("IsContextOverflow(final) = false for a mid-stream overflow")
	}
}

func TestStreamErrorKeepsAdapterDiagnostics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stream, writer := sigma.NewStream(ctx)
	adapter := sigma.Diagnostic{Kind: "adapter", Message: "kept"}
	go func() {
		_ = writer.Error(ctx, sigma.NewProviderError("openai", sigma.APIOpenAIResponses, "gpt", 500, "", 0, nil, sigma.ErrProviderResponse), sigma.AssistantMessage{StopReason: sigma.StopReasonError, Diagnostics: []sigma.Diagnostic{adapter}})
	}()

	final, _ := sigma.Collect(ctx, stream)
	if len(final.Diagnostics) != 1 || final.Diagnostics[0] != adapter {
		t.Fatalf("diagnostics = %+v, want only the adapter diagnostic", final.Diagnostics)
	}
}
