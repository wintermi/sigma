// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

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
	writer        sigma.StreamWriter
	model         sigma.Model
	final         sigma.AssistantMessage
	started       bool
	nextBlock     int
	text          map[int]*responsesTextState
	thinking      map[int]*responsesThinkingState
	toolCalls     map[int]*streamblocks.ToolCall
	toolItemIDs   map[int]string
	responseID    string
	providerModel string
	usage         *sigma.Usage
	stopReason    sigma.StopReason
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

func parseResponsesStream(ctx context.Context, r io.Reader, writer sigma.StreamWriter, model sigma.Model) (sigma.AssistantMessage, error) {
	parser := responsesStreamParser{
		writer:      writer,
		model:       model,
		text:        make(map[int]*responsesTextState),
		thinking:    make(map[int]*responsesThinkingState),
		toolCalls:   make(map[int]*streamblocks.ToolCall),
		toolItemIDs: make(map[int]string),
		final: sigma.AssistantMessage{
			Model:    model.ID,
			Provider: model.Provider,
		},
	}
	err := sse.Parse(ctx, r, func(event sse.Event) error {
		if event.Done {
			return sse.ErrStop
		}
		return parser.handleEvent(ctx, event)
	})
	if err != nil {
		return parser.finalize(ctx), err
	}
	return parser.finalize(ctx), nil
}

func (p *responsesStreamParser) handleEvent(ctx context.Context, event sse.Event) error {
	var parsed responsesEvent
	if err := json.Unmarshal([]byte(event.Data), &parsed); err != nil {
		return fmt.Errorf("openai responses: decode stream event: %w", err)
	}
	if parsed.Type == "" {
		parsed.Type = event.Event
	}
	p.captureEventMetadata(parsed)
	switch parsed.Type {
	case "response.created", "response.in_progress":
		p.captureResponse(parsed.Response)
		return p.emitStart(ctx)
	case "response.completed":
		p.captureResponse(parsed.Response)
		return p.emitStart(ctx)
	case "response.failed", "response.incomplete":
		p.captureResponse(parsed.Response)
		if parsed.Response.Error != nil {
			return fmt.Errorf("openai responses: stream error: %s", parsed.Response.Error.Message)
		}
		return fmt.Errorf("openai responses: stream ended with status %q", parsed.Response.Status)
	case "error":
		if parsed.Error != nil {
			return fmt.Errorf("openai responses: stream error: %s", parsed.Error.Message)
		}
		return fmt.Errorf("openai responses: stream error")
	case "response.output_item.added":
		if err := p.emitStart(ctx); err != nil {
			return err
		}
		return p.handleOutputItemAdded(ctx, parsed)
	case "response.output_item.done":
		p.captureOutputItem(parsed.OutputIndex, parsed.Item)
		return nil
	case "response.output_text.delta":
		if err := p.emitStart(ctx); err != nil {
			return err
		}
		return p.emitText(ctx, parsed.OutputIndex, parsed.ItemID, "", parsed.Delta)
	case "response.refusal.delta":
		if err := p.emitStart(ctx); err != nil {
			return err
		}
		return p.emitText(ctx, parsed.OutputIndex, parsed.ItemID, "", parsed.Delta)
	case "response.output_text.done":
		p.finishText(parsed.OutputIndex, parsed.ItemID, parsed.Text)
		return nil
	case "response.content_part.added":
		if parsed.Part.Type == "output_text" || parsed.Part.Type == "refusal" {
			p.finishText(parsed.OutputIndex, parsed.ItemID, firstNonEmpty(parsed.Part.Text, parsed.Part.Refusal))
		}
		return nil
	case "response.reasoning_summary_text.delta":
		if err := p.emitStart(ctx); err != nil {
			return err
		}
		return p.emitThinking(ctx, parsed.OutputIndex, parsed.ItemID, parsed.Delta)
	case "response.reasoning_text.delta":
		if err := p.emitStart(ctx); err != nil {
			return err
		}
		return p.emitThinking(ctx, parsed.OutputIndex, parsed.ItemID, parsed.Delta)
	case "response.reasoning_summary_text.done":
		p.finishThinking(parsed.OutputIndex, parsed.ItemID, parsed.Text)
		return nil
	case "response.function_call_arguments.delta":
		if err := p.emitStart(ctx); err != nil {
			return err
		}
		return p.emitToolCall(ctx, parsed.OutputIndex, streamToolCallDelta{
			Function: streamFunctionDelta{
				Arguments: parsed.Delta,
			},
		})
	case "response.function_call_arguments.done":
		state := p.toolCallState(parsed.OutputIndex)
		if parsed.Arguments != "" {
			state.SetArguments(parsed.Arguments)
		}
		return nil
	default:
		// The Responses API emits lifecycle, content-part, refusal, and tool
		// progress events that carry no additional public sigma content.
		return nil
	}
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
	if response.Usage != nil {
		usage := response.Usage.sigmaUsage()
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
				state.Set(text)
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
			state.Set(summary)
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
	}
}

func (p *responsesStreamParser) handleOutputItemAdded(ctx context.Context, event responsesEvent) error {
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
		state.Set(text)
	}
}

func (p *responsesStreamParser) finishThinking(outputIndex int, itemID string, text string) {
	state := p.thinkingState(outputIndex)
	state.itemID = firstNonEmpty(state.itemID, itemID)
	if text != "" {
		state.Set(text)
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
		usage := *p.usage
		p.final.Usage = &usage
		cost := sigma.CostForUsage(p.model, usage)
		p.final.Cost = &cost
	}
	p.final.ProviderMetadata = responseMetadata(p.responseID, p.providerModel, p.model.ID)
	return p.final
}

func (p *responsesStreamParser) emitEndEvents(ctx context.Context) {
	events := make([]struct {
		index int
		emit  func()
	}, 0, len(p.text)+len(p.thinking)+len(p.toolCalls))
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
