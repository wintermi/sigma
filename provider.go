// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import "context"

// TextProvider adapts a provider API into sigma's streaming text interface.
type TextProvider interface {
	API() API
	Stream(context.Context, Model, Request, Options) *Stream
}

// ImageProvider adapts a provider API into sigma's image generation interface.
type ImageProvider interface {
	API() ImageAPI
	Generate(context.Context, ImageModel, ImageRequest, Options) (AssistantImages, error)
}

// EmbeddingProvider adapts a provider API into sigma's vector embeddings interface.
type EmbeddingProvider interface {
	API() EmbeddingAPI
	Embed(context.Context, EmbeddingModel, EmbeddingRequest, Options) (Embeddings, error)
}

// StreamingImageProvider optionally adapts a provider API into sigma's
// streaming image interface.
type StreamingImageProvider interface {
	ImageProvider
	StreamImages(context.Context, ImageModel, ImageRequest, Options) *ImageStream
}
