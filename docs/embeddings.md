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
	CacheNamespace:       "search-production-tenant-a",
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

Set `Cache` and a non-empty, non-secret `CacheNamespace` to reuse embeddings
across separate `EmbedBatch` calls. A configured cache with a blank namespace
returns `ErrInvalidOptions`. `ReuseDuplicateInputs` alone needs no namespace.

Version 2 `EmbeddingCacheKey` values include `Version`, `Namespace`, and
`ConfigurationSHA256`, alongside provider, API, model, dimensions, input type,
and the per-input SHA-256 hash. The configuration digest covers the effective
model, request dimensions and input type, request provider metadata, effective
metadata and target-provider options, and oversized-input split settings.
Map insertion order does not affect the digest. Raw inputs and fingerprint
configuration are never included in keys or traces.

Cache implementations must compare **every key field**. Old persisted entries
are invalidated: Sigma does not read legacy keys. HTTP clients, callbacks, and
credential resolvers are not fingerprinted. Callers own namespace isolation for
opaque endpoints, tenants, custom transports, and provider configuration that
Sigma cannot inspect, including auth-derived configuration. Change the namespace
when any of those identities change; never use credentials as the namespace.

Model registration, provider registration, inputs, and effective options are
validated before cache lookup. Option functions run once per batch operation.
Cache hits honor cancellation without requiring authentication or provider calls.
The cache interface has no context argument, so a blocking cache implementation
must arrange its own I/O bounds; Sigma checks cancellation between lookups.

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
not perform vector-store persistence or provider token estimation.

## Retrieval Primitives

Sigma includes compact in-memory retrieval helpers for applications that need
small local search flows without adopting an external vector database:

```go
index := sigma.NewInMemoryRetrievalIndex(client, model, sigma.InMemoryRetrievalIndexConfig{
	Splitter: sigma.RetrievalSplitterConfig{
		ChunkSize:    1000,
		ChunkOverlap: 200,
	},
})

err := index.AddDocuments(ctx, []sigma.RetrievalDocument{
	{ID: "intro", Text: "Sigma generates vector embeddings."},
})
if err != nil {
	return err
}

results, err := index.Search(ctx, "embedding support", 3)
if err != nil {
	return err
}
for _, result := range results {
	fmt.Println(result.Chunk.ID, result.Score)
}
```

`SplitRetrievalText` and `SplitRetrievalDocuments` provide deterministic
character-based splitting with rune-safe byte offsets, overlap, separator
preference, and metadata copying. `InMemoryRetrievalIndex` embeds chunks as
`EmbeddingInputTypeDocument`, embeds searches as `EmbeddingInputTypeQuery`,
routes provider work through `Client.EmbedBatch`, stores normalized vectors
internally, and returns `RetrievalResult` values without exposing stored
vectors. Insertions validate and normalize the entire batch before changing the
index. Configured dimensions are enforced; otherwise the first successful
insertion establishes dimensions for later insertions. Failed or canceled
insertions leave existing items and inferred dimensions unchanged. The index
retains its existing caller-owned synchronization contract.

## Current Scope

Embedding adapters cover OpenAI-compatible `/v1/embeddings`, Google Gemini,
Google Vertex AI, and Amazon Bedrock. Register the route you use with
`openai.RegisterEmbeddings`, `google.RegisterEmbeddings`,
`google.RegisterVertexEmbeddings`, or `bedrock.RegisterEmbeddings` respectively.
Generated metadata includes representative models for these routes;
`sigma.OpenAICompatibleEmbeddingModel` supports caller-registered compatible
endpoints. The existing stable and preview classifications remain unchanged;
see the [release contract](../RELEASING.md).

External vector stores, tokenizer-aware chunking and estimates, and
provider-selection fallback remain outside this surface.
