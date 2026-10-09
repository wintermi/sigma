// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wintermi/sigma"
)

func TestHTTPConverseStreamUsesErrorTypeHeaderAsProviderCode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Amzn-Errortype", "ThrottlingException:http://internal.amazon.com/coral/com.amazon.bedrock/")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"Too many tokens, please wait before trying again."}`))
	}))
	t.Cleanup(server.Close)

	ctx := context.Background()
	client, err := newHTTPConverseStreamClient(ctx, Config{Region: "us-east-1", Endpoint: server.URL}, sigma.Options{MaxRetries: new(int)}, CredentialInfo{BearerToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ConverseStream(ctx, ConverseRequest{ModelID: "model"})
	if err == nil {
		t.Fatal("ConverseStream returned nil error")
	}
	classification := sigma.ClassifyError(err)
	if classification.Class != sigma.ErrorClassRateLimited || !classification.RetryHint.Retryable {
		t.Fatalf("classification = %+v, want retryable rate limit", classification)
	}
	if classification.ProviderCode != "ThrottlingException" {
		t.Fatalf("provider code = %q, want ThrottlingException", classification.ProviderCode)
	}
}
