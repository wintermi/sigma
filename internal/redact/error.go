// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package redact

import (
	"encoding/json"
	"fmt"
	"io"
)

// Error protects displayed diagnostics while preserving the original error chain.
// Explicitly inspecting an unwrapped error remains the caller's responsibility.
func Error(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	safe := String(message)
	if safe == message {
		return err
	}
	// String also canonicalizes valid JSON. Formatting alone does not require
	// wrapping an otherwise safe error.
	var decoded any
	if json.Unmarshal([]byte(message), &decoded) == nil {
		canonical, marshalErr := json.Marshal(decoded)
		if marshalErr == nil && safe == string(canonical) {
			return err
		}
	}
	return &diagnosticError{message: safe, cause: err}
}

type diagnosticError struct {
	message string
	cause   error
}

func (e *diagnosticError) Error() string  { return e.message }
func (e *diagnosticError) String() string { return e.message }
func (e *diagnosticError) Unwrap() error  { return e.cause }
func (e *diagnosticError) Format(state fmt.State, verb rune) {
	if verb == 'q' {
		_, _ = fmt.Fprintf(state, "%q", e.message)
		return
	}
	_, _ = io.WriteString(state, e.message)
}
