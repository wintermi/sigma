// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

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

type testEmbeddingProvider struct {
	api sigma.EmbeddingAPI
}

func (p testEmbeddingProvider) API() sigma.EmbeddingAPI {
	return p.api
}

func (p testEmbeddingProvider) Embed(context.Context, sigma.EmbeddingModel, sigma.EmbeddingRequest, sigma.Options) (sigma.Embeddings, error) {
	return sigma.Embeddings{}, nil
}

type testTextModelSource struct {
	mu     sync.Mutex
	models []sigma.Model
	err    error
}

type testImageModelSource struct {
	mu     sync.Mutex
	models []sigma.ImageModel
	err    error
}

type testEmbeddingModelSource struct {
	mu     sync.Mutex
	models []sigma.EmbeddingModel
	err    error
}

func (s *testTextModelSource) TextModels(context.Context) ([]sigma.Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return nil, s.err
	}
	models := make([]sigma.Model, len(s.models))
	copy(models, s.models)
	return models, nil
}

func (s *testTextModelSource) Set(models ...sigma.Model) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.models = append([]sigma.Model(nil), models...)
}

func (s *testImageModelSource) ImageModels(context.Context) ([]sigma.ImageModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return nil, s.err
	}
	models := make([]sigma.ImageModel, len(s.models))
	copy(models, s.models)
	return models, nil
}

func (s *testImageModelSource) Set(models ...sigma.ImageModel) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.models = append([]sigma.ImageModel(nil), models...)
}

func (s *testEmbeddingModelSource) EmbeddingModels(context.Context) ([]sigma.EmbeddingModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return nil, s.err
	}
	models := make([]sigma.EmbeddingModel, len(s.models))
	copy(models, s.models)
	return models, nil
}

func (s *testEmbeddingModelSource) Set(models ...sigma.EmbeddingModel) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.models = append([]sigma.EmbeddingModel(nil), models...)
}

func TestRegistryRegistersProvidersAndModelsInOrder(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	openAIText := testTextProvider{api: sigma.APIOpenAIResponses}
	openAIImage := testImageProvider{api: "openai-images"}
	openAIEmbedding := testEmbeddingProvider{api: sigma.EmbeddingAPIOpenAIEmbeddings}
	mistralText := testTextProvider{api: sigma.APIMistralConversations}

	if err := registry.RegisterTextProvider(sigma.ProviderOpenAI, openAIText); err != nil {
		t.Fatalf("RegisterTextProvider(openai) returned error: %v", err)
	}
	if err := registry.RegisterImageProvider(sigma.ProviderOpenAI, openAIImage); err != nil {
		t.Fatalf("RegisterImageProvider(openai) returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingProvider(sigma.ProviderOpenAI, openAIEmbedding); err != nil {
		t.Fatalf("RegisterEmbeddingProvider(openai) returned error: %v", err)
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
	if got, want := providers[0].EmbeddingAPI, sigma.EmbeddingAPIOpenAIEmbeddings; got != want {
		t.Fatalf("openai embedding api = %q, want %q", got, want)
	}
	if got, want := providers[1].ID, sigma.ProviderMistral; got != want {
		t.Fatalf("second provider = %q, want %q", got, want)
	}

	models := registry.ListModels()
	if got, want := []sigma.ModelID{models[0].ID, models[1].ID}, []sigma.ModelID{"gpt-custom", "mistral-custom"}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("model order = %v, want %v", got, want)
	}
}

func TestRegistryTextModelSourceDuplicateHandlingRequiresOverride(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic")
	source := &testTextModelSource{}
	if err := registry.RegisterTextModelSource(provider, source); err != nil {
		t.Fatalf("RegisterTextModelSource returned error: %v", err)
	}
	if err := registry.RegisterTextModelSource(provider, source); err == nil {
		t.Fatal("duplicate text model source registration returned nil error")
	}
	if err := registry.RegisterTextModelSource(provider, source, sigma.WithOverride()); err != nil {
		t.Fatalf("override text model source registration returned error: %v", err)
	}
}

func TestRegistryRefreshTextModelsReplacesOnlySourceOwnedModels(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic")
	if err := registry.RegisterTextProvider(provider, testTextProvider{api: sigma.APIOpenAICompletions}); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	if err := registry.RegisterModel(sigma.Model{
		ID:       "manual",
		Provider: provider,
		API:      sigma.APIOpenAICompletions,
		Name:     "Manual",
	}); err != nil {
		t.Fatalf("RegisterModel(manual) returned error: %v", err)
	}
	source := &testTextModelSource{}
	source.Set(
		sigma.Model{ID: "source-old", Provider: provider, API: sigma.APIOpenAICompletions},
		sigma.Model{ID: "source-keep", Provider: provider, API: sigma.APIOpenAICompletions},
	)
	if err := registry.RegisterTextModelSource(provider, source); err != nil {
		t.Fatalf("RegisterTextModelSource returned error: %v", err)
	}
	if err := registry.RefreshTextModels(context.Background(), provider); err != nil {
		t.Fatalf("first RefreshTextModels returned error: %v", err)
	}

	source.Set(
		sigma.Model{ID: "source-new", Provider: provider, API: sigma.APIOpenAICompletions},
		sigma.Model{ID: "source-keep", Provider: provider, API: sigma.APIOpenAICompletions, Name: "updated"},
	)
	if err := registry.RefreshTextModels(context.Background(), provider); err != nil {
		t.Fatalf("second RefreshTextModels returned error: %v", err)
	}

	models := registry.ListModels()
	got := make([]sigma.ModelID, 0, len(models))
	for _, model := range models {
		got = append(got, model.ID)
	}
	want := []sigma.ModelID{"manual", "source-new", "source-keep"}
	if len(got) != len(want) {
		t.Fatalf("model ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("model ids = %v, want %v", got, want)
		}
	}
	if _, ok := registry.Model(provider, "source-old"); ok {
		t.Fatal("stale source-owned model remained registered")
	}
	updated, ok := registry.Model(provider, "source-keep")
	if !ok {
		t.Fatal("updated source model was not registered")
	}
	if got, want := updated.Name, "updated"; got != want {
		t.Fatalf("updated source model name = %q, want %q", got, want)
	}
}

func TestRegistryRefreshTextModelsPreservesCallerOwnedConflicts(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic")
	if err := registry.RegisterTextProvider(provider, testTextProvider{api: sigma.APIOpenAICompletions}); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	manual := sigma.Model{ID: "manual", Provider: provider, API: sigma.APIOpenAICompletions, Name: "manual"}
	if err := registry.RegisterModel(manual); err != nil {
		t.Fatalf("RegisterModel(manual) returned error: %v", err)
	}
	source := &testTextModelSource{}
	source.Set(sigma.Model{ID: "manual", Provider: provider, API: sigma.APIOpenAICompletions, Name: "source"})
	if err := registry.RegisterTextModelSource(provider, source); err != nil {
		t.Fatalf("RegisterTextModelSource returned error: %v", err)
	}

	if err := registry.RefreshTextModels(context.Background(), provider); err == nil {
		t.Fatal("RefreshTextModels with caller-owned id conflict returned nil error")
	}
	got, ok := registry.Model(provider, "manual")
	if !ok {
		t.Fatal("caller-owned model was removed after refresh conflict")
	}
	if got.Name != "manual" {
		t.Fatalf("caller-owned model name = %q, want manual", got.Name)
	}
}

func TestRegistryRegisterModelOverrideTakesSourceOwnership(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic")
	if err := registry.RegisterTextProvider(provider, testTextProvider{api: sigma.APIOpenAICompletions}); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	source := &testTextModelSource{}
	source.Set(sigma.Model{ID: "source", Provider: provider, API: sigma.APIOpenAICompletions, Name: "source"})
	if err := registry.RegisterTextModelSource(provider, source); err != nil {
		t.Fatalf("RegisterTextModelSource returned error: %v", err)
	}
	if err := registry.RefreshTextModels(context.Background(), provider); err != nil {
		t.Fatalf("RefreshTextModels returned error: %v", err)
	}

	if err := registry.RegisterModel(sigma.Model{
		ID:       "source",
		Provider: provider,
		API:      sigma.APIOpenAICompletions,
		Name:     "manual",
	}, sigma.WithOverride()); err != nil {
		t.Fatalf("RegisterModel override returned error: %v", err)
	}
	if err := registry.RefreshTextModels(context.Background(), provider); err == nil {
		t.Fatal("RefreshTextModels after caller override returned nil conflict")
	}
	got, ok := registry.Model(provider, "source")
	if !ok {
		t.Fatal("caller-overridden source model was removed")
	}
	if got.Name != "manual" {
		t.Fatalf("model name = %q, want manual", got.Name)
	}
}

func TestRegistryRefreshTextModelsValidatesBeforeApplying(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic")
	if err := registry.RegisterTextProvider(provider, testTextProvider{api: sigma.APIOpenAICompletions}); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	source := &testTextModelSource{}
	source.Set(sigma.Model{ID: "good", Provider: provider, API: sigma.APIOpenAICompletions})
	if err := registry.RegisterTextModelSource(provider, source); err != nil {
		t.Fatalf("RegisterTextModelSource returned error: %v", err)
	}
	if err := registry.RefreshTextModels(context.Background(), provider); err != nil {
		t.Fatalf("initial RefreshTextModels returned error: %v", err)
	}

	source.Set(
		sigma.Model{ID: "bad-api", Provider: provider, API: sigma.API("wrong-api")},
		sigma.Model{ID: "new", Provider: provider, API: sigma.APIOpenAICompletions},
	)
	if err := registry.RefreshTextModels(context.Background(), provider); err == nil {
		t.Fatal("RefreshTextModels with invalid model returned nil error")
	}
	if _, ok := registry.Model(provider, "good"); !ok {
		t.Fatal("existing source model was removed after failed refresh")
	}
	if _, ok := registry.Model(provider, "new"); ok {
		t.Fatal("new model from failed refresh was registered")
	}
}

func TestRegistryRefreshTextModelsRejectsDuplicateAndWrongProviderModels(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		models []sigma.Model
	}{
		{
			name: "duplicate model",
			models: []sigma.Model{
				{ID: "same", Provider: sigma.ProviderID("dynamic"), API: sigma.APIOpenAICompletions},
				{ID: "same", Provider: sigma.ProviderID("dynamic"), API: sigma.APIOpenAICompletions},
			},
		},
		{
			name: "wrong provider",
			models: []sigma.Model{
				{ID: "other", Provider: sigma.ProviderID("other"), API: sigma.APIOpenAICompletions},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := sigma.NewRegistry()
			provider := sigma.ProviderID("dynamic")
			if err := registry.RegisterTextProvider(provider, testTextProvider{api: sigma.APIOpenAICompletions}); err != nil {
				t.Fatalf("RegisterTextProvider returned error: %v", err)
			}
			source := &testTextModelSource{}
			source.Set(tt.models...)
			if err := registry.RegisterTextModelSource(provider, source); err != nil {
				t.Fatalf("RegisterTextModelSource returned error: %v", err)
			}
			if err := registry.RefreshTextModels(context.Background(), provider); err == nil {
				t.Fatal("RefreshTextModels returned nil error")
			}
			if got := len(registry.ListModels()); got != 0 {
				t.Fatalf("model count = %d, want 0", got)
			}
		})
	}
}

func TestRegistryRefreshTextModelsSupportsMetadataOnlySources(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("metadata-only")
	source := &testTextModelSource{}
	source.Set(sigma.Model{ID: "listed", Provider: provider, API: sigma.APIOpenAICompletions})
	if err := registry.RegisterTextModelSource(provider, source, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterTextModelSource returned error: %v", err)
	}
	if err := registry.RefreshTextModels(context.Background(), provider); err != nil {
		t.Fatalf("RefreshTextModels returned error: %v", err)
	}
	if _, ok := registry.Model(provider, "listed"); !ok {
		t.Fatal("metadata-only source model was not registered")
	}
}

func TestRegistryRefreshTextModelsJoinsErrorsAndAppliesSuccessfulSources(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	goodProvider := sigma.ProviderID("good")
	badProvider := sigma.ProviderID("bad")
	if err := registry.RegisterTextProvider(goodProvider, testTextProvider{api: sigma.APIOpenAICompletions}); err != nil {
		t.Fatalf("RegisterTextProvider(good) returned error: %v", err)
	}
	if err := registry.RegisterTextProvider(badProvider, testTextProvider{api: sigma.APIOpenAICompletions}); err != nil {
		t.Fatalf("RegisterTextProvider(bad) returned error: %v", err)
	}
	good := &testTextModelSource{}
	good.Set(sigma.Model{ID: "fresh", Provider: goodProvider, API: sigma.APIOpenAICompletions})
	bad := &testTextModelSource{err: context.Canceled}
	if err := registry.RegisterTextModelSource(goodProvider, good); err != nil {
		t.Fatalf("RegisterTextModelSource(good) returned error: %v", err)
	}
	if err := registry.RegisterTextModelSource(badProvider, bad); err != nil {
		t.Fatalf("RegisterTextModelSource(bad) returned error: %v", err)
	}

	if err := registry.RefreshTextModels(context.Background()); err == nil {
		t.Fatal("RefreshTextModels returned nil error")
	}
	if _, ok := registry.Model(goodProvider, "fresh"); !ok {
		t.Fatal("successful source was not applied when another source failed")
	}
}

func TestRegistryRefreshTextModelsCancellationLeavesExistingModels(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic")
	if err := registry.RegisterTextProvider(provider, testTextProvider{api: sigma.APIOpenAICompletions}); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	source := &testTextModelSource{}
	source.Set(sigma.Model{ID: "good", Provider: provider, API: sigma.APIOpenAICompletions})
	if err := registry.RegisterTextModelSource(provider, source); err != nil {
		t.Fatalf("RegisterTextModelSource returned error: %v", err)
	}
	if err := registry.RefreshTextModels(context.Background(), provider); err != nil {
		t.Fatalf("initial RefreshTextModels returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := registry.RegisterTextModelSource(provider, sigma.TextModelSourceFunc(func(ctx context.Context) ([]sigma.Model, error) {
		return nil, ctx.Err()
	}), sigma.WithOverride()); err != nil {
		t.Fatalf("override RegisterTextModelSource returned error: %v", err)
	}
	if err := registry.RefreshTextModels(ctx, provider); err == nil {
		t.Fatal("RefreshTextModels with canceled context returned nil error")
	}
	if _, ok := registry.Model(provider, "good"); !ok {
		t.Fatal("existing source model was removed after canceled refresh")
	}
}

func TestClientRefreshTextModelsUpdatesConfiguredRegistry(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic")
	if err := registry.RegisterTextProvider(provider, testTextProvider{api: sigma.APIOpenAICompletions}); err != nil {
		t.Fatalf("RegisterTextProvider returned error: %v", err)
	}
	source := &testTextModelSource{}
	source.Set(sigma.Model{ID: "fresh", Provider: provider, API: sigma.APIOpenAICompletions})
	if err := registry.RegisterTextModelSource(provider, source); err != nil {
		t.Fatalf("RegisterTextModelSource returned error: %v", err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if err := client.RefreshTextModels(context.Background(), provider); err != nil {
		t.Fatalf("client RefreshTextModels returned error: %v", err)
	}
	models := client.Models()
	if got, want := len(models), 1; got != want {
		t.Fatalf("client model count = %d, want %d", got, want)
	}
	if got, want := models[0].ID, sigma.ModelID("fresh"); got != want {
		t.Fatalf("client model id = %q, want %q", got, want)
	}
}

func TestRegistryImageModelSourceDuplicateHandlingRequiresOverride(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-images")
	source := &testImageModelSource{}
	if err := registry.RegisterImageModelSource(provider, source); err != nil {
		t.Fatalf("RegisterImageModelSource returned error: %v", err)
	}
	if err := registry.RegisterImageModelSource(provider, source); err == nil {
		t.Fatal("duplicate image model source registration returned nil error")
	}
	if err := registry.RegisterImageModelSource(provider, source, sigma.WithOverride()); err != nil {
		t.Fatalf("override image model source registration returned error: %v", err)
	}
}

func TestRegistryRefreshImageModelsReplacesOnlySourceOwnedModels(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-images")
	if err := registry.RegisterImageProvider(provider, testImageProvider{api: sigma.ImageAPIOpenAIImages}); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	if err := registry.RegisterImageModel(sigma.ImageModel{
		ID:       "manual",
		Provider: provider,
		API:      sigma.ImageAPIOpenAIImages,
		Name:     "Manual",
	}); err != nil {
		t.Fatalf("RegisterImageModel(manual) returned error: %v", err)
	}
	source := &testImageModelSource{}
	source.Set(
		sigma.ImageModel{ID: "source-old", Provider: provider, API: sigma.ImageAPIOpenAIImages},
		sigma.ImageModel{ID: "source-keep", Provider: provider, API: sigma.ImageAPIOpenAIImages},
	)
	if err := registry.RegisterImageModelSource(provider, source); err != nil {
		t.Fatalf("RegisterImageModelSource returned error: %v", err)
	}
	if err := registry.RefreshImageModels(context.Background(), provider); err != nil {
		t.Fatalf("first RefreshImageModels returned error: %v", err)
	}

	source.Set(
		sigma.ImageModel{ID: "source-new", Provider: provider, API: sigma.ImageAPIOpenAIImages},
		sigma.ImageModel{ID: "source-keep", Provider: provider, API: sigma.ImageAPIOpenAIImages, Name: "updated"},
	)
	if err := registry.RefreshImageModels(context.Background(), provider); err != nil {
		t.Fatalf("second RefreshImageModels returned error: %v", err)
	}

	models := registry.ListImageModels()
	got := make([]sigma.ModelID, 0, len(models))
	for _, model := range models {
		got = append(got, model.ID)
	}
	want := []sigma.ModelID{"manual", "source-new", "source-keep"}
	if len(got) != len(want) {
		t.Fatalf("image model ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("image model ids = %v, want %v", got, want)
		}
	}
	if _, ok := registry.ImageModel(provider, "source-old"); ok {
		t.Fatal("stale source-owned image model remained registered")
	}
	updated, ok := registry.ImageModel(provider, "source-keep")
	if !ok {
		t.Fatal("updated source image model was not registered")
	}
	if got, want := updated.Name, "updated"; got != want {
		t.Fatalf("updated source image model name = %q, want %q", got, want)
	}
}

func TestRegistryRefreshImageModelsPreservesCallerOwnedConflicts(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-images")
	if err := registry.RegisterImageProvider(provider, testImageProvider{api: sigma.ImageAPIOpenAIImages}); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	manual := sigma.ImageModel{ID: "manual", Provider: provider, API: sigma.ImageAPIOpenAIImages, Name: "manual"}
	if err := registry.RegisterImageModel(manual); err != nil {
		t.Fatalf("RegisterImageModel(manual) returned error: %v", err)
	}
	source := &testImageModelSource{}
	source.Set(sigma.ImageModel{ID: "manual", Provider: provider, API: sigma.ImageAPIOpenAIImages, Name: "source"})
	if err := registry.RegisterImageModelSource(provider, source); err != nil {
		t.Fatalf("RegisterImageModelSource returned error: %v", err)
	}

	if err := registry.RefreshImageModels(context.Background(), provider); err == nil {
		t.Fatal("RefreshImageModels with caller-owned id conflict returned nil error")
	}
	got, ok := registry.ImageModel(provider, "manual")
	if !ok {
		t.Fatal("caller-owned image model was removed after refresh conflict")
	}
	if got.Name != "manual" {
		t.Fatalf("caller-owned image model name = %q, want manual", got.Name)
	}
}

func TestRegistryRegisterImageModelOverrideTakesSourceOwnership(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-images")
	if err := registry.RegisterImageProvider(provider, testImageProvider{api: sigma.ImageAPIOpenAIImages}); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	source := &testImageModelSource{}
	source.Set(sigma.ImageModel{ID: "source", Provider: provider, API: sigma.ImageAPIOpenAIImages, Name: "source"})
	if err := registry.RegisterImageModelSource(provider, source); err != nil {
		t.Fatalf("RegisterImageModelSource returned error: %v", err)
	}
	if err := registry.RefreshImageModels(context.Background(), provider); err != nil {
		t.Fatalf("RefreshImageModels returned error: %v", err)
	}

	if err := registry.RegisterImageModel(sigma.ImageModel{
		ID:       "source",
		Provider: provider,
		API:      sigma.ImageAPIOpenAIImages,
		Name:     "manual",
	}, sigma.WithOverride()); err != nil {
		t.Fatalf("RegisterImageModel override returned error: %v", err)
	}
	if err := registry.RefreshImageModels(context.Background(), provider); err == nil {
		t.Fatal("RefreshImageModels after caller override returned nil conflict")
	}
	got, ok := registry.ImageModel(provider, "source")
	if !ok {
		t.Fatal("caller-overridden source image model was removed")
	}
	if got.Name != "manual" {
		t.Fatalf("image model name = %q, want manual", got.Name)
	}
}

func TestRegistryRefreshImageModelsValidatesBeforeApplying(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-images")
	if err := registry.RegisterImageProvider(provider, testImageProvider{api: sigma.ImageAPIOpenAIImages}); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	source := &testImageModelSource{}
	source.Set(sigma.ImageModel{ID: "good", Provider: provider, API: sigma.ImageAPIOpenAIImages})
	if err := registry.RegisterImageModelSource(provider, source); err != nil {
		t.Fatalf("RegisterImageModelSource returned error: %v", err)
	}
	if err := registry.RefreshImageModels(context.Background(), provider); err != nil {
		t.Fatalf("initial RefreshImageModels returned error: %v", err)
	}

	source.Set(
		sigma.ImageModel{ID: "bad-api", Provider: provider, API: sigma.ImageAPIGoogleImages},
		sigma.ImageModel{ID: "new", Provider: provider, API: sigma.ImageAPIOpenAIImages},
	)
	if err := registry.RefreshImageModels(context.Background(), provider); err == nil {
		t.Fatal("RefreshImageModels with invalid image model returned nil error")
	}
	if _, ok := registry.ImageModel(provider, "good"); !ok {
		t.Fatal("existing source image model was removed after failed refresh")
	}
	if _, ok := registry.ImageModel(provider, "new"); ok {
		t.Fatal("new image model from failed refresh was registered")
	}
}

func TestRegistryRefreshImageModelsRejectsDuplicateAndWrongProviderModels(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		models []sigma.ImageModel
	}{
		{
			name: "duplicate image model",
			models: []sigma.ImageModel{
				{ID: "same", Provider: sigma.ProviderID("dynamic-images"), API: sigma.ImageAPIOpenAIImages},
				{ID: "same", Provider: sigma.ProviderID("dynamic-images"), API: sigma.ImageAPIOpenAIImages},
			},
		},
		{
			name: "wrong provider",
			models: []sigma.ImageModel{
				{ID: "other", Provider: sigma.ProviderID("other"), API: sigma.ImageAPIOpenAIImages},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := sigma.NewRegistry()
			provider := sigma.ProviderID("dynamic-images")
			if err := registry.RegisterImageProvider(provider, testImageProvider{api: sigma.ImageAPIOpenAIImages}); err != nil {
				t.Fatalf("RegisterImageProvider returned error: %v", err)
			}
			source := &testImageModelSource{}
			source.Set(tt.models...)
			if err := registry.RegisterImageModelSource(provider, source); err != nil {
				t.Fatalf("RegisterImageModelSource returned error: %v", err)
			}
			if err := registry.RefreshImageModels(context.Background(), provider); err == nil {
				t.Fatal("RefreshImageModels returned nil error")
			}
			if got := len(registry.ListImageModels()); got != 0 {
				t.Fatalf("image model count = %d, want 0", got)
			}
		})
	}
}

func TestRegistryRefreshImageModelsSupportsMetadataOnlySources(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("metadata-only-images")
	source := &testImageModelSource{}
	source.Set(sigma.ImageModel{ID: "listed", Provider: provider, API: sigma.ImageAPIOpenAIImages})
	if err := registry.RegisterImageModelSource(provider, source, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterImageModelSource returned error: %v", err)
	}
	if err := registry.RefreshImageModels(context.Background(), provider); err != nil {
		t.Fatalf("RefreshImageModels returned error: %v", err)
	}
	if _, ok := registry.ImageModel(provider, "listed"); !ok {
		t.Fatal("metadata-only source image model was not registered")
	}
}

func TestRegistryRefreshImageModelsJoinsErrorsAndAppliesSuccessfulSources(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	goodProvider := sigma.ProviderID("good-images")
	badProvider := sigma.ProviderID("bad-images")
	if err := registry.RegisterImageProvider(goodProvider, testImageProvider{api: sigma.ImageAPIOpenAIImages}); err != nil {
		t.Fatalf("RegisterImageProvider(good) returned error: %v", err)
	}
	if err := registry.RegisterImageProvider(badProvider, testImageProvider{api: sigma.ImageAPIOpenAIImages}); err != nil {
		t.Fatalf("RegisterImageProvider(bad) returned error: %v", err)
	}
	good := &testImageModelSource{}
	good.Set(sigma.ImageModel{ID: "fresh", Provider: goodProvider, API: sigma.ImageAPIOpenAIImages})
	bad := &testImageModelSource{err: context.Canceled}
	if err := registry.RegisterImageModelSource(goodProvider, good); err != nil {
		t.Fatalf("RegisterImageModelSource(good) returned error: %v", err)
	}
	if err := registry.RegisterImageModelSource(badProvider, bad); err != nil {
		t.Fatalf("RegisterImageModelSource(bad) returned error: %v", err)
	}

	if err := registry.RefreshImageModels(context.Background()); err == nil {
		t.Fatal("RefreshImageModels returned nil error")
	}
	if _, ok := registry.ImageModel(goodProvider, "fresh"); !ok {
		t.Fatal("successful image source was not applied when another source failed")
	}
}

func TestRegistryRefreshImageModelsCancellationLeavesExistingModels(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-images")
	if err := registry.RegisterImageProvider(provider, testImageProvider{api: sigma.ImageAPIOpenAIImages}); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	source := &testImageModelSource{}
	source.Set(sigma.ImageModel{ID: "good", Provider: provider, API: sigma.ImageAPIOpenAIImages})
	if err := registry.RegisterImageModelSource(provider, source); err != nil {
		t.Fatalf("RegisterImageModelSource returned error: %v", err)
	}
	if err := registry.RefreshImageModels(context.Background(), provider); err != nil {
		t.Fatalf("initial RefreshImageModels returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := registry.RegisterImageModelSource(provider, sigma.ImageModelSourceFunc(func(ctx context.Context) ([]sigma.ImageModel, error) {
		return nil, ctx.Err()
	}), sigma.WithOverride()); err != nil {
		t.Fatalf("override RegisterImageModelSource returned error: %v", err)
	}
	if err := registry.RefreshImageModels(ctx, provider); err == nil {
		t.Fatal("RefreshImageModels with canceled context returned nil error")
	}
	if _, ok := registry.ImageModel(provider, "good"); !ok {
		t.Fatal("existing source image model was removed after canceled refresh")
	}
}

func TestClientRefreshImageModelsUpdatesConfiguredRegistry(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-images")
	if err := registry.RegisterImageProvider(provider, testImageProvider{api: sigma.ImageAPIOpenAIImages}); err != nil {
		t.Fatalf("RegisterImageProvider returned error: %v", err)
	}
	source := &testImageModelSource{}
	source.Set(sigma.ImageModel{ID: "fresh", Provider: provider, API: sigma.ImageAPIOpenAIImages})
	if err := registry.RegisterImageModelSource(provider, source); err != nil {
		t.Fatalf("RegisterImageModelSource returned error: %v", err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if err := client.RefreshImageModels(context.Background(), provider); err != nil {
		t.Fatalf("client RefreshImageModels returned error: %v", err)
	}
	models := client.ImageModels()
	if got, want := len(models), 1; got != want {
		t.Fatalf("client image model count = %d, want %d", got, want)
	}
	if got, want := models[0].ID, sigma.ModelID("fresh"); got != want {
		t.Fatalf("client image model id = %q, want %q", got, want)
	}
}

func TestRegistryEmbeddingModelSourceDuplicateHandlingRequiresOverride(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-embeddings")
	source := &testEmbeddingModelSource{}
	if err := registry.RegisterEmbeddingModelSource(provider, source); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModelSource(provider, source); err == nil {
		t.Fatal("duplicate embedding model source registration returned nil error")
	}
	if err := registry.RegisterEmbeddingModelSource(provider, source, sigma.WithOverride()); err != nil {
		t.Fatalf("override embedding model source registration returned error: %v", err)
	}
}

func TestRegistryRefreshEmbeddingModelsReplacesOnlySourceOwnedModels(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-embeddings")
	if err := registry.RegisterEmbeddingProvider(provider, testEmbeddingProvider{api: sigma.EmbeddingAPIOpenAIEmbeddings}); err != nil {
		t.Fatalf("RegisterEmbeddingProvider returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModel(sigma.EmbeddingModel{
		ID:       "manual",
		Provider: provider,
		API:      sigma.EmbeddingAPIOpenAIEmbeddings,
		Name:     "Manual",
	}); err != nil {
		t.Fatalf("RegisterEmbeddingModel(manual) returned error: %v", err)
	}
	source := &testEmbeddingModelSource{}
	source.Set(
		sigma.EmbeddingModel{ID: "source-old", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings},
		sigma.EmbeddingModel{ID: "source-keep", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings},
	)
	if err := registry.RegisterEmbeddingModelSource(provider, source); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource returned error: %v", err)
	}
	if err := registry.RefreshEmbeddingModels(context.Background(), provider); err != nil {
		t.Fatalf("first RefreshEmbeddingModels returned error: %v", err)
	}

	source.Set(
		sigma.EmbeddingModel{ID: "source-new", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings},
		sigma.EmbeddingModel{ID: "source-keep", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings, Name: "updated"},
	)
	if err := registry.RefreshEmbeddingModels(context.Background(), provider); err != nil {
		t.Fatalf("second RefreshEmbeddingModels returned error: %v", err)
	}

	models := registry.ListEmbeddingModels()
	got := make([]sigma.ModelID, 0, len(models))
	for _, model := range models {
		got = append(got, model.ID)
	}
	want := []sigma.ModelID{"manual", "source-new", "source-keep"}
	if len(got) != len(want) {
		t.Fatalf("embedding model ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("embedding model ids = %v, want %v", got, want)
		}
	}
	if _, ok := registry.EmbeddingModel(provider, "source-old"); ok {
		t.Fatal("stale source-owned embedding model remained registered")
	}
	updated, ok := registry.EmbeddingModel(provider, "source-keep")
	if !ok {
		t.Fatal("updated source embedding model was not registered")
	}
	if got, want := updated.Name, "updated"; got != want {
		t.Fatalf("updated source embedding model name = %q, want %q", got, want)
	}
}

func TestRegistryRefreshEmbeddingModelsPreservesCallerOwnedConflicts(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-embeddings")
	if err := registry.RegisterEmbeddingProvider(provider, testEmbeddingProvider{api: sigma.EmbeddingAPIOpenAIEmbeddings}); err != nil {
		t.Fatalf("RegisterEmbeddingProvider returned error: %v", err)
	}
	manual := sigma.EmbeddingModel{ID: "manual", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings, Name: "manual"}
	if err := registry.RegisterEmbeddingModel(manual); err != nil {
		t.Fatalf("RegisterEmbeddingModel(manual) returned error: %v", err)
	}
	source := &testEmbeddingModelSource{}
	source.Set(sigma.EmbeddingModel{ID: "manual", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings, Name: "source"})
	if err := registry.RegisterEmbeddingModelSource(provider, source); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource returned error: %v", err)
	}

	if err := registry.RefreshEmbeddingModels(context.Background(), provider); err == nil {
		t.Fatal("RefreshEmbeddingModels with caller-owned id conflict returned nil error")
	}
	got, ok := registry.EmbeddingModel(provider, "manual")
	if !ok {
		t.Fatal("caller-owned embedding model was removed after refresh conflict")
	}
	if got.Name != "manual" {
		t.Fatalf("caller-owned embedding model name = %q, want manual", got.Name)
	}
}

func TestRegistryRegisterEmbeddingModelOverrideTakesSourceOwnership(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-embeddings")
	if err := registry.RegisterEmbeddingProvider(provider, testEmbeddingProvider{api: sigma.EmbeddingAPIOpenAIEmbeddings}); err != nil {
		t.Fatalf("RegisterEmbeddingProvider returned error: %v", err)
	}
	source := &testEmbeddingModelSource{}
	source.Set(sigma.EmbeddingModel{ID: "source", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings, Name: "source"})
	if err := registry.RegisterEmbeddingModelSource(provider, source); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource returned error: %v", err)
	}
	if err := registry.RefreshEmbeddingModels(context.Background(), provider); err != nil {
		t.Fatalf("RefreshEmbeddingModels returned error: %v", err)
	}

	if err := registry.RegisterEmbeddingModel(sigma.EmbeddingModel{
		ID:       "source",
		Provider: provider,
		API:      sigma.EmbeddingAPIOpenAIEmbeddings,
		Name:     "manual",
	}, sigma.WithOverride()); err != nil {
		t.Fatalf("RegisterEmbeddingModel override returned error: %v", err)
	}
	if err := registry.RefreshEmbeddingModels(context.Background(), provider); err == nil {
		t.Fatal("RefreshEmbeddingModels after caller override returned nil conflict")
	}
	got, ok := registry.EmbeddingModel(provider, "source")
	if !ok {
		t.Fatal("caller-overridden source embedding model was removed")
	}
	if got.Name != "manual" {
		t.Fatalf("embedding model name = %q, want manual", got.Name)
	}
}

func TestRegistryRefreshEmbeddingModelsValidatesBeforeApplying(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-embeddings")
	if err := registry.RegisterEmbeddingProvider(provider, testEmbeddingProvider{api: sigma.EmbeddingAPIOpenAIEmbeddings}); err != nil {
		t.Fatalf("RegisterEmbeddingProvider returned error: %v", err)
	}
	source := &testEmbeddingModelSource{}
	source.Set(sigma.EmbeddingModel{ID: "good", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings})
	if err := registry.RegisterEmbeddingModelSource(provider, source); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource returned error: %v", err)
	}
	if err := registry.RefreshEmbeddingModels(context.Background(), provider); err != nil {
		t.Fatalf("initial RefreshEmbeddingModels returned error: %v", err)
	}

	source.Set(
		sigma.EmbeddingModel{ID: "bad-api", Provider: provider, API: sigma.EmbeddingAPIGoogleEmbeddings},
		sigma.EmbeddingModel{ID: "new", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings},
	)
	if err := registry.RefreshEmbeddingModels(context.Background(), provider); err == nil {
		t.Fatal("RefreshEmbeddingModels with invalid embedding model returned nil error")
	}
	if _, ok := registry.EmbeddingModel(provider, "good"); !ok {
		t.Fatal("existing source embedding model was removed after failed refresh")
	}
	if _, ok := registry.EmbeddingModel(provider, "new"); ok {
		t.Fatal("new embedding model from failed refresh was registered")
	}
}

func TestRegistryRefreshEmbeddingModelsRejectsDuplicateAndWrongProviderModels(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		models []sigma.EmbeddingModel
	}{
		{
			name: "duplicate embedding model",
			models: []sigma.EmbeddingModel{
				{ID: "same", Provider: sigma.ProviderID("dynamic-embeddings"), API: sigma.EmbeddingAPIOpenAIEmbeddings},
				{ID: "same", Provider: sigma.ProviderID("dynamic-embeddings"), API: sigma.EmbeddingAPIOpenAIEmbeddings},
			},
		},
		{
			name: "wrong provider",
			models: []sigma.EmbeddingModel{
				{ID: "other", Provider: sigma.ProviderID("other"), API: sigma.EmbeddingAPIOpenAIEmbeddings},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := sigma.NewRegistry()
			provider := sigma.ProviderID("dynamic-embeddings")
			if err := registry.RegisterEmbeddingProvider(provider, testEmbeddingProvider{api: sigma.EmbeddingAPIOpenAIEmbeddings}); err != nil {
				t.Fatalf("RegisterEmbeddingProvider returned error: %v", err)
			}
			source := &testEmbeddingModelSource{}
			source.Set(tt.models...)
			if err := registry.RegisterEmbeddingModelSource(provider, source); err != nil {
				t.Fatalf("RegisterEmbeddingModelSource returned error: %v", err)
			}
			if err := registry.RefreshEmbeddingModels(context.Background(), provider); err == nil {
				t.Fatal("RefreshEmbeddingModels returned nil error")
			}
			if got := len(registry.ListEmbeddingModels()); got != 0 {
				t.Fatalf("embedding model count = %d, want 0", got)
			}
		})
	}
}

func TestRegistryRefreshEmbeddingModelsSupportsMetadataOnlySources(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("metadata-only-embeddings")
	source := &testEmbeddingModelSource{}
	source.Set(sigma.EmbeddingModel{ID: "listed", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings})
	if err := registry.RegisterEmbeddingModelSource(provider, source, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource returned error: %v", err)
	}
	if err := registry.RefreshEmbeddingModels(context.Background(), provider); err != nil {
		t.Fatalf("RefreshEmbeddingModels returned error: %v", err)
	}
	if _, ok := registry.EmbeddingModel(provider, "listed"); !ok {
		t.Fatal("metadata-only source embedding model was not registered")
	}
}

func TestRegistryRefreshEmbeddingModelsJoinsErrorsAndAppliesSuccessfulSources(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	goodProvider := sigma.ProviderID("good-embeddings")
	badProvider := sigma.ProviderID("bad-embeddings")
	if err := registry.RegisterEmbeddingProvider(goodProvider, testEmbeddingProvider{api: sigma.EmbeddingAPIOpenAIEmbeddings}); err != nil {
		t.Fatalf("RegisterEmbeddingProvider(good) returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingProvider(badProvider, testEmbeddingProvider{api: sigma.EmbeddingAPIOpenAIEmbeddings}); err != nil {
		t.Fatalf("RegisterEmbeddingProvider(bad) returned error: %v", err)
	}
	good := &testEmbeddingModelSource{}
	good.Set(sigma.EmbeddingModel{ID: "fresh", Provider: goodProvider, API: sigma.EmbeddingAPIOpenAIEmbeddings})
	bad := &testEmbeddingModelSource{err: context.Canceled}
	if err := registry.RegisterEmbeddingModelSource(goodProvider, good); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource(good) returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModelSource(badProvider, bad); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource(bad) returned error: %v", err)
	}

	if err := registry.RefreshEmbeddingModels(context.Background()); err == nil {
		t.Fatal("RefreshEmbeddingModels returned nil error")
	}
	if _, ok := registry.EmbeddingModel(goodProvider, "fresh"); !ok {
		t.Fatal("successful embedding source was not applied when another source failed")
	}
}

func TestRegistryRefreshEmbeddingModelsCancellationLeavesExistingModels(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-embeddings")
	if err := registry.RegisterEmbeddingProvider(provider, testEmbeddingProvider{api: sigma.EmbeddingAPIOpenAIEmbeddings}); err != nil {
		t.Fatalf("RegisterEmbeddingProvider returned error: %v", err)
	}
	source := &testEmbeddingModelSource{}
	source.Set(sigma.EmbeddingModel{ID: "good", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings})
	if err := registry.RegisterEmbeddingModelSource(provider, source); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource returned error: %v", err)
	}
	if err := registry.RefreshEmbeddingModels(context.Background(), provider); err != nil {
		t.Fatalf("initial RefreshEmbeddingModels returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := registry.RegisterEmbeddingModelSource(provider, sigma.EmbeddingModelSourceFunc(func(ctx context.Context) ([]sigma.EmbeddingModel, error) {
		return nil, ctx.Err()
	}), sigma.WithOverride()); err != nil {
		t.Fatalf("override RegisterEmbeddingModelSource returned error: %v", err)
	}
	if err := registry.RefreshEmbeddingModels(ctx, provider); err == nil {
		t.Fatal("RefreshEmbeddingModels with canceled context returned nil error")
	}
	if _, ok := registry.EmbeddingModel(provider, "good"); !ok {
		t.Fatal("existing source embedding model was removed after canceled refresh")
	}
}

func TestClientRefreshEmbeddingModelsUpdatesConfiguredRegistry(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	provider := sigma.ProviderID("dynamic-embeddings")
	if err := registry.RegisterEmbeddingProvider(provider, testEmbeddingProvider{api: sigma.EmbeddingAPIOpenAIEmbeddings}); err != nil {
		t.Fatalf("RegisterEmbeddingProvider returned error: %v", err)
	}
	source := &testEmbeddingModelSource{}
	source.Set(sigma.EmbeddingModel{ID: "fresh", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings})
	if err := registry.RegisterEmbeddingModelSource(provider, source); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource returned error: %v", err)
	}

	client := sigma.NewClient(sigma.WithRegistry(registry))
	if err := client.RefreshEmbeddingModels(context.Background(), provider); err != nil {
		t.Fatalf("client RefreshEmbeddingModels returned error: %v", err)
	}
	models := client.EmbeddingModels()
	if got, want := len(models), 1; got != want {
		t.Fatalf("client embedding model count = %d, want %d", got, want)
	}
	if got, want := models[0].ID, sigma.ModelID("fresh"); got != want {
		t.Fatalf("client embedding model id = %q, want %q", got, want)
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

func TestRegistryProviderAuthRegistrationCloneAndSnapshot(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	auth := sigma.ProviderAuth{
		APIKey: sigma.EnvironmentAPIKeyAuth("Test API key", "TEST_API_KEY"),
	}
	if err := registry.RegisterProviderAuth(sigma.ProviderOpenAI, auth); err != nil {
		t.Fatalf("RegisterProviderAuth returned error: %v", err)
	}
	if err := registry.RegisterProviderAuth(sigma.ProviderOpenAI, auth); err == nil {
		t.Fatal("duplicate provider auth registration returned nil error")
	}
	withOAuth := sigma.ProviderAuth{
		APIKey: sigma.EnvironmentAPIKeyAuth("Test API key", "TEST_API_KEY"),
		OAuth:  &sigma.OAuthAuth{Name: "Test OAuth"},
	}
	if err := registry.RegisterProviderAuth(sigma.ProviderOpenAI, withOAuth, sigma.WithOverride()); err != nil {
		t.Fatalf("override provider auth registration returned error: %v", err)
	}

	registered, ok := registry.ProviderAuth(sigma.ProviderOpenAI)
	if !ok {
		t.Fatal("provider auth was not registered")
	}
	if registered.APIKey == nil || registered.OAuth == nil {
		t.Fatalf("registered auth = %#v, want api-key and oauth", registered)
	}
	registered.APIKey.EnvVars[0] = "MUTATED"
	again, ok := registry.ProviderAuth(sigma.ProviderOpenAI)
	if !ok {
		t.Fatal("provider auth was not registered after mutation")
	}
	if got, want := again.APIKey.EnvVars[0], "TEST_API_KEY"; got != want {
		t.Fatalf("provider auth env var = %q, want %q", got, want)
	}

	clone := registry.Clone()
	if err := clone.RegisterProviderAuth(sigma.ProviderAnthropic, auth); err != nil {
		t.Fatalf("clone RegisterProviderAuth returned error: %v", err)
	}
	if _, ok := registry.ProviderAuth(sigma.ProviderAnthropic); ok {
		t.Fatal("clone provider auth leaked into original registry")
	}

	snapshot := registry.Snapshot()
	if got, want := len(snapshot.ProviderAuths), 1; got != want {
		t.Fatalf("snapshot provider auth count = %d, want %d", got, want)
	}
	if got := snapshot.ProviderAuths[0]; got.ID != sigma.ProviderOpenAI || !got.APIKey || !got.OAuth {
		t.Fatalf("snapshot provider auth = %#v, want openai api-key+oauth", got)
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

func TestRegistryEmbeddingModelRegistration(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	providerID := sigma.ProviderID("embedding-lab")
	embeddingAPI := sigma.EmbeddingAPI("openai-embeddings")
	if err := registry.RegisterEmbeddingProvider(providerID, testEmbeddingProvider{api: embeddingAPI}); err != nil {
		t.Fatalf("RegisterEmbeddingProvider returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModel(sigma.EmbeddingModel{
		ID:                  "embedding-custom",
		Provider:            providerID,
		API:                 embeddingAPI,
		DefaultDimensions:   1536,
		MaxInputTokens:      8192,
		InputCostPerMillion: 0.02,
		CostCurrency:        "USD",
	}); err != nil {
		t.Fatalf("RegisterEmbeddingModel returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModel(sigma.EmbeddingModel{
		ID:       "wrong-embedding-api",
		Provider: providerID,
		API:      "other-embeddings",
	}); err == nil {
		t.Fatal("embedding model with mismatched api returned nil error")
	}

	model, ok := registry.EmbeddingModel(providerID, "embedding-custom")
	if !ok {
		t.Fatal("embedding model was not registered")
	}
	if got, want := model.DefaultDimensions, 1536; got != want {
		t.Fatalf("default dimensions = %d, want %d", got, want)
	}
	snapshot := registry.Snapshot()
	if got, want := len(snapshot.EmbeddingModels), 1; got != want {
		t.Fatalf("snapshot embedding models = %d, want %d", got, want)
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
		CostTiers:        []sigma.ModelCostTier{{InputTokensAbove: 272_000, InputCostPerMillion: 2}},
		ProviderMetadata: nestedProviderMetadata(),
	}
	if err := registry.RegisterModel(model, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterModel returned error: %v", err)
	}
	mutateNestedProviderMetadata(model.ProviderMetadata)
	model.CostTiers[0].InputCostPerMillion = 20

	listed := registry.ListModels()
	listed[0].ThinkingLevels[0] = sigma.ThinkingLevelHigh
	listed[0].UnsupportedThinkingLevels[0] = sigma.ThinkingLevelMedium
	listed[0].CostTiers[0].InputCostPerMillion = 20
	listed[0].ProviderMetadata["family"] = "mutated"
	mutateNestedProviderMetadata(listed[0].ProviderMetadata)

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
	if got.CostTiers[0].InputCostPerMillion != 2 {
		t.Fatalf("cost tier input rate = %v, want 2", got.CostTiers[0].InputCostPerMillion)
	}
	if got.ProviderMetadata["family"] != "gpt" {
		t.Fatalf("provider metadata family = %q, want %q", got.ProviderMetadata["family"], "gpt")
	}
	assertNestedProviderMetadata(t, got.ProviderMetadata)

	got.ProviderMetadata["family"] = "mutated"
	got.CostTiers[0].InputCostPerMillion = 20
	mutateNestedProviderMetadata(got.ProviderMetadata)
	got, ok = registry.Model(sigma.ProviderOpenAI, "gpt-custom")
	if !ok {
		t.Fatal("model was not registered")
	}
	assertNestedProviderMetadata(t, got.ProviderMetadata)

	snapshot := registry.Snapshot()
	snapshot.Models[0].CostTiers[0].InputCostPerMillion = 20
	snapshot.Models[0].ProviderMetadata["family"] = "mutated"
	mutateNestedProviderMetadata(snapshot.Models[0].ProviderMetadata)
	got, ok = registry.Model(sigma.ProviderOpenAI, "gpt-custom")
	if !ok {
		t.Fatal("model was not registered")
	}
	assertNestedProviderMetadata(t, got.ProviderMetadata)

	clone := registry.Clone()
	cloned, ok := clone.Model(sigma.ProviderOpenAI, "gpt-custom")
	if !ok {
		t.Fatal("cloned model was not registered")
	}
	cloned.ProviderMetadata["family"] = "mutated"
	cloned.CostTiers[0].InputCostPerMillion = 20
	mutateNestedProviderMetadata(cloned.ProviderMetadata)
	got, ok = registry.Model(sigma.ProviderOpenAI, "gpt-custom")
	if !ok {
		t.Fatal("model was not registered")
	}
	if got.CostTiers[0].InputCostPerMillion != 2 {
		t.Fatalf("cloned cost tier input rate leaked into original: %v", got.CostTiers[0].InputCostPerMillion)
	}
	assertNestedProviderMetadata(t, got.ProviderMetadata)
	cloned, ok = clone.Model(sigma.ProviderOpenAI, "gpt-custom")
	if !ok {
		t.Fatal("cloned model was not registered")
	}
	assertNestedProviderMetadata(t, cloned.ProviderMetadata)
}

func TestRegistryReturnsDefensiveImageAndEmbeddingMetadataCopies(t *testing.T) {
	t.Parallel()

	registry := sigma.NewRegistry()
	if err := registry.RegisterImageModel(sigma.ImageModel{
		ID:               "image-custom",
		Provider:         sigma.ProviderOpenAI,
		API:              sigma.ImageAPIOpenAIImages,
		ProviderMetadata: nestedProviderMetadata(),
	}, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterImageModel returned error: %v", err)
	}
	if err := registry.RegisterEmbeddingModel(sigma.EmbeddingModel{
		ID:               "embedding-custom",
		Provider:         sigma.ProviderOpenAI,
		API:              sigma.EmbeddingAPIOpenAIEmbeddings,
		ProviderMetadata: nestedProviderMetadata(),
	}, sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterEmbeddingModel returned error: %v", err)
	}

	images := registry.ListImageModels()
	mutateNestedProviderMetadata(images[0].ProviderMetadata)
	image, ok := registry.ImageModel(sigma.ProviderOpenAI, "image-custom")
	if !ok {
		t.Fatal("image model was not registered")
	}
	assertNestedProviderMetadata(t, image.ProviderMetadata)

	embeddings := registry.ListEmbeddingModels()
	mutateNestedProviderMetadata(embeddings[0].ProviderMetadata)
	embedding, ok := registry.EmbeddingModel(sigma.ProviderOpenAI, "embedding-custom")
	if !ok {
		t.Fatal("embedding model was not registered")
	}
	assertNestedProviderMetadata(t, embedding.ProviderMetadata)
}

func nestedProviderMetadata() map[string]any {
	return map[string]any{
		"family":  "gpt",
		"nested":  map[string]any{"inner": "kept"},
		"items":   []any{map[string]any{"value": "kept"}},
		"strings": []string{"kept"},
		"headers": map[string]string{"x-test": "kept"},
		"schema": sigma.Schema{
			"properties": map[string]any{
				"field": map[string]any{"type": "string"},
			},
		},
	}
}

func mutateNestedProviderMetadata(metadata map[string]any) {
	metadata["nested"].(map[string]any)["inner"] = "mutated"
	metadata["items"].([]any)[0].(map[string]any)["value"] = "mutated"
	metadata["strings"].([]string)[0] = "mutated"
	metadata["headers"].(map[string]string)["x-test"] = "mutated"
	schema := metadata["schema"].(sigma.Schema)
	properties := schema["properties"].(map[string]any)
	properties["field"].(map[string]any)["type"] = "number"
}

func assertNestedProviderMetadata(t *testing.T, metadata map[string]any) {
	t.Helper()

	if got, want := metadata["family"], "gpt"; got != want {
		t.Fatalf("provider metadata family = %q, want %q", got, want)
	}
	if got, want := metadata["nested"].(map[string]any)["inner"], "kept"; got != want {
		t.Fatalf("nested metadata = %q, want %q", got, want)
	}
	if got, want := metadata["items"].([]any)[0].(map[string]any)["value"], "kept"; got != want {
		t.Fatalf("metadata item = %q, want %q", got, want)
	}
	if got, want := metadata["strings"].([]string)[0], "kept"; got != want {
		t.Fatalf("metadata string = %q, want %q", got, want)
	}
	if got, want := metadata["headers"].(map[string]string)["x-test"], "kept"; got != want {
		t.Fatalf("metadata header = %q, want %q", got, want)
	}
	schema := metadata["schema"].(sigma.Schema)
	properties := schema["properties"].(map[string]any)
	if got, want := properties["field"].(map[string]any)["type"], "string"; got != want {
		t.Fatalf("metadata schema type = %q, want %q", got, want)
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
	if _, ok := registry.EmbeddingModel(sigma.ProviderOpenAI, "text-embedding-3-small"); !ok {
		t.Fatal("default registry missing built-in embedding model metadata")
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

func TestRegistryRejectsStaleTextModelRefreshAfterSourceReplacement(t *testing.T) {
	t.Parallel()

	provider := sigma.ProviderID("stale-text-source")
	started := make(chan struct{})
	release := make(chan struct{})
	base := sigma.NewRegistry()
	if err := base.RegisterTextModelSource(provider, sigma.TextModelSourceFunc(func(context.Context) ([]sigma.Model, error) {
		close(started)
		<-release
		return []sigma.Model{{ID: "stale", Provider: provider, API: sigma.APIOpenAIResponses}}, nil
	}), sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterTextModelSource returned error: %v", err)
	}
	registry := base.Clone()
	refreshErr := make(chan error, 1)
	go func() { refreshErr <- registry.RefreshTextModels(context.Background(), provider) }()
	waitForModelRefreshStart(t, started)
	if err := registry.RegisterTextModelSource(provider, sigma.TextModelSourceFunc(func(context.Context) ([]sigma.Model, error) {
		return []sigma.Model{{ID: "fresh", Provider: provider, API: sigma.APIOpenAIResponses}}, nil
	}), sigma.WithMetadataOnly(), sigma.WithOverride()); err != nil {
		t.Fatalf("override RegisterTextModelSource returned error: %v", err)
	}
	close(release)
	if err := <-refreshErr; err == nil || !strings.Contains(err.Error(), "source changed during refresh") {
		t.Fatalf("stale RefreshTextModels error = %v, want source conflict", err)
	}
	if _, ok := registry.Model(provider, "stale"); ok {
		t.Fatal("stale text model was applied")
	}
	if err := registry.RefreshTextModels(context.Background(), provider); err != nil {
		t.Fatalf("replacement RefreshTextModels returned error: %v", err)
	}
	if _, ok := registry.Model(provider, "fresh"); !ok {
		t.Fatal("replacement text model was not applied")
	}
}

func TestRegistryRejectsStaleImageModelRefreshAfterSourceReplacement(t *testing.T) {
	t.Parallel()

	provider := sigma.ProviderID("stale-image-source")
	started := make(chan struct{})
	release := make(chan struct{})
	registry := sigma.NewRegistry()
	if err := registry.RegisterImageModelSource(provider, sigma.ImageModelSourceFunc(func(context.Context) ([]sigma.ImageModel, error) {
		close(started)
		<-release
		return []sigma.ImageModel{{ID: "stale", Provider: provider, API: sigma.ImageAPIOpenAIImages}}, nil
	}), sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterImageModelSource returned error: %v", err)
	}
	refreshErr := make(chan error, 1)
	go func() { refreshErr <- registry.RefreshImageModels(context.Background(), provider) }()
	waitForModelRefreshStart(t, started)
	if err := registry.RegisterImageModelSource(provider, sigma.ImageModelSourceFunc(func(context.Context) ([]sigma.ImageModel, error) {
		return []sigma.ImageModel{{ID: "fresh", Provider: provider, API: sigma.ImageAPIOpenAIImages}}, nil
	}), sigma.WithMetadataOnly(), sigma.WithOverride()); err != nil {
		t.Fatalf("override RegisterImageModelSource returned error: %v", err)
	}
	close(release)
	if err := <-refreshErr; err == nil || !strings.Contains(err.Error(), "source changed during refresh") {
		t.Fatalf("stale RefreshImageModels error = %v, want source conflict", err)
	}
	if _, ok := registry.ImageModel(provider, "stale"); ok {
		t.Fatal("stale image model was applied")
	}
	if err := registry.RefreshImageModels(context.Background(), provider); err != nil {
		t.Fatalf("replacement RefreshImageModels returned error: %v", err)
	}
	if _, ok := registry.ImageModel(provider, "fresh"); !ok {
		t.Fatal("replacement image model was not applied")
	}
}

func TestRegistryRejectsStaleEmbeddingModelRefreshAfterSourceReplacement(t *testing.T) {
	t.Parallel()

	provider := sigma.ProviderID("stale-embedding-source")
	started := make(chan struct{})
	release := make(chan struct{})
	registry := sigma.NewRegistry()
	if err := registry.RegisterEmbeddingModelSource(provider, sigma.EmbeddingModelSourceFunc(func(context.Context) ([]sigma.EmbeddingModel, error) {
		close(started)
		<-release
		return []sigma.EmbeddingModel{{ID: "stale", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings}}, nil
	}), sigma.WithMetadataOnly()); err != nil {
		t.Fatalf("RegisterEmbeddingModelSource returned error: %v", err)
	}
	refreshErr := make(chan error, 1)
	go func() { refreshErr <- registry.RefreshEmbeddingModels(context.Background(), provider) }()
	waitForModelRefreshStart(t, started)
	if err := registry.RegisterEmbeddingModelSource(provider, sigma.EmbeddingModelSourceFunc(func(context.Context) ([]sigma.EmbeddingModel, error) {
		return []sigma.EmbeddingModel{{ID: "fresh", Provider: provider, API: sigma.EmbeddingAPIOpenAIEmbeddings}}, nil
	}), sigma.WithMetadataOnly(), sigma.WithOverride()); err != nil {
		t.Fatalf("override RegisterEmbeddingModelSource returned error: %v", err)
	}
	close(release)
	if err := <-refreshErr; err == nil || !strings.Contains(err.Error(), "source changed during refresh") {
		t.Fatalf("stale RefreshEmbeddingModels error = %v, want source conflict", err)
	}
	if _, ok := registry.EmbeddingModel(provider, "stale"); ok {
		t.Fatal("stale embedding model was applied")
	}
	if err := registry.RefreshEmbeddingModels(context.Background(), provider); err != nil {
		t.Fatalf("replacement RefreshEmbeddingModels returned error: %v", err)
	}
	if _, ok := registry.EmbeddingModel(provider, "fresh"); !ok {
		t.Fatal("replacement embedding model was not applied")
	}
}

func waitForModelRefreshStart(t *testing.T, started <-chan struct{}) {
	t.Helper()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("model refresh did not start")
	}
}
