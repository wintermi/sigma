// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"context"
	"fmt"
	"sort"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/streamblocks"
)

// ConverseEventKind identifies provider-owned Bedrock stream events.
type ConverseEventKind string

const (
	ConverseEventMessageStart      ConverseEventKind = "message_start"
	ConverseEventContentBlockStart ConverseEventKind = "content_block_start"
	ConverseEventContentBlockDelta ConverseEventKind = "content_block_delta"
	ConverseEventContentBlockStop  ConverseEventKind = "content_block_stop"
	ConverseEventMessageStop       ConverseEventKind = "message_stop"
	ConverseEventMetadata          ConverseEventKind = "metadata"
	ConverseEventError             ConverseEventKind = "error"
)

// ConverseStream is the fakeable Bedrock stream seam.
type ConverseStream interface {
	Events() <-chan ConverseEvent
	Close() error
	Err() error
}

// ConverseEvent is a provider-owned Bedrock stream event without AWS SDK types.
type ConverseEvent struct {
	Kind              ConverseEventKind
	ContentBlockIndex int
	Role              string
	TextDelta         string
	ThinkingDelta     string
	ThinkingSignature string
	RedactedThinking  string
	ToolUseID         string
	ToolName          string
	ToolInputDelta    string
	StopReason        string
	Usage             *ConverseUsage
	Err               error
}

// ConverseUsage carries Bedrock token accounting.
type ConverseUsage struct {
	InputTokens           int
	OutputTokens          int
	TotalTokens           int
	CacheReadInputTokens  int
	CacheWriteInputTokens int
	Raw                   map[string]any
}

type converseStreamParser struct {
	writer              sigma.StreamWriter
	model               sigma.Model
	final               sigma.AssistantMessage
	started             bool
	nextBlock           int
	text                map[int]*streamblocks.Text
	thinking            map[int]*streamblocks.Thinking
	toolCalls           map[int]*streamblocks.ToolCall
	responseFormatTools map[int]struct{}
	usage               *sigma.Usage
	stop                sigma.StopReason
}

func parseConverseStream(ctx context.Context, stream ConverseStream, writer sigma.StreamWriter, model sigma.Model, responseFormat bool) (sigma.AssistantMessage, error) {
	parser := converseStreamParser{
		writer:              writer,
		model:               model,
		text:                make(map[int]*streamblocks.Text),
		thinking:            make(map[int]*streamblocks.Thinking),
		toolCalls:           make(map[int]*streamblocks.ToolCall),
		responseFormatTools: make(map[int]struct{}),
		final: sigma.AssistantMessage{
			Model:    model.ID,
			Provider: model.Provider,
		},
	}
	if !responseFormat {
		parser.responseFormatTools = nil
	}
	for {
		select {
		case <-ctx.Done():
			return parser.finalize(ctx), ctx.Err()
		case event, ok := <-stream.Events():
			if !ok {
				if err := stream.Err(); err != nil {
					return parser.finalize(ctx), err
				}
				return parser.finalize(ctx), nil
			}
			if err := parser.handleEvent(ctx, event); err != nil {
				return parser.finalize(ctx), err
			}
		}
	}
}

func (p *converseStreamParser) handleEvent(ctx context.Context, event ConverseEvent) error {
	switch event.Kind {
	case ConverseEventMessageStart:
		return p.emitStart(ctx)
	case ConverseEventContentBlockStart:
		if err := p.emitStart(ctx); err != nil {
			return err
		}
		if event.ToolUseID != "" || event.ToolName != "" {
			if p.markResponseFormatTool(event.ContentBlockIndex, event.ToolName) {
				return nil
			}
			return p.emitToolCall(ctx, event.ContentBlockIndex, event.ToolInputDelta, event.ToolUseID, event.ToolName)
		}
		return nil
	case ConverseEventContentBlockDelta:
		if err := p.emitStart(ctx); err != nil {
			return err
		}
		if event.TextDelta != "" {
			return p.emitText(ctx, event.ContentBlockIndex, event.TextDelta)
		}
		if event.ThinkingDelta != "" || event.ThinkingSignature != "" || event.RedactedThinking != "" {
			return p.emitThinking(ctx, event.ContentBlockIndex, event)
		}
		if event.ToolInputDelta != "" {
			if p.isResponseFormatTool(event.ContentBlockIndex) {
				return p.emitText(ctx, event.ContentBlockIndex, event.ToolInputDelta)
			}
			return p.emitToolCall(ctx, event.ContentBlockIndex, event.ToolInputDelta, event.ToolUseID, event.ToolName)
		}
		return nil
	case ConverseEventContentBlockStop:
		return nil
	case ConverseEventMessageStop:
		p.stop = bedrockStopReason(event.StopReason)
		if p.stop == sigma.StopReasonUnknown {
			return providerError(p.model, fmt.Errorf("bedrock converse stream: unhandled stop reason %q", event.StopReason))
		}
		return nil
	case ConverseEventMetadata:
		if event.Usage != nil {
			usage := event.Usage.sigmaUsage()
			usage, _ = sigma.AccountUsage(p.model, usage, sigma.WithRawUsage(event.Usage.Raw))
			p.usage = &usage
		}
		return nil
	case ConverseEventError:
		if event.Err != nil {
			return providerError(p.model, event.Err)
		}
		return providerError(p.model, fmt.Errorf("bedrock converse stream: stream error"))
	default:
		return nil
	}
}

func (p *converseStreamParser) emitStart(ctx context.Context) error {
	if p.started {
		return nil
	}
	p.started = true
	return p.writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindStart})
}

func (p *converseStreamParser) emitText(ctx context.Context, index int, delta string) error {
	state := p.textState(index)
	if !state.Started {
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindTextStart,
			ContentIndex: intPtr(state.ContentIndex),
		}); err != nil {
			return err
		}
		state.Started = true
	}
	text := state.Append(delta)
	return p.writer.Emit(ctx, sigma.Event{
		Kind:         sigma.EventKindTextDelta,
		ContentIndex: intPtr(state.ContentIndex),
		DeltaText:    delta,
		Text:         text,
	})
}

func (p *converseStreamParser) emitThinking(ctx context.Context, index int, event ConverseEvent) error {
	state := p.thinkingState(index)
	if event.ThinkingSignature != "" {
		state.Signature += event.ThinkingSignature
	}
	if event.RedactedThinking != "" {
		state.ProviderSignature = event.RedactedThinking
		state.Redacted = true
		return nil
	}
	if !state.Started {
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindThinkingStart,
			ContentIndex: intPtr(state.ContentIndex),
		}); err != nil {
			return err
		}
		state.Started = true
	}
	if event.ThinkingDelta == "" {
		return nil
	}
	thinking := state.Append(event.ThinkingDelta)
	return p.writer.Emit(ctx, sigma.Event{
		Kind:         sigma.EventKindThinkingDelta,
		ContentIndex: intPtr(state.ContentIndex),
		DeltaText:    event.ThinkingDelta,
		Thinking:     thinking,
	})
}

func (p *converseStreamParser) emitToolCall(ctx context.Context, index int, delta string, id string, name string) error {
	state := p.toolCallState(index)
	state.SetID(id)
	state.SetName(name)
	state.AppendArguments(delta)
	partial := state.Partial(delta, streamblocks.ToolPartialArgumentsText)
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
	if delta == "" {
		return nil
	}
	return p.writer.Emit(ctx, sigma.Event{
		Kind:            sigma.EventKindToolCallDelta,
		ContentIndex:    intPtr(state.ContentIndex),
		PartialToolCall: partial,
	})
}

func (p *converseStreamParser) markResponseFormatTool(index int, name string) bool {
	if p.responseFormatTools == nil || name != bedrockResponseFormatToolName {
		return false
	}
	p.responseFormatTools[index] = struct{}{}
	return true
}

func (p *converseStreamParser) isResponseFormatTool(index int) bool {
	if p.responseFormatTools == nil {
		return false
	}
	_, ok := p.responseFormatTools[index]
	return ok
}

func (p *converseStreamParser) finalize(ctx context.Context) sigma.AssistantMessage {
	contentByIndex := make(map[int]sigma.ContentBlock)
	for _, state := range p.sortedText() {
		contentByIndex[state.ContentIndex] = sigma.Text(state.String())
		if state.Started && !state.Closed {
			_ = p.writer.Emit(ctx, sigma.Event{
				Kind:         sigma.EventKindTextEnd,
				ContentIndex: intPtr(state.ContentIndex),
				Text:         state.String(),
			})
			state.Closed = true
		}
	}
	for _, state := range p.sortedThinking() {
		block := sigma.Thinking(state.String(), state.Signature)
		block.Redacted = state.Redacted
		block.ProviderSignature = state.ProviderSignature
		contentByIndex[state.ContentIndex] = block
		if state.Started && !state.Closed {
			_ = p.writer.Emit(ctx, sigma.Event{
				Kind:         sigma.EventKindThinkingEnd,
				ContentIndex: intPtr(state.ContentIndex),
				Thinking:     state.String(),
			})
			state.Closed = true
		}
	}
	for _, state := range p.sortedToolCalls() {
		call := state.ToolCall()
		contentByIndex[state.ContentIndex] = sigma.ToolCallBlock(call.ID, call.Name, call.Arguments)
		if state.Started && !state.Closed {
			_ = p.writer.Emit(ctx, sigma.Event{
				Kind:         sigma.EventKindToolCallEnd,
				ContentIndex: intPtr(state.ContentIndex),
				ToolCall:     &call,
			})
			state.Closed = true
		}
	}

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
	if p.stop == sigma.StopReasonToolCalls && len(p.toolCalls) == 0 {
		p.final.StopReason = sigma.StopReasonEndTurn
	} else if p.stop != "" {
		p.final.StopReason = p.stop
	} else if len(p.toolCalls) > 0 {
		p.final.StopReason = sigma.StopReasonToolCalls
	} else {
		p.final.StopReason = sigma.StopReasonEndTurn
	}
	if p.usage != nil {
		usage, cost := sigma.AccountUsage(p.model, *p.usage)
		p.final.Usage = &usage
		p.final.Cost = &cost
	}
	return p.final
}

func (p *converseStreamParser) textState(index int) *streamblocks.Text {
	state := p.text[index]
	if state == nil {
		state = &streamblocks.Text{ContentIndex: p.contentIndex(index)}
		p.text[index] = state
	}
	return state
}

func (p *converseStreamParser) thinkingState(index int) *streamblocks.Thinking {
	state := p.thinking[index]
	if state == nil {
		state = &streamblocks.Thinking{ContentIndex: p.contentIndex(index)}
		p.thinking[index] = state
	}
	return state
}

func (p *converseStreamParser) toolCallState(index int) *streamblocks.ToolCall {
	state := p.toolCalls[index]
	if state == nil {
		state = &streamblocks.ToolCall{ContentIndex: p.contentIndex(index)}
		p.toolCalls[index] = state
	}
	return state
}

func (p *converseStreamParser) contentIndex(index int) int {
	for _, state := range p.text {
		if state.ContentIndex == index {
			return state.ContentIndex
		}
	}
	if state := p.text[index]; state != nil {
		return state.ContentIndex
	}
	if state := p.thinking[index]; state != nil {
		return state.ContentIndex
	}
	if state := p.toolCalls[index]; state != nil {
		return state.ContentIndex
	}
	if index >= 0 {
		return index
	}
	value := p.nextBlock
	p.nextBlock++
	return value
}

func (p *converseStreamParser) sortedText() []*streamblocks.Text {
	states := make([]*streamblocks.Text, 0, len(p.text))
	for _, state := range p.text {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (p *converseStreamParser) sortedThinking() []*streamblocks.Thinking {
	states := make([]*streamblocks.Thinking, 0, len(p.thinking))
	for _, state := range p.thinking {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (p *converseStreamParser) sortedToolCalls() []*streamblocks.ToolCall {
	states := make([]*streamblocks.ToolCall, 0, len(p.toolCalls))
	for _, state := range p.toolCalls {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (u ConverseUsage) sigmaUsage() sigma.Usage {
	return sigma.Usage{
		InputTokens:           u.InputTokens,
		OutputTokens:          u.OutputTokens,
		TotalTokens:           u.TotalTokens,
		CacheReadInputTokens:  u.CacheReadInputTokens,
		CacheWriteInputTokens: u.CacheWriteInputTokens,
	}
}

func bedrockStopReason(reason string) sigma.StopReason {
	switch reason {
	case "end_turn":
		return sigma.StopReasonEndTurn
	case "max_tokens":
		return sigma.StopReasonMaxTokens
	case "stop_sequence":
		return sigma.StopReasonStopSequence
	case "tool_use":
		return sigma.StopReasonToolCalls
	case "guardrail_intervened", "content_filtered":
		return sigma.StopReasonContentFilter
	default:
		if reason == "" {
			return ""
		}
		return sigma.StopReasonUnknown
	}
}

func intPtr(value int) *int {
	return &value
}
