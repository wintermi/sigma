// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"context"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestAuditGoogleSignedEmptyText(t *testing.T) {
	t.Parallel()
	for _, api := range []sigma.API{sigma.APIGoogleGenerativeAI, sigma.APIGoogleVertex} {
		for _, first := range []string{`{"text":"reasoning","thought":true,"thoughtSignature":"Zmlyc3Q="}`, `{"functionCall":{"id":"call","name":"lookup","args":{}},"thoughtSignature":"Zmlyc3Q="}`} {
			model := sigma.Model{ID: "test", Provider: sigma.ProviderGoogle, API: api}
			if api == sigma.APIGoogleVertex {
				model.Provider = sigma.ProviderGoogleVertex
			}
			ctx := context.Background()
			stream, writer := sigma.NewStream(ctx)
			var events []sigma.Event
			done := make(chan struct{})
			go func() {
				defer close(done)
				for event := range stream.Events() {
					events = append(events, event)
				}
			}()
			final, err := parseGenerativeStream(ctx, strings.NewReader(`data: {"candidates":[{"content":{"parts":[`+first+`,{"text":"","thoughtSignature":"c2Vjb25k"}]},"finishReason":"STOP"}]}`+"\n\n"), writer, model)
			writer.Close()
			<-done
			if err != nil {
				t.Fatal(err)
			}
			if len(final.Content) != 2 || final.Content[0].ProviderSignature != "Zmlyc3Q=" || final.Content[1].Type != sigma.ContentBlockText || final.Content[1].Text != "" || final.Content[1].ProviderSignature != "c2Vjb25k" {
				t.Fatalf("lost signed text: %+v", final.Content)
			}
			started, ended := false, false
			for _, event := range events {
				if event.ContentIndex != nil && *event.ContentIndex == 1 {
					started = started || event.Kind == sigma.EventKindTextStart
					ended = ended || event.Kind == sigma.EventKindTextEnd
				}
			}
			if !started || !ended {
				t.Fatalf("missing text boundaries: %+v", events)
			}
			message := sigma.Message{Role: sigma.RoleAssistant, Provider: model.Provider, API: api, Model: model.ID, Content: final.Content}
			parts, err := googleAssistantParts(model, message, newGoogleToolCallIDNormalizer(model))
			if err != nil || len(parts) != 2 || parts[0]["thoughtSignature"] != "Zmlyc3Q=" || parts[1]["thoughtSignature"] != "c2Vjb25k" || parts[1]["text"] != "" {
				t.Fatalf("replay: %+v (%v)", parts, err)
			}
			message.Model = "another-model"
			parts, err = googleAssistantParts(model, message, newGoogleToolCallIDNormalizer(model))
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range parts {
				if _, ok := part["thoughtSignature"]; ok {
					t.Fatalf("replayed foreign signature: %+v", parts)
				}
			}
			if final.Content[0].ProviderSignature != "Zmlyc3Q=" || final.Content[1].ProviderSignature != "c2Vjb25k" {
				t.Fatal("replay mutated caller history")
			}
		}
	}
}
