// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"sync"
	"testing"

	"github.com/wintermi/sigma"
)

type testTextProvider struct {
	api sigma.API
}

func (p testTextProvider) API() sigma.API {
	return p.api
}

func (p testTextProvider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	stream, writer := sigma.NewStream(ctx)
	go func() {
		_ = writer.Done(ctx, sigma.AssistantMessage{
			Model:      model.ID,
			Provider:   model.Provider,
			StopReason: sigma.StopReasonEndTurn,
		})
	}()
	return stream
}

type testImageProvider struct {
	api sigma.ImageAPI
}

func (p testImageProvider) API() sigma.ImageAPI {
	return p.api
}

func (p testImageProvider) Generate(context.Context, sigma.ImageModel, sigma.ImageRequest, sigma.Options) (sigma.AssistantImages, error) {
	return sigma.AssistantImages{}, nil
}

func TestRegistryRegistersProvidersAndModelsInOrder(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	openAIText := testTextProvider{api: sigma.APIOpenAIResponses}
	openAIImage := testImageProvider{api: "openai-images"}
	mistralText := testTextProvider{api: sigma.APIMistralConversations}

	if err := registry.RegisterTextProvider(sigma.ProviderOpenAI, openAIText); err != nil {
		t.Fatalf("RegisterTextProvider(openai) returned error: %v", err)
	}
	if err := registry.RegisterImageProvider(sigma.ProviderOpenAI, openAIImage); err != nil {
		t.Fatalf("RegisterImageProvider(openai) returned error: %v", err)
	}
	if err := registry.RegisterTextProvider(sigma.ProviderMistral, mistralText); err != nil {
		t.Fatalf("RegisterTextProvider(mistral) returned error: %v", err)
	}
	if err := registry.RegisterModel(sigma.Model{
		ID:       "gpt-custom",
		Provider: sigma.ProviderOpenAI,
		API:      sigma.APIOpenAIResponses,
	}); err != nil {
		t.Fatalf("RegisterModel(openai) returned error: %v", err)
	}
	if err := registry.RegisterModel(sigma.Model{
		ID:       "mistral-custom",
		Provider: sigma.ProviderMistral,
		API:      sigma.APIMistralConversations,
	}); err != nil {
		t.Fatalf("RegisterModel(mistral) returned error: %v", err)
	}

	providers := registry.ListProviders()
	if got, want := len(providers), 2; got != want {
		t.Fatalf("provider count = %d, want %d", got, want)
	}
	if got, want := providers[0].ID, sigma.ProviderOpenAI; got != want {
		t.Fatalf("first provider = %q, want %q", got, want)
	}
	if got, want := providers[0].ImageAPI, sigma.ImageAPI("openai-images"); got != want {
		t.Fatalf("openai image api = %q, want %q", got, want)
	}
	if got, want := providers[1].ID, sigma.ProviderMistral; got != want {
		t.Fatalf("second provider = %q, want %q", got, want)
	}

	models := registry.ListModels()
	if got, want := []sigma.ModelID{models[0].ID, models[1].ID}, []sigma.ModelID{"gpt-custom", "mistral-custom"}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("model order = %v, want %v", got, want)
	}
}

func TestRegistryDuplicateHandlingRequiresOverride(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := testTextProvider{api: sigma.APIOpenAIResponses}
	if err := registry.RegisterTextProvider(sigma.ProviderOpenAI, provider); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterTextProvider(sigma.ProviderOpenAI, provider); err == nil {
		t.Fatal("duplicate provider registration returned nil error")
	}
	if err := registry.RegisterTextProvider(sigma.ProviderOpenAI, provider, sigma.WithOverride()); err != nil {
		t.Fatalf("override provider registration returned error: %v", err)
	}

	model := sigma.Model{
		ID:       "gpt-custom",
		Provider: sigma.ProviderOpenAI,
		API:      sigma.APIOpenAIResponses,
		Name:     "first",
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	model.Name = "second"
	if err := registry.RegisterModel(model); err == nil {
		t.Fatal("duplicate model registration returned nil error")
	}
	if err := registry.RegisterModel(model, sigma.WithOverride()); err != nil {
		t.Fatalf("override model registration returned error: %v", err)
	}

	got, ok := registry.Model(sigma.ProviderOpenAI, "gpt-custom")
	if !ok {
		t.Fatal("model was not registered")
	}
	if got.Name != "second" {
		t.Fatalf("model name = %q, want %q", got.Name, "second")
	}
	if got, want := len(registry.ListModels()), 1; got != want {
		t.Fatalf("model count = %d, want %d", got, want)
	}
}

func TestRegistryValidatesModelProviderAPI(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	model := sigma.Model{
		ID:       "gpt-custom",
		Provider: sigma.ProviderOpenAI,
		API:      sigma.APIOpenAIResponses,
	}
	if err := registry.RegisterModel(model); err == nil {
		t.Fatal("model with missing provider returned nil error")
	}
	if err := registry.RegisterModel(model, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("metadata-only model registration returned error: %v", err)
	}
	if err := registry.RegisterTextProvider(sigma.ProviderMistral, testTextProvider{api: sigma.APIMistralConversations}); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(sigma.Model{
		ID:       "wrong-api",
		Provider: sigma.ProviderMistral,
		API:      sigma.APIOpenAIResponses,
	}); err == nil {
		t.Fatal("model with mismatched provider api returned nil error")
	}
}

func TestRegistryAllowsCustomOpenAICompatibleModel(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("local-vllm")
	if err := registry.RegisterTextProvider(providerID, testTextProvider{api: sigma.APIOpenAIResponses}); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(sigma.Model{
		ID:               "acme/llama-4",
		Provider:         providerID,
		API:              sigma.APIOpenAIResponses,
		ContextWindow:    131072,
		SupportsTools:    true,
		DefaultTransport: sigma.TransportSSE,
	}); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	model, ok := registry.Model(providerID, "acme/llama-4")
	if !ok {
		t.Fatal("custom model was not registered")
	}
	if model.API != sigma.APIOpenAIResponses {
		t.Fatalf("custom model api = %q, want %q", model.API, sigma.APIOpenAIResponses)
	}
}

func TestRegistryImageModelRegistration(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("image-lab")
	imageAPI := sigma.ImageAPI("openai-images")
	if err := registry.RegisterImageProvider(providerID, testImageProvider{api: imageAPI}); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	if err := registry.RegisterImageModel(sigma.ImageModel{
		ID:               "image-custom",
		Provider:         providerID,
		API:              imageAPI,
		SupportedSizes:   []string{"1024x1024"},
		SupportedFormats: []string{"image/png"},
	}); err != nil {
		t.Fatalf("RegisterImageModel returned error: %v", err)
	}
	if err := registry.RegisterImageModel(sigma.ImageModel{
		ID:       "wrong-image-api",
		Provider: providerID,
		API:      "other-images",
	}); err == nil {
		t.Fatal("image model with mismatched api returned nil error")
	}

	model, ok := registry.ImageModel(providerID, "image-custom")
	if !ok {
		t.Fatal("image model was not registered")
	}
	if got, want := model.SupportedFormats[0], "image/png"; got != want {
		t.Fatalf("image format = %q, want %q", got, want)
	}
}

func TestRegistryReturnsDefensiveModelCopies(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	model := sigma.Model{
		ID:             "gpt-custom",
		Provider:       sigma.ProviderOpenAI,
		API:            sigma.APIOpenAIResponses,
		ThinkingLevels: []sigma.ThinkingLevel{sigma.ThinkingLevelLow},
		UnsupportedThinkingLevels: []sigma.ThinkingLevel{
			sigma.ThinkingLevelOff,
		},
		ProviderMetadata: map[string]any{"family": "gpt"},
	}
	if err := registry.RegisterModel(model, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	listed := registry.ListModels()
	listed[0].ThinkingLevels[0] = sigma.ThinkingLevelHigh
	listed[0].UnsupportedThinkingLevels[0] = sigma.ThinkingLevelMedium
	listed[0].ProviderMetadata["family"] = "mutated"

	got, ok := registry.Model(sigma.ProviderOpenAI, "gpt-custom")
	if !ok {
		t.Fatal("model was not registered")
	}
	if got.ThinkingLevels[0] != sigma.ThinkingLevelLow {
		t.Fatalf("thinking level = %q, want %q", got.ThinkingLevels[0], sigma.ThinkingLevelLow)
	}
	if got.UnsupportedThinkingLevels[0] != sigma.ThinkingLevelOff {
		t.Fatalf("unsupported thinking level = %q, want %q", got.UnsupportedThinkingLevels[0], sigma.ThinkingLevelOff)
	}
	if got.ProviderMetadata["family"] != "gpt" {
		t.Fatalf("provider metadata family = %q, want %q", got.ProviderMetadata["family"], "gpt")
	}
}

func TestRegistryIsolation(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := registry.RegisterModel(sigma.Model{
		ID:       "custom-only",
		Provider: sigma.ProviderCustom,
		API:      sigma.APIOpenAICompletions,
	}, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	clone := registry.Clone()
	if err := clone.RegisterModel(sigma.Model{
		ID:       "clone-only",
		Provider: sigma.ProviderCustom,
		API:      sigma.APIOpenAICompletions,
	}, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("clone RegisterModel returned error: %v", err)
	}

	if _, ok := registry.Model(sigma.ProviderCustom, "clone-only"); ok {
		t.Fatal("clone model leaked into original registry")
	}
	if _, ok := sigma.DefaultRegistry().Model(sigma.ProviderCustom, "custom-only"); ok {
		t.Fatal("custom model leaked into default registry")
	}

	client := sigma.New(sigma.WithRegistry(registry))
	if _, ok := client.Registry().Model(sigma.ProviderCustom, "custom-only"); !ok {
		t.Fatal("client did not use configured registry")
	}
}

func TestDefaultRegistryHasBuiltInMetadata(t *testing.T) {
	t.Parallel()

	registry := sigma.DefaultRegistry()
	if _, ok := registry.Model(sigma.ProviderOpenAI, "gpt-4o-mini"); !ok {
		t.Fatal("default registry missing built-in text model metadata")
	}
	if _, ok := registry.ImageModel(sigma.ProviderOpenAI, "gpt-image-1"); !ok {
		t.Fatal("default registry missing built-in image model metadata")
	}

	if err := registry.RegisterModel(sigma.Model{
		ID:       "test-only",
		Provider: sigma.ProviderCustom,
		API:      sigma.APIOpenAICompletions,
	}, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	if _, ok := sigma.DefaultRegistry().Model(sigma.ProviderCustom, "test-only"); ok {
		t.Fatal("mutating default registry clone changed package default registry")
	}
}

func TestRegistryConcurrentReads(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := registry.RegisterModel(sigma.Model{
		ID:       "gpt-custom",
		Provider: sigma.ProviderOpenAI,
		API:      sigma.APIOpenAIResponses,
	}, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if _, ok := registry.Model(sigma.ProviderOpenAI, "gpt-custom"); !ok {
					t.Error("registered model was not found")
				}
				if got := len(registry.ListModels()); got != 1 {
					t.Errorf("model count = %d, want 1", got)
				}
				_ = registry.Snapshot()
			}
		}()
	}
	wg.Wait()
}

func TestCompleteCollectsTextProviderStream(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	model := sigma.Model{ID: "gpt-custom", Provider: sigma.ProviderOpenAI, API: sigma.APIOpenAIResponses}
	if err := registry.RegisterTextProvider(sigma.ProviderOpenAI, testTextProvider{api: sigma.APIOpenAIResponses}); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(model); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	final, err := client.Complete(context.Background(), model, sigma.Request{})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.Model, model.ID; got != want {
		t.Fatalf("final model = %q, want %q", got, want)
	}
}
