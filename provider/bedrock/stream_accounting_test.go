// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma"
)

func TestConverseCacheDurationAccounting(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, details string
		long          int
	}{
		{"one hour", `[{"ttl":"ONE_HOUR","inputTokens":1000}]`, 1000},
		{"mixed", `[{"ttl":"FIVE_MINUTES","inputTokens":750},{"ttl":"ONE_HOUR","inputTokens":250}]`, 250},
		{"multiple long entries", `[{"ttl":"ONE_HOUR","inputTokens":200},{"ttl":"ONE_HOUR","inputTokens":300},{"ttl":"ONE_HOUR"},{"ttl":"FIVE_MINUTES","inputTokens":500}]`, 500},
		{"absent", `null`, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var payload map[string]any
			if err := json.Unmarshal([]byte(`{"usage":{"inputTokens":10,"outputTokens":5,"cacheWriteInputTokens":1000,"cacheDetails":`+tt.details+`}}`), &payload); err != nil {
				t.Fatal(err)
			}
			if tt.details == "null" {
				delete(payload["usage"].(map[string]any), "cacheDetails")
			}
			fake := &fakeConverseClient{stream: fakeStream(
				ConverseEvent{Kind: ConverseEventMessageStart, Role: "assistant"},
				ConverseEvent{Kind: ConverseEventMessageStop, StopReason: "end_turn"},
				ConverseEvent{Kind: ConverseEventMetadata, Usage: usageFromPayload(payload)},
			)}
			model := bedrockTestModel("bedrock-cache-duration")
			model.InputCostPerMillion = 3
			model.OutputCostPerMillion = 15
			model.CacheWriteInputCostPerMillion = 3.75
			client := bedrockTestClient(t, model.Provider, model, fake, fakeCredentialDetector{})
			final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
			if err != nil {
				t.Fatal(err)
			}
			if final.Usage == nil || final.Usage.CacheWriteInputTokens != 1000 || final.Usage.LongCacheWriteInputTokens != tt.long {
				t.Fatalf("usage = %#v, want cache write 1000 and long subset %d", final.Usage, tt.long)
			}
			if !reflect.DeepEqual(final.Usage.Raw, payload["usage"]) {
				t.Fatalf("raw usage = %#v", final.Usage.Raw)
			}
			wantWrite := (float64(1000-tt.long)*3.75 + float64(tt.long)*6) / 1e6
			wantTotal := wantWrite + (10*3+5*15)/1e6
			if final.Cost == nil || math.Abs(final.Cost.CacheWriteInputCost-wantWrite) > 1e-12 || math.Abs(final.Cost.TotalCost-wantTotal) > 1e-12 {
				t.Fatalf("cost = %#v, want write %g and total %g", final.Cost, wantWrite, wantTotal)
			}
		})
	}
}

type countingConverseClient struct {
	*fakeConverseClient
	calls int
}

func (c *countingConverseClient) ConverseStream(ctx context.Context, req ConverseRequest) (ConverseStream, error) {
	c.calls++
	return c.fakeConverseClient.ConverseStream(ctx, req)
}

func TestConverseMissingStopReasonRetainsUsageAndErrorPrecedence(t *testing.T) {
	t.Parallel()
	transportErr := errors.New("transport sentinel")
	for _, tt := range []struct {
		name       string
		underlying error
	}{
		{"missing reason", nil}, {"transport error", transportErr}, {"canceled", context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stream := fakeStream(
				ConverseEvent{Kind: ConverseEventMessageStart, Role: "assistant"},
				ConverseEvent{Kind: ConverseEventContentBlockDelta, TextDelta: "partial"},
				ConverseEvent{Kind: ConverseEventMessageStop},
				ConverseEvent{Kind: ConverseEventMetadata, Usage: &ConverseUsage{InputTokens: 7, OutputTokens: 3}},
			)
			stream.err = tt.underlying
			fake := &countingConverseClient{fakeConverseClient: &fakeConverseClient{stream: stream}}
			model := bedrockTestModel("bedrock-missing-reason")
			client := bedrockTestClient(t, model.Provider, model, fake, fakeCredentialDetector{})
			final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}}, sigma.WithMaxRetries(3))
			if tt.underlying != nil {
				if !errors.Is(err, tt.underlying) {
					t.Fatalf("error = %v, want %v", err, tt.underlying)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "bedrock converse stream: stream ended without a stop reason") {
					t.Fatalf("error = %v, want missing reason", err)
				}
				classification := sigma.ClassifyError(err)
				if classification.Class != sigma.ErrorClassTransient || !classification.RetryHint.Retryable {
					t.Fatalf("classification = %#v", classification)
				}
			}
			wantStop := sigma.StopReasonError
			if tt.underlying == context.Canceled {
				wantStop = sigma.StopReasonAborted
			}
			if final.StopReason != wantStop || len(final.Content) != 1 || final.Content[0].Text != "partial" || final.Usage == nil || final.Usage.InputTokens != 7 || final.Usage.OutputTokens != 3 {
				t.Fatalf("partial final = %#v", final)
			}
			if fake.calls != 1 {
				t.Fatalf("calls = %d, want no automatic replay", fake.calls)
			}
		})
	}
}

func TestConverseValidEmptyCompletion(t *testing.T) {
	t.Parallel()
	model := bedrockTestModel("bedrock-empty-completion")
	fake := &fakeConverseClient{stream: fakeStream(ConverseEvent{Kind: ConverseEventMessageStart, Role: "assistant"}, ConverseEvent{Kind: ConverseEventMessageStop, StopReason: "end_turn"})}
	client := bedrockTestClient(t, model.Provider, model, fake, fakeCredentialDetector{})
	final, err := client.Complete(context.Background(), model, sigma.Request{Messages: []sigma.Message{sigma.UserText("hi")}})
	if err != nil || len(final.Content) != 0 || final.StopReason != sigma.StopReasonEndTurn {
		t.Fatalf("final=%#v, error=%v", final, err)
	}
}
