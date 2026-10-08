// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wintermi/sigma/internal/modeldata"
)

// Long-context tiers from models.dev must survive a catalog refresh, with
// rates a tier omits falling back to the base price.
func TestBuildModelsDevCandidateCatalogImportsContextTiers(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "api.json")
	source := `{"openai":{"models":{
	"gpt-tiered":{"name":"GPT Tiered","tool_call":true,"reasoning":true,"limit":{"context":1050000,"output":128000},
		"cost":{"input":2.5,"output":15,"cache_read":0.25,"tiers":[{"input":5,"output":22.5,"tier":{"type":"context","size":272000}},{"input":9,"tier":{"type":"time"}}]},
		"modalities":{"input":["text"]}},
	"gpt-existing":{"name":"GPT Existing Updated","tool_call":true,"reasoning":true,"limit":{"context":256000,"output":32000},
		"cost":{"input":1.25,"output":10,"cache_read":0.25,"cache_write":1,"tiers":[{"input":3,"output":20,"cache_read":0.3,"tier":{"type":"context","size":200000}}]},
		"modalities":{"input":["text","image"]}}}}}`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	candidate, err := buildModelsDevCandidateCatalog(refreshTestCatalog(), path, "2026-10-08", false)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		id   string
		want []modeldata.CostTier
	}{
		{id: "gpt-tiered", want: []modeldata.CostTier{{InputTokensAbove: 272_000, InputPerMillion: 5, OutputPerMillion: 22.5, CacheReadInputPerMillion: 0.25}}},
		{id: "gpt-existing", want: []modeldata.CostTier{{InputTokensAbove: 200_000, InputPerMillion: 3, OutputPerMillion: 20, CacheReadInputPerMillion: 0.3, CacheWriteInputPerMillion: 1}}},
	} {
		row, ok := findTextModel(candidate, "openai", "openai-responses", tt.id)
		if !ok {
			t.Fatalf("candidate missing %s", tt.id)
		}
		if !reflect.DeepEqual(row.Cost.Tiers, tt.want) {
			t.Fatalf("%s tiers = %#v, want %#v", tt.id, row.Cost.Tiers, tt.want)
		}
	}
}
