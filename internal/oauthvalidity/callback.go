// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package oauthvalidity

import (
	"context"
	"net/http"
	"time"
)

// callbackShutdownTimeout bounds how long a closing callback server waits for
// an in-flight response.
const callbackShutdownTimeout = time.Second

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

// CloseCallbackServer stops a login callback server after any in-flight
// response has been written. A login completes from inside the callback
// handler, and closing the server abruptly could drop the success page the
// browser is still receiving.
func CloseCallbackServer(server *http.Server) {
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), callbackShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
	}
}
