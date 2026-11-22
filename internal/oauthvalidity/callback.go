// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package oauthvalidity

// CallbackError reports a failed OnRefresh persistence callback. Its message
// omits the callback's error, which may quote the credentials being saved,
// while errors.Is and errors.As still reach it.
type CallbackError struct {
	Prefix string
	Err    error
}

func (e *CallbackError) Error() string {
	return e.Prefix + ": refresh callback failed"
}

func (e *CallbackError) Unwrap() error {
	return e.Err
}
