// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package openai

import (
	"bytes"
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
	Usage        *openAIUsage    `json:"usage"`
}

type streamDelta struct {
	Role             string                `json:"role"`
	Content          json.RawMessage       `json:"content"`
	ReasoningContent *string               `json:"reasoning_content"`
	Reasoning        *string               `json:"reasoning"`
	ReasoningText    *string               `json:"reasoning_text"`
	ReasoningDetails json.RawMessage       `json:"reasoning_details"`
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
	PromptCacheHitTokens    *int                `json:"prompt_cache_hit_tokens"`
	PromptTokensDetails     *promptTokenDetails `json:"prompt_tokens_details"`
	CompletionTokensDetails *outputTokenDetails `json:"completion_tokens_details"`
	Cost                    *float64            `json:"cost"`
	Currency                string              `json:"currency"`
}

type promptTokenDetails struct {
	CachedTokens     *int `json:"cached_tokens"`
	CacheWriteTokens *int `json:"cache_write_tokens"`
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
	writer           sigma.StreamWriter
	model            sigma.Model
	final            sigma.AssistantMessage
	started          bool
	text             *streamblocks.Text
	thinking         *streamblocks.Thinking
	toolCalls        map[string]*streamblocks.ToolCall
	toolCallOrdinals map[string]int
	lastToolCallKey  string
	nextBlock        int
	usage            *sigma.Usage
	finishReason     sigma.StopReason
	responseID       string
	providerModel    string
	metadata         map[string]any
	sources          []map[string]any
	reasoningDetails []any
	sawFinishReason  bool
}

func parseCompletionsStream(ctx context.Context, r io.Reader, writer sigma.StreamWriter, model sigma.Model) (sigma.AssistantMessage, error) {
	parser := completionStreamParser{
		writer:           writer,
		model:            model,
		toolCalls:        make(map[string]*streamblocks.ToolCall),
		toolCallOrdinals: make(map[string]int),
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
	if !parser.sawFinishReason {
		return parser.finalize(ctx), errors.New("openai completions: stream ended without finish_reason")
	}
	return parser.finalize(ctx), nil
}

func (p *completionStreamParser) handleEvent(ctx context.Context, event sse.Event) error {
	var chunk streamChunk
	data := providerText(event.Data)
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return fmt.Errorf("openai completions: decode stream chunk: %w", err)
	}
	if chunk.Error != nil {
		return openAIStreamProviderError(p.model, sigma.APIOpenAICompletions, chunk.Error)
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
		usage, _ = sigma.AccountUsage(p.model, usage, sigma.WithRawUsage(*chunk.Usage))
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
		if chunk.Usage == nil && choice.Usage != nil {
			usage := choice.Usage.sigmaUsage()
			usage, _ = sigma.AccountUsage(p.model, usage, sigma.WithRawUsage(*choice.Usage))
			p.usage = &usage
			p.captureCompletionTokenDetails(choice.Usage.CompletionTokensDetails)
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			if err := providerFinishReasonError(p.model, *choice.FinishReason); err != nil {
				return err
			}
			p.sawFinishReason = true
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

func openAIStreamProviderError(model sigma.Model, api sigma.API, err *streamError) *sigma.ProviderError {
	body, _ := json.Marshal(map[string]any{"error": err})
	cause := sigma.ErrProviderResponse
	if contextOverflowCause(body) != nil {
		cause = sigma.ErrContextOverflow
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

func (p *completionStreamParser) handleDelta(ctx context.Context, delta streamDelta) error {
	if reasoning, ok := firstReasoningDelta(delta); ok {
		if err := p.emitThinking(ctx, reasoning); err != nil {
			return err
		}
	}
	if len(delta.ReasoningDetails) > 0 && !bytes.Equal(bytes.TrimSpace(delta.ReasoningDetails), []byte("null")) {
		details, err := parseReasoningDetails(delta.ReasoningDetails)
		if err != nil {
			return err
		}
		p.reasoningDetails = append(p.reasoningDetails, details...)
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
		if err := p.emitToolCall(ctx, p.toolCallKey(order, toolCall), toolCall); err != nil {
			return err
		}
	}
	return nil
}

func firstReasoningDelta(delta streamDelta) (string, bool) {
	for _, value := range []*string{
		delta.ReasoningContent,
		delta.Reasoning,
		delta.ReasoningText,
		delta.Thinking,
	} {
		if value != nil && *value != "" {
			return *value, true
		}
	}
	return "", false
}

func (p *completionStreamParser) emitStart(ctx context.Context) error {
	if p.started {
		return nil
	}
	p.started = true
	return p.writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindStart})
}

func (p *completionStreamParser) emitText(ctx context.Context, delta string) error {
	delta = providerText(delta)
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
	delta = providerText(delta)
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

func (p *completionStreamParser) toolCallKey(order int, delta streamToolCallDelta) string {
	if delta.Index != nil {
		return fmt.Sprintf("index:%d", *delta.Index)
	}
	if delta.ID != "" {
		return "id:" + delta.ID
	}
	// An index-less, id-less delta with no function name is an argument
	// continuation of the most recent tool call (providers that send the id
	// only on the first delta).
	if delta.Function.Name == "" && p.lastToolCallKey != "" {
		return p.lastToolCallKey
	}
	return fmt.Sprintf("order:%d", order)
}

func (p *completionStreamParser) emitToolCall(ctx context.Context, key string, delta streamToolCallDelta) error {
	state := p.toolCalls[key]
	if state == nil {
		state = &streamblocks.ToolCall{ContentIndex: p.nextContentIndex()}
		p.toolCallOrdinals[key] = len(p.toolCalls)
		p.toolCalls[key] = state
	}
	p.lastToolCallKey = key
	if state.ID() == "" && delta.ID == "" && delta.Function.Name != "" {
		// Synthetic ids must be unique across the whole stream: prefer the
		// provider index and fall back to the tool call's creation ordinal,
		// never the delta's position within its own chunk.
		fallback := p.toolCallOrdinals[key]
		if delta.Index != nil {
			fallback = *delta.Index
		}
		state.SetID(fmt.Sprintf("call_%d", fallback))
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
	idlessDetails, detailsByID := partitionReasoningDetails(p.reasoningDetails)
	for position, state := range p.sortedToolCalls() {
		details := detailsByID[state.ID()]
		// Details without an id cannot be correlated to a specific call;
		// attach them once, to the first tool call, so replay does not send
		// duplicate copies for every parallel call.
		if position == 0 && len(idlessDetails) > 0 {
			details = append(idlessDetails, details...)
		}
		if len(details) > 0 {
			if state.ProviderMetadata == nil {
				state.ProviderMetadata = make(map[string]any)
			}
			state.ProviderMetadata["reasoning_details"] = details
		}
		call := state.ToolCall()
		block := sigma.ToolCallBlock(call.ID, call.Name, call.Arguments)
		block.ProviderSignature = call.ProviderSignature
		block.ProviderMetadata = call.ProviderMetadata
		contentByIndex[state.ContentIndex] = block
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
		usage, cost := sigma.AccountUsage(p.model, *p.usage)
		p.final.Usage = &usage
		p.final.Cost = &cost
	}
	p.final.ProviderMetadata = p.responseMetadata()
	return p.final
}

func partitionReasoningDetails(details []any) (idless []any, byID map[string][]any) {
	for _, detail := range details {
		typed, _ := detail.(map[string]any)
		id, _ := typed["id"].(string)
		if id == "" {
			idless = append(idless, detail)
			continue
		}
		if byID == nil {
			byID = make(map[string][]any)
		}
		byID[id] = append(byID[id], detail)
	}
	return idless, byID
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
		if u.PromptTokensDetails.CachedTokens != nil {
			usage.CacheReadInputTokens = *u.PromptTokensDetails.CachedTokens
		}
		if u.PromptTokensDetails.CacheWriteTokens != nil {
			usage.CacheWriteInputTokens = *u.PromptTokensDetails.CacheWriteTokens
		}
	}
	if usage.CacheReadInputTokens == 0 && u.PromptCacheHitTokens != nil {
		usage.CacheReadInputTokens = *u.PromptCacheHitTokens
	}
	if usage.CacheReadInputTokens > 0 || usage.CacheWriteInputTokens > 0 {
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

func parseReasoningDetails(raw json.RawMessage) ([]any, error) {
	var details []any
	if err := json.Unmarshal(raw, &details); err != nil {
		return nil, fmt.Errorf("openai completions: decode reasoning_details: %w", err)
	}
	return details, nil
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
	case "stop", "end":
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

func providerFinishReasonError(model sigma.Model, reason string) error {
	switch reason {
	case "network_error", "model_context_window_exceeded":
		return openAIStreamProviderError(model, sigma.APIOpenAICompletions, &streamError{
			Message: "Provider finish_reason: " + reason,
			Type:    reason,
			Code:    reason,
		})
	default:
		return nil
	}
}

func intPtr(value int) *int {
	return &value
}
