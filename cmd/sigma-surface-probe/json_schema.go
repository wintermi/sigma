// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/wintermi/sigma"
)

var errJSONSchemaMismatch = errors.New(`json_schema output did not match the requested schema {"answer": string}`)

// validatesJSONSchema reports the attempts that request the probe schema, so
// an upstream that accepts response_format but ignores it is not reported ok.
func validatesJSONSchema(attempt string) bool {
	return attempt == jsonSchemaType || attempt == "json_schema_more_tokens"
}

// validateJSONSchemaProbe checks the reply against the probe schema: a single
// JSON object whose only field is a string "answer".
func validateJSONSchemaProbe(message sigma.AssistantMessage) error {
	if message.StopReason != sigma.StopReasonEndTurn && message.StopReason != sigma.StopReasonStopSequence {
		return errors.New("json_schema output did not complete normally")
	}
	decoder := json.NewDecoder(strings.NewReader(assistantText(message)))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		return errJSONSchemaMismatch
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errJSONSchemaMismatch
	}
	var answer *string
	if len(object) != 1 || json.Unmarshal(object["answer"], &answer) != nil || answer == nil {
		return errJSONSchemaMismatch
	}
	return nil
}
