// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/wintermi/sigma"
)

const jsonTextCase = "json_text"

func jsonTextProbeCase(route routeSpec) probeCase {
	return singleTurnCase(jsonTextCase, "plain-text JSON with local validation", basicRequest(`Return JSON exactly {"ok":true}.`), []sigma.Option{
		sigma.WithProviderOption(route.Provider, "extra_body", map[string]any{"response_format": map[string]any{jsonTypeKey: "text"}}),
		sigma.WithMaxTokens(256),
	})
}

func validateJSONTextProbe(message sigma.AssistantMessage) error {
	if message.StopReason != sigma.StopReasonEndTurn && message.StopReason != sigma.StopReasonStopSequence {
		return errors.New("JSON text fallback did not complete normally")
	}
	var text strings.Builder
	for _, block := range message.Content {
		if block.Type == sigma.ContentBlockText {
			text.WriteString(block.Text)
		}
	}
	var compact bytes.Buffer
	// Validate the exact probe answer, allowing whitespace but not prose,
	// Markdown fences, duplicate keys, extra fields, or trailing JSON values.
	if err := json.Compact(&compact, []byte(text.String())); err != nil || compact.String() != `{"ok":true}` {
		return errors.New(`JSON text fallback did not return the expected {"ok":true} object`)
	}
	return nil
}
