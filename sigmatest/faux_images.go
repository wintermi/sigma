// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigmatest

import (
	"context"
	"sync"
	"time"

	"github.com/wintermi/sigma"
)

const (
	// ImageAPI is the provider API reported by FauxImageProvider.
	ImageAPI sigma.ImageAPI = "sigmatest-images"
	// ImageModelID is the model id returned by ImageModel.
	ImageModelID sigma.ModelID = "sigmatest-image"
)

// ImageScript describes one deterministic image provider response.
//
// Each call to FauxImageProvider.Generate consumes one script. Delay waits
// before the terminal result. WaitForCancel makes the script block until the
// request context is canceled.
type ImageScript struct {
	Response      sigma.AssistantImages
	Err           error
	Delay         time.Duration
	WaitForCancel bool
}

// ImageRequestCapture is an immutable-by-convention copy of one image request.
type ImageRequestCapture struct {
	Model   sigma.ImageModel
	Request sigma.ImageRequest
	Options sigma.Options
}

// FauxImageProvider is a deterministic sigma.ImageProvider for tests and examples.
type FauxImageProvider struct {
	mu       sync.Mutex
	scripts  []ImageScript
	requests []ImageRequestCapture
}

// NewFauxImageProvider constructs an image provider that consumes scripts in call order.
func NewFauxImageProvider(scripts ...ImageScript) *FauxImageProvider {
	provider := &FauxImageProvider{}
	provider.Enqueue(scripts...)
	return provider
}

// API reports the faux image API.
func (p *FauxImageProvider) API() sigma.ImageAPI {
	return ImageAPI
}

// Enqueue appends scripts to be consumed by future Generate calls.
func (p *FauxImageProvider) Enqueue(scripts ...ImageScript) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, script := range scripts {
		p.scripts = append(p.scripts, cloneImageScript(script))
	}
}

// Requests returns copies of image requests received by the provider.
func (p *FauxImageProvider) Requests() []ImageRequestCapture {
	p.mu.Lock()
	defer p.mu.Unlock()

	requests := make([]ImageRequestCapture, len(p.requests))
	for i, request := range p.requests {
		requests[i] = cloneImageCapture(request)
	}
	return requests
}

// LastRequest returns a copy of the most recent image request.
func (p *FauxImageProvider) LastRequest() (ImageRequestCapture, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.requests) == 0 {
		return ImageRequestCapture{}, false
	}
	return cloneImageCapture(p.requests[len(p.requests)-1]), true
}

// Generate records the request and returns the next scripted image response.
func (p *FauxImageProvider) Generate(ctx context.Context, model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) (sigma.AssistantImages, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	script := p.nextImageScript(model, req, opts)
	if script.WaitForCancel {
		<-ctx.Done()
		return finalImageResponse(model, script.Response, sigma.StopReasonAborted), ctx.Err()
	}
	if !wait(ctx, script.Delay) {
		return finalImageResponse(model, script.Response, sigma.StopReasonAborted), ctx.Err()
	}
	if script.Err != nil {
		return finalImageResponse(model, script.Response, sigma.StopReasonError), script.Err
	}
	return finalImageResponse(model, script.Response, sigma.StopReasonEndTurn), nil
}

func (p *FauxImageProvider) nextImageScript(model sigma.ImageModel, req sigma.ImageRequest, opts sigma.Options) ImageScript {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.requests = append(p.requests, ImageRequestCapture{
		Model:   cloneImageModel(model),
		Request: cloneImageRequest(req),
		Options: cloneOptions(opts),
	})

	if len(p.scripts) == 0 {
		return ImageScript{}
	}
	script := p.scripts[0]
	p.scripts = p.scripts[1:]
	return cloneImageScript(script)
}

// ImageModel returns an image model suitable for isolated sigmatest registries.
func ImageModel() sigma.ImageModel {
	return sigma.ImageModel{
		ID:               ImageModelID,
		Provider:         ProviderID,
		API:              ImageAPI,
		Name:             "Sigma Test Image",
		MaxWidth:         1536,
		MaxHeight:        1536,
		SupportedSizes:   []string{string(sigma.ImageSize1024x1024), string(sigma.ImageSize1024x1536), string(sigma.ImageSize1536x1024)},
		SupportedFormats: []string{"image/png", "image/jpeg", "image/webp"},
	}
}

// RegisterImages adds an image provider and image models to registry. If no
// models are supplied, ImageModel is registered.
func RegisterImages(registry *sigma.Registry, provider *FauxImageProvider, models ...sigma.ImageModel) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	if provider == nil {
		provider = NewFauxImageProvider()
	}
	if len(models) == 0 {
		models = []sigma.ImageModel{ImageModel()}
	}
	if err := registry.RegisterImageProvider(ProviderID, provider); err != nil {
		return err
	}
	for _, model := range models {
		if err := registry.RegisterImageModel(model); err != nil {
			return err
		}
	}
	return nil
}

// ImageRegistry constructs an isolated registry containing image provider and models.
func ImageRegistry(provider *FauxImageProvider, models ...sigma.ImageModel) (*sigma.Registry, error) {
	registry := sigma.NewRegistry()
	if err := RegisterImages(registry, provider, models...); err != nil {
		return nil, err
	}
	return registry, nil
}

func finalImageResponse(model sigma.ImageModel, response sigma.AssistantImages, defaultStop sigma.StopReason) sigma.AssistantImages {
	response = cloneAssistantImages(response)
	if response.Model == "" {
		response.Model = model.ID
	}
	if response.Provider == "" {
		response.Provider = model.Provider
	}
	if response.StopReason == "" {
		response.StopReason = defaultStop
	}
	return response
}

func cloneImageScript(script ImageScript) ImageScript {
	script.Response = cloneAssistantImages(script.Response)
	return script
}

func cloneImageCapture(capture ImageRequestCapture) ImageRequestCapture {
	return ImageRequestCapture{
		Model:   cloneImageModel(capture.Model),
		Request: cloneImageRequest(capture.Request),
		Options: cloneOptions(capture.Options),
	}
}

func cloneImageModel(model sigma.ImageModel) sigma.ImageModel {
	model.SupportedSizes = append([]string(nil), model.SupportedSizes...)
	model.SupportedFormats = append([]string(nil), model.SupportedFormats...)
	model.ProviderMetadata = cloneMap(model.ProviderMetadata)
	return model
}

func cloneImageRequest(req sigma.ImageRequest) sigma.ImageRequest {
	req.Inputs = cloneImageInputs(req.Inputs)
	req.Mask = cloneImageInputPtr(req.Mask)
	req.ProviderMetadata = cloneMap(req.ProviderMetadata)
	return req
}

func cloneAssistantImages(response sigma.AssistantImages) sigma.AssistantImages {
	response.Images = cloneImageInputs(response.Images)
	response.Errors = append([]sigma.ImageError(nil), response.Errors...)
	for i := range response.Errors {
		response.Errors[i].ProviderMetadata = cloneMap(response.Errors[i].ProviderMetadata)
	}
	response.Usage = ptrCopy(response.Usage)
	response.Cost = ptrCopy(response.Cost)
	response.ProviderMetadata = cloneMap(response.ProviderMetadata)
	return response
}

func cloneImageInputs(inputs []sigma.ImageInput) []sigma.ImageInput {
	return append([]sigma.ImageInput(nil), inputs...)
}

func cloneImageInputPtr(input *sigma.ImageInput) *sigma.ImageInput {
	if input == nil {
		return nil
	}
	copied := *input
	return &copied
}
