// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package xai adapts xAI's Grok OpenAI-compatible Chat Completions endpoint to
// sigma.
//
// The provider reuses sigma's OpenAI Chat Completions adapter with xAI
// defaults. Credentials resolve through sigma.Options.AuthResolver instead of
// direct environment reads.
package xai
