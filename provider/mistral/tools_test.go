// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral_test

import (
	"reflect"
	"testing"

	"github.com/wintermi/sigma/provider/mistral"
)

func TestToolsBuildMistralRetrievalTools(t *testing.T) {
	t.Parallel()

	libraryIDs := []string{"lib_one", "lib_two"}
	tools := []struct {
		name     string
		gotType  string
		got      any
		wantType string
		want     any
	}{
		{
			name:     "web search",
			gotType:  mistral.Tools.WebSearch().ProviderDefinedType,
			got:      mistral.Tools.WebSearch().ProviderDefinedOptions,
			wantType: "mistral.web_search",
			want:     map[string]any(nil),
		},
		{
			name:     "premium web search",
			gotType:  mistral.Tools.WebSearchPremium().ProviderDefinedType,
			got:      mistral.Tools.WebSearchPremium().ProviderDefinedOptions,
			wantType: "mistral.web_search_premium",
			want:     map[string]any(nil),
		},
		{
			name:     "document library",
			gotType:  mistral.Tools.DocumentLibrary(libraryIDs...).ProviderDefinedType,
			got:      mistral.Tools.DocumentLibrary(libraryIDs...).ProviderDefinedOptions,
			wantType: "mistral.document_library",
			want:     map[string]any{"library_ids": []string{"lib_one", "lib_two"}},
		},
	}

	libraryTool := mistral.Tools.DocumentLibrary(libraryIDs...)
	libraryIDs[0] = "changed"
	if got, want := libraryTool.ProviderDefinedOptions["library_ids"], []string{"lib_one", "lib_two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("document library IDs = %#v, want %#v", got, want)
	}

	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, want := tt.gotType, tt.wantType; got != want {
				t.Fatalf("provider type = %q, want %q", got, want)
			}
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Fatalf("provider options = %#v, want %#v", tt.got, tt.want)
			}
		})
	}
}
