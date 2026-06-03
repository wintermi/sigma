// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package openai adapts OpenAI-compatible APIs to sigma.
//
// It includes providers for Chat Completions-compatible endpoints, OpenAI
// Responses, Azure OpenAI Responses, OpenAI Codex Responses, OpenAI Images, and
// OpenAI Embeddings. Chat Completions-compatible registration is also the path
// for local endpoints and routers described with sigma.OpenAICompatibleModel.
//
// Providers resolve credentials through sigma.Options.AuthResolver or explicit
// token-provider options instead of reading environment variables directly.
package openai
