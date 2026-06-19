// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package mistral

import (
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

type conversationEvent struct {
	Type           string                 `json:"type"`
	ConversationID string                 `json:"conversation_id"`
	OutputIndex    *int                   `json:"output_index"`
	ContentIndex   *int                   `json:"content_index"`
	ID             string                 `json:"id"`
	Model          string                 `json:"model"`
	AgentID        string                 `json:"agent_id"`
	Role           string                 `json:"role"`
	Content        conversationContent    `json:"content"`
	Name           string                 `json:"name"`
	ToolCallID     string                 `json:"tool_call_id"`
	Arguments      string                 `json:"arguments"`
	Usage          *conversationUsage     `json:"usage"`
	StopReason     string                 `json:"stop_reason"`
	Error          *conversationAPIError  `json:"error"`
	Message        string                 `json:"message"`
	Code           any                    `json:"code"`
	Metadata       map[string]any         `json:"metadata"`
	Raw            map[string]interface{} `json:"-"`
}

type conversationContent struct {
	Set    bool
	Text   *string
	Chunks []conversationContentChunk
}

type conversationContentChunk struct {
	Type string
	Text string
}

func (c *conversationContent) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	c.Set = true
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		c.Text = &text
		return nil
	}
	var rawChunks []json.RawMessage
	if err := json.Unmarshal(data, &rawChunks); err == nil {
		for _, raw := range rawChunks {
			chunk, err := parseConversationContentChunk(raw)
			if err != nil {
				return err
			}
			c.Chunks = append(c.Chunks, chunk)
		}
		return nil
	}
	chunk, err := parseConversationContentChunk(data)
	if err != nil {
		return err
	}
	c.Chunks = append(c.Chunks, chunk)
	return nil
}

func parseConversationContentChunk(data []byte) (conversationContentChunk, error) {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		return conversationContentChunk{Type: payloadValueText, Text: text}, nil
	}
	var raw struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Thinking json.RawMessage `json:"thinking"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return conversationContentChunk{}, err
	}
	chunk := conversationContentChunk{Type: raw.Type, Text: raw.Text}
	if raw.Type == "thinking" && len(raw.Thinking) > 0 {
		chunk.Text = thinkingText(raw.Thinking)
	}
	if chunk.Type == "" {
		chunk.Type = payloadValueText
	}
	return chunk, nil
}

func thinkingText(data json.RawMessage) string {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &parts); err == nil {
		var out strings.Builder
		for _, part := range parts {
			out.WriteString(part.Text)
		}
		return out.String()
	}
	return ""
}

type conversationUsage struct {
	PromptTokens             int                       `json:"prompt_tokens"`
	CompletionTokens         int                       `json:"completion_tokens"`
	TotalTokens              int                       `json:"total_tokens"`
	ConnectorTokens          int                       `json:"connector_tokens"`
	Connectors               map[string]int            `json:"connectors"`
	PromptTokensDetails      *conversationTokenDetails `json:"prompt_tokens_details"`
	PromptTokensDetailsCamel *conversationTokenDetails `json:"promptTokensDetails"`
	PromptTokenDetails       *conversationTokenDetails `json:"prompt_token_details"`
	PromptTokenDetailsCamel  *conversationTokenDetails `json:"promptTokenDetails"`
	NumCachedTokens          int                       `json:"num_cached_tokens"`
	NumCachedTokensCamel     int                       `json:"numCachedTokens"`
}

type conversationTokenDetails struct {
	CachedTokens      int `json:"cached_tokens"`
	CachedTokensCamel int `json:"cachedTokens"`
}

type conversationAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

type conversationStreamParser struct {
	writer         sigma.StreamWriter
	model          sigma.Model
	final          sigma.AssistantMessage
	started        bool
	textBlocks     map[string]*streamblocks.Text
	thinkingBlocks map[string]*streamblocks.Thinking
	toolCalls      map[string]*streamblocks.ToolCall
	nextBlock      int
	usage          *sigma.Usage
	stopReason     sigma.StopReason
	conversationID string
	providerModel  string
	agentID        string
	responseID     string
}

func parseConversationStream(ctx context.Context, r io.Reader, writer sigma.StreamWriter, model sigma.Model) (sigma.AssistantMessage, error) {
	parser := conversationStreamParser{
		writer:         writer,
		model:          model,
		textBlocks:     make(map[string]*streamblocks.Text),
		thinkingBlocks: make(map[string]*streamblocks.Thinking),
		toolCalls:      make(map[string]*streamblocks.ToolCall),
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

func (p *conversationStreamParser) handleEvent(ctx context.Context, event sse.Event) error {
	var parsed conversationEvent
	if err := json.Unmarshal([]byte(event.Data), &parsed); err != nil {
		return fmt.Errorf("mistral conversations: decode stream event: %w", err)
	}
	if parsed.Type == "" {
		parsed.Type = event.Event
	}
	if parsed.Error != nil || parsed.Type == "conversation.response.error" {
		return p.eventError(parsed)
	}
	p.capture(parsed)

	switch parsed.Type {
	case "conversation.response.started":
		return p.emitStart(ctx)
	case "message.output.delta":
		return p.emitContent(ctx, parsed)
	case "function.call.delta":
		return p.emitToolCall(ctx, parsed)
	case "conversation.response.done":
		if parsed.StopReason != "" {
			p.stopReason = mistralStopReason(parsed.StopReason)
		}
		return p.emitStart(ctx)
	case "tool.execution.started", "tool.execution.done", "agent.handoff.started", "agent.handoff.done":
		return p.emitStart(ctx)
	default:
		return nil
	}
}

func (p *conversationStreamParser) capture(event conversationEvent) {
	if event.ConversationID != "" {
		p.conversationID = event.ConversationID
	}
	if event.Model != "" {
		p.providerModel = event.Model
	}
	if event.AgentID != "" {
		p.agentID = event.AgentID
	}
	if event.ID != "" && p.responseID == "" {
		p.responseID = event.ID
	}
	if event.Usage != nil {
		usage := event.Usage.sigmaUsage()
		usage, _ = sigma.AccountUsage(p.model, usage, sigma.WithRawUsage(*event.Usage))
		p.usage = &usage
	}
}

func (p *conversationStreamParser) emitStart(ctx context.Context) error {
	if p.started {
		return nil
	}
	p.started = true
	return p.writer.Emit(ctx, sigma.Event{Kind: sigma.EventKindStart})
}

func (p *conversationStreamParser) emitContent(ctx context.Context, event conversationEvent) error {
	if !event.Content.Set {
		return nil
	}
	if event.Content.Text != nil {
		return p.emitText(ctx, outputContentKey(event, sigma.ContentBlockText), *event.Content.Text)
	}
	for _, chunk := range event.Content.Chunks {
		switch chunk.Type {
		case "thinking":
			if err := p.emitThinking(ctx, outputContentKey(event, sigma.ContentBlockThinking), chunk.Text); err != nil {
				return err
			}
		default:
			if err := p.emitText(ctx, outputContentKey(event, sigma.ContentBlockText), chunk.Text); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *conversationStreamParser) emitText(ctx context.Context, key string, delta string) error {
	if err := p.emitStart(ctx); err != nil {
		return err
	}
	state := p.textBlocks[key]
	if state == nil {
		state = &streamblocks.Text{ContentIndex: p.nextContentIndex()}
		p.textBlocks[key] = state
	}
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

func (p *conversationStreamParser) emitThinking(ctx context.Context, key string, delta string) error {
	if err := p.emitStart(ctx); err != nil {
		return err
	}
	state := p.thinkingBlocks[key]
	if state == nil {
		state = &streamblocks.Thinking{ContentIndex: p.nextContentIndex()}
		p.thinkingBlocks[key] = state
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

func (p *conversationStreamParser) emitToolCall(ctx context.Context, event conversationEvent) error {
	if err := p.emitStart(ctx); err != nil {
		return err
	}
	key := toolCallKey(event)
	state := p.toolCalls[key]
	if state == nil {
		state = &streamblocks.ToolCall{ContentIndex: p.nextContentIndex()}
		p.toolCalls[key] = state
	}
	state.SetID(event.ToolCallID)
	if state.ID() == "" {
		state.SetID(event.ID)
	}
	state.SetName(event.Name)
	state.AppendArguments(event.Arguments)

	partial := state.Partial(event.Arguments, streamblocks.ToolPartialArgumentsText)
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

func (p *conversationStreamParser) eventError(event conversationEvent) error {
	errorType := ""
	if event.Error != nil {
		errorType = event.Error.Type
	}
	if errorType == "" {
		errorType = "stream"
	}
	body, _ := json.Marshal(event)
	return sigma.NewProviderError(
		p.model.Provider,
		sigma.APIMistralConversations,
		p.model.ID,
		0,
		"",
		0,
		body,
		fmt.Errorf("mistral conversations: %s error", errorType),
	)
}

func (p *conversationStreamParser) finalize(ctx context.Context) sigma.AssistantMessage {
	items := make([]finalContentItem, 0, len(p.textBlocks)+len(p.thinkingBlocks)+len(p.toolCalls))
	for _, state := range p.sortedTextBlocks() {
		state := state
		items = append(items, finalContentItem{
			index: state.ContentIndex,
			block: sigma.Text(state.String()),
			close: func() {
				if !state.Closed && state.Started {
					_ = p.writer.Emit(ctx, sigma.Event{
						Kind:         sigma.EventKindTextEnd,
						ContentIndex: intPtr(state.ContentIndex),
						Text:         state.String(),
					})
					state.Closed = true
				}
			},
		})
	}
	for _, state := range p.sortedThinkingBlocks() {
		state := state
		items = append(items, finalContentItem{
			index: state.ContentIndex,
			block: sigma.Thinking(state.String(), state.Signature),
			close: func() {
				if !state.Closed && state.Started {
					_ = p.writer.Emit(ctx, sigma.Event{
						Kind:         sigma.EventKindThinkingEnd,
						ContentIndex: intPtr(state.ContentIndex),
						Thinking:     state.String(),
					})
					state.Closed = true
				}
			},
		})
	}
	for _, state := range p.sortedToolCalls() {
		state := state
		call := state.ToolCall()
		items = append(items, finalContentItem{
			index: state.ContentIndex,
			block: sigma.ToolCallBlock(call.ID, call.Name, call.Arguments),
			close: func() {
				call := state.ToolCall()
				if !state.Closed && state.Started {
					_ = p.writer.Emit(ctx, sigma.Event{
						Kind:         sigma.EventKindToolCallEnd,
						ContentIndex: intPtr(state.ContentIndex),
						ToolCall:     &call,
					})
					state.Closed = true
				}
			},
		})
	}

	if len(items) > 0 {
		sort.Slice(items, func(i, j int) bool {
			return items[i].index < items[j].index
		})
		p.final.Content = make([]sigma.ContentBlock, 0, len(items))
		for _, item := range items {
			p.final.Content = append(p.final.Content, item.block)
			item.close()
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
		usage, cost := sigma.AccountUsage(p.model, *p.usage)
		p.final.Usage = &usage
		p.final.Cost = &cost
	}
	p.final.ProviderMetadata = p.responseMetadata()
	return p.final
}

type finalContentItem struct {
	index int
	block sigma.ContentBlock
	close func()
}

func (p *conversationStreamParser) responseMetadata() map[string]any {
	metadata := make(map[string]any)
	if p.conversationID != "" {
		metadata["conversation_id"] = p.conversationID
	}
	if p.responseID != "" {
		metadata["id"] = p.responseID
	}
	if p.providerModel != "" && p.providerModel != string(p.model.ID) {
		metadata["model"] = p.providerModel
	}
	if p.agentID != "" {
		metadata["agent_id"] = p.agentID
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func (p *conversationStreamParser) nextContentIndex() int {
	index := p.nextBlock
	p.nextBlock++
	return index
}

func (p *conversationStreamParser) sortedTextBlocks() []*streamblocks.Text {
	states := make([]*streamblocks.Text, 0, len(p.textBlocks))
	for _, state := range p.textBlocks {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (p *conversationStreamParser) sortedThinkingBlocks() []*streamblocks.Thinking {
	states := make([]*streamblocks.Thinking, 0, len(p.thinkingBlocks))
	for _, state := range p.thinkingBlocks {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (p *conversationStreamParser) sortedToolCalls() []*streamblocks.ToolCall {
	states := make([]*streamblocks.ToolCall, 0, len(p.toolCalls))
	for _, state := range p.toolCalls {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (u conversationUsage) sigmaUsage() sigma.Usage {
	toolUseInputTokens := u.ConnectorTokens
	if toolUseInputTokens == 0 {
		for _, tokens := range u.Connectors {
			toolUseInputTokens += tokens
		}
	}
	cacheReadInputTokens := u.cachedPromptTokens()
	return sigma.Usage{
		InputTokens:          max(0, u.PromptTokens-cacheReadInputTokens),
		OutputTokens:         u.CompletionTokens,
		TotalTokens:          u.TotalTokens,
		CacheReadInputTokens: cacheReadInputTokens,
		ToolUseInputTokens:   toolUseInputTokens,
	}
}

func (u conversationUsage) cachedPromptTokens() int {
	candidates := []int{
		cachedTokens(u.PromptTokensDetails),
		cachedTokens(u.PromptTokensDetailsCamel),
		cachedTokens(u.PromptTokenDetails),
		cachedTokens(u.PromptTokenDetailsCamel),
		u.NumCachedTokens,
		u.NumCachedTokensCamel,
	}
	for _, tokens := range candidates {
		if tokens > 0 {
			return min(u.PromptTokens, tokens)
		}
	}
	return 0
}

func cachedTokens(details *conversationTokenDetails) int {
	if details == nil {
		return 0
	}
	if details.CachedTokens > 0 {
		return details.CachedTokens
	}
	return details.CachedTokensCamel
}

func outputContentKey(event conversationEvent, kind sigma.ContentBlockType) string {
	outputIndex := 0
	if event.OutputIndex != nil {
		outputIndex = *event.OutputIndex
	}
	contentIndex := 0
	if event.ContentIndex != nil {
		contentIndex = *event.ContentIndex
	}
	return fmt.Sprintf("%d:%d:%s", outputIndex, contentIndex, kind)
}

func toolCallKey(event conversationEvent) string {
	if event.OutputIndex != nil {
		return fmt.Sprintf("output:%d", *event.OutputIndex)
	}
	if event.ToolCallID != "" {
		return "tool:" + event.ToolCallID
	}
	return "entry:" + event.ID
}

func mistralStopReason(reason string) sigma.StopReason {
	switch reason {
	case "stop", "end_turn", "end-turn":
		return sigma.StopReasonEndTurn
	case "length", "max_tokens", "model_length":
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
