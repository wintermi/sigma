// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigmatest

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/wintermi/sigma"
)

const (
	// ProviderID is the provider id used by TextModel and registry helpers.
	ProviderID sigma.ProviderID = "sigmatest"
	// TextAPI is the provider API reported by FauxProvider.
	TextAPI sigma.API = "sigmatest-text"
	// TextModelID is the model id returned by TextModel.
	TextModelID sigma.ModelID = "sigmatest-text"
)

var errScriptExhausted = errors.New("sigmatest: no scripted response queued")

// Script describes one deterministic provider response.
//
// Each call to FauxProvider.Stream consumes one script. Events are emitted in
// order, then either Err or Final terminates the stream. Delay waits before
// each scripted event and before the terminal result. WaitForCancel makes the
// script block until the request context is canceled.
type Script struct {
	Events        []sigma.Event
	Final         sigma.AssistantMessage
	Err           error
	Delay         time.Duration
	WaitForCancel bool
}

// RequestCapture is an immutable-by-convention copy of one provider request.
type RequestCapture struct {
	Model   sigma.Model
	Request sigma.Request
	Options sigma.Options
}

// FauxProvider is a deterministic sigma.TextProvider for tests and examples.
type FauxProvider struct {
	mu       sync.Mutex
	scripts  []Script
	requests []RequestCapture
}

// NewFauxProvider constructs a provider that consumes scripts in call order.
func NewFauxProvider(scripts ...Script) *FauxProvider {
	provider := &FauxProvider{}
	provider.Enqueue(scripts...)
	return provider
}

// API reports the faux text API.
func (p *FauxProvider) API() sigma.API {
	return TextAPI
}

// Enqueue appends scripts to be consumed by future Stream calls.
func (p *FauxProvider) Enqueue(scripts ...Script) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, script := range scripts {
		p.scripts = append(p.scripts, cloneScript(script))
	}
}

// Requests returns copies of requests received by the provider.
func (p *FauxProvider) Requests() []RequestCapture {
	p.mu.Lock()
	defer p.mu.Unlock()

	requests := make([]RequestCapture, len(p.requests))
	for i, request := range p.requests {
		requests[i] = cloneCapture(request)
	}
	return requests
}

// LastRequest returns a copy of the most recent provider request.
func (p *FauxProvider) LastRequest() (RequestCapture, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.requests) == 0 {
		return RequestCapture{}, false
	}
	return cloneCapture(p.requests[len(p.requests)-1]), true
}

// Stream records the request and emits the next scripted stream.
func (p *FauxProvider) Stream(ctx context.Context, model sigma.Model, req sigma.Request, opts sigma.Options) *sigma.Stream {
	if ctx == nil {
		ctx = context.Background()
	}
	script := p.nextScript(model, req, opts)
	stream, writer := sigma.NewStream(ctx)

	go func() {
		if script.WaitForCancel {
			<-ctx.Done()
			return
		}
		events := script.Events
		if len(events) == 0 && script.Err == nil {
			events = synthesizeEvents(script.Final)
		}
		for _, event := range events {
			if !wait(ctx, script.Delay) {
				return
			}
			if err := writer.Emit(ctx, cloneEvent(event)); err != nil {
				return
			}
		}
		if !wait(ctx, script.Delay) {
			return
		}
		if script.Err != nil {
			_ = writer.Error(ctx, script.Err, finalMessage(model, script.Final, sigma.StopReasonError))
			return
		}
		_ = writer.Done(ctx, finalMessage(model, script.Final, sigma.StopReasonEndTurn))
	}()

	return stream
}

func (p *FauxProvider) nextScript(model sigma.Model, req sigma.Request, opts sigma.Options) Script {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.requests = append(p.requests, RequestCapture{
		Model:   cloneModel(model),
		Request: cloneRequest(req),
		Options: cloneOptions(opts),
	})

	if len(p.scripts) == 0 {
		return Script{Err: errScriptExhausted}
	}
	script := p.scripts[0]
	p.scripts = p.scripts[1:]
	return cloneScript(script)
}

// TextModel returns a model suitable for isolated sigmatest registries.
func TextModel() sigma.Model {
	return sigma.Model{
		ID:               TextModelID,
		Provider:         ProviderID,
		API:              TextAPI,
		Name:             "Sigma Test Text",
		SupportedInputs:  []sigma.ContentBlockType{sigma.ContentBlockText},
		SupportsTools:    true,
		SupportsThinking: true,
		ThinkingLevels: []sigma.ThinkingLevel{
			sigma.ThinkingLevelLow,
			sigma.ThinkingLevelMedium,
			sigma.ThinkingLevelHigh,
		},
	}
}

// Register adds provider and models to registry. If no models are supplied,
// TextModel is registered.
func Register(registry *sigma.Registry, provider *FauxProvider, models ...sigma.Model) error {
	if registry == nil {
		return &sigma.Error{Code: sigma.ErrorUnsupported, Message: "registry is required"}
	}
	if provider == nil {
		provider = NewFauxProvider()
	}
	if len(models) == 0 {
		models = []sigma.Model{TextModel()}
	}
	if err := registry.RegisterTextProvider(ProviderID, provider); err != nil {
		return err
	}
	for _, model := range models {
		if err := registry.RegisterModel(model); err != nil {
			return err
		}
	}
	return nil
}

// Registry constructs an isolated registry containing provider and models.
func Registry(provider *FauxProvider, models ...sigma.Model) (*sigma.Registry, error) {
	registry := sigma.NewRegistry()
	if err := Register(registry, provider, models...); err != nil {
		return nil, err
	}
	return registry, nil
}

func wait(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func finalMessage(model sigma.Model, final sigma.AssistantMessage, defaultStop sigma.StopReason) sigma.AssistantMessage {
	final = cloneAssistant(final)
	if final.Model == "" {
		final.Model = model.ID
	}
	if final.Provider == "" {
		final.Provider = model.Provider
	}
	if final.StopReason == "" {
		final.StopReason = defaultStop
	}
	return final
}

func synthesizeEvents(final sigma.AssistantMessage) []sigma.Event {
	events := []sigma.Event{{Kind: sigma.EventKindStart}}
	for index, block := range final.Content {
		contentIndex := index
		switch block.Type {
		case sigma.ContentBlockText:
			events = append(events,
				sigma.Event{Kind: sigma.EventKindTextStart, ContentIndex: &contentIndex},
				sigma.Event{Kind: sigma.EventKindTextDelta, ContentIndex: &contentIndex, DeltaText: block.Text, Text: block.Text},
				sigma.Event{Kind: sigma.EventKindTextEnd, ContentIndex: &contentIndex, Text: block.Text},
			)
		case sigma.ContentBlockThinking:
			events = append(events,
				sigma.Event{Kind: sigma.EventKindThinkingStart, ContentIndex: &contentIndex},
				sigma.Event{Kind: sigma.EventKindThinkingDelta, ContentIndex: &contentIndex, DeltaText: block.ThinkingText, Thinking: block.ThinkingText},
				sigma.Event{Kind: sigma.EventKindThinkingEnd, ContentIndex: &contentIndex, Thinking: block.ThinkingText},
			)
		case sigma.ContentBlockToolCall:
			arguments := toolArgumentsText(block.ToolArguments)
			events = append(events,
				sigma.Event{
					Kind:         sigma.EventKindToolCallStart,
					ContentIndex: &contentIndex,
					PartialToolCall: &sigma.PartialToolCall{
						ID:   block.ToolCallID,
						Name: block.ToolName,
					},
				},
				sigma.Event{
					Kind:         sigma.EventKindToolCallDelta,
					ContentIndex: &contentIndex,
					PartialToolCall: &sigma.PartialToolCall{
						ArgumentsDelta: arguments,
					},
				},
				sigma.Event{
					Kind:         sigma.EventKindToolCallEnd,
					ContentIndex: &contentIndex,
					ToolCall: &sigma.ToolCall{
						ID:                block.ToolCallID,
						Name:              block.ToolName,
						Arguments:         cloneAny(block.ToolArguments),
						ProviderSignature: block.ProviderSignature,
						ProviderMetadata:  cloneMap(block.ProviderMetadata),
					},
				},
			)
		case sigma.ContentBlockImage, sigma.ContentBlockDocument:
		}
	}
	return events
}

func toolArgumentsText(arguments any) string {
	if arguments == nil {
		return "{}"
	}
	data, err := json.Marshal(arguments)
	if err != nil {
		return ""
	}
	return string(data)
}

func cloneScript(script Script) Script {
	script.Events = cloneEvents(script.Events)
	script.Final = cloneAssistant(script.Final)
	return script
}

func cloneCapture(capture RequestCapture) RequestCapture {
	return RequestCapture{
		Model:   cloneModel(capture.Model),
		Request: cloneRequest(capture.Request),
		Options: cloneOptions(capture.Options),
	}
}

func cloneModel(model sigma.Model) sigma.Model {
	model.SupportedInputs = slices.Clone(model.SupportedInputs)
	model.ThinkingLevels = slices.Clone(model.ThinkingLevels)
	model.ThinkingLevelMap = maps.Clone(model.ThinkingLevelMap)
	model.ProviderMetadata = cloneMap(model.ProviderMetadata)
	return model
}

func cloneRequest(req sigma.Request) sigma.Request {
	req.Messages = slices.Clone(req.Messages)
	for i := range req.Messages {
		req.Messages[i].Content = cloneContent(req.Messages[i].Content)
	}
	req.Tools = slices.Clone(req.Tools)
	for i := range req.Tools {
		req.Tools[i].InputSchema = cloneAny(req.Tools[i].InputSchema)
		req.Tools[i].ProviderMetadata = cloneMap(req.Tools[i].ProviderMetadata)
	}
	return req
}

func cloneOptions(options sigma.Options) sigma.Options {
	options.Temperature = ptrCopy(options.Temperature)
	options.MaxTokens = ptrCopy(options.MaxTokens)
	options.Headers = maps.Clone(options.Headers)
	options.Timeout = ptrCopy(options.Timeout)
	options.MaxRetries = ptrCopy(options.MaxRetries)
	options.MaxRetryDelay = ptrCopy(options.MaxRetryDelay)
	options.Metadata = cloneMap(options.Metadata)
	options.ThinkingBudgetTokens = ptrCopy(options.ThinkingBudgetTokens)
	options.ProviderAuthResolvers = maps.Clone(options.ProviderAuthResolvers)
	options.ProviderOptions = cloneProviderOptions(options.ProviderOptions)
	options.OpenAIOptions = ptrCopy(options.OpenAIOptions)
	options.AnthropicOptions = cloneAnthropicOptions(options.AnthropicOptions)
	options.GoogleOptions = cloneGoogleOptions(options.GoogleOptions)
	return options
}

func cloneProviderOptions(values map[sigma.ProviderID]map[string]any) map[sigma.ProviderID]map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[sigma.ProviderID]map[string]any, len(values))
	for provider, providerValues := range values {
		copied[provider] = cloneMap(providerValues)
	}
	return copied
}

func cloneAnthropicOptions(options *sigma.AnthropicOptions) *sigma.AnthropicOptions {
	copied := ptrCopy(options)
	if copied != nil {
		copied.ThinkingBudgetTokens = ptrCopy(options.ThinkingBudgetTokens)
	}
	return copied
}

func cloneGoogleOptions(options *sigma.GoogleOptions) *sigma.GoogleOptions {
	copied := ptrCopy(options)
	if copied != nil {
		copied.ThinkingBudgetTokens = ptrCopy(options.ThinkingBudgetTokens)
	}
	return copied
}

func cloneAssistant(message sigma.AssistantMessage) sigma.AssistantMessage {
	message.Content = cloneContent(message.Content)
	message.Usage = ptrCopy(message.Usage)
	message.Cost = ptrCopy(message.Cost)
	message.ProviderMetadata = cloneMap(message.ProviderMetadata)
	message.Diagnostics = slices.Clone(message.Diagnostics)
	return message
}

func cloneContent(content []sigma.ContentBlock) []sigma.ContentBlock {
	content = slices.Clone(content)
	for i := range content {
		content[i].ToolArguments = cloneAny(content[i].ToolArguments)
		content[i].ProviderMetadata = cloneMap(content[i].ProviderMetadata)
		content[i].ExtraFields = cloneMap(content[i].ExtraFields)
	}
	return content
}

func cloneEvents(events []sigma.Event) []sigma.Event {
	events = slices.Clone(events)
	for i := range events {
		events[i] = cloneEvent(events[i])
	}
	return events
}

func cloneEvent(event sigma.Event) sigma.Event {
	event.ContentIndex = ptrCopy(event.ContentIndex)
	if event.Image != nil {
		image := *event.Image
		image.ToolArguments = cloneAny(image.ToolArguments)
		image.ProviderMetadata = cloneMap(image.ProviderMetadata)
		image.ExtraFields = cloneMap(image.ExtraFields)
		event.Image = &image
	}
	if event.PartialImage != nil {
		image := *event.PartialImage
		image.ToolArguments = cloneAny(image.ToolArguments)
		image.ProviderMetadata = cloneMap(image.ProviderMetadata)
		image.ExtraFields = cloneMap(image.ExtraFields)
		event.PartialImage = &image
	}
	if event.ToolCall != nil {
		toolCall := *event.ToolCall
		toolCall.Arguments = cloneAny(toolCall.Arguments)
		toolCall.ProviderMetadata = cloneMap(toolCall.ProviderMetadata)
		event.ToolCall = &toolCall
	}
	if event.PartialToolCall != nil {
		partial := *event.PartialToolCall
		partial.ProviderMetadata = cloneMap(partial.ProviderMetadata)
		event.PartialToolCall = &partial
	}
	if event.PartialMessage != nil {
		message := cloneAssistant(*event.PartialMessage)
		event.PartialMessage = &message
	}
	if event.FinalMessage != nil {
		message := cloneAssistant(*event.FinalMessage)
		event.FinalMessage = &message
	}
	event.Usage = ptrCopy(event.Usage)
	return event
}

func cloneMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = cloneAny(value)
	}
	return copied
}

func cloneAny(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneMap(value)
	case []any:
		copied := slices.Clone(value)
		for i, item := range copied {
			copied[i] = cloneAny(item)
		}
		return copied
	default:
		return value
	}
}

func ptrCopy[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
