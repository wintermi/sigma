// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openrouter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

type imageResponseTransport func(*http.Request) (*http.Response, error)

func (f imageResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func imageResponseOptions() sigma.Options {
	return sigma.Options{AuthResolver: sigma.AuthResolverFunc(func(context.Context, sigma.Model, sigma.Options) (sigma.Credential, error) {
		return sigma.Credential{Value: "synthetic"}, nil
	})}
}

func TestImagesAcceptLargePNGAndMultipleImages(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 1536, 1024))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Uint32())
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	data := base64.StdEncoding.EncodeToString(encoded.Bytes())
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			t.Parallel()
			images := make([]any, count)
			for i := range images {
				images[i] = map[string]any{"image_url": map[string]any{"url": "data:image/png;base64," + data}}
			}
			body, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"images": images}}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(body) <= 8<<20 {
				t.Fatal("fixture no longer exceeds old limit")
			}
			provider := NewImagesProvider(WithImagesHTTPClient(&http.Client{Transport: imageResponseTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
			})}))
			model := sigma.ImageModel{ID: "google/gemini-2.5-flash-image", Provider: sigma.ProviderOpenRouter, API: sigma.ImageAPIOpenRouterImages}
			result, err := provider.Generate(context.Background(), model, sigma.ImageRequest{Prompt: "noise", Count: count, Size: "1536x1024"}, imageResponseOptions())
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Images) != count {
				t.Fatalf("images = %d, want %d", len(result.Images), count)
			}
			for _, output := range result.Images {
				if output.Data != data {
					t.Fatal("image data was truncated")
				}
			}
		})
	}
}

// A repeated byte reader exercises the real boundary without retaining another
// 64 MiB fixture alongside the adapter's response buffer.
type imagePaddingReader struct{}

func (imagePaddingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

type imageFailureReader struct{ err error }

func (r imageFailureReader) Read([]byte) (int, error) { return 0, r.err }

func TestImagesResponseBoundaries(t *testing.T) {
	t.Parallel()
	// Keep large-buffer cases sequential to bound race-test memory usage.
	for _, tt := range []struct {
		name    string
		size    int64
		invalid bool
	}{
		{"exact limit", maxImageResponseBytes, false},
		{"overflow", maxImageResponseBytes + 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prefix := `{"choices":[{"finish_reason":"stop","message":{"images":[{"image_url":{"url":"https://example.com/image.png"}}]}}]}`
			body := io.MultiReader(strings.NewReader(prefix), io.LimitReader(imagePaddingReader{}, tt.size-int64(len(prefix))))
			calls := 0
			provider := NewImagesProvider(WithImagesHTTPClient(&http.Client{Transport: imageResponseTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": []string{"req_limit"}}, Body: io.NopCloser(body), Request: req}, nil
			})}))
			model := sigma.ImageModel{ID: "image-test", Provider: sigma.ProviderOpenRouter, API: sigma.ImageAPIOpenRouterImages}
			opts := imageResponseOptions()
			sigma.WithMaxRetries(2)(&opts)
			result, err := provider.Generate(context.Background(), model, sigma.ImageRequest{Prompt: "image"}, opts)
			if tt.invalid {
				var pe *sigma.ProviderError
				if !errors.As(err, &pe) || pe.StatusCode != 200 || pe.RequestID != "req_limit" || !strings.Contains(err.Error(), "64 MiB") || strings.Contains(err.Error(), "example.com") {
					t.Fatalf("overflow error = %#v / %v", pe, err)
				}
				if result.StopReason != sigma.StopReasonError {
					t.Fatalf("stop reason = %s", result.StopReason)
				}
			} else if err != nil || len(result.Images) != 1 {
				t.Fatalf("exact-limit result = %#v / %v", result, err)
			}
			if calls != 1 {
				t.Fatalf("consumed body retried %d times", calls)
			}
		})
	}
}

func TestImagesResponseReadFailures(t *testing.T) {
	t.Parallel()
	failure := errors.New("body read failed")
	for _, tt := range []struct {
		name    string
		body    io.Reader
		status  int
		want    error
		aborted bool
	}{
		{name: "malformed", body: strings.NewReader(`{"choices":`), status: 200},
		{name: "read failure", body: imageFailureReader{failure}, status: 200, want: failure},
		{name: "cancellation", body: imageFailureReader{context.Canceled}, status: 200, want: context.Canceled, aborted: true},
		{name: "timeout", body: imageFailureReader{context.DeadlineExceeded}, status: 200, want: context.DeadlineExceeded, aborted: true},
		{name: "provider error", body: strings.NewReader(`{"error":{"message":"Authorization: Bearer sk-secret-image-token"}}`), status: 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			provider := NewImagesProvider(WithImagesHTTPClient(&http.Client{Transport: imageResponseTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Header: http.Header{"X-Request-Id": []string{"req_failure"}}, Body: io.NopCloser(tt.body), Request: req}, nil
			})}))
			model := sigma.ImageModel{ID: "image-test", Provider: sigma.ProviderOpenRouter, API: sigma.ImageAPIOpenRouterImages}
			result, err := provider.Generate(context.Background(), model, sigma.ImageRequest{Prompt: "image"}, imageResponseOptions())
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			wantStop := sigma.StopReasonError
			if tt.aborted {
				wantStop = sigma.StopReasonAborted
			}
			if result.StopReason != wantStop {
				t.Fatalf("stop = %s, want %s", result.StopReason, wantStop)
			}
			if tt.status == 400 {
				var pe *sigma.ProviderError
				if !errors.As(err, &pe) || pe.StatusCode != 400 || pe.RequestID != "req_failure" || strings.Contains(err.Error(), "sk-secret-image-token") {
					t.Fatalf("unsafe provider error = %v", err)
				}
			}
		})
	}
}

func TestImagesErrorBodyLimitUnchanged(t *testing.T) {
	t.Parallel()
	body, err := readImageResponse(&http.Response{StatusCode: 400, Body: io.NopCloser(io.LimitReader(imagePaddingReader{}, 9<<20))})
	if err != nil || len(body) != 8<<20 {
		t.Fatalf("error body bytes = %d, error = %v", len(body), err)
	}
}
