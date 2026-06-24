// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package azure adapts Azure OpenAI Responses to sigma.
//
// The provider reuses Sigma's Azure OpenAI Responses adapter with the built-in
// Azure OpenAI Responses provider ID. Credentials resolve through
// sigma.Options.AuthResolver or caller-supplied token credentials instead of
// direct environment reads.
package azure
