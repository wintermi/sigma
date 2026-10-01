// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

// VertexCredentialMode selects the Google Vertex AI authentication path.
type VertexCredentialMode string

const (
	// VertexCredentialAuto resolves a sigma credential first, then falls back to
	// the configured token provider when no API key or token is available.
	VertexCredentialAuto VertexCredentialMode = ""
	// VertexCredentialAPIKey requires an API-key credential.
	VertexCredentialAPIKey VertexCredentialMode = "api-key"
	// VertexCredentialToken requires an OAuth token credential.
	VertexCredentialToken VertexCredentialMode = "token"
)
