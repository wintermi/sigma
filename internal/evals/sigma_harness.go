// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wintermi/sigma"
)

// SigmaInput is one prompt sequence evaluated against a Sigma text model.
type SigmaInput struct {
	ID      string   `json:"id,omitempty"`
	Prompts []string `json:"prompts"`
}

// EvalGroupID returns the optional stable comparative identity.
func (i SigmaInput) EvalGroupID() string {
	return i.ID
}

// Prompt constructs a single-prompt Sigma input.
func Prompt(prompt string) SigmaInput {
	return SigmaInput{Prompts: []string{prompt}}
}

// Conversation constructs a multi-turn Sigma input.
func Conversation(prompts ...string) SigmaInput {
	return SigmaInput{Prompts: append([]string(nil), prompts...)}
}

const defaultSigmaToolRounds = 4

// SigmaToolOutput is one text result returned by a caller-owned evaluation tool.
type SigmaToolOutput struct {
	Text    string
	IsError bool
}

// SigmaToolExecutor executes one caller-owned tool call during an evaluation.
type SigmaToolExecutor func(context.Context, sigma.ToolCall) (SigmaToolOutput, error)

// SigmaHarnessConfig configures a Sigma-backed evaluation harness.
type SigmaHarnessConfig struct {
	Name                  string
	Client                *sigma.Client
	Model                 sigma.Model
	BaseRequest           sigma.Request
	Options               []sigma.Option
	TransformSystemPrompt func(string) (string, error)
	ToolExecutor          SigmaToolExecutor
	MaxToolRounds         int
}

// SigmaRun exposes the final response and complete replayable conversation to
// a caller-supplied output transform.
type SigmaRun struct {
	Response string
	Final    sigma.AssistantMessage
	Request  sigma.Request
	Messages []sigma.Message
}

// SigmaOutputFunc transforms a successful Sigma run into domain output.
type SigmaOutputFunc[O any] func(context.Context, SigmaRun) (O, error)

type sigmaHarness[O any] struct {
	name      string
	client    *sigma.Client
	model     sigma.Model
	base      sigma.Request
	options   []sigma.Option
	transform func(string) (string, error)
	executor  SigmaToolExecutor
	maxRounds int
	output    SigmaOutputFunc[O]
}

type sigmaTranscriptRecord struct {
	Type      string                  `json:"type"`
	Message   *sigma.Message          `json:"message,omitempty"`
	Assistant *sigma.AssistantMessage `json:"assistant,omitempty"`
	Usage     *Usage                  `json:"usage,omitempty"`
}

// NewSigmaTextHarness constructs a harness whose output is final assistant text.
func NewSigmaTextHarness(config SigmaHarnessConfig) (Harness[SigmaInput, string], error) {
	return NewSigmaHarness(config, func(_ context.Context, run SigmaRun) (string, error) {
		return run.Response, nil
	})
}

// NewSigmaHarness constructs a Sigma-backed harness with domain output.
func NewSigmaHarness[O any](config SigmaHarnessConfig, output SigmaOutputFunc[O]) (Harness[SigmaInput, O], error) {
	name := strings.TrimSpace(config.Name)
	if name == "" {
		name = "sigma-text"
	}
	if config.Client == nil {
		return nil, errors.New("evals: Sigma client is required")
	}
	if err := sigma.ValidateModelRef(sigma.ModelRef{Provider: config.Model.Provider, ID: config.Model.ID}); err != nil {
		return nil, fmt.Errorf("evals: Sigma model: %w", err)
	}
	if output == nil {
		return nil, errors.New("evals: Sigma output transform is required")
	}
	if config.MaxToolRounds < 0 {
		return nil, errors.New("evals: Sigma max tool rounds must not be negative")
	}
	if config.ToolExecutor == nil && config.MaxToolRounds != 0 {
		return nil, errors.New("evals: Sigma max tool rounds require a tool executor")
	}
	base, err := cloneSigmaRequest(config.BaseRequest)
	if err != nil {
		return nil, fmt.Errorf("evals: Sigma base request: %w", err)
	}
	maxRounds := config.MaxToolRounds
	if config.ToolExecutor != nil && maxRounds == 0 {
		maxRounds = defaultSigmaToolRounds
	}
	return &sigmaHarness[O]{
		name:      name,
		client:    config.Client,
		model:     config.Model,
		base:      base,
		options:   append([]sigma.Option(nil), config.Options...),
		transform: config.TransformSystemPrompt,
		executor:  config.ToolExecutor,
		maxRounds: maxRounds,
		output:    output,
	}, nil
}

func (h *sigmaHarness[O]) HarnessName() string {
	if h == nil {
		return ""
	}
	return h.name
}

func (h *sigmaHarness[O]) Run(
	ctx context.Context,
	input SigmaInput,
	runContext *RunContext,
) (result RunResult[O], runErr error) {
	startedAt := time.Now()
	transcript := make([]sigmaTranscriptRecord, 0, len(h.base.Messages)+len(input.Prompts)*2+1)
	defer func() {
		result.Timings.Total = time.Since(startedAt)
		transcript = append(transcript, sigmaTranscriptRecord{Type: "usage", Usage: &result.Usage})
		attachmentErr := attachSigmaTranscript(runContext, transcript)
		runErr = errors.Join(runErr, attachmentErr)
	}()

	if runContext == nil {
		return result, errors.New("evals: run context is required")
	}
	if len(input.Prompts) == 0 {
		return result, errors.New("evals: Sigma input must include at least one prompt")
	}
	request, err := cloneSigmaRequest(h.base)
	if err != nil {
		return result, fmt.Errorf("evals: clone Sigma request: %w", err)
	}
	if h.transform != nil {
		request.SystemPrompt, err = h.transform(request.SystemPrompt)
		if err != nil {
			return result, fmt.Errorf("evals: transform Sigma system prompt: %w", err)
		}
	}
	for i := range request.Messages {
		message := request.Messages[i]
		transcript = append(transcript, sigmaTranscriptRecord{Type: "message", Message: &message})
		result.Events = append(result.Events, transcriptEventsForMessage(message)...)
	}

	result.Usage.Provider = string(h.model.Provider)
	result.Usage.Model = string(h.model.ID)
	var response string
	var final sigma.AssistantMessage
	for _, prompt := range input.Prompts {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if strings.TrimSpace(prompt) == "" {
			return result, errors.New("evals: Sigma prompts must not be empty")
		}
		user := sigma.UserText(prompt)
		request.Messages = append(request.Messages, user)
		transcript = append(transcript, sigmaTranscriptRecord{Type: "message", Message: &user})
		result.Events = append(result.Events, transcriptEventsForMessage(user)...)

		toolRounds := 0
		for {
			final, err = h.client.Complete(ctx, h.model, request, h.options...)
			accumulateSigmaUsage(&result.Usage, h.model, final)
			assistant := cloneAssistant(final)
			transcript = append(transcript, sigmaTranscriptRecord{Type: "assistant", Assistant: &assistant})
			result.Events = append(result.Events, transcriptEventsForAssistant(final)...)
			if err != nil {
				return result, err
			}
			request.Messages = append(request.Messages, assistantMessage(h.model, final))

			switch final.StopReason {
			case sigma.StopReasonEndTurn:
				response, err = evalAssistantText(final)
				if err != nil {
					return result, err
				}
				if strings.TrimSpace(response) == "" {
					return result, errors.New("evals: Sigma run produced no assistant text")
				}
			case sigma.StopReasonToolCalls:
				if h.executor == nil {
					return result, fmt.Errorf("evals: Sigma run ended with unexpected stop reason %q", final.StopReason)
				}
				if toolRounds >= h.maxRounds {
					return result, fmt.Errorf("evals: Sigma run exceeded %d tool rounds", h.maxRounds)
				}
				calls, callErr := sigmaToolCalls(final)
				if callErr != nil {
					return result, callErr
				}
				toolRounds++
				for _, call := range calls {
					toolOutput, executeErr := h.executor(ctx, call)
					if executeErr != nil {
						return result, fmt.Errorf("evals: execute Sigma tool %q: %w", call.Name, executeErr)
					}
					toolMessage := sigma.ToolResult(call.ID, toolOutput.Text)
					if toolOutput.IsError {
						toolMessage = sigma.ToolError(call.ID, toolOutput.Text)
					}
					toolMessage.ToolName = call.Name
					request.Messages = append(request.Messages, toolMessage)
					transcriptMessage := toolMessage
					transcript = append(transcript, sigmaTranscriptRecord{Type: "message", Message: &transcriptMessage})
					result.Events = append(result.Events, transcriptEventsForMessage(toolMessage)...)
				}
				continue
			default:
				return result, fmt.Errorf("evals: Sigma run ended with unexpected stop reason %q", final.StopReason)
			}
			break
		}
	}

	output, err := h.output(ctx, SigmaRun{
		Response: response,
		Final:    cloneAssistant(final),
		Request:  request,
		Messages: cloneMessages(request.Messages),
	})
	result.Output = output
	if err != nil {
		return result, fmt.Errorf("evals: transform Sigma output: %w", err)
	}
	return result, nil
}

func attachSigmaTranscript(run *RunContext, records []sigmaTranscriptRecord) error {
	if run == nil {
		return nil
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return fmt.Errorf("evals: encode Sigma transcript: %w", err)
		}
	}
	return run.Attach(AttachmentTranscript, "transcript.jsonl", "application/jsonl", body.Bytes())
}

func cloneSigmaRequest(request sigma.Request) (sigma.Request, error) {
	encoded, err := sigma.MarshalRequest(request)
	if err != nil {
		return sigma.Request{}, fmt.Errorf("marshal request: %w", err)
	}
	cloned, err := sigma.UnmarshalRequest(encoded)
	if err != nil {
		return sigma.Request{}, fmt.Errorf("unmarshal request: %w", err)
	}
	return cloned, nil
}

func cloneAssistant(message sigma.AssistantMessage) sigma.AssistantMessage {
	encoded, err := json.Marshal(message)
	if err != nil {
		return message
	}
	var cloned sigma.AssistantMessage
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return message
	}
	return cloned
}

func cloneMessages(messages []sigma.Message) []sigma.Message {
	request, err := cloneSigmaRequest(sigma.Request{Messages: messages})
	if err != nil {
		return append([]sigma.Message(nil), messages...)
	}
	return request.Messages
}

func assistantMessage(model sigma.Model, final sigma.AssistantMessage) sigma.Message {
	message := sigma.Message{
		Role:       sigma.RoleAssistant,
		Content:    make([]sigma.ContentBlock, len(final.Content)),
		Provider:   final.Provider,
		API:        model.API,
		Model:      final.Model,
		StopReason: final.StopReason,
	}
	if message.Provider == "" {
		message.Provider = model.Provider
	}
	if message.Model == "" {
		message.Model = model.ID
	}
	for i, block := range final.Content {
		message.Content[i] = block.Clone()
	}
	if final.Usage != nil {
		usage := *final.Usage
		message.Usage = &usage
	}
	return message
}

func evalAssistantText(message sigma.AssistantMessage) (string, error) {
	var text strings.Builder
	for _, block := range message.Content {
		switch block.Type {
		case sigma.ContentBlockText:
			text.WriteString(block.Text)
		case sigma.ContentBlockThinking:
		case sigma.ContentBlockToolCall, sigma.ContentBlockImage, sigma.ContentBlockDocument:
			return "", fmt.Errorf("evals: Sigma assistant message contains unsupported %q content", block.Type)
		default:
			return "", fmt.Errorf("evals: Sigma assistant message contains unknown %q content", block.Type)
		}
	}
	return text.String(), nil
}

func sigmaToolCalls(message sigma.AssistantMessage) ([]sigma.ToolCall, error) {
	var calls []sigma.ToolCall
	for _, content := range message.Content {
		if content.Type != sigma.ContentBlockToolCall {
			continue
		}
		block := content.Clone()
		if strings.TrimSpace(block.ToolCallID) == "" {
			return nil, errors.New("evals: Sigma tool call is missing an id")
		}
		if strings.TrimSpace(block.ToolName) == "" {
			return nil, errors.New("evals: Sigma tool call is missing a name")
		}
		calls = append(calls, sigma.ToolCall{
			ID:                block.ToolCallID,
			Name:              block.ToolName,
			Arguments:         block.ToolArguments,
			ProviderSignature: block.ProviderSignature,
			ProviderMetadata:  block.ProviderMetadata,
		})
	}
	if len(calls) == 0 {
		return nil, errors.New("evals: Sigma run stopped for tool calls without returning any tool calls")
	}
	return calls, nil
}

func accumulateSigmaUsage(usage *Usage, model sigma.Model, final sigma.AssistantMessage) {
	if final.Usage != nil {
		usage.InputTokens += final.Usage.InputTokens
		usage.OutputTokens += final.Usage.OutputTokens
		usage.TotalTokens += final.Usage.Total()
		if usage.Metadata == nil {
			usage.Metadata = make(map[string]any)
		}
		addUsageMetadata(usage.Metadata, "cacheReadTokens", final.Usage.CacheReadInputTokens)
		addUsageMetadata(usage.Metadata, "cacheWriteTokens", final.Usage.CacheWriteInputTokens)
	}
	for _, block := range final.Content {
		if block.Type == sigma.ContentBlockToolCall {
			usage.ToolCalls++
		}
	}
	if !modelHasUSDPrice(model) {
		return
	}
	cost := final.Cost
	if cost == nil && final.Usage != nil {
		calculated := sigma.CostForUsage(model, *final.Usage)
		cost = &calculated
	}
	if cost == nil || (cost.Currency != "" && !strings.EqualFold(cost.Currency, "USD")) {
		return
	}
	if usage.EstimatedCostUSD == nil {
		usage.EstimatedCostUSD = new(float64)
	}
	*usage.EstimatedCostUSD += cost.TotalCost
}

func addUsageMetadata(metadata map[string]any, name string, value int) {
	if value == 0 {
		return
	}
	current, _ := metadata[name].(int)
	metadata[name] = current + value
}

func modelHasUSDPrice(model sigma.Model) bool {
	if model.CostCurrency != "" && !strings.EqualFold(model.CostCurrency, "USD") {
		return false
	}
	if model.InputCostPerMillion != 0 || model.OutputCostPerMillion != 0 ||
		model.CacheReadInputCostPerMillion != 0 || model.CacheWriteInputCostPerMillion != 0 {
		return true
	}
	for _, tier := range model.CostTiers {
		if tier.InputCostPerMillion != 0 || tier.OutputCostPerMillion != 0 ||
			tier.CacheReadInputCostPerMillion != 0 || tier.CacheWriteInputCostPerMillion != 0 {
			return true
		}
	}
	return false
}

func transcriptEventsForAssistant(message sigma.AssistantMessage) []TranscriptEvent {
	persisted := sigma.Message{Role: sigma.RoleAssistant, Content: message.Content}
	return transcriptEventsForMessage(persisted)
}

func transcriptEventsForMessage(message sigma.Message) []TranscriptEvent {
	var events []TranscriptEvent
	var text strings.Builder
	for _, block := range message.Content {
		switch block.Type {
		case sigma.ContentBlockText:
			text.WriteString(block.Text)
		case sigma.ContentBlockThinking:
			events = append(events, TranscriptEvent{Type: "thinking", Role: string(message.Role), Content: block.ThinkingText})
		case sigma.ContentBlockToolCall:
			events = append(events, TranscriptEvent{
				Type:      "tool_call",
				Role:      string(message.Role),
				ID:        block.ToolCallID,
				Name:      block.ToolName,
				Arguments: normalizeArguments(block.ToolArguments),
			})
		case sigma.ContentBlockImage, sigma.ContentBlockDocument:
			events = append(events, TranscriptEvent{Type: string(block.Type), Role: string(message.Role), Content: block})
		default:
		}
	}
	if text.Len() > 0 {
		events = append([]TranscriptEvent{{Type: "message", Role: string(message.Role), Content: text.String()}}, events...)
	}
	if message.Role == sigma.RoleTool {
		content := text.String()
		event := TranscriptEvent{
			Type:       "tool_result",
			Role:       string(message.Role),
			ToolCallID: message.ToolCallID,
			Name:       message.ToolName,
			Content:    content,
		}
		if message.IsError {
			event.Error = content
		}
		return []TranscriptEvent{event}
	}
	return events
}

func normalizeArguments(arguments any) map[string]any {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return nil
	}
	var normalized map[string]any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil
	}
	return normalized
}
