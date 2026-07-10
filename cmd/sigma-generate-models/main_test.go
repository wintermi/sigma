// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"bytes"
	"go/format"
	"strings"
	"testing"

	"github.com/wintermi/sigma/internal/modeldata"
)

func TestRenderGeneratedFilesIsDeterministic(t *testing.T) {
	t.Parallel()

	catalog, err := modeldata.Load("../../internal/modeldata/catalog.json")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	firstText, err := format.Source(renderTextModels(catalog))
	if err != nil {
		t.Fatalf("format first text models: %v", err)
	}
	secondText, err := format.Source(renderTextModels(catalog))
	if err != nil {
		t.Fatalf("format second text models: %v", err)
	}
	if !bytes.Equal(firstText, secondText) {
		t.Fatal("text model rendering was not deterministic")
	}

	firstImages, err := format.Source(renderImageModels(catalog))
	if err != nil {
		t.Fatalf("format first image models: %v", err)
	}
	secondImages, err := format.Source(renderImageModels(catalog))
	if err != nil {
		t.Fatalf("format second image models: %v", err)
	}
	if !bytes.Equal(firstImages, secondImages) {
		t.Fatal("image model rendering was not deterministic")
	}
	firstEmbeddings, err := format.Source(renderEmbeddingModels(catalog))
	if err != nil {
		t.Fatalf("format first embedding models: %v", err)
	}
	secondEmbeddings, err := format.Source(renderEmbeddingModels(catalog))
	if err != nil {
		t.Fatalf("format second embedding models: %v", err)
	}
	if !bytes.Equal(firstEmbeddings, secondEmbeddings) {
		t.Fatal("embedding model rendering was not deterministic")
	}
	if !strings.Contains(string(firstText), "Source snapshot date: 2026-07-10") {
		t.Fatal("generated text models missing source snapshot date")
	}
	if !strings.Contains(string(firstText), "https://platform.openai.com/docs/models") {
		t.Fatal("generated text models missing source URL")
	}
}

func TestRenderCatalogReportSummarizesProviderAPIBuckets(t *testing.T) {
	t.Parallel()

	report := renderCatalogReport(modeldata.Catalog{
		SnapshotDate: "2026-06-10",
		Sources: []modeldata.Source{
			{Name: "one", URL: "https://example.test/one"},
			{Name: "two", URL: "https://example.test/two"},
		},
		TextModels: []modeldata.TextModel{
			{Provider: "z-provider", API: "chat", SupportsTools: true, SupportsThinking: true},
			{Provider: "a-provider", API: "chat", SupportsTools: true},
			{Provider: "a-provider", API: "responses", ThinkingLevelMap: map[string]string{"high": "high"}},
		},
		ImageModels: []modeldata.ImageModel{
			{Provider: "openrouter", API: "openrouter-images"},
		},
		EmbeddingModels: []modeldata.EmbeddingModel{
			{Provider: "openai", API: "openai-embeddings"},
			{Provider: "openai", API: "openai-embeddings"},
		},
	})

	for _, want := range []string{
		"Catalog snapshot: 2026-06-10\n",
		"Sources: 2\n",
		"Text models: 3 (tools: 2, reasoning: 2)\n",
		"  a-provider / chat: 1\n",
		"  a-provider / responses: 1\n",
		"  z-provider / chat: 1\n",
		"Image models: 1\n",
		"  openrouter / openrouter-images: 1\n",
		"Embedding models: 2\n",
		"  openai / openai-embeddings: 2\n",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("catalog report missing %q:\n%s", want, report)
		}
	}
	if strings.Index(report, "a-provider / chat") > strings.Index(report, "z-provider / chat") {
		t.Fatalf("catalog report buckets are not sorted:\n%s", report)
	}
}

func TestRenderCatalogDiffReportsCandidateChanges(t *testing.T) {
	t.Parallel()

	base := modeldata.Catalog{
		SnapshotDate: "2026-06-10",
		TextModels: []modeldata.TextModel{
			{
				ID:              "unchanged-text",
				Provider:        "z-provider",
				API:             "chat",
				Name:            "Unchanged Text",
				Cost:            modeldata.Cost{InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"},
				MaxOutputTokens: 1000,
			},
			{
				ID:               "changed-text",
				Provider:         "a-provider",
				API:              "chat",
				Name:             "Old Text",
				Cost:             modeldata.Cost{InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"},
				MaxOutputTokens:  1000,
				ProviderMetadata: map[string]any{"route": "old"},
			},
			{ID: "removed-text", Provider: "a-provider", API: "chat", Name: "Removed Text"},
		},
		ImageModels: []modeldata.ImageModel{
			{
				ID:       "changed-image",
				Provider: "openrouter",
				API:      "openrouter-images",
				Name:     "Changed Image",
				Headers:  map[string]string{"X-Old": "old"},
			},
		},
		EmbeddingModels: []modeldata.EmbeddingModel{
			{
				ID:                  "changed-embedding",
				Provider:            "openai",
				API:                 "openai-embeddings",
				Name:                "Changed Embedding",
				DefaultDimensions:   1536,
				ProviderMetadata:    map[string]any{"family": "old"},
				InputCostPerMillion: 0.01,
			},
		},
	}
	candidate := modeldata.Catalog{
		SnapshotDate: "2026-06-20",
		TextModels: []modeldata.TextModel{
			{
				ID:       "added-text",
				Provider: "b-provider",
				API:      "chat",
				Name:     "Added Text",
			},
			{
				ID:              "unchanged-text",
				Provider:        "z-provider",
				API:             "chat",
				Name:            "Unchanged Text",
				Cost:            modeldata.Cost{InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"},
				MaxOutputTokens: 1000,
			},
			{
				ID:               "changed-text",
				Provider:         "a-provider",
				API:              "chat",
				Name:             "New Text",
				Cost:             modeldata.Cost{InputPerMillion: 3, OutputPerMillion: 2, Currency: "USD"},
				MaxOutputTokens:  2000,
				ProviderMetadata: map[string]any{"route": "new"},
			},
		},
		ImageModels: []modeldata.ImageModel{
			{
				ID:       "changed-image",
				Provider: "openrouter",
				API:      "openrouter-images",
				Name:     "Changed Image",
				Headers:  map[string]string{"X-New": "new"},
			},
		},
		EmbeddingModels: []modeldata.EmbeddingModel{
			{
				ID:                  "changed-embedding",
				Provider:            "openai",
				API:                 "openai-embeddings",
				Name:                "Changed Embedding",
				DefaultDimensions:   3072,
				ProviderMetadata:    map[string]any{"family": "new"},
				InputCostPerMillion: 0.01,
			},
		},
	}

	report := renderCatalogDiff(base, candidate)
	for _, want := range []string{
		"Catalog diff: 2026-06-10 -> 2026-06-20\n",
		"Text models: added 1, removed 1, changed 1, unchanged 1\n",
		"  Added:\n    + b-provider / chat / added-text\n",
		"  Removed:\n    - a-provider / chat / removed-text\n",
		"  Changed:\n    ~ a-provider / chat / changed-text: name, cost, maxOutputTokens, providerMetadata\n",
		"Image models: added 0, removed 0, changed 1, unchanged 0\n",
		"  Changed:\n    ~ openrouter / openrouter-images / changed-image: headers\n",
		"Embedding models: added 0, removed 0, changed 1, unchanged 0\n",
		"  Changed:\n    ~ openai / openai-embeddings / changed-embedding: defaultDimensions, providerMetadata\n",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("catalog diff missing %q:\n%s", want, report)
		}
	}
}

func TestRenderCatalogDiffOrdersRowsDeterministically(t *testing.T) {
	t.Parallel()

	base := modeldata.Catalog{
		SnapshotDate: "2026-06-10",
		TextModels: []modeldata.TextModel{
			{ID: "z-removed", Provider: "z-provider", API: "chat"},
			{ID: "a-removed", Provider: "a-provider", API: "chat"},
		},
	}
	candidate := modeldata.Catalog{
		SnapshotDate: "2026-06-20",
		TextModels: []modeldata.TextModel{
			{ID: "z-added", Provider: "z-provider", API: "chat"},
			{ID: "a-added", Provider: "a-provider", API: "chat"},
		},
	}

	report := renderCatalogDiff(base, candidate)
	assertBefore(t, report, "+ a-provider / chat / a-added", "+ z-provider / chat / z-added")
	assertBefore(t, report, "- a-provider / chat / a-removed", "- z-provider / chat / z-removed")
}

func assertBefore(t *testing.T, value, before, after string) {
	t.Helper()

	beforeIndex := strings.Index(value, before)
	if beforeIndex < 0 {
		t.Fatalf("report missing %q:\n%s", before, value)
	}
	afterIndex := strings.Index(value, after)
	if afterIndex < 0 {
		t.Fatalf("report missing %q:\n%s", after, value)
	}
	if beforeIndex > afterIndex {
		t.Fatalf("%q appeared after %q:\n%s", before, after, value)
	}
}
