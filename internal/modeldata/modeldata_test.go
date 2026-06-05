// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package modeldata

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
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
	if got, want := hex.EncodeToString(sum[:]), "bc139b73e903289090ef485f4d4403e9f1ffd6f93f1ab0f3aba40d0d18269577"; got != want {
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
