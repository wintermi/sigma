// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package modeldata

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCatalogFileChecksumAndValidation(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("catalog.json")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	sum := sha256.Sum256(data)
	if got, want := hex.EncodeToString(sum[:]), "6b54b138799c15583f8b10da4315573353c0717ce0e85fb72a2b8f51ce8b313f"; got != want {
		t.Fatalf("catalog checksum = %s, want %s", got, want)
	}
	if _, err := Decode(strings.NewReader(string(data))); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
}

func TestCatalogValidationReportsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	_, err := Decode(strings.NewReader(`{
		"schemaVersion": 1,
		"snapshotDate": "2026-05-26",
		"sources": [{"name": "test", "url": "https://example.test/models"}],
		"textModels": [{
			"id": "missing-base-url",
			"name": "Missing Base URL",
			"provider": "openai",
			"api": "openai-responses",
			"supportedInputs": ["text"],
			"cost": {"inputPerMillion": 1, "outputPerMillion": 2, "currency": "USD"},
			"contextWindow": 128000,
			"maxOutputTokens": 8192,
			"authEnvNames": ["OPENAI_API_KEY"],
			"defaultTransport": "sse"
		}],
		"imageModels": [{
			"id": "image-test",
			"name": "Image Test",
			"provider": "openai",
			"api": "openai-images",
			"baseURL": "https://api.openai.com/v1",
			"maxWidth": 1024,
			"maxHeight": 1024,
			"supportedSizes": ["1024x1024"],
			"supportedFormats": ["image/png"],
			"cost": {"unit": "image", "currency": "USD", "values": {"image": 1}},
			"authEnvNames": ["OPENAI_API_KEY"]
		}],
		"embeddingModels": [{
			"id": "embedding-test",
			"name": "Embedding Test",
			"provider": "openai",
			"api": "openai-embeddings",
			"baseURL": "https://api.openai.com/v1",
			"defaultDimensions": 1536,
			"minDimensions": 1,
			"maxDimensions": 1536,
			"maxInputTokens": 8192,
			"inputCostPerMillion": 0.02,
			"currency": "USD",
			"authEnvNames": ["OPENAI_API_KEY"]
		}]
	}`))
	if err == nil {
		t.Fatal("Decode returned nil error for missing baseURL")
	}
	if !strings.Contains(err.Error(), `textModels[0] "missing-base-url": baseURL is required`) {
		t.Fatalf("Decode error = %q, want missing baseURL context", err)
	}
}

func TestCatalogValidationRejectsNegativeCacheCosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cost string
		want string
	}{
		{
			name: "cache read",
			cost: `"cacheReadInputPerMillion": -0.1`,
			want: "cost.cacheReadInputPerMillion must be non-negative",
		},
		{
			name: "cache write",
			cost: `"cacheWriteInputPerMillion": -0.1`,
			want: "cost.cacheWriteInputPerMillion must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Decode(strings.NewReader(`{
				"schemaVersion": 1,
				"snapshotDate": "2026-05-26",
				"sources": [{"name": "test", "url": "https://example.test/models"}],
				"textModels": [{
					"id": "cache-test",
					"name": "Cache Test",
					"provider": "openai",
					"api": "openai-responses",
					"baseURL": "https://api.openai.com/v1",
					"supportedInputs": ["text"],
					"cost": {"inputPerMillion": 1, "outputPerMillion": 2, ` + tt.cost + `, "currency": "USD"},
					"contextWindow": 128000,
					"maxOutputTokens": 8192,
					"authEnvNames": ["OPENAI_API_KEY"],
					"defaultTransport": "sse"
				}],
				"imageModels": [{
					"id": "image-test",
					"name": "Image Test",
					"provider": "openai",
					"api": "openai-images",
					"baseURL": "https://api.openai.com/v1",
					"maxWidth": 1024,
					"maxHeight": 1024,
					"supportedSizes": ["1024x1024"],
					"supportedFormats": ["image/png"],
					"cost": {"unit": "image", "currency": "USD", "values": {"image": 1}},
					"authEnvNames": ["OPENAI_API_KEY"]
				}],
				"embeddingModels": [{
					"id": "embedding-test",
					"name": "Embedding Test",
					"provider": "openai",
					"api": "openai-embeddings",
					"baseURL": "https://api.openai.com/v1",
					"defaultDimensions": 1536,
					"minDimensions": 1,
					"maxDimensions": 1536,
					"maxInputTokens": 8192,
					"inputCostPerMillion": 0.02,
					"currency": "USD",
					"authEnvNames": ["OPENAI_API_KEY"]
				}]
			}`))
			if err == nil {
				t.Fatal("Decode returned nil error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Decode error = %q, want %q", err, tt.want)
			}
		})
	}
}

func TestCatalogValidationRejectsInvalidCostTiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tiers []CostTier
		want  string
	}{
		{
			name:  "negative threshold",
			tiers: []CostTier{{InputTokensAbove: -1}},
			want:  "cost.tiers[0].inputTokensAbove must be non-negative",
		},
		{
			name:  "non-increasing threshold",
			tiers: []CostTier{{InputTokensAbove: 10}, {InputTokensAbove: 10}},
			want:  "cost.tiers[1].inputTokensAbove must be strictly increasing",
		},
		{
			name:  "negative cache write rate",
			tiers: []CostTier{{InputTokensAbove: 10, CacheWriteInputPerMillion: -1}},
			want:  "cost.tiers[0].cacheWriteInputPerMillion must be non-negative",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateCost(Cost{InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD", Tiers: tt.tiers})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateCost error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestWriteValidatesAndOrdersCatalog(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "catalog.json")
	catalog := Catalog{
		SchemaVersion: 1,
		SnapshotDate:  "2026-06-25",
		Sources:       []Source{{Name: "test", URL: "https://example.test/models"}},
		TextModels: []TextModel{
			{
				ID:              "z-model",
				Name:            "Z Model",
				Provider:        "z-provider",
				API:             "chat",
				BaseURL:         "https://z.example.test",
				SupportedInputs: []string{"text"},
				Cost:            Cost{InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"},
				ContextWindow:   4096,
				MaxOutputTokens: 1024,
				AuthEnvNames:    []string{"Z_API_KEY"},
			},
			{
				ID:              "a-model",
				Name:            "A Model",
				Provider:        "a-provider",
				API:             "chat",
				BaseURL:         "https://a.example.test",
				SupportedInputs: []string{"text"},
				Cost:            Cost{InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"},
				ContextWindow:   4096,
				MaxOutputTokens: 1024,
				AuthEnvNames:    []string{"A_API_KEY"},
			},
		},
		ImageModels: []ImageModel{
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
				Cost:             ImageCost{Unit: "image", Currency: "USD", Values: map[string]float64{"image": 1}},
				AuthEnvNames:     []string{"OPENAI_API_KEY"},
			},
		},
		EmbeddingModels: []EmbeddingModel{
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

	if err := Write(path, catalog); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(data), "\n  \"textModels\": [\n") {
		t.Fatalf("catalog was not written with stable indentation:\n%s", data)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got, want := []string{loaded.TextModels[0].ID, loaded.TextModels[1].ID}, []string{"a-model", "z-model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("text model order = %#v, want %#v", got, want)
	}
}
