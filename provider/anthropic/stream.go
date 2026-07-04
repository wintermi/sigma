// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/internal/sse"
	"github.com/wintermi/sigma/internal/streamblocks"
)

type streamEvent struct {
	Type              string          `json:"type"`
	Index             int             `json:"index"`
	Message           streamMessage   `json:"message"`
	Content           streamContent   `json:"content_block"`
	Delta             streamDelta     `json:"delta"`
	Usage             *streamUsage    `json:"usage"`
	ContextManagement map[string]any  `json:"context_management"`
	Error             *streamAPIError `json:"error"`
}

type streamMessage struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Role       string          `json:"role"`
	Model      string          `json:"model"`
	Content    []streamContent `json:"content"`
	StopReason string          `json:"stop_reason"`
	Usage      *streamUsage    `json:"usage"`
}

type streamContent struct {
	Type      string           `json:"type"`
	Text      string           `json:"text"`
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Input     any              `json:"input"`
	Thinking  string           `json:"thinking"`
	Signature string           `json:"signature"`
	Data      string           `json:"data"`
	Citations []map[string]any `json:"citations"`
}

type streamDelta struct {
	Type        string         `json:"type"`
	Text        string         `json:"text"`
	Thinking    string         `json:"thinking"`
	Signature   string         `json:"signature"`
	PartialJSON string         `json:"partial_json"`
	StopReason  string         `json:"stop_reason"`
	Citation    map[string]any `json:"citation"`
	Container   map[string]any `json:"container"`
}

type streamUsage struct {
	InputTokens              *int                 `json:"input_tokens"`
	OutputTokens             *int                 `json:"output_tokens"`
	CacheCreationInputTokens *int                 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int                 `json:"cache_read_input_tokens"`
	CacheCreation            *streamCacheCreation `json:"cache_creation"`
	OutputTokensDetails      *streamOutputDetails `json:"output_tokens_details"`
	ServerToolUse            map[string]int       `json:"server_tool_use"`
	ServiceTier              string               `json:"service_tier"`
}

type streamOutputDetails struct {
	ThinkingTokens int `json:"thinking_tokens"`
}

type streamCacheCreation struct {
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
}

type streamAPIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type streamParser struct {
	writer         sigma.StreamWriter
	model          sigma.Model
	compat         messagesCompat
	requestTools   []sigma.Tool
	final          sigma.AssistantMessage
	started        bool
	nextBlock      int
	text           map[int]*streamblocks.Text
	thinking       map[int]*streamblocks.Thinking
	toolCalls      map[int]*streamblocks.ToolCall
	responseID     string
	providerModel  string
	metadata       map[string]any
	usage          *sigma.Usage
	stopReason     sigma.StopReason
	messageStarted bool
	messageStopped bool
}

func parseMessagesStream(ctx context.Context, r io.Reader, writer sigma.StreamWriter, model sigma.Model, compat messagesCompat, tools []sigma.Tool) (sigma.AssistantMessage, error) {
	parser := streamParser{
		writer:       writer,
		model:        model,
		compat:       compat,
		requestTools: tools,
		text:         make(map[int]*streamblocks.Text),
		thinking:     make(map[int]*streamblocks.Thinking),
		toolCalls:    make(map[int]*streamblocks.ToolCall),
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
	if parser.messageStarted && !parser.messageStopped {
		return parser.finalize(ctx), fmt.Errorf("anthropic messages: stream ended before message_stop")
	}
	return parser.finalize(ctx), nil
}

func (p *streamParser) handleEvent(ctx context.Context, event sse.Event) error {
	if event.Event != "" && !isAnthropicStreamEvent(event.Event) {
		return nil
	}
	var parsed streamEvent
	if err := decodeStreamEvent(event.Data, &parsed); err != nil {
		return fmt.Errorf("anthropic messages: decode stream event: %w", err)
	}
	if parsed.Type == "" {
		parsed.Type = event.Event
	}
	switch parsed.Type {
	case "message_start":
		p.captureMessage(parsed.Message)
		p.messageStarted = true
		return p.emitStart(ctx)
	case "content_block_start":
		if err := p.emitStart(ctx); err != nil {
			return err
		}
		return p.handleContentBlockStart(ctx, parsed.Index, parsed.Content)
	case "content_block_delta":
		if err := p.emitStart(ctx); err != nil {
			return err
		}
		return p.handleDelta(ctx, parsed.Index, parsed.Delta)
	case "content_block_stop":
		return p.handleContentBlockStop(ctx, parsed.Index)
	case "message_delta":
		if parsed.Delta.StopReason != "" {
			p.stopReason = anthropicStopReason(parsed.Delta.StopReason)
		}
		if len(parsed.Delta.Container) > 0 {
			p.setMetadata("container", parsed.Delta.Container)
		}
		if len(parsed.ContextManagement) > 0 {
			p.setMetadata("context_management", parsed.ContextManagement)
		}
		if parsed.Usage != nil {
			p.mergeUsage(parsed.Usage)
		}
		return nil
	case "message_stop":
		p.messageStopped = true
		return sse.ErrStop
	case "ping":
		return nil
	case "error":
		if parsed.Error != nil {
			return streamError(p.model, parsed.Error)
		}
		return streamError(p.model, nil)
	default:
		return nil
	}
}

func (p *streamParser) captureMessage(message streamMessage) {
	if message.ID != "" {
		p.responseID = message.ID
	}
	if message.Model != "" {
		p.providerModel = message.Model
	}
	if message.StopReason != "" {
		p.stopReason = anthropicStopReason(message.StopReason)
	}
	if message.Usage != nil {
		p.mergeUsage(message.Usage)
	}
	for index, content := range message.Content {
		p.captureContent(index, content)
	}
}

func (p *streamParser) handleContentBlockStop(ctx context.Context, index int) error {
	if state := p.text[index]; state != nil && state.Started && !state.Closed {
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindTextEnd,
			ContentIndex: intPtr(state.ContentIndex),
			Text:         state.String(),
		}); err != nil {
			return err
		}
		state.Closed = true
		return nil
	}
	if state := p.thinking[index]; state != nil && state.Started && !state.Closed {
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindThinkingEnd,
			ContentIndex: intPtr(state.ContentIndex),
			Thinking:     state.String(),
		}); err != nil {
			return err
		}
		state.Closed = true
		return nil
	}
	if state := p.toolCalls[index]; state != nil && state.Started && !state.Closed {
		call := anthropicToolCall(state)
		if err := p.writer.Emit(ctx, sigma.Event{
			Kind:         sigma.EventKindToolCallEnd,
			ContentIndex: intPtr(state.ContentIndex),
			ToolCall:     &call,
		}); err != nil {
			return err
		}
		state.Closed = true
		return nil
	}
	return nil
}

func (p *streamParser) handleContentBlockStart(ctx context.Context, index int, content streamContent) error {
	p.captureContent(index, content)
	switch content.Type {
	case "text": //nolint:goconst
		return p.emitText(ctx, index, "")
	case "thinking":
		return p.emitThinking(ctx, index, "")
	case "redacted_thinking":
		_ = p.thinkingState(index)
		return nil
	case "tool_use", "server_tool_use":
		return p.emitToolCall(ctx, index, "")
	default:
		return nil
	}
}

func (p *streamParser) captureContent(index int, content streamContent) {
	switch content.Type {
	case "text":
		state := p.textState(index)
		if content.Text != "" {
			state.Set(content.Text)
		}
		if len(content.Citations) > 0 {
			state.ProviderMetadata = addCitations(state.ProviderMetadata, content.Citations)
		}
	case "thinking":
		state := p.thinkingState(index)
		if content.Thinking != "" {
			state.Set(content.Thinking)
		}
		if content.Signature != "" {
			state.Signature = content.Signature
		}
	case "redacted_thinking":
		state := p.thinkingState(index)
		state.Redacted = true
		if content.Data != "" {
			state.ProviderSignature = content.Data
		}
	case "tool_use", "server_tool_use":
		state := p.toolCallState(index)
		state.SetID(content.ID)
		name := content.Name
		if p.compat.claudeCodeIdentity {
			name = restoreCallerToolName(name, p.requestTools)
		}
		state.SetName(name)
		if content.Type == "server_tool_use" {
			state.ProviderMetadata = withProviderMetadata(state.ProviderMetadata, "type", "server_tool_use")
		}
		if content.Input != nil {
			data, err := json.Marshal(content.Input)
			if err == nil && (state.ArgumentsText() == "" || string(data) != "{}") {
				state.SetArguments(string(data))
			}
		}
	}
}

func (p *streamParser) handleDelta(ctx context.Context, index int, delta streamDelta) error {
	switch delta.Type {
	case "text_delta":
		return p.emitText(ctx, index, delta.Text)
	case "thinking_delta":
		return p.emitThinking(ctx, index, delta.Thinking)
	case "signature_delta":
		state := p.thinkingState(index)
		state.Signature += delta.Signature
		return nil
	case "input_json_delta":
		return p.emitToolCall(ctx, index, delta.PartialJSON)
	case "citations_delta":
		if len(delta.Citation) > 0 {
			state := p.textState(index)
			state.ProviderMetadata = addCitations(state.ProviderMetadata, []map[string]any{delta.Citation})
		}
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

func (p *streamParser) emitText(ctx context.Context, index int, delta string) error {
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

func (p *streamParser) emitThinking(ctx context.Context, index int, delta string) error {
	state := p.thinkingState(index)
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

func (p *streamParser) emitToolCall(ctx context.Context, index int, argumentsDelta string) error {
	state := p.toolCallState(index)
	if argumentsDelta != "" {
		if state.ArgumentsText() == "{}" {
			state.SetArguments("")
		}
		state.AppendArguments(argumentsDelta)
	}
	partial := state.Partial(argumentsDelta, streamblocks.ToolPartialArguments)
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

func (p *streamParser) finalize(ctx context.Context) sigma.AssistantMessage {
	contentByIndex := make(map[int]sigma.ContentBlock)
	for _, state := range p.sortedText() {
		block := sigma.Text(state.String())
		block.ProviderMetadata = copyAnyMap(state.ProviderMetadata)
		contentByIndex[state.ContentIndex] = block
	}
	for _, state := range p.sortedThinking() {
		block := sigma.Thinking(state.String(), state.Signature)
		block.Redacted = state.Redacted
		block.ProviderSignature = state.ProviderSignature
		contentByIndex[state.ContentIndex] = block
	}
	for _, state := range p.sortedToolCalls() {
		call := anthropicToolCall(state)
		block := sigma.ToolCallBlock(call.ID, call.Name, call.Arguments)
		block.ProviderSignature = call.ProviderSignature
		block.ProviderMetadata = copyAnyMap(call.ProviderMetadata)
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
		usage, cost := sigma.AccountUsage(p.model, *p.usage)
		p.final.Usage = &usage
		p.final.Cost = &cost
	}
	p.final.ProviderMetadata = p.responseMetadata()
	return p.final
}

func (p *streamParser) emitEndEvents(ctx context.Context) {
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
				call := anthropicToolCall(state)
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

func isAnthropicStreamEvent(name string) bool {
	switch name {
	case "message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop", "ping", "error":
		return true
	default:
		return false
	}
}

func decodeStreamEvent(data string, event *streamEvent) error {
	if err := json.Unmarshal([]byte(data), event); err == nil {
		return nil
	}
	repaired := repairJSON(data)
	if repaired == data {
		return json.Unmarshal([]byte(data), event)
	}
	return json.Unmarshal([]byte(repaired), event)
}

func anthropicToolCall(state *streamblocks.ToolCall) sigma.ToolCall {
	if _, ok := state.DecodeArguments(); !ok {
		if repaired := repairJSON(state.ArgumentsText()); repaired != state.ArgumentsText() && json.Valid([]byte(repaired)) {
			state.SetArguments(repaired)
		}
	}
	return state.ToolCall()
}

func repairJSON(input string) string {
	var repaired strings.Builder
	repaired.Grow(len(input))
	inString := false

	for index := 0; index < len(input); {
		r, size := utf8.DecodeRuneInString(input[index:])
		if r == utf8.RuneError && size == 1 {
			repaired.WriteByte(input[index])
			index++
			continue
		}

		if !inString {
			repaired.WriteString(input[index : index+size])
			if r == '"' {
				inString = true
			}
			index += size
			continue
		}

		switch r {
		case '"':
			repaired.WriteByte('"')
			inString = false
			index += size
		case '\\':
			if index+1 >= len(input) {
				repaired.WriteString(`\\`)
				index += size
				continue
			}
			next := input[index+1]
			if next == 'u' && index+6 <= len(input) && isHex4(input[index+2:index+6]) {
				repaired.WriteString(input[index : index+6])
				index += 6
				continue
			}
			if isJSONEscape(next) {
				repaired.WriteByte('\\')
				repaired.WriteByte(next)
				index += 2
				continue
			}
			repaired.WriteString(`\\`)
			index += size
		default:
			if r >= 0 && r <= 0x1f {
				writeEscapedControl(&repaired, r)
			} else {
				repaired.WriteString(input[index : index+size])
			}
			index += size
		}
	}

	return repaired.String()
}

func isJSONEscape(value byte) bool {
	switch value {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return true
	default:
		return false
	}
}

func isHex4(value string) bool {
	if len(value) != 4 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func writeEscapedControl(builder *strings.Builder, r rune) {
	switch r {
	case '\b':
		builder.WriteString(`\b`)
	case '\f':
		builder.WriteString(`\f`)
	case '\n':
		builder.WriteString(`\n`)
	case '\r':
		builder.WriteString(`\r`)
	case '\t':
		builder.WriteString(`\t`)
	default:
		fmt.Fprintf(builder, `\u%04x`, r)
	}
}

func (p *streamParser) textState(index int) *streamblocks.Text {
	state := p.text[index]
	if state == nil {
		state = &streamblocks.Text{ContentIndex: p.nextContentIndex()}
		p.text[index] = state
	}
	return state
}

func (p *streamParser) thinkingState(index int) *streamblocks.Thinking {
	state := p.thinking[index]
	if state == nil {
		state = &streamblocks.Thinking{ContentIndex: p.nextContentIndex()}
		p.thinking[index] = state
	}
	return state
}

func (p *streamParser) toolCallState(index int) *streamblocks.ToolCall {
	state := p.toolCalls[index]
	if state == nil {
		state = &streamblocks.ToolCall{ContentIndex: p.nextContentIndex()}
		p.toolCalls[index] = state
	}
	return state
}

func (p *streamParser) nextContentIndex() int {
	index := p.nextBlock
	p.nextBlock++
	return index
}

func (p *streamParser) sortedText() []*streamblocks.Text {
	states := make([]*streamblocks.Text, 0, len(p.text))
	for _, state := range p.text {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (p *streamParser) sortedThinking() []*streamblocks.Thinking {
	states := make([]*streamblocks.Thinking, 0, len(p.thinking))
	for _, state := range p.thinking {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (p *streamParser) sortedToolCalls() []*streamblocks.ToolCall {
	states := make([]*streamblocks.ToolCall, 0, len(p.toolCalls))
	for _, state := range p.toolCalls {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].ContentIndex < states[j].ContentIndex
	})
	return states
}

func (p *streamParser) mergeUsage(update *streamUsage) {
	if update == nil {
		return
	}
	usage := sigma.Usage{}
	if p.usage != nil {
		usage = *p.usage
	}
	if update.InputTokens != nil {
		usage.InputTokens = *update.InputTokens
	}
	if update.OutputTokens != nil {
		usage.OutputTokens = *update.OutputTokens
	}
	if update.CacheReadInputTokens != nil {
		usage.CacheReadInputTokens = *update.CacheReadInputTokens
	}
	if update.OutputTokensDetails != nil {
		usage.ThinkingTokens = update.OutputTokensDetails.ThinkingTokens
	}
	if len(update.ServerToolUse) > 0 {
		usage.ToolUseInputTokens = 0
		for _, tokens := range update.ServerToolUse {
			usage.ToolUseInputTokens += tokens
		}
	}
	if update.CacheCreation != nil {
		usage.LongCacheWriteInputTokens = update.CacheCreation.Ephemeral1hInputTokens
	}
	if update.CacheCreationInputTokens != nil {
		usage.CacheWriteInputTokens = *update.CacheCreationInputTokens
	} else if usage.CacheWriteInputTokens == 0 && update.CacheCreation != nil {
		usage.CacheWriteInputTokens = update.CacheCreation.Ephemeral5mInputTokens + update.CacheCreation.Ephemeral1hInputTokens
	}
	usage, _ = sigma.AccountUsage(p.model, usage, sigma.WithRawUsage(*update))
	p.usage = &usage
}

func (p *streamParser) setMetadata(key string, value any) {
	if p.metadata == nil {
		p.metadata = make(map[string]any)
	}
	p.metadata[key] = value
}

func (p *streamParser) responseMetadata() map[string]any {
	metadata := responseMetadata(p.responseID, p.providerModel, p.model.ID)
	if len(p.metadata) == 0 {
		return metadata
	}
	if metadata == nil {
		metadata = make(map[string]any, len(p.metadata))
	}
	for key, value := range p.metadata {
		metadata[key] = value
	}
	return metadata
}

func addCitations(metadata map[string]any, citations []map[string]any) map[string]any {
	if len(citations) == 0 {
		return metadata
	}
	if metadata == nil {
		metadata = make(map[string]any)
	}
	existing, _ := metadata["citations"].([]map[string]any)
	metadata["citations"] = append(existing, citations...)
	return metadata
}

func withProviderMetadata(metadata map[string]any, key string, value any) map[string]any {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata[key] = value
	return metadata
}

func anthropicStopReason(reason string) sigma.StopReason {
	switch reason {
	case "end_turn", "pause_turn":
		return sigma.StopReasonEndTurn
	case "max_tokens":
		return sigma.StopReasonMaxTokens
	case "stop_sequence":
		return sigma.StopReasonStopSequence
	case "tool_use":
		return sigma.StopReasonToolCalls
	case "refusal", "sensitive":
		return sigma.StopReasonContentFilter
	default:
		return sigma.StopReasonUnknown
	}
}

func streamError(model sigma.Model, err *streamAPIError) error {
	if err == nil {
		return sigma.NewProviderError(model.Provider, sigma.APIAnthropicMessages, model.ID, 0, "", 0, []byte(`{"error":{"message":"stream error"}}`), sigma.ErrProviderResponse)
	}
	body, _ := json.Marshal(map[string]any{"error": err})
	if err.Type == "invalid_request_error" && contextOverflowCause([]byte(err.Message)) != nil {
		return sigma.NewProviderError(model.Provider, sigma.APIAnthropicMessages, model.ID, 0, "", 0, body, sigma.ErrContextOverflow)
	}
	return sigma.NewProviderError(model.Provider, sigma.APIAnthropicMessages, model.ID, 0, "", 0, body, sigma.ErrProviderResponse)
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

func intPtr(value int) *int {
	return &value
}
