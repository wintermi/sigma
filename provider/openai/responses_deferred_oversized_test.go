// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestDeferredResponseRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	// A valid response followed by padding: an unbounded decoder accepts the
	// first value and leaves the padding unread.
	body := io.MultiReader(
		strings.NewReader(`{"id":"resp_1","status":"completed"}`),
		io.LimitReader(oversizedImageBody{}, maxDeferredResponseBytes),
	)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(body)}
	model := sigma.Model{ID: "gpt-test", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses}

	_, err := NewResponsesProvider().decodeDeferredResponse(context.Background(), resp, model, sigma.DeferredResponseHandle{}, sigma.Options{})
	if !errors.Is(err, sigma.ErrProviderResponse) || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("decodeDeferredResponse error = %v, want oversized provider response error", err)
	}
}
