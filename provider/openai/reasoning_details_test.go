// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

func TestReasoningDetailIdentityBoundaries(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"reasoning.text", "reasoning.summary"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			field := "text"
			if kind == "reasoning.summary" {
				field = "summary"
			}
			for _, key := range []string{"id", "format", "signature", "index"} {
				t.Run(key, func(t *testing.T) {
					t.Parallel()
					var left, right any = "first", "second"
					if key == "index" {
						left, right = float64(0), float64(1)
					}
					current := map[string]any{"type": kind, field: "a", key: left}
					next := map[string]any{"type": kind, field: "b", key: right}
					got := appendReasoningDetail([]any{current}, next)
					if len(got) != 2 || current[field] != "a" || current[key] != left {
						t.Fatalf("conflicting identity merged: %#v", got)
					}
				})
			}
			first := map[string]any{"type": kind, field: "a"}
			second := map[string]any{"type": kind, field: "b", "id": "r1", "format": "v1", "index": float64(0), "signature": "s1"}
			got := appendReasoningDetail([]any{first}, second)
			got = appendReasoningDetail(got, map[string]any{"type": kind, field: "c", "id": "r1", "index": float64(0)})
			if len(got) != 1 || first[field] != "abc" || first["id"] != "r1" || first["index"] != float64(0) {
				t.Fatalf("compatible fragments failed to merge: %#v", got)
			}
		})
	}
	encrypted := map[string]any{"type": "reasoning.encrypted", "data": "opaque"}
	first := map[string]any{"type": "reasoning.text", "text": "a"}
	got := appendReasoningDetail([]any{first}, encrypted)
	got = appendReasoningDetail(got, encrypted)
	got = appendReasoningDetail(got, map[string]any{"type": "reasoning.text", "text": "b"})
	if len(got) != 4 || first["text"] != "a" {
		t.Fatalf("encrypted or nonadjacent entries merged: %#v", got)
	}
}

func TestReasoningReplayProvenanceAndPersistence(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAICompletions, ID: "target", SupportsTools: true}
	for _, representation := range []string{"modern", "legacy", "modern-precedence", "legacy-fallback"} {
		for _, provenance := range []string{"matching", "provider-mismatch", "api-mismatch", "model-mismatch", "provider-missing", "api-missing", "model-missing", "absent"} {
			t.Run(representation+"/"+provenance, func(t *testing.T) {
				t.Parallel()
				modern := []any{map[string]any{"type": "reasoning.text", "text": "private", "id": "r1"}}
				legacy := []any{map[string]any{"type": "reasoning.encrypted", "data": "opaque"}}
				text := sigma.Text("answer")
				call := sigma.ToolCallBlock("call_1", "read", map[string]any{})
				want := legacy
				if representation != "legacy" {
					text.ProviderMetadata = map[string]any{orderedReasoningDetailsMetadataKey: modern}
					want = modern
				}
				if representation != "modern" {
					call.ProviderMetadata = map[string]any{"reasoning_details": legacy}
				}
				if representation == "legacy-fallback" {
					text.ProviderMetadata[orderedReasoningDetailsMetadataKey] = []any{map[string]any{"type": "reasoning.text", "text": 42}}
					want = legacy
				}
				message := sigma.Message{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, Content: []sigma.ContentBlock{text, call}}
				switch provenance {
				case "provider-mismatch":
					message.Provider = sigma.ProviderOpenRouter
				case "api-mismatch":
					message.API = sigma.APIOpenAIResponses
				case "model-mismatch":
					message.Model = "other"
				case "provider-missing":
					message.Provider = ""
				case "api-missing":
					message.API = ""
				case "model-missing":
					message.Model = ""
				case "absent":
					message.Provider, message.API, message.Model = "", "", ""
				}
				req := sigma.Request{Messages: []sigma.Message{message, sigma.ToolResult("call_1", "contents")}}
				marshal, unmarshal := sigma.MarshalRequest, sigma.UnmarshalRequest
				if provenance == "provider-missing" {
					// Persistence already rejects this partial provenance combination; test
					// the outbound gate independently without relaxing that storage rule.
					marshal = func(req sigma.Request) ([]byte, error) { return json.Marshal(req) }
					unmarshal = func(data []byte) (sigma.Request, error) {
						var req sigma.Request
						err := json.Unmarshal(data, &req)
						return req, err
					}
				}
				persisted, err := marshal(req)
				if err != nil {
					t.Fatal(err)
				}
				restored, err := unmarshal(persisted)
				if err != nil {
					t.Fatal(err)
				}
				payload, err := chatCompletionsPayload(model, restored, sigma.Options{}, openAICompletionsCompat(model, DefaultBaseURL))
				if err != nil {
					t.Fatal(err)
				}
				messages := payload["messages"].([]map[string]any)
				assistant := messages[0]
				got := assistant["reasoning_details"]
				if provenance == "matching" {
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("replayed %#v, want %#v", got, want)
					}
				} else if got != nil {
					t.Fatalf("untrusted reasoning replayed: %#v", got)
				}
				if assistant["content"] != "answer" || len(assistant["tool_calls"].([]map[string]any)) != 1 || messages[1]["content"] != "contents" {
					t.Fatalf("ordinary history lost: %#v", messages)
				}
				after, err := marshal(restored)
				if err != nil || string(after) != string(persisted) {
					t.Fatalf("persisted history changed: %s / %v", after, err)
				}
			})
		}
	}
	if sameOpenAICompletionsProvenance(sigma.Model{}, sigma.Message{}) {
		t.Fatal("empty provenance matched")
	}
}
