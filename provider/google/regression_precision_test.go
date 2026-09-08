// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestRegressionGoogleToolArgumentIntegerPrecision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stream, writer := sigma.NewStream(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range stream.Events() {
		}
	}()
	final, err := parseGenerativeStream(ctx, strings.NewReader("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"call1\",\"name\":\"lookup\",\"args\":{\"record_id\":9007199254740993}}}]},\"finishReason\":\"STOP\"}]}\n\n"), writer, sigma.Model{Provider: sigma.ProviderGoogle, ID: "audit"})
	writer.Close()
	<-done
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(final.Content[0].ToolArguments)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"record_id":9007199254740993}` {
		t.Fatalf("provider tool arguments changed to %s", got)
	}
}
