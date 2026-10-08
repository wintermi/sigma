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

type oversizedImageBody struct{}

func (oversizedImageBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

type oversizedImageTransport struct{}

func (oversizedImageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := io.LimitReader(oversizedImageBody{}, maxImagesResponseBytes+1)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(body), Request: req}, nil
}

func TestImagesRejectOversizedSuccessBody(t *testing.T) {
	t.Parallel()

	model := sigma.ImageModel{ID: "gpt-image-1", Provider: sigma.ProviderOpenAI, API: sigma.ImageAPIOpenAIImages}
	opts := sigma.Options{
		HTTPClient: &http.Client{Transport: oversizedImageTransport{}},
		AuthResolver: sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
			return sigma.Credential{Type: sigma.CredentialTypeAPIKey, Value: "synthetic"}, nil
		}),
	}
	result, err := NewImagesProvider().Generate(context.Background(), model, sigma.ImageRequest{Prompt: "image"}, opts)
	var providerErr *sigma.ProviderError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != http.StatusOK || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("error = %v, want a typed 64 MiB overflow provider error", err)
	}
	if result.StopReason != sigma.StopReasonError {
		t.Fatalf("stop reason = %q, want error", result.StopReason)
	}
}
