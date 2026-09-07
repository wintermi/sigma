// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package opencode routes OpenCode Zen and OpenCode Go models to the Sigma
// adapter matching each model's OpenCode API family.
//
// Use sigma.WithSessionID with a stable conversation ID to send
// x-opencode-session on every routed API, including when caching is disabled.
// Explicit provider, model, and request session headers retain precedence,
// and final header suppression still applies. The caller owns session IDs;
// this package does not generate an ID when none is supplied.
package opencode
