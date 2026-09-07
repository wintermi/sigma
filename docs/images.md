# Images

Sigma has two image paths:

- Image input for text models, using `sigma.ImageBase64` or `sigma.ImageURL`
  inside `sigma.UserContent`.
- Image generation, using `sigma.ImageRequest` and `Client.GenerateImages`.

These paths are separate because chat/completion providers and image providers
have different request and response shapes.

## Image Input

```go
final, err := client.Complete(ctx, model, sigma.Request{
	Messages: []sigma.Message{
		sigma.UserContent(
			sigma.Text("Describe this image."),
			sigma.ImageURL("image/png", "https://example.test/cat.png"),
		),
	},
})
```

Check model metadata before sending images:

```go
if !model.SupportsImages() {
	return fmt.Errorf("%s does not advertise image input", model.ID)
}
```

`sigma.UnmarshalRequest` validates persisted image blocks. Base64 image blocks
must contain valid base64 data and a MIME type. URL image blocks must contain a
URL and a MIME type. Providers may still reject an image that is too large, not
fetchable, or unsupported by the routed model.

## Image Generation

Image generation uses `ImageModel` metadata, an image provider, and
`ImageRequest`:

```go
images, err := client.GenerateImages(ctx, imageModel, sigma.ImageRequest{
	Prompt:   "A simple blue square icon",
	Size:     string(sigma.ImageSize1024x1024),
	Quality:  string(sigma.ImageQualityLow),
	MIMEType: "image/png",
	Count:    1,
})
```

Image provider options mirror text options but use `ImageOption` helpers:

- `sigma.WithImageAPIKey`
- `sigma.WithImageAuthResolver`
- `sigma.WithImageTimeout`
- `sigma.WithImageMaxRetries`
- `sigma.WithImageHeader`
- `sigma.WithImageProviderOption`

`AssistantImages.Images` contains provider-neutral `ImageInput` values. A
generated image can be base64 data (`sigma.ImageOutputData`) or a URL
(`sigma.ImageOutputURL`), depending on the provider response.

## Streaming and cancellation

OpenAI image streams require an `image_generation.completed` or
`image_edit.completed` event. EOF, keepalives, partial previews, unmarked image
data, and `[DONE]` alone do not establish success. A completion event with empty
output remains valid; no separate start event or image-count check is required.
Incomplete streams return an error with transient/retryable advice and retain
available completed images, usage, and metadata. Already-emitted previews remain
partial events and are not promoted to completed images. Sigma does not
automatically replay these streamed requests.

`ImageStream.Close` cancels in-flight OpenAI image requests, including requests
waiting for response headers or stalled response-body reads. The root streaming
fallback also cancels the context passed to `ImageProvider.Generate`; custom
providers must honor that context. Parent cancellation and request timeouts use
the same lifecycle. Normal completion releases timeout and watcher resources.
Repeated calls to `Close` are safe.

Explicit closure does not synthesize an aborted result. Before a terminal result
is accepted, context cancellation preserves available partials with an aborted
stop reason. Once accepted, success or error remains available through `Final`,
`Err`, and `CollectImages`, even if cancellation prevents terminal-event delivery.
See [Cancellation](cancellation.md).

OpenAI image generation, streaming, edits, and variations apply auth-derived
base URLs, endpoints, headers, and provider options before building each request.
Explicit request configuration overrides these defaults, with case-insensitive
header matching. Embedding requests follow the same auth-resolution contract.

## OpenRouter Images

An implemented image-generation adapter is `provider/openrouter`, which sends
non-streaming OpenRouter Chat Completions requests to image-capable models:

```go
registry := sigma.NewRegistry()
_ = openrouter.RegisterImages(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `OPENROUTER_API_KEY`.

OpenRouter maps `Size`, `Quality`, and provider-specific routing values to
OpenRouter request fields where possible. Support depends on the routed upstream
model. Generated metadata includes a Grok Imagine route through OpenRouter; it
uses `OPENROUTER_API_KEY` and the existing `openrouter-images` adapter rather
than a direct xAI image provider.

## OpenAI Images

`ImageAPIOpenAIImages` uses OpenAI's dedicated image generation, edit, and
variation endpoints.

```go
registry := sigma.NewRegistry()
_ = openai.RegisterImages(registry, sigma.ProviderOpenAI)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `OPENAI_API_KEY`.

The adapter supports generation through `ImageRequest.Prompt`, edits through
`ImageRequest.Inputs`, edit masks through `ImageRequest.Mask`, explicit
`dall-e-2` variations through `ImageOperationVariation`, and streaming partial
images through `Client.StreamImages`. OpenAI Responses image-generation tool
output is represented as assistant image content in text streams.

## Examples

The [images example](../examples/images/main.go) demonstrates both image input
to a text model and deterministic image generation through `sigmatest`.
