// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wintermi/sigma/internal/modeldata"
)

const modelsDevSourceName = "models.dev API snapshot"

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	Name       string              `json:"name"`
	ToolCall   bool                `json:"tool_call"`
	Reasoning  bool                `json:"reasoning"`
	Limit      modelsDevLimit      `json:"limit"`
	Cost       modelsDevCost       `json:"cost"`
	Modalities modelsDevModalities `json:"modalities"`
}

type modelsDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type modelsDevCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type modelsDevModalities struct {
	Input []string `json:"input"`
}

type modelsDevMapping struct {
	Provider string
	API      string
}

var modelsDevProviderMappings = map[string]modelsDevMapping{
	"amazon-bedrock": {Provider: "amazon-bedrock", API: "bedrock-converse-stream"},
	"anthropic":      {Provider: "anthropic", API: "anthropic-messages"},
	"cerebras":       {Provider: "cerebras", API: "openai-completions"},
	"deepseek":       {Provider: "deepseek", API: "openai-completions"},
	"google":         {Provider: "google", API: "google-generative-ai"},
	"google-vertex":  {Provider: "google-vertex", API: "google-vertex"},
	"groq":           {Provider: "groq", API: "openai-completions"},
	"mistral":        {Provider: "mistral", API: "mistral-conversations"},
	"nvidia":         {Provider: "nvidia", API: "openai-completions"},
	"openai":         {Provider: "openai", API: "openai-responses"},
	"openrouter":     {Provider: "openrouter", API: "openai-completions"},
	"together":       {Provider: "together", API: "openai-completions"},
	"xai":            {Provider: "xai", API: "openai-completions"},
}

func buildModelsDevCandidateCatalog(base modeldata.Catalog, source string, snapshotDate string, allowNetwork bool) (modeldata.Catalog, error) {
	if _, err := time.Parse("2006-01-02", snapshotDate); err != nil {
		return modeldata.Catalog{}, fmt.Errorf("refresh-snapshot-date must use YYYY-MM-DD: %w", err)
	}
	sourceData, sourceURL, err := readModelsDevSource(source, allowNetwork)
	if err != nil {
		return modeldata.Catalog{}, err
	}
	modelsDev, err := decodeModelsDev(sourceData)
	if err != nil {
		return modeldata.Catalog{}, err
	}

	candidate := cloneCatalog(base)
	candidate.SnapshotDate = snapshotDate
	candidate.Sources = upsertSource(candidate.Sources, modeldata.Source{Name: modelsDevSourceName, URL: sourceURL})
	mergeModelsDevTextModels(&candidate, modelsDev)
	candidate.Sort()
	if err := candidate.Validate(); err != nil {
		return modeldata.Catalog{}, fmt.Errorf("validate refresh catalog: %w", err)
	}
	return candidate, nil
}

func readModelsDevSource(source string, allowNetwork bool) ([]byte, string, error) {
	if source == "" {
		return nil, "", fmt.Errorf("models-dev-source is required")
	}
	parsed, err := url.Parse(source)
	if err == nil && isHTTPURL(parsed) {
		return readModelsDevHTTPSource(source, allowNetwork)
	}

	return readModelsDevFileSource(source)
}

func isHTTPURL(parsed *url.URL) bool {
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func readModelsDevHTTPSource(source string, allowNetwork bool) ([]byte, string, error) {
	if !allowNetwork {
		return nil, "", fmt.Errorf("models-dev-source %q requires -allow-network", source)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, source, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create models-dev-source request: %w", err)
	}
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req) // #nosec G107 -- command-line URL is explicit and gated by -allow-network.
	if err != nil {
		return nil, "", fmt.Errorf("fetch models-dev-source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", fmt.Errorf("fetch models-dev-source: status %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read models-dev-source response: %w", err)
	}
	return data, source, nil
}

func readModelsDevFileSource(source string) ([]byte, string, error) {
	data, err := os.ReadFile(source) // #nosec G304 -- command-line path is explicit generator input.
	if err != nil {
		return nil, "", fmt.Errorf("read models-dev-source: %w", err)
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return nil, "", fmt.Errorf("resolve models-dev-source path: %w", err)
	}
	return data, (&url.URL{Scheme: "file", Host: "localhost", Path: filepath.ToSlash(absolute)}).String(), nil
}

func decodeModelsDev(data []byte) (map[string]modelsDevProvider, error) {
	var decoded map[string]modelsDevProvider
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode models.dev source: %w", err)
	}
	return decoded, nil
}

func mergeModelsDevTextModels(catalog *modeldata.Catalog, source map[string]modelsDevProvider) {
	defaults := textDefaults(catalog.TextModels)
	byKey := textModelsByKey(catalog.TextModels)

	for sourceProvider, provider := range source {
		mapping, ok := modelsDevProviderMappings[sourceProvider]
		if !ok {
			continue
		}
		defaultModel, ok := defaults[textModelProviderAPIKey(mapping.Provider, mapping.API)]
		if !ok {
			continue
		}
		for id, model := range provider.Models {
			if !model.ToolCall {
				continue
			}
			row := mergeModelsDevTextModel(defaultModel, model, id)
			key := textModelKey(row)
			if existing, ok := byKey[key]; ok {
				row = mergeModelsDevTextModel(existing, model, id)
				catalog.TextModels[existing.index] = row
				byKey[key] = indexedTextModel{TextModel: row, index: existing.index}
				continue
			}
			catalog.TextModels = append(catalog.TextModels, row)
			byKey[key] = indexedTextModel{TextModel: row, index: len(catalog.TextModels) - 1}
		}
	}
}

type indexedTextModel struct {
	modeldata.TextModel
	index int
}

func mergeModelsDevTextModel(base indexedTextModel, source modelsDevModel, id string) modeldata.TextModel {
	model := base.TextModel
	model.ID = id
	model.Name = firstNonEmptyString(source.Name, id)
	model.SupportedInputs = modelsDevSupportedInputs(source)
	model.SupportsTools = true
	model.SupportsThinking = source.Reasoning
	model.Cost = modeldata.Cost{
		InputPerMillion:           source.Cost.Input,
		OutputPerMillion:          source.Cost.Output,
		CacheReadInputPerMillion:  source.Cost.CacheRead,
		CacheWriteInputPerMillion: source.Cost.CacheWrite,
		Tiers:                     append([]modeldata.CostTier(nil), model.Cost.Tiers...),
		Currency:                  firstNonEmptyString(model.Cost.Currency, "USD"),
	}
	model.ContextWindow = positiveOrDefault(source.Limit.Context, model.ContextWindow)
	model.MaxOutputTokens = positiveOrDefault(source.Limit.Output, model.MaxOutputTokens)
	return model
}

func modelsDevSupportedInputs(model modelsDevModel) []string {
	inputs := []string{"text"}
	for _, input := range model.Modalities.Input {
		if input == "image" {
			return append(inputs, "image")
		}
	}
	return inputs
}

func textDefaults(models []modeldata.TextModel) map[string]indexedTextModel {
	defaults := make(map[string]indexedTextModel)
	for index, model := range models {
		key := textModelProviderAPIKey(model.Provider, model.API)
		if _, ok := defaults[key]; !ok {
			defaults[key] = indexedTextModel{TextModel: textModelDefault(model), index: index}
		}
	}
	return defaults
}

func textModelDefault(model modeldata.TextModel) modeldata.TextModel {
	return modeldata.TextModel{
		Provider:         model.Provider,
		API:              model.API,
		BaseURL:          model.BaseURL,
		Headers:          cloneStringMap(model.Headers),
		AuthEnvNames:     append([]string(nil), model.AuthEnvNames...),
		DefaultTransport: model.DefaultTransport,
		Cost:             modeldata.Cost{Currency: firstNonEmptyString(model.Cost.Currency, "USD")},
		ContextWindow:    model.ContextWindow,
		MaxOutputTokens:  model.MaxOutputTokens,
	}
}

func textModelsByKey(models []modeldata.TextModel) map[string]indexedTextModel {
	byKey := make(map[string]indexedTextModel, len(models))
	for index, model := range models {
		byKey[textModelKey(model)] = indexedTextModel{TextModel: model, index: index}
	}
	return byKey
}

func textModelKey(model modeldata.TextModel) string {
	return textModelProviderAPIKey(model.Provider, model.API) + "\x00" + model.ID
}

func textModelProviderAPIKey(provider, api string) string {
	return provider + "\x00" + api
}

func upsertSource(sources []modeldata.Source, source modeldata.Source) []modeldata.Source {
	for index := range sources {
		if strings.EqualFold(sources[index].Name, source.Name) {
			sources[index] = source
			return sources
		}
	}
	return append(sources, source)
}

func cloneCatalog(catalog modeldata.Catalog) modeldata.Catalog {
	data, err := json.Marshal(catalog)
	if err != nil {
		panic(err)
	}
	var cloned modeldata.Catalog
	if err := json.Unmarshal(data, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func positiveOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
