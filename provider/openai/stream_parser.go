// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamblocks"
)

type streamChunk struct {
	ID        string         `json:"id"`
	Model     string         `json:"model"`
	Choices   []streamChoice `json:"choices"`
	Usage     *openAIUsage   `json:"usage"`
	Error     *streamError   `json:"error"`
	Citations []string       `json:"citations"`
}

type streamChoice struct {
	Index        int             `json:"index"`
	Delta        streamDelta     `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs"`
}

type streamDelta struct {
	Role             string                `json:"role"`
	Content          json.RawMessage       `json:"content"`
	ReasoningContent *string               `json:"reasoning_content"`
	Reasoning        *string               `json:"reasoning"`
	Thinking         *string               `json:"thinking"`
	ToolCalls        []streamToolCallDelta `json:"tool_calls"`
	Annotations      []streamAnnotation    `json:"annotations"`
}

type streamToolCallDelta struct {
	Index    *int                `json:"index"`
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function streamFunctionDelta `json:"function"`
}

type streamFunctionDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type streamError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

type openAIUsage struct {
	PromptTokens            int                 `json:"prompt_tokens"`
	CompletionTokens        int                 `json:"completion_tokens"`
	TotalTokens             int                 `json:"total_tokens"`
	PromptTokensDetails     *promptTokenDetails `json:"prompt_tokens_details"`
	CompletionTokensDetails *outputTokenDetails `json:"completion_tokens_details"`
}

type promptTokenDetails struct {
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

type outputTokenDetails struct {
	ReasoningTokens          int `json:"reasoning_tokens"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
}

type streamAnnotation struct {
	Type        string             `json:"type"`
	URLCitation *streamURLCitation `json:"url_citation"`
}

type streamURLCitation struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	StartIndex int    `json:"start_index"`
	EndIndex   int    `json:"end_index"`
}

type completionStreamParser struct {
	writer        sigma.StreamWriter
	model         sigma.Model
	final         sigma.AssistantMessage
	started       bool
	text          *streamblocks.Text
	thinking      *streamblocks.Thinking
	toolCalls     map[int]*streamblocks.ToolCall
	nextBlock     int
	usage         *sigma.Usage
	finishReason  sigma.StopReason
	responseID    string
	providerModel string
	metadata      map[string]any
	sources       []map[string]any
}

func parseCompletionsStream(ctx context.Context, r io.Reader, writer sigma.StreamWriter, model sigma.Model) (sigma.AssistantMessage, error) {
	parser := completionStreamParser{
		writer:    writer,
		model:     model,
		toolCalls: make(map[int]*streamblocks.ToolCall),
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

func (p *completionStreamParser) handleEvent(ctx context.Context, event sse.Event) error {
	var chunk streamChunk
	if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
		return fmt.Errorf("openai completions: decode stream chunk: %w", err)
	}
	if chunk.Error != nil {
		return fmt.Errorf("openai completions: stream error: %s", chunk.Error.Message)
	}
	if chunk.Model != "" {
		p.providerModel = chunk.Model
	}
	if chunk.ID != "" {
		p.responseID = chunk.ID
	}
	for _, citation := range chunk.Citations {
		p.addSource(map[string]any{
			providerToolOptionTypeKey: "url",
			"url":                     citation,
			"id":                      fmt.Sprintf("citation_%d", len(p.sources)),
		})
	}
	if chunk.Usage != nil {
		usage := chunk.Usage.sigmaUsage()
		p.usage = &usage
		p.captureCompletionTokenDetails(chunk.Usage.CompletionTokensDetails)
	}
	if len(chunk.Choices) == 0 {
		return nil
	}
	if err := p.emitStart(ctx); err != nil {
		return err
	}
	for _, choice := range chunk.Choices {
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			p.finishReason = stopReason(*choice.FinishReason)
		}
		if len(choice.Logprobs) > 0 && !bytes.Equal(bytes.TrimSpace(choice.Logprobs), []byte("null")) {
			var logprobs any
			if err := json.Unmarshal(choice.Logprobs, &logprobs); err == nil && logprobs != nil {
				p.appendLogprobs(logprobs)
			}
		}
		if err := p.handleDelta(ctx, choice.Delta); err != nil {
			return err
		}
	}
	return nil
}

func (p *completionStreamParser) handleDelta(ctx context.Context, delta streamDelta) error {
	if delta.ReasoningContent != nil {
		if err := p.emitThinking(ctx, *delta.ReasoningContent); err != nil {
			return err
		}
	}
	if delta.Reasoning != nil {
		if err := p.emitThinking(ctx, *delta.Reasoning); err != nil {
			return err
		}
	}
	if delta.Thinking != nil {
		if err := p.emitThinking(ctx, *delta.Thinking); err != nil {
			return err
		}
	}
	if len(delta.Content) > 0 {
		text, ok, err := streamContentText(delta.Content)
		if err != nil {
			return fmt.Errorf("openai completions: decode stream content: %w", err)
		}
		if ok {
			if err := p.emitText(ctx, text); err != nil {
				return err
			}
		}
	}
	for _, annotation := range delta.Annotations {
		if annotation.Type != "url_citation" || annotation.URLCitation == nil {
			continue
		}
		p.addSource(map[string]any{
			providerToolOptionTypeKey: "url",
			"url":                     annotation.URLCitation.URL,
			"title":                   annotation.URLCitation.Title,
			"startIndex":              annotation.URLCitation.StartIndex,
			"endIndex":                annotation.URLCitation.EndIndex,
		})
	}
	for order, toolCall := range delta.ToolCalls {
		index := order
		if toolCall.Index != nil {
			index = *toolCall.Index
		}
		if err := p.emitToolCall(ctx, index, toolCall); err != nil {
			return err
		}
	}
	return nil
}

func (p *completionStreamParser) emitStart(ctx context.Context) error {
	if p.started {
		return nil
	}
	p.started = true
	return p.writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindStart})
}

func (p *completionStreamParser) emitText(ctx context.Context, delta string) error {
	if p.text == nil {
		p.text = &streamblocks.Text{ContentIndex: p.nextContentIndex()}
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindTextStart,
			ContentIndex: intPtr(p.text.ContentIndex),
		}); err != nil {
			return err
		}
		p.text.Started = true
	}
	if delta == "" {
		return nil
	}
	text := p.text.Append(delta)
	return p.writer.Emit(ctx, sigma.Event{
		Kind:         sigma.EventKindTextDelta,
		ContentIndex: intPtr(p.text.ContentIndex),
		DeltaText:    delta,
		Text:         text,
	})
}

func (p *completionStreamParser) emitThinking(ctx context.Context, delta string) error {
	if p.thinking == nil {
		p.thinking = &streamblocks.Thinking{ContentIndex: p.nextContentIndex()}
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindThinkingStart,
			ContentIndex: intPtr(p.thinking.ContentIndex),
		}); err != nil {
			return err
		}
		p.thinking.Started = true
	}
	if delta == "" {
		return nil
	}
	thinking := p.thinking.Append(delta)
	return p.writer.Emit(ctx, sigma.Event{
		Kind:         sigma.EventKindThinkingDelta,
		ContentIndex: intPtr(p.thinking.ContentIndex),
		DeltaText:    delta,
		Thinking:     thinking,
	})
}

func (p *completionStreamParser) emitToolCall(ctx context.Context, providerIndex int, delta streamToolCallDelta) error {
	state := p.toolCalls[providerIndex]
	if state == nil {
		state = &streamblocks.ToolCall{ContentIndex: p.nextContentIndex()}
		p.toolCalls[providerIndex] = state
	}
	if state.ID() == "" && delta.ID == "" && delta.Function.Name != "" {
		state.SetID(fmt.Sprintf("call_%d", providerIndex))
	}
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

func (p *completionStreamParser) finalize(ctx context.Context) sigma.AssistantMessage {
	contentByIndex := make(map[int]sigma.ContentBlock)
	if p.text != nil {
		contentByIndex[p.text.ContentIndex] = sigma.Text(p.text.String())
		if !p.text.Closed {
			_ = p.writer.Emit(ctx, sigma.Event{
				Kind:         sigma.EventKindTextEnd,
				ContentIndex: intPtr(p.text.ContentIndex),
				Text:         p.text.String(),
			})
			p.text.Closed = true
		}
	}
	if p.thinking != nil {
		contentByIndex[p.thinking.ContentIndex] = sigma.Thinking(p.thinking.String(), "")
		if !p.thinking.Closed {
			_ = p.writer.Emit(ctx, sigma.Event{
				Kind:         sigma.EventKindThinkingEnd,
				ContentIndex: intPtr(p.thinking.ContentIndex),
				Thinking:     p.thinking.String(),
			})
			p.thinking.Closed = true
		}
	}
	for _, state := range p.sortedToolCalls() {
		call := state.ToolCall()
		contentByIndex[state.ContentIndex] = sigma.ToolCallBlock(call.ID, call.Name, call.Arguments)
		if !state.Closed {
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
	if p.finishReason != "" {
		p.final.StopReason = p.finishReason
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
	p.final.ProviderMetadata = p.responseMetadata()
	return p.final
}

func (p *completionStreamParser) nextContentIndex() int {
	index := p.nextBlock
	p.nextBlock++
	return index
}

func (p *completionStreamParser) sortedToolCalls() []*streamblocks.ToolCall {
	states := make([]*streamblocks.ToolCall, 0, len(p.toolCalls))
	for _, state := range p.toolCalls {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (u openAIUsage) sigmaUsage() sigma.Usage {
	usage := sigma.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		TotalTokens:  u.TotalTokens,
	}
	if u.PromptTokensDetails != nil {
		usage.CacheReadInputTokens = u.PromptTokensDetails.CachedTokens
		usage.CacheWriteInputTokens = u.PromptTokensDetails.CacheWriteTokens
		usage.InputTokens = max(0, u.PromptTokens-usage.CacheReadInputTokens-usage.CacheWriteInputTokens)
	}
	if u.CompletionTokensDetails != nil {
		usage.ThinkingTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	return usage
}

func (p *completionStreamParser) setMetadata(key string, value any) {
	if p.metadata == nil {
		p.metadata = make(map[string]any)
	}
	p.metadata[key] = value
}

func (p *completionStreamParser) appendLogprobs(value any) {
	next, ok := value.(map[string]any)
	if !ok {
		p.setMetadata("logprobs", value)
		return
	}
	if p.metadata == nil {
		p.metadata = make(map[string]any)
	}
	current, _ := p.metadata["logprobs"].(map[string]any)
	if current == nil {
		p.metadata["logprobs"] = copyAnyMap(next)
		return
	}
	for key, nextValue := range next {
		currentSlice, currentOK := current[key].([]any)
		nextSlice, nextOK := nextValue.([]any)
		if currentOK && nextOK {
			current[key] = append(currentSlice, nextSlice...)
			continue
		}
		current[key] = nextValue
	}
}

func (p *completionStreamParser) captureCompletionTokenDetails(details *outputTokenDetails) {
	if details == nil {
		return
	}
	if tokens := details.AcceptedPredictionTokens; tokens > 0 {
		p.setMetadata("acceptedPredictionTokens", tokens)
	}
	if tokens := details.RejectedPredictionTokens; tokens > 0 {
		p.setMetadata("rejectedPredictionTokens", tokens)
	}
}

func (p *completionStreamParser) addSource(source map[string]any) {
	if len(source) == 0 {
		return
	}
	p.sources = append(p.sources, source)
}

func (p *completionStreamParser) responseMetadata() map[string]any {
	size := len(p.metadata)
	if p.responseID != "" {
		size++
	}
	if p.providerModel != "" && p.providerModel != string(p.model.ID) {
		size++
	}
	if len(p.sources) > 0 {
		size++
	}
	if size == 0 {
		return nil
	}

	metadata := make(map[string]any, size)
	for key, value := range p.metadata {
		metadata[key] = value
	}
	if p.responseID != "" {
		metadata["id"] = p.responseID
	}
	if p.providerModel != "" && p.providerModel != string(p.model.ID) {
		metadata["model"] = p.providerModel
	}
	if len(p.sources) > 0 {
		sources := make([]map[string]any, len(p.sources))
		copy(sources, p.sources)
		metadata["sources"] = sources
	}
	return metadata
}

func streamContentText(raw json.RawMessage) (string, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false, nil
	}

	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		return text, true, nil
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return "", false, err
	}

	var builder strings.Builder
	found := false
	for _, part := range parts {
		if part.Type != "" && part.Type != "text" {
			continue
		}
		found = true
		builder.WriteString(part.Text)
	}
	return builder.String(), found, nil
}

func stopReason(reason string) sigma.StopReason {
	switch reason {
	case "stop":
		return sigma.StopReasonEndTurn
	case "length":
		return sigma.StopReasonMaxTokens
	case "tool_calls", "function_call":
		return sigma.StopReasonToolCalls
	case "content_filter":
		return sigma.StopReasonContentFilter
	default:
		return sigma.StopReasonUnknown
	}
}

func intPtr(value int) *int {
	return &value
}
