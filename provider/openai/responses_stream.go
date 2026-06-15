// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamblocks"
)

type responsesEvent struct {
	Type         string               `json:"type"`
	ResponseID   string               `json:"response_id"`
	Model        string               `json:"model"`
	ItemID       string               `json:"item_id"`
	OutputIndex  int                  `json:"output_index"`
	ContentIndex int                  `json:"content_index"`
	SummaryIndex int                  `json:"summary_index"`
	Delta        string               `json:"delta"`
	Text         string               `json:"text"`
	Arguments    string               `json:"arguments"`
	B64JSON      string               `json:"b64_json"`
	PartialImage string               `json:"partial_image_b64"`
	ImageURL     string               `json:"image_url"`
	Item         responsesOutputItem  `json:"item"`
	Part         responsesContentPart `json:"part"`
	Response     responsesResponse    `json:"response"`
	Error        *responsesError      `json:"error"`
	Sequence     int                  `json:"sequence_number"`
}

type responsesResponse struct {
	ID                string                `json:"id"`
	Model             string                `json:"model"`
	Status            string                `json:"status"`
	ServiceTier       string                `json:"service_tier"`
	Output            []responsesOutputItem `json:"output"`
	Usage             *responsesUsage       `json:"usage"`
	Error             *responsesError       `json:"error"`
	IncompleteDetails *incompleteDetails    `json:"incomplete_details"`
}

type responsesOutputItem struct {
	ID               string                 `json:"id"`
	Type             string                 `json:"type"`
	Role             string                 `json:"role"`
	Status           string                 `json:"status"`
	Content          []responsesContentPart `json:"content"`
	Summary          []responsesSummaryPart `json:"summary"`
	CallID           string                 `json:"call_id"`
	Name             string                 `json:"name"`
	Arguments        string                 `json:"arguments"`
	Result           string                 `json:"result"`
	B64JSON          string                 `json:"b64_json"`
	ImageURL         string                 `json:"image_url"`
	EncryptedContent string                 `json:"encrypted_content"`
	Signature        string                 `json:"signature"`
}

type responsesContentPart struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	Refusal   string `json:"refusal"`
	Signature string `json:"signature"`
}

type responsesSummaryPart struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Signature string `json:"signature"`
}

type responsesUsage struct {
	InputTokens         int                          `json:"input_tokens"`
	OutputTokens        int                          `json:"output_tokens"`
	TotalTokens         int                          `json:"total_tokens"`
	InputTokensDetails  *responsesInputTokenDetails  `json:"input_tokens_details"`
	OutputTokensDetails *responsesOutputTokenDetails `json:"output_tokens_details"`
	Cost                *float64                     `json:"cost"`
	Currency            string                       `json:"currency"`
}

type responsesInputTokenDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type responsesOutputTokenDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type responsesError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

type incompleteDetails struct {
	Reason string `json:"reason"`
}

type responsesStreamParser struct {
	writer              sigma.StreamWriter
	model               sigma.Model
	options             responsesStreamOptions
	final               sigma.AssistantMessage
	started             bool
	nextBlock           int
	text                map[int]*responsesTextState
	thinking            map[int]*responsesThinkingState
	images              map[int]*responsesImageState
	toolCalls           map[int]*streamblocks.ToolCall
	toolItemIDs         map[int]string
	responseID          string
	providerModel       string
	responseServiceTier string
	usage               *sigma.Usage
	stopReason          sigma.StopReason
}

type responsesStreamOptions struct {
	requestServiceTier          string
	applyServiceTierCosts       bool
	useCodexServiceTierFallback bool
}

type responsesTextState struct {
	streamblocks.Text
	itemID    string
	partID    string
	signature string
}

type responsesThinkingState struct {
	streamblocks.Thinking
	itemID string
}

type responsesImageState struct {
	ContentIndex int
	itemID       string
	status       string
	image        sigma.ContentBlock
	Started      bool
	Closed       bool
}

func parseResponsesStream(ctx context.Context, r io.Reader, writer sigma.StreamWriter, model sigma.Model, opts responsesStreamOptions) (sigma.AssistantMessage, error) {
	parser := newResponsesStreamParser(writer, model, opts)
	err := sse.Parse(ctx, r, func(event sse.Event) error {
		if event.Done {
			return sse.ErrStop
		}
		return parser.handleSSEEvent(ctx, event)
	})
	if errors.Is(err, sse.ErrStop) {
		return parser.finalize(ctx), nil
	}
	if err != nil {
		return parser.finalize(ctx), err
	}
	return parser.finalize(ctx), nil
}

func newResponsesStreamParser(writer sigma.StreamWriter, model sigma.Model, opts responsesStreamOptions) *responsesStreamParser {
	return &responsesStreamParser{
		writer:      writer,
		model:       model,
		options:     opts,
		text:        make(map[int]*responsesTextState),
		thinking:    make(map[int]*responsesThinkingState),
		images:      make(map[int]*responsesImageState),
		toolCalls:   make(map[int]*streamblocks.ToolCall),
		toolItemIDs: make(map[int]string),
		final: sigma.AssistantMessage{
			Model:    model.ID,
			Provider: model.Provider,
		},
	}
}

func (p *responsesStreamParser) handleSSEEvent(ctx context.Context, event sse.Event) error {
	completed, err := p.handleEventData(ctx, event.Event, event.Data)
	if err != nil {
		return err
	}
	if completed {
		return sse.ErrStop
	}
	return nil
}

func (p *responsesStreamParser) handleEventData(ctx context.Context, eventName string, data string) (bool, error) {
	var parsed responsesEvent
	data = providerText(data)
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		return false, fmt.Errorf("openai responses: decode stream event: %w", err)
	}
	if parsed.Type == "" {
		parsed.Type = eventName
	}
	p.captureEventMetadata(parsed)
	switch parsed.Type {
	case "response.created", "response.in_progress":
		p.captureResponse(parsed.Response)
		return false, p.emitStart(ctx)
	case "response.completed":
		p.captureResponse(parsed.Response)
		return true, p.emitStart(ctx)
	case "response.failed", "response.incomplete":
		p.captureResponse(parsed.Response)
		if parsed.Response.Error != nil {
			return false, openAIResponsesStreamProviderError(p.model, parsed.Response.Error)
		}
		return false, fmt.Errorf("openai responses: stream ended with status %q", parsed.Response.Status)
	case "error":
		if parsed.Error != nil {
			return false, openAIResponsesStreamProviderError(p.model, parsed.Error)
		}
		return false, fmt.Errorf("openai responses: stream error")
	case "response.output_item.added":
		if err := p.emitStart(ctx); err != nil {
			return false, err
		}
		return false, p.handleOutputItemAdded(ctx, parsed)
	case "response.output_item.done":
		p.captureOutputItem(parsed.OutputIndex, parsed.Item)
		return false, nil
	case "response.output_text.delta":
		if err := p.emitStart(ctx); err != nil {
			return false, err
		}
		return false, p.emitText(ctx, parsed.OutputIndex, parsed.ItemID, "", parsed.Delta)
	case "response.refusal.delta":
		if err := p.emitStart(ctx); err != nil {
			return false, err
		}
		return false, p.emitText(ctx, parsed.OutputIndex, parsed.ItemID, "", parsed.Delta)
	case "response.output_text.done":
		p.finishText(parsed.OutputIndex, parsed.ItemID, parsed.Text)
		return false, nil
	case "response.content_part.added":
		if parsed.Part.Type == "output_text" || parsed.Part.Type == "refusal" {
			p.finishText(parsed.OutputIndex, parsed.ItemID, firstNonEmpty(parsed.Part.Text, parsed.Part.Refusal))
		}
		return false, nil
	case "response.reasoning_summary_text.delta":
		if err := p.emitStart(ctx); err != nil {
			return false, err
		}
		return false, p.emitThinking(ctx, parsed.OutputIndex, parsed.ItemID, parsed.Delta)
	case "response.reasoning_text.delta":
		if err := p.emitStart(ctx); err != nil {
			return false, err
		}
		return false, p.emitThinking(ctx, parsed.OutputIndex, parsed.ItemID, parsed.Delta)
	case "response.reasoning_summary_text.done":
		p.finishThinking(parsed.OutputIndex, parsed.ItemID, parsed.Text)
		return false, nil
	case "response.function_call_arguments.delta":
		if err := p.emitStart(ctx); err != nil {
			return false, err
		}
		return false, p.emitToolCall(ctx, parsed.OutputIndex, streamToolCallDelta{
			Function: streamFunctionDelta{
				Arguments: parsed.Delta,
			},
		})
	case "response.function_call_arguments.done":
		state := p.toolCallState(parsed.OutputIndex)
		if parsed.Arguments != "" {
			state.SetArguments(parsed.Arguments)
		}
		return false, nil
	case "response.image_generation_call.partial_image":
		if err := p.emitStart(ctx); err != nil {
			return false, err
		}
		return false, p.emitImage(ctx, parsed.OutputIndex, parsed.ItemID, firstNonEmpty(parsed.PartialImage, parsed.B64JSON, parsed.Delta, parsed.ImageURL), true)
	default:
		// The Responses API emits lifecycle, content-part, refusal, and tool
		// progress events that carry no additional public sigma content.
		return false, nil
	}
}

func openAIResponsesStreamProviderError(model sigma.Model, err *responsesError) *sigma.ProviderError {
	body, _ := json.Marshal(map[string]any{"error": err})
	cause := sigma.ErrProviderResponse
	if contextOverflowCause(body) != nil {
		cause = sigma.ErrContextOverflow
	}
	api := model.API
	if api == "" {
		api = sigma.APIOpenAIResponses
	}
	return sigma.NewProviderError(
		model.Provider,
		api,
		model.ID,
		0,
		"",
		0,
		body,
		cause,
	)
}

func (p *responsesStreamParser) captureEventMetadata(event responsesEvent) {
	if event.ResponseID != "" {
		p.responseID = event.ResponseID
	}
	if event.Model != "" {
		p.providerModel = event.Model
	}
}

func (p *responsesStreamParser) captureResponse(response responsesResponse) {
	if response.ID != "" {
		p.responseID = response.ID
	}
	if response.Model != "" {
		p.providerModel = response.Model
	}
	if response.ServiceTier != "" {
		p.responseServiceTier = response.ServiceTier
	}
	if response.Usage != nil {
		usage := response.Usage.sigmaUsage()
		usage, _ = sigma.AccountUsage(p.model, usage, sigma.WithRawUsage(*response.Usage))
		p.usage = &usage
	}
	if response.IncompleteDetails != nil && response.IncompleteDetails.Reason != "" {
		p.stopReason = responsesStopReason(response.IncompleteDetails.Reason)
	}
	for index, item := range response.Output {
		p.captureOutputItem(index, item)
	}
}

func (p *responsesStreamParser) captureOutputItem(outputIndex int, item responsesOutputItem) {
	switch item.Type {
	case "message":
		for _, part := range item.Content {
			if part.Type != "output_text" && part.Type != "refusal" {
				continue
			}
			state := p.textState(outputIndex)
			state.itemID = firstNonEmpty(state.itemID, item.ID)
			state.partID = firstNonEmpty(state.partID, part.ID)
			state.signature = firstNonEmpty(state.signature, part.Signature)
			if text := firstNonEmpty(part.Text, part.Refusal); text != "" {
				state.Set(providerText(text))
			}
		}
	case "reasoning":
		state := p.thinkingState(outputIndex)
		state.itemID = firstNonEmpty(state.itemID, item.ID)
		state.Signature = firstNonEmpty(state.Signature, item.Signature)
		state.ProviderSignature = firstNonEmpty(state.ProviderSignature, item.EncryptedContent)
		var summary string
		for _, part := range item.Summary {
			if part.Text != "" {
				summary += part.Text
			}
			if part.Signature != "" {
				state.Signature = part.Signature
			}
		}
		if summary != "" {
			state.Set(providerText(summary))
		}
	case "function_call":
		state := p.toolCallState(outputIndex)
		if item.ID != "" {
			p.toolItemIDs[outputIndex] = item.ID
		}
		state.SetID(firstNonEmpty(state.ID(), item.CallID))
		if item.ID != "" {
			if state.ID() == "" {
				state.SetID(item.ID)
			}
		}
		state.SetName(firstNonEmpty(state.Name(), item.Name))
		if item.Arguments != "" {
			state.SetArguments(item.Arguments)
		}
	case "image_generation_call":
		p.finishImage(outputIndex, item.ID, item.Status, firstNonEmpty(item.Result, item.B64JSON, item.ImageURL))
	}
}

func (p *responsesStreamParser) handleOutputItemAdded(ctx context.Context, event responsesEvent) error {
	if event.Item.Type == "image_generation_call" {
		p.captureOutputItem(event.OutputIndex, event.Item)
		return p.emitImage(ctx, event.OutputIndex, event.Item.ID, firstNonEmpty(event.Item.Result, event.Item.B64JSON, event.Item.ImageURL), true)
	}
	if event.Item.Type != "function_call" {
		p.captureOutputItem(event.OutputIndex, event.Item)
		return nil
	}
	if event.Item.ID != "" {
		p.toolItemIDs[event.OutputIndex] = event.Item.ID
	}
	delta := streamToolCallDelta{
		ID: event.Item.CallID,
		Function: streamFunctionDelta{
			Name:      event.Item.Name,
			Arguments: event.Item.Arguments,
		},
	}
	if delta.ID == "" {
		delta.ID = event.Item.ID
	}
	return p.emitToolCall(ctx, event.OutputIndex, delta)
}

func (p *responsesStreamParser) emitStart(ctx context.Context) error {
	if p.started {
		return nil
	}
	p.started = true
	return p.writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindStart})
}

func (p *responsesStreamParser) emitText(ctx context.Context, outputIndex int, itemID string, partID string, delta string) error {
	delta = providerText(delta)
	state := p.textState(outputIndex)
	state.itemID = firstNonEmpty(state.itemID, itemID)
	state.partID = firstNonEmpty(state.partID, partID)
	if !state.Started {
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindTextStart,
			ContentIndex: intPtr(state.ContentIndex),
		}); err != nil {
			return err
		}
		state.Started = true
	}
	if delta == "" {
		return nil
	}
	text := state.Append(delta)
	return p.writer.Emit(ctx, sigma.Event{
		Kind:         sigma.EventKindTextDelta,
		ContentIndex: intPtr(state.ContentIndex),
		DeltaText:    delta,
		Text:         text,
	})
}

func (p *responsesStreamParser) emitThinking(ctx context.Context, outputIndex int, itemID string, delta string) error {
	delta = providerText(delta)
	state := p.thinkingState(outputIndex)
	state.itemID = firstNonEmpty(state.itemID, itemID)
	if !state.Started {
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindThinkingStart,
			ContentIndex: intPtr(state.ContentIndex),
		}); err != nil {
			return err
		}
		state.Started = true
	}
	if delta == "" {
		return nil
	}
	thinking := state.Append(delta)
	return p.writer.Emit(ctx, sigma.Event{
		Kind:         sigma.EventKindThinkingDelta,
		ContentIndex: intPtr(state.ContentIndex),
		DeltaText:    delta,
		Thinking:     thinking,
	})
}

func (p *responsesStreamParser) emitImage(ctx context.Context, outputIndex int, itemID string, value string, partial bool) error {
	state := p.imageState(outputIndex)
	state.itemID = firstNonEmpty(state.itemID, itemID)
	if !state.Started {
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindImageStart,
			ContentIndex: intPtr(state.ContentIndex),
		}); err != nil {
			return err
		}
		state.Started = true
	}
	if value == "" {
		return nil
	}
	image := responseImageBlock(value)
	image.ProviderMetadata = responsesMetadata(state.itemID, "", "")
	state.image = image
	event := sigma.Event{
		Kind:         sigma.EventKindImageDelta,
		ContentIndex: intPtr(state.ContentIndex),
		PartialImage: &image,
	}
	if !partial {
		event.Kind = sigma.EventKindImageEnd
		event.Image = &image
		state.Closed = true
	}
	return p.writer.Emit(ctx, event)
}

func (p *responsesStreamParser) emitToolCall(ctx context.Context, outputIndex int, delta streamToolCallDelta) error {
	state := p.toolCallState(outputIndex)
	state.SetID(delta.ID)
	state.SetName(delta.Function.Name)
	state.AppendArguments(delta.Function.Arguments)
	partial := state.Partial(delta.Function.Arguments, streamblocks.ToolPartialArgumentsText)
	if !state.Started {
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:            sigma.EventKindToolCallStart,
			ContentIndex:    intPtr(state.ContentIndex),
			PartialToolCall: partial,
		}); err != nil {
			return err
		}
		state.Started = true
	}
	return p.writer.Emit(ctx, sigma.Event{
		Kind:            sigma.EventKindToolCallDelta,
		ContentIndex:    intPtr(state.ContentIndex),
		PartialToolCall: partial,
	})
}

func (p *responsesStreamParser) finishText(outputIndex int, itemID string, text string) {
	state := p.textState(outputIndex)
	state.itemID = firstNonEmpty(state.itemID, itemID)
	if text != "" {
		state.Set(providerText(text))
	}
}

func (p *responsesStreamParser) finishThinking(outputIndex int, itemID string, text string) {
	state := p.thinkingState(outputIndex)
	state.itemID = firstNonEmpty(state.itemID, itemID)
	if text != "" {
		state.Set(providerText(text))
	}
}

func (p *responsesStreamParser) finishImage(outputIndex int, itemID string, status string, value string) {
	state := p.imageState(outputIndex)
	state.itemID = firstNonEmpty(state.itemID, itemID)
	state.status = firstNonEmpty(state.status, status)
	if value != "" {
		image := responseImageBlock(value)
		image.ProviderMetadata = responsesMetadata(state.itemID, "", "")
		if state.status != "" {
			if image.ProviderMetadata == nil {
				image.ProviderMetadata = make(map[string]any)
			}
			image.ProviderMetadata["status"] = state.status
		}
		state.image = image
	}
}

func (p *responsesStreamParser) finalize(ctx context.Context) sigma.AssistantMessage {
	contentByIndex := make(map[int]sigma.ContentBlock)
	for _, state := range p.sortedText() {
		block := sigma.Text(state.String())
		block.Signature = state.signature
		block.ProviderMetadata = responsesMetadata(state.itemID, state.partID, "")
		contentByIndex[state.ContentIndex] = block
	}
	for _, state := range p.sortedThinking() {
		block := sigma.Thinking(state.String(), state.Signature)
		block.ProviderSignature = state.ProviderSignature
		block.ProviderMetadata = responsesMetadata(state.itemID, "", "")
		contentByIndex[state.ContentIndex] = block
	}
	for _, state := range p.sortedImages() {
		block := state.image
		if block.Type == "" {
			block = sigma.ContentBlock{Type: sigma.ContentBlockImage}
		}
		if block.ProviderMetadata == nil {
			block.ProviderMetadata = responsesMetadata(state.itemID, "", "")
		}
		if state.status != "" {
			if block.ProviderMetadata == nil {
				block.ProviderMetadata = make(map[string]any)
			}
			block.ProviderMetadata["status"] = state.status
		}
		contentByIndex[state.ContentIndex] = block
	}
	for _, state := range p.sortedToolCalls() {
		call := state.ToolCall()
		block := sigma.ToolCallBlock(call.ID, call.Name, call.Arguments)
		block.ProviderMetadata = responsesMetadata(p.toolItemID(state), "", state.ID())
		contentByIndex[state.ContentIndex] = block
	}
	p.emitEndEvents(ctx)

	if len(contentByIndex) > 0 {
		indexes := make([]int, 0, len(contentByIndex))
		for index := range contentByIndex {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		p.final.Content = make([]sigma.ContentBlock, 0, len(indexes))
		for _, index := range indexes {
			p.final.Content = append(p.final.Content, contentByIndex[index])
		}
	}
	if p.stopReason != "" {
		p.final.StopReason = p.stopReason
	} else if len(p.toolCalls) > 0 {
		p.final.StopReason = sigma.StopReasonToolCalls
	} else {
		p.final.StopReason = sigma.StopReasonEndTurn
	}
	if p.usage != nil {
		opts := []sigma.UsageAccountingOption{}
		if p.options.applyServiceTierCosts {
			opts = append(opts, sigma.WithEstimatedCostAdjustment(func(cost *sigma.Cost) {
				applyResponsesServiceTierCost(cost, p.model, p.resolvedServiceTier())
			}))
		}
		usage, cost := sigma.AccountUsage(p.model, *p.usage, opts...)
		p.final.Usage = &usage
		p.final.Cost = &cost
	}
	p.final.ProviderMetadata = responseMetadata(p.responseID, p.providerModel, p.model.ID)
	return p.final
}

func (p *responsesStreamParser) emitEndEvents(ctx context.Context) {
	events := make([]struct {
		index int
		emit  func()
	}, 0, len(p.text)+len(p.thinking)+len(p.images)+len(p.toolCalls))
	for _, state := range p.text {
		state := state
		if state.Started && !state.Closed {
			events = append(events, struct {
				index int
				emit  func()
			}{index: state.ContentIndex, emit: func() {
				_ = p.writer.Emit(ctx, sigma.Event{
					Kind:         sigma.EventKindTextEnd,
					ContentIndex: intPtr(state.ContentIndex),
					Text:         state.String(),
				})
				state.Closed = true
			}})
		}
	}
	for _, state := range p.thinking {
		state := state
		if state.Started && !state.Closed {
			events = append(events, struct {
				index int
				emit  func()
			}{index: state.ContentIndex, emit: func() {
				_ = p.writer.Emit(ctx, sigma.Event{
					Kind:         sigma.EventKindThinkingEnd,
					ContentIndex: intPtr(state.ContentIndex),
					Thinking:     state.String(),
				})
				state.Closed = true
			}})
		}
	}
	for _, state := range p.toolCalls {
		state := state
		if state.Started && !state.Closed {
			events = append(events, struct {
				index int
				emit  func()
			}{index: state.ContentIndex, emit: func() {
				call := state.ToolCall()
				_ = p.writer.Emit(ctx, sigma.Event{
					Kind:         sigma.EventKindToolCallEnd,
					ContentIndex: intPtr(state.ContentIndex),
					ToolCall:     &call,
				})
				state.Closed = true
			}})
		}
	}
	for _, state := range p.images {
		state := state
		if state.Started && !state.Closed {
			events = append(events, struct {
				index int
				emit  func()
			}{index: state.ContentIndex, emit: func() {
				image := state.image
				if image.Type == "" {
					image = sigma.ContentBlock{Type: sigma.ContentBlockImage}
				}
				_ = p.writer.Emit(ctx, sigma.Event{
					Kind:         sigma.EventKindImageEnd,
					ContentIndex: intPtr(state.ContentIndex),
					Image:        &image,
				})
				state.Closed = true
			}})
		}
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].index < events[j].index
	})
	for _, event := range events {
		event.emit()
	}
}

func (p *responsesStreamParser) textState(outputIndex int) *responsesTextState {
	state := p.text[outputIndex]
	if state == nil {
		state = &responsesTextState{Text: streamblocks.Text{ContentIndex: p.nextContentIndex()}}
		p.text[outputIndex] = state
	}
	return state
}

func (p *responsesStreamParser) thinkingState(outputIndex int) *responsesThinkingState {
	state := p.thinking[outputIndex]
	if state == nil {
		state = &responsesThinkingState{Thinking: streamblocks.Thinking{ContentIndex: p.nextContentIndex()}}
		p.thinking[outputIndex] = state
	}
	return state
}

func (p *responsesStreamParser) toolCallState(outputIndex int) *streamblocks.ToolCall {
	state := p.toolCalls[outputIndex]
	if state == nil {
		state = &streamblocks.ToolCall{ContentIndex: p.nextContentIndex()}
		p.toolCalls[outputIndex] = state
	}
	return state
}

func (p *responsesStreamParser) imageState(outputIndex int) *responsesImageState {
	state := p.images[outputIndex]
	if state == nil {
		state = &responsesImageState{ContentIndex: p.nextContentIndex()}
		p.images[outputIndex] = state
	}
	return state
}

func (p *responsesStreamParser) nextContentIndex() int {
	index := p.nextBlock
	p.nextBlock++
	return index
}

func (p *responsesStreamParser) sortedText() []*responsesTextState {
	states := make([]*responsesTextState, 0, len(p.text))
	for _, state := range p.text {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (p *responsesStreamParser) sortedThinking() []*responsesThinkingState {
	states := make([]*responsesThinkingState, 0, len(p.thinking))
	for _, state := range p.thinking {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (p *responsesStreamParser) sortedToolCalls() []*streamblocks.ToolCall {
	states := make([]*streamblocks.ToolCall, 0, len(p.toolCalls))
	for _, state := range p.toolCalls {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (p *responsesStreamParser) sortedImages() []*responsesImageState {
	states := make([]*responsesImageState, 0, len(p.images))
	for _, state := range p.images {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func responseImageBlock(value string) sigma.ContentBlock {
	if before, after, ok := strings.Cut(value, ","); ok && strings.Contains(before, "base64") {
		mimeType := strings.TrimPrefix(strings.TrimSuffix(before, ";base64"), "data:")
		return sigma.ImageBase64(mimeType, after)
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return sigma.ImageURL("", value)
	}
	return sigma.ImageBase64("image/png", value)
}

func (p *responsesStreamParser) toolItemID(state *streamblocks.ToolCall) string {
	for outputIndex, candidate := range p.toolCalls {
		if candidate == state {
			return p.toolItemIDs[outputIndex]
		}
	}
	return ""
}

func responseMetadata(responseID string, providerModel string, modelID sigma.ModelID) map[string]any {
	metadata := make(map[string]any)
	if responseID != "" {
		metadata["id"] = responseID
	}
	if providerModel != "" && providerModel != string(modelID) {
		metadata["model"] = providerModel
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func openAIResponsesStreamOptions(opts sigma.Options) responsesStreamOptions {
	return responsesStreamOptions{
		requestServiceTier:    openAIRequestServiceTier(opts),
		applyServiceTierCosts: true,
	}
}

func codexResponsesStreamOptions(opts sigma.Options) responsesStreamOptions {
	return responsesStreamOptions{
		requestServiceTier:          openAIRequestServiceTier(opts),
		applyServiceTierCosts:       true,
		useCodexServiceTierFallback: true,
	}
}

func openAIRequestServiceTier(opts sigma.Options) string {
	if opts.OpenAIOptions == nil {
		return ""
	}
	return opts.OpenAIOptions.ServiceTier
}

func (p *responsesStreamParser) resolvedServiceTier() string {
	if p.options.useCodexServiceTierFallback && p.responseServiceTier == "default" {
		switch p.options.requestServiceTier {
		case "flex", "priority":
			return p.options.requestServiceTier
		}
	}
	if p.responseServiceTier != "" {
		return p.responseServiceTier
	}
	return p.options.requestServiceTier
}

func applyResponsesServiceTierCost(cost *sigma.Cost, model sigma.Model, serviceTier string) {
	multiplier := responsesServiceTierCostMultiplier(model, serviceTier)
	if multiplier == 1 {
		return
	}
	cost.InputCost *= multiplier
	cost.OutputCost *= multiplier
	cost.CacheReadInputCost *= multiplier
	cost.CacheWriteInputCost *= multiplier
	cost.TotalCost = cost.InputCost + cost.OutputCost + cost.CacheReadInputCost + cost.CacheWriteInputCost
}

func responsesServiceTierCostMultiplier(model sigma.Model, serviceTier string) float64 {
	switch serviceTier {
	case "flex":
		return 0.5
	case "priority":
		if openAIServiceTierModelID(model) == "gpt-5.5" {
			return 2.5
		}
		return 2
	default:
		return 1
	}
}

func openAIServiceTierModelID(model sigma.Model) string {
	if model.OpenAICodexResponses != nil && model.OpenAICodexResponses.Model != "" {
		return model.OpenAICodexResponses.Model
	}
	return string(model.ID)
}

func responsesMetadata(itemID string, partID string, callID string) map[string]any {
	metadata := make(map[string]any)
	if itemID != "" {
		metadata["id"] = itemID
	}
	if partID != "" {
		metadata["content_id"] = partID
	}
	if callID != "" {
		metadata["call_id"] = callID
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func (u responsesUsage) sigmaUsage() sigma.Usage {
	usage := sigma.Usage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  u.TotalTokens,
	}
	if u.InputTokensDetails != nil {
		usage.CacheReadInputTokens = u.InputTokensDetails.CachedTokens
		usage.InputTokens = max(0, u.InputTokens-usage.CacheReadInputTokens)
	}
	if u.OutputTokensDetails != nil {
		usage.ThinkingTokens = u.OutputTokensDetails.ReasoningTokens
	}
	return usage
}

func responsesStopReason(reason string) sigma.StopReason {
	switch reason {
	case "max_output_tokens":
		return sigma.StopReasonMaxTokens
	case "content_filter":
		return sigma.StopReasonContentFilter
	default:
		return sigma.StopReasonUnknown
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
