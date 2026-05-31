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

## OpenRouter Images

An implemented image-generation adapter is `provider/openrouter`, which sends
non-streaming OpenRouter Chat Completions requests to image-capable models:

```go
registry := sigma.NewRegistry()
_ = openrouter.Register(registry)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `OPENROUTER_API_KEY`.

OpenRouter maps `Size`, `Quality`, and provider-specific routing values to
OpenRouter request fields where possible. Support depends on the routed upstream
model. Generated metadata includes a Grok Imagine route through OpenRouter; it
uses `OPENROUTER_API_KEY` and the existing `openrouter-images` adapter rather
than a direct xAI image provider.

## OpenAI Images

`ImageAPIOpenAIImages` uses OpenAI's dedicated image generation endpoint.

```go
registry := sigma.NewRegistry()
_ = openai.RegisterImages(registry, sigma.ProviderOpenAI)
client := sigma.NewClient(sigma.WithRegistry(registry))
```

Environment: `OPENAI_API_KEY`.

The adapter implements generation-only requests through `ImageRequest.Prompt`.
Reference-image editing through `ImageRequest.Inputs`, variations, streaming
partial images, and Responses image-tool generation are deferred.

## Examples

The [images example](../examples/images/main.go) demonstrates both image input
to a text model and deterministic image generation through `sigmatest`.
