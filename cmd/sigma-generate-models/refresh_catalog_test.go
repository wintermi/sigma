// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wintermi/sigma/internal/modeldata"
)

func TestBuildModelsDevCandidateCatalogMergesSupportedTextRows(t *testing.T) {
	t.Parallel()

	sourcePath := writeModelsDevFixture(t)
	base := refreshTestCatalog()
	candidate, err := buildModelsDevCandidateCatalog(base, sourcePath, "2026-06-25", false)
	if err != nil {
		t.Fatalf("buildModelsDevCandidateCatalog returned error: %v", err)
	}

	updated, ok := findTextModel(candidate, "openai", "openai-responses", "gpt-existing")
	if !ok {
		t.Fatal("candidate missing updated OpenAI text row")
	}
	if got, want := updated.Name, "GPT Existing Updated"; got != want {
		t.Fatalf("updated name = %q, want %q", got, want)
	}
	if got, want := updated.ContextWindow, 256000; got != want {
		t.Fatalf("updated context window = %d, want %d", got, want)
	}
	if got, want := updated.MaxOutputTokens, 32000; got != want {
		t.Fatalf("updated max output = %d, want %d", got, want)
	}
	if !updated.SupportsTools || !updated.SupportsThinking {
		t.Fatalf("updated capabilities = tools:%t thinking:%t, want true/true", updated.SupportsTools, updated.SupportsThinking)
	}
	if got, want := updated.SupportedInputs, []string{"text", "image"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("updated inputs = %#v, want %#v", got, want)
	}
	if got, want := updated.Cost, (modeldata.Cost{
		InputPerMillion:           1.25,
		OutputPerMillion:          10,
		CacheReadInputPerMillion:  0.25,
		CacheWriteInputPerMillion: 1,
		Currency:                  "USD",
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("updated cost = %#v, want %#v", got, want)
	}
	if got, want := updated.ProviderMetadata["preserved"], "yes"; got != want {
		t.Fatalf("updated provider metadata = %v, want preserved", updated.ProviderMetadata)
	}
	if updated.OpenAICompletionsCompat != nil {
		t.Fatalf("updated OpenAI Responses row compat = %#v, want nil", updated.OpenAICompletionsCompat)
	}

	added, ok := findTextModel(candidate, "openai", "openai-responses", "gpt-new")
	if !ok {
		t.Fatal("candidate missing added OpenAI text row")
	}
	if got, want := added.BaseURL, "https://api.openai.com/v1"; got != want {
		t.Fatalf("added baseURL = %q, want %q", got, want)
	}
	if got, want := added.AuthEnvNames, []string{"OPENAI_API_KEY"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("added auth env names = %#v, want %#v", got, want)
	}
	if len(added.ProviderMetadata) != 0 {
		t.Fatalf("added provider metadata = %#v, want none", added.ProviderMetadata)
	}

	groq, ok := findTextModel(candidate, "groq", "openai-completions", "llama-new")
	if !ok {
		t.Fatal("candidate missing added Groq text row")
	}
	if got, want := groq.OpenAICompletionsCompat, (*modeldata.OpenAICompletionsCompat)(nil); got != want {
		t.Fatalf("new Groq compat = %#v, want nil until reviewed", got)
	}
	if got, want := groq.BaseURL, "https://api.groq.com/openai/v1"; got != want {
		t.Fatalf("Groq baseURL = %q, want %q", got, want)
	}

	if _, ok := findTextModel(candidate, "openai", "openai-responses", "gpt-no-tools"); ok {
		t.Fatal("candidate included model without tool_call support")
	}
	if _, ok := findTextModel(candidate, "unsupported", "openai-completions", "unsupported-1"); ok {
		t.Fatal("candidate included unsupported provider")
	}
	if !reflect.DeepEqual(candidate.ImageModels, base.ImageModels) {
		t.Fatalf("image models changed: %#v", candidate.ImageModels)
	}
	if !reflect.DeepEqual(candidate.EmbeddingModels, base.EmbeddingModels) {
		t.Fatalf("embedding models changed: %#v", candidate.EmbeddingModels)
	}
	if err := candidate.Validate(); err != nil {
		t.Fatalf("candidate did not validate: %v", err)
	}
	assertTextOrder(t, candidate.TextModels)
}

func TestRunGenerateModelsRefreshCatalogWritesCandidateOnly(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	inputPath := filepath.Join(tmp, "catalog.json")
	sourcePath := writeModelsDevFixture(t)
	outputPath := filepath.Join(tmp, "candidate.json")
	modelsPath := filepath.Join(tmp, "models.go")
	imageModelsPath := filepath.Join(tmp, "image_models.go")
	embeddingModelsPath := filepath.Join(tmp, "embedding_models.go")
	if err := modeldata.Write(inputPath, refreshTestCatalog()); err != nil {
		t.Fatalf("write base catalog: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runGenerateModels([]string{
		"-input", inputPath,
		"-refresh-catalog", outputPath,
		"-models-dev-source", sourcePath,
		"-refresh-snapshot-date", "2026-06-25",
		"-models", modelsPath,
		"-image-models", imageModelsPath,
		"-embedding-models", embeddingModelsPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runGenerateModels returned error: %v\nstderr:\n%s", err, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout/stderr = %q / %q, want empty", stdout.String(), stderr.String())
	}
	candidate, err := modeldata.Load(outputPath)
	if err != nil {
		t.Fatalf("load candidate: %v", err)
	}
	if got, want := candidate.SnapshotDate, "2026-06-25"; got != want {
		t.Fatalf("candidate snapshot date = %q, want %q", got, want)
	}
	for _, path := range []string{modelsPath, imageModelsPath, embeddingModelsPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("generated path %s stat error = %v, want not exist", path, err)
		}
	}

	diff := renderCatalogDiff(refreshTestCatalog(), candidate)
	if !strings.Contains(diff, "Text models: added 2, removed 0, changed 1") {
		t.Fatalf("diff did not report refresh changes:\n%s", diff)
	}
}

func TestRunGenerateModelsRefreshCatalogRequiresSnapshotDate(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	inputPath := filepath.Join(tmp, "catalog.json")
	if err := modeldata.Write(inputPath, refreshTestCatalog()); err != nil {
		t.Fatalf("write base catalog: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runGenerateModels([]string{
		"-input", inputPath,
		"-refresh-catalog", filepath.Join(tmp, "candidate.json"),
		"-models-dev-source", writeModelsDevFixture(t),
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("runGenerateModels returned nil error without refresh-snapshot-date")
	}
	if !strings.Contains(err.Error(), "refresh-snapshot-date must use YYYY-MM-DD") {
		t.Fatalf("error = %v, want snapshot-date message", err)
	}
}

func TestRunGenerateModelsRefreshCatalogRejectsNetworkWithoutOptIn(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	inputPath := filepath.Join(tmp, "catalog.json")
	if err := modeldata.Write(inputPath, refreshTestCatalog()); err != nil {
		t.Fatalf("write base catalog: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runGenerateModels([]string{
		"-input", inputPath,
		"-refresh-catalog", filepath.Join(tmp, "candidate.json"),
		"-models-dev-source", "https://models.dev/api.json",
		"-refresh-snapshot-date", "2026-06-25",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("runGenerateModels returned nil error for network source without opt-in")
	}
	if !strings.Contains(err.Error(), "requires -allow-network") {
		t.Fatalf("error = %v, want allow-network message", err)
	}
}

func writeModelsDevFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "models-dev.json")
	data := `{
  "openai": {
    "models": {
      "gpt-existing": {
        "name": "GPT Existing Updated",
        "tool_call": true,
        "reasoning": true,
        "limit": {"context": 256000, "output": 32000},
        "cost": {"input": 1.25, "output": 10, "cache_read": 0.25, "cache_write": 1},
        "modalities": {"input": ["text", "image"]}
      },
      "gpt-new": {
        "name": "GPT New",
        "tool_call": true,
        "limit": {"context": 128000, "output": 16000},
        "cost": {"input": 1, "output": 8},
        "modalities": {"input": ["text"]}
      },
      "gpt-no-tools": {
        "name": "GPT No Tools",
        "tool_call": false,
        "limit": {"context": 128000, "output": 16000},
        "cost": {"input": 1, "output": 8}
      }
    }
  },
  "groq": {
    "models": {
      "llama-new": {
        "name": "Llama New",
        "tool_call": true,
        "reasoning": true,
        "limit": {"context": 131072, "output": 8192},
        "cost": {"input": 0.05, "output": 0.1}
      }
    }
  },
  "unsupported-provider": {
    "models": {
      "unsupported-1": {
        "name": "Unsupported",
        "tool_call": true,
        "limit": {"context": 4096, "output": 1024},
        "cost": {"input": 1, "output": 1}
      }
    }
  }
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write models.dev fixture: %v", err)
	}
	return path
}

func refreshTestCatalog() modeldata.Catalog {
	return modeldata.Catalog{
		SchemaVersion: 1,
		SnapshotDate:  "2026-06-13",
		Sources: []modeldata.Source{
			{Name: "base", URL: "https://example.test/base"},
		},
		TextModels: []modeldata.TextModel{
			{
				ID:               "gpt-existing",
				Name:             "GPT Existing",
				Provider:         "openai",
				API:              "openai-responses",
				BaseURL:          "https://api.openai.com/v1",
				SupportedInputs:  []string{"text"},
				SupportsTools:    true,
				Cost:             modeldata.Cost{InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"},
				ContextWindow:    128000,
				MaxOutputTokens:  8192,
				AuthEnvNames:     []string{"OPENAI_API_KEY"},
				DefaultTransport: "sse",
				ProviderMetadata: map[string]any{"preserved": "yes"},
			},
			{
				ID:               "llama-existing",
				Name:             "Llama Existing",
				Provider:         "groq",
				API:              "openai-completions",
				BaseURL:          "https://api.groq.com/openai/v1",
				SupportedInputs:  []string{"text"},
				SupportsTools:    true,
				Cost:             modeldata.Cost{InputPerMillion: 0.1, OutputPerMillion: 0.2, Currency: "USD"},
				ContextWindow:    8192,
				MaxOutputTokens:  2048,
				AuthEnvNames:     []string{"GROQ_API_KEY"},
				DefaultTransport: "sse",
			},
		},
		ImageModels: []modeldata.ImageModel{
			{
				ID:               "image-test",
				Name:             "Image Test",
				Provider:         "openai",
				API:              "openai-images",
				BaseURL:          "https://api.openai.com/v1",
				MaxWidth:         1024,
				MaxHeight:        1024,
				SupportedSizes:   []string{"1024x1024"},
				SupportedFormats: []string{"image/png"},
				Cost:             modeldata.ImageCost{Unit: "image", Currency: "USD", Values: map[string]float64{"image": 1}},
				AuthEnvNames:     []string{"OPENAI_API_KEY"},
			},
		},
		EmbeddingModels: []modeldata.EmbeddingModel{
			{
				ID:                  "embedding-test",
				Name:                "Embedding Test",
				Provider:            "openai",
				API:                 "openai-embeddings",
				BaseURL:             "https://api.openai.com/v1",
				DefaultDimensions:   1536,
				MinDimensions:       1,
				MaxDimensions:       1536,
				MaxInputTokens:      8192,
				InputCostPerMillion: 0.02,
				Currency:            "USD",
				AuthEnvNames:        []string{"OPENAI_API_KEY"},
			},
		},
	}
}

func findTextModel(catalog modeldata.Catalog, provider, api, id string) (modeldata.TextModel, bool) {
	for _, model := range catalog.TextModels {
		if model.Provider == provider && model.API == api && model.ID == id {
			return model, true
		}
	}
	return modeldata.TextModel{}, false
}

func assertTextOrder(t *testing.T, models []modeldata.TextModel) {
	t.Helper()

	previous := ""
	for _, model := range models {
		key := catalogDiffKey(model.Provider, model.API, model.ID)
		if previous > key {
			t.Fatalf("text models are not sorted: %q before %q", previous, key)
		}
		previous = key
	}
}
