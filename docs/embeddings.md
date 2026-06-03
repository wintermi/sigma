# Embeddings

Sigma exposes vector embeddings as a separate provider surface from text
generation and image generation.

Use embedding models when you need numeric vectors for search, clustering,
classification, recommendation, or other similarity workflows. Do not use
`Client.Stream`, `Client.Complete`, or `Client.GenerateImages` for embeddings.

## Basic Usage

```go
registry := sigma.NewRegistry()
_ = openai.RegisterEmbeddings(registry, sigma.ProviderOpenAI)

client := sigma.NewClient(sigma.WithRegistry(registry))
model, ok := client.GetEmbeddingModel(sigma.ProviderOpenAI, "text-embedding-3-small")
if !ok {
	return fmt.Errorf("embedding model is not registered")
}

result, err := client.Embed(ctx, model, sigma.EmbeddingRequest{
	Inputs: []string{
		"Sigma supports streaming text generation.",
		"Embeddings turn text into vectors.",
	},
	Dimensions: 512,
}, sigma.WithEmbeddingAPIKey(os.Getenv("OPENAI_API_KEY")))
if err != nil {
	return err
}

for _, embedding := range result.Vectors {
	fmt.Println(embedding.Index, len(embedding.Vector))
}
```

## Model Discovery

Embedding models use `EmbeddingModel` metadata and can be discovered from a
client or from the default registry:

```go
models := client.EmbeddingModels()
model, ok := sigma.GetEmbeddingModel(sigma.ProviderOpenAI, "text-embedding-3-large")
```

Built-in embedding model metadata is metadata-only by default. Register the
matching provider before runtime dispatch:

```go
_ = openai.RegisterEmbeddings(registry, sigma.ProviderOpenAI)
```

## Dimensions

`EmbeddingRequest.Dimensions` requests a smaller embedding vector when the
provider and model support dimensionality reduction. Leave it at zero to use the
model default recorded in `EmbeddingModel.DefaultDimensions`.

Sigma validates that dimensions are non-negative. Providers may still reject
unsupported dimensions.

## Batching, Usage, And Cost

`EmbeddingRequest.Inputs` accepts multiple non-empty text strings. Returned
vectors include the provider-reported index so callers can match vectors back to
their inputs.

Provider usage maps prompt tokens to `Usage.InputTokens`. When model pricing is
available, Sigma calculates `Cost.InputCost` and `Cost.TotalCost` from input
tokens and `EmbeddingModel.InputCostPerMillion`.

## Current Scope

The first embedding provider is OpenAI's `/v1/embeddings` API, with generated
metadata for `text-embedding-3-small` and `text-embedding-3-large`.

Vector stores, text chunking, tokenizer-based estimates, similarity/ranking
helpers, and non-OpenAI embedding providers are intentionally outside this
surface.

