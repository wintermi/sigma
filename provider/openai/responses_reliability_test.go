// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

type reliabilityEmbeddingTransport func(*http.Request) (*http.Response, error)

func (f reliabilityEmbeddingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRegressionEmbeddingAcceptsSupportedLargeBatchResponse(t *testing.T) {
	model, ok := sigma.GetEmbeddingModel(sigma.ProviderOpenAI, "text-embedding-3-large")
	if !ok {
		t.Fatal("model missing")
	}
	const count = 512
	vector := "[" + strings.Repeat("0.123456789,", model.DefaultDimensions-1) + "0.123456789]"
	var body strings.Builder
	body.WriteString(`{"data":[`)
	for i := range count {
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"index":%d,"embedding":%s}`, i, vector)
	}
	body.WriteString(`],"usage":{"prompt_tokens":512,"total_tokens":512}}`)
	t.Logf("inputs=%d allowed=%d dimensions=%d response_bytes=%d", count, model.MaxBatchInputs, model.DefaultDimensions, body.Len())
	transport := reliabilityEmbeddingTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body.String())), Request: req}, nil
	})
	provider := NewEmbeddingsProvider(WithHTTPClient(&http.Client{Transport: transport}))
	registry := sigma.NewRegistry()
	if err := registry.RegisterEmbeddingProvider(model.Provider, provider); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterEmbeddingModel(model); err != nil {
		t.Fatal(err)
	}
	client := sigma.NewClient(sigma.WithRegistry(registry))
	inputs := make([]string, count)
	for i := range inputs {
		inputs[i] = fmt.Sprintf("input %d", i)
	}
	result, err := client.EmbedBatch(context.Background(), model, sigma.EmbeddingRequest{Inputs: inputs}, sigma.EmbeddingBatchConfig{}, sigma.WithEmbeddingAPIKey("synthetic"))
	if err != nil {
		t.Fatalf("valid successful response rejected: vectors=%d class=%s error=%v", len(result.Embeddings.Vectors), sigma.ClassifyError(err).Class, err)
	}
	if len(result.Embeddings.Vectors) != count {
		t.Fatalf("got %d vectors", len(result.Embeddings.Vectors))
	}
}

func TestRegressionResponsesParallelForeignToolIDs(t *testing.T) {
	t.Parallel()
	target := sigma.Model{Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses, ID: "target", SupportsTools: true}
	req := sigma.Request{Messages: []sigma.Message{{Role: sigma.RoleAssistant, Provider: sigma.ProviderAnthropic, API: sigma.APIAnthropicMessages, Model: "source", Content: []sigma.ContentBlock{
		sigma.ToolCallBlock("tool_a", "lookup", map[string]any{}),
		sigma.ToolCallBlock("tool_b", "lookup", map[string]any{}),
	}}}}
	adapted, err := sigma.TransformRequestForModel(target, req)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := responsesPayload(target, adapted.Request, sigma.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, item := range payload["input"].([]map[string]any) {
		if item["type"] != "function_call" {
			continue
		}
		id, _ := item["id"].(string)
		t.Logf("call_id=%v item_id=%s", item["call_id"], id)
		if id != "" && ids[id] {
			t.Errorf("duplicate item ID %q for distinct calls", id)
		}
		ids[id] = true
	}
}

func TestRegressionResponsesHandoffDropsForeignToolSignature(t *testing.T) {
	t.Parallel()
	target := sigma.Model{Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses, ID: "target", SupportsTools: true}
	block := sigma.ToolCallBlock("google_call", "lookup", map[string]any{})
	block.ProviderSignature = "google-thought-signature"
	req := sigma.Request{Messages: []sigma.Message{{Role: sigma.RoleAssistant, Provider: sigma.ProviderGoogle, API: sigma.APIGoogleGenerativeAI, Model: "source", Content: []sigma.ContentBlock{block}}}}
	adapted, err := sigma.TransformRequestForModel(target, req)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := responsesPayload(target, adapted.Request, sigma.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range payload["input"].([]map[string]any) {
		if value, exists := item["encrypted_content"]; exists {
			t.Errorf("foreign signature replayed as encrypted_content=%v", value)
		}
	}
}

func TestRegressionResponsesDistinctCallIDsRemainDistinct(t *testing.T) {
	t.Parallel()
	target := sigma.Model{Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses, ID: "target", SupportsTools: true}
	req := sigma.Request{Messages: []sigma.Message{{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{
		sigma.ToolCallBlock("call.a", "lookup", map[string]any{}),
		sigma.ToolCallBlock("call/a", "lookup", map[string]any{}),
	}}}}
	payload, err := responsesPayload(target, req, sigma.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, item := range payload["input"].([]map[string]any) {
		if item["type"] != "function_call" {
			continue
		}
		id := item["call_id"].(string)
		if ids[id] {
			t.Errorf("distinct tool IDs collapse to %q", id)
		}
		ids[id] = true
	}
}

func TestResponsesReplayIDsPreserveCallResultIdentity(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses, ID: "test"}
	rawIDs := []string{"call.a", "call/a", "call_a", strings.Repeat("long", 30) + ".", strings.Repeat("long", 30) + "/", "same|fc_one", "same|fc_two", "same"}
	// An existing safe ID must win over a generated hash candidate.
	rawIDs = append(rawIDs, newResponsesIDs(nil).callID(rawIDs[0]))
	message := sigma.Message{Role: sigma.RoleAssistant}
	for i, id := range rawIDs {
		name := "lookup"
		if i%2 == 1 {
			name = "grammar"
		}
		message.Content = append(message.Content, sigma.ToolCallBlock(id, name, map[string]any{"input": "query"}))
	}
	req := sigma.Request{Messages: []sigma.Message{message}}
	for i, id := range rawIDs {
		req.Messages = append(req.Messages, sigma.ToolResult(id, fmt.Sprint(i)))
	}
	before, _ := json.Marshal(req)
	run := func() []map[string]any {
		items, err := responsesInput(model, req, responsesDeferredToolsAdditional, nil, map[string]string{"grammar": "input"}, false)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	items := run()
	seen := map[string]bool{}
	for i := range rawIDs {
		call, result := items[i], items[len(rawIDs)+i]
		id := call["call_id"].(string)
		if seen[id] || !validResponsesCallID(id) {
			t.Errorf("invalid or colliding call ID %q", id)
		}
		seen[id] = true
		if result["call_id"] != id {
			t.Errorf("unmatched result: %v vs %v", call, result)
		}
		if i%2 == 1 && result["type"] != "custom_tool_call_output" {
			t.Errorf("lost grammar lookup: %v", result)
		}
	}
	if items[2]["call_id"] != "call_a" || items[7]["call_id"] != "same" || items[8]["call_id"] != rawIDs[8] {
		t.Fatal("existing safe IDs were displaced")
	}
	again, _ := json.Marshal(run())
	first, _ := json.Marshal(items)
	if string(again) != string(first) {
		t.Fatal("normalization is not deterministic")
	}
	after, _ := json.Marshal(req)
	if string(after) != string(before) {
		t.Fatal("caller history mutated")
	}
}

func TestResponsesGeneratedItemIDsReserveNativeIDs(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses, ID: "test"}
	blocks := []sigma.ContentBlock{sigma.Thinking("first", ""), sigma.Thinking("second", ""), sigma.Text("text"), sigma.ToolCallBlock("a", "lookup", nil), sigma.ToolCallBlock("b", "lookup", nil), sigma.ToolCallBlock("c", "grammar", map[string]any{"input": "x"}), sigma.ToolCallBlock("d", "grammar", map[string]any{"input": "y"})}
	native := []sigma.ContentBlock{sigma.Thinking("native", ""), sigma.Text("native"), sigma.ToolCallBlock("e", "lookup", nil), sigma.ToolCallBlock("f", "grammar", map[string]any{"input": "z"}), sigma.Thinking("native suffix", ""), sigma.Text("native suffix"), sigma.ToolCallBlock("g", "lookup", nil)}
	nativeIDs := []string{"rs_sigma_0_0", "msg_sigma_0_0", "fc_sigma_0_3", "ctc_sigma_0_5", "rs_existing_-", "msg_existing_-", "fc_existing_-"}
	for i := range native {
		native[i].ProviderMetadata = map[string]any{"id": nativeIDs[i]}
	}
	for _, content := range [][]sigma.ContentBlock{blocks, native} {
		for i := range content {
			if content[i].Type == sigma.ContentBlockThinking {
				content[i].ProviderSignature = "encrypted"
			}
		}
	}
	assistant := func(content []sigma.ContentBlock) sigma.Message {
		return sigma.Message{Role: sigma.RoleAssistant, Content: content, Provider: model.Provider, API: model.API, Model: model.ID}
	}
	req := sigma.Request{Messages: []sigma.Message{assistant(blocks), assistant(native)}}
	items, err := responsesInput(model, req, responsesDeferredToolsAdditional, nil, map[string]string{"grammar": "input"}, false)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range items {
		id := item["id"].(string)
		if seen[id] || len(id) > 64 {
			t.Errorf("duplicate/invalid item ID %q", id)
		}
		seen[id] = true
	}
	for _, id := range nativeIDs {
		if !seen[id] {
			t.Errorf("native ID %q not preserved", id)
		}
	}
}

func TestResponsesSignatureReplayRequiresExactProvenance(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses, ID: "test"}
	for _, mismatch := range []string{"matching", "missing", "provider", "api", "model"} {
		t.Run(mismatch, func(t *testing.T) {
			t.Parallel()
			blocks := []sigma.ContentBlock{sigma.Text("visible"), sigma.Thinking("reasoning", ""), sigma.ToolCallBlock("a", "lookup", nil), sigma.ToolCallBlock("b", "grammar", map[string]any{"input": "query"})}
			for i := range blocks {
				blocks[i].Signature = "signature"
				blocks[i].ProviderSignature = "encrypted"
			}
			message := sigma.Message{Role: sigma.RoleAssistant, Provider: model.Provider, API: model.API, Model: model.ID, Content: blocks}
			switch mismatch {
			case "missing":
				message.Provider = ""
				message.API = ""
				message.Model = ""
			case "provider":
				message.Provider = sigma.ProviderGoogle
			case "api":
				message.API = sigma.APIOpenAICompletions
			case "model":
				message.Model = "other"
			}
			req := sigma.Request{Messages: []sigma.Message{message}}
			before, _ := json.Marshal(req)
			items, err := responsesInput(model, req, responsesDeferredToolsAdditional, nil, map[string]string{"grammar": "input"}, false)
			if err != nil {
				t.Fatal(err)
			}
			fields := 0
			for _, item := range items {
				if item["type"] == "message" {
					item = item["content"].([]map[string]any)[0]
				}
				for _, key := range []string{"signature", "encrypted_content"} {
					if _, ok := item[key]; ok {
						fields++
					}
				}
			}
			want := 0
			if mismatch == "matching" {
				want = 5
			}
			if fields != want {
				t.Fatalf("opaque fields=%d want=%d: %v", fields, want, items)
			}
			after, _ := json.Marshal(req)
			if string(before) != string(after) {
				t.Fatal("signatures erased from history")
			}
		})
	}
}

func TestResponsesDeferredIDsAvoidHistoryCollisions(t *testing.T) {
	t.Parallel()
	model := sigma.Model{Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses, ID: "test"}
	raw := "call.lookup"
	reserved := responsesToolSearchCallID(newResponsesIDs(nil).callID(raw), []string{"late"})
	result := sigma.ToolResult(raw, "loaded")
	result.AddedToolNames = []string{"late"}
	req := sigma.Request{Messages: []sigma.Message{{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock(raw, "lookup", nil), sigma.ToolCallBlock(reserved, "other", nil)}}, result, sigma.ToolResult(reserved, "ok")}}
	tools := map[string]sigma.Tool{"late": {Name: "late", InputSchema: sigma.Schema{"type": "object"}}}
	items, err := responsesInput(model, req, responsesDeferredToolsSearch, tools, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if items[0]["call_id"] != items[2]["call_id"] || items[1]["call_id"] != items[5]["call_id"] {
		t.Fatal("history call/result mapping changed")
	}
	search, output := items[3], items[4]
	if search["type"] != "tool_search_call" || output["type"] != "tool_search_output" || search["call_id"] != output["call_id"] || search["call_id"] == reserved {
		t.Fatalf("invalid deferred pairing: %v %v", search, output)
	}
}

func TestResponsesPreservesSafeBoundaryCallIDs(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"_leading", "trailing-", "_", strings.Repeat("x", 64)} {
		ids := newResponsesIDs([]sigma.Message{{Role: sigma.RoleAssistant, Content: []sigma.ContentBlock{sigma.ToolCallBlock(raw, "lookup", nil)}}})
		if got := ids.callID(raw); got != raw {
			t.Fatalf("valid ID %q changed to %q", raw, got)
		}
	}
}
