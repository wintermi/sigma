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

`EmbeddingModel` records routing metadata for dimensions, input limits, batch
limits, and cost. `DefaultDimensions` is the provider default, `MinDimensions`
and `MaxDimensions` describe the supported reduction range when known,
`MaxInputTokens` records the provider input limit, `MaxBatchInputs` and
`MaxBatchBytes` describe known provider batch caps, and
`InputCostPerMillion` plus `CostCurrency` drive deterministic embedding cost
calculation.

## Dimensions

`EmbeddingRequest.Dimensions` requests a smaller embedding vector when the
provider and model support dimensionality reduction. Leave it at zero to use the
model default recorded in `EmbeddingModel.DefaultDimensions`.

Sigma validates that dimensions are non-negative. Dimension ranges are model
metadata for discovery and routing; providers may still reject unsupported
dimensions.

## Query And Document Intent

Use `EmbeddingQuery` and `EmbeddingDocuments` when your application needs to
distinguish search queries from indexed documents:

```go
query := sigma.EmbeddingQuery("streaming support")
documents := sigma.EmbeddingDocuments([]string{
	"Sigma streams text responses.",
	"Sigma generates vector embeddings.",
})
```

The helpers set `EmbeddingRequest.InputType` to `query` or `document` and clone
document input slices. Providers that support task-specific embedding modes can
use the field; OpenAI's `/v1/embeddings` adapter intentionally ignores it
because that endpoint does not accept a separate query/document field.

Sigma does not silently alter input text. If you want newline normalization,
apply it explicitly before embedding:

```go
req := sigma.EmbeddingDocuments(
	sigma.NormalizeEmbeddingNewlines(rawDocuments),
)
```

## Attempt Metadata

Embedding responses include SDK-level attempt metadata when the provider can
report it:

```go
for _, attempt := range result.Attempts {
	fmt.Println(attempt.Provider, attempt.API, attempt.Model)
	fmt.Println(attempt.Attempt, attempt.StatusCode, attempt.RequestID, attempt.Latency)
}
```

`EmbeddingAttempt` records provider, API, model, zero-based retry attempt,
status code, request ID, and per-attempt latency. These are SDK transport facts,
not provider-specific response payload fields.

## Batching, Usage, And Cost

`EmbeddingRequest.Inputs` accepts multiple non-empty text strings. Returned
vectors include the provider-reported index so callers can match vectors back to
their inputs.

Provider usage maps prompt tokens to `Usage.InputTokens`. When model pricing is
available, Sigma calculates `Cost.InputCost` and `Cost.TotalCost` from input
tokens and `EmbeddingModel.InputCostPerMillion`.

For larger batches, `Client.EmbedBatch` keeps the same provider-neutral model
and request shape while adding duplicate input reuse, retry-aware batch
splitting, optional oversized-input splitting, progress callbacks, and aggregate
usage/cost summaries:

```go
result, err := client.EmbedBatch(ctx, model, sigma.EmbeddingRequest{
	Inputs: []string{"alpha", "beta", "alpha"},
}, sigma.EmbeddingBatchConfig{
	ReuseDuplicateInputs: true,
	MaxBatchInputs:       256,
	Cache:                cache,
	MaxRetries:           2,
	SplitOversized:       true,
	Progress: func(progress sigma.EmbeddingBatchProgress) error {
		return nil
	},
})
if err != nil {
	return err
}

for _, embedding := range result.Embeddings.Vectors {
	fmt.Println(embedding.Index, len(embedding.Vector))
}
```

`MaxRetries` controls batch-level split attempts after the underlying provider
call returns. It does not replace `WithEmbeddingMaxRetries`, which still
controls HTTP retry behaviour inside provider adapters.

`MaxBatchInputs` and `MaxBatchBytes` split provider-bound work before dispatch.
When left at zero, Sigma uses the selected `EmbeddingModel` limits when known.
Byte limits count UTF-8 input bytes, not JSON payload bytes. Token-budget
estimates remain caller-owned because provider tokenizers vary.

Set `Cache` to reuse embeddings across separate `EmbedBatch` calls. Cache keys
include provider, API, model, dimensions, and a SHA-256 hash of the input text;
raw input text is not exposed to the cache key. `ReuseDuplicateInputs` remains
the in-request duplicate coalescing option.

Oversized singleton recovery uses `EmbeddingSplitPolicy` to choose safer split
points. The zero-value policy prefers a nearby newline, then nearby whitespace,
then a UTF-8-safe rune midpoint.

`EmbeddingBatchSummary` keeps aggregate batch telemetry: successful provider
result count, total request attempts, error count, vector count, status buckets,
request IDs, attempts, trace events, usage, and cost. `Trace` records redacted
batch execution events such as cache lookup/store, planned limit splits,
provider attempts, and oversized-input splits without raw input text.

## Vector Utilities

Sigma includes deterministic helpers for the small amount of vector math that
callers commonly need around embeddings:

```go
scores, err := sigma.RankEmbeddingsByCosine(queryVector, result.Vectors)
if err != nil {
	return err
}
for _, score := range scores {
	fmt.Println(score.Embedding.Index, score.Score)
}
```

`DotProduct`, `CosineSimilarity`, `NormalizeEmbeddingVector`,
`CombineEmbeddingVectors`, and `RankEmbeddingsByCosine` return typed sentinel
errors for mismatched dimensions, zero-norm vectors, weight mismatches, and
zero total weight. These helpers are deterministic numeric utilities; they do
not perform vector-store persistence, chunking, or provider token estimation.

## Current Scope

The first embedding provider is OpenAI's `/v1/embeddings` API. Sigma includes
generated metadata for `text-embedding-3-small` and
`text-embedding-3-large`, plus `sigma.OpenAICompatibleEmbeddingModel` for
caller-registered OpenAI-compatible embedding endpoints.

Vector stores, general text chunking, tokenizer-based estimates,
provider-selection fallback, and non-OpenAI embedding providers are
intentionally outside this surface.
