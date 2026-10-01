// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package embeddingwire

import (
	"errors"
	"io"
	"net/http"
)

// ErrResponseTooLarge identifies a successful response exceeding the decoding bound.
var ErrResponseTooLarge = errors.New("embedding response exceeds 256 MiB limit")

// ReadResponse bounds successful vector responses separately from error bodies.
// The extra byte distinguishes an oversized success from malformed JSON.
func ReadResponse(resp *http.Response) ([]byte, error) {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	}
	return readResponse(resp.Body, 256<<20)
}

func readResponse(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, ErrResponseTooLarge
	}
	return body, nil
}
