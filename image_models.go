// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

const (
	// ImageAPIOpenAIImages identifies the OpenAI image generation API.
	ImageAPIOpenAIImages ImageAPI = "openai-images"
	// ImageAPIOpenRouterImages identifies OpenRouter image generation through Chat Completions.
	ImageAPIOpenRouterImages ImageAPI = "openrouter-images"
	// ImageAPIGoogleImages identifies Google's Gemini and Imagen image APIs.
	ImageAPIGoogleImages ImageAPI = "google-images"
	// ImageAPIGoogleVertexImages identifies Google's Vertex AI Imagen image API.
	ImageAPIGoogleVertexImages ImageAPI = "google-vertex-images"
)

const (
	// ImageOperationGenerate requests text-to-image generation.
	ImageOperationGenerate ImageOperation = "generate"
	// ImageOperationEdit requests reference-image editing.
	ImageOperationEdit ImageOperation = "edit"
	// ImageOperationVariation requests a variation of one source image.
	ImageOperationVariation ImageOperation = "variation"
)

const (
	// ImageInputText identifies text input for image APIs.
	ImageInputText = "text"
	// ImageInputImage identifies image input or output for image APIs.
	ImageInputImage = "image"
	// ImageSourceBase64 identifies inline base64 image data.
	ImageSourceBase64 = "base64"
	// ImageSourceURL identifies URL-backed image data.
	ImageSourceURL = "url"
	// ImageSourceFileID identifies an already-uploaded provider file reference.
	ImageSourceFileID = "file_id"
)

// ImageText constructs a text input for image APIs.
func ImageText(text string) ImageInput {
	return ImageInput{
		Type: ImageInputText,
		Text: text,
	}
}

// ImageData constructs a base64 image input or output for image APIs.
func ImageData(mimeType string, data string) ImageInput {
	return ImageInput{
		Type:     ImageInputImage,
		MIMEType: mimeType,
		Source:   ImageSourceBase64,
		Data:     data,
	}
}

// ImageOutputData constructs a base64 generated image output.
func ImageOutputData(mimeType string, data string) ImageInput {
	return ImageData(mimeType, data)
}

// ImageOutputURL constructs a URL-backed generated image output.
func ImageOutputURL(mimeType string, url string) ImageInput {
	return ImageInput{
		Type:     ImageInputImage,
		MIMEType: mimeType,
		Source:   ImageSourceURL,
		URL:      url,
	}
}

// ImageFileID constructs a provider file reference for image APIs.
func ImageFileID(id string) ImageInput {
	return ImageInput{
		Type:   ImageInputImage,
		Source: ImageSourceFileID,
		Data:   id,
	}
}
