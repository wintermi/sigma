// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// RegisterOption configures registry registration behavior.
type RegisterOption func(*registerOptions)

type registerOptions struct {
	override     bool
	metadataOnly bool
}

// TextModelSource lists text models for a provider-owned runtime source.
type TextModelSource interface {
	TextModels(context.Context) ([]Model, error)
}

// ImageModelSource lists image models for a provider-owned runtime source.
type ImageModelSource interface {
	ImageModels(context.Context) ([]ImageModel, error)
}

// TextModelSourceFunc adapts a function into a TextModelSource.
type TextModelSourceFunc func(context.Context) ([]Model, error)

// ImageModelSourceFunc adapts a function into an ImageModelSource.
type ImageModelSourceFunc func(context.Context) ([]ImageModel, error)

// TextModels calls f.
func (f TextModelSourceFunc) TextModels(ctx context.Context) ([]Model, error) {
	if f == nil {
		return nil, registryError("text model source is required")
	}
	return f(ctx)
}

// ImageModels calls f.
func (f ImageModelSourceFunc) ImageModels(ctx context.Context) ([]ImageModel, error) {
	if f == nil {
		return nil, registryError("image model source is required")
	}
	return f(ctx)
}

// ModelFilter reports whether a model should be included in a model list.
type ModelFilter func(Model) bool

// WithOverride allows a registration to replace an existing provider or model.
func WithOverride() RegisterOption {
	return func(opts *registerOptions) {
		opts.override = true
	}
}

// WithMetadataOnly allows model metadata to be registered without a provider.
func WithMetadataOnly() RegisterOption {
	return func(opts *registerOptions) {
		opts.metadataOnly = true
	}
}

// ProviderInfo is a copyable view of registered provider capabilities.
type ProviderInfo struct {
	ID           ProviderID   `json:"id"`
	TextAPI      API          `json:"textApi,omitempty"`
	ImageAPI     ImageAPI     `json:"imageApi,omitempty"`
	EmbeddingAPI EmbeddingAPI `json:"embeddingApi,omitempty"`
}

// ProviderAuthInfo is a copyable view of registered provider auth capabilities.
type ProviderAuthInfo struct {
	ID     ProviderID `json:"id"`
	APIKey bool       `json:"apiKey,omitempty"`
	OAuth  bool       `json:"oauth,omitempty"`
}

// RegistrySnapshot is an immutable-by-convention copy of registry state.
type RegistrySnapshot struct {
	Providers       []ProviderInfo     `json:"providers,omitempty"`
	ProviderAuths   []ProviderAuthInfo `json:"providerAuths,omitempty"`
	Models          []Model            `json:"models,omitempty"`
	ImageModels     []ImageModel       `json:"imageModels,omitempty"`
	EmbeddingModels []EmbeddingModel   `json:"embeddingModels,omitempty"`
}

type providerRegistration struct {
	text         TextProvider
	textAPI      API
	image        ImageProvider
	imageAPI     ImageAPI
	embedding    EmbeddingProvider
	embeddingAPI EmbeddingAPI
	textSet      bool
	imageSet     bool
	embeddingSet bool
}

type textModelSourceRegistration struct {
	source       TextModelSource
	metadataOnly bool
}

type imageModelSourceRegistration struct {
	source       ImageModelSource
	metadataOnly bool
}

// Registry stores provider implementations and model metadata.
type Registry struct {
	mu sync.RWMutex

	providers     map[ProviderID]providerRegistration
	providerOrder []ProviderID

	textModelSources     map[ProviderID]textModelSourceRegistration
	textModelSourceOrder []ProviderID
	textModelSourceRefs  map[ProviderID]map[ModelRef]struct{}

	providerAuths     map[ProviderID]ProviderAuth
	providerAuthOrder []ProviderID

	models     map[ModelRef]Model
	modelOrder []ModelRef

	imageModels     map[ModelRef]ImageModel
	imageModelOrder []ModelRef

	imageModelSources     map[ProviderID]imageModelSourceRegistration
	imageModelSourceOrder []ProviderID
	imageModelSourceRefs  map[ProviderID]map[ModelRef]struct{}

	embeddingModels     map[ModelRef]EmbeddingModel
	embeddingModelOrder []ModelRef
}

var defaultRegistry = newDefaultRegistry()

// NewRegistry constructs an isolated empty registry.
func NewRegistry() *Registry {
	return newRegistry()
}

// DefaultRegistry returns a clone of the package-level default registry.
func DefaultRegistry() *Registry {
	return defaultRegistry.Clone()
}

// RegisterDefaultTextProvider registers a text provider on the default registry.
func RegisterDefaultTextProvider(id ProviderID, provider TextProvider, opts ...RegisterOption) error {
	return defaultRegistry.RegisterTextProvider(id, provider, opts...)
}

// RegisterDefaultImageProvider registers an image provider on the default registry.
func RegisterDefaultImageProvider(id ProviderID, provider ImageProvider, opts ...RegisterOption) error {
	return defaultRegistry.RegisterImageProvider(id, provider, opts...)
}

// RegisterDefaultEmbeddingProvider registers an embedding provider on the default registry.
func RegisterDefaultEmbeddingProvider(id ProviderID, provider EmbeddingProvider, opts ...RegisterOption) error {
	return defaultRegistry.RegisterEmbeddingProvider(id, provider, opts...)
}

// RegisterDefaultProviderAuth registers provider auth on the default registry.
func RegisterDefaultProviderAuth(id ProviderID, auth ProviderAuth, opts ...RegisterOption) error {
	return defaultRegistry.RegisterProviderAuth(id, auth, opts...)
}

// RegisterDefaultModel registers a text model on the default registry.
func RegisterDefaultModel(model Model, opts ...RegisterOption) error {
	return defaultRegistry.RegisterModel(model, opts...)
}

// RegisterDefaultImageModel registers an image model on the default registry.
func RegisterDefaultImageModel(model ImageModel, opts ...RegisterOption) error {
	return defaultRegistry.RegisterImageModel(model, opts...)
}

// RegisterDefaultEmbeddingModel registers an embedding model on the default registry.
func RegisterDefaultEmbeddingModel(model EmbeddingModel, opts ...RegisterOption) error {
	return defaultRegistry.RegisterEmbeddingModel(model, opts...)
}

// RegisterProvider registers a text provider on registry.
func RegisterProvider(registry *Registry, id ProviderID, provider TextProvider, opts ...RegisterOption) error {
	if registry == nil {
		return registryError("registry is required")
	}
	return registry.RegisterTextProvider(id, provider, opts...)
}

// RegisterModel registers text model metadata on registry.
func RegisterModel(registry *Registry, model Model, opts ...RegisterOption) error {
	if registry == nil {
		return registryError("registry is required")
	}
	return registry.RegisterModel(model, opts...)
}

// RegisterTextModelSource registers a runtime text model source on registry.
func RegisterTextModelSource(registry *Registry, provider ProviderID, source TextModelSource, opts ...RegisterOption) error {
	if registry == nil {
		return registryError("registry is required")
	}
	return registry.RegisterTextModelSource(provider, source, opts...)
}

// RegisterImageModelSource registers a runtime image model source on registry.
func RegisterImageModelSource(registry *Registry, provider ProviderID, source ImageModelSource, opts ...RegisterOption) error {
	if registry == nil {
		return registryError("registry is required")
	}
	return registry.RegisterImageModelSource(provider, source, opts...)
}

// RegisterProviderAuth registers auth metadata on registry.
func RegisterProviderAuth(registry *Registry, provider ProviderID, auth ProviderAuth, opts ...RegisterOption) error {
	if registry == nil {
		return registryError("registry is required")
	}
	return registry.RegisterProviderAuth(provider, auth, opts...)
}

// RegisterEmbeddingModel registers embedding model metadata on registry.
func RegisterEmbeddingModel(registry *Registry, model EmbeddingModel, opts ...RegisterOption) error {
	if registry == nil {
		return registryError("registry is required")
	}
	return registry.RegisterEmbeddingModel(model, opts...)
}

// RegisterTextProvider registers the implementation for a provider's text API.
func (r *Registry) RegisterTextProvider(id ProviderID, provider TextProvider, opts ...RegisterOption) error {
	if id == "" {
		return registryError("provider id is required")
	}
	if provider == nil {
		return registryError("text provider is required")
	}
	api := provider.API()
	if api == "" {
		return registryError("text provider api is required")
	}

	options := applyRegisterOptions(opts)
	r.ensure()

	r.mu.Lock()
	defer r.mu.Unlock()

	registration := r.providers[id]
	if registration.textSet && !options.override {
		return registryConflict("text provider already registered")
	}
	if !registration.textSet && !registration.imageSet && !registration.embeddingSet {
		r.providerOrder = append(r.providerOrder, id)
	}
	registration.text = provider
	registration.textAPI = api
	registration.textSet = true
	r.providers[id] = registration
	return nil
}

// RegisterTextModelSource registers a runtime text model source for provider.
func (r *Registry) RegisterTextModelSource(provider ProviderID, source TextModelSource, opts ...RegisterOption) error {
	if provider == "" {
		return registryError("provider id is required")
	}
	if source == nil {
		return registryError("text model source is required")
	}

	options := applyRegisterOptions(opts)
	r.ensure()

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.textModelSources[provider]; ok && !options.override {
		return registryConflict("text model source already registered")
	}
	if _, ok := r.textModelSources[provider]; !ok {
		r.textModelSourceOrder = append(r.textModelSourceOrder, provider)
	}
	r.textModelSources[provider] = textModelSourceRegistration{
		source:       source,
		metadataOnly: options.metadataOnly,
	}
	return nil
}

// RegisterImageModelSource registers a runtime image model source for provider.
func (r *Registry) RegisterImageModelSource(provider ProviderID, source ImageModelSource, opts ...RegisterOption) error {
	if provider == "" {
		return registryError("provider id is required")
	}
	if source == nil {
		return registryError("image model source is required")
	}

	options := applyRegisterOptions(opts)
	r.ensure()

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.imageModelSources[provider]; ok && !options.override {
		return registryConflict("image model source already registered")
	}
	if _, ok := r.imageModelSources[provider]; !ok {
		r.imageModelSourceOrder = append(r.imageModelSourceOrder, provider)
	}
	r.imageModelSources[provider] = imageModelSourceRegistration{
		source:       source,
		metadataOnly: options.metadataOnly,
	}
	return nil
}

// RegisterImageProvider registers the implementation for a provider's image API.
func (r *Registry) RegisterImageProvider(id ProviderID, provider ImageProvider, opts ...RegisterOption) error {
	if id == "" {
		return registryError("provider id is required")
	}
	if provider == nil {
		return registryError("image provider is required")
	}
	api := provider.API()
	if api == "" {
		return registryError("image provider api is required")
	}

	options := applyRegisterOptions(opts)
	r.ensure()

	r.mu.Lock()
	defer r.mu.Unlock()

	registration := r.providers[id]
	if registration.imageSet && !options.override {
		return registryConflict("image provider already registered")
	}
	if !registration.textSet && !registration.imageSet && !registration.embeddingSet {
		r.providerOrder = append(r.providerOrder, id)
	}
	registration.image = provider
	registration.imageAPI = api
	registration.imageSet = true
	r.providers[id] = registration
	return nil
}

// RegisterEmbeddingProvider registers the implementation for a provider's embeddings API.
func (r *Registry) RegisterEmbeddingProvider(id ProviderID, provider EmbeddingProvider, opts ...RegisterOption) error {
	if id == "" {
		return registryError("provider id is required")
	}
	if provider == nil {
		return registryError("embedding provider is required")
	}
	api := provider.API()
	if api == "" {
		return registryError("embedding provider api is required")
	}

	options := applyRegisterOptions(opts)
	r.ensure()

	r.mu.Lock()
	defer r.mu.Unlock()

	registration := r.providers[id]
	if registration.embeddingSet && !options.override {
		return registryConflict("embedding provider already registered")
	}
	if !registration.textSet && !registration.imageSet && !registration.embeddingSet {
		r.providerOrder = append(r.providerOrder, id)
	}
	registration.embedding = provider
	registration.embeddingAPI = api
	registration.embeddingSet = true
	r.providers[id] = registration
	return nil
}

// RegisterProviderAuth registers auth metadata for provider.
func (r *Registry) RegisterProviderAuth(provider ProviderID, auth ProviderAuth, opts ...RegisterOption) error {
	if provider == "" {
		return registryError("provider id is required")
	}
	if auth.APIKey == nil && auth.OAuth == nil {
		return registryError("provider auth is required")
	}

	options := applyRegisterOptions(opts)
	r.ensure()

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.providerAuths[provider]; ok && !options.override {
		return registryConflict("provider auth already registered")
	}
	if _, ok := r.providerAuths[provider]; !ok {
		r.providerAuthOrder = append(r.providerAuthOrder, provider)
	}
	r.providerAuths[provider] = cloneProviderAuth(auth)
	return nil
}

// RegisterModel registers text model metadata.
func (r *Registry) RegisterModel(model Model, opts ...RegisterOption) error {
	if err := validateModel(model); err != nil {
		return err
	}

	options := applyRegisterOptions(opts)
	key := ModelRef{Provider: model.Provider, ID: model.ID}
	r.ensure()

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.validateTextModelProviderLocked(model, options.metadataOnly); err != nil {
		return err
	}
	if _, ok := r.models[key]; ok && !options.override {
		return registryConflict("model already registered")
	}
	if _, ok := r.models[key]; !ok {
		r.modelOrder = append(r.modelOrder, key)
	}
	r.models[key] = cloneModel(model)
	r.removeTextModelSourceRefLocked(model.Provider, key)
	return nil
}

// RegisterImageModel registers image model metadata.
func (r *Registry) RegisterImageModel(model ImageModel, opts ...RegisterOption) error {
	if err := validateImageModel(model); err != nil {
		return err
	}

	options := applyRegisterOptions(opts)
	key := ModelRef{Provider: model.Provider, ID: model.ID}
	r.ensure()

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.validateImageModelProviderLocked(model, options.metadataOnly); err != nil {
		return err
	}
	if _, ok := r.imageModels[key]; ok && !options.override {
		return registryConflict("image model already registered")
	}
	if _, ok := r.imageModels[key]; !ok {
		r.imageModelOrder = append(r.imageModelOrder, key)
	}
	r.imageModels[key] = cloneImageModel(model)
	r.removeImageModelSourceRefLocked(model.Provider, key)
	return nil
}

// RegisterEmbeddingModel registers embedding model metadata.
func (r *Registry) RegisterEmbeddingModel(model EmbeddingModel, opts ...RegisterOption) error {
	if err := validateEmbeddingModel(model); err != nil {
		return err
	}

	options := applyRegisterOptions(opts)
	key := ModelRef{Provider: model.Provider, ID: model.ID}
	r.ensure()

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.validateEmbeddingModelProviderLocked(model, options.metadataOnly); err != nil {
		return err
	}
	if _, ok := r.embeddingModels[key]; ok && !options.override {
		return registryConflict("embedding model already registered")
	}
	if _, ok := r.embeddingModels[key]; !ok {
		r.embeddingModelOrder = append(r.embeddingModelOrder, key)
	}
	r.embeddingModels[key] = cloneEmbeddingModel(model)
	return nil
}

// RefreshTextModels refreshes text models from registered runtime sources.
func (r *Registry) RefreshTextModels(ctx context.Context, providers ...ProviderID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.ensure()

	sources, err := r.textSourcesForRefresh(providers)
	if err != nil {
		return err
	}

	var errs []error
	for _, source := range sources {
		models, err := source.registration.source.TextModels(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("refresh text models for %s: %w", source.provider, err))
			continue
		}
		if err := r.applyTextModelRefresh(source.provider, source.registration, models); err != nil {
			errs = append(errs, fmt.Errorf("refresh text models for %s: %w", source.provider, err))
		}
	}
	return errors.Join(errs...)
}

// RefreshImageModels refreshes image models from registered runtime sources.
func (r *Registry) RefreshImageModels(ctx context.Context, providers ...ProviderID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.ensure()

	sources, err := r.imageSourcesForRefresh(providers)
	if err != nil {
		return err
	}

	var errs []error
	for _, source := range sources {
		models, err := source.registration.source.ImageModels(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("refresh image models for %s: %w", source.provider, err))
			continue
		}
		if err := r.applyImageModelRefresh(source.provider, source.registration, models); err != nil {
			errs = append(errs, fmt.Errorf("refresh image models for %s: %w", source.provider, err))
		}
	}
	return errors.Join(errs...)
}

// TextProvider returns the registered text provider for id.
func (r *Registry) TextProvider(id ProviderID) (TextProvider, bool) {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	registration, ok := r.providers[id]
	return registration.text, ok && registration.textSet
}

// ImageProvider returns the registered image provider for id.
func (r *Registry) ImageProvider(id ProviderID) (ImageProvider, bool) {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	registration, ok := r.providers[id]
	return registration.image, ok && registration.imageSet
}

// EmbeddingProvider returns the registered embedding provider for id.
func (r *Registry) EmbeddingProvider(id ProviderID) (EmbeddingProvider, bool) {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	registration, ok := r.providers[id]
	return registration.embedding, ok && registration.embeddingSet
}

// ProviderAuth returns registered auth metadata for provider.
func (r *Registry) ProviderAuth(provider ProviderID) (ProviderAuth, bool) {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	auth, ok := r.providerAuths[provider]
	return cloneProviderAuth(auth), ok
}

// ListProviders returns providers in first-registration order.
func (r *Registry) ListProviders() []ProviderInfo {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make([]ProviderInfo, 0, len(r.providerOrder))
	for _, id := range r.providerOrder {
		registration := r.providers[id]
		providers = append(providers, ProviderInfo{
			ID:           id,
			TextAPI:      registration.textAPI,
			ImageAPI:     registration.imageAPI,
			EmbeddingAPI: registration.embeddingAPI,
		})
	}
	return providers
}

// ListProviderAuths returns registered provider auth metadata in registration order.
func (r *Registry) ListProviderAuths() []ProviderAuthInfo {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	auths := make([]ProviderAuthInfo, 0, len(r.providerAuthOrder))
	for _, id := range r.providerAuthOrder {
		auth := r.providerAuths[id]
		auths = append(auths, ProviderAuthInfo{
			ID:     id,
			APIKey: auth.APIKey != nil,
			OAuth:  auth.OAuth != nil,
		})
	}
	return auths
}

// ListModels returns text models in registration order.
func (r *Registry) ListModels() []Model {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]Model, 0, len(r.modelOrder))
	for _, key := range r.modelOrder {
		models = append(models, cloneModel(r.models[key]))
	}
	return models
}

// ListImageModels returns image models in registration order.
func (r *Registry) ListImageModels() []ImageModel {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]ImageModel, 0, len(r.imageModelOrder))
	for _, key := range r.imageModelOrder {
		models = append(models, cloneImageModel(r.imageModels[key]))
	}
	return models
}

// ListEmbeddingModels returns embedding models in registration order.
func (r *Registry) ListEmbeddingModels() []EmbeddingModel {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]EmbeddingModel, 0, len(r.embeddingModelOrder))
	for _, key := range r.embeddingModelOrder {
		models = append(models, cloneEmbeddingModel(r.embeddingModels[key]))
	}
	return models
}

// Model returns a text model by provider and model id.
func (r *Registry) Model(provider ProviderID, id ModelID) (Model, bool) {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	model, ok := r.models[ModelRef{Provider: provider, ID: id}]
	if !ok {
		return Model{}, false
	}
	return cloneModel(model), true
}

// ImageModel returns an image model by provider and model id.
func (r *Registry) ImageModel(provider ProviderID, id ModelID) (ImageModel, bool) {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	model, ok := r.imageModels[ModelRef{Provider: provider, ID: id}]
	if !ok {
		return ImageModel{}, false
	}
	return cloneImageModel(model), true
}

// EmbeddingModel returns an embedding model by provider and model id.
func (r *Registry) EmbeddingModel(provider ProviderID, id ModelID) (EmbeddingModel, bool) {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	model, ok := r.embeddingModels[ModelRef{Provider: provider, ID: id}]
	if !ok {
		return EmbeddingModel{}, false
	}
	return cloneEmbeddingModel(model), true
}

// Clone returns an isolated copy of the registry.
func (r *Registry) Clone() *Registry {
	r.ensure()

	r.mu.RLock()
	defer r.mu.RUnlock()

	clone := newRegistry()
	clone.providerOrder = append(clone.providerOrder, r.providerOrder...)
	for id, registration := range r.providers {
		clone.providers[id] = registration
	}
	clone.textModelSourceOrder = append(clone.textModelSourceOrder, r.textModelSourceOrder...)
	for provider, source := range r.textModelSources {
		clone.textModelSources[provider] = source
	}
	for provider, refs := range r.textModelSourceRefs {
		clone.textModelSourceRefs[provider] = copyModelRefSet(refs)
	}
	clone.providerAuthOrder = append(clone.providerAuthOrder, r.providerAuthOrder...)
	for provider, auth := range r.providerAuths {
		clone.providerAuths[provider] = cloneProviderAuth(auth)
	}
	clone.modelOrder = append(clone.modelOrder, r.modelOrder...)
	for key, model := range r.models {
		clone.models[key] = cloneModel(model)
	}
	clone.imageModelOrder = append(clone.imageModelOrder, r.imageModelOrder...)
	for key, model := range r.imageModels {
		clone.imageModels[key] = cloneImageModel(model)
	}
	clone.imageModelSourceOrder = append(clone.imageModelSourceOrder, r.imageModelSourceOrder...)
	for provider, source := range r.imageModelSources {
		clone.imageModelSources[provider] = source
	}
	for provider, refs := range r.imageModelSourceRefs {
		clone.imageModelSourceRefs[provider] = copyModelRefSet(refs)
	}
	clone.embeddingModelOrder = append(clone.embeddingModelOrder, r.embeddingModelOrder...)
	for key, model := range r.embeddingModels {
		clone.embeddingModels[key] = cloneEmbeddingModel(model)
	}
	return clone
}

// Snapshot returns a copy of registry providers and model metadata.
func (r *Registry) Snapshot() RegistrySnapshot {
	return RegistrySnapshot{
		Providers:       r.ListProviders(),
		ProviderAuths:   r.ListProviderAuths(),
		Models:          r.ListModels(),
		ImageModels:     r.ListImageModels(),
		EmbeddingModels: r.ListEmbeddingModels(),
	}
}

func newDefaultRegistry() *Registry {
	registry := NewRegistry()
	_ = registerBuiltinTextModels(registry)
	_ = registerBuiltinImageModels(registry)
	_ = registerBuiltinEmbeddingModels(registry)
	registerBuiltinProviderAuths(registry)
	return registry
}

func newRegistry() *Registry {
	return &Registry{
		providers:            make(map[ProviderID]providerRegistration),
		textModelSources:     make(map[ProviderID]textModelSourceRegistration),
		textModelSourceRefs:  make(map[ProviderID]map[ModelRef]struct{}),
		providerAuths:        make(map[ProviderID]ProviderAuth),
		models:               make(map[ModelRef]Model),
		imageModels:          make(map[ModelRef]ImageModel),
		imageModelSources:    make(map[ProviderID]imageModelSourceRegistration),
		imageModelSourceRefs: make(map[ProviderID]map[ModelRef]struct{}),
		embeddingModels:      make(map[ModelRef]EmbeddingModel),
	}
}

func (r *Registry) ensure() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.providers == nil {
		r.providers = make(map[ProviderID]providerRegistration)
	}
	if r.textModelSources == nil {
		r.textModelSources = make(map[ProviderID]textModelSourceRegistration)
	}
	if r.textModelSourceRefs == nil {
		r.textModelSourceRefs = make(map[ProviderID]map[ModelRef]struct{})
	}
	if r.providerAuths == nil {
		r.providerAuths = make(map[ProviderID]ProviderAuth)
	}
	if r.models == nil {
		r.models = make(map[ModelRef]Model)
	}
	if r.imageModels == nil {
		r.imageModels = make(map[ModelRef]ImageModel)
	}
	if r.imageModelSources == nil {
		r.imageModelSources = make(map[ProviderID]imageModelSourceRegistration)
	}
	if r.imageModelSourceRefs == nil {
		r.imageModelSourceRefs = make(map[ProviderID]map[ModelRef]struct{})
	}
	if r.embeddingModels == nil {
		r.embeddingModels = make(map[ModelRef]EmbeddingModel)
	}
}

func (r *Registry) removeTextModelSourceRefLocked(provider ProviderID, ref ModelRef) {
	refs := r.textModelSourceRefs[provider]
	if len(refs) == 0 {
		return
	}
	delete(refs, ref)
	if len(refs) == 0 {
		delete(r.textModelSourceRefs, provider)
	}
}

func (r *Registry) removeImageModelSourceRefLocked(provider ProviderID, ref ModelRef) {
	refs := r.imageModelSourceRefs[provider]
	if len(refs) == 0 {
		return
	}
	delete(refs, ref)
	if len(refs) == 0 {
		delete(r.imageModelSourceRefs, provider)
	}
}

type textSourceForRefresh struct {
	provider     ProviderID
	registration textModelSourceRegistration
}

type imageSourceForRefresh struct {
	provider     ProviderID
	registration imageModelSourceRegistration
}

func (r *Registry) textSourcesForRefresh(providers []ProviderID) ([]textSourceForRefresh, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(providers) == 0 {
		sources := make([]textSourceForRefresh, 0, len(r.textModelSourceOrder))
		for _, provider := range r.textModelSourceOrder {
			registration, ok := r.textModelSources[provider]
			if !ok {
				continue
			}
			sources = append(sources, textSourceForRefresh{
				provider:     provider,
				registration: registration,
			})
		}
		return sources, nil
	}

	sources := make([]textSourceForRefresh, 0, len(providers))
	for _, provider := range providers {
		if provider == "" {
			return nil, registryError("provider id is required")
		}
		registration, ok := r.textModelSources[provider]
		if !ok {
			return nil, registryError("text model source is not registered")
		}
		sources = append(sources, textSourceForRefresh{
			provider:     provider,
			registration: registration,
		})
	}
	return sources, nil
}

func (r *Registry) imageSourcesForRefresh(providers []ProviderID) ([]imageSourceForRefresh, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(providers) == 0 {
		sources := make([]imageSourceForRefresh, 0, len(r.imageModelSourceOrder))
		for _, provider := range r.imageModelSourceOrder {
			registration, ok := r.imageModelSources[provider]
			if !ok {
				continue
			}
			sources = append(sources, imageSourceForRefresh{
				provider:     provider,
				registration: registration,
			})
		}
		return sources, nil
	}

	sources := make([]imageSourceForRefresh, 0, len(providers))
	for _, provider := range providers {
		if provider == "" {
			return nil, registryError("provider id is required")
		}
		registration, ok := r.imageModelSources[provider]
		if !ok {
			return nil, registryError("image model source is not registered")
		}
		sources = append(sources, imageSourceForRefresh{
			provider:     provider,
			registration: registration,
		})
	}
	return sources, nil
}

func (r *Registry) applyTextModelRefresh(provider ProviderID, source textModelSourceRegistration, models []Model) error {
	refs, copied, err := r.validateTextModelRefresh(provider, source.metadataOnly, models)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	owned := r.textModelSourceRefs[provider]
	for _, ref := range refs {
		if _, exists := r.models[ref]; exists {
			if _, ok := owned[ref]; !ok {
				return registryConflict("text model source returned model already registered outside source")
			}
		}
	}

	ownedRefs := r.textModelSourceRefs[provider]
	if len(ownedRefs) > 0 {
		for ref := range ownedRefs {
			delete(r.models, ref)
		}
		r.modelOrder = removeModelRefs(r.modelOrder, ownedRefs)
	}
	for index, ref := range refs {
		r.models[ref] = copied[index]
		r.modelOrder = append(r.modelOrder, ref)
	}
	r.textModelSourceRefs[provider] = modelRefSet(refs)
	return nil
}

func (r *Registry) applyImageModelRefresh(provider ProviderID, source imageModelSourceRegistration, models []ImageModel) error {
	refs, copied, err := r.validateImageModelRefresh(provider, source.metadataOnly, models)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	owned := r.imageModelSourceRefs[provider]
	for _, ref := range refs {
		if _, exists := r.imageModels[ref]; exists {
			if _, ok := owned[ref]; !ok {
				return registryConflict("image model source returned model already registered outside source")
			}
		}
	}

	ownedRefs := r.imageModelSourceRefs[provider]
	if len(ownedRefs) > 0 {
		for ref := range ownedRefs {
			delete(r.imageModels, ref)
		}
		r.imageModelOrder = removeModelRefs(r.imageModelOrder, ownedRefs)
	}
	for index, ref := range refs {
		r.imageModels[ref] = copied[index]
		r.imageModelOrder = append(r.imageModelOrder, ref)
	}
	r.imageModelSourceRefs[provider] = modelRefSet(refs)
	return nil
}

func (r *Registry) validateTextModelRefresh(provider ProviderID, metadataOnly bool, models []Model) ([]ModelRef, []Model, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	refs := make([]ModelRef, 0, len(models))
	copied := make([]Model, 0, len(models))
	seen := make(map[ModelRef]struct{}, len(models))
	for _, model := range models {
		if model.Provider != provider {
			return nil, nil, registryError("text model source returned model for different provider")
		}
		if err := validateModel(model); err != nil {
			return nil, nil, err
		}
		if err := r.validateTextModelProviderLocked(model, metadataOnly); err != nil {
			return nil, nil, err
		}
		ref := ModelRef{Provider: model.Provider, ID: model.ID}
		if _, ok := seen[ref]; ok {
			return nil, nil, registryConflict("text model source returned duplicate model")
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
		copied = append(copied, cloneModel(model))
	}
	return refs, copied, nil
}

func (r *Registry) validateImageModelRefresh(provider ProviderID, metadataOnly bool, models []ImageModel) ([]ModelRef, []ImageModel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	refs := make([]ModelRef, 0, len(models))
	copied := make([]ImageModel, 0, len(models))
	seen := make(map[ModelRef]struct{}, len(models))
	for _, model := range models {
		if model.Provider != provider {
			return nil, nil, registryError("image model source returned model for different provider")
		}
		if err := validateImageModel(model); err != nil {
			return nil, nil, err
		}
		if err := r.validateImageModelProviderLocked(model, metadataOnly); err != nil {
			return nil, nil, err
		}
		ref := ModelRef{Provider: model.Provider, ID: model.ID}
		if _, ok := seen[ref]; ok {
			return nil, nil, registryConflict("image model source returned duplicate model")
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
		copied = append(copied, cloneImageModel(model))
	}
	return refs, copied, nil
}

func modelRefSet(refs []ModelRef) map[ModelRef]struct{} {
	if len(refs) == 0 {
		return nil
	}
	set := make(map[ModelRef]struct{}, len(refs))
	for _, ref := range refs {
		set[ref] = struct{}{}
	}
	return set
}

func copyModelRefSet(refs map[ModelRef]struct{}) map[ModelRef]struct{} {
	if len(refs) == 0 {
		return nil
	}
	copied := make(map[ModelRef]struct{}, len(refs))
	for ref := range refs {
		copied[ref] = struct{}{}
	}
	return copied
}

func removeModelRefs(refs []ModelRef, remove map[ModelRef]struct{}) []ModelRef {
	if len(remove) == 0 {
		return refs
	}
	filtered := refs[:0]
	for _, ref := range refs {
		if _, ok := remove[ref]; !ok {
			filtered = append(filtered, ref)
		}
	}
	return filtered
}

func (r *Registry) validateTextModelProviderLocked(model Model, metadataOnly bool) error {
	if model.API == "" || metadataOnly {
		return nil
	}
	registration, ok := r.providers[model.Provider]
	if !ok || !registration.textSet {
		return registryError("text provider must be registered before model")
	}
	if registration.textAPI != model.API {
		return registryError("text model api does not match registered provider")
	}
	return nil
}

func (r *Registry) validateImageModelProviderLocked(model ImageModel, metadataOnly bool) error {
	if model.API == "" || metadataOnly {
		return nil
	}
	registration, ok := r.providers[model.Provider]
	if !ok || !registration.imageSet {
		return registryError("image provider must be registered before model")
	}
	if registration.imageAPI != model.API {
		return registryError("image model api does not match registered provider")
	}
	return nil
}

func (r *Registry) validateEmbeddingModelProviderLocked(model EmbeddingModel, metadataOnly bool) error {
	if model.API == "" || metadataOnly {
		return nil
	}
	registration, ok := r.providers[model.Provider]
	if !ok || !registration.embeddingSet {
		return registryError("embedding provider must be registered before model")
	}
	if registration.embeddingAPI != model.API {
		return registryError("embedding model api does not match registered provider")
	}
	return nil
}

func applyRegisterOptions(options []RegisterOption) registerOptions {
	var applied registerOptions
	for _, option := range options {
		if option != nil {
			option(&applied)
		}
	}
	return applied
}

func validateModel(model Model) error {
	if err := ValidateModelRef(ModelRef{Provider: model.Provider, ID: model.ID}); err != nil {
		return err
	}
	if model.API == "" {
		return registryError("model api is required")
	}
	if model.ContextWindow < 0 {
		return registryError("context window must be non-negative")
	}
	if model.MaxOutputTokens < 0 {
		return registryError("max output tokens must be non-negative")
	}
	if err := validateCosts(model); err != nil {
		return err
	}
	if err := validateSupportedInputs(model.SupportedInputs); err != nil {
		return err
	}
	if err := validateOpenAICompatibleModel(model); err != nil {
		return err
	}
	return nil
}

func validateImageModel(model ImageModel) error {
	return ValidateModelRef(ModelRef{Provider: model.Provider, ID: model.ID})
}

func validateEmbeddingModel(model EmbeddingModel) error {
	if err := ValidateModelRef(ModelRef{Provider: model.Provider, ID: model.ID}); err != nil {
		return err
	}
	if model.API == "" {
		return registryError("embedding model api is required")
	}
	if model.DefaultDimensions < 0 {
		return registryError("embedding default dimensions must be non-negative")
	}
	if model.MinDimensions < 0 {
		return registryError("embedding min dimensions must be non-negative")
	}
	if model.MaxDimensions < 0 {
		return registryError("embedding max dimensions must be non-negative")
	}
	if model.MinDimensions > 0 && model.MaxDimensions > 0 && model.MinDimensions > model.MaxDimensions {
		return registryError("embedding min dimensions must be less than or equal to max dimensions")
	}
	if model.DefaultDimensions > 0 && model.MinDimensions > 0 && model.DefaultDimensions < model.MinDimensions {
		return registryError("embedding default dimensions must be within supported dimensions")
	}
	if model.DefaultDimensions > 0 && model.MaxDimensions > 0 && model.DefaultDimensions > model.MaxDimensions {
		return registryError("embedding default dimensions must be within supported dimensions")
	}
	if model.MaxInputTokens < 0 {
		return registryError("embedding max input tokens must be non-negative")
	}
	if model.MaxBatchInputs < 0 {
		return registryError("embedding max batch inputs must be non-negative")
	}
	if model.MaxBatchBytes < 0 {
		return registryError("embedding max batch bytes must be non-negative")
	}
	if model.InputCostPerMillion < 0 {
		return registryError("embedding input cost per million must be non-negative")
	}
	if err := validateOpenAICompatibleEmbeddingModel(model); err != nil {
		return err
	}
	return nil
}

func registryError(message string) error {
	return &Error{Code: ErrorUnsupported, Message: message}
}

func registryConflict(message string) error {
	return &Error{Code: ErrorUnsupported, Message: message}
}

func validateCosts(model Model) error {
	costs := []struct {
		name  string
		value float64
	}{
		{name: "input cost per million", value: model.InputCostPerMillion},
		{name: "output cost per million", value: model.OutputCostPerMillion},
		{name: "cache read input cost per million", value: model.CacheReadInputCostPerMillion},
		{name: "cache write input cost per million", value: model.CacheWriteInputCostPerMillion},
	}
	for _, cost := range costs {
		if cost.value < 0 {
			return registryError(cost.name + " must be non-negative")
		}
	}
	return nil
}

func validateSupportedInputs(inputs []ContentBlockType) error {
	for _, input := range inputs {
		switch input {
		case ContentBlockText, ContentBlockImage:
		default:
			return registryError(fmt.Sprintf("unsupported model input %q", input))
		}
	}
	return nil
}

func validateOpenAICompatibleModel(model Model) error {
	if model.OpenAICompletionsCompat != nil && model.API != APIOpenAICompletions {
		return registryError("openai completions compatibility requires api openai-completions")
	}
	if compatible, _ := model.ProviderMetadata[MetadataOpenAICompatible].(bool); !compatible {
		return nil
	}
	if model.API != APIOpenAICompletions {
		return registryError("openai-compatible model api must be openai-completions")
	}
	baseURL, _ := model.ProviderMetadata[MetadataOpenAICompatibleBaseURL].(string)
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return registryError("openai-compatible model base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return registryError("openai-compatible model base URL must be absolute")
	}
	return validateOpenAICompatibleHeaders(model.ProviderMetadata[MetadataOpenAICompatibleHeaders])
}

func validateOpenAICompatibleEmbeddingModel(model EmbeddingModel) error {
	if compatible, _ := model.ProviderMetadata[MetadataOpenAICompatible].(bool); !compatible {
		return nil
	}
	if model.API != EmbeddingAPIOpenAIEmbeddings {
		return registryError("openai-compatible embedding model api must be openai-embeddings")
	}
	baseURL, _ := model.ProviderMetadata[MetadataOpenAICompatibleBaseURL].(string)
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return registryError("openai-compatible embedding model base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return registryError("openai-compatible embedding model base URL must be absolute")
	}
	return validateOpenAICompatibleHeaders(model.ProviderMetadata[MetadataOpenAICompatibleHeaders])
}

func validateOpenAICompatibleHeaders(value any) error {
	switch headers := value.(type) {
	case nil:
		return nil
	case map[string]string:
		for key := range headers {
			if strings.TrimSpace(key) == "" {
				return registryError("openai-compatible model header name is required")
			}
		}
	case map[string]any:
		for key, value := range headers {
			if strings.TrimSpace(key) == "" {
				return registryError("openai-compatible model header name is required")
			}
			if _, ok := value.(string); !ok {
				return registryError("openai-compatible model headers must be strings")
			}
		}
	default:
		return registryError("openai-compatible model headers must be a string map")
	}
	return nil
}
