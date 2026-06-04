// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package modeldata

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// Catalog is the reviewable input used by cmd/sigma-generate-models.
type Catalog struct {
	SchemaVersion   int              `json:"schemaVersion"`
	SnapshotDate    string           `json:"snapshotDate"`
	Sources         []Source         `json:"sources"`
	TextModels      []TextModel      `json:"textModels"`
	ImageModels     []ImageModel     `json:"imageModels"`
	EmbeddingModels []EmbeddingModel `json:"embeddingModels"`
}

// Source records where the stored metadata snapshot came from.
type Source struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// TextModel is generator input for root-package sigma.Model metadata.
type TextModel struct {
	ID                        string                   `json:"id"`
	Name                      string                   `json:"name"`
	Provider                  string                   `json:"provider"`
	API                       string                   `json:"api"`
	BaseURL                   string                   `json:"baseURL"`
	SupportedInputs           []string                 `json:"supportedInputs"`
	SupportsTools             bool                     `json:"supportsTools"`
	SupportsThinking          bool                     `json:"supportsThinking"`
	ThinkingLevels            []string                 `json:"thinkingLevels,omitempty"`
	ThinkingLevelMap          map[string]string        `json:"thinkingLevelMap,omitempty"`
	UnsupportedThinkingLevels []string                 `json:"unsupportedThinkingLevels,omitempty"`
	Cost                      Cost                     `json:"cost"`
	ContextWindow             int                      `json:"contextWindow"`
	MaxOutputTokens           int                      `json:"maxOutputTokens"`
	Headers                   map[string]string        `json:"headers,omitempty"`
	AuthEnvNames              []string                 `json:"authEnvNames"`
	DefaultTransport          string                   `json:"defaultTransport"`
	OpenAICompletionsCompat   *OpenAICompletionsCompat `json:"openAICompletionsCompat,omitempty"`
	AnthropicMessagesCompat   *AnthropicMessagesCompat `json:"anthropicMessagesCompat,omitempty"`
	AzureOpenAIResponses      *AzureOpenAIResponses    `json:"azureOpenAIResponses,omitempty"`
	OpenAICodexResponses      *OpenAICodexResponses    `json:"openAICodexResponses,omitempty"`
	ProviderMetadata          map[string]any           `json:"providerMetadata,omitempty"`
}

// ImageModel is generator input for root-package sigma.ImageModel metadata.
type ImageModel struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Provider         string            `json:"provider"`
	API              string            `json:"api"`
	BaseURL          string            `json:"baseURL"`
	MaxWidth         int               `json:"maxWidth"`
	MaxHeight        int               `json:"maxHeight"`
	SupportedSizes   []string          `json:"supportedSizes"`
	SupportedFormats []string          `json:"supportedFormats"`
	Cost             ImageCost         `json:"cost"`
	Headers          map[string]string `json:"headers,omitempty"`
	AuthEnvNames     []string          `json:"authEnvNames"`
	ProviderMetadata map[string]any    `json:"providerMetadata,omitempty"`
}

// EmbeddingModel is generator input for root-package sigma.EmbeddingModel metadata.
type EmbeddingModel struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Provider            string            `json:"provider"`
	API                 string            `json:"api"`
	BaseURL             string            `json:"baseURL"`
	DefaultDimensions   int               `json:"defaultDimensions"`
	MinDimensions       int               `json:"minDimensions"`
	MaxDimensions       int               `json:"maxDimensions"`
	MaxInputTokens      int               `json:"maxInputTokens"`
	MaxBatchInputs      int               `json:"maxBatchInputs,omitempty"`
	MaxBatchBytes       int               `json:"maxBatchBytes,omitempty"`
	InputCostPerMillion float64           `json:"inputCostPerMillion"`
	Currency            string            `json:"currency"`
	Headers             map[string]string `json:"headers,omitempty"`
	AuthEnvNames        []string          `json:"authEnvNames"`
	ProviderMetadata    map[string]any    `json:"providerMetadata,omitempty"`
}

// Cost records text model pricing in currency units per one million tokens.
type Cost struct {
	InputPerMillion           float64 `json:"inputPerMillion"`
	OutputPerMillion          float64 `json:"outputPerMillion"`
	CacheReadInputPerMillion  float64 `json:"cacheReadInputPerMillion,omitempty"`
	CacheWriteInputPerMillion float64 `json:"cacheWriteInputPerMillion,omitempty"`
	Currency                  string  `json:"currency"`
}

// ImageCost records image pricing notes for metadata discovery.
type ImageCost struct {
	Unit     string             `json:"unit"`
	Currency string             `json:"currency"`
	Values   map[string]float64 `json:"values"`
}

// OpenAICompletionsCompat mirrors sigma.OpenAICompletionsCompat without
// importing the root package from this internal generator package.
type OpenAICompletionsCompat struct {
	SupportsStore                               string                            `json:"supportsStore,omitempty"`
	SupportsDeveloperRole                       string                            `json:"supportsDeveloperRole,omitempty"`
	ReasoningFormat                             string                            `json:"reasoningFormat,omitempty"`
	SupportsReasoningEffort                     string                            `json:"supportsReasoningEffort,omitempty"`
	SupportsStreamingUsage                      string                            `json:"supportsStreamingUsage,omitempty"`
	SupportsStrictTools                         string                            `json:"supportsStrictTools,omitempty"`
	SupportsToolStream                          string                            `json:"supportsToolStream,omitempty"`
	MaxTokensField                              string                            `json:"maxTokensField,omitempty"`
	CacheControlFormat                          string                            `json:"cacheControlFormat,omitempty"`
	SupportsSessionAffinity                     string                            `json:"supportsSessionAffinity,omitempty"`
	RequiresToolResultName                      string                            `json:"requiresToolResultName,omitempty"`
	RequiresAssistantAfterToolResult            string                            `json:"requiresAssistantAfterToolResult,omitempty"`
	RequiresReasoningContentOnAssistantMessages string                            `json:"requiresReasoningContentOnAssistantMessages,omitempty"`
	OpenRouterRouting                           *OpenRouterRoutingPreference      `json:"openRouterRouting,omitempty"`
	VercelAIGatewayRouting                      *VercelAIGatewayRoutingPreference `json:"vercelAIGatewayRouting,omitempty"`
}

// AnthropicMessagesCompat mirrors sigma.AnthropicMessagesCompat.
type AnthropicMessagesCompat struct {
	SupportsEagerToolInputStreaming string `json:"supportsEagerToolInputStreaming,omitempty"`
	SupportsLongCacheRetention      string `json:"supportsLongCacheRetention,omitempty"`
	SupportsSessionAffinity         string `json:"supportsSessionAffinity,omitempty"`
	SupportsCacheControlOnTools     string `json:"supportsCacheControlOnTools,omitempty"`
	SupportsEmptyThinkingSignature  string `json:"supportsEmptyThinkingSignature,omitempty"`
	SupportsTemperature             string `json:"supportsTemperature,omitempty"`
	ThinkingFormat                  string `json:"thinkingFormat,omitempty"`
}

// OpenRouterRoutingPreference mirrors sigma.OpenRouterRoutingPreference.
type OpenRouterRoutingPreference struct {
	Order                  []string       `json:"order,omitempty"`
	Only                   []string       `json:"only,omitempty"`
	Ignore                 []string       `json:"ignore,omitempty"`
	AllowFallbacks         *bool          `json:"allow_fallbacks,omitempty"`
	RequireParameters      *bool          `json:"require_parameters,omitempty"`
	DataCollection         string         `json:"data_collection,omitempty"`
	ZDR                    *bool          `json:"zdr,omitempty"`
	EnforceDistillableText *bool          `json:"enforce_distillable_text,omitempty"`
	Quantizations          []string       `json:"quantizations,omitempty"`
	MaxPrice               map[string]any `json:"max_price,omitempty"`
	PreferredMinThroughput any            `json:"preferred_min_throughput,omitempty"`
	PreferredMaxLatency    any            `json:"preferred_max_latency,omitempty"`
	Sort                   any            `json:"sort,omitempty"`
}

// VercelAIGatewayRoutingPreference mirrors sigma.VercelAIGatewayRoutingPreference.
type VercelAIGatewayRoutingPreference struct {
	Order   []string       `json:"order,omitempty"`
	Only    []string       `json:"only,omitempty"`
	Models  []string       `json:"models,omitempty"`
	Caching string         `json:"caching,omitempty"`
	BYOK    map[string]any `json:"byok,omitempty"`
}

// AzureOpenAIResponses mirrors sigma.AzureOpenAIResponsesConfig.
type AzureOpenAIResponses struct {
	Endpoint         string `json:"endpoint,omitempty"`
	Deployment       string `json:"deployment,omitempty"`
	APIVersion       string `json:"apiVersion,omitempty"`
	APIKeyEnvVar     string `json:"apiKeyEnvVar,omitempty"`
	CredentialSource string `json:"credentialSource,omitempty"`
}

// OpenAICodexResponses mirrors sigma.OpenAICodexResponsesConfig.
type OpenAICodexResponses struct {
	Model string `json:"model,omitempty"`
}

// Load reads and validates a catalog JSON file.
func Load(path string) (Catalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return Catalog{}, err
	}
	defer file.Close()
	return Decode(file)
}

// Decode reads and validates catalog JSON.
func Decode(r io.Reader) (Catalog, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()

	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Catalog{}, fmt.Errorf("catalog contains trailing JSON data")
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	catalog.Sort()
	return catalog, nil
}

// Sort returns metadata in generated output order.
func (c *Catalog) Sort() {
	sort.SliceStable(c.TextModels, func(i, j int) bool {
		return textLess(c.TextModels[i], c.TextModels[j])
	})
	sort.SliceStable(c.ImageModels, func(i, j int) bool {
		return imageLess(c.ImageModels[i], c.ImageModels[j])
	})
	sort.SliceStable(c.EmbeddingModels, func(i, j int) bool {
		return embeddingLess(c.EmbeddingModels[i], c.EmbeddingModels[j])
	})
}

// Validate reports clear generator failures for missing or duplicate metadata.
func (c Catalog) Validate() error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must be 1")
	}
	if _, err := time.Parse("2006-01-02", c.SnapshotDate); err != nil {
		return fmt.Errorf("snapshotDate must use YYYY-MM-DD: %w", err)
	}
	if len(c.Sources) == 0 {
		return fmt.Errorf("sources must contain at least one source URL")
	}
	for i, source := range c.Sources {
		if err := validateSource(source); err != nil {
			return fmt.Errorf("sources[%d]: %w", i, err)
		}
	}
	if len(c.TextModels) == 0 {
		return fmt.Errorf("textModels must contain at least one model")
	}
	if len(c.ImageModels) == 0 {
		return fmt.Errorf("imageModels must contain at least one model")
	}
	if len(c.EmbeddingModels) == 0 {
		return fmt.Errorf("embeddingModels must contain at least one model")
	}

	seenText := map[string]struct{}{}
	for i, model := range c.TextModels {
		if err := validateTextModel(model); err != nil {
			return fmt.Errorf("textModels[%d] %q: %w", i, model.ID, err)
		}
		key := model.Provider + "\x00" + model.API + "\x00" + model.ID
		if _, ok := seenText[key]; ok {
			return fmt.Errorf("textModels[%d] %q: duplicate provider/api/id", i, model.ID)
		}
		seenText[key] = struct{}{}
	}

	seenImages := map[string]struct{}{}
	for i, model := range c.ImageModels {
		if err := validateImageModel(model); err != nil {
			return fmt.Errorf("imageModels[%d] %q: %w", i, model.ID, err)
		}
		key := model.Provider + "\x00" + model.API + "\x00" + model.ID
		if _, ok := seenImages[key]; ok {
			return fmt.Errorf("imageModels[%d] %q: duplicate provider/api/id", i, model.ID)
		}
		seenImages[key] = struct{}{}
	}
	seenEmbeddings := map[string]struct{}{}
	for i, model := range c.EmbeddingModels {
		if err := validateEmbeddingModel(model); err != nil {
			return fmt.Errorf("embeddingModels[%d] %q: %w", i, model.ID, err)
		}
		key := model.Provider + "\x00" + model.API + "\x00" + model.ID
		if _, ok := seenEmbeddings[key]; ok {
			return fmt.Errorf("embeddingModels[%d] %q: duplicate provider/api/id", i, model.ID)
		}
		seenEmbeddings[key] = struct{}{}
	}
	return nil
}

func validateSource(source Source) error {
	if source.Name == "" {
		return fmt.Errorf("name is required")
	}
	if source.URL == "" {
		return fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(source.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("url must be absolute")
	}
	return nil
}

func validateTextModel(model TextModel) error {
	if err := validateCommonModel(model.ID, model.Name, model.Provider, model.API, model.BaseURL, model.AuthEnvNames); err != nil {
		return err
	}
	if model.ContextWindow <= 0 {
		return fmt.Errorf("contextWindow is required")
	}
	if model.MaxOutputTokens <= 0 {
		return fmt.Errorf("maxOutputTokens is required")
	}
	if len(model.SupportedInputs) == 0 {
		return fmt.Errorf("supportedInputs is required")
	}
	if err := validateCost(model.Cost); err != nil {
		return err
	}
	return nil
}

func validateImageModel(model ImageModel) error {
	if err := validateCommonModel(model.ID, model.Name, model.Provider, model.API, model.BaseURL, model.AuthEnvNames); err != nil {
		return err
	}
	if model.MaxWidth <= 0 {
		return fmt.Errorf("maxWidth is required")
	}
	if model.MaxHeight <= 0 {
		return fmt.Errorf("maxHeight is required")
	}
	if len(model.SupportedSizes) == 0 {
		return fmt.Errorf("supportedSizes is required")
	}
	if len(model.SupportedFormats) == 0 {
		return fmt.Errorf("supportedFormats is required")
	}
	if model.Cost.Unit == "" {
		return fmt.Errorf("cost.unit is required")
	}
	if model.Cost.Currency == "" {
		return fmt.Errorf("cost.currency is required")
	}
	if len(model.Cost.Values) == 0 {
		return fmt.Errorf("cost.values is required")
	}
	return nil
}

func validateEmbeddingModel(model EmbeddingModel) error {
	if err := validateCommonModel(model.ID, model.Name, model.Provider, model.API, model.BaseURL, model.AuthEnvNames); err != nil {
		return err
	}
	if model.DefaultDimensions <= 0 {
		return fmt.Errorf("defaultDimensions must be positive")
	}
	if model.MinDimensions <= 0 {
		return fmt.Errorf("minDimensions must be positive")
	}
	if model.MaxDimensions <= 0 {
		return fmt.Errorf("maxDimensions must be positive")
	}
	if model.MinDimensions > model.MaxDimensions {
		return fmt.Errorf("minDimensions must be less than or equal to maxDimensions")
	}
	if model.DefaultDimensions < model.MinDimensions || model.DefaultDimensions > model.MaxDimensions {
		return fmt.Errorf("defaultDimensions must be within supported dimensions")
	}
	if model.MaxInputTokens <= 0 {
		return fmt.Errorf("maxInputTokens must be positive")
	}
	if model.MaxBatchInputs < 0 {
		return fmt.Errorf("maxBatchInputs must be non-negative")
	}
	if model.MaxBatchBytes < 0 {
		return fmt.Errorf("maxBatchBytes must be non-negative")
	}
	if model.InputCostPerMillion < 0 {
		return fmt.Errorf("inputCostPerMillion must be non-negative")
	}
	if model.Currency == "" {
		return fmt.Errorf("currency is required")
	}
	return nil
}

func validateCommonModel(id, name, provider, api, baseURL string, authEnvNames []string) error {
	if id == "" {
		return fmt.Errorf("id is required")
	}
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if provider == "" {
		return fmt.Errorf("provider is required")
	}
	if api == "" {
		return fmt.Errorf("api is required")
	}
	if baseURL == "" {
		return fmt.Errorf("baseURL is required")
	}
	if len(authEnvNames) == 0 {
		return fmt.Errorf("authEnvNames is required")
	}
	return nil
}

func validateCost(cost Cost) error {
	if cost.Currency == "" {
		return fmt.Errorf("cost.currency is required")
	}
	if cost.InputPerMillion < 0 {
		return fmt.Errorf("cost.inputPerMillion must be non-negative")
	}
	if cost.OutputPerMillion < 0 {
		return fmt.Errorf("cost.outputPerMillion must be non-negative")
	}
	return nil
}

func textLess(a, b TextModel) bool {
	return strings.Compare(sortKey(a.Provider, a.API, a.ID), sortKey(b.Provider, b.API, b.ID)) < 0
}

func imageLess(a, b ImageModel) bool {
	return strings.Compare(sortKey(a.Provider, a.API, a.ID), sortKey(b.Provider, b.API, b.ID)) < 0
}

func embeddingLess(a, b EmbeddingModel) bool {
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	if a.API != b.API {
		return a.API < b.API
	}
	return a.ID < b.ID
}

func sortKey(provider, api, id string) string {
	return provider + "\x00" + api + "\x00" + id
}
