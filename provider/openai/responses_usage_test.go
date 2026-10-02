// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestResponsesCacheWriteAccounting(t *testing.T) {
	t.Parallel()
	model, ok := sigma.GetModel(sigma.ProviderOpenAI, "gpt-5.6-sol")
	if !ok {
		t.Fatal("checked-in model missing")
	}
	tests := []struct {
		name, details, tier          string
		input, ordinary, read, write int
		cost                         float64
	}{
		{name: "absent details", input: 10000, ordinary: 10000, cost: 0.04},
		{name: "absent writes", details: `{"cached_tokens":2000}`, input: 10000, ordinary: 8000, read: 2000, cost: 0.0328},
		{name: "zero writes", details: `{"cached_tokens":2000,"cache_write_tokens":0}`, input: 10000, ordinary: 8000, read: 2000, cost: 0.0328},
		{name: "writes only", details: `{"cache_write_tokens":6000}`, input: 10000, ordinary: 4000, write: 6000, cost: 0.046},
		{name: "audit combined", details: `{"cached_tokens":2000,"cache_write_tokens":6000}`, input: 10000, ordinary: 2000, read: 2000, write: 6000, cost: 0.0388},
		{name: "clamped input", details: `{"cached_tokens":2000,"cache_write_tokens":6000}`, input: 1000, read: 2000, write: 6000, cost: 0.0308},
		{name: "flex", tier: "flex", details: `{"cached_tokens":2000,"cache_write_tokens":6000}`, input: 10000, ordinary: 2000, read: 2000, write: 6000, cost: 0.0194},
		{name: "priority", tier: "priority", details: `{"cached_tokens":2000,"cache_write_tokens":6000}`, input: 10000, ordinary: 2000, read: 2000, write: 6000, cost: 0.0776},
		{name: "long context", details: `{"cached_tokens":2000,"cache_write_tokens":6000}`, input: 300000, ordinary: 292000, read: 2000, write: 6000, cost: 2.3976},
	}
	for _, tt := range tests {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tt.name, streaming), func(t *testing.T) {
				t.Parallel()
				usage := fmt.Sprintf(`{"input_tokens":%d,"output_tokens":0,"total_tokens":%d}`, tt.input, tt.input)
				if tt.details != "" {
					usage = strings.TrimSuffix(usage, "}") + `,"input_tokens_details":` + tt.details + `}`
				}
				raw := fmt.Sprintf(`{"id":"resp_usage","status":"completed","output":[],"service_tier":%q,"usage":%s}`, tt.tier, usage)
				options := responsesStreamOptions{applyServiceTierCosts: true}
				var final sigma.AssistantMessage
				var err error
				if streaming {
					final, err = parseResponsesStream(context.Background(), strings.NewReader("data: {\"type\":\"response.completed\",\"response\":"+raw+"}\n\n"), auditWriter{}, model, options)
				} else {
					var response responsesResponse
					if err = json.Unmarshal([]byte(raw), &response); err != nil {
						t.Fatal(err)
					}
					final, err = parseResponsesObject(context.Background(), response, model, options)
				}
				if err != nil {
					t.Fatal(err)
				}
				got := final.Usage
				if got == nil || got.InputTokens != tt.ordinary || got.CacheReadInputTokens != tt.read || got.CacheWriteInputTokens != tt.write || got.TotalTokens != tt.input {
					t.Fatalf("usage = %#v", got)
				}
				if final.Cost == nil || math.Abs(final.Cost.TotalCost-tt.cost) > 1e-10 {
					t.Fatalf("cost = %#v, want %.8f", final.Cost, tt.cost)
				}
				details, _ := got.Raw["input_tokens_details"].(map[string]any)
				value, supplied := details["cache_write_tokens"]
				if supplied != strings.Contains(tt.details, "cache_write_tokens") {
					t.Fatalf("raw field presence changed: %#v", got.Raw)
				}
				if supplied && fmt.Sprint(value) != fmt.Sprint(tt.write) {
					t.Fatalf("raw write count = %v", value)
				}
			})
		}
	}
}
