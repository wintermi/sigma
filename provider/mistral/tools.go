// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral

import "github.com/wintermi/sigma"

const (
	mistralProviderToolWebSearch        = "mistral.web_search"
	mistralProviderToolWebSearchPremium = "mistral.web_search_premium"
	mistralProviderToolDocumentLibrary  = "mistral.document_library"
)

// Tools provides factories for Mistral server-side retrieval tools.
var Tools = struct {
	WebSearch        func() sigma.Tool
	WebSearchPremium func() sigma.Tool
	DocumentLibrary  func(libraryIDs ...string) sigma.Tool
}{
	WebSearch:        webSearchTool,
	WebSearchPremium: webSearchPremiumTool,
	DocumentLibrary:  documentLibraryTool,
}

func webSearchTool() sigma.Tool {
	return providerTool("web_search", mistralProviderToolWebSearch, nil)
}

func webSearchPremiumTool() sigma.Tool {
	return providerTool("web_search_premium", mistralProviderToolWebSearchPremium, nil)
}

func documentLibraryTool(libraryIDs ...string) sigma.Tool {
	return providerTool("document_library", mistralProviderToolDocumentLibrary, map[string]any{
		"library_ids": append([]string(nil), libraryIDs...),
	})
}

func providerTool(name string, providerType string, options map[string]any) sigma.Tool {
	if len(options) == 0 {
		options = nil
	}
	return sigma.Tool{
		Name:                   name,
		ProviderDefinedType:    providerType,
		ProviderDefinedOptions: options,
	}
}
