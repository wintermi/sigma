// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigmatest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wintermi/sigma"
)

const (
	// EmbeddingAPI is the provider API reported by FauxEmbeddingProvider.
	EmbeddingAPI sigma.EmbeddingAPI = "sigmatest-embeddings"
	// EmbeddingModelID is the model id returned by EmbeddingModel.
	EmbeddingModelID sigma.ModelID = "sigmatest-embedding"
)

// EmbeddingScript describes one deterministic embedding provider response.
type EmbeddingScript struct {
	Response      sigma.Embeddings
	Err           error
	Delay         time.Duration
	WaitForCancel bool
}

// EmbeddingRequestCapture is an immutable-by-convention copy of one embedding request.
type EmbeddingRequestCapture struct {
	Model   sigma.EmbeddingModel
	Request sigma.EmbeddingRequest
	Options sigma.Options
}

// FauxEmbeddingProvider is a deterministic sigma.EmbeddingProvider for tests.
type FauxEmbeddingProvider struct {
	mu       sync.Mutex
	scripts  []EmbeddingScript
	requests []EmbeddingRequestCapture
}

// NewFauxEmbeddingProvider constructs an embedding provider that consumes scripts in call order.
func NewFauxEmbeddingProvider(scripts ...EmbeddingScript) *FauxEmbeddingProvider {
	provider := &FauxEmbeddingProvider{}
	provider.Enqueue(scripts...)
	return provider
}

// API reports the faux embedding API.
func (p *FauxEmbeddingProvider) API() sigma.EmbeddingAPI {
	return EmbeddingAPI
}

// Enqueue appends scripts to be consumed by future Embed calls.
func (p *FauxEmbeddingProvider) Enqueue(scripts ...EmbeddingScript) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, script := range scripts {
		p.scripts = append(p.scripts, cloneEmbeddingScript(script))
	}
}

// Requests returns copies of embedding requests received by the provider.
func (p *FauxEmbeddingProvider) Requests() []EmbeddingRequestCapture {
	p.mu.Lock()
	defer p.mu.Unlock()

	requests := make([]EmbeddingRequestCapture, len(p.requests))
	for i, request := range p.requests {
		requests[i] = cloneEmbeddingCapture(request)
	}
	return requests
}

// LastRequest returns a copy of the most recent embedding request.
func (p *FauxEmbeddingProvider) LastRequest() (EmbeddingRequestCapture, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.requests) == 0 {
		return EmbeddingRequestCapture{}, false
	}
	return cloneEmbeddingCapture(p.requests[len(p.requests)-1]), true
}

// Embed records the request and returns the next scripted embedding response.
func (p *FauxEmbeddingProvider) Embed(ctx context.Context, model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) (sigma.Embeddings, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	script := p.nextEmbeddingScript(model, req, opts)
	if script.WaitForCancel {
		<-ctx.Done()
		return finalEmbeddings(model, script.Response), ctx.Err()
	}
	if !wait(ctx, script.Delay) {
		return finalEmbeddings(model, script.Response), ctx.Err()
	}
	if script.Err != nil {
		return finalEmbeddings(model, script.Response), script.Err
	}
	return finalEmbeddings(model, script.Response), nil
}

func (p *FauxEmbeddingProvider) nextEmbeddingScript(model sigma.EmbeddingModel, req sigma.EmbeddingRequest, opts sigma.Options) EmbeddingScript {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.requests = append(p.requests, EmbeddingRequestCapture{
		Model:   cloneEmbeddingModel(model),
		Request: cloneEmbeddingRequest(req),
		Options: cloneOptions(opts),
	})

	if len(p.scripts) == 0 {
		return EmbeddingScript{}
	}
	script := p.scripts[0]
	p.scripts = p.scripts[1:]
	return cloneEmbeddingScript(script)
}

// EmbeddingModel returns an embedding model suitable for isolated sigmatest registries.
func EmbeddingModel() sigma.EmbeddingModel {
	return sigma.EmbeddingModel{
		ID:                  EmbeddingModelID,
		Provider:            ProviderID,
		API:                 EmbeddingAPI,
		Name:                "Sigma Test Embedding",
		DefaultDimensions:   3,
		MaxInputTokens:      8192,
		InputCostPerMillion: 0.01,
		CostCurrency:        "USD",
	}
}

// RegisterEmbeddings adds an embedding provider and models to registry.
func RegisterEmbeddings(registry *sigma.Registry, provider *FauxEmbeddingProvider, models ...sigma.EmbeddingModel) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	if provider == nil {
		provider = NewFauxEmbeddingProvider()
	}
	if len(models) == 0 {
		models = []sigma.EmbeddingModel{EmbeddingModel()}
	}
	if err := registry.RegisterEmbeddingProvider(ProviderID, provider); err != nil {
		return fmt.Errorf("register embedding provider: %w", err)
	}
	for _, model := range models {
		if err := registry.RegisterEmbeddingModel(model); err != nil {
			return fmt.Errorf("register embedding model: %w", err)
		}
	}
	return nil
}

// EmbeddingRegistry constructs an isolated registry containing embedding provider and models.
func EmbeddingRegistry(provider *FauxEmbeddingProvider, models ...sigma.EmbeddingModel) (*sigma.Registry, error) {
	registry := sigma.NewRegistry()
	if err := RegisterEmbeddings(registry, provider, models...); err != nil {
		return nil, err
	}
	return registry, nil
}

func finalEmbeddings(model sigma.EmbeddingModel, response sigma.Embeddings) sigma.Embeddings {
	response = cloneEmbeddings(response)
	if response.Model == "" {
		response.Model = model.ID
	}
	if response.Provider == "" {
		response.Provider = model.Provider
	}
	if response.Usage != nil && response.Cost == nil {
		cost := sigma.CostForEmbeddingUsage(model, *response.Usage)
		response.Cost = &cost
	}
	return response
}

func cloneEmbeddingScript(script EmbeddingScript) EmbeddingScript {
	script.Response = cloneEmbeddings(script.Response)
	return script
}

func cloneEmbeddingCapture(capture EmbeddingRequestCapture) EmbeddingRequestCapture {
	return EmbeddingRequestCapture{
		Model:   cloneEmbeddingModel(capture.Model),
		Request: cloneEmbeddingRequest(capture.Request),
		Options: cloneOptions(capture.Options),
	}
}

func cloneEmbeddingModel(model sigma.EmbeddingModel) sigma.EmbeddingModel {
	model.ProviderMetadata = cloneMap(model.ProviderMetadata)
	return model
}

func cloneEmbeddingRequest(req sigma.EmbeddingRequest) sigma.EmbeddingRequest {
	req.Inputs = append([]string(nil), req.Inputs...)
	req.ProviderMetadata = cloneMap(req.ProviderMetadata)
	return req
}

func cloneEmbeddings(response sigma.Embeddings) sigma.Embeddings {
	response.Vectors = append([]sigma.Embedding(nil), response.Vectors...)
	for i := range response.Vectors {
		response.Vectors[i].Vector = append([]float32(nil), response.Vectors[i].Vector...)
	}
	response.Usage = ptrCopy(response.Usage)
	response.Cost = ptrCopy(response.Cost)
	response.ProviderMetadata = cloneMap(response.ProviderMetadata)
	return response
}
