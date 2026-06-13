// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package minimax adapts MiniMax's Anthropic-compatible Messages endpoints to
// sigma.
//
// The provider reuses sigma's Anthropic Messages adapter with MiniMax global
// and CN defaults. Credentials resolve through sigma.Options.AuthResolver
// instead of direct environment reads.
package minimax
