// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"net/http"
	"testing"

	"github.com/wintermi/sigma"
)

func TestMetadataHTTPClientBypassesProxy(t *testing.T) {
	t.Parallel()

	client := metadataHTTPClient(sigma.Options{})
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("metadata client uses a proxy, want direct connections to link-local metadata endpoints")
	}
	if client == http.DefaultClient {
		t.Fatal("metadata client is http.DefaultClient")
	}

	custom := &http.Client{}
	if got := metadataHTTPClient(sigma.Options{HTTPClient: custom}); got != custom {
		t.Fatal("metadata client ignored the caller's HTTP client")
	}
}
