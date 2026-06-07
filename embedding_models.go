// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

const (
	// EmbeddingAPIOpenAIEmbeddings identifies OpenAI's embeddings API.
	EmbeddingAPIOpenAIEmbeddings EmbeddingAPI = "openai-embeddings"
	// EmbeddingAPIGoogleEmbeddings identifies Google's Gemini embeddings API.
	EmbeddingAPIGoogleEmbeddings EmbeddingAPI = "google-embeddings"
	// EmbeddingAPIGoogleVertexEmbeddings identifies Google's Vertex AI embeddings API.
	EmbeddingAPIGoogleVertexEmbeddings EmbeddingAPI = "google-vertex-embeddings"
	// EmbeddingAPIBedrockEmbeddings identifies Amazon Bedrock's InvokeModel embeddings API.
	EmbeddingAPIBedrockEmbeddings EmbeddingAPI = "bedrock-embeddings"
)
