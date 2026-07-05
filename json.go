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
	"reflect"
	"strings"
)

// contentBlockJSONKeys is derived from ContentBlock's json struct tags so the
// known-key set cannot drift when fields are added to the struct.
var contentBlockJSONKeys = contentBlockKnownJSONKeys()

func contentBlockKnownJSONKeys() map[string]struct{} {
	blockType := reflect.TypeFor[ContentBlock]()
	keys := make(map[string]struct{}, blockType.NumField())
	for field := range blockType.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		keys[name] = struct{}{}
	}
	return keys
}

// isKnownContentBlockKey matches encoding/json field resolution: an exact tag
// match or a case-insensitive fallback both bind to the struct field.
func isKnownContentBlockKey(key string) bool {
	if _, known := contentBlockJSONKeys[key]; known {
		return true
	}
	for known := range contentBlockJSONKeys {
		if strings.EqualFold(known, key) {
			return true
		}
	}
	return false
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
	if err := decodeUseNumber(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range b.ExtraFields {
		if _, exists := fields[key]; exists || isKnownContentBlockKey(key) {
			continue
		}
		fields[key] = value
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
		if isKnownContentBlockKey(key) {
			continue
		}
		var value any
		if err := decodeUseNumber(raw, &value); err != nil {
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

// decodeUseNumber decodes a single JSON value with number precision preserved,
// rejecting trailing data.
func decodeUseNumber(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decodeSingleValue(decoder, out)
}

// decodeStrictUseNumber decodes like decodeUseNumber but also rejects unknown
// struct fields.
func decodeStrictUseNumber(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	return decodeSingleValue(decoder, out)
}

func decodeSingleValue(decoder *json.Decoder, out any) error {
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
