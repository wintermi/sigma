// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/provider/openai"
)

// failingBody streams a prefix and then fails as a dropped HTTP/2 stream does.
type failingBody struct {
	prefix *strings.Reader
	err    error
}

func (b *failingBody) Read(p []byte) (int, error) {
	if b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	return 0, b.err
}

func (*failingBody) Close() error { return nil }

func TestStreamBodyTransportFailuresClassifyAsTransient(t *testing.T) {
	t.Parallel()
	for _, cause := range []string{
		"stream error: stream ID 1; INTERNAL_ERROR; received from peer",
		"http2: server sent GOAWAY and closed the connection; LastStreamID=1, ErrCode=NO_ERROR, debug=\"\"",
	} {
		t.Run(cause, func(t *testing.T) {
			t.Parallel()
			registry := sigma.NewRegistry()
			model := sigma.Model{ID: "test", Provider: "body-errors", API: sigma.APIOpenAICompletions}
			if err := openai.Register(registry, model.Provider); err != nil {
				t.Fatal(err)
			}
			if err := registry.RegisterModel(model); err != nil {
				t.Fatal(err)
			}
			transport := ownershipTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body: &failingBody{
						prefix: strings.NewReader("data: {\"id\":\"c\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"),
						err:    errors.New(cause),
					},
					Request: r,
				}, nil
			})
			client := sigma.NewClient(sigma.WithRegistry(registry), sigma.WithHTTPClient(&http.Client{Transport: transport}))

			_, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, sigma.WithAPIKey("synthetic"))
			if err == nil {
				t.Fatal("Complete succeeded after a dropped stream")
			}
			if got := sigma.ClassifyError(err); got.Class != sigma.ErrorClassTransient || !got.RetryHint.Retryable {
				t.Fatalf("classification = %+v, want retryable transient (err %v)", got, err)
			}
		})
	}
}
