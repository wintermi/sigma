// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamblocks"
)

type generateContentResponse struct {
	Candidates     []googleCandidate    `json:"candidates"`
	PromptFeedback map[string]any       `json:"promptFeedback"`
	UsageMetadata  *googleUsageMetadata `json:"usageMetadata"`
	ModelVersion   string               `json:"modelVersion"`
	ResponseID     string               `json:"responseId"`
	Error          *googleAPIError      `json:"error"`
}

type googleCandidate struct {
	Content       googleStreamContent `json:"content"`
	FinishReason  string              `json:"finishReason"`
	SafetyRatings []any               `json:"safetyRatings"`
	Index         int                 `json:"index"`
}

type googleStreamContent struct {
	Role  string       `json:"role"`
	Parts []googlePart `json:"parts"`
}

type googlePart struct {
	Text             string              `json:"text"`
	Thought          bool                `json:"thought"`
	ThoughtSignature string              `json:"thoughtSignature"`
	FunctionCall     *googleFunctionCall `json:"functionCall"`
}

type googleFunctionCall struct {
	Name string `json:"name"`
	Args any    `json:"args"`
	ID   string `json:"id"`
}

type googleUsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
}

type googleAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

type streamParser struct {
	writer          sigma.StreamWriter
	model           sigma.Model
	final           sigma.AssistantMessage
	started         bool
	blocks          []*googleBlockState
	currentText     *googleBlockState
	currentThink    *googleBlockState
	usage           *sigma.Usage
	stopReason      sigma.StopReason
	responseID      string
	modelVersion    string
	promptFeedback  map[string]any
	toolCallIDs     map[string]struct{}
	toolCallCounter int
}

type googleBlockState struct {
	contentIndex int
	kind         sigma.ContentBlockType
	signature    string
	text         streamblocks.Text
	thinking     streamblocks.Thinking
	tool         streamblocks.ToolCall
	started      bool
	closed       bool
}

func parseGenerativeStream(ctx context.Context, r io.Reader, writer sigma.StreamWriter, model sigma.Model) (sigma.AssistantMessage, error) {
	parser := streamParser{
		writer: writer,
		model:  model,
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

func (p *streamParser) handleEvent(ctx context.Context, event sse.Event) error {
	var response generateContentResponse
	if err := json.Unmarshal([]byte(event.Data), &response); err != nil {
		return fmt.Errorf("google generative ai: decode stream event: %w", err)
	}
	if response.Error != nil {
		return streamError(response.Error)
	}
	p.captureResponse(response)
	if len(response.Candidates) == 0 {
		return p.emitStart(ctx)
	}
	if err := p.emitStart(ctx); err != nil {
		return err
	}
	for _, candidate := range response.Candidates {
		if candidate.FinishReason != "" {
			p.stopReason = googleStopReason(candidate.FinishReason)
		}
		for _, part := range candidate.Content.Parts {
			if err := p.handlePart(ctx, part); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *streamParser) captureResponse(response generateContentResponse) {
	if response.ResponseID != "" {
		p.responseID = response.ResponseID
	}
	if response.ModelVersion != "" {
		p.modelVersion = response.ModelVersion
	}
	if len(response.PromptFeedback) > 0 {
		p.promptFeedback = response.PromptFeedback
	}
	if response.UsageMetadata != nil {
		usage := response.UsageMetadata.sigmaUsage()
		p.usage = &usage
	}
}

func (p *streamParser) handlePart(ctx context.Context, part googlePart) error {
	switch {
	case part.FunctionCall != nil:
		return p.emitToolCall(ctx, part)
	case part.Thought:
		return p.emitThinking(ctx, part)
	case part.Text != "":
		return p.emitText(ctx, part)
	case part.ThoughtSignature != "":
		p.attachSignature(part.ThoughtSignature)
		return nil
	default:
		return nil
	}
}

func (p *streamParser) emitStart(ctx context.Context) error {
	if p.started {
		return nil
	}
	p.started = true
	return p.writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindStart})
}

func (p *streamParser) emitText(ctx context.Context, part googlePart) error {
	state := p.currentText
	if state == nil || p.lastBlock() != state {
		state = p.newBlock(sigma.ContentBlockText)
		p.currentText = state
	}
	if part.ThoughtSignature != "" {
		state.signature = part.ThoughtSignature
	}
	if !state.started {
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindTextStart,
			ContentIndex: intPtr(state.contentIndex),
		}); err != nil {
			return err
		}
		state.started = true
	}
	text := state.text.Append(part.Text)
	return p.writer.Emit(ctx, sigma.Event{
		Kind:         sigma.EventKindTextDelta,
		ContentIndex: intPtr(state.contentIndex),
		DeltaText:    part.Text,
		Text:         text,
	})
}

func (p *streamParser) emitThinking(ctx context.Context, part googlePart) error {
	state := p.currentThink
	if state == nil || p.lastBlock() != state {
		state = p.newBlock(sigma.ContentBlockThinking)
		p.currentThink = state
	}
	if part.ThoughtSignature != "" {
		state.signature = part.ThoughtSignature
	}
	if !state.started {
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindThinkingStart,
			ContentIndex: intPtr(state.contentIndex),
		}); err != nil {
			return err
		}
		state.started = true
	}
	if part.Text == "" {
		return nil
	}
	thinking := state.thinking.Append(part.Text)
	return p.writer.Emit(ctx, sigma.Event{
		Kind:         sigma.EventKindThinkingDelta,
		ContentIndex: intPtr(state.contentIndex),
		DeltaText:    part.Text,
		Thinking:     thinking,
	})
}

func (p *streamParser) emitToolCall(ctx context.Context, part googlePart) error {
	state := p.newBlock(sigma.ContentBlockToolCall)
	state.signature = part.ThoughtSignature
	state.tool.SetID(p.googleToolCallID(part.FunctionCall.ID))
	state.tool.SetName(part.FunctionCall.Name)
	state.tool.ProviderSignature = state.signature
	arguments, err := json.Marshal(part.FunctionCall.Args)
	if err != nil {
		return fmt.Errorf("google generative ai: encode function call args: %w", err)
	}
	if part.FunctionCall.Args == nil {
		arguments = []byte("{}")
	}
	state.tool.SetArguments(string(arguments))
	partial := state.partial(state.tool.ArgumentsText())
	if err := p.writer.Emit(ctx, sigma.Event{
		Kind:            sigma.EventKindToolCallStart,
		ContentIndex:    intPtr(state.contentIndex),
		PartialToolCall: partial,
	}); err != nil {
		return err
	}
	state.started = true
	return p.writer.Emit(ctx, sigma.Event{
		Kind:            sigma.EventKindToolCallDelta,
		ContentIndex:    intPtr(state.contentIndex),
		PartialToolCall: partial,
	})
}

func (p *streamParser) googleToolCallID(id string) string {
	if p.toolCallIDs == nil {
		p.toolCallIDs = make(map[string]struct{})
	}
	if id != "" {
		if _, exists := p.toolCallIDs[id]; !exists {
			p.toolCallIDs[id] = struct{}{}
			return id
		}
	}
	for {
		p.toolCallCounter++
		synthetic := fmt.Sprintf("google_tool_call_%d", p.toolCallCounter)
		if _, exists := p.toolCallIDs[synthetic]; !exists {
			p.toolCallIDs[synthetic] = struct{}{}
			return synthetic
		}
	}
}

func (p *streamParser) attachSignature(signature string) {
	if len(p.blocks) == 0 {
		state := p.newBlock(sigma.ContentBlockText)
		state.signature = signature
		return
	}
	p.blocks[len(p.blocks)-1].signature = signature
}

func (p *streamParser) finalize(ctx context.Context) sigma.AssistantMessage {
	p.emitEndEvents(ctx)
	if len(p.blocks) > 0 {
		p.final.Content = make([]sigma.ContentBlock, 0, len(p.blocks))
		for _, state := range p.blocks {
			p.final.Content = append(p.final.Content, state.contentBlock())
		}
	}
	if p.hasToolCalls() && (p.stopReason == "" || p.stopReason == sigma.StopReasonEndTurn || p.stopReason == sigma.StopReasonUnknown) {
		p.final.StopReason = sigma.StopReasonToolCalls
	} else if p.stopReason != "" {
		p.final.StopReason = p.stopReason
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

func (p *streamParser) emitEndEvents(ctx context.Context) {
	for _, state := range p.blocks {
		if !state.started || state.closed {
			continue
		}
		switch state.kind { //nolint:exhaustive
		case sigma.ContentBlockText:
			_ = p.writer.Emit(ctx, sigma.Event{
				Kind:         sigma.EventKindTextEnd,
				ContentIndex: intPtr(state.contentIndex),
				Text:         state.text.String(),
			})
		case sigma.ContentBlockThinking:
			_ = p.writer.Emit(ctx, sigma.Event{
				Kind:         sigma.EventKindThinkingEnd,
				ContentIndex: intPtr(state.contentIndex),
				Thinking:     state.thinking.String(),
			})
		case sigma.ContentBlockToolCall:
			call := state.toolCall()
			_ = p.writer.Emit(ctx, sigma.Event{
				Kind:         sigma.EventKindToolCallEnd,
				ContentIndex: intPtr(state.contentIndex),
				ToolCall:     &call,
			})
		}
		state.closed = true
	}
}

func (p *streamParser) newBlock(kind sigma.ContentBlockType) *googleBlockState {
	state := &googleBlockState{
		contentIndex: len(p.blocks),
		kind:         kind,
	}
	p.blocks = append(p.blocks, state)
	return state
}

func (p *streamParser) lastBlock() *googleBlockState {
	if len(p.blocks) == 0 {
		return nil
	}
	return p.blocks[len(p.blocks)-1]
}

func (p *streamParser) hasToolCalls() bool {
	for _, state := range p.blocks {
		if state.kind == sigma.ContentBlockToolCall {
			return true
		}
	}
	return false
}

func (p *streamParser) responseMetadata() map[string]any {
	metadata := make(map[string]any)
	if p.responseID != "" {
		metadata["id"] = p.responseID
	}
	if p.modelVersion != "" && p.modelVersion != string(p.model.ID) {
		metadata["modelVersion"] = p.modelVersion
	}
	if len(p.promptFeedback) > 0 {
		metadata["promptFeedback"] = p.promptFeedback
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func (s *googleBlockState) partial(delta string) *sigma.PartialToolCall {
	s.tool.ProviderSignature = s.signature
	return s.tool.Partial(delta, streamblocks.ToolPartialArguments)
}

func (s *googleBlockState) toolCall() sigma.ToolCall {
	s.tool.ProviderSignature = s.signature
	return s.tool.ToolCall()
}

func (s *googleBlockState) contentBlock() sigma.ContentBlock {
	switch s.kind {
	case sigma.ContentBlockThinking:
		block := sigma.Thinking(s.thinking.String(), "")
		block.ProviderSignature = s.signature
		return block
	case sigma.ContentBlockToolCall:
		call := s.toolCall()
		block := sigma.ToolCallBlock(call.ID, call.Name, call.Arguments)
		block.ProviderSignature = s.signature
		return block
	default:
		block := sigma.Text(s.text.String())
		block.ProviderSignature = s.signature
		return block
	}
}

func (u googleUsageMetadata) sigmaUsage() sigma.Usage {
	return sigma.Usage{
		InputTokens:          max(0, u.PromptTokenCount-u.CachedContentTokenCount),
		OutputTokens:         u.CandidatesTokenCount + u.ThoughtsTokenCount,
		TotalTokens:          u.TotalTokenCount,
		CacheReadInputTokens: u.CachedContentTokenCount,
		ThinkingTokens:       u.ThoughtsTokenCount,
	}
}

func googleStopReason(reason string) sigma.StopReason {
	switch reason {
	case "STOP":
		return sigma.StopReasonEndTurn
	case "MAX_TOKENS":
		return sigma.StopReasonMaxTokens
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII",
		"IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION", "IMAGE_OTHER",
		"LANGUAGE", "NO_IMAGE":
		return sigma.StopReasonContentFilter
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL":
		return sigma.StopReasonToolCalls
	default:
		return sigma.StopReasonUnknown
	}
}

func streamError(err *googleAPIError) error {
	if err == nil {
		return fmt.Errorf("google generative ai: stream error")
	}
	if err.Status != "" {
		return fmt.Errorf("google generative ai: stream error: %s: %s", err.Status, err.Message)
	}
	return fmt.Errorf("google generative ai: stream error: %s", err.Message)
}

func intPtr(value int) *int {
	return &value
}
