// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var contentBlockJSONKeys = map[string]struct{}{
	"type":              {},
	"text":              {},
	"thinking":          {},
	"signature":         {},
	"redacted":          {},
	"mimeType":          {},
	"imageSource":       {},
	"documentSource":    {},
	"filename":          {},
	"fileID":            {},
	"data":              {},
	"url":               {},
	"toolCallID":        {},
	"toolName":          {},
	"toolArguments":     {},
	"providerSignature": {},
	"providerMetadata":  {},
}

func (b ContentBlock) MarshalJSON() ([]byte, error) {
	type contentBlockJSON ContentBlock
	data, err := json.Marshal(contentBlockJSON(b))
	if err != nil {
		return nil, err
	}
	if len(b.ExtraFields) == 0 {
		return data, nil
	}

	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range b.ExtraFields {
		if _, exists := fields[key]; !exists {
			fields[key] = value
		}
	}
	return json.Marshal(fields)
}

func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	if b == nil {
		return errors.New("sigma: nil ContentBlock")
	}
	type contentBlockJSON ContentBlock
	var decoded contentBlockJSON
	if err := decodeUseNumber(data, &decoded); err != nil {
		return err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	extras := make(map[string]any)
	for key, raw := range fields {
		if _, known := contentBlockJSONKeys[key]; known {
			continue
		}
		value, err := decodeRawUseNumber(raw)
		if err != nil {
			return err
		}
		extras[key] = value
	}

	*b = ContentBlock(decoded)
	if len(extras) > 0 {
		b.ExtraFields = extras
	}
	return nil
}

func (c *ToolCall) UnmarshalJSON(data []byte) error {
	if c == nil {
		return errors.New("sigma: nil ToolCall")
	}
	type toolCallJSON ToolCall
	var decoded toolCallJSON
	if err := decodeUseNumber(data, &decoded); err != nil {
		return err
	}
	*c = ToolCall(decoded)
	return nil
}

func decodeRawUseNumber(raw json.RawMessage) (any, error) {
	var value any
	if err := decodeUseNumber(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func decodeUseNumber(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
